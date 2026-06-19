// Package admin 提供管理 API：上游密钥、下游凭证、指标查询。
//
// 所有管理端点在 /api/admin 下，需 ADMIN_TOKEN 鉴权。
// Phase 1 端点：
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
//   POST   /api/admin/models/sync        立即同步模型
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
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/autopilot"
	"github.com/nslmcrs/gateway/internal/config"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/modelmeta"
)

// Handler 管理 API 处理器。
type Handler struct {
	store   *data.Store
	syncer  *modelmeta.Syncer
	cfg     *config.Config
	ap      *autopilot.Controller // Auto-Pilot 总控（可选；main.go 装配后注入）
}

// New 创建管理 API 处理器。
func New(store *data.Store, syncer *modelmeta.Syncer, cfg *config.Config) *Handler {
	return &Handler{store: store, syncer: syncer, cfg: cfg}
}

// AuthMiddleware 管理 API 鉴权（ADMIN_TOKEN）。
func AuthMiddleware(adminToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 无配置 token 时跳过（开发模式，生产强烈建议配置）
		if adminToken == "" {
			c.Next()
			return
		}
		token := c.GetHeader("X-Admin-Token")
		if token == "" {
			auth := c.GetHeader("Authorization")
			if len(auth) > 7 && auth[:7] == "Bearer " {
				token = auth[7:]
			}
		}
		if token != adminToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的管理令牌"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RegisterRoutes 注册管理路由。
func (h *Handler) RegisterRoutes(r *gin.Engine, adminToken string) {
	g := r.Group("/api/admin")
	g.Use(AuthMiddleware(adminToken))
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
		g.POST("/models/sync", h.syncModels)

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

func (h *Handler) listModels(c *gin.Context) {
	ms, err := h.store.ListAllModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ms})
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
