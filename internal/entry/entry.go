// Package entry 提供 HTTP 入口层：OpenAI 兼容端点 + 下游凭证认证 + Trace ID 注入。
//
// 暴露的端点（Phase 1）：
//   POST /v1/chat/completions  — 对话补全（流式/非流式）
//   POST /v1/completions       — 文本补全
//   GET  /v1/models             — 模型列表
//
// Phase 2 新增：
//   POST /v1/embeddings         — 嵌入
//   POST /v1/messages           — Claude 兼容
//   POST /v1beta/models/:m:generateContent — Gemini 兼容
package entry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/modelcatalog"
	"github.com/nslmcrs/gateway/internal/modelmeta"
	"github.com/nslmcrs/gateway/internal/protocol"
	"github.com/nslmcrs/gateway/internal/scheduler"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// Handler 入口层处理器。
type Handler struct {
	sched   *scheduler.Scheduler
	store   *data.Store
	matcher *modelmeta.StalenessChecker
}

// New 创建入口处理器。
func New(sched *scheduler.Scheduler, store *data.Store, matcher *modelmeta.StalenessChecker) *Handler {
	return &Handler{sched: sched, store: store, matcher: matcher}
}

// RegisterRoutesWithAuth 注册转发路由并应用鉴权中间件。
// models 端点允许匿名（在中间件内通过路径跳过）。
func (h *Handler) RegisterRoutesWithAuth(r gin.IRoutes, auth gin.HandlerFunc) {
	v1 := r.(*gin.Engine).Group("/v1")
	v1.Use(auth)
	{
		// OpenAI 协议
		v1.POST("/chat/completions", h.handleChatCompletions)
		v1.POST("/completions", h.handleCompletions)
		v1.GET("/models", h.handleListModels)

		// Embedding / Rerank（路由到 ai.api.nvidia.com）
		v1.POST("/embeddings", h.handleEmbeddings)
		v1.POST("/ranking", h.handleRanking)

		// Claude (Anthropic) 协议
		v1.POST("/messages", h.handleAnthropicMessages)
	}

	// Gemini (Google) 协议：/v1beta/models/:model:generateContent
	// 注意：gin 不支持含冒号的路径段匹配 :model:generateContent，需用 NoRoute 通配兜底。
	// 这里改注册一个显式路由组，path 中的 :generateContent 由前端拼好。
	v1beta := r.(*gin.Engine).Group("/v1beta")
	v1beta.Use(auth)
	{
		// 标准 Gemini SDK 调用形如 /v1beta/models/gemini-1.5-pro:generateContent
		v1beta.POST("/models/*rest", h.handleGeminiGenerate)
	}
}

// RegisterRoutes 注册路由（无鉴权，仅测试用）。
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	v1 := r.Group("/v1")
	{
		v1.POST("/chat/completions", h.handleChatCompletions)
		v1.POST("/completions", h.handleCompletions)
		v1.GET("/models", h.handleListModels)
	}
}

// --- Trace ID ---

// generateTraceID 生成 16 字节随机 Trace ID（32 hex 字符）。
func generateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- 下游凭证认证中间件 ---

