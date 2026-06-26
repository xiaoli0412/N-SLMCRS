package agent

// StepTrace 单步推理轨迹（调试/可观测用，前端"推理链"展示）。
type StepTrace struct {
	Step     int    `json:"Step"`               // 步序号（从 1，每轮 +1）
	Role     string `json:"Role"`               // think|act|observe
	Content  string `json:"Content"`            // think=LLM 文本；act=调用描述；observe=工具返回
	ToolName string `json:"ToolName,omitempty"` // act 时工具名
	ToolArgs string `json:"ToolArgs,omitempty"` // act 时参数 JSON
	Error    string `json:"Error,omitempty"`    // 出错时附错误
}
