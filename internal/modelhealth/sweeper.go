// Package modelhealth 提供模型级健康扫描与熔断判定（v0.9）。
//
// 与按 Key 熔断（scheduler/ratelimit）互补：按模型聚合成功率，对每个模型
// 遍历其能力对应的所有 NVIDIA 推理接口各探 N 次，按成功率判定
// closed / open / permanent。失败模型从公开 /v1/models 隐藏，并在请求时
// 返回双语熔断说明。
//
// 判定规则（均可由管理面板热改）：
//   - 成功率 ≥ SuccessRateThreshold → closed，清零坏扫描计数
//   - SuccessRateFloor ≤ 成功率 < Threshold → open（指数退避冷却，封顶 10min）
//   - 成功率 < Floor 且连续 BadSweepToPermanent 次 → permanent（永久熔断）
//
// 参照：Hystrix/oxcircuitbreaker 三态 + 连续失败窗口；new-api 逐模型探活；
// LiteLLM deployment fallback 健康聚合。
package modelhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/kernelctl"
	"github.com/nslmcrs/gateway/internal/modelcatalog"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// Config 健康扫描与熔断配置（运行时可热改）。
type Config struct {
	ProbeCount           int
	ProbeInterval        time.Duration
	SweepInterval        time.Duration
	SuccessRateFloor     int
	SuccessRateThreshold int
	BadSweepToPermanent  int
	CooldownBase         time.Duration
}

// Sweeper 模型健康扫描器。
type Sweeper struct {
	store    *data.Store
	client   *upstream.Client
	cfg      Config
	mu       sync.RWMutex
	running  bool
	kernel   *kernelctl.Client // v0.11：判定计算下沉 Rust sidecar（可选；不可达降级回 Go）
}

// New 创建扫描器。
func New(store *data.Store, client *upstream.Client, cfg Config) *Sweeper {
	applyDefaults(&cfg)
	return &Sweeper{store: store, client: client, cfg: cfg}
}

// SetKernel 注入 Rust sidecar 客户端（启用 /verdict 判定下沉；nil=纯 Go）。
func (s *Sweeper) SetKernel(k *kernelctl.Client) *Sweeper {
	s.kernel = k
	return s
}

// Config 返回当前配置快照（线程安全）。
func (s *Sweeper) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// UpdateConfig 运行时热改配置（零值字段保持不变）。
func (s *Sweeper) UpdateConfig(patch Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if patch.ProbeCount > 0 {
		s.cfg.ProbeCount = patch.ProbeCount
	}
	if patch.ProbeInterval > 0 {
		s.cfg.ProbeInterval = patch.ProbeInterval
	}
	if patch.SweepInterval > 0 {
		s.cfg.SweepInterval = patch.SweepInterval
	}
	if patch.SuccessRateFloor > 0 {
		s.cfg.SuccessRateFloor = patch.SuccessRateFloor
	}
	if patch.SuccessRateThreshold > 0 {
		s.cfg.SuccessRateThreshold = patch.SuccessRateThreshold
	}
	if patch.BadSweepToPermanent > 0 {
		s.cfg.BadSweepToPermanent = patch.BadSweepToPermanent
	}
	if patch.CooldownBase > 0 {
		s.cfg.CooldownBase = patch.CooldownBase
	}
}

// Start 启动周期扫描（阻塞，应在 goroutine 中调用）。
// 启动时若距上次扫描超过一个周期则立即扫一次。
func (s *Sweeper) Start(ctx context.Context) {
	time.Sleep(20 * time.Second) // 错开同步/探活启动峰值
	if s.staleEnough() {
		if err := s.SweepAll(ctx); err != nil {
			log.Printf("[modelhealth] 启动扫描失败: %v", err)
		}
	}
	ticker := time.NewTicker(s.Config().SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[modelhealth] 扫描器停止")
			return
		case <-ticker.C:
			if err := s.SweepAll(ctx); err != nil {
				log.Printf("[modelhealth] 周期扫描失败: %v", err)
			}
		}
	}
}

// SweepAll 对所有 active 模型执行一轮健康扫描。返回首个错误（不中断其余）。
func (s *Sweeper) SweepAll(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("扫描正在进行中")
	}
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()

	models, err := s.store.ListActiveModels(ctx)
	if err != nil {
		return fmt.Errorf("列出模型: %w", err)
	}
	if len(models) == 0 {
		return nil
	}
	key, err := s.firstKey(ctx)
	if err != nil {
		return err
	}
	cfg := s.Config()
	scanned := 0
	for _, m := range models {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		iface := interfacesFor(m.Capability)
		if len(iface) == 0 {
			continue // 无对应推理接口（如 safety/reward/translation/parsing），跳过
		}
		rate := s.probeModel(ctx, m.ID, m.Capability, iface, key, cfg)
		if err := s.applyVerdict(ctx, m.ID, rate, cfg); err != nil {
			log.Printf("[modelhealth] 判定模型 %s 失败: %v", m.ID, err)
		}
		scanned++
	}
	log.Printf("[modelhealth] 扫描完成: %d 个模型", scanned)
	return nil
}

