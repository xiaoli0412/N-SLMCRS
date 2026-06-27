package autopilot

// Tier 客户端并发量级别（v0.7 新增）。
//
// 系统在入口层用全局在途请求 gauge 实时探测并发量，按阈值归入四档，
// Auto-Pilot 据档位 + 可用 key 数实时调整并发度/权重。零配置、全自动。
type Tier int

const (
	TierUnknown Tier = iota // 0，无数据
	TierLow                 // 1，≈5 并发：轻载，保守并发
	TierMid                 // 2，≈10 并发：常规
	TierHigh                // 3，≈50 并发：重载，激进并发
	TierPeak                // 4，≈100 并发：峰值，全量压榨
)

// String 人类可读档位名（前端展示与审计用）。
func (t Tier) String() string {
	switch t {
	case TierLow:
		return "low(5)"
	case TierMid:
		return "mid(10)"
	case TierHigh:
		return "high(50)"
	case TierPeak:
		return "peak(100)"
	default:
		return "unknown"
	}
}

// tierThresholds 在途量归档阈值（含下界）。
// ≤8 → Low，≤25 → Mid，≤75 → High，其余 → Peak。
// 选在档位中点附近以平滑过渡，避免边界抖动。
var tierThresholds = [...]int64{8, 25, 75}

// ClassifyTier 按全局在途请求数归入并发档位。
func ClassifyTier(inflight int64) Tier {
	switch {
	case inflight <= 0:
		return TierUnknown
	case inflight <= tierThresholds[0]:
		return TierLow
	case inflight <= tierThresholds[1]:
		return TierMid
	case inflight <= tierThresholds[2]:
		return TierHigh
	default:
		return TierPeak
	}
}

// tierConcurrencyByKeys 按档位与可用活跃 key 数推荐并发度基线。
//   Low/Mid：min(可用key, 档位名) —— 保守，每路 1 并发足够
//   High/Peak：min(可用key×每key配额, MaxConcurrency) —— 激进压榨
// 基线再由引擎的 PID/预测微调。
func tierConcurrencyByKeys(tier Tier, availKeys, maxConcurrency int) int {
	if availKeys <= 0 {
		return 1
	}
	switch tier {
	case TierLow:
		return clampInt(minInt(availKeys, 5), 1, maxConcurrency)
	case TierMid:
		return clampInt(minInt(availKeys, 10), 1, maxConcurrency)
	case TierHigh:
		// 每路 key 压 2 并发，但不超过档位 50 与 MaxConcurrency
		return clampInt(minInt(availKeys*2, 50), 1, maxConcurrency)
	case TierPeak:
		// 每路 key 压 4 并发，但不超过档位 100 与 MaxConcurrency
		return clampInt(minInt(availKeys*4, 100), 1, maxConcurrency)
	default:
		return clampInt(availKeys, 1, maxConcurrency)
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
