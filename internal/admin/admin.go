// Package admin 提供管理 API：上游密钥、下游凭证、指标查询。
//
// 鉴权：默认初始令牌 admin，首次登录后强制改密并写入 bcrypt 哈希（admin:token_hash）。
// 鉴权端点（无需常规中间件）：
//   POST /api/admin/login             校验令牌，返回 must_change_password
//   POST /api/admin/change-password   修改令牌（写入 bcrypt 哈希，旧默认令牌失效）
//   GET  /api/admin/auth/status       探测是否需强制改密
// Phase 1 端点（受 AuthMiddleware 保护）：
//   GET    /api/admin/keys              列出上游密钥
//   POST   /api/admin/keys              新增上游密钥
//   DELETE /api/admin/keys/:id          删除上游密钥
//   PATCH  /api/admin/keys/:id          更新（启用/停用）
//   GET    /api/admin/credentials        列出下游凭证
//   POST   /api/admin/credentials        新增下游凭证
//   DELETE /api/admin/credentials/:id    删除下游凭证
//   GET    /api/admin/metrics            聚合指标（?window=1h|24h|7d）
//   GET    /api/admin/timeseries         时序曲线（?window=1h&bucket=60）
//   GET    /api/admin/key-health         每 Key 健康
//   GET    /api/admin/models             模型目录（含失效）
//   GET    /api/admin/models/plaza        模型广场视图（capability 过滤 + 用量统计 + 可用度）
//   POST   /api/admin/models/sync         立即同步模型
//   POST   /api/admin/models/test         探活单个模型（可用度测试）
//   POST   /api/admin/models/probe-all     探活所有 chat 模型
//   GET    /api/admin/settings            读取熔断/调度运行时配置
//   PUT    /api/admin/settings            更新熔断/调度运行时配置（落库 + 热生效）
//   GET    /api/admin/logs               查询日志
//   GET    /api/admin/backup             列出数据库备份
//   POST   /api/admin/backup             立即备份（VACUUM INTO 快照）
//   GET    /api/admin/backup/:file       下载备份文件
//   DELETE /api/admin/backup/:file       删除备份文件
// Phase 3 端点（Auto-Pilot）：
//   GET    /api/admin/autopilot/state                 完整状态
//   GET    /api/admin/autopilot/snapshot              决策快照
//   PUT    /api/admin/autopilot/mode                  热切换模式
//   PUT    /api/admin/autopilot/engine                热切换引擎
//   GET    /api/admin/autopilot/pending               待审建议
//   POST   /api/admin/autopilot/pending/:key/approve  批准
//   POST   /api/admin/autopilot/pending/:key/reject   驳回
package admin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/autopilot"
	"github.com/nslmcrs/gateway/internal/backup"
	"github.com/nslmcrs/gateway/internal/config"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/modelcatalog"
	"github.com/nslmcrs/gateway/internal/modelhealth"
	"github.com/nslmcrs/gateway/internal/modelmeta"
	"github.com/nslmcrs/gateway/internal/scheduler"
	"golang.org/x/crypto/bcrypt"
)

// settingKeyTokenHash 存储管理令牌的 bcrypt 哈希。
// 不存在时表示仍处于初始默认令牌状态（首次登录后强制改密）。
const settingKeyTokenHash = "admin:token_hash"

// Handler 管理 API 处理器。
type Handler struct {
	store    *data.Store
	syncer   *modelmeta.Syncer
	prober   *modelmeta.Prober     // 模型探活器（可选；main.go 装配后注入）
	cfg      *config.Config
	ap       *autopilot.Controller // Auto-Pilot 总控（可选；main.go 装配后注入）
	sched    *scheduler.Scheduler  // 调度器（可选；用于运行时改熔断/并发配置）
	bk       *backup.Service       // 数据库备份服务（v0.8；可选）
	sweeper  *modelhealth.Sweeper  // 模型级健康扫描器（v0.9；可选）
}

// New 创建管理 API 处理器。
func New(store *data.Store, syncer *modelmeta.Syncer, cfg *config.Config) *Handler {
	return &Handler{store: store, syncer: syncer, cfg: cfg}
}

