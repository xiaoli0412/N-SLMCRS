// Claude / Anthropic 协议适配（/v1/messages）。
//
// Anthropic Messages API ↔ OpenAI Chat Completions 翻译。
// 字段映射要点：
//   - system：Anthropic 顶层 system 字段 → OpenAI 首条 system 消息
//   - messages：content 可为字符串或 content blocks 数组；OpenAI 统一为 string
//     （仅翻译 text 块；image/tool_use 等在 Phase 3 扩展）
//   - max_tokens：Anthropic 必填 → OpenAI max_tokens
//   - stop_sequences → stop
//   - 响应：Anthropic 的 content blocks → OpenAI choices[0].message.content
package protocol

import (
	"encoding/json"
	"fmt"
)

// ─── Anthropic 请求 ───

// AnthropicRequest /v1/messages 入站请求。
type AnthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        json.RawMessage    `json:"system,omitempty"`     // 可能是 string 或 array
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
}

// AnthropicMessage Anthropic 消息（content 可为字符串或 blocks 数组）。
type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// AnthropicContentBlock content block（text/tool_use/tool_result）。
type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToOpenAIChatRequest 将 Anthropic 请求翻译为 OpenAI Chat Completions 请求体（字节）。
func (a *AnthropicRequest) ToOpenAIChatRequest() ([]byte, error) {
	msgs := make([]openAIMessage, 0, len(a.Messages)+1)

	// system → 首条 system 消息
	if sys := parseAnthropicSystem(a.System); sys != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: sys})
	}

	// 翻译每条消息
	for _, m := range a.Messages {
		role := m.Role
		if role == "" {
			role = "user"
		}
		text := parseAnthropicContent(m.Content)
		msgs = append(msgs, openAIMessage{Role: role, Content: text})
	}

	req := openAIChatRequest{
		Model:       a.Model,
		Messages:    msgs,
		MaxTokens:   a.MaxTokens,
		Temperature: a.Temperature,
		TopP:        a.TopP,
		Stream:      a.Stream,
	}
	if len(a.StopSequences) > 0 {
		req.Stop = a.StopSequences
	}
	return json.Marshal(req)
}

// parseAnthropicSystem 解析 system 字段（string 或 [{type,text}]）。
func parseAnthropicSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// 先尝试字符串
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// 再尝试 blocks 数组
	return joinBlocks(raw)
}

// parseAnthropicContent 解析 content（string 或 blocks 数组）为纯文本。
func parseAnthropicContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return joinBlocks(raw)
}

// joinBlocks 把 content blocks 数组中的 text 块拼接。
func joinBlocks(raw json.RawMessage) string {
	var blocks []AnthropicContentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	out := ""
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if out != "" {
				out += "\n"
			}
			out += b.Text
		}
	}
	return out
}

// ─── Anthropic 响应 ───

// AnthropicResponse /v1/messages 出站响应。
type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"` // "message"
	Role         string                  `json:"role"` // "assistant"
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   *string                 `json:"stop_reason,omitempty"`
	StopSequence *string                 `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

// AnthropicUsage Anthropic token 用量。
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// OpenAIToAnthropicResponse 把 OpenAI Chat 响应翻译回 Anthropic 格式。
func OpenAIToAnthropicResponse(openaiBody []byte, model string) ([]byte, error) {
	var r openAIChatResponse
	if err := json.Unmarshal(openaiBody, &r); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 响应: %w", err)
	}

	content := "（空响应）"
	if len(r.Choices) > 0 {
		content = r.Choices[0].Message.Content
		if content == "" && len(r.Choices[0].Message.Refusal) > 0 {
			content = r.Choices[0].Message.Refusal
		}
	}

	var stopReason *string
	if len(r.Choices) > 0 {
		sr := mapAnthropicStopReason(r.Choices[0].FinishReason)
		stopReason = &sr
	}

	resp := AnthropicResponse{
		ID:   r.ID,
		Type: "message",
		Role: "assistant",
		Content: []AnthropicContentBlock{
			{Type: "text", Text: content},
		},
		Model:      orDefault(model, r.Model),
		StopReason: stopReason,
		Usage: AnthropicUsage{
			InputTokens:  r.Usage.PromptTokens,
			OutputTokens: r.Usage.CompletionTokens,
		},
	}
	return json.Marshal(resp)
}

// mapAnthropicStopReason OpenAI finish_reason → Anthropic stop_reason。
func mapAnthropicStopReason(finish string) string {
	switch finish {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "end_turn"
	default:
		return "end_turn"
	}
}

func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