// AuthMiddleware 校验下游凭证（sk-nv-xxx）。
// skipPaths 中的路径跳过鉴权（如 /v1/models 允许匿名查看）。
func AuthMiddleware(store *data.Store, skipPaths []string) gin.HandlerFunc {
	skip := make(map[string]bool)
	for _, p := range skipPaths {
		skip[p] = true
	}
	return func(c *gin.Context) {
		if skip[c.Request.URL.Path] {
			c.Next()
			return
		}
		auth := c.GetHeader("Authorization")
		if auth == "" {
			respondError(c, http.StatusUnauthorized, "auth_missing", "", "", "", nil)
			c.Abort()
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		cred, err := store.GetDownstreamCredentialByValue(c.Request.Context(), token)
		if err != nil || cred == nil || !cred.Enabled {
			respondError(c, http.StatusUnauthorized, "auth_invalid", "", "", "", nil)
			c.Abort()
			return
		}
		// 增加计数
		_ = store.IncrementCredentialRequests(c.Request.Context(), cred.ID)
		c.Set("cred", cred)
		c.Next()
	}
}

// --- Chat Completions ---

// chatRequest OpenAI 格式的对话请求。
type chatRequest struct {
	Model       string         `json:"model"`
	Messages    []chatMessage  `json:"messages"`
	MaxTokens   int            `json:"max_tokens"`
	Temperature *float64       `json:"temperature,omitempty"`
	TopP        *float64       `json:"top_p,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
	Stop        any            `json:"stop,omitempty"`
	Tools       []any          `json:"tools,omitempty"`
	// 防止未用字段警告
}

// chatMessage 单条消息。Content 为 any 以透传多模态内容块数组（视觉模型）。
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func (h *Handler) handleChatCompletions(c *gin.Context) {
	traceID := generateTraceID()
	c.Header("X-Trace-ID", traceID)

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondErrorf(c, http.StatusBadRequest, "invalid_body", traceID, nil)
		return
	}

	// 请求方式自动转换（v0.9）：纠正 messages 为字符串、缺 messages 但有 prompt、模型别名
	body, _, convErr := normalizeChatRequest(raw)
	if convErr != nil {
		// 解析失败 → 转换失败说明
		respondError(c, http.StatusBadRequest, "conversion_failed",
			"请求体格式错误", "Invalid request body: "+convErr.Error(), traceID, nil)
		return
	}

	// 解析出 model 用于失效/熔断检测
	var peek chatRequest
	_ = json.Unmarshal(body, &peek)
	model := mapModelAlias(peek.Model)

	// 模型失效检测
	if h.matcher != nil && model != "" {
		if r := h.matcher.Check(c.Request.Context(), model); r.Stale {
			zh, en := modelmeta.StaleMessage(model, r)
			respondError(c, http.StatusNotFound, "model_unavailable", zh, en, traceID,
				gin.H{"suggested_model": r.SuggestedModel})
			return
		}
	}

	// 模型熔断检测（v0.9）：永久/临时熔断则直接返回双语说明
	if h.checkModelCircuit(c, model, traceID) {
		return
	}

	if peek.Stream {
		h.handleStream(c, traceID, model, body)
	} else {
		h.handleNonStream(c, traceID, model, body)
	}
}

func (h *Handler) handleNonStream(c *gin.Context, traceID, model string, body []byte) {
	result, err := h.sched.Dispatch(c.Request.Context(), traceID, model, body)
	if err != nil {
		respondErrorf(c, http.StatusBadGateway, "upstream_all_failed", traceID, gin.H{"detail": err.Error()})
		return
	}

	c.Header("X-Trace-ID", traceID)
	c.Data(result.StatusCode, "application/json", result.Body)
}

func (h *Handler) handleStream(c *gin.Context, traceID, model string, body []byte) {
	result, err := h.sched.DispatchStream(c.Request.Context(), traceID, model, body)
	if err != nil {
		respondErrorf(c, http.StatusBadGateway, "upstream_all_failed", traceID, gin.H{"detail": err.Error()})
		return
	}
	defer result.StreamResp.Body.Close()
	defer result.StreamCancel()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Trace-ID", traceID)

	// 设置 Flush
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		respondErrorf(c, http.StatusInternalServerError, "unsupported_endpoint", traceID, nil)
		return
	}

	// 直接透传 SSE
	buf := make([]byte, 4096)
	for {
		n, err := result.StreamResp.Body.Read(buf)
		if n > 0 {
			c.Writer.Write(buf[:n])
			flusher.Flush()
		}
		if err != nil {
			break
		}
	}
}

// --- Completions ---

func (h *Handler) handleCompletions(c *gin.Context) {
	traceID := generateTraceID()
	c.Header("X-Trace-ID", traceID)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondErrorf(c, http.StatusBadRequest, "invalid_body", traceID, nil)
		return
	}

	result, err := h.sched.Dispatch(c.Request.Context(), traceID, "", body)
	if err != nil {
		respondErrorf(c, http.StatusBadGateway, "upstream_all_failed", traceID, gin.H{"detail": err.Error()})
		return
	}
	c.Data(result.StatusCode, "application/json", result.Body)
}

// --- Models ---

// handleListModels 公开模型列表（OpenAI 兼容 /v1/models）。
//
// 默认仅返回可对话能力（chat/reasoning/code/vision），避免把嵌入/重排序/安全等
// 非 chat 模型暴露给 /v1/chat/completions 客户端。查询参数：
//
//	?capability=<cap>   仅返回指定能力（chat/embedding/rerank/...）
//	?all=true            返回全部可用模型（不做能力过滤，含嵌入/重排序等）
//
// 熔断门控（v0.9）：处于 open（未过冷却）或 permanent 的模型从公开列表隐藏，
// 实时变动用户可见模型列表——确保只暴露当前可用的模型。
func (h *Handler) handleListModels(c *gin.Context) {
	var (
		models []data.Model
		err    error
	)
	if capq := strings.TrimSpace(c.Query("capability")); capq != "" {
		models, err = h.store.ListActiveModelsByCapability(c.Request.Context(), capq)
	} else if c.Query("all") == "true" {
		models, err = h.store.ListActiveModels(c.Request.Context())
	} else {
		// 默认：全部可用模型再按「可对话」过滤
		all, e := h.store.ListActiveModels(c.Request.Context())
		if e != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "查询模型列表失败"}})
			return
		}
		for _, m := range all {
			if modelcatalog.IsChatCapable(m.Capability) {
				models = append(models, m)
			}
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "查询模型列表失败"}})
		return
	}

	// 熔断门控：隐藏 open（未过冷却）与 permanent 的模型
	hidden, err := h.store.ListCircuitHiddenModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "查询模型熔断状态失败"}})
		return
	}
	if len(hidden) > 0 {
		filtered := models[:0]
		for _, m := range models {
			if _, blocked := hidden[m.ID]; blocked {
				continue
			}
			filtered = append(filtered, m)
		}
		models = filtered
	}

	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	entries := make([]modelEntry, len(models))
	for i, m := range models {
		entries[i] = modelEntry{
			ID:      m.ID,
			Object:  m.Object,
			Created: m.Created,
			OwnedBy: m.OwnedBy,
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   entries,
	})
}

// --- Embeddings ---

// embeddingRequest OpenAI 嵌入请求（model + input）。
type embeddingRequest struct {
	Model          string `json:"model"`
	Input          any    `json:"input"` // string 或 []string
	EncodingFormat string `json:"encoding_format,omitempty"`
	InputType      string `json:"input_type,omitempty"` // NVIDIA 扩展：query/pass
	Truncate       string `json:"truncate,omitempty"`   // NVIDIA 扩展：NONE/START/END
}

// handleEmbeddings 向量嵌入端点。
// 直接转发到 ai.api.nvidia.com/v1/embeddings（能力路由）。
func (h *Handler) handleEmbeddings(c *gin.Context) {
	traceID := generateTraceID()
	c.Header("X-Trace-ID", traceID)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "读取请求体失败", "type": "invalid_request"}})
		return
	}

	var peek embeddingRequest
	var model string
	if json.Unmarshal(body, &peek) == nil {
		model = peek.Model
	}

	if h.checkModelCircuit(c, model, traceID) {
		return
	}

	result, err := h.sched.DispatchCap(c.Request.Context(), upstream.CapEmbedding, traceID, model, body)
	if err != nil {
		respondErrorf(c, http.StatusBadGateway, "upstream_all_failed", traceID, gin.H{"detail": err.Error()})
		return
	}
	c.Header("X-Trace-ID", traceID)
	c.Data(result.StatusCode, "application/json", result.Body)
}

// --- Rerank ---

// handleRanking 重排序端点。
// 直接转发到 ai.api.nvidia.com/v1/ranking（能力路由）。
func (h *Handler) handleRanking(c *gin.Context) {
	traceID := generateTraceID()
	c.Header("X-Trace-ID", traceID)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "读取请求体失败", "type": "invalid_request"}})
		return
	}

	var peek embeddingRequest
	var model string
	if json.Unmarshal(body, &peek) == nil {
		model = peek.Model
	}

	if h.checkModelCircuit(c, model, traceID) {
		return
	}

	result, err := h.sched.DispatchCap(c.Request.Context(), upstream.CapRerank, traceID, model, body)
	if err != nil {
		respondErrorf(c, http.StatusBadGateway, "upstream_all_failed", traceID, gin.H{"detail": err.Error()})
		return
	}
	c.Header("X-Trace-ID", traceID)
	c.Data(result.StatusCode, "application/json", result.Body)
}

// --- Anthropic (Claude) /v1/messages ---

// handleAnthropicMessages Claude 协议端点。
// 入站 Anthropic 格式 → 翻译为 OpenAI → 调度 → 翻译回 Anthropic。
func (h *Handler) handleAnthropicMessages(c *gin.Context) {
	traceID := generateTraceID()
	c.Header("X-Trace-ID", traceID)

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondErrorf(c, http.StatusBadRequest, "invalid_body", traceID, nil)
		return
	}

	var req protocol.AnthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_body",
			"请求体格式错误", "Invalid request body: "+err.Error(), traceID, nil)
		return
	}

	// Claude 模型名 → NVIDIA 模型名映射
	origModel := req.Model
	mappedModel := mapClaudeModel(origModel)

	// 失效模型检测（对映射后的 NVIDIA 模型名检测）
	if h.matcher != nil {
		if r := h.matcher.Check(c.Request.Context(), mappedModel); r.Stale {
			zh, en := modelmeta.StaleMessage(mappedModel, r)
			respondError(c, http.StatusNotFound, "model_unavailable", zh, en, traceID,
				gin.H{"suggested_model": r.SuggestedModel})
			return
		}
	}

	// 模型熔断检测（v0.9）
	if h.checkModelCircuit(c, mappedModel, traceID) {
		return
	}

	// Claude SSE 与 OpenAI 差异较大，强制非流式以获得完整 JSON 再翻译回 Anthropic。
	req.Stream = false
	req.Model = mappedModel

	// 翻译为 OpenAI 请求体
	openaiBody, err := req.ToOpenAIChatRequest()
	if err != nil {
		respondError(c, http.StatusBadRequest, "translation_failed",
			"协议翻译失败", "Protocol translation failed: "+err.Error(), traceID, nil)
		return
	}

	result, err := h.sched.Dispatch(c.Request.Context(), traceID, mappedModel, openaiBody)
	if err != nil {
		respondErrorf(c, http.StatusBadGateway, "upstream_all_failed", traceID, gin.H{"detail": err.Error()})
		return
	}

	// 上游错误：原样返回（带 Anthropic 错误结构）
	if result.StatusCode != http.StatusOK {
		c.Header("X-Trace-ID", traceID)
		c.Data(result.StatusCode, "application/json", result.Body)
		return
	}

	// 翻译回 Anthropic 响应（回填原始模型名，保持客户端语义一致）
	anthBody, err := protocol.OpenAIToAnthropicResponse(result.Body, origModel)
	if err != nil {
		respondError(c, http.StatusBadGateway, "translation_failed",
			"响应翻译失败", "Response translation failed: "+err.Error(), traceID, nil)
		return
	}
	c.Header("X-Trace-ID", traceID)
	c.Data(http.StatusOK, "application/json", anthBody)
}

// --- Gemini (Google) /v1beta/models/:model:generateContent --

// handleGeminiGenerate Gemini 协议端点。
// 路径形如 /models/gemini-1.5-pro:generateContent，需解析出模型名与动作。
func (h *Handler) handleGeminiGenerate(c *gin.Context) {
	traceID := generateTraceID()
	c.Header("X-Trace-ID", traceID)

	// 解析 model 与 action：rest = "/<model>:<action>"
	rest := c.Param("rest")
	model, action := parseGeminiPath(rest)
	if model == "" || action != "generateContent" {
		respondError(c, http.StatusNotFound, "unsupported_endpoint",
			"不支持的端点，仅支持 :generateContent",
			"Unsupported endpoint; only :generateContent is supported", traceID, nil)
		return
	}

	// Gemini 模型名 → NVIDIA 模型名（若客户端传 gemini-xxx，需映射到 NVIDIA 等价模型）
	model = mapGeminiModel(model)

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		respondErrorf(c, http.StatusBadRequest, "invalid_body", traceID, nil)
		return
	}

	var req protocol.GeminiRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_body",
			"请求体格式错误", "Invalid request body: "+err.Error(), traceID, nil)
		return
	}

	// 失效模型检测
	if h.matcher != nil {
		if r := h.matcher.Check(c.Request.Context(), model); r.Stale {
			zh, en := modelmeta.StaleMessage(model, r)
			respondError(c, http.StatusNotFound, "model_unavailable", zh, en, traceID, nil)
			return
		}
	}

	// 模型熔断检测（v0.9）
	if h.checkModelCircuit(c, model, traceID) {
		return
	}

	// 翻译为 OpenAI 请求体
	openaiBody, err := req.ToOpenAIChatRequest(model)
	if err != nil {
		respondError(c, http.StatusBadRequest, "translation_failed",
			"协议翻译失败", "Protocol translation failed: "+err.Error(), traceID, nil)
		return
	}

	result, err := h.sched.Dispatch(c.Request.Context(), traceID, model, openaiBody)
	if err != nil {
		respondErrorf(c, http.StatusBadGateway, "upstream_all_failed", traceID, gin.H{"detail": err.Error()})
		return
	}

	if result.StatusCode != http.StatusOK {
		c.Header("X-Trace-ID", traceID)
		c.Data(result.StatusCode, "application/json", result.Body)
		return
	}

	// 翻译回 Gemini 响应
	gemBody, err := protocol.OpenAIToGeminiResponse(result.Body)
	if err != nil {
		respondError(c, http.StatusBadGateway, "translation_failed",
			"响应翻译失败", "Response translation failed: "+err.Error(), traceID, nil)
		return
	}
	c.Header("X-Trace-ID", traceID)
	c.Data(http.StatusOK, "application/json", gemBody)
}

// parseGeminiPath 解析 "/gemini-1.5-pro:generateContent" → ("gemini-1.5-pro", "generateContent")。
func parseGeminiPath(rest string) (model, action string) {
	rest = strings.TrimPrefix(rest, "/")
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return rest, ""
	}
	return rest[:idx], rest[idx+1:]
}

// mapGeminiModel 将 Gemini 模型名映射为 NVIDIA 等价模型（客户端若传 gemini-xxx 则转换）。
// 若已是 NVIDIA 模型格式（含 "/"），原样返回。
func mapGeminiModel(name string) string {
	// 已是 NVIDIA 格式
	if strings.Contains(name, "/") {
		return name
	}
	// 常见 Gemini 别名 → NVIDIA 模型
	alias := map[string]string{
		"gemini-1.5-pro":        "meta/llama-3.1-405b-instruct",
		"gemini-1.5-flash":      "meta/llama-3.1-8b-instruct",
		"gemini-pro":            "meta/llama-3.1-70b-instruct",
	}
	if m, ok := alias[name]; ok {
		return m
	}
	return name
}

// mapClaudeModel 将 Claude(Anthropic) 模型名映射为 NVIDIA 等价模型。
// 客户端常传 claude-3-5-sonnet-... 等别名，NVIDIA 上游不识别，需转换。
// 若已是 NVIDIA 模型格式（含 "/"），原样返回。
func mapClaudeModel(name string) string {
	// 已是 NVIDIA 格式
	if strings.Contains(name, "/") {
		return name
	}
	// 精确别名优先
	alias := map[string]string{
		"claude-3-opus":   "meta/llama-3.1-405b-instruct",
		"claude-3-sonnet": "meta/llama-3.1-70b-instruct",
		"claude-3-haiku":  "meta/llama-3.1-8b-instruct",
	}
	if m, ok := alias[name]; ok {
		return m
	}
	// 模糊匹配：按系列关键词映射（忽略版本后缀）
	switch {
	case strings.Contains(name, "opus"):
		return "meta/llama-3.1-405b-instruct"
	case strings.Contains(name, "haiku"):
		return "meta/llama-3.1-8b-instruct"
	case strings.Contains(name, "sonnet"), strings.HasPrefix(name, "claude"):
		return "meta/llama-3.1-70b-instruct"
	}
	return name
}
