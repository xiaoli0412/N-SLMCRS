package autopilot

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/nslmcrs/gateway/internal/ratelimit"
)

// AdaptiveEngine 基于指数加权移动平均(EWMA)成功率与 PID 反馈控制器。
//
// 输出：目标并发度（set_concurrency）；并对成功率骤降/连续失败的 Key
// 降权（set_weight_boost）。无外部依赖、确定性、毫秒级响应。
type AdaptiveEngine struct {
	mu sync.Mutex

	// PID 状态（进程内，重启复位）
	ewmaRate    float64 // 平滑后的全局成功率（0..1）
	integral    float64 // 误差积分
	prevError   float64 // 上次误差（微分用）
	lastDecide  time.Time

	alpha float64 // EWMA 平滑系数
	kp, ki, kd float64 // PID 增益
	targetRate float64 // 目标成功率（0..1）
}

// NewAdaptiveEngine 创建自适应引擎（PID 增益为保守默认）。
func NewAdaptiveEngine() *AdaptiveEngine {
	return &AdaptiveEngine{
		alpha:      0.3,
		kp:         0.5,
		ki:         0.05,
		kd:         0.2,
		targetRate: 0.99,
	}
}

// ID 引擎标识。
func (e *AdaptiveEngine) ID() EngineID { return EngineAdaptive }

// Decide 依据快照决策。
func (e *AdaptiveEngine) Decide(_ context.Context, snap Snapshot) ([]Action, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	// 计算全局成功率（基于快照中的活跃 Key）
	globalRate := aggregateSuccessRate(snap.Keys)
	if globalRate <= 0 {
		// 无数据：维持默认，不误动
		return nil, nil
	}
	// EWMA 平滑
	if e.lastDecide.IsZero() {
		e.ewmaRate = globalRate
	} else {
		e.ewmaRate = e.alpha*globalRate + (1-e.alpha)*e.ewmaRate
	}
	e.lastDecide = now

	actions := make([]Action, 0, len(snap.Keys)+1)

	// PID：e = target - ewmaRate
	err := e.targetRate - e.ewmaRate
	e.integral += err
	// 抗积分饱和
	if e.integral > 10 {
		e.integral = 10
	} else if e.integral < -10 {
		e.integral = -10
	}
	deriv := err - e.prevError
	e.prevError = err
	u := e.kp*err + e.ki*e.integral + e.kd*deriv

	// u 为误差的 PID 合成：成功率低于目标时 err>0、u>0。
	// 映射为并发度：成功率低（u>0）→ 降并发（back-off）；成功率高（u<0）→ 提并发（scale-up）。
	// 故取 base - u*...：u 正向时 target 低于基线，u 负向时高于基线。
	// （旧实现 base + u*... 在 err 正向时反而升并发，与"低→降并发"语义相反，已修正。）
	base := float64(snap.DefaultConcurrency)
	if base <= 0 {
		base = 5
	}
	target := base - u*float64(snap.MaxConcurrency)*0.5
	// 钳位到 [1, MaxConcurrency]
	if target < 1 {
		target = 1
	}
	if snap.MaxConcurrency > 0 && target > float64(snap.MaxConcurrency) {
		target = float64(snap.MaxConcurrency)
	}
	targetInt := int(math.Round(target))
	if targetInt < 1 {
		targetInt = 1
	}
	if targetInt != snap.CurrentConcurrency {
		actions = append(actions, Action{
			Kind:       ActSetConcurrency,
			Value:      float64(targetInt),
			Reason:     reasonConcurrency(e.ewmaRate, targetInt),
			Confidence: clamp01(0.6 + 0.3*math.Abs(err)),
			Source:     EngineAdaptive,
		})
	}

	// 对单 Key 降权：成功率 < 50% 或连续失败 >= 3
	for _, k := range snap.Keys {
		if !k.Enabled || k.Status == "circuit_open" {
			continue
		}
		boost := 1.0
		if k.SuccessRate < 0.5 || k.ConsecFail >= 3 {
			boost = 0.1
			actions = append(actions, Action{
				Kind:       ActSetWeightBoost,
				KeyID:      k.ID,
				Value:      boost,
				Reason:     reasonWeightBoost(k),
				Confidence: clamp01(0.5 + 0.4*(1-k.SuccessRate)),
				Source:     EngineAdaptive,
			})
		}
	}

	// 对持续失败/限流的 Key 建议短熔断（保守，仅建议）
	for _, k := range snap.Keys {
		if k.Enabled && k.Status == "active" && k.ConsecFail >= 5 {
			actions = append(actions, Action{
				Kind:       ActOpenCircuit,
				KeyID:      k.ID,
				Value:      60, // 短冷却 60s
				Reason:     reasonCircuit(k),
				Confidence: 0.75,
				Source:     EngineAdaptive,
			})
		}
	}

	return actions, nil
}

// aggregateSuccessRate 计算快照中所有启用 Key 的加权平均成功率（按请求量无法获取，用算术平均近似）。
func aggregateSuccessRate(keys []KeySnap) float64 {
	var sum float64
	var n int
	for _, k := range keys {
		if !k.Enabled {
			continue
		}
		sum += k.SuccessRate
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// 预留：HealthTracker 在快照采集时已转为 SuccessRate，此处保留导入。
var _ = ratelimit.HealthTracker{}