// SetScheduler 注入调度器（启用 /api/admin/settings 运行时配置读写）。
func (h *Handler) SetScheduler(s *scheduler.Scheduler) *Handler {
	h.sched = s
	return h
}

// SetProber 注入模型探活器（启用 /api/admin/models/test 与 /models/probe-all）。
func (h *Handler) SetProber(p *modelmeta.Prober) *Handler {
	h.prober = p
	return h
}

// SetBackup 注入数据库备份服务（启用 /api/admin/backup 系列）。
func (h *Handler) SetBackup(b *backup.Service) *Handler {
	h.bk = b
	return h
}

// SetHealthSweeper 注入模型级健康扫描器（启用 /api/admin/models/circuit 系列，v0.9）。
func (h *Handler) SetHealthSweeper(s *modelhealth.Sweeper) *Handler {
	h.sweeper = s
	return h
}

// tokenFromRequest 从请求中提取管理令牌（X-Admin-Token 或 Bearer）。
func tokenFromRequest(c *gin.Context) string {
	token := c.GetHeader("X-Admin-Token")
	if token == "" {
		auth := c.GetHeader("Authorization")
		if len(auth) > 7 && auth[:7] == "Bearer " {
			token = auth[7:]
		}
	}
	return token
}

// verifyToken 校验令牌是否有效。
//
// 策略：若已设置哈希（admin:token_hash 存在）则用 bcrypt 比对；
// 否则（初始状态）与配置默认令牌做常量时间明文比对。
// 返回 (ok, isDefault)。isDefault=true 表示仍用默认初始令牌、应强制改密。
func (h *Handler) verifyToken(ctx context.Context, token string) (ok bool, isDefault bool, err error) {
	hash, _ := h.store.GetSetting(ctx, settingKeyTokenHash)
	if hash != "" {
		if bcryptErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)); bcryptErr != nil {
			return false, false, nil
		}
		return true, false, nil
	}
	// 初始状态：比对默认令牌（常量时间防时序）
	def := h.cfg.Server.AdminToken
	if def == "" {
		// 未配置且无哈希 → 无鉴权（仅开发模式）；视为非默认（不强制改密）
		return true, false, nil
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(def)) == 1 {
		return true, true, nil
	}
	return false, false, nil
}

// mustChangePassword 是否处于「首次登录需强制改密」状态。
func (h *Handler) mustChangePassword(ctx context.Context) bool {
	hash, _ := h.store.GetSetting(ctx, settingKeyTokenHash)
	return hash == "" && h.cfg.Server.AdminToken != ""
}

// AuthMiddleware 管理 API 鉴权（ADMIN_TOKEN 或 bcrypt 哈希）。
//
// 安全锁定：若仍处于「首次登录需强制改密」状态（无 bcrypt 哈希且配置了默认令牌），
// 所有受保护端点一律返回 403 + must_change_password=true，拒绝任何管理操作。
// 仅 /api/admin/login、/api/admin/change-password、/api/admin/auth/status 可用（注册在中间件组外）。
func (h *Handler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := tokenFromRequest(c)
		ok, isDefault, err := h.verifyToken(c.Request.Context(), token)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			c.Abort()
			return
		}
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的管理令牌"})
			c.Abort()
			return
		}
		// 强制改密锁定：默认令牌状态下拒绝所有管理操作
		if isDefault {
			c.JSON(http.StatusForbidden, gin.H{
				"error":                 "首次登录必须先修改管理令牌",
				"type":                  "must_change_password",
				"must_change_password":  true,
			})
			c.Header("X-Admin-Must-Change", "1")
			c.Abort()
			return
		}
		c.Next()
	}
}

