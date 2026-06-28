package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// 内置 Chat 测试台（v0.11，仿 NVIDIA Studio）。
//
// 管理凭证鉴权下直接调度，复用 N 路并发 + 熔断 + 限流，绕过下游凭证
// （用内部上游 Key，无需 sk-nv- 下游令牌）。流式走 SSE 透传，与
// /v1/chat/completions 同路径，便于在面板内验证模型可用性与效果。
//
// POST /api/admin/playground/chat  body: OpenAI chat/completions 请求体（含 stream 字段）

// playgroundTraceID 生成 8 字节 hex trace id（与 entry.generateTraceID 同形）。
func playgroundTraceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// playgroundChat 测试台入口：解析 model/stream，分流流式与非流式。
func (h *Handler) playgroundChat(c *gin.Context) {
	if h.sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "调度器未启用"})
		return
	}
	traceID := playgroundTraceID()
	c.Header("X-Trace-ID", traceID)

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体读取失败"})
		return
	}
	var peek struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream,omitempty"`
	}
	_ = json.Unmarshal(raw, &peek)
	model := strings.TrimSpace(peek.Model)
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 model 字段"})
		return
	}

	if peek.Stream {
		h.playgroundStream(c, traceID, model, raw)
		return
	}
	result, err := h.sched.Dispatch(c.Request.Context(), traceID, model, raw)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "trace_id": traceID})
		return
	}
	c.Header("X-Trace-ID", traceID)
	c.Data(result.StatusCode, "application/json", result.Body)
}

// playgroundStream 流式调度 + SSE 透传（镜像 entry.handleStream）。
func (h *Handler) playgroundStream(c *gin.Context, traceID, model string, body []byte) {
	result, err := h.sched.DispatchStream(c.Request.Context(), traceID, model, body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "trace_id": traceID})
		return
	}
	defer result.StreamResp.Body.Close()
	defer result.StreamCancel()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Trace-ID", traceID)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "当前响应器不支持 SSE"})
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := result.StreamResp.Body.Read(buf)
		if n > 0 {
			_, _ = c.Writer.Write(buf[:n])
			flusher.Flush()
		}
		if err != nil {
			break
		}
	}
}
