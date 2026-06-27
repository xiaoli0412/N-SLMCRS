package autopilot

import (
	"context"
	"math"
)

// ForecastEngine 基于 Holt-Winters 三次指数平滑的轻量预测引擎。
//
// 输入 24h 每分钟一桶的请求数序列，拟合 level/trend/seasonality，
// 预测未来 RPM。当预测值逼近池化容量阈值（len(keys)*40）时，
// 提前降并发 + 预冷密钥，前瞻性预防限流。无 GPU、确定性。
type ForecastEngine struct {
	// 平滑参数（固定合理默认）
	alpha float64 // level
	beta  float64 // trend
	gamma float64 // seasonality

	seasonLen int // 季节周期（桶数）：1440 = 24h 每分钟一桶

	// kernel Rust sidecar 客户端（可选）。优先委托数值密集的 fit/forecast，
	// 不可达时降级回本地 fit（无单点依赖）。
	kernel *kernelClient
}

// NewForecastEngine 创建预测引擎。kernel 可为 nil（纯 Go）。
func NewForecastEngine(kernel *kernelClient) *ForecastEngine {
	return &ForecastEngine{
		alpha:     0.3,
		beta:      0.1,
		gamma:     0.2,
		seasonLen: 60, // 实际可用数据通常远少于 24h，用 60（1h）作周期更稳
		kernel:    kernel,
	}
}

// ID 引擎标识。
func (e *ForecastEngine) ID() EngineID { return EngineForecast }

// Decide 依据快照决策。
func (e *ForecastEngine) Decide(ctx context.Context, snap Snapshot) ([]Action, error) {
	series := snap.Series
	// 数据过少不误动（避免噪声）
	const minData = 20
	if len(series) < minData {
		return nil, nil
	}

	// 季节长度自适应：理想 60（1h 周期），数据不足 2 周期时缩短到 len/4（下限 2）。
	// 这样新部署/低流量场景（20+ 桶即可）也能触发预测，而非强求 2h 时序。
	L := 60
	if len(series) < L*2 {
		L = len(series) / 4
		if L < 2 {
			L = 2
		}
	}
	e.seasonLen = L

	// 取 Count 序列
	counts := make([]float64, 0, len(series))
	for _, p := range series {
		counts = append(counts, float64(p.Count))
	}

	// v0.7：优先委托 Rust sidecar 做 fit/forecast（数值密集），不可达则降级本地 fit。
	var forecastNext float64
	if kn, ok := e.kernel.forecast(ctx, counts); ok {
		forecastNext = kn
	} else {
		level, trend, season := e.fit(counts)
		// 预测未来 1 分钟（下一桶）的请求数；折算为 RPM（桶=1分钟时相等）。
		// fit 为加法模型（deseasonalized = data - season），故预测用加法：level+trend+season[0]。
		// 旧实现用乘法 (level+trend)*season[0]，在季节分量≈0（如平稳流量）时预测≈0，
		// 导致引擎几乎永不触发——已修正为加法。
		forecastNext = level + trend + season[0]
	}
	if forecastNext < 0 {
		forecastNext = 0
	}

	// v0.7：容量 = 可用活跃 key 数 × 40 RPM（NVIDIA 免费层）。
	// （每 key RPMOverride 求和需 ratelimit 暴露 per-key RPM，此处仍用 40 兜底；
	//  旧的 enabled 计数会把 cooling/circuit_open 的 key 也算进容量，高估，已改为只算 active。
	//  无快照可用 key 数时退回 enabled 计数，兼容旧测试/无在途数据场景。）
	availKeys := snap.AvailableKeyCount
	if availKeys <= 0 {
		for _, k := range snap.Keys {
			if k.Enabled {
				availKeys++
			}
		}
	}
	capacity := float64(availKeys) * 40.0
	if capacity <= 0 {
		return nil, nil
	}

	actions := make([]Action, 0, 2)
	utilization := forecastNext / capacity

	// v0.7：高档/峰值档下，主动提并发以匹配客户端并发量级别（避免排队）。
	// 仅在利用率健康（<80%）且当前并发低于档位基线时提并发。
	if utilization < 0.8 && snap.ClientConcurrencyTier >= TierHigh {
		desired := tierConcurrencyByKeys(snap.ClientConcurrencyTier, availKeys, snap.MaxConcurrency)
		if desired > snap.CurrentConcurrency {
			actions = append(actions, Action{
				Kind:       ActSetConcurrency,
				Value:      float64(desired),
				Reason:     reasonTierScaleUp(snap.ClientConcurrencyTier, availKeys, desired),
				Confidence: 0.8,
				Source:     EngineForecast,
			})
		}
	}

	// 逼近 80% 容量：预降并发
	if utilization >= 0.8 {
		// 降并发到当前 × (容量/预测)，下限 1
		scale := capacity / forecastNext
		if scale > 1 {
			scale = 1
		}
		target := int(math.Round(float64(snap.CurrentConcurrency) * scale))
		if target < 1 {
			target = 1
		}
		// 仅在确实更低时建议
		if target < snap.CurrentConcurrency {
			actions = append(actions, Action{
				Kind:       ActSetConcurrency,
				Value:      float64(target),
				Reason:     reasonForecastThrottle(forecastNext, capacity, target),
				Confidence: clamp01(utilization),
				Source:     EngineForecast,
			})
		}
	}

	// 逼近 95% 容量：预冷健康分最低的密钥（短冷却）
	if utilization >= 0.95 && availKeys > 1 {
		// 选成功率最低的启用密钥
		var worst *KeySnap
		for i := range snap.Keys {
			k := &snap.Keys[i]
			if !k.Enabled {
				continue
			}
			if worst == nil || k.SuccessRate < worst.SuccessRate {
				worst = k
			}
		}
		if worst != nil {
			actions = append(actions, Action{
				Kind:       ActOpenCircuit,
				KeyID:      worst.ID,
				Value:      30, // 预冷 30s
				Reason:     reasonForecastCooldown(forecastNext, capacity, 30),
				Confidence: clamp01(utilization),
				Source:     EngineForecast,
			})
		}
	}

	return actions, nil
}

// fit 执行 Holt-Winters 加法模型的初始化与一次完整拟合，
// 返回最终的 level、trend 与季节指数数组。
func (e *ForecastEngine) fit(data []float64) (level, trend float64, season []float64) {
	L := e.seasonLen
	if L < 2 || len(data) < L*2 {
		return 0, 0, []float64{1}
	}

	// 初始化：季节指数 = 各季节位置的平均值 - 全局平均
	season = make([]float64, L)
	globalAvg := mean(data)
	for i := 0; i < L; i++ {
		var sum float64
		var n int
		for j := i; j < len(data); j += L {
			sum += data[j]
			n++
		}
		if n > 0 {
			season[i] = sum/float64(n) - globalAvg
		}
	}

	// 初始 level/trend 用前两个周期
	level = mean(data[:L])
	trend = mean(data[L:2*L]) - level

	// 迭代更新
	a, b, g := e.alpha, e.beta, e.gamma
	for i := 0; i < len(data); i++ {
		si := i % L
		deseasonalized := data[i] - season[si]
		newLevel := a*deseasonalized + (1-a)*(level+trend)
		newTrend := b*(newLevel-level) + (1-b)*trend
		season[si] = g*(data[i]-newLevel) + (1-g)*season[si]
		level, trend = newLevel, newTrend
	}

	return level, trend, season
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}