// RegisterRoutes 注册管理路由。
//
// 受保护端点在 /api/admin 下走 AuthMiddleware；鉴权相关的 login /
// change-password / auth-status 注册在组外，便于首登与改密场景访问。
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	// 鉴权相关（无需常规中间件，内部自带校验）
	r.POST("/api/admin/login", h.login)
	r.POST("/api/admin/change-password", h.changePassword)
	r.GET("/api/admin/auth/status", h.authStatus)

	g := r.Group("/api/admin")
	g.Use(h.AuthMiddleware())
	{
		g.GET("/keys", h.listKeys)
		g.POST("/keys", h.addKey)
		g.POST("/keys/bulk", h.bulkAddKeys)
		g.DELETE("/keys/:id", h.deleteKey)
		g.PATCH("/keys/:id", h.updateKey)

		g.GET("/credentials", h.listCredentials)
		g.POST("/credentials", h.addCredential)
		g.DELETE("/credentials/:id", h.deleteCredential)

		g.GET("/metrics", h.getMetrics)
		g.GET("/timeseries", h.getTimeSeries)
		g.GET("/key-health", h.getKeyHealth)

		g.GET("/models", h.listModels)
		g.GET("/models/plaza", h.listModelsPlaza)
		// 模型二级/三级详情（v0.7）：模型 id 含 "/"（如 meta/llama-...），
		// Gin 路径参数不友好，故用查询参数 ?id= 传递完整模型 id。
		g.GET("/models/detail", h.getModelDetail)
		g.GET("/models/timeseries", h.getModelTimeSeries)
		g.GET("/models/probes", h.getModelProbes)
		g.POST("/models/sync", h.syncModels)
		g.POST("/models/test", h.testModel)
		g.POST("/models/probe-all", h.probeAllModels)

		// 模型级熔断（v0.9）
		g.GET("/models/circuit", h.listModelCircuit)
		g.POST("/models/health-sweep", h.healthSweep)
		g.POST("/models/circuit/reset", h.resetModelCircuit)

		g.GET("/settings", h.getSettings)
		g.PUT("/settings", h.putSettings)

		g.GET("/logs", h.getLogs)

		// 数据库备份（v0.8）
		g.GET("/backup", h.listBackups)
		g.POST("/backup", h.createBackup)
		g.GET("/backup/:file", h.downloadBackup)
		g.DELETE("/backup/:file", h.deleteBackup)

		// Auto-Pilot（Phase 3）
		g.GET("/autopilot/state", h.apState)
		g.GET("/autopilot/snapshot", h.apSnapshot)
		g.PUT("/autopilot/mode", h.apSetMode)
		g.PUT("/autopilot/engine", h.apSetEngine)
		g.GET("/autopilot/pending", h.apListPending)
		g.POST("/autopilot/pending/:key/approve", h.apApprovePending)
		g.POST("/autopilot/pending/:key/reject", h.apRejectPending)
	}
}

// --- 鉴权 / 改密 ---

type loginReq struct {
	Token string `json:"token"`
}

// login 校验令牌并返回是否需要强制改密。
// 成功响应：{ ok:true, must_change_password, is_default }
func (h *Handler) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ok, isDefault, err := h.verifyToken(c.Request.Context(), strings.TrimSpace(req.Token))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "无效的管理令牌"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "must_change_password": isDefault, "is_default": isDefault})
}

type changePasswordReq struct {
	Current string `json:"current"`
	Next    string `json:"next"`
}

// changePassword 修改管理令牌。
// 要求 current 有效、next 长度 ≥ 6 且不能等于默认值 admin。
// 成功后写入 bcrypt 哈希，旧默认令牌即失效。
func (h *Handler) changePassword(c *gin.Context) {
	var req changePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	current := strings.TrimSpace(req.Current)
	next := strings.TrimSpace(req.Next)

	// 校验当前令牌
	ok, _, err := h.verifyToken(c.Request.Context(), current)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前令牌无效"})
		return
	}
	// 校验新令牌强度
	if len(next) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新令牌至少 6 个字符"})
		return
	}
	// 拒绝使用初始默认令牌（大小写无关，含 "admin"/"ADMIN"）
	if strings.EqualFold(next, config.DefaultAdminToken) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新令牌不能使用初始默认值 " + config.DefaultAdminToken})
		return
	}
	if next == current {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新令牌不能与当前令牌相同"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.SetSetting(c.Request.Context(), settingKeyTokenHash, string(hash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// authStatus 返回鉴权状态（无需 token，供前端探测是否需强制改密）。
func (h *Handler) authStatus(c *gin.Context) {
	initialized := !h.mustChangePassword(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"initialized":         initialized,
		"must_change_password": !initialized,
	})
}

// --- 上游密钥 ---

