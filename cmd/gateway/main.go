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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/admin"
	"github.com/nslmcrs/gateway/internal/autopilot"
	"github.com/nslmcrs/gateway/internal/config"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/entry"
	"github.com/nslmcrs/gateway/internal/modelmeta"
	"github.com/nslmcrs/gateway/internal/ratelimit"
	"github.com/nslmcrs/gateway/internal/scheduler"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// version 通过 -ldflags "-X main.version=..." 注入；默认 v0.4.0。
var version = "v0.4.0"

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
	} else {
		// 提示初始默认令牌：首次登录后强制修改并写入 bcrypt 哈希。
		log.Printf("[INFO] 管理面板初始令牌=%q（未修改前为默认值，首次登录后将强制改密）", cfg.Server.AdminToken)
	}

	// 2. 数据库
	store, err := data.Open(cfg.Data.SQLitePath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer store.Close()

	// 3. 限流器 + 健康追踪器
	rlMgr := ratelimit.NewManager(cfg.Upstream.DefaultRPM)
	health := ratelimit.NewHealthTracker(2 * time.Minute)
	if err := registerKeys(context.Background(), store, rlMgr); err != nil {
		log.Printf("[WARN] 注册已存密钥失败: %v", err)
	}

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
	apCtrl := autopilot.NewController(store, health, rlMgr, apRuntime,
		2*time.Minute, cfg.Scheduler.DefaultConcurrency, cfg.Scheduler.MaxConcurrency,
		autopilot.LLMConfig{BaseURL: cfg.AutoPilot.LLMBaseURL, APIKey: cfg.AutoPilot.LLMAPIKey, Model: cfg.AutoPilot.LLMModel})

	// 5c. 启动时加载已持久化的熔断/调度覆盖（来自 settings 表）并应用到调度器
	if err := admin.LoadPersistedSchedulerOverrides(rootCtx, sched, store); err != nil {
		log.Printf("[WARN] 加载已持久化的调度覆盖失败: %v", err)
	}

	// 6. 失效检测 + 模型同步
	checker := modelmeta.NewStalenessChecker(store)
	syncer := modelmeta.NewSyncer(store, client, cfg.Upstream.ModelSyncInterval)

	go apCtrl.Start(rootCtx) // Auto-Pilot 30s 决策循环
	go syncer.Start(rootCtx) // 模型目录同步

	// 7. 路由
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestLogger())

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
		log.Printf("[N-SLMCRS] 网关启动，监听 %s", addr)
		log.Printf("[N-SLMCRS] 转发端点: POST /v1/chat/completions 等")
		log.Printf("[N-SLMCRS] 管理 API: /api/admin/*")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务失败: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[N-SLMCRS] 收到关闭信号，优雅关闭中...")
	cancel()
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[N-SLMCRS] 关闭异常: %v", err)
	}
	log.Println("[N-SLMCRS] 已关闭")
}

// registerKeys 把数据库中已存的上游 Key 注册到限流器。
func registerKeys(ctx context.Context, store *data.Store, rl *ratelimit.Manager) error {
	keys, err := store.ListUpstreamKeys(ctx)
	if err != nil {
		return err
	}
	for _, k := range keys {
		rl.Register(k.ID, k.RPMOverride)
	}
	log.Printf("[启动] 已注册 %d 个上游密钥到限流器", len(keys))
	return nil
}

// requestLogger 简易请求日志中间件。
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("[%.0fms] %s %s %d", float64(time.Since(start).Microseconds())/1000, c.Request.Method, c.Request.URL.Path, c.Writer.Status())
	}
}
