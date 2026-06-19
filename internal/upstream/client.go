// Package upstream 封装 NVIDIA NIM API 客户端，支持多域名路由与流式 SSE 转发。
//
// 核心职责：
//   - 对话/补全/模型列表 → integrate.api.nvidia.com
//   - 嵌入/重排序 → ai.api.nvidia.com（不同域名）
//   - 连接池复用、超时控制、错误归一化
//   - 流式 SSE 透传（先到先得时，首个返回首块的锁定，其余取消）
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Capability 能力类型，决定路由到哪个上游域名。
type Capability string

const (
	CapChat      Capability = "chat"      // /v1/chat/completions, /v1/completions
	CapModels    Capability = "models"    // /v1/models
	CapEmbedding Capability = "embedding" // /v1/embeddings/{model}
	CapRerank    Capability = "rerank"    // /v1/ranking/{model} 或 /v1/retrieval/{model}
)

// Client NVIDIA API 客户端。
type Client struct {
	chatHTTP     *http.Client // integrate.api.nvidia.com
	retrievalHTTP *http.Client // ai.api.nvidia.com
	chatBaseURL   string
	retrievalBaseURL string
}

// NewClient 创建 NVIDIA 客户端。
func NewClient(chatBaseURL, retrievalBaseURL string, timeout time.Duration) *Client {
	newTransport := func() *http.Transport {
		return &http.Transport{
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
			// 关键：ai.api.nvidia.com 仅支持 HTTP/2；自定义 Transport 不会
			// 自动启用 HTTP/2，必须显式 ForceAttemptHTTP2（经 ALPN 协商）。
			ForceAttemptHTTP2: true,
		}
	}
	return &Client{
		chatHTTP: &http.Client{
			Transport: newTransport(),
			Timeout:   timeout,
		},
		retrievalHTTP: &http.Client{
			Transport: newTransport(),
			Timeout:   timeout,
		},
		chatBaseURL:      chatBaseURL,
		retrievalBaseURL: retrievalBaseURL,
	}
}

// baseURL 根据 capability 选择上游域名。
// 重要：NVIDIA 的嵌入端点与 chat 共用 integrate.api.nvidia.com（OpenAI 兼容 /v1/embeddings）；
// 仅重排序(rerank)使用 ai.api.nvidia.com 的检索域名（模型专属路径）。
func (c *Client) baseURL(cap Capability) string {
	switch cap {
	case CapRerank:
		return c.retrievalBaseURL
	default:
		return c.chatBaseURL
	}
}

// httpClient 根据 capability 选择 HTTP client。
func (c *Client) httpClient(cap Capability) *http.Client {
	switch cap {
	case CapRerank:
		return c.retrievalHTTP
	default:
		return c.chatHTTP
	}
}

// ChatCompletion 发送对话补全请求（非流式）。
// key 为 nvapi-xxx 完整密钥；body 为 OpenAI 格式请求体（已包含 model 字段）。
func (c *Client) ChatCompletion(ctx context.Context, key string, body []byte) (*Response, error) {
	return c.doRequest(ctx, CapChat, key, "/chat/completions", body)
}

// ChatCompletionStream 发送流式对话补全请求，返回原始 response body（调用方负责 SSE 读取与关闭）。
func (c *Client) ChatCompletionStream(ctx context.Context, key string, body []byte) (*http.Response, error) {
	return c.doStreamRequest(ctx, CapChat, key, "/chat/completions", body)
}

// ListModels 获取模型列表。
func (c *Client) ListModels(ctx context.Context, key string) (*ModelsResponse, error) {
	resp, err := c.doRequest(ctx, CapModels, key, "/models", nil)
	if err != nil {
		return nil, err
	}
	var models ModelsResponse
	if err := json.Unmarshal(resp.Body, &models); err != nil {
		return nil, fmt.Errorf("解析模型列表: %w", err)
	}
	return &models, nil
}

