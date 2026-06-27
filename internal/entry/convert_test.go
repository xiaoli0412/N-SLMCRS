package entry

import (
	"encoding/json"
	"testing"
)

// TestNormalizeChatRequest_MessagesString 验证 messages 为字符串时自动包装。
func TestNormalizeChatRequest_MessagesString(t *testing.T) {
	in := []byte(`{"model":"meta/llama-3.1-8b-instruct","messages":"hello"}`)
	out, changed, err := normalizeChatRequest(in)
	if err != nil || !changed {
		t.Fatalf("应改写：err=%v changed=%v", err, changed)
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msgs, ok := obj["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages 应为单元素数组，实为 %v", obj["messages"])
	}
}

// TestNormalizeChatRequest_PromptToMessages 验证缺 messages 但有 prompt 时转换。
func TestNormalizeChatRequest_PromptToMessages(t *testing.T) {
	in := []byte(`{"model":"meta/llama-3.1-8b-instruct","prompt":"hi","max_tokens":1}`)
	out, changed, err := normalizeChatRequest(in)
	if err != nil || !changed {
		t.Fatalf("应改写：err=%v changed=%v", err, changed)
	}
	var obj map[string]any
	_ = json.Unmarshal(out, &obj)
	if _, hasPrompt := obj["prompt"]; hasPrompt {
		t.Error("prompt 应被删除")
	}
	if _, hasMsgs := obj["messages"]; !hasMsgs {
		t.Error("应生成 messages")
	}
}

// TestMapModelAlias 验证 OpenAI 别名 → NVIDIA 映射；NVIDIA 格式与未知别名原样返回。
func TestMapModelAlias(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":                  "meta/llama-3.1-405b-instruct",
		"GPT-4":                   "meta/llama-3.1-405b-instruct",
		"gpt-3.5-turbo":           "meta/llama-3.1-8b-instruct",
		"meta/llama-3.1-8b-instruct": "meta/llama-3.1-8b-instruct", // 已是 NVIDIA 格式
		"unknown-model":           "unknown-model",                  // 未知别名原样
		"":                        "",
	}
	for in, want := range cases {
		if got := mapModelAlias(in); got != want {
			t.Errorf("mapModelAlias(%q)=%q, want %q", in, got, want)
		}
	}
}

// TestNormalizeChatRequest_NoChange 验证无需改写时返回原字节且 changed=false。
func TestNormalizeChatRequest_NoChange(t *testing.T) {
	in := []byte(`{"model":"meta/llama-3.1-8b-instruct","messages":[{"role":"user","content":"hi"}]}`)
	out, changed, err := normalizeChatRequest(in)
	if err != nil || changed {
		t.Fatalf("不应改写：err=%v changed=%v", err, changed)
	}
	if string(out) != string(in) {
		t.Errorf("应返回原字节")
	}
}
