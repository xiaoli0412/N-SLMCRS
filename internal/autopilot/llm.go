package autopilot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LLMEngine 用大模型做可解释的复杂决策。
//
// 设计：prompt 构造 + JSON 解析齐全；上游 LLMBackend 可切换。
// - stubBackend：返回模板 JSON（确定性策略），保证 stub 也能产出可执行 Action。
// - gatewayBackend：调自身网关 /v1/chat/completions（环境变量 LLM_BASE_URL/LLM_API_KEY/LLM_MODEL 齐全时启用）。
// 配置缺失时用 stub；齐全则接真实 LLM 网关调用。
type LLMEngine struct {
	backend LLMBackend
}

// LLMBackend LLM 调用抽象。
type LLMBackend interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// stubBackend 返回确定性模板策略（复用简单规则），不依赖任何密钥。
type stubBackend struct{}

// NewLLMEngine 创建 LLM 引擎。backend 为 nil 时用 stubBackend。
func NewLLMEngine(backend LLMBackend) *LLMEngine {
	if backend == nil {
		backend = &stubBackend{}
	}
	return &LLMEngine{backend: backend}
}

// ID 引擎标识。
func (e *LLMEngine) ID() EngineID { return EngineLLM }

// Generate stub 实现：不接收 prompt，返回确定性的模板决策。
func (s *stubBackend) Generate(_ context.Context, _ string) (string, error) {
	return "", nil // 实际策略在 Decide 内直接构造，绕过 JSON 往返
}

// Decide 依据快照决策。
func (e *LLMEngine) Decide(ctx context.Context, snap Snapshot) ([]Action, error) {
	prompt := buildPrompt(snap)

	var actions []Action
	if sb, ok := e.backend.(*stubBackend); ok && sb != nil {
		// stub：直接用确定性规则（复用 adaptive 思路的精简版），保证可执行
		actions = e.stubDecide(snap)
		_ = prompt // stub 不消费 prompt，但保留构造（审计/未来真实 LLM 用）
	} else {
		// 真实 LLM：调后端 → 解析 JSON
		out, err := e.backend.Generate(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("LLM 调用失败: %w", err)
		}
		actions, err = parseLLMActions(out, snap)
		if err != nil {
			return nil, fmt.Errorf("解析 LLM 输出: %w", err)
		}
	}
	return actions, nil
}

// stubDecide 确定性的模板决策（与 adaptive 精简版一致，但带 LLM 风格的根因）。
func (e *LLMEngine) stubDecide(snap Snapshot) []Action {
	actions := make([]Action, 0, len(snap.Keys)+1)

	// 全局成功率过低 → 降并发
	rate := aggregateSuccessRate(snap.Keys)
	if rate > 0 && rate < 0.8 {
		target := snap.DefaultConcurrency
		if target <= 0 {
			target = 3
		}
		actions = append(actions, Action{
			Kind:       ActSetConcurrency,
			Value:      float64(target),
			Reason:     reasonLLMStub(fmt.Sprintf("全局成功率%.0f%%偏低，根因可能为上游限流/单Key过载，建议降并发并观察", rate*100)),
			Confidence: clamp01(0.6 + 0.3*(1-rate)),
			Source:     EngineLLM,
		})
	}

	// 故障 Key：连续失败≥5 → 建议短熔断
	for _, k := range snap.Keys {
		if !k.Enabled || k.Status == "circuit_open" {
			continue
		}
		if k.ConsecFail >= 5 || k.SuccessRate < 0.3 {
			act := Action{
				Kind:       ActOpenCircuit,
				KeyID:      k.ID,
				Value:      90,
				Reason:     reasonLLMStub(fmt.Sprintf("密钥 %s 健康%.0f%%/连续失败%d，疑似密钥失效或区域故障，建议隔离观察", k.Mask, k.SuccessRate*100, k.ConsecFail)),
				Confidence: 0.8,
				Source:     EngineLLM,
			}
			actions = append(actions, act)
		}
	}
	return actions
}

// buildPrompt 构造给真实 LLM 的提示词。
func buildPrompt(snap Snapshot) string {
	var sb strings.Builder
	sb.WriteString("你是 N-SLMCRS 网关的智能调度器。基于以下现状给出调度决策。\n")
	sb.WriteString(fmt.Sprintf("- 当前并发度: %d (默认%d/上限%d)\n", snap.CurrentConcurrency, snap.DefaultConcurrency, snap.MaxConcurrency))
	sb.WriteString(fmt.Sprintf("- 全局成功率(1h): %.1f%%, 当前RPM: %d\n", snap.Metrics.SuccessRate, snap.Metrics.CurrentRPM))
	sb.WriteString("- 密钥列表:\n")
	for _, k := range snap.Keys {
		sb.WriteString(fmt.Sprintf("  - id=%d %s enabled=%v status=%s successRate=%.1f%% consecFail=%d\n",
			k.ID, k.Mask, k.Enabled, k.Status, k.SuccessRate*100, k.ConsecFail))
	}
	sb.WriteString("\n可选动作: set_concurrency(目标并发度) | set_weight_boost(keyID, 0-1降权) | disable_key(keyID) | open_circuit(keyID, 冷却秒数) | revoke_credential(credID)\n")
	sb.WriteString("输出纯 JSON，格式: {\"actions\":[{\"kind\":\"set_concurrency\",\"value\":3,\"reason\":\"...\",\"confidence\":0.8}],\"rationale\":\"总体说明\"}\n")
	return sb.String()
}

// llmAction 单条 LLM 返回的动作（带 omitempty，便于解析）。
type llmAction struct {
	Kind       string  `json:"kind"`
	KeyID      int64   `json:"key_id,omitempty"`
	CredID     int64   `json:"cred_id,omitempty"`
	Value      float64 `json:"value,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type llmResponse struct {
	Actions   []llmAction `json:"actions"`
	Rationale string      `json:"rationale"`
}

// parseLLMActions 解析 LLM 返回的 JSON 为动作列表。
func parseLLMActions(raw string, _ Snapshot) ([]Action, error) {
	// 兼容模型把 JSON 包在 ```json ... ``` 中
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var resp llmResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("非法 JSON: %w", err)
	}
	out := make([]Action, 0, len(resp.Actions))
	for _, a := range resp.Actions {
		out = append(out, Action{
			Kind:       ActionKind(a.Kind),
			KeyID:      a.KeyID,
			CredID:     a.CredID,
			Value:      a.Value,
			Reason:     a.Reason,
			Confidence: clamp01(a.Confidence),
			Source:     EngineLLM,
		})
	}
	return out, nil
}
