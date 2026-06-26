package autopilot

import (
	"context"
	"testing"
	"time"

	"github.com/nslmcrs/gateway/internal/ratelimit"
)

// newTestController 构造一个 Controller（带临时 store + 限流器/健康器）。
func newTestController(t *testing.T) (*Controller, *Runtime) {
	t.Helper()
	store := newTestStore(t)
	rt := NewRuntime()
	health := ratelimit.NewHealthTracker(2 * time.Minute)
	rl := ratelimit.NewManager(40)
	ctrl := NewController(store, health, rl, rt, 2*time.Minute, 5, 10, LLMConfig{})
	return ctrl, rt
}

// TestController_EffectiveConcurrency 验证 Step 5 的核心修复：
// Runtime 覆盖优先于默认，且钳位到 MaxConcurrency；未覆盖时回退默认。
func TestController_EffectiveConcurrency(t *testing.T) {
	ctrl, rt := newTestController(t)

	// 默认：无覆盖 → DefaultConcurrency
	if got := ctrl.effectiveConcurrency(); got != 5 {
		t.Fatalf("无覆盖时应为默认 5，得到 %d", got)
	}

	// 覆盖为 7 → 生效
	rt.SetConcurrency(7)
	if got := ctrl.effectiveConcurrency(); got != 7 {
		t.Fatalf("覆盖 7 应生效，得到 %d", got)
	}

	// 覆盖超过 Max(10) → 钳位到 10
	rt.SetConcurrency(99)
	if got := ctrl.effectiveConcurrency(); got != 10 {
		t.Fatalf("超过上限应钳位到 10，得到 %d", got)
	}

	// 恢复默认
	rt.SetConcurrency(0)
	if got := ctrl.effectiveConcurrency(); got != 5 {
		t.Fatalf("恢复默认后应为 5，得到 %d", got)
	}
}

// TestController_StateReflectsRuntime State 应上报当前 Runtime 并发度（供前端"当前策略"卡片）。
func TestController_StateReflectsRuntime(t *testing.T) {
	ctrl, rt := newTestController(t)
	rt.SetConcurrency(7)

	st := ctrl.State(context.Background())
	if st.RuntimeConcurrency != 7 {
		t.Fatalf("State.RuntimeConcurrency 应为 7，得到 %d", st.RuntimeConcurrency)
	}
	if st.MaxConcurrency != 10 || st.DefaultConcurrency != 5 {
		t.Fatalf("State 并发上下文不符：Default=%d Max=%d", st.DefaultConcurrency, st.MaxConcurrency)
	}
	// 默认模式/引擎
	if st.Mode != ModeAssisted {
		t.Fatalf("默认模式应为 assisted，得到 %s", st.Mode)
	}
	if st.Engine != EngineAdaptive {
		t.Fatalf("默认引擎应为 adaptive，得到 %s", st.Engine)
	}
}

// TestController_SetModeValidates 非法模式被拒，合法模式持久化到 settings。
func TestController_SetModeValidates(t *testing.T) {
	ctrl, _ := newTestController(t)
	ctx := context.Background()

	if err := ctrl.SetMode(ctx, "bogus"); err == nil {
		t.Fatal("非法模式应报错")
	}
	if err := ctrl.SetMode(ctx, ModeFullAuto); err != nil {
		t.Fatalf("合法模式应成功: %v", err)
	}
	// 持久化：直接读 settings 校验
	persisted, _ := ctrl.store.GetSetting(ctx, settingKeyMode)
	if persisted != string(ModeFullAuto) {
		t.Fatalf("模式未持久化，settings=%q", persisted)
	}
	if got := ctrl.State(ctx).Mode; got != ModeFullAuto {
		t.Fatalf("切换后 State.Mode 应为 fullauto，得到 %s", got)
	}
}

// TestController_SetEngineValidates 非法引擎被拒，合法引擎持久化。
func TestController_SetEngineValidates(t *testing.T) {
	ctrl, _ := newTestController(t)
	ctx := context.Background()

	if err := ctrl.SetEngine(ctx, "nope"); err == nil {
		t.Fatal("非法引擎应报错")
	}
	for _, e := range []EngineID{EngineAdaptive, EngineForecast, EngineLLM} {
		if err := ctrl.SetEngine(ctx, e); err != nil {
			t.Fatalf("切换引擎 %s 失败: %v", e, err)
		}
		persisted, _ := ctrl.store.GetSetting(ctx, settingKeyEngine)
		if persisted != string(e) {
			t.Fatalf("引擎 %s 未持久化，settings=%q", e, persisted)
		}
	}
}

// TestController_LoadPersisted 启动时应从 settings 恢复之前持久化的 mode/engine。
func TestController_LoadPersisted(t *testing.T) {
	ctrl, _ := newTestController(t)
	ctx := context.Background()
	// 写入持久化值
	ctrl.store.SetSetting(ctx, settingKeyMode, string(ModeManual))
	ctrl.store.SetSetting(ctx, settingKeyEngine, string(EngineForecast))

	ctrl.loadPersisted(ctx)

	st := ctrl.State(ctx)
	if st.Mode != ModeManual {
		t.Fatalf("应恢复 mode=manual，得到 %s", st.Mode)
	}
	if st.Engine != EngineForecast {
		t.Fatalf("应恢复 engine=predict，得到 %s", st.Engine)
	}
}