// probeModel 对单模型的所有接口各探 ProbeCount 次，返回总成功率（0..100）。
func (s *Sweeper) probeModel(ctx context.Context, modelID, capability string, ifaces []probeIface, key string, cfg Config) int {
	total, ok := 0, 0
	for _, it := range ifaces {
		for i := 0; i < cfg.ProbeCount; i++ {
			if ctx.Err() != nil {
				return ratePct(ok, total)
			}
			pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			success := s.probeOnce(pctx, it.cap, modelID, key, it.body(modelID))
			cancel()
			total++
			if success {
				ok++
			}
			if cfg.ProbeInterval > 0 {
				select {
				case <-time.After(cfg.ProbeInterval):
				case <-ctx.Done():
					return ratePct(ok, total)
				}
			}
		}
	}
	return ratePct(ok, total)
}

// probeOnce 发一次最小探测，返回是否成功（2xx）。
func (s *Sweeper) probeOnce(ctx context.Context, cap upstream.Capability, modelID, key string, body []byte) bool {
	path := capPath(cap, modelID)
	resp, err := s.client.Request(ctx, cap, key, path, body)
	if err != nil {
		return false
	}
	return resp.IsSuccess()
}

// applyVerdict 根据成功率更新模型熔断状态。
func (s *Sweeper) applyVerdict(ctx context.Context, modelID string, rate int, cfg Config) error {
	mc, err := s.store.GetModelCircuit(ctx, modelID)
	if err != nil {
		return err
	}
	if mc == nil {
		mc = &data.ModelCircuit{Model: modelID, State: data.CircuitClosed, SuccessRatePct: 100}
	}
	mc.SuccessRatePct = rate
	mc.LastSweepAt = time.Now().Unix()

	// v0.11：判定计算下沉 Rust sidecar；不可达或异常时降级回下方 Go 实现。
	// 数值与 kernel-rs /verdict 对齐，确保降级透明。
	if s.kernel != nil {
		if v, ok := s.kernel.Verdict(ctx, rate, mc.State, mc.BadSweepCount,
			cfg.SuccessRateFloor, cfg.SuccessRateThreshold, cfg.BadSweepToPermanent,
			int64(cfg.CooldownBase.Seconds())); ok {
			wasPerm := mc.Permanent
			mc.State = v.State
			mc.OpenUntil = v.OpenUntil
			mc.BadSweepCount = v.BadSweepCount
			mc.Permanent = v.Permanent
			if v.Permanent && !wasPerm {
				log.Printf("[modelhealth] 模型 %s 永久熔断（连续 %d 次低于 %d%%）", modelID, v.BadSweepCount, cfg.SuccessRateFloor)
			}
			return s.store.UpsertModelCircuit(ctx, *mc)
		}
	}

	switch {
	case rate >= cfg.SuccessRateThreshold:
		// 健康：闭合，清零坏扫描
		if mc.State != data.CircuitPermanent {
			mc.State = data.CircuitClosed
			mc.OpenUntil = 0
		}
		mc.BadSweepCount = 0
	case rate < cfg.SuccessRateFloor:
		// 远低于地板：累加坏扫描，达阈值永久熔断
		mc.BadSweepCount++
		if mc.BadSweepCount >= cfg.BadSweepToPermanent {
			mc.State = data.CircuitPermanent
			mc.Permanent = true
			log.Printf("[modelhealth] 模型 %s 永久熔断（连续 %d 次低于 %d%%）", modelID, mc.BadSweepCount, cfg.SuccessRateFloor)
		} else if mc.State != data.CircuitPermanent {
			mc.State = data.CircuitOpen
			mc.OpenUntil = time.Now().Unix() + int64(cfg.CooldownBase.Seconds())
		}
	default:
		// 地板≤rate<阈值：临时熔断
		if mc.State != data.CircuitPermanent {
			mc.State = data.CircuitOpen
			mc.OpenUntil = nextCooldown(mc, cfg)
		}
	}
	return s.store.UpsertModelCircuit(ctx, *mc)
}

