package autopilot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nslmcrs/gateway/internal/data"
)

// newTestStore 构造一个临时 SQLite Store（测试结束自动清理）。
func newTestStore(t *testing.T) *data.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := data.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("打开测试 DB: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// seedKey 插入一个上游密钥并返回其 ID（供破坏性动作指向真实记录）。
func seedKey(t *testing.T, store *data.Store, idSeed string) int64 {
	t.Helper()
	k, err := store.AddUpstreamKey(context.Background(), "nvapi-"+idSeed, "test", "t@e", 40)
	if err != nil {
		t.Fatalf("AddUpstreamKey: %v", err)
	}
	return k.ID
}

// TestExecutor_ManualNeverExecutes manual 模式下任何动作都不得执行（仅观察）。
func TestExecutor_ManualNeverExecutes(t *testing.T) {
	store := newTestStore(t)
	rt := NewRuntime()
	exec := NewExecutor(store, rt)
	keyID := seedKey(t, store, "manualkey")

	acts := []Action{
		{Kind: ActSetConcurrency, Value: 9, Confidence: 0.99, Source: EngineAdaptive},
		{Kind: ActSetWeightBoost, KeyID: keyID, Value: 0.1, Confidence: 0.99, Source: EngineAdaptive},
		{Kind: ActDisableKey, KeyID: keyID, Confidence: 0.99, Source: EngineAdaptive},
	}
	applied := exec.Apply(context.Background(), ModeManual, acts)
	if applied != 0 {
		t.Fatalf("manual 模式 applied 应为 0，得到 %d", applied)
	}
	if rt.Concurrency() != 0 {
		t.Fatalf("manual 不应写入 Runtime，Concurrency=%d", rt.Concurrency())
	}
	if rt.WeightBoost(keyID) != 1.0 {
		t.Fatalf("manual 不应写入 WeightBoost，得到 %v", rt.WeightBoost(keyID))
	}
	// 密钥应仍启用
	k, _ := store.GetUpstreamKey(context.Background(), keyID)
	if !k.Enabled {
		t.Fatalf("manual 不应禁用密钥")
	}
	// 但应留有审计事件（观察记录）
	_, interventions, events := exec.Stats()
	if len(events) != 3 {
		t.Fatalf("manual 应为每个动作写观察事件，得到 %d 事件", len(events))
	}
	if interventions != 0 {
		t.Fatalf("manual 不应累加干预数，得到 %d", interventions)
	}
}

// TestExecutor_FullAutoExecutesReversible fullauto 下可逆调参直接写入 Runtime。
func TestExecutor_FullAutoExecutesReversible(t *testing.T) {
	store := newTestStore(t)
	rt := NewRuntime()
	exec := NewExecutor(store, rt)
	keyID := seedKey(t, store, "revkey")

	acts := []Action{
		{Kind: ActSetConcurrency, Value: 7, Confidence: 0.6, Source: EngineAdaptive},
		{Kind: ActSetWeightBoost, KeyID: keyID, Value: 0.2, Confidence: 0.6, Source: EngineAdaptive},
	}
	applied := exec.Apply(context.Background(), ModeFullAuto, acts)
	if applied != 2 {
		t.Fatalf("fullauto 可逆动作应全部执行，applied=%d", applied)
	}
	if rt.Concurrency() != 7 {
		t.Fatalf("Runtime 并发度应为 7，得到 %d", rt.Concurrency())
	}
	if rt.WeightBoost(keyID) != 0.2 {
		t.Fatalf("Runtime 权重应为 0.2，得到 %v", rt.WeightBoost(keyID))
	}
}

// TestExecutor_FullAutoDestructiveConfidenceGate fullauto 下破坏性动作需 Confidence>=0.7，
// 否则跳过执行。
func TestExecutor_FullAutoDestructiveConfidenceGate(t *testing.T) {
	store := newTestStore(t)
	rt := NewRuntime()
	exec := NewExecutor(store, rt)
	keyID := seedKey(t, store, "gatekey")

	// 低置信度禁用 → 应跳过
	low := []Action{{Kind: ActDisableKey, KeyID: keyID, Confidence: 0.69, Source: EngineAdaptive}}
	if applied := exec.Apply(context.Background(), ModeFullAuto, low); applied != 0 {
		t.Fatalf("低置信度破坏性动作应跳过，applied=%d", applied)
	}
	k, _ := store.GetUpstreamKey(context.Background(), keyID)
	if !k.Enabled {
		t.Fatalf("低置信度不应禁用密钥")
	}

	// 高置信度禁用 → 应执行
	high := []Action{{Kind: ActDisableKey, KeyID: keyID, Confidence: 0.7, Source: EngineAdaptive}}
	if applied := exec.Apply(context.Background(), ModeFullAuto, high); applied != 1 {
		t.Fatalf("高置信度破坏性动作应执行，applied=%d", applied)
	}
	k2, _ := store.GetUpstreamKey(context.Background(), keyID)
	if k2.Enabled {
		t.Fatalf("高置信度应已禁用密钥")
	}
}

// TestExecutor_AssistedWritesPending assisted 模式下破坏性动作写入 pending，等人工批准。
func TestExecutor_AssistedWritesPending(t *testing.T) {
	store := newTestStore(t)
	rt := NewRuntime()
	exec := NewExecutor(store, rt)
	keyID := seedKey(t, store, "pendkey")

	act := Action{Kind: ActOpenCircuit, KeyID: keyID, Value: 60, Confidence: 0.9, Source: EngineAdaptive}
	if applied := exec.Apply(context.Background(), ModeAssisted, []Action{act}); applied != 0 {
		t.Fatalf("assisted 不应直接执行，applied=%d", applied)
	}

	pending, err := exec.ListPending(context.Background())
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("应写入 1 条 pending，得到 %d", len(pending))
	}

	// 批准后应实际执行熔断（status → circuit_open）
	settingKey := pending[0].Key
	if err := exec.ApprovePending(context.Background(), settingKey); err != nil {
		t.Fatalf("ApprovePending: %v", err)
	}
	k, _ := store.GetUpstreamKey(context.Background(), keyID)
	if k.Status != "circuit_open" {
		t.Fatalf("批准后应熔断，status=%s", k.Status)
	}
	// 批准后 pending 应被删除
	if n := exec.CountPending(context.Background()); n != 0 {
		t.Fatalf("批准后 pending 应清空，剩余 %d", n)
	}
}

// TestExecutor_AssistedReversibleNotExecuted assisted 下可逆调参也不直接执行（待确认）。
func TestExecutor_AssistedReversibleNotExecuted(t *testing.T) {
	store := newTestStore(t)
	rt := NewRuntime()
	exec := NewExecutor(store, rt)

	act := Action{Kind: ActSetConcurrency, Value: 4, Confidence: 0.9, Source: EngineAdaptive}
	exec.Apply(context.Background(), ModeAssisted, []Action{act})

	if rt.Concurrency() != 0 {
		t.Fatalf("assisted 不应直接写 Runtime，Concurrency=%d", rt.Concurrency())
	}
}
