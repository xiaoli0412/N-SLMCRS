package modelcatalog

import "testing"

func TestClassify(t *testing.T) {
	cases := map[string]string{
		// chat（默认）
		"meta/llama-3.1-8b-instruct":      CapChat,
		"meta/llama-3.3-70b-instruct":     CapChat,
		"qwen/qwen3.5-122b-a10b":          CapChat,
		"mistralai/mistral-large-2-instruct": CapChat,
		"deepseek-ai/deepseek-v4-flash":   CapChat,
		"openai/gpt-oss-120b":             CapChat,
		"01-ai/yi-large":                  CapChat,

		// embedding（必须在 rerank 之前命中：检索族嵌入）
		"nvidia/nv-embed-v1":                            CapEmbedding,
		"nvidia/llama-3.2-nemoretriever-1b-vlm-embed-v1": CapEmbedding,
		"nvidia/nv-embedcode-7b-v1":                     CapEmbedding,
		"baai/bge-m3":                                   CapEmbedding,
		"snowflake/arctic-embed-l":                       CapEmbedding,
		"nvidia/llama-nemotron-embed-vl-1b-v2":          CapEmbedding,

		// rerank（bare nemoretriever 归 rerank）
		"nvidia/llama-3.2-nemoretriever-500m-rerank-v2": CapRerank,

		// vision
		"meta/llama-3.2-11b-vision-instruct": CapVision,
		"meta/llama-3.2-90b-vision-instruct": CapVision,
		"nvidia/vila":        CapVision,
		"nvidia/nvclip":      CapVision,
		"adept/fuyu-8b":      CapVision,
		"microsoft/kosmos-2": CapVision,
		"google/deplot":      CapVision,
		"nvidia/nemotron-nano-12b-v2-vl": CapVision,
		"nvidia/neva-22b": CapVision,
		"microsoft/phi-4-multimodal-instruct": CapVision,

		// reasoning（仅命名明确含 reasoning 关键词者；策展覆盖的 qwen3-next /
		// nemotron-nano 由 Enrich 经 catalog 返回 reasoning，见 TestEnrich）
		"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning": CapReasoning,

		// code
		"meta/codellama-70b":               CapCode,
		"deepseek-ai/deepseek-coder-6.7b-instruct": CapCode,
		"mistralai/codestral-22b-instruct-v0.1": CapCode,
		"bigcode/starcoder2-15b":           CapCode,
		"google/codegemma-7b":              CapCode,

		// safety / guard / pii / detector
		"nvidia/llama-3.1-nemoguard-8b-content-safety": CapSafety,
		"meta/llama-guard-4-12b":        CapSafety,
		"nvidia/nemotron-3-content-safety": CapSafety,
		"nvidia/gliner-pii":             CapSafety,
		"nvidia/ai-synthetic-video-detector": CapSafety,

		// reward
		"nvidia/nemotron-4-340b-reward": CapReward,

		// translation
		"nvidia/riva-translate-4b-instruct":     CapTranslation,
		"nvidia/riva-translate-4b-instruct-v1.1": CapTranslation,

		// parsing（nemoretriever-parse / nemotron-parse）
		"nvidia/nemotron-parse":      CapParsing,
		"nvidia/nemoretriever-parse": CapParsing,
	}
	for id, want := range cases {
		got := Classify(id)
		if got != want {
			t.Errorf("Classify(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestIsChatCapable(t *testing.T) {
	chat := []string{CapChat, CapReasoning, CapCode, CapVision, ""}
	nonChat := []string{CapEmbedding, CapRerank, CapSafety, CapReward, CapTranslation, CapParsing}
	for _, c := range chat {
		if !IsChatCapable(c) {
			t.Errorf("IsChatCapable(%q) = false, want true", c)
		}
	}
	for _, c := range nonChat {
		if IsChatCapable(c) {
			t.Errorf("IsChatCapable(%q) = true, want false", c)
		}
	}
}
