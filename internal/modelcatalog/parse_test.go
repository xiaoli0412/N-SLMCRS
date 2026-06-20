package modelcatalog

import "testing"

func TestParseParamCount(t *testing.T) {
	cases := map[string]string{
		"meta/llama-3.1-8b-instruct":          "8B",
		"meta/llama-3.1-70b-instruct":         "70B",
		"google/gemma-2-2b-it":                 "2B",
		"deepseek-ai/deepseek-coder-6.7b-instruct": "6.7B",
		"mistralai/mixtral-8x22b-v0.1":         "8×22B",
		"mistralai/mixtral-8x7b-instruct-v0.1": "8×7B",
		"nvidia/nemotron-3-ultra-550b-a55b":    "550B(55B)",
		"nvidia/nemotron-3-super-120b-a12b":     "120B(12B)",
		"nvidia/nemotron-3-nano-30b-a3b":        "30B(3B)",
		"nvidia/nv-embedqa-e5-v5":               "", // ID 内无参数 token，436M 来自策展表
		"qwen/qwen3-next-80b-a3b-instruct":      "80B(3B)",
		"bigcode/starcoder2-15b":               "15B",
		// 无可识别参数
		"nvidia/nemotron-parse": "",
		"nvidia/vila":          "",
	}
	for id, want := range cases {
		got := ParseParamCount(id)
		if got != want {
			t.Errorf("ParseParamCount(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestEnrich(t *testing.T) {
	// 策展命中：返回完整富元数据
	m := Enrich("meta/llama-3.1-8b-instruct")
	if m.Capability != CapChat || m.ContextLength != 131072 || m.ParamCount != "8B" || m.Description == "" {
		t.Errorf("enrich curated llama-3.1-8b unexpected: %+v", m)
	}
	// 策展命中 embedding（验证 embed 在 rerank 之前）
	e := Enrich("nvidia/llama-3.2-nemoretriever-1b-vlm-embed-v1")
	if e.Capability != CapEmbedding {
		t.Errorf("enrich embed-nemoretriever capability = %q, want embedding", e.Capability)
	}
	// 未命中：启发式分类 + 参数解析 + 默认描述
	u := Enrich("some-vendor/unknown-13b-chat-model")
	if u.Capability != CapChat || u.ParamCount != "13B" || u.ContextLength != 0 {
		t.Errorf("enrich unknown unexpected: %+v", u)
	}
	// 策展覆盖能力（命名无 reasoning 关键词，经 catalog 标记为 reasoning）
	for _, id := range []string{"qwen/qwen3-next-80b-a3b-instruct", "nvidia/nvidia-nemotron-nano-9b-v2"} {
		if got := Enrich(id).Capability; got != CapReasoning {
			t.Errorf("Enrich(%q).Capability = %q, want reasoning (catalog override)", id, got)
		}
	}
}
