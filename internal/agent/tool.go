// Package agent 提供轻量 ReAct 推理代理：LLM + function-calling 工具 + 多步循环 + 推理轨迹。
//
// 设计目标：让 Auto-Pilot 的 LLM 引擎从"单轮 JSON 策略函数"升级为真正的 agent——
// LLM 在循环中思考(think)→调用工具(act)→读取观测(observe)→继续或收手，
// 全程产出可调试的 StepTrace。本包不依赖 autopilot，可独立复用与测试。
package agent

import "context"

// Tool 工具抽象：agent 通过 function-calling 调用的动作。
type Tool interface {
	// Name 工具名（即 OpenAI function name，需唯一）。
	Name() string
	// Description 给 LLM 的工具说明。
	Description() string
	// Parameters OpenAI function parameters 的 JSON Schema（map 形式）。
	Parameters() map[string]any
	// Run 执行工具，args 为 LLM 给出的参数；返回观测文本（回灌 LLM）。
	Run(ctx context.Context, args map[string]any) (string, error)
}

// ToolRegistry 工具注册表（保持注册顺序，便于稳定的工具定义输出）。
type ToolRegistry struct {
	order []string
	tools map[string]Tool
}

// NewRegistry 创建注册表。
func NewRegistry(tools ...Tool) *ToolRegistry {
	r := &ToolRegistry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

// Register 注册/覆盖一个工具。
func (r *ToolRegistry) Register(t Tool) {
	if _, exists := r.tools[t.Name()]; !exists {
		r.order = append(r.order, t.Name())
	}
	r.tools[t.Name()] = t
}

// Get 取工具。
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List 按注册顺序返回全部工具。
func (r *ToolRegistry) List() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, n := range r.order {
		if t, ok := r.tools[n]; ok {
			out = append(out, t)
		}
	}
	return out
}

// OpenAITools 转 OpenAI chat.completions 的 tools 字段格式（type=function）。
func (r *ToolRegistry) OpenAITools() []map[string]any {
	list := r.List()
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		})
	}
	return out
}
