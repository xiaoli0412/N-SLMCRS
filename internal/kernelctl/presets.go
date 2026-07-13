package kernelctl

// Go 侧策略预设（v0.14）。与 kernel-rs/src/strategy.rs PRESETS 对齐——
// admin 查询与降级路径据此解析 id→参数；kernel 在线时 kernel 为 /reserve 权威。
// 修改任一侧须同步另一侧。

// Presets 返回全部策略预设（顺序即 UI 展示顺序）。
func Presets() []StrategyPreset {
	return []StrategyPreset{
		{
			ID: "guardian", Icon: "🛡️", NameZh: "保守护航", NameEn: "Guardian",
			CharacterZh: "不死、不烧、最省", CharacterEn: "Never die, never burn, most frugal",
			Selection: "strict_priority", Fanout: 1,
			BreakerThreshold: 20, BreakerCooldownSec: 60, RPMHeadroom: 0.80,
			MinKeys: 0, MaxKeys: 3,
			ScenarioZh:  "密钥稀缺（≤3）或单付费密钥。StrictPriority 永远先打最健康的、失败才回退；扇出 1 消除并发 loser 的 RPM 浪费；宽容熔断（阈值 20）避免自残掉 33–50% 容量；80% 头寸留突发余量防 429 烧键。",
			ScenarioEn:  "Scarce keys (≤3) or single paid key. StrictPriority always tries the healthiest first, falling back only on failure; fan-out 1 eliminates concurrent-loser RPM waste; lenient breaker (threshold 20) avoids self-harm; 80% headroom reserves burst buffer against 429s.",
		},
		{
			ID: "balanced", Icon: "⚖️", NameZh: "均衡调度", NameEn: "Balanced",
			CharacterZh: "全面均衡", CharacterEn: "Fully balanced",
			Selection: "weighted_random", Fanout: 0,
			BreakerThreshold: 5, BreakerCooldownSec: 30, RPMHeadroom: 1.0,
			MinKeys: 4, MaxKeys: 7,
			ScenarioZh:  "默认通用。密钥够多能吸收随机性；加权随机分散负载又偏向健康；标准熔断切掉真坏的键；满容量骑 RPM。",
			ScenarioEn:  "Default general-purpose. Enough keys to absorb randomness; weighted random spreads load while favoring healthy keys; standard breaker cuts genuinely broken keys; full-capacity RPM.",
		},
		{
			ID: "velocity", Icon: "⚡", NameZh: "极速竞速", NameEn: "Velocity",
			CharacterZh: "低延迟、高并发", CharacterEn: "Low latency, high concurrency",
			Selection: "least_inflight", Fanout: 0,
			BreakerThreshold: 5, BreakerCooldownSec: 30, RPMHeadroom: 1.0,
			MinKeys: 8, MaxKeys: 0,
			ScenarioZh:  "密钥≥8，聚合 RPM 远超需求，延迟主导。LeastInflight 把请求发给最闲的密钥降 P99；高扇出让先到先得快速返回；可承受 loser 的 RPM 浪费。",
			ScenarioEn:  "Keys≥8, aggregate RPM far exceeds demand, latency dominates. LeastInflight sends to the least-queued key to cut P99; high fan-out returns first-success fast; can afford loser RPM waste.",
		},
		{
			ID: "fairshare", Icon: "🎯", NameZh: "均轮分发", NameEn: "Fairshare",
			CharacterZh: "雨露均沾、最大化总吞吐", CharacterEn: "Even spread, max total throughput",
			Selection: "round_robin", Fanout: 1,
			BreakerThreshold: 5, BreakerCooldownSec: 30, RPMHeadroom: 1.0,
			MinKeys: 2, MaxKeys: 0,
			ScenarioZh:  "批处理/稳态、吞吐优先。RoundRobin 均匀轮转使没有密钥提前触限而其他空闲，最大化聚合 RPM 利用率；扇出 1 零浪费；满容量骑满。",
			ScenarioEn:  "Batch/steady, throughput-first. RoundRobin even rotation prevents any key hitting its limit early while others idle, maximizing aggregate RPM utilization; fan-out 1 zero waste; ride full capacity.",
		},
		{
			ID: "adaptive", Icon: "🤖", NameZh: "智能自适应", NameEn: "Adaptive",
			CharacterZh: "AI 自动调优", CharacterEn: "AI auto-tuning",
			Selection: "weighted_random", Fanout: 0,
			BreakerThreshold: 5, BreakerCooldownSec: 30, RPMHeadroom: 1.0,
			MinKeys: 0, MaxKeys: 0,
			ScenarioZh:  "Auto-Pilot 30s 控制环按实时工况调并发档位 + 逐键权重乘子（weight_boost）。流量多变、不想手调时最优——加权随机为底，AI 注入权重。",
			ScenarioEn:  "Auto-Pilot 30s control loop tunes concurrency tier + per-key weight_boost to live conditions. Best for variable traffic when you don't want to hand-tune—weighted random base with AI-injected weights.",
		},
	}
}

// PresetByID 按 id 查预设。
func PresetByID(id string) (StrategyPreset, bool) {
	for _, p := range Presets() {
		if p.ID == id {
			return p, true
		}
	}
	return StrategyPreset{}, false
}

// BalancedPreset 返回默认 balanced 预设。
func BalancedPreset() StrategyPreset {
	p, _ := PresetByID("balanced")
	return p
}

// Recommend 按密钥数推荐策略 id（与 Rust recommend 对齐）。
func Recommend(keyCount int) string {
	switch {
	case keyCount <= 3:
		return "guardian"
	case keyCount <= 7:
		return "balanced"
	default:
		return "velocity"
	}
}
