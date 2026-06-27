// Package hooks 提供集成钩子（v0.10）：Webhook 事件回调 + new-api/sapi 渠道对接。
//
// - Webhook：请求成功/失败/限流/熔断时向配置的 URL 异步 POST JSON 通知，带 HMAC-SHA256 签名。
// - 渠道：new-api/sapi 通过 OpenAI 兼容协议接入本网关；本包提供渠道配置生成与用量回采。
package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"golang.org/x/crypto/bcrypt"
)

// --- Webhook ---

// WebhookConfig Webhook 运行时配置（存 settings 表，可热改）。
type WebhookConfig struct {
	URL    string
	Secret string
	Events string // 逗号分隔：success,error,rate_limited,circuit；空=全部
}

// Event 钩子事件。
type Event struct {
	Type     string `json:"type"`      // success|error|rate_limited|circuit
	TraceID  string `json:"trace_id"`
	Model    string `json:"model"`
	Key      string `json:"key_mask"`
	Status   int    `json:"status"`
	Latency  int    `json:"latency_ms"`
	Reason   string `json:"reason,omitempty"`
	TS       int64  `json:"ts"`
}

// Webhook 事件回调服务。
type Webhook struct {
	cfg  WebhookConfig
	hc   *http.Client
}

// NewWebhook 创建 Webhook 服务。
func NewWebhook(cfg WebhookConfig) *Webhook {
	return &Webhook{cfg: cfg, hc: &http.Client{Timeout: 5 * time.Second}}
}

// UpdateConfig 热改配置。
func (w *Webhook) UpdateConfig(cfg WebhookConfig) { w.cfg = cfg }

// Config 返回当前配置。
func (w *Webhook) Config() WebhookConfig { return w.cfg }

// Emit 异步发送事件（不阻塞请求主流程；URL 为空则跳过）。
func (w *Webhook) Emit(ctx context.Context, e Event) {
	if w.cfg.URL == "" {
		return
	}
	if !w.eventEnabled(e.Type) {
		return
	}
	e.TS = time.Now().Unix()
	go w.post(e)
}

// EmitFields 适配 scheduler.WebhookEmitter 接口的字段式调用。
func (w *Webhook) EmitFields(ctx context.Context, typ, traceID, model, keyMask, reason string, status, latency int) {
	w.Emit(ctx, Event{
		Type:    typ,
		TraceID: traceID,
		Model:   model,
		Key:     keyMask,
		Status:  status,
		Latency: latency,
		Reason:  reason,
	})
}

// Test 发送一条测试事件，返回错误（供管理 API /hooks/webhook/test）。
func (w *Webhook) Test(ctx context.Context) error {
	if w.cfg.URL == "" {
		return fmt.Errorf("webhook URL 未配置")
	}
	return w.post(Event{Type: "test", TS: time.Now().Unix(), Reason: "manual test"})
}

func (w *Webhook) eventEnabled(t string) bool {
	if w.cfg.Events == "" {
		return true
	}
	for _, ev := range splitCSV(w.cfg.Events) {
		if ev == t {
			return true
		}
	}
	return false
}

func (w *Webhook) post(e Event) error {
	body, _ := json.Marshal(e)
	req, err := http.NewRequest(http.MethodPost, w.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "n-slmcrs-webhook/1.0")
	if w.cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(w.cfg.Secret))
		mac.Write(body)
		req.Header.Set("X-N-SLMCRS-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := w.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回 %d", resp.StatusCode)
	}
	return nil
}

// --- 渠道配置生成 ---

// ChannelConfig 供管理员粘贴到 new-api/sapi 的渠道配置。
type ChannelConfig struct {
	Type     string   `json:"type"`      // openai（new-api/sapi 均用 OpenAI 兼容渠道）
	Name     string   `json:"name"`
	BaseURL  string   `json:"base_url"`  // 渠道接入地址（如 http://host:8787/v1）
	APIKey   string   `json:"api_key"`   // 渠道密钥（仅此一次明文返回）
	Models   []string `json:"models"`    // 可用模型列表（来自 /v1/models）
	UsageURL string   `json:"usage_url"` // 计费回采端点
}

// GenerateChannelConfig 生成渠道配置（含明文密钥，仅创建/重置时返回）。
func GenerateChannelConfig(ch *data.Channel, apiKey, baseURL string, models []string) ChannelConfig {
	return ChannelConfig{
		Type:     "openai",
		Name:     ch.Name,
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Models:   models,
		UsageURL: baseURL + "/api/admin/hooks/channels/" + fmt.Sprintf("%d", ch.ID) + "/usage",
	}
}

// HashAPIKey 渠道密钥 bcrypt 哈希（data 包委托调用，避免循环依赖）。
func HashAPIKey(key string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	return string(h), err
}

// VerifyAPIKey 校验渠道密钥。
func VerifyAPIKey(hash, key string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(key)) == nil
}

// splitCSV 简单逗号分隔。
func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else if r != ' ' {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
