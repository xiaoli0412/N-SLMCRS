// Package modelcatalog 提供模型富元数据的推导：能力分类、参数量解析、
// 策展目录与统一入口 Enrich。
//
// NVIDIA /v1/models 仅返回 {id, object, created, owned_by}，不含能力类型 /
// 上下文长度 / 参数量。本包通过「策展表精确命中 + 命名启发式兜底」补齐这些
// 信息，供 modelmeta 同步器写入 models 表，并在前端模型广场展示。
package modelcatalog

import "strings"

// 能力类型常量。capability 取值之一。
const (
	CapChat        = "chat"        // /v1/chat/completions 对话补全
	CapReasoning   = "reasoning"   // 推理类（思维链 / 深度思考）
	CapCode        = "code"        // 代码生成 / 补全
	CapVision      = "vision"      // 多模态视觉理解
	CapEmbedding   = "embedding"   // 向量嵌入
	CapRerank      = "rerank"      // 重排序
	CapSafety      = "safety"      // 内容安全 / 护栏 / PII
	CapReward      = "reward"      // 奖励模型 / 偏好对齐
	CapTranslation = "translation" // 翻译 / Riva
	CapParsing     = "parsing"     // 文档解析
)

// chatCapabilities 支持对话补全的能力集合（用于公开 /v1/models 默认过滤）。
// embedding/rerank/safety/reward/translation/parsing 不支持 /v1/chat/completions。
var chatCapabilities = map[string]bool{
	CapChat: true, CapReasoning: true, CapCode: true, CapVision: true,
}

// IsChatCapable 判断某能力是否支持 /v1/chat/completions（含多模态/代码/推理）。
func IsChatCapable(capability string) bool {
	if capability == "" {
		return true // 未分类视为可对话，避免误过滤
	}
	return chatCapabilities[capability]
}

// containsAny 判断 s 是否包含 tokens 中任一子串（大小写不敏感）。
func containsAny(s string, tokens ...string) bool {
	for _, t := range tokens {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

// Classify 按模型 ID 的命名约定推断能力类型。
//
// 优先级（高→低）经过对 NVIDIA 实际 121 个模型 ID 的校准：
//
//	parsing → safety/reward → translation → embedding → rerank
//	→ vision → reasoning → code → chat(默认)
//
// embed 在 rerank 之前：处理 nvidia/llama-3.2-nemoretriever-*-embed-v1
// 这类「检索族嵌入」模型；仅当不含 embed/parse 时，bare nemoretriever 才归 rerank。
func Classify(id string) string {
	s := strings.ToLower(id)

	// 1. 文档解析
	if containsAny(s, "-parse", "nemotron-parse", "nemoretriever-parse") {
		return CapParsing
	}
	// 2. 内容安全 / 护栏 / PII / 检测器
	if containsAny(s, "content-safety", "nemoguard", "guard", "gliner-pii", "pii", "detector") {
		return CapSafety
	}
	// 3. 奖励模型（在 safety 之后，reward 关键字独立）
	if strings.Contains(s, "reward") {
		return CapReward
	}
	// 4. 翻译（Riva）
	if containsAny(s, "riva-translate", "riva_transl", "-translate") {
		return CapTranslation
	}
	// 5. 嵌入（必须在 rerank 之前：捕捉 *-embed-* / bge- / arctic-embed / nv-embed）
	if containsAny(s, "embed", "bge-", "arctic-embed") {
		return CapEmbedding
	}
	// 6. 重排序（nemoretriever 检索族的 rerank 变体；embed/parse 已被上面捕获）
	if containsAny(s, "rerank", "reranker", "nemoretriever") {
		return CapRerank
	}
	// 7. 视觉多模态
	if containsAny(s, "vision", "-vl", "vila", "nvclip", "fuyu", "deplot", "kosmos", "multimodal", "neva") {
		return CapVision
	}
	// 8. 推理（思维链）
	if containsAny(s, "reasoning", "deepseek-r", "nano-omni", "-r1") {
		return CapReasoning
	}
	// 9. 代码
	if containsAny(s, "code", "codestral", "starcoder", "codegemma", "codellama") {
		return CapCode
	}
	// 10. 默认对话
	return CapChat
}
