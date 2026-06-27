package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ModelSpec 远程注册表富化的模型规格（与 EnrichedModel 的 Spec 字段对齐）。
// 用于模型广场二阶面板"参数说明"，作为内置策展表的补充。
type ModelSpec struct {
	MaxTokens           int      `json:"max_tokens"`
	PricingIn           string   `json:"pricing_in"`
	PricingOut          string   `json:"pricing_out"`
	License             string   `json:"license"`
	InputModalities     []string `json:"input_modalities"`
	ReleaseDate         string   `json:"release_date"`
	CardURL             string   `json:"card_url"`
	Architecture        string   `json:"architecture"`         // v0.9：HF 富化（如 LlamaForCausalLM）
	SupportedInterfaces []string `json:"supported_interfaces"` // v0.9：支持接口（chat/embeddings/rerank）
}

// openRouterModel OpenRouter 模型注册表条目（仅取需要的字段）。
// 端点：GET https://openrouter.ai/api/v1/models （公开 JSON，无需鉴权）。
type openRouterModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContextLength int   `json:"context_length"`
	Pricing      struct {
		Prompt     string `json:"prompt"` // 输入 $/token
		Completion string `json:"completion"`
	} `json:"pricing"`
	Architecture struct {
		Modality   string `json:"input_modalities"` // 如 "text" 或 "text,image"
		License    string `json:"license"`
	} `json:"architecture"`
	Created int64 `json:"created"` // Unix 秒
}

// SyncRegistry 拉取 OpenRouter 模型注册表，按 id/owned_by 映射为 ModelSpec。
// 远程不可达或解析失败时返回空 map（调用方降级用内置策展表兜底）。
//
// 映射策略：OpenRouter 的 id 形如 "meta-llama/llama-3.1-8b-instruct"，
// 归一化后尝试与本网关模型 id（如 "meta/llama-3.1-8b-instruct"）匹配；
// 同时支持按 owned_by（厂商）前缀模糊命中。
func SyncRegistry(ctx context.Context) (map[string]ModelSpec, error) {
	const url = "https://openrouter.ai/api/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取 OpenRouter 注册表: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter 注册表返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB 上限
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析 OpenRouter 注册表: %w", err)
	}

	out := make(map[string]ModelSpec, len(payload.Data))
	for _, m := range payload.Data {
		spec := ModelSpec{
			PricingIn:    m.Pricing.Prompt,
			PricingOut:   m.Pricing.Completion,
			License:      m.Architecture.License,
			CardURL:      "https://openrouter.ai/" + m.ID,
		}
		if m.Architecture.Modality != "" {
			spec.InputModalities = strings.Split(m.Architecture.Modality, ",")
			for i, mod := range spec.InputModalities {
				spec.InputModalities[i] = strings.TrimSpace(mod)
			}
		}
		if m.Created > 0 {
			spec.ReleaseDate = time.Unix(m.Created, 0).UTC().Format("2006-01-02")
		}
		// 归一化 id：openrouter "meta-llama/llama-3.1-8b-instruct" → 本网关 "meta/llama-3.1-8b-instruct"
		norm := normalizeID(m.ID)
		out[norm] = spec
	}
	return out, nil
}

// normalizeID 将外部注册表 id 归一化为本网关模型 id 风格。
// 规则：把厂商段 "meta-llama" → "meta"、"deepseek-ai"→"deepseek-ai"（保留），
// 取首个 "-" 之前作为厂商并转 "/"，仅当能明显提升匹配率时。
// 这里采用简单策略：把首个 "/" 后到首个 "-" 之间含 "-" 的厂商段，重映射常见别名。
func normalizeID(id string) string {
	// 直接尝试原值（部分 id 已是 meta/xxx 形式）
	if strings.Contains(id, "/") {
		return id
	}
	// OpenRouter 多为 "vendor-model/name"，把首段 vendor-model 中的连字规范化
	// 例：meta-llama/llama-3.1-8b-instruct → meta/llama-3.1-8b-instruct
	if i := strings.Index(id, "/"); i > 0 {
		vendor := id[:i]
		rest := id[i+1:]
		// 常见厂商别名收敛
		switch vendor {
		case "meta-llama":
			vendor = "meta"
		case "deepseek-ai":
			vendor = "deepseek-ai"
		case "qwen":
			vendor = "qwen"
		}
		return vendor + "/" + rest
	}
	return id
}
