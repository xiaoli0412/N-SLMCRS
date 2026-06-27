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

// hfModelCard HuggingFace 模型卡 API 响应（仅取需要的字段）。
// 端点：GET https://huggingface.co/api/models/{owner}/{name} （公开，无需鉴权）。
type hfModelCard struct {
	ID            string `json:"_id"`
	Downloads     int64  `json:"downloads"`
	Likes         int    `json:"likes"`
	LibraryName   string `json:"library_name"`
	PipelineTag   string `json:"pipeline_tag"`
	License       string `json:"cardData,omitempty"`
	Config        struct {
		Architectures string `json:"architectures"`
		ModelType     string `json:"model_type"`
		MaxPosition   int    `json:"max_position_embeddings"`
		HiddenSize    int    `json:"hidden_size"`
		NumParams     float64 `json:"num_params"` // 单位：十亿（B）
	} `json:"config"`
	Tags []string `json:"tags"`
}

// FetchHuggingFace 拉取单个模型的 HuggingFace 模型卡，返回架构与 license 富化字段。
// model 形如 "meta/llama-3.1-8b-instruct"。远程不可达/未找到时返回空 Spec + nil。
func FetchHuggingFace(ctx context.Context, model string) (ModelSpec, error) {
	spec := ModelSpec{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://huggingface.co/api/models/"+model, nil)
	if err != nil {
		return spec, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return spec, nil // 降级：网络不可达不视为错误
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return spec, nil // 404 等不视为错误
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB
	if err != nil {
		return spec, nil
	}
	var card hfModelCard
	if err := json.Unmarshal(body, &card); err != nil {
		return spec, fmt.Errorf("解析 HF 模型卡 %s: %w", model, err)
	}
	// 架构：优先 architectures，回退 model_type
	if card.Config.Architectures != "" {
		spec.Architecture = card.Config.Architectures
	} else if card.Config.ModelType != "" {
		spec.Architecture = card.Config.ModelType
	}
	if card.Config.MaxPosition > 0 {
		// HF 上下文长度补 OpenRouter 之缺
		spec.MaxTokens = 0 // 留给 context 字段（models.context_length）；此处不覆盖 max_tokens
		_ = card.Config.MaxPosition
	}
	// license：HF tags 中常含 license-xxx
	for _, tag := range card.Tags {
		if strings.HasPrefix(tag, "license:") {
			spec.License = strings.TrimPrefix(tag, "license:")
			break
		}
		if strings.HasPrefix(tag, "license-") && spec.License == "" {
			spec.License = strings.TrimPrefix(tag, "license-")
		}
	}
	if spec.CardURL == "" {
		spec.CardURL = "https://huggingface.co/" + model
	}
	return spec, nil
}

// SupportedInterfacesFor 按能力推导模型支持的推理接口（v0.9）。
// 不存在的接口不列入，符合"不存在的接口不转换"原则。
func SupportedInterfacesFor(capability string) []string {
	switch capability {
	case CapEmbedding:
		return []string{"embeddings"}
	case CapRerank:
		return []string{"rerank"}
	case CapChat, CapReasoning, CapCode, CapVision:
		return []string{"chat"}
	default:
		return nil // safety/reward/translation/parsing 无稳定推理接口
	}
}
