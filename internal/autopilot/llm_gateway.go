package autopilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// gatewayBackend 通过网关自身的 /v1/chat/completions 调用真实 LLM。
//
// 与 stubBackend 相反：构造 OpenAI 兼容请求 → POST {baseURL}/chat/completions
// → 取 choices[0].message.content → 交给 parseLLMActions 解析。
//
// 装配：LLM_BASE_URL / LLM_API_KEY / LLM_MODEL 任一为空则回退 stubBackend。
// baseURL 为网关转发地址（如 http://localhost:8787/v1），apiKey 为下游凭证。
type gatewayBackend struct {
	baseURL string // 如 http://localhost:8787/v1（不含末尾斜杠）
	apiKey  string // 下游凭证（sk-nv-xxx）
	model   string // 目标模型，如 meta/llama-3.1-8b-instruct
	client  *http.Client
}

// newGatewayBackendFromEnv 从环境变量装配网关后端；任一缺失返回 nil（用 stub）。
func newGatewayBackendFromEnv() *gatewayBackend {
	base := os.Getenv("LLM_BASE_URL")
	key := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")
	if base == "" || key == "" || model == "" {
		return nil
	}
	return &gatewayBackend{
		baseURL: base,
		apiKey:  key,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Generate 调网关 /chat/completions，返回模型文本。
func (g *gatewayBackend) Generate(ctx context.Context, prompt string) (string, error) {
	body := map[string]any{
		"model":       g.model,
		"temperature": 0.2,
		"messages": []map[string]string{
			{"role": "system", "content": "你是 N-SLMCRS 网关的智能调度器，仅输出纯 JSON，不要任何解释或代码块标记。"},
			{"role": "user", "content": prompt},
		},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("编码请求: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用网关: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("网关返回 HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var cc struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &cc); err != nil {
		return "", fmt.Errorf("解析响应: %w", err)
	}
	if len(cc.Choices) == 0 {
		return "", fmt.Errorf("网关返回空 choices")
	}
	return cc.Choices[0].Message.Content, nil
}

// truncate 截断字符串到 n 字符（错误信息用）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
