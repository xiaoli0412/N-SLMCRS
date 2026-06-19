// Package protocol 提供客户端协议适配：在 OpenAI（网关内部基准格式）与
// Anthropic(Claude) / Google(Gemini) 之间做请求与响应的双向翻译。
//
// 设计：
//   - 网关内部统一以 OpenAI Chat Completions 格式与上游 NVIDIA 通信
//     （NVIDIA NIM 原生 OpenAI 兼容）。
//   - Claude / Gemini 端点收到请求后，先翻译为 OpenAI 格式 → 经调度器转发 →
//     再把 OpenAI 响应翻译回对应协议返回给客户端。
//   - 翻译器是无状态纯函数，便于测试与扩展。
//
// 覆盖范围（Phase 2）：
//   - Anthropic：messages / system / max_tokens / stream / temperature / stop
//   - Gemini：contents(parts) / systemInstruction / generationConfig / stream
package protocol