// Embedding 发送嵌入请求。
// NVIDIA 嵌入端点走 OpenAI 兼容路径，与 chat 共用 integrate.api.nvidia.com。
// （请求体含 model 字段，如 "nvidia/nv-embed-v1"。）
func (c *Client) Embedding(ctx context.Context, key string, body []byte) (*Response, error) {
	return c.doRequest(ctx, CapEmbedding, key, "/embeddings", body)
}

// Rerank 发送重排序请求。
// NVIDIA 检索端点使用模型专属路径：ai.api.nvidia.com/v1/retrieval/{model}/reranking
// （路径段中 model 的 '.' 需替换为 '_'，以匹配 NVIDIA 路由约定）。
func (c *Client) Rerank(ctx context.Context, key, model string, body []byte) (*Response, error) {
	return c.doRequest(ctx, CapRerank, key, rerankPath(model), body)
}

// rerankPath 构造 NVIDIA 重排序的模型专属路径段。
// model 形如 "nvidia/llama-3.2-nemoretriever-500m-rerank-v2"。
func rerankPath(model string) string {
	m := strings.ReplaceAll(model, ".", "_")
	m = strings.TrimPrefix(m, "/")
	return "/retrieval/" + m + "/reranking"
}

// Request 通用上游请求（按 capability 选域名）。
// 由调度器对 chat/embedding/rerank 等能力统一调用，
// path 如 "/chat/completions"、"/embeddings"、"/ranking"。
func (c *Client) Request(ctx context.Context, cap Capability, key, path string, body []byte) (*Response, error) {
	return c.doRequest(ctx, cap, key, path, body)
}

// doRequest 执行非流式请求。
func (c *Client) doRequest(ctx context.Context, cap Capability, key, path string, body []byte) (*Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	url := c.baseURL(cap) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("构建请求: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	// 模型列表用 GET
	if cap == CapModels && body == nil {
		req.Method = http.MethodGet
	}

	resp, err := c.httpClient(cap).Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求上游 %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       respBody,
	}, nil
}

// doStreamRequest 执行流式请求，返回原始 response（调用方负责读取 + 关闭）。
func (c *Client) doStreamRequest(ctx context.Context, cap Capability, key, path string, body []byte) (*http.Response, error) {
	url := c.baseURL(cap) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient(cap).Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Response 上游响应（非流式）。
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// IsSuccess 判断是否成功（2xx）。
func (r *Response) IsSuccess() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

// IsRateLimited 判断是否被限流（429）。
func (r *Response) IsRateLimited() bool { return r.StatusCode == 429 }

// IsClientError 判断是否客户端错误（4xx）。
func (r *Response) IsClientError() bool { return r.StatusCode >= 400 && r.StatusCode < 500 }

// IsServerError 判断是否服务端错误（5xx）。
func (r *Response) IsServerError() bool { return r.StatusCode >= 500 }

// RateLimitRemaining 从 X-RateLimit-Remaining 头获取剩余配额。-1 表示头不存在。
func (r *Response) RateLimitRemaining() int {
	v := r.Header.Get("X-RateLimit-Remaining")
	if v == "" {
		return -1
	}
	n := 0
	for _, c := range []byte(v) {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// ParseNVIDIAError 解析 NVIDIA 的 JSON-API 风格错误。
// 429 体可能为空，需防御。
type NVIDIAError struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func (r *Response) ParseNVIDIAError() *NVIDIAError {
	if len(r.Body) == 0 {
		return &NVIDIAError{Status: r.StatusCode, Title: http.StatusText(r.StatusCode)}
	}
	var e NVIDIAError
	if json.Unmarshal(r.Body, &e) == nil && e.Title != "" {
		return &e
	}
	return &NVIDIAError{Status: r.StatusCode, Title: string(r.Body)}
}

// ModelsResponse /v1/models 响应。
type ModelsResponse struct {
	Object string   `json:"object"`
	Data   []Model `json:"data"`
}

// Model 单个模型条目（NVIDIA /v1/models 返回的最小 schema）。
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
	Root    string `json:"root"`
}
