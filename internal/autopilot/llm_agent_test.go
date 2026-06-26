package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nslmcrs/gateway/internal/agent"
)

// fakeLLMBackend 可编排的 LLM 后端桩：按顺序返回预设响应，模拟 ReAct 多步。
type fakeLLMBackend struct {
	mode      string
	responses []agent.ChatResponse
	calls     int
}

func (f *fakeLLMBackend) Mode() string { return f.mode }
func (f *fakeLLMBackend) Chat(_ context.Context, _ []agent.Message, _ []map[string]any) (agent.ChatResponse, error) {
	if f.calls >= len(f.responses) {
		return agent.ChatResponse{}, nil // 超出预设即收手
	}
	r := f.responses[f.calls]
	f.calls++
	return r, nil
}

// TestHTTPBackend_ChatWithTools 验证 HTTPBackend.Chat 解析 content 与 tool_calls，
// 并按 OpenAI 线格式发送 messages/tools（function.arguments 为 JSON 字符串）。
func TestHTTPBackend_ChatWithTools(t *testing.T) {
	// 模型返回带 tool_calls 的响应
	llmOut := `{"choices":[{"message":{"content":"降并发","tool_calls":[{"id":"call_1","type":"function","function":{"name":"set_concurrency","arguments":"{\"value\":4,\"reason\":\"成功率偏低\",\"confidence\":0.8}"}}]}}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("意外请求: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "meta/llama-3.1-8b-instruct" {
			t.Errorf("model=%v, want meta/llama-3.1-8b-instruct", body["model"])
		}
		// 应携带 tools 与 messages
		if _, ok := body["tools"]; !ok {
			t.Errorf("请求未携带 tools")
		}
		msgs, _ := body["messages"].([]any)
		if len(msgs) < 2 {
			t.Errorf("messages 应含 system+user，得到 %d 条", len(msgs))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(llmOut))
	}))
	defer srv.Close()

	b := &agent.HTTPBackend{
		BaseURL: strings.TrimSuffix(srv.URL, "/"),
		APIKey:  "sk-test",
		Model:   "meta/llama-3.1-8b-instruct",
		Client:  srv.Client(),
	}
	tools := []map[string]any{{"type": "function", "function": map[string]any{"name": "set_concurrency"}}}
	resp, err := b.Chat(context.Background(), []agent.Message{
		{Role: "system", Content: "你是调度器"},
		{Role: "user", Content: "现状..."},
	}, tools)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "降并发" {
		t.Errorf("Content=%q, want 降并发", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("应解析 1 个 tool_call, 得到 %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "set_concurrency" {
		t.Errorf("tool_call=%+v, want call_1/set_concurrency", tc)
	}
	if v, _ := tc.Args["value"].(float64); v != 4 {
		t.Errorf("args.value=%v, want 4", tc.Args["value"])
	}
	if b.Mode() != "gateway" {
		t.Errorf("Mode=%v, want gateway", b.Mode())
	}
}

// TestLLMEngine_AgentLoopDecides 端到端：LLMEngine 用 fake 后端跑 ReAct 循环，
// 第一轮返回 set_weight_boost 工具调用，第二轮收手；应产出 1 条动作 + 推理轨迹。
func TestLLMEngine_AgentLoopDecides(t *testing.T) {
	backend := &fakeLLMBackend{
		mode: "gateway",
		responses: []agent.ChatResponse{
			{Content: "密钥2健康差，降权", ToolCalls: []agent.ToolCall{
				{ID: "call_1", Name: "set_weight_boost", Args: map[string]any{
					"key_id": float64(2), "value": 0.5, "reason": "该密钥健康差", "confidence": 0.75,
				}},
			}},
			{Content: "已完成调度建议", ToolCalls: nil}, // 收手
		},
	}
	eng := NewLLMEngine(backend)
	snap := Snapshot{
		Keys:              []KeySnap{{ID: 2, Mask: "nvapi-***", Enabled: true, SuccessRate: 0.4, ConsecFail: 6}},
		DefaultConcurrency: 5,
		MaxConcurrency:     10,
	}
	actions, err := eng.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("应产出 1 条动作, 得到 %d", len(actions))
	}
	a := actions[0]
	if a.Kind != ActSetWeightBoost || a.KeyID != 2 || a.Value != 0.5 {
		t.Errorf("动作=%+v, want set_weight_boost key=2 value=0.5", a)
	}
	if a.Source != EngineLLM {
		t.Errorf("Source=%v, want llm", a.Source)
	}
	// 推理轨迹应含 think/act/observe
	trace := eng.LastTrace()
	if len(trace) < 3 {
		t.Fatalf("轨迹应含 think/act/observe，得到 %d 步", len(trace))
	}
	if eng.BackendMode() != "gateway" {
		t.Errorf("BackendMode=%v, want gateway", eng.BackendMode())
	}
}

// TestLLMEngine_StubFallback nil 后端走 stub 降级，BackendMode=stub，轨迹标注降级。
func TestLLMEngine_StubFallback(t *testing.T) {
	eng := NewLLMEngine(nil) // nil → stub
	if eng.BackendMode() != "stub" {
		t.Fatalf("BackendMode=%v, want stub", eng.BackendMode())
	}
	snap := makeSnap(0.2, 6, 5, 5, 10) // 明显故障
	acts, err := eng.Decide(context.Background(), snap)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(acts) == 0 {
		t.Fatalf("stub 降级在明显故障下应产出动作")
	}
	trace := eng.LastTrace()
	if len(trace) != 1 || !strings.Contains(trace[0].Content, "stub") {
		t.Fatalf("stub 轨迹应标注降级，得到 %+v", trace)
	}
}
