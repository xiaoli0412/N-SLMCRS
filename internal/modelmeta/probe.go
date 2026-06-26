package modelmeta

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// Prober 模型主动探活器：用首个可用 Key 对模型发最小 ping（max_tokens:1），
// 测可用性与延迟。与 request_logs 被动统计互补——新模型/低流量模型也能获得可用度。
type Prober struct {
	store    *data.Store
	client   *upstream.Client
	interval time.Duration
	timeout  time.Duration
}

// NewProber 创建探活器。interval 为周期探活间隔（<=0 默认 10min）。
func NewProber(store *data.Store, client *upstream.Client, interval time.Duration) *Prober {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	return &Prober{store: store, client: client, interval: interval, timeout: 30 * time.Second}
}

// Start 启动周期探活（阻塞，应在 goroutine 中调用）。
func (p *Prober) Start(ctx context.Context) {
	// 启动时延迟一小段再首次探活，避免与同步/启动抢资源
	time.Sleep(30 * time.Second)
	if err := p.ProbeAll(ctx); err != nil {
		log.Printf("[probe] 启动探活失败: %v", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[probe] 探活器停止")
			return
		case <-ticker.C:
			if err := p.ProbeAll(ctx); err != nil {
				log.Printf("[probe] 周期探活失败: %v", err)
			}
		}
	}
}

// ProbeAll 对所有可用 chat 模型逐一探活。返回首个错误（不中断其余）。
func (p *Prober) ProbeAll(ctx context.Context) error {
	models, err := p.store.ListActiveModelsByCapability(ctx, "chat")
	if err != nil {
		return fmt.Errorf("列出 chat 模型: %w", err)
	}
	if len(models) == 0 {
		return nil
	}
	key, err := p.firstKey(ctx)
	if err != nil {
		return err
	}
	for _, m := range models {
		pctx, cancel := context.WithTimeout(ctx, p.timeout)
		res := p.probeWith(pctx, m.ID, key)
		cancel()
		_ = p.store.UpsertModelProbe(ctx, res)
	}
	log.Printf("[probe] 探活完成: %d 个 chat 模型", len(models))
	return nil
}

// Probe 探活单个模型（供 /api/admin/models/test 调用）。
func (p *Prober) Probe(ctx context.Context, modelID string) (data.ProbeResult, error) {
	key, err := p.firstKey(ctx)
	if err != nil {
		return data.ProbeResult{}, err
	}
	pctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	res := p.probeWith(pctx, modelID, key)
	_ = p.store.UpsertModelProbe(ctx, res)
	return res, nil
}

// probeWith 用指定 key 探活一个模型：发 max_tokens:1 的最小 chat 请求。
func (p *Prober) probeWith(ctx context.Context, modelID, key string) data.ProbeResult {
	body, _ := json.Marshal(map[string]any{
		"model":      modelID,
		"max_tokens": 1,
		"stream":     false,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})
	start := time.Now()
	res := data.ProbeResult{ModelID: modelID, TS: start.Unix(), Status: "error"}
	resp, err := p.client.Request(ctx, upstream.CapChat, key, "/chat/completions", body)
	res.LatencyMS = int(time.Since(start).Milliseconds())
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			res.Status = "timeout"
			res.Error = "探活超时"
		} else {
			res.Error = err.Error()
		}
		return res
	}
	res.HTTPStatus = resp.StatusCode
	if resp.IsSuccess() {
		res.OK = true
		res.Status = "ok"
	} else {
		nvErr := resp.ParseNVIDIAError()
		res.Error = nvErr.Title
		if res.Error == "" {
			res.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
	}
	return res
}

// firstKey 取第一个可用上游 Key（enabled 且非熔断/停用）。
func (p *Prober) firstKey(ctx context.Context) (string, error) {
	keys, err := p.store.ListUpstreamKeys(ctx)
	if err != nil {
		return "", fmt.Errorf("获取上游密钥: %w", err)
	}
	for _, k := range keys {
		if k.Enabled && k.Status != "circuit_open" && k.Status != "disabled" {
			return k.KeyValue, nil
		}
	}
	return "", fmt.Errorf("无可用上游密钥用于探活（先在 /keys 配置 nvapi- 密钥）")
}
