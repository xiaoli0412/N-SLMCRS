// Command gateway 是 N-SLMCRS 网关主入口。
//
// 启动流程：
//  1. 加载配置
//  2. 打开数据库
//  3. 注册已存上游 Key 到限流器
//  4. 创建上游客户端、调度器、入口处理器、管理处理器
//  5. 启动模型目录同步器（goroutine）
//  6. 启动 HTTP 服务
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/admin"
	"github.com/nslmcrs/gateway/internal/autopilot"
	"github.com/nslmcrs/gateway/internal/backup"
	"github.com/nslmcrs/gateway/internal/config"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/entry"
	"github.com/nslmcrs/gateway/internal/hooks"
	"github.com/nslmcrs/gateway/internal/logging"
	"github.com/nslmcrs/gateway/internal/modelhealth"
	"github.com/nslmcrs/gateway/internal/modelmeta"
	"github.com/nslmcrs/gateway/internal/ratelimit"
	"github.com/nslmcrs/gateway/internal/scheduler"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// version 通过 -ldflags "-X main.version=..." 注入（Dockerfile 默认注入 v0.9.0）；
// go run 直跑时回退到此默认值，保持与前端 package.json 一致。
var version = "v0.9.0"

func main() {
	// -version：打印版本后退出（供 Docker healthcheck 与运维探活使用）
	showVersion := flag.Bool("version", false, "打印版本号并退出")
	flag.Parse()
	if *showVersion {
		fmt.Println("n-slmcrs gateway", version)
		os.Exit(0)
	}

	// 1. 配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if cfg.Server.AdminToken == "" {
		log.Println("[WARN] 未配置 ADMIN_TOKEN，管理 API 处于无鉴权模式（仅供开发）")
	}

	// 2. 数据库
	store, err := data.Open(cfg.Data.SQLitePath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	// 2b. 统一结构化日志（v0.9）：slog 扇出 stdout + app_logs；trace_id 经 context 注入
	logger := logging.New(store, logging.Config{
		Level:  logging.ParseLevel(cfg.Logging.Level),
		Format: cfg.Logging.Format,
	})
	slog.SetDefault(logger.StdLogger()) // 让第三方库的 slog 调用也走统一处理器

	// 3. 限流器 + 健康追踪器
	rlMgr := ratelimit.NewManager(cfg.Upstream.DefaultRPM)
	health := ratelimit.NewHealthTracker(2 * time.Minute)
	if err := registerKeys(context.Background(), store, rlMgr, logger); err != nil {
		logger.Warn(context.Background(), "server", "注册已存密钥失败: "+err.Error())
	}
	// 3b. NVIDIA_TEST_KEY 非空则启动时幂等注册为首个上游密钥
	seedTestKey(context.Background(), store, rlMgr, logger)

	// 4. 上游客户端
	client := upstream.NewClient(cfg.Upstream.ChatBaseURL, cfg.Upstream.RetrievalBaseURL, cfg.Upstream.RequestTimeout)

	// 5. 调度器
	sched := scheduler.New(store, client, rlMgr, health, scheduler.SchedulerConfig{
		DefaultConcurrency: cfg.Scheduler.DefaultConcurrency,
		MaxConcurrency:     cfg.Scheduler.MaxConcurrency,
		RequestTimeout:     cfg.Scheduler.RequestTimeout,
		CircuitThreshold:   cfg.Scheduler.CircuitThreshold,
		CircuitCooldown:    cfg.Scheduler.CircuitCooldown,
		HealthWindow:       2 * time.Minute,
	})

	// 后台任务根上下文（优雅关闭时取消）；提前创建以便启动阶段复用
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5b. Auto-Pilot：共享 Runtime 注入调度器，Controller 周期决策 → Executor 写 Runtime/落库
	apRuntime := autopilot.NewRuntime()
	sched.SetRuntime(apRuntime)
	// 5b.1 Webhook 事件回调（v0.10）：成功/失败/限流时异步外发通知
	webhook := hooks.NewWebhook(hooks.WebhookConfig{
		URL:    cfg.Hooks.WebhookURL,
		Secret: cfg.Hooks.WebhookSecret,
		Events: cfg.Hooks.WebhookEvents,
	})
	sched.SetWebhook(webhook)
	apCtrl := autopilot.NewController(store, health, rlMgr, apRuntime,
		2*time.Minute, cfg.Scheduler.DefaultConcurrency, cfg.Scheduler.MaxConcurrency,
		autopilot.LLMConfig{BaseURL: cfg.AutoPilot.LLMBaseURL, APIKey: cfg.AutoPilot.LLMAPIKey, Model: cfg.AutoPilot.LLMModel})

	// 5c. 启动时加载已持久化的熔断/调度覆盖（来自 settings 表）并应用到调度器
	if err := admin.LoadPersistedSchedulerOverrides(rootCtx, sched, store); err != nil {
		logger.Warn(rootCtx, "server", "加载已持久化的调度覆盖失败: "+err.Error())
	}

	// 6. 失效检测 + 模型同步
	checker := modelmeta.NewStalenessChecker(store)
	syncer := modelmeta.NewSyncer(store, client, cfg.Upstream.ModelSyncInterval)
	// 6b. 模型探活器（主动可用度检测；与 request_logs 被动统计互补）
	prober := modelmeta.NewProber(store, client, 10*time.Minute)
	// 6c. 模型级健康扫描与熔断（v0.9）：按模型遍历所有 NVIDIA 接口探测，判定 closed/open/permanent
	healthSweeper := modelhealth.New(store, client, modelhealth.Config{
		ProbeCount:           cfg.ModelHealth.ProbeCount,
		ProbeInterval:        cfg.ModelHealth.ProbeInterval,
		SweepInterval:        cfg.ModelHealth.SweepInterval,
		SuccessRateFloor:     cfg.ModelHealth.SuccessRateFloor,
		SuccessRateThreshold: cfg.ModelHealth.SuccessRateThreshold,
		BadSweepToPermanent:  cfg.ModelHealth.BadSweepToPermanent,
		CooldownBase:         cfg.ModelHealth.CooldownBase,
	})
	// 6c.1 启动时加载已持久化的模型健康扫描覆盖（来自 settings 表）
	if err := admin.LoadPersistedModelHealthOverrides(rootCtx, healthSweeper, store); err != nil {
		logger.Warn(rootCtx, "server", "加载已持久化的模型健康覆盖失败: "+err.Error())
	}
	// 6d. 数据库备份服务（v0.8）：VACUUM INTO 快照 + 定时轮转；未配置 interval 则仅手动触发
	backupSvc := backup.New(store, cfg.Backup.Dir, cfg.Backup.Interval, cfg.Backup.Retention)

	go apCtrl.Start(rootCtx)      // Auto-Pilot 2 分钟决策循环
	go syncer.Start(rootCtx)      // 模型目录同步
	go prober.Start(rootCtx)      // 模型探活
	go healthSweeper.Start(rootCtx) // 模型级健康扫描与熔断
	go backupSvc.Start(rootCtx)   // 数据库定时备份

	// 7. 路由
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger(logger))

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "n-slmcrs", "version": version})
	})

	// 转发 API（下游凭证鉴权）
	entryHandler := entry.New(sched, store, checker)
	// /v1/models 允许匿名；其余需凭证
	entryHandler.RegisterRoutesWithAuth(r, entry.AuthMiddleware(store, []string{"/v1/models"}))

	// 管理 API（鉴权由 Handler 持有，token 默认 admin + 首次强制改密）
	adminHandler := admin.New(store, syncer, cfg)
	adminHandler.SetAutopilot(apCtrl)
	adminHandler.SetScheduler(sched)
	adminHandler.SetProber(prober)
	adminHandler.SetBackup(backupSvc)
	adminHandler.SetHealthSweeper(healthSweeper)
	adminHandler.SetWebhook(webhook) // v0.10：启用 /api/admin/hooks/* 渠道管理与 webhook 配置/测试
	adminHandler.RegisterRoutes(r)

	// 前端静态资源 + SPA 兜底（最后注册）
	entry.ServeFrontend(r)

	// 启动服务
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	go func() {
		logger.Info(rootCtx, "server", "网关启动，监听 "+addr, "endpoints", "/v1/* /api/admin/*")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务失败: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info(context.Background(), "server", "收到关闭信号，优雅关闭中...")
	cancel()
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error(context.Background(), "server", "关闭异常: "+err.Error())
	}
	logger.Info(context.Background(), "server", "已关闭")
}

