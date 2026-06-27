// entry 包请求方式自动转换（v0.9）。
//
// 处理客户端常见的"请求方式错误"并尝试自动纠正，转换失败则交由调用方
// 返回双语说明。覆盖：
//   - messages 为字符串 → 包装为 [{role:"user", content:...}]
//   - /v1/chat/completions 收到 completions 风格 prompt 字段 → 转 messages
//   - OpenAI 别名模型名（gpt-4o/gpt-4/gpt-3.5-turbo 等）→ NVIDIA 等价
package entry

import (
	"encoding/json"
	"strings"

	"github.com/nslmcrs/gateway/internal/modelcatalog"
)

// openAIAlias OpenAI 公共模型别名 → NVIDIA 等价（命中即映射；未命中保持原值）。
var openAIAlias = map[string]string{
	"gpt-4o":         "meta/llama-3.1-405b-instruct",
	"gpt-4o-mini":    "meta/llama-3.1-8b-instruct",
	"gpt-4":          "meta/llama-3.1-405b-instruct",
	"gpt-4-turbo":    "meta/llama-3.1-405b-instruct",
	"gpt-3.5-turbo":  "meta/llama-3.1-8b-instruct",
	"gpt-3.5":        "meta/llama-3.1-8b-instruct",
	"text-davinci-003": "meta/llama-3.1-70b-instruct",
}

// mapModelAlias 将 OpenAI 公共别名映射为 NVIDIA 等价模型。
// 已是 NVIDIA 格式（含 "/"）或非别名则原样返回。
func mapModelAlias(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	if strings.Contains(name, "/") {
		return name // 已是 NVIDIA 格式
	}
	if m, ok := openAIAlias[strings.ToLower(name)]; ok {
		return m
	}
	return name
}

// normalizeChatRequest 纠正常见的请求体形状问题，返回规整后的请求体字节与是否改写。
// 处理：messages 为字符串、缺 messages 但有 prompt、模型别名映射。
func normalizeChatRequest(raw []byte) ([]byte, bool, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, false, err
	}
	changed := false

	// 模型别名映射
	if m, ok := obj["model"].(string); ok {
		if mapped := mapModelAlias(m); mapped != m {
			obj["model"] = mapped
			changed = true
		}
	}

	// messages 为字符串 → 包装为数组
	if msg, ok := obj["messages"].(string); ok && msg != "" {
		obj["messages"] = []map[string]string{{"role": "user", "content": msg}}
		changed = true
	}

	// 缺 messages 但有 prompt（completions 风格误打到 chat 端点）→ 转 messages
	if _, hasMsgs := obj["messages"]; !hasMsgs {
		if prompt, ok := obj["prompt"].(string); ok && prompt != "" {
			obj["messages"] = []map[string]string{{"role": "user", "content": prompt}}
			delete(obj, "prompt")
			changed = true
		}
	}

	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, false, err
	}
	return out, true, nil
}

// endpointForCapability 模型能力 → 正确端点路径（用于能力/端点错配提示）。
// 返回空串表示该能力无独立端点（不应转换）。
func endpointForCapability(capability string) string {
	switch capability {
	case modelcatalog.CapEmbedding:
		return "/v1/embeddings"
	case modelcatalog.CapRerank:
		return "/v1/ranking"
	default:
		return "/v1/chat/completions"
	}
}
