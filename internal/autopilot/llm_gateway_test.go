package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeLLMResponse 构造一个 OpenAI 兼容的 chat completion 响应。
func fakeLLMResponse(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"content": content}},
		},
	})
	return string(b)
}

func TestGatewayBackend_GenerateAndParse(t *testing.T) {
	// 模型返回的 JSON 决策（带 ```json 围栏，覆盖 parseLLMActions 的去围栏逻辑）
	llmOut := "```json\n" + `{"actions":[{"kind":"set_concurrency","value":4,"reason":"成功率偏低","confidence":0.8}],"rationale":"降并发观察"}` + "\n```"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("意外请求: %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		// 校验请求体携带了 model 与 messages
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "meta/llama-3.1-8b-instruct" {
			t.Errorf("model=%v, want meta/llama-3.1-8b-instruct", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeLLMResponse(llmOut)))
	}))
	defer srv.Close()

	// 构造 gatewayBackend 指向测试服务器（baseURL 去掉末尾斜杠）
	gb := &gatewayBackend{
		baseURL: strings.TrimSuffix(srv.URL, "/"),
		apiKey:  "sk-test",
		model:   "meta/llama-3.1-8b-instruct",
		client:  srv.Client(),
	}

	out, err := gb.Generate(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(out, "set_concurrency") {
		t.Fatalf("Generate 输出未包含动作, got %q", out)
	}

	// 验证 parseLLMActions 能解析（含去围栏）
	snap := Snapshot{DefaultConcurrency: 5, MaxConcurrency: 10}
	actions, err := parseLLMActions(out, snap)
	if err != nil {
		t.Fatalf("parseLLMActions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("应解析出 1 条动作, 得到 %d", len(actions))
	}
	if actions[0].Kind != ActSetConcurrency || actions[0].Value != 4 {
		t.Errorf("动作=%+v, want set_concurrency value=4", actions[0])
	}
	if actions[0].Confidence != 0.8 {
		t.Errorf("Confidence=%v, want 0.8", actions[0].Confidence)
	}
}

// TestLLMEngine_RealBackendDecide 端到端：LLMEngine 用 gatewayBackend → Decide 产出动作。
func TestLLMEngine_RealBackendDecide(t *testing.T) {
	llmOut := `{"actions":[{"kind":"set_weight_boost","key_id":2,"value":0.5,"reason":"该密钥健康差","confidence":0.75}],"rationale":"降权"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeLLMResponse(llmOut)))
	}))
	defer srv.Close()

	gb := &gatewayBackend{
		baseURL: strings.TrimSuffix(srv.URL, "/"),
		apiKey:  "sk-test",
		model:   "meta/llama-3.1-8b-instruct",
		client:  srv.Client(),
	}
	eng := NewLLMEngine(gb)

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
	if actions[0].Kind != ActSetWeightBoost || actions[0].KeyID != 2 || actions[0].Value != 0.5 {
		t.Errorf("动作=%+v, want set_weight_boost key=2 value=0.5", actions[0])
	}
	if actions[0].Source != EngineLLM {
		t.Errorf("Source=%v, want llm", actions[0].Source)
	}
}
