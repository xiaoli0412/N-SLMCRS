package scheduler

import (
	"testing"
	"time"
)

// fakeRuntime 测试用 RuntimeOverrides 桩。
type fakeRuntime struct {
	conc int
	wb   float64
}

func (f fakeRuntime) Concurrency() int          { return f.conc }
func (f fakeRuntime) WeightBoost(int64) float64 { return f.wb }

// TestEffectiveConcurrency_Precedence 验证并发度生效优先级（Phase 1 回归）：
// Runtime 覆盖优先于配置默认，且钳位到 MaxConcurrency；未覆盖时回退配置默认。
//
// 这锁定 UpdateConfig（落库的配置默认）与 Runtime.SetConcurrency（Auto-Pilot 内存覆盖）
// 两套并发度通道的读时优先级：effectiveConcurrency() 始终 Runtime 优先、回退配置默认，
// 二者不会"互踩"——admin 改配置默认不会被 Runtime 覆盖掩盖（除非 autopilot 主动设置覆盖）。
func TestEffectiveConcurrency_Precedence(t *testing.T) {
	s := &Scheduler{config: SchedulerConfig{
		DefaultConcurrency: 5,
		MaxConcurrency:     10,
		RequestTimeout:     180 * time.Second,
		CircuitThreshold:   5,
		CircuitCooldown:    30 * time.Second,
		HealthWindow:       2 * time.Minute,
	}}

	// 无 Runtime 覆盖 → 配置默认 5
	if got := s.effectiveConcurrency(); got != 5 {
		t.Fatalf("无覆盖时应为配置默认 5，得到 %d", got)
	}

	// Runtime 覆盖 7 → 生效（优先于配置默认）
	s.runtime = fakeRuntime{conc: 7}
	if got := s.effectiveConcurrency(); got != 7 {
		t.Fatalf("Runtime 覆盖 7 应优先生效，得到 %d", got)
	}

	// Runtime 覆盖超过 Max(10) → 钳位到 10
	s.runtime = fakeRuntime{conc: 99}
	if got := s.effectiveConcurrency(); got != 10 {
		t.Fatalf("超过上限应钳位到 10，得到 %d", got)
	}

	// Runtime 返回 <=0（未覆盖）→ 回退配置默认
	s.runtime = fakeRuntime{conc: 0}
	if got := s.effectiveConcurrency(); got != 5 {
		t.Fatalf("Runtime<=0 应回退配置默认 5，得到 %d", got)
	}
}
