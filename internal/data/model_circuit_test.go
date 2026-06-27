package data

import (
	"context"
	"testing"
	"time"
)

// seedModel 插入一个模型行（满足 model_circuit 的外键约束）。
func seedModel(t *testing.T, store *Store, id string) {
	t.Helper()
	if _, err := store.UpsertModels(context.Background(), []Model{{ID: id, Object: "model", OwnedBy: "test"}}); err != nil {
		t.Fatalf("seed model %s: %v", id, err)
	}
}

// TestModelCircuit_FailureThenOpen 验证被动路径：连续失败达阈值转 open，成功回退 closed。
func TestModelCircuit_FailureThenOpen(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	const model = "meta/llama-3.1-8b-instruct"
	seedModel(t, store, model)

	// 阈值 3，冷却 60s：连续失败 2 次仍 closed，第 3 次转 open
	for i := 1; i <= 2; i++ {
		if err := store.RecordModelCircuitFailure(ctx, model, 3, 60); err != nil {
			t.Fatalf("fail #%d: %v", i, err)
		}
		mc, _ := store.GetModelCircuit(ctx, model)
		if mc.State != CircuitClosed {
			t.Fatalf("第 %d 次失败后应为 closed，实为 %s", i, mc.State)
		}
	}
	if err := store.RecordModelCircuitFailure(ctx, model, 3, 60); err != nil {
		t.Fatalf("fail #3: %v", err)
	}
	mc, err := store.GetModelCircuit(ctx, model)
	if err != nil || mc == nil {
		t.Fatalf("GetModelCircuit: %v %v", mc, err)
	}
	if mc.State != CircuitOpen {
		t.Fatalf("第 3 次失败后应为 open，实为 %s", mc.State)
	}
	if mc.OpenUntil <= time.Now().Unix() {
		t.Fatalf("open_until 应在未来")
	}

	// IsModelCircuitOpen 应报告阻塞
	blocked, state, _ := store.IsModelCircuitOpen(ctx, model)
	if !blocked || state != CircuitOpen {
		t.Fatalf("应阻塞 open，实为 blocked=%v state=%s", blocked, state)
	}

	// 成功 → 回退 closed
	if err := store.ResetModelCircuitConsecutive(ctx, model); err != nil {
		t.Fatalf("reset: %v", err)
	}
	mc, _ = store.GetModelCircuit(ctx, model)
	if mc.State != CircuitClosed || mc.ConsecutiveFail != 0 {
		t.Fatalf("成功后应 closed 且清零，实为 %s consec=%d", mc.State, mc.ConsecutiveFail)
	}
}

// TestModelCircuit_Permanent 验证永久熔断：manual SetPermanent 后被动成功/失败不影响。
func TestModelCircuit_Permanent(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	const model = "nvidia/nv-embed-v1"
	seedModel(t, store, model)

	// 设为永久熔断
	mc := ModelCircuit{Model: model, State: CircuitPermanent, Permanent: true, SuccessRatePct: 10, BadSweepCount: 3}
	if err := store.UpsertModelCircuit(ctx, mc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	blocked, state, _ := store.IsModelCircuitOpen(ctx, model)
	if !blocked || state != CircuitPermanent {
		t.Fatalf("应阻塞 permanent，实为 blocked=%v state=%s", blocked, state)
	}

	// 被动成功不应解除永久熔断
	_ = store.ResetModelCircuitConsecutive(ctx, model)
	blocked, _, _ = store.IsModelCircuitOpen(ctx, model)
	if !blocked {
		t.Fatal("永久熔断不应被被动成功解除")
	}

	// 被动失败也不改其状态
	_ = store.RecordModelCircuitFailure(ctx, model, 3, 60)
	mcPtr, _ := store.GetModelCircuit(ctx, model)
	if mcPtr.State != CircuitPermanent {
		t.Fatalf("永久熔断不应因被动失败变化，实为 %s", mcPtr.State)
	}

	// 手动复位 → closed
	if err := store.ResetModelCircuit(ctx, model); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	blocked, _, _ = store.IsModelCircuitOpen(ctx, model)
	if blocked {
		t.Fatal("手动复位后应不再阻塞")
	}
}

// TestModelCircuit_HiddenList 验证 ListCircuitHiddenModels：open+permanent 入隐藏集，closed 不入。
func TestModelCircuit_HiddenList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	seedModel(t, store, "m/open")
	seedModel(t, store, "m/perm")
	seedModel(t, store, "m/closed")

	// open（未过冷却）
	_ = store.UpsertModelCircuit(ctx, ModelCircuit{Model: "m/open", State: CircuitOpen, OpenUntil: time.Now().Unix() + 300})
	// permanent
	_ = store.UpsertModelCircuit(ctx, ModelCircuit{Model: "m/perm", State: CircuitPermanent, Permanent: true})
	// closed（不隐藏）
	_ = store.UpsertModelCircuit(ctx, ModelCircuit{Model: "m/closed", State: CircuitClosed})

	hidden, err := store.ListCircuitHiddenModels(ctx)
	if err != nil {
		t.Fatalf("ListCircuitHiddenModels: %v", err)
	}
	if len(hidden) != 2 {
		t.Fatalf("应隐藏 2 个，实为 %d: %v", len(hidden), hidden)
	}
	if _, ok := hidden["m/open"]; !ok {
		t.Error("m/open 应在隐藏集")
	}
	if _, ok := hidden["m/perm"]; !ok {
		t.Error("m/perm 应在隐藏集")
	}
	if _, ok := hidden["m/closed"]; ok {
		t.Error("m/closed 不应在隐藏集")
	}
}