// registerKeys 把数据库中已存的上游 Key 注册到限流器。
func registerKeys(ctx context.Context, store *data.Store, rl *ratelimit.Manager, logger *logging.Logger) error {
	keys, err := store.ListUpstreamKeys(ctx)
	if err != nil {
		return err
	}
	for _, k := range keys {
		rl.Register(k.ID, k.RPMOverride)
	}
	logger.Info(ctx, "server", "已注册上游密钥到限流器", "count", len(keys))
	return nil
}

// seedTestKey 若 NVIDIA_TEST_KEY 非空，启动时幂等注册为首个上游密钥。
// 已存在则跳过（BulkAddUpstreamKeys 对 UNIQUE 冲突视为已存在）；新增后注册到限流器。
func seedTestKey(ctx context.Context, store *data.Store, rl *ratelimit.Manager, logger *logging.Logger) {
	kv := strings.TrimSpace(os.Getenv("NVIDIA_TEST_KEY"))
	if kv == "" {
		return
	}
	res, err := store.BulkAddUpstreamKeys(ctx, []string{kv}, "NVIDIA_TEST_KEY", "", 0)
	if err != nil {
		logger.Warn(ctx, "server", "注册 NVIDIA_TEST_KEY 失败: "+err.Error())
		return
	}
	for _, id := range res.AddedIDs {
		rl.Register(id, 0)
	}
	if res.Added > 0 {
		logger.Info(ctx, "server", "已从 NVIDIA_TEST_KEY 注册上游密钥", "added", res.Added)
	}
}

// requestLogger 结构化请求日志中间件（v0.9：slog + trace_id 落 app_logs）。
func requestLogger(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info(c.Request.Context(), "entry",
			"request",
			c.Request.Method+" "+c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds())
	}
}
