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
//   GET    /api/admin/models/plaza        模型广场视图（capability 过滤 + 用量统计）
//   POST   /api/admin/models/sync         立即同步模型
//   GET    /api/admin/settings            读取熔断/调度运行时配置
//   PUT    /api/admin/settings            更新熔断/调度运行时配置（落库 + 热生效）
//   GET    /api/admin/logs               查询日志
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
	"github.com/nslmcrs/gateway/internal/config"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/modelmeta"
	"github.com/nslmcrs/gateway/internal/scheduler"
	"golang.org/x/crypto/bcrypt"
)

// settingKeyTokenHash 存储管理令牌的 bcrypt 哈希。
// 不存在时表示仍处于初始默认令牌状态（首次登录后强制改密）。
const settingKeyTokenHash = "admin:token_hash"

// Handler 管理 API 处理器。
type Handler struct {
	store  *data.Store
	syncer *modelmeta.Syncer
	cfg    *config.Config
	ap     *autopilot.Controller // Auto-Pilot 总控（可选；main.go 装配后注入）
	sched  *scheduler.Scheduler  // 调度器（可选；用于运行时改熔断/并发配置）
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
		g.POST("/models/sync", h.syncModels)

		g.GET("/settings", h.getSettings)
		g.PUT("/settings", h.putSettings)

		g.GET("/logs", h.getLogs)

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
	m, err := h.store.GetMetrics(c.Request.Context(), window)
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
	ts, err := h.store.GetTimeSeries(c.Request.Context(), window, bucket)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ts})
}

func (h *Handler) getKeyHealth(c *gin.Context) {
	window := parseWindow(c.Query("window"))
	hl, err := h.store.GetKeyHealth(c.Request.Context(), window)
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
	SyncedAt      int64   `json:"synced_at"`
	// 用量统计（近 1h）
	RequestCount int64   `json:"request_count"`
	SuccessRate  float64 `json:"success_rate"` // 0..100
}

// toModelViews 将 data.Model 列表拼装为带用量统计的广场视图。
func (h *Handler) toModelViews(ctx context.Context, ms []data.Model) []modelView {
	usage, _ := h.store.ModelUsageStats(ctx, time.Hour)
	views := make([]modelView, 0, len(ms))
	for _, m := range ms {
		u := usage[m.ID]
		views = append(views, modelView{
			ID: m.ID, Object: m.Object, Created: m.Created, OwnedBy: m.OwnedBy, Root: m.Root,
			Capability: m.Capability, ParamCount: m.ParamCount, ContextLength: m.ContextLength,
			Description: m.Description, IsActive: m.IsActive, SyncedAt: m.SyncedAt,
			RequestCount: u.RequestCount, SuccessRate: u.SuccessRate,
		})
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
func (h *Handler) listModelsPlaza(c *gin.Context) {
	capability := strings.TrimSpace(c.Query("capability"))
	activeOnly := c.DefaultQuery("active_only", "true") == "true"

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
