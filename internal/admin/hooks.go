package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/hooks"
)

// webhookCfgView webhook 配置对外契约。
type webhookCfgView struct {
	URL    string `json:"url"`
	Secret string `json:"secret"` // 仅回显是否已设置（掩码），不回明文
	Events string `json:"events"`
}

// --- 渠道 ---

// listChannels 列出全部集成渠道。GET /api/admin/hooks/channels
func (h *Handler) listChannels(c *gin.Context) {
	list, err := h.store.ListChannels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// addChannel 新增渠道。POST /api/admin/hooks/channels
// body: {name, type(newapi|sapi), base_url?, api_key}
// 返回渠道配置（含明文密钥，仅此一次）。
func (h *Handler) addChannel(c *gin.Context) {
	var req struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Name == "" || req.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name 与 api_key 必填"})
		return
	}
	if req.Type == "" {
		req.Type = "newapi"
	}
	if req.BaseURL == "" {
		req.BaseURL = guessBaseURL(c)
	}
	ch, err := h.store.AddChannel(c.Request.Context(), req.Name, req.Type, req.BaseURL, req.APIKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 返回可粘贴的渠道配置（含明文密钥）
	models := h.channelModels(c.Request.Context())
	cfg := hooks.GenerateChannelConfig(ch, req.APIKey, req.BaseURL, models)
	c.JSON(http.StatusOK, gin.H{"channel": ch, "config": cfg})
}

// deleteChannel 删除渠道。DELETE /api/admin/hooks/channels/:id
func (h *Handler) deleteChannel(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteChannel(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// toggleChannel 启用/停用渠道。PATCH /api/admin/hooks/channels/:id  body:{enabled}
func (h *Handler) toggleChannel(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 enabled"})
		return
	}
	if err := h.store.ToggleChannel(c.Request.Context(), id, *req.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// channelConfig 重新生成渠道配置（不含明文密钥——明文仅创建时返回一次）。
// GET /api/admin/hooks/channels/:id/config
func (h *Handler) channelConfig(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	ch, err := h.store.GetChannel(c.Request.Context(), id)
	if err != nil || ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在"})
		return
	}
	models := h.channelModels(c.Request.Context())
	cfg := hooks.GenerateChannelConfig(ch, "(见创建时返回)", ch.BaseURL, models)
	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

// channelUsage 渠道用量回采（计费用）。GET /api/admin/hooks/channels/:id/usage
func (h *Handler) channelUsage(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	ch, err := h.store.GetChannel(c.Request.Context(), id)
	if err != nil || ch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "渠道不存在"})
		return
	}
	// 近 1h 该渠道相关请求的 token 用量（按 trace 聚合较复杂，此处返回渠道级累计 + 近窗指标）
	metrics, _ := h.store.GetMetrics(c.Request.Context(), time.Hour, "")
	c.JSON(http.StatusOK, gin.H{
		"channel_id":     ch.ID,
		"total_requests": ch.TotalRequests,
		"window":         "1h",
		"total_tokens":   metrics.TotalTokens,
		"success_rate":   metrics.SuccessRate,
	})
}

// --- Webhook ---

// getWebhookCfg 读取 webhook 配置。GET /api/admin/hooks/webhook
func (h *Handler) getWebhookCfg(c *gin.Context) {
	v := webhookCfgView{Events: ""}
	if h.webhook != nil {
		cfg := h.webhook.Config()
		v.URL = cfg.URL
		v.Events = cfg.Events
		if cfg.Secret != "" {
			v.Secret = "••••（已设置）"
		}
	}
	// 兜底读 env 默认
	if v.URL == "" {
		v.URL = h.cfg.Hooks.WebhookURL
		v.Events = h.cfg.Hooks.WebhookEvents
	}
	c.JSON(http.StatusOK, v)
}

// putWebhookCfg 更新 webhook 配置（热生效 + 落库）。PUT /api/admin/hooks/webhook
func (h *Handler) putWebhookCfg(c *gin.Context) {
	var req webhookCfgView
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 落库持久化
	ctx := c.Request.Context()
	if req.URL != "" {
		_ = h.store.SetSetting(ctx, "hooks.webhook_url", req.URL)
	}
	if req.Events != "" {
		_ = h.store.SetSetting(ctx, "hooks.webhook_events", req.Events)
	}
	// secret 为掩码占位时不覆盖；非空且非掩码才更新
	if req.Secret != "" && !strings.HasPrefix(req.Secret, "••••") {
		_ = h.store.SetSetting(ctx, "hooks.webhook_secret", req.Secret)
	}
	// 热生效
	if h.webhook != nil {
		cfg := hooks.WebhookConfig{
			URL:    req.URL,
			Events: req.Events,
			Secret: req.Secret,
		}
		if req.Secret == "" || strings.HasPrefix(req.Secret, "••••") {
			// 保留原 secret
			cfg.Secret = h.webhook.Config().Secret
		}
		if cfg.URL == "" {
			cfg.URL = h.webhook.Config().URL
		}
		h.webhook.UpdateConfig(cfg)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testWebhook 发送测试事件。POST /api/admin/hooks/webhook/test
func (h *Handler) testWebhook(c *gin.Context) {
	if h.webhook == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "webhook 服务未启用"})
		return
	}
	if err := h.webhook.Test(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- 辅助 ---

// channelModels 返回可用模型 id 列表（供渠道配置的 models 字段）。
func (h *Handler) channelModels(ctx context.Context) []string {
	ms, err := h.store.ListActiveModels(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// guessBaseURL 从请求推断渠道接入地址（如 http://host:8787/v1）。
func guessBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request.Host
	if host == "" {
		host = "localhost:8787"
	}
	return scheme + "://" + host + "/v1"
}
