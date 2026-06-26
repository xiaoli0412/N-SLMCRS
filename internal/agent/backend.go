package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message 对话消息（内部表示；HTTPBackend.Chat 负责转为 OpenAI 线格式）。
type Message struct {
	Role       string     // system|user|assistant|tool
	Content    string     // 文本部分
	ToolCalls  []ToolCall // role=assistant 时可能携带
	ToolCallID string     // role=tool 时回填对应 tool_call id
}

// ToolCall LLM 发起的工具调用。
type ToolCall struct {
	ID   string         // 工具调用 id（回填 role=tool 消息时用）
	Name string         // 工具名
	Args map[string]any // 参数
}

// ChatResponse 一次 LLM 回复。
type ChatResponse struct {
	Content   string     // 文本部分
	ToolCalls []ToolCall // 工具调用（可能为空；空表示 LLM 决定收手）
}

// LLMBackend LLM 后端抽象（支持 function-calling）。
type LLMBackend interface {
	// Chat 发起一次对话。tools 为 OpenAI 格式工具定义（可空）。
	Chat(ctx context.Context, messages []Message, tools []map[string]any) (ChatResponse, error)
	// Mode 后端模式：stub|gateway（供可观测，避免误以为"AI 在工作"而实为 stub）。
	Mode() string
}

// HTTPBackend 通过 OpenAI 兼容 /chat/completions 调用真实 LLM（支持 tools/tool_calls）。
type HTTPBackend struct {
	BaseURL string // 如 http://localhost:8787/v1（不含末尾斜杠）
	APIKey  string // 下游凭证
	Model   string // 目标模型
	Client  *http.Client
}

// NewHTTPBackend 构造后端；任一为空返回 nil（调用方走 stub 降级）。
// 返回接口类型以避免"nil 具体指针装入非 nil 接口"的陷阱。
func NewHTTPBackend(base, key, model string) LLMBackend {
	if base == "" || key == "" || model == "" {
		return nil
	}
	return &HTTPBackend{BaseURL: base, APIKey: key, Model: model, Client: &http.Client{Timeout: 30 * time.Second}}
}

// Mode 后端模式。
func (b *HTTPBackend) Mode() string { return "gateway" }

// Chat 调 /chat/completions，解析 content 与 tool_calls。
func (b *HTTPBackend) Chat(ctx context.Context, messages []Message, tools []map[string]any) (ChatResponse, error) {
	// 内部 Message → OpenAI 线格式（assistant.tool_calls 用 function.arguments=JSON 字符串）
	reqMessages := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		mm := map[string]any{"role": m.Role}
		if m.Content != "" {
			mm["content"] = m.Content
		}
		if m.ToolCallID != "" {
			mm["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Args)
				tcs = append(tcs, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":       tc.Name,
						"arguments":  string(argsJSON),
					},
				})
			}
			mm["tool_calls"] = tcs
		}
		reqMessages = append(reqMessages, mm)
	}

	body := map[string]any{
		"model":       b.Model,
		"temperature": 0.2,
		"messages":    reqMessages,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("编码请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.APIKey)

	resp, err := b.Client.Do(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("调用网关: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("网关返回 HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var cc struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				ToolCalls []struct {
					ID   string `json:"id"`
					Type string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"` // JSON 字符串
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &cc); err != nil {
		return ChatResponse{}, fmt.Errorf("解析响应: %w", err)
	}
	if len(cc.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("网关返回空 choices")
	}
	msg := cc.Choices[0].Message
	out := ChatResponse{Content: msg.Content}
	for _, tc := range msg.ToolCalls {
		args := map[string]any{}
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: args})
	}
	return out, nil
}

// truncate 截断字符串到 n 字符（错误信息用）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
