package autopilot

import (
	"context"
	"fmt"
	"strconv"

	"github.com/nslmcrs/gateway/internal/agent"
)

// newLLMTools 构造本次决策的 agent 工具集，绑定当前快照与动作收集器。
//
// 工具不直接执行——仅记录 Action 并返回当前观测，执行由 Executor.Apply 按模式统一处理。
// 这保证 LLM 引擎与其余引擎走同一执行路径（manual/assisted/fullauto 语义一致），
// 同时让 agent 的"观测"反映真实当前状态，支撑多步推理。
func newLLMTools(snap Snapshot) (*agent.ToolRegistry, *[]Action) {
	actions := &[]Action{}
	reg := agent.NewRegistry(
		setConcurrencyTool{snap: snap, out: actions},
		setWeightBoostTool{snap: snap, out: actions},
		disableKeyTool{snap: snap, out: actions},
		openCircuitTool{snap: snap, out: actions},
		revokeCredTool{snap: snap, out: actions},
	)
	return reg, actions
}

// keyObs 返回某密钥的观测摘要（供工具回灌 LLM）。
func keyObs(snap Snapshot, keyID int64) string {
	for _, k := range snap.Keys {
		if k.ID == keyID {
			return fmt.Sprintf("密钥 %s：enabled=%v status=%s successRate=%.1f%% consecFail=%d",
				k.Mask, k.Enabled, k.Status, k.SuccessRate*100, k.ConsecFail)
		}
	}
	return fmt.Sprintf("未找到 id=%d 的密钥", keyID)
}

// --- 工具 1：set_concurrency ---
type setConcurrencyTool struct {
	snap Snapshot
	out  *[]Action
}

func (t setConcurrencyTool) Name() string { return "set_concurrency" }
func (t setConcurrencyTool) Description() string {
	return "设置 N 路并发度（先到先得的并发请求数）。成功率低时降低以 back-off，高时提升以扩容。"
}
func (t setConcurrencyTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value":      map[string]any{"type": "integer", "description": "目标并发度（1..MaxConcurrency）"},
			"reason":     map[string]any{"type": "string", "description": "根因说明"},
			"confidence": map[string]any{"type": "number", "description": "置信度 0..1"},
		},
		"required": []string{"value", "reason"},
	}
}
func (t setConcurrencyTool) Run(_ context.Context, args map[string]any) (string, error) {
	val := toInt(args["value"])
	if val < 1 {
		val = 1
	}
	if t.snap.MaxConcurrency > 0 && val > t.snap.MaxConcurrency {
		val = t.snap.MaxConcurrency
	}
	*t.out = append(*t.out, Action{
		Kind: ActSetConcurrency, Value: float64(val),
		Reason: toStr(args["reason"]), Confidence: clamp01(toFloat(args["confidence"])), Source: EngineLLM,
	})
	return fmt.Sprintf("已记录：并发度→%d（当前 %d/默认 %d/上限 %d）。全局成功率 1h=%.1f%%，当前 RPM=%d。",
		val, t.snap.CurrentConcurrency, t.snap.DefaultConcurrency, t.snap.MaxConcurrency,
		t.snap.Metrics.SuccessRate, t.snap.Metrics.CurrentRPM), nil
}

// --- 工具 2：set_weight_boost ---
type setWeightBoostTool struct {
	snap Snapshot
	out  *[]Action
}

func (t setWeightBoostTool) Name() string { return "set_weight_boost" }
func (t setWeightBoostTool) Description() string {
	return "设置某密钥的权重乘子：<1 降权（减少选中概率），>1 加权。用于隔离健康差的密钥而不必直接禁用。"
}
func (t setWeightBoostTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key_id":     map[string]any{"type": "integer", "description": "密钥 ID"},
			"value":      map[string]any{"type": "number", "description": "权重乘子（0..1 降权，>1 加权）"},
			"reason":     map[string]any{"type": "string", "description": "根因说明"},
			"confidence": map[string]any{"type": "number", "description": "置信度 0..1"},
		},
		"required": []string{"key_id", "value", "reason"},
	}
}
func (t setWeightBoostTool) Run(_ context.Context, args map[string]any) (string, error) {
	keyID := int64(toInt(args["key_id"]))
	*t.out = append(*t.out, Action{
		Kind: ActSetWeightBoost, KeyID: keyID, Value: toFloat(args["value"]),
		Reason: toStr(args["reason"]), Confidence: clamp01(toFloat(args["confidence"])), Source: EngineLLM,
	})
	return "已记录：密钥权重调整。" + keyObs(t.snap, keyID), nil
}

