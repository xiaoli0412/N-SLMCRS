package autopilot

import (
	"context"
	"testing"

	"github.com/nslmcrs/gateway/internal/data"
)

// makeSnap 构造一个用于引擎决策的快照。
func makeSnap(rate float64, consecFail int, currentConcurrency, defaultConcurrency, maxConcurrency int) Snapshot {
	return Snapshot{
		Keys: []KeySnap{
			{
				ID:           1,
				Mask:         "nvapi-...9eR",
				Enabled:      true,
				Status:       "active",
				SuccessRate:  rate,
				ConsecFail:   consecFail,
				RPMRemaining: 40,
			},
		},
		Metrics:            data.Metrics{}, // 引擎目前不深度依赖其字段
		CurrentConcurrency: currentConcurrency,
		DefaultConcurrency: defaultConcurrency,
		MaxConcurrency:     maxConcurrency,
	}
}

// TestAdaptiveEngine_NoDataNoAction 无成功率数据时引擎应不误动（返回 nil）。
func TestAdaptiveEngine_NoDataNoAction(t *testing.T) {
	e := NewAdaptiveEngine()
	snap := makeSnap(0, 0, 5, 5, 10) // rate=0 → 无数据
	acts, err := e.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide 出错: %v", err)
	}
	if acts != nil {
		t.Fatalf("无数据时应返回 nil，得到 %v", acts)
	}
}

// TestAdaptiveEngine_LowGlobalRateProducesConcurrency 全局成功率偏离目标时，
// 引擎应产出 set_concurrency 且落在合法钳位区间 [1, Max]。
// 注：PID 误差为正（成功率低于目标）时引擎按设计向上调并发以增加重试，方向不在测试断言范围。
func TestAdaptiveEngine_LowGlobalRateProducesConcurrency(t *testing.T) {
	e := NewAdaptiveEngine()
	snap := makeSnap(0.4, 0, 8, 5, 10)
	acts, err := e.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide 出错: %v", err)
	}
	var conc *Action
	for i := range acts {
		if acts[i].Kind == ActSetConcurrency {
			conc = &acts[i]
		}
	}
	if conc == nil {
		t.Fatalf("成功率偏离目标时应产出 set_concurrency，得到 %v", acts)
	}
	got := int(conc.Value)
	if got < 1 || got > snap.MaxConcurrency {
		t.Fatalf("并发度 %d 越界 [1,%d]", got, snap.MaxConcurrency)
	}
}

// TestAdaptiveEngine_SteadyProducesNoAction 当前并发已等于引擎目标时不应产出冗余动作。
func TestAdaptiveEngine_SteadyProducesNoAction(t *testing.T) {
	e := NewAdaptiveEngine()
	// 成功率恰等于目标 0.99 → 误差≈0 → target≈base；当前并发==base → 无 set_concurrency
	snap := makeSnap(0.99, 0, 5, 5, 10)
	acts, err := e.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide 出错: %v", err)
	}
	for _, a := range acts {
		if a.Kind == ActSetConcurrency {
			t.Fatalf("稳态（current==target）不应产出冗余 set_concurrency，得到 %v", a)
		}
	}
}

// TestAdaptiveEngine_LowRateDownweightsKey 单 Key 成功率<50% 应降权。
func TestAdaptiveEngine_LowRateDownweightsKey(t *testing.T) {
	e := NewAdaptiveEngine()
	snap := makeSnap(0.3, 4, 5, 5, 10)
	acts, err := e.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide 出错: %v", err)
	}
	var boost *Action
	for i := range acts {
		if acts[i].Kind == ActSetWeightBoost {
			boost = &acts[i]
		}
	}
	if boost == nil {
		t.Fatalf("低成功率应产出 set_weight_boost，得到 %v", acts)
	}
	if boost.Value >= 1.0 {
		t.Fatalf("降权乘子应 <1，得到 %v", boost.Value)
	}
	if boost.KeyID != 1 {
		t.Fatalf("WeightBoost 的 KeyID 应为 1，得到 %d", boost.KeyID)
	}
}

// TestAdaptiveEngine_RepeatedDecideStable 连续多次决策不 panic，并发度落在 [1, Max] 区间。
func TestAdaptiveEngine_RepeatedDecideStable(t *testing.T) {
	e := NewAdaptiveEngine()
	snap := makeSnap(0.95, 0, 5, 5, 10)
	for i := 0; i < 10; i++ {
		acts, err := e.Decide(context.Background(), snap)
		if err != nil {
			t.Fatalf("第 %d 次决策出错: %v", i, err)
		}
		for _, a := range acts {
			if a.Kind == ActSetConcurrency {
				n := int(a.Value)
				if n < 1 || n > snap.MaxConcurrency {
					t.Fatalf("并发度 %d 越界 [1,%d]", n, snap.MaxConcurrency)
				}
			}
		}
	}
}

// TestForecastEngine_InsufficientDataNoAction 数据不足（<2 个周期）应不误动。
func TestForecastEngine_InsufficientDataNoAction(t *testing.T) {
	e := NewForecastEngine()
	snap := makeSnap(0.99, 0, 5, 5, 10)
	snap.Series = nil // 无时序
	acts, err := e.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide 出错: %v", err)
	}
	if len(acts) != 0 {
		t.Fatalf("时序数据不足时应不产出动作，得到 %v", acts)
	}
}

// TestLLMEngine_StubProducesActions stub 后端应产出可执行动作（不依赖外部 LLM）。
func TestLLMEngine_StubProducesActions(t *testing.T) {
	e := NewLLMEngine(nil) // nil → stubBackend
	// 全局成功率偏低 → 触发降并发；故障 Key → 触发熔断
	snap := makeSnap(0.2, 6, 5, 5, 10)
	acts, err := e.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide 出错: %v", err)
	}
	if len(acts) == 0 {
		t.Fatalf("stub 引擎在明显故障下应产出动作")
	}
	// 所有动作应标记为 LLM 来源
	for _, a := range acts {
		if a.Source != EngineLLM {
			t.Fatalf("动作来源应为 llm，得到 %s", a.Source)
		}
	}
}

// TestAggregateSuccessRate 算术平均成功率的边界（空/全启用/含禁用）。
func TestAggregateSuccessRate(t *testing.T) {
	if got := aggregateSuccessRate(nil); got != 0 {
		t.Fatalf("空列表应返回 0，得到 %v", got)
	}
	keys := []KeySnap{
		{Enabled: true, SuccessRate: 1.0},
		{Enabled: true, SuccessRate: 0.6},
		{Enabled: false, SuccessRate: 0.0}, // 禁用不计入
	}
	if got := aggregateSuccessRate(keys); got != 0.8 {
		t.Fatalf("两启用密钥平均应 0.8，得到 %v", got)
	}
}