type addKeyReq struct {
	KeyValue    string `json:"key_value"`
	Label       string `json:"label"`
	Email       string `json:"email"`
	RPMOverride int    `json:"rpm_override"`
}

func (h *Handler) listKeys(c *gin.Context) {
	keys, err := h.store.ListUpstreamKeys(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 返回脱敏值
	type keyView struct {
		ID          int64  `json:"id"`
		KeyMask     string `json:"key_mask"`
		Label       string `json:"label"`
		Email       string `json:"email"`
		RPMOverride int    `json:"rpm_override"`
		Enabled     bool   `json:"enabled"`
		Status      string `json:"status"`
		ConsecutiveFail int `json:"consecutive_fail"`
	}
	views := make([]keyView, len(keys))
	for i, k := range keys {
		views[i] = keyView{
			ID: k.ID, KeyMask: k.KeyMask, Label: k.Label, Email: k.Email,
			RPMOverride: k.RPMOverride, Enabled: k.Enabled, Status: k.Status,
			ConsecutiveFail: k.ConsecutiveFail,
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

func (h *Handler) addKey(c *gin.Context) {
	var req addKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	k, err := h.store.AddUpstreamKey(c.Request.Context(), req.KeyValue, req.Label, req.Email, req.RPMOverride)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": k.ID, "key_mask": k.KeyMask})
}

// bulkAddKeysReq 批量导入请求体。
// 既支持预拆分数组，也支持单个多行/逗号分隔字符串（便于粘贴）。
type bulkAddKeysReq struct {
	Keys        []string `json:"keys"`         // 精确数组
	Raw         string   `json:"raw"`          // 多行 / 逗号分隔粘贴文本（与 keys 合并）
	Label       string   `json:"label"`        // 批量统一标签
	Email       string   `json:"email"`
	RPMOverride int      `json:"rpm_override"`
}

// bulkAddKeys 批量导入上游密钥。
//
// 解析顺序：合并 keys 数组与 raw 文本 → 按换行/逗号/空白拆分 → 去空格。
// 导入在单事务内完成，逐条返回 added/duplicate/invalid 结果。
func (h *Handler) bulkAddKeys(c *gin.Context) {
	var req bulkAddKeysReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 合并数组与原始文本
	raws := append([]string{}, req.Keys...)
	if req.Raw != "" {
		raws = append(raws, splitKeys(req.Raw)...)
	}
	if len(raws) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未提供任何密钥"})
		return
	}

	res, err := h.store.BulkAddUpstreamKeys(c.Request.Context(), raws, req.Label, req.Email, req.RPMOverride)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total":   res.Total,
		"added":   res.Added,
		"skipped": res.Skipped,
		"items":   res.Items,
	})
}

// splitKeys 将粘贴文本拆成独立密钥串（兼容换行、逗号、空白、分号）。
func splitKeys(s string) []string {
	out := []string{}
	cur := make([]rune, 0, len(s))
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		switch r {
		case '\n', '\r', ',', ';', ' ', '\t':
			flush()
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

func (h *Handler) deleteKey(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteUpstreamKey(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type updateKeyReq struct {
	Enabled *bool `json:"enabled"`
}

func (h *Handler) updateKey(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req updateKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Enabled != nil {
		if err := h.store.SetUpstreamKeyEnabled(c.Request.Context(), id, *req.Enabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- 下游凭证 ---

type addCredReq struct {
	Name          string `json:"name"`
	RPMLimit      int    `json:"rpm_limit"`
	AllowedModels string `json:"allowed_models"`
}

func (h *Handler) listCredentials(c *gin.Context) {
	creds, err := h.store.ListDownstreamCredentials(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type credView struct {
		ID             int64  `json:"id"`
		CredentialMask string `json:"credential_mask"`
		Name           string `json:"name"`
		Enabled        bool   `json:"enabled"`
		RPMLimit       int    `json:"rpm_limit"`
		AllowedModels  string `json:"allowed_models"`
		TotalRequests  int64  `json:"total_requests"`
	}
	views := make([]credView, len(creds))
	for i, cr := range creds {
		views[i] = credView{
			ID: cr.ID, CredentialMask: cr.CredentialMask, Name: cr.Name,
			Enabled: cr.Enabled, RPMLimit: cr.RPMLimit, AllowedModels: cr.AllowedModels,
			TotalRequests: cr.TotalRequests,
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

func (h *Handler) addCredential(c *gin.Context) {
	var req addCredReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 生成 sk-nv- + 32 hex
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	cred := h.cfg.Auth.DownstreamKeyPrefix + hex.EncodeToString(b)
	cr, err := h.store.AddDownstreamCredential(c.Request.Context(), cred, req.Name, req.RPMLimit, req.AllowedModels)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": cr.ID, "credential": cred, "credential_mask": cr.CredentialMask})
}

func (h *Handler) deleteCredential(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteDownstreamCredential(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- 指标 ---

func (h *Handler) getMetrics(c *gin.Context) {
	window := parseWindow(c.Query("window"))
	m, err := h.store.GetMetrics(c.Request.Context(), window, c.Query("model"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *Handler) getTimeSeries(c *gin.Context) {
	window := parseWindow(c.Query("window"))
	bucket, _ := strconv.Atoi(c.DefaultQuery("bucket", "60"))
	if bucket <= 0 {
		bucket = 60
	}
	ts, err := h.store.GetTimeSeries(c.Request.Context(), window, bucket, c.Query("model"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ts})
}

func (h *Handler) getKeyHealth(c *gin.Context) {
	window := parseWindow(c.Query("window"))
	hl, err := h.store.GetKeyHealth(c.Request.Context(), window, c.Query("model"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": hl})
}

// --- 模型 ---

// modelView 模型广场富视图（契约对齐前端 Models.tsx）。
// 旧版前端误读了 model_id/status/alternative 等不存在的字段，这里显式给出 JSON 标签。
type modelView struct {
	ID            string  `json:"id"`
	Object        string  `json:"object"`
	Created       int64   `json:"created"`
	OwnedBy       string  `json:"owned_by"`
	Root          string  `json:"root"`
	Capability    string  `json:"capability"`
	ParamCount    string  `json:"param_count"`
	ContextLength int     `json:"context_length"`
	Description   string  `json:"description"`
	IsActive      bool    `json:"is_active"`
	Status        string  `json:"status"`              // active|gone|disabled（前端据 gone 灰暗）
	LastSeenActiveAt int64 `json:"last_seen_active_at"` // 最后活跃时刻
	SyncedAt      int64   `json:"synced_at"`
	// 用量统计（近 1h，被动流量聚合）
	RequestCount int64   `json:"request_count"`
	SuccessRate  float64 `json:"success_rate"` // 0..100
	// 可用度（被动聚合 + 主动探活，仿 new-api）
	AvailabilityScore float64 `json:"availability_score"` // 0..100 综合评分
	AvgLatencyMS      int64   `json:"avg_latency_ms"`
	ErrorCount        int64   `json:"error_count"`
	LastProbeTS       int64   `json:"last_probe_ts"`
	ProbeOK           bool    `json:"probe_ok"`
	ProbeStatus       string  `json:"probe_status"` // ok|error|timeout
	ProbeLatencyMS    int     `json:"probe_latency_ms"`
	// 扩展规格（v0.7 模型广场二阶面板"参数说明"，来自远程注册表，留空前端显示"—"）
	MaxTokens       int      `json:"max_tokens"`
	PricingIn       string   `json:"pricing_in"`
	PricingOut      string   `json:"pricing_out"`
	License         string   `json:"license"`
	InputModalities []string `json:"input_modalities"`
	ReleaseDate     string   `json:"release_date"`
	CardURL         string   `json:"card_url"`
	// v0.9：HF 富化架构 + 能力推导的支持接口
	Architecture        string   `json:"architecture"`
	SupportedInterfaces []string `json:"supported_interfaces"`
	// v0.9：模型级熔断状态
	CircuitState        string `json:"circuit_state"`         // closed|open|half_open|permanent
	CircuitSuccessRate  int    `json:"circuit_success_rate"`  // 最近扫描成功率
	CircuitPermanent    bool   `json:"circuit_permanent"`
	CircuitOpenUntil    int64  `json:"circuit_open_until"`
}

// toModelViews 将 data.Model 列表拼装为带用量统计、探活结果、扩展规格与熔断状态的广场视图。
func (h *Handler) toModelViews(ctx context.Context, ms []data.Model) []modelView {
	health, _ := h.store.ModelHealthStats(ctx, time.Hour)
	probes, _ := h.store.ListModelProbes(ctx)
	specs, _ := h.store.ListModelSpecs(ctx)
	views := make([]modelView, 0, len(ms))
	for _, m := range ms {
		u := health[m.ID]
		pr := probes[m.ID]
		sp := specs[m.ID]
		mc, _ := h.store.GetModelCircuit(ctx, m.ID)
		v := modelView{
			ID: m.ID, Object: m.Object, Created: m.Created, OwnedBy: m.OwnedBy, Root: m.Root,
			Capability: m.Capability, ParamCount: m.ParamCount, ContextLength: m.ContextLength,
			Description: m.Description, IsActive: m.IsActive, Status: m.Status, LastSeenActiveAt: m.LastSeenActiveAt, SyncedAt: m.SyncedAt,
			RequestCount:      u.RequestCount,
			SuccessRate:       u.SuccessRate,
			AvailabilityScore: u.AvailabilityScore,
			AvgLatencyMS:      u.AvgLatencyMS,
			ErrorCount:        u.ErrorCount,
			LastProbeTS:       pr.TS,
			ProbeOK:           pr.OK,
			ProbeStatus:       pr.Status,
			ProbeLatencyMS:    pr.LatencyMS,
			MaxTokens:         sp.MaxTokens,
			PricingIn:         sp.PricingIn,
			PricingOut:        sp.PricingOut,
			License:           sp.License,
			ReleaseDate:       sp.ReleaseDate,
			CardURL:           sp.CardURL,
			Architecture:      sp.Architecture,
		}
		if sp.InputModalities != "" {
			v.InputModalities = strings.Split(sp.InputModalities, ",")
		}
		// 支持接口：DB 已富化则取，否则按能力推导
		if sp.SupportedInterfaces != "" {
			v.SupportedInterfaces = strings.Split(sp.SupportedInterfaces, ",")
		} else {
			v.SupportedInterfaces = modelcatalog.SupportedInterfacesFor(m.Capability)
		}
		// 熔断状态（无记录视为 closed）
		v.CircuitState = data.CircuitClosed
		if mc != nil {
			v.CircuitState = mc.State
			v.CircuitSuccessRate = mc.SuccessRatePct
			v.CircuitPermanent = mc.Permanent
			v.CircuitOpenUntil = mc.OpenUntil
		}
		views = append(views, v)
	}
	return views
}

// listModels 列出全部模型（含已失效），带用量统计。
func (h *Handler) listModels(c *gin.Context) {
	ms, err := h.store.ListAllModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	views := h.toModelViews(c.Request.Context(), ms)
	// 同步时间取最新一条（前端展示「上次同步」用）
	var lastSync int64
	for _, v := range views {
		if v.SyncedAt > lastSync {
			lastSync = v.SyncedAt
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": views, "last_sync": lastSync, "total": len(views)})
}

// listModelsPlaza 模型广场视图：支持 capability 过滤与仅可用过滤。
//   GET /api/admin/models/plaza?capability=chat&active_only=true
//
// 默认 active_only=false：让 gone（已从上游消失）模型仍展示在广场（前端灰暗），
// 而公开 /v1/models 仍只列 active。需求：消失模型不进客户端拉取列表，但保留在广场。
func (h *Handler) listModelsPlaza(c *gin.Context) {
	capability := strings.TrimSpace(c.Query("capability"))
	// 默认含全部状态（active+gone+disabled）；仅显式 active_only=true 才排除失效。
	activeOnly := c.Query("active_only") == "true"

	var (
		ms  []data.Model
		err error
	)
	if activeOnly {
		ms, err = h.store.ListActiveModelsByCapability(c.Request.Context(), capability)
	} else {
		ms, err = h.store.ListAllModels(c.Request.Context())
		if err == nil && capability != "" {
			filtered := ms[:0]
			for _, m := range ms {
				if m.Capability == capability {
					filtered = append(filtered, m)
				}
			}
			ms = filtered
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	views := h.toModelViews(c.Request.Context(), ms)
	var lastSync int64
	for _, v := range views {
		if v.SyncedAt > lastSync {
			lastSync = v.SyncedAt
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": views, "last_sync": lastSync, "total": len(views)})
}

func (h *Handler) syncModels(c *gin.Context) {
	if err := h.syncer.SyncOnce(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testModel 探活单个模型（仿 new-api 的"测试"按钮）。
// POST /api/admin/models/test  body: {"model":"meta/..."} 或 ?model=meta/...
func (h *Handler) testModel(c *gin.Context) {
	if h.prober == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "探活器未启用"})
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	_ = c.ShouldBindJSON(&req)
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(c.Query("model"))
	}
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 model 参数"})
		return
	}
	res, err := h.prober.Probe(c.Request.Context(), model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// probeAllModels 探活所有 chat 模型。POST /api/admin/models/probe-all
func (h *Handler) probeAllModels(c *gin.Context) {
	if h.prober == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "探活器未启用"})
		return
	}
	if err := h.prober.ProbeAll(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// getModelDetail 单模型详情聚合（二级页）。
//   GET /api/admin/models/detail?id=meta/llama-3.1-8b-instruct
// 返回模型静态信息 + 近 1h 用量/可用度 + 最近探活，供详情页概览 tab。
func (h *Handler) getModelDetail(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	m, err := h.store.GetModel(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if m == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	views := h.toModelViews(c.Request.Context(), []data.Model{*m})
	if len(views) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	c.JSON(http.StatusOK, views[0])
}

// getModelTimeSeries 单模型时序（三级页 health tab）。
//   GET /api/admin/models/timeseries?id=...&window=1h&bucket=60
func (h *Handler) getModelTimeSeries(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	window := parseWindow(c.Query("window"))
	bucket, _ := strconv.Atoi(c.DefaultQuery("bucket", "60"))
	if bucket <= 0 {
		bucket = 60
	}
	ts, err := h.store.GetTimeSeries(c.Request.Context(), window, bucket, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ts})
}

// getModelProbes 单模型探活历史（三级页 probes tab）。
//   GET /api/admin/models/probes?id=...&limit=100
func (h *Handler) getModelProbes(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	history, err := h.store.ListModelProbeHistory(c.Request.Context(), id, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	latest, _ := h.store.ListModelProbes(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"history": history, "latest": latest[id]})
}

// --- 模型级熔断（v0.9）---

// listModelCircuit 列出全部模型熔断状态。
//   GET /api/admin/models/circuit?state=open   仅返回指定状态（可选）
func (h *Handler) listModelCircuit(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	var list []data.ModelCircuit
	var err error
	if state != "" {
		list, err = h.store.ListModelCircuitByState(c.Request.Context(), state)
	} else {
		list, err = h.store.ListModelCircuitAll(c.Request.Context())
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// healthSweep 触发一次全量模型健康扫描。
//   POST /api/admin/models/health-sweep
func (h *Handler) healthSweep(c *gin.Context) {
	if h.sweeper == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "健康扫描器未启用"})
		return
	}
	if err := h.sweeper.SweepAll(c.Request.Context()); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// resetModelCircuit 手动复位模型熔断（解除永久/临时熔断）。
//   POST /api/admin/models/circuit/reset   body: {"model":"meta/llama-..."}
func (h *Handler) resetModelCircuit(c *gin.Context) {
	var req struct {
		Model string `json:"model"`
	}
	_ = c.ShouldBindJSON(&req)
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(c.Query("model"))
	}
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 model 参数"})
		return
	}
	if err := h.store.ResetModelCircuit(c.Request.Context(), model); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "model": model})
}

// --- 日志 ---

func (h *Handler) getLogs(c *gin.Context) {
	traceID := c.Query("trace_id")
	level := c.Query("level")
	source := c.Query("source")
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "200"), 10, 64)
	logs, err := h.store.QueryLogs(c.Request.Context(), traceID, level, source, 0, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// parseWindow 解析窗口参数。
func parseWindow(s string) time.Duration {
	switch s {
	case "7d":
		return 7 * 24 * time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}
