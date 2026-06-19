// 共享的 OpenAI Chat Completions 内部类型（翻译基准格式）。
// 仅包含翻译所需的字段，避免与上游客户端的类型耦合。
package protocol

// openAIChatRequest OpenAI Chat Completions 请求。
type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Stop        any             `json:"stop,omitempty"`
}

// openAIMessage OpenAI 消息。
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIChatResponse OpenAI Chat Completions 响应。
type openAIChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openAIChoice     `json:"choices"`
	Usage   openAIUsage        `json:"usage"`
}

// openAIChoice 单个选项。
type openAIChoice struct {
	Index        int            `json:"index"`
	Message      openAIChoiceMsg `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

// openAIChoiceMsg 选择消息。
type openAIChoiceMsg struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Refusal  string `json:"refusal,omitempty"`
}

// openAIUsage OpenAI token 用量。
type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