// nextCooldown 指数退避冷却（每次坏扫描翻倍，封顶 10min）。
func nextCooldown(mc *data.ModelCircuit, cfg Config) int64 {
	cooldown := cfg.CooldownBase
	for i := 1; i < mc.BadSweepCount; i++ {
		cooldown *= 2
	}
	if cooldown > 10*time.Minute {
		cooldown = 10 * time.Minute
	}
	return time.Now().Unix() + int64(cooldown.Seconds())
}

// firstKey 取第一个可用上游 Key。
func (s *Sweeper) firstKey(ctx context.Context) (string, error) {
	keys, err := s.store.ListUpstreamKeys(ctx)
	if err != nil {
		return "", fmt.Errorf("获取上游密钥: %w", err)
	}
	for _, k := range keys {
		if k.Enabled && k.Status != "circuit_open" && k.Status != "disabled" {
			return k.KeyValue, nil
		}
	}
	return "", fmt.Errorf("无可用上游密钥用于健康扫描")
}

// staleEnough 距上次扫描是否已超过一个周期（决定启动时是否立即扫）。
func (s *Sweeper) staleEnough() bool {
	// 简化：任一模型 last_sweep_at 老于周期，或全无记录 → 立即扫
	list, err := s.store.ListModelCircuitAll(context.Background())
	if err != nil || len(list) == 0 {
		return true
	}
	cutoff := time.Now().Add(-s.Config().SweepInterval).Unix()
	for _, mc := range list {
		if mc.LastSweepAt < cutoff {
			return true
		}
	}
	return false
}

// --- 接口映射 ---

// probeIface 单个探测接口：能力 + 最小请求体构造器。
type probeIface struct {
	cap  upstream.Capability
	body func(model string) []byte
}

// interfacesFor 按模型能力返回需探测的 NVIDIA 接口集合。
// 不存在的接口（如 NVIDIA 未提供 images/audio）不返回，即不转换。
func interfacesFor(capability string) []probeIface {
	switch capability {
	case modelcatalog.CapEmbedding:
		return []probeIface{{
			cap: upstream.CapEmbedding,
			body: func(m string) []byte {
				b, _ := json.Marshal(map[string]any{
					"model": m, "input": "ping", "input_type": "query",
				})
				return b
			},
		}}
	case modelcatalog.CapRerank:
		return []probeIface{{
			cap: upstream.CapRerank,
			body: func(m string) []byte {
				b, _ := json.Marshal(map[string]any{
					"model": m, "query": "ping",
					"passages": []string{"pong"},
				})
				return b
			},
		}}
	case modelcatalog.CapChat, modelcatalog.CapReasoning, modelcatalog.CapCode, modelcatalog.CapVision:
		return []probeIface{{
			cap: upstream.CapChat,
			body: func(m string) []byte {
				b, _ := json.Marshal(map[string]any{
					"model": m, "max_tokens": 1, "stream": false,
					"messages": []map[string]string{{"role": "user", "content": "ping"}},
				})
				return b
			},
		}}
	default:
		// safety/reward/translation/parsing 无稳定推理探测路径，跳过
		return nil
	}
}

// capPath 能力 → 上游路径（与 scheduler.capPath 对齐，避免循环依赖而内联）。
func capPath(cap upstream.Capability, model string) string {
	switch cap {
	case upstream.CapEmbedding:
		return "/embeddings"
	case upstream.CapRerank:
		m := model
		for i := 0; i < len(m); i++ {
			if m[i] == '.' {
				m = m[:i] + "_" + m[i+1:]
				i++
			}
		}
		return "/retrieval/" + m + "/reranking"
	default:
		return "/chat/completions"
	}
}

// ratePct 计算成功率百分比。
func ratePct(ok, total int) int {
	if total <= 0 {
		return 0
	}
	return 100 * ok / total
}

// applyDefaults 为零值字段填充默认值。
func applyDefaults(cfg *Config) {
	if cfg.ProbeCount <= 0 {
		cfg.ProbeCount = 3
	}
	if cfg.ProbeInterval <= 0 {
		cfg.ProbeInterval = 2 * time.Second
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = 30 * time.Minute
	}
	if cfg.SuccessRateFloor <= 0 {
		cfg.SuccessRateFloor = 30
	}
	if cfg.SuccessRateThreshold <= 0 {
		cfg.SuccessRateThreshold = 80
	}
	if cfg.BadSweepToPermanent <= 0 {
		cfg.BadSweepToPermanent = 3
	}
	if cfg.CooldownBase <= 0 {
		cfg.CooldownBase = 30 * time.Second
	}
}
