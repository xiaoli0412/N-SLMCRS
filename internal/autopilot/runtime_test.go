package autopilot

import "testing"

// TestRuntimeDefaults 覆盖零值语义：不覆盖时并发度=0、权重=1.0（调度器视为"用默认"）。
func TestRuntimeDefaults(t *testing.T) {
	r := NewRuntime()
	if got := r.Concurrency(); got != 0 {
		t.Fatalf("默认 Concurrency 应为 0，得到 %d", got)
	}
	if got := r.WeightBoost(42); got != 1.0 {
		t.Fatalf("未知 key 的 WeightBoost 应为 1.0，得到 %v", got)
	}
}

// TestSetWeightBoost 是针对 Step 5 修复的回归测试：
// 原实现错误地把 mult>=1 全部删除，导致无法加权（与 types.go 契约 ">1 加权" 矛盾）。
// 此处锁定：降权(0.1)、加权(2.0)、恢复(≈1)、非法(<=0, NaN) 四类语义。
func TestSetWeightBoost(t *testing.T) {
	r := NewRuntime()

	// 1) 降权到 0.1 应被保留
	r.SetWeightBoost(1, 0.1)
	if got := r.WeightBoost(1); got != 0.1 {
		t.Fatalf("降权 0.1 未生效，得到 %v", got)
	}

	// 2) 加权到 2.0 应被保留（这是旧实现的 bug：会被丢弃）
	r.SetWeightBoost(2, 2.0)
	if got := r.WeightBoost(2); got != 2.0 {
		t.Fatalf("加权 2.0 未生效（回归：旧版 mult>=1 会误删），得到 %v", got)
	}

	// 3) 接近 1.0 视为"恢复默认"，应删除覆盖
	r.SetWeightBoost(1, 1.0)
	if got := r.WeightBoost(1); got != 1.0 {
		t.Fatalf("WeightBoost(key1) 应恢复为 1.0，得到 %v", got)
	}

	// 4) 非法值 <=0 / NaN 应删除覆盖
	r.SetWeightBoost(2, -1)
	if got := r.WeightBoost(2); got != 1.0 {
		t.Fatalf("负值未清除覆盖，得到 %v", got)
	}
	r.SetWeightBoost(2, 2.0)
	r.SetWeightBoost(2, 0) // 清除
	if got := r.WeightBoost(2); got != 1.0 {
		t.Fatalf("0 未清除覆盖，得到 %v", got)
	}
}

// TestSetConcurrency 与 Reset 的覆盖/恢复语义。
func TestSetConcurrencyAndReset(t *testing.T) {
	r := NewRuntime()
	r.SetConcurrency(7)
	if got := r.Concurrency(); got != 7 {
		t.Fatalf("SetConcurrency(7) 未生效，得到 %d", got)
	}
	r.SetConcurrency(0) // <=0 视为恢复默认
	if got := r.Concurrency(); got != 0 {
		t.Fatalf("SetConcurrency(0) 应恢复默认，得到 %d", got)
	}

	r.SetConcurrency(7)
	r.SetWeightBoost(1, 0.5)
	r.Reset()
	if r.Concurrency() != 0 || r.WeightBoost(1) != 1.0 {
		t.Fatalf("Reset 未清除覆盖：Concurrency=%d WeightBoost=%v", r.Concurrency(), r.WeightBoost(1))
	}
}

// TestRuntimeImplementsOverrides 是编译期契约的运行期对应：
// 确保 *Runtime 可作为 scheduler.RuntimeOverrides 注入（接口方法签名一致）。
func TestRuntimeImplementsOverrides(t *testing.T) {
	var _ schedulerRuntimeOverrides = NewRuntime()
	// 顺带验证字段访问不 panic
	rt := NewRuntime()
	rt.SetConcurrency(3)
	rt.SetWeightBoost(5, 0.2)
	_ = rt.Concurrency()
	_ = rt.WeightBoost(5)
}
