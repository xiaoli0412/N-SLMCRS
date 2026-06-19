package autopilot

import "sync"

// Runtime 线程安全的调度策略覆盖，注入到 Scheduler。
// 零值表示"不覆盖，使用调度器默认"。所有字段通过方法读写（带锁）。
//
// 仅承载可逆的运行时调参：
//   - Concurrency：覆盖 DefaultConcurrency（0=用默认）
//   - WeightBoost：keyID → 权重乘子（0..1 降权，>1 加权；不存在则 1.0）
//
// 破坏性操作（启停/熔断/吊销）由 Executor 直接落库，不经此处。
type Runtime struct {
	mu          sync.RWMutex
	concurrency int                 // 0=用默认
	weightBoost map[int64]float64   // keyID → 乘子
}

// NewRuntime 创建运行时（默认不覆盖任何值）。
func NewRuntime() *Runtime {
	return &Runtime{weightBoost: make(map[int64]float64)}
}

// Concurrency 返回覆盖的并发度（0=用默认）。
func (r *Runtime) Concurrency() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.concurrency
}

// SetConcurrency 设置覆盖的并发度（<=0 表示恢复默认）。
func (r *Runtime) SetConcurrency(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.concurrency = n
}

// WeightBoost 返回某密钥的权重乘子（不存在返回 1.0，表示不变）。
func (r *Runtime) WeightBoost(keyID int64) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if v, ok := r.weightBoost[keyID]; ok {
		return v
	}
	return 1.0
}

// SetWeightBoost 设置某密钥的权重乘子。
// mult<=0 或非常接近 1（恢复默认）时移除该覆盖；否则接受 0<mult<1（降权）或 mult>1（加权）。
func (r *Runtime) SetWeightBoost(keyID int64, mult float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 非法（<=0）或无意义（≈1，含 NaN）→ 移除覆盖，恢复正常
	if mult <= 0 || mult != mult || (mult >= 0.99 && mult <= 1.01) {
		delete(r.weightBoost, keyID)
		return
	}
	r.weightBoost[keyID] = mult
}

// Reset 清除所有覆盖（恢复调度器默认行为）。
func (r *Runtime) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.concurrency = 0
	r.weightBoost = make(map[int64]float64)
}

// 编译期断言：*Runtime 满足 scheduler.RuntimeOverrides，确保 Scheduler.SetRuntime 能注入。
var _ schedulerRuntimeOverrides = (*Runtime)(nil)

// schedulerRuntimeOverrides 与 scheduler.RuntimeOverrides 同构，置于此处仅为编译期校验，
// 避免在 autopilot 包内反向依赖 scheduler 造成循环导入。
type schedulerRuntimeOverrides interface {
	Concurrency() int
	WeightBoost(keyID int64) float64
}
