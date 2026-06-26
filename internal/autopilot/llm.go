package autopilot

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nslmcrs/gateway/internal/agent"
)

// LLMEngine 用大模型做可解释的复杂决策（agent 化）。
//
// 真后端（agent.LLMBackend 非 nil）：跑 ReAct 循环——LLM 在工具调用中
// 思考(think)→调用调度工具(act)→读取观测(observe)→继续或收手，
// 全程产出可调试的 StepTrace（供前端"推理链"展示）。
//
// 无后端（nil）：回退确定性 stubDecide（仍可产出动作，但非真 LLM），
// 并在 State.LLMBackendMode 标注 "stub"，避免误以为"AI 在工作"而实为 stub。
//
// 工具不直接执行——仅记录 Action 并返回当前观测；执行由 Executor.Apply 按模式统一处理，
// 使 LLM 引擎与其余引擎（adaptive/forecast）走同一执行路径，manual/assisted/fullauto 语义一致。
type LLMEngine struct {
	backend agent.LLMBackend // nil → stub 降级

	mu        sync.Mutex
	lastTrace []agent.StepTrace
}

// NewLLMEngine 创建 LLM 引擎。backend 为 nil 时走 stub 降级。
func NewLLMEngine(backend agent.LLMBackend) *LLMEngine {
	return &LLMEngine{backend: backend}
}

// ID 引擎标识。
func (e *LLMEngine) ID() EngineID { return EngineLLM }

// LastTrace 返回最近一次决策的推理轨迹（供 Controller 填入 State.RecentTrace）。
func (e *LLMEngine) LastTrace() []agent.StepTrace {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastTrace
}

// BackendMode 返回后端模式：stub|gateway（供可观测）。
func (e *LLMEngine) BackendMode() string {
	if e.backend == nil {
		return "stub"
	}
	return e.backend.Mode()
}

// Decide 依据快照决策。
func (e *LLMEngine) Decide(ctx context.Context, snap Snapshot) ([]Action, error) {
	if e.backend == nil {
		// stub：无 LLM 可调，回退确定性规则（与 adaptive 思路一致的精简版）
		acts := e.stubDecide(snap)
		e.mu.Lock()
		e.lastTrace = []agent.StepTrace{{
			Step: 1, Role: "think",
			Content: "LLM(stub)：未配置真 LLM 后端，回退确定性规则降级输出",
		}}
		e.mu.Unlock()
		return acts, nil
	}

	// 真 LLM：ReAct agent 循环
	registry, out := newLLMTools(snap)
	ag := agent.NewAgent(e.backend, registry, 6)
	res, err := ag.Run(ctx, buildSystemPrompt(snap), buildUserInput(snap))
	e.mu.Lock()
	e.lastTrace = res.Steps
	e.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("agent 循环失败: %w", err)
	}
	return *out, nil
}

// stubDecide 确定性的模板决策（无 LLM 时的降级，与 adaptive 精简版一致，但带 LLM 风格的根因）。
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

// buildSystemPrompt 构造系统提示（角色 + 工具 + 原则）。
func buildSystemPrompt(_ Snapshot) string {
	return "你是 N-SLMCRS 网关的智能调度 agent。基于现状用工具给出调度决策。\n" +
		"可用工具：set_concurrency / set_weight_boost / disable_key / open_circuit / revoke_credential。\n" +
		"每次调用工具会返回当前观测；可多步推理后收手（不再调用工具即结束）。\n" +
		"原则：成功率偏低先 back-off（降并发）；故障 Key 降权或隔离；保守、可解释、不误动。"
}

// buildUserInput 构造用户输入（当前现状快照）。
func buildUserInput(snap Snapshot) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("当前并发度: %d (默认%d/上限%d)\n", snap.CurrentConcurrency, snap.DefaultConcurrency, snap.MaxConcurrency))
	sb.WriteString(fmt.Sprintf("全局成功率(1h): %.1f%%, 当前RPM: %d\n", snap.Metrics.SuccessRate, snap.Metrics.CurrentRPM))
	sb.WriteString("密钥列表:\n")
	for _, k := range snap.Keys {
		sb.WriteString(fmt.Sprintf("  - id=%d %s enabled=%v status=%s successRate=%.1f%% consecFail=%d\n",
			k.ID, k.Mask, k.Enabled, k.Status, k.SuccessRate*100, k.ConsecFail))
	}
	sb.WriteString("\n请给出调度决策。")
	return sb.String()
}
