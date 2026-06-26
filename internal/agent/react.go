package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// RunResult agent 一次运行的产出。
type RunResult struct {
	Steps   []StepTrace // 完整推理轨迹（think/act/observe 链）
	Actions []ToolCall  // 所有工具调用（供调用方转为业务动作）
}

// Agent ReAct 推理代理：think→act→observe 循环，maxSteps 钳位。
//
// 循环：每轮调 LLM.Chat → 若返回 tool_calls 则逐个执行工具、把观测回灌消息历史，
// 继续下一轮；若不返回 tool_calls（LLM 决定收手）则结束。每步落 StepTrace。
type Agent struct {
	backend  LLMBackend
	registry *ToolRegistry
	maxSteps int
}

// NewAgent 创建 agent。maxSteps<=0 时默认 6。
func NewAgent(backend LLMBackend, registry *ToolRegistry, maxSteps int) *Agent {
	if maxSteps <= 0 {
		maxSteps = 6
	}
	return &Agent{backend: backend, registry: registry, maxSteps: maxSteps}
}

// Run 执行 ReAct 循环。system 为系统提示，input 为用户输入（任务+现状）。
func (a *Agent) Run(ctx context.Context, system, input string) (RunResult, error) {
	var steps []StepTrace
	var actions []ToolCall

	tools := a.registry.OpenAITools()
	messages := []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: input},
	}

	finished := false
	for step := 1; step <= a.maxSteps; step++ {
		resp, err := a.backend.Chat(ctx, messages, tools)
		if err != nil {
			steps = append(steps, StepTrace{Step: step, Role: "think", Error: err.Error()})
			return RunResult{Steps: steps, Actions: actions}, fmt.Errorf("step %d LLM 调用失败: %w", step, err)
		}

		// think：记录 LLM 文本
		steps = append(steps, StepTrace{Step: step, Role: "think", Content: resp.Content})

		// 无 tool_calls → LLM 决定收手
		if len(resp.ToolCalls) == 0 {
			finished = true
			break
		}

		// 把 assistant 回复（含 tool_calls）加入历史
		messages = append(messages, Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})

		// 逐个执行工具 → observe 回灌
		for _, tc := range resp.ToolCalls {
			actions = append(actions, tc)
			argsJSON, _ := json.Marshal(tc.Args)
			steps = append(steps, StepTrace{
				Step: step, Role: "act", ToolName: tc.Name, ToolArgs: string(argsJSON),
				Content: fmt.Sprintf("调用 %s(%s)", tc.Name, string(argsJSON)),
			})

			obs := ""
			tool, ok := a.registry.Get(tc.Name)
			if !ok {
				obs = fmt.Sprintf("错误：未知工具 %q", tc.Name)
			} else {
				out, err := tool.Run(ctx, tc.Args)
				if err != nil {
					obs = fmt.Sprintf("执行失败: %v", err)
				} else {
					obs = out
				}
			}
			steps = append(steps, StepTrace{Step: step, Role: "observe", Content: obs})
			messages = append(messages, Message{Role: "tool", ToolCallID: tc.ID, Content: obs})
		}
	}

	if !finished {
		log.Printf("[agent] 达到 maxSteps=%d 上限，终止循环", a.maxSteps)
	}
	return RunResult{Steps: steps, Actions: actions}, nil
}