// --- 工具 3：disable_key ---
type disableKeyTool struct {
	snap Snapshot
	out  *[]Action
}

func (t disableKeyTool) Name() string { return "disable_key" }
func (t disableKeyTool) Description() string {
	return "禁用某上游密钥（破坏性，fullauto 需 Confidence≥0.7，assisted 进 pending 待审）。用于彻底隔离失效密钥。"
}
func (t disableKeyTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key_id":     map[string]any{"type": "integer", "description": "密钥 ID"},
			"reason":     map[string]any{"type": "string", "description": "根因说明"},
			"confidence": map[string]any{"type": "number", "description": "置信度 0..1"},
		},
		"required": []string{"key_id", "reason"},
	}
}
func (t disableKeyTool) Run(_ context.Context, args map[string]any) (string, error) {
	keyID := int64(toInt(args["key_id"]))
	*t.out = append(*t.out, Action{
		Kind: ActDisableKey, KeyID: keyID,
		Reason: toStr(args["reason"]), Confidence: clamp01(toFloat(args["confidence"])), Source: EngineLLM,
	})
	return "已记录：禁用密钥建议。" + keyObs(t.snap, keyID), nil
}

// --- 工具 4：open_circuit ---
type openCircuitTool struct {
	snap Snapshot
	out  *[]Action
}

func (t openCircuitTool) Name() string { return "open_circuit" }
func (t openCircuitTool) Description() string {
	return "对某密钥触发短时熔断（破坏性，需置信度门槛）。冷却期内该密钥不参与调度。"
}
func (t openCircuitTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key_id":     map[string]any{"type": "integer", "description": "密钥 ID"},
			"cooldown_seconds": map[string]any{"type": "integer", "description": "冷却秒数（默认 60）"},
			"reason":     map[string]any{"type": "string", "description": "根因说明"},
			"confidence": map[string]any{"type": "number", "description": "置信度 0..1"},
		},
		"required": []string{"key_id", "reason"},
	}
}
func (t openCircuitTool) Run(_ context.Context, args map[string]any) (string, error) {
	keyID := int64(toInt(args["key_id"]))
	cooldown := toInt(args["cooldown_seconds"])
	if cooldown <= 0 {
		cooldown = 60
	}
	*t.out = append(*t.out, Action{
		Kind: ActOpenCircuit, KeyID: keyID, Value: float64(cooldown),
		Reason: toStr(args["reason"]), Confidence: clamp01(toFloat(args["confidence"])), Source: EngineLLM,
	})
	return fmt.Sprintf("已记录：熔断密钥 %d（冷却 %ds）。", keyID, cooldown) + keyObs(t.snap, keyID), nil
}

// --- 工具 5：revoke_credential ---
type revokeCredTool struct {
	snap Snapshot
	out  *[]Action
}

func (t revokeCredTool) Name() string { return "revoke_credential" }
func (t revokeCredTool) Description() string {
	return "吊销某下游凭证（破坏性，需置信度门槛）。用于撤销泄漏或滥用的下游 sk-nv- 凭证。"
}
func (t revokeCredTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cred_id":    map[string]any{"type": "integer", "description": "下游凭证 ID"},
			"reason":     map[string]any{"type": "string", "description": "根因说明"},
			"confidence": map[string]any{"type": "number", "description": "置信度 0..1"},
		},
		"required": []string{"cred_id", "reason"},
	}
}
func (t revokeCredTool) Run(_ context.Context, args map[string]any) (string, error) {
	credID := int64(toInt(args["cred_id"]))
	*t.out = append(*t.out, Action{
		Kind: ActRevokeCredential, CredID: credID,
		Reason: toStr(args["reason"]), Confidence: clamp01(toFloat(args["confidence"])), Source: EngineLLM,
	})
	return fmt.Sprintf("已记录：吊销下游凭证 %d。", credID), nil
}

// --- 参数解析辅助（JSON 反序列化后值多为 float64）---

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f
		}
	}
	return 0
}

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
