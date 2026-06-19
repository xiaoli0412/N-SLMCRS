// Gemini / Google 协议适配（/v1beta/models/:model:generateContent）。
//
// Gemini generateContent ↔ OpenAI Chat Completions 翻译。
// 字段映射要点：
//   - systemInstruction → OpenAI 首条 system 消息
//   - contents[]：每项的 parts[].text 拼接为一条消息内容；role model→assistant，余→user
//   - generationConfig.maxOutputTokens → max_tokens
//   - generationConfig.temperature / topP / stopSequences → 同名
//   - 响应：candidates[0].content.parts[].text 拼接 → OpenAI content
package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ─── Gemini 请求 ───

// GeminiRequest /v1beta/models/:model:generateContent 入站请求。
type GeminiRequest struct {
	Contents          []GeminiContent    `json:"contents"`
	SystemInstruction *GeminiContent      `json:"systemInstruction,omitempty"`
	GenerationConfig  *GeminiGenConfig    `json:"generationConfig,omitempty"`
	// model 从 URL 路径解析，不在 body 内
}

// GeminiContent Gemini content（含 role 与 parts）。
type GeminiContent struct {
	Role  string       `json:"role,omitempty"` // "user" / "model"
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart Gemini part（仅翻译 text，inline_data 等留待 Phase 3）。
type GeminiPart struct {
	Text string `json:"text,omitempty"`
}

// GeminiGenConfig Gemini 生成配置。
type GeminiGenConfig struct {
	MaxOutputTokens int       `json:"maxOutputTokens,omitempty"`
	Temperature     *float64  `json:"temperature,omitempty"`
	TopP            *float64  `json:"topP,omitempty"`
	TopK            *int      `json:"topK,omitempty"`
	StopSequences   []string  `json:"stopSequences,omitempty"`
}

// ToOpenAIChatRequest 将 Gemini 请求翻译为 OpenAI Chat Completions 请求体（字节）。
// model 参数从 URL 路径解析后注入。
func (g *GeminiRequest) ToOpenAIChatRequest(model string) ([]byte, error) {
	msgs := make([]openAIMessage, 0, len(g.Contents)+1)

	// systemInstruction → 首条 system 消息
	if g.SystemInstruction != nil {
		if sys := joinGeminiParts(g.SystemInstruction.Parts); sys != "" {
			msgs = append(msgs, openAIMessage{Role: "system", Content: sys})
		}
	}

	// contents → OpenAI messages
	for _, c := range g.Contents {
		role := "user"
		if c.Role == "model" {
			role = "assistant"
		}
		text := joinGeminiParts(c.Parts)
		msgs = append(msgs, openAIMessage{Role: role, Content: text})
	}

	req := openAIChatRequest{
		Model:    model,
		Messages: msgs,
	}
	if g.GenerationConfig != nil {
		req.MaxTokens = g.GenerationConfig.MaxOutputTokens
		req.Temperature = g.GenerationConfig.Temperature
		req.TopP = g.GenerationConfig.TopP
		if len(g.GenerationConfig.StopSequences) > 0 {
			req.Stop = g.GenerationConfig.StopSequences
		}
	}
	return json.Marshal(req)
}

// joinGeminiParts 把 parts 的 text 拼接。
func joinGeminiParts(parts []GeminiPart) string {
	var sb strings.Builder
	for i, p := range parts {
		if p.Text == "" {
			continue
		}
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(p.Text)
	}
	return sb.String()
}

// ─── Gemini 响应 ───

// GeminiResponse generateContent 出站响应。
type GeminiResponse struct {
	Candidates     []GeminiCandidate `json:"candidates"`
	PromptFeedback *GeminiPromptFeedback `json:"promptFeedback,omitempty"`
	UsageMetadata  *GeminiUsage      `json:"usageMetadata,omitempty"`
}

// GeminiCandidate 候选项。
type GeminiCandidate struct {
	Content       GeminiContent  `json:"content"`
	FinishReason  string         `json:"finishReason,omitempty"`
	Index         int            `json:"index"`
}

// GeminiPromptFeedback 提示反馈。
type GeminiPromptFeedback struct {
	BlockReason string `json:"blockReason,omitempty"`
}

// GeminiUsage 用量。
type GeminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// OpenAIToGeminiResponse 把 OpenAI Chat 响应翻译回 Gemini 格式。
func OpenAIToGeminiResponse(openaiBody []byte) ([]byte, error) {
	var r openAIChatResponse
	if err := json.Unmarshal(openaiBody, &r); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 响应: %w", err)
	}

	content := ""
	finish := "STOP"
	if len(r.Choices) > 0 {
		content = r.Choices[0].Message.Content
		finish = mapGeminiFinishReason(r.Choices[0].FinishReason)
	}

	resp := GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{{Text: content}},
				},
				FinishReason: finish,
				Index:        0,
			},
		},
	}
	if r.Usage.TotalTokens > 0 {
		resp.UsageMetadata = &GeminiUsage{
			PromptTokenCount:     r.Usage.PromptTokens,
			CandidatesTokenCount: r.Usage.CompletionTokens,
			TotalTokenCount:      r.Usage.TotalTokens,
		}
	}
	return json.Marshal(resp)
}

// mapGeminiFinishReason OpenAI finish_reason → Gemini finishReason。
func mapGeminiFinishReason(finish string) string {
	switch finish {
	case "stop":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	case "tool_calls", "function_call":
		return "STOP"
	default:
		return "STOP"
	}
}
