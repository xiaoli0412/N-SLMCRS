// entry 包错误响应工具（v0.9）。
//
// 统一双语错误契约：{error:{type, message(zh), message_en(en), trace_id, ...extras}}。
// 所有入口层错误响应应经此构造，确保客户端同时获得中英文说明。
package entry

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// errPair 双语错误对。
type errPair struct {
	zh string
	en string
}

// 错误目录：按 type 取中英文标准文案。调用方可覆盖 message 字段。
var errCatalog = map[string]errPair{
	"auth_missing":            {"缺少认证凭据", "Authentication credential is missing"},
	"auth_invalid":            {"无效或已停用的下游凭证", "Invalid or disabled downstream credential"},
	"invalid_body":            {"请求体格式错误", "Invalid request body"},
	"model_unavailable":       {"模型不可用或已下线", "Model is unavailable or has been removed"},
	"model_circuit_open":      {"模型已临时熔断，请稍后重试或切换模型", "Model is temporarily circuit-broken; retry later or switch models"},
	"model_permanently_broken": {"模型已永久熔断（长期不可用），请切换模型", "Model is permanently circuit-broken (chronically unavailable); please switch models"},
	"upstream_all_failed":     {"所有上游均失败", "All upstream keys failed"},
	"rate_limited":            {"请求被限流，请稍后重试", "Request rate-limited; please retry later"},
	"translation_failed":      {"协议翻译失败", "Protocol translation failed"},
	"conversion_failed":       {"请求方式转换失败，请检查模型与端点匹配", "Request-shape conversion failed; check model/endpoint compatibility"},
	"unsupported_endpoint":    {"不支持的端点", "Unsupported endpoint"},
}

// respondError 统一返回双语错误。extras 为附加字段（如 suggested_model）。
func respondError(c *gin.Context, status int, errType, zh, en, traceID string, extras gin.H) {
	if zh == "" || en == "" {
		if p, ok := errCatalog[errType]; ok {
			if zh == "" {
				zh = p.zh
			}
			if en == "" {
				en = p.en
			}
		}
	}
	body := gin.H{"error": gin.H{
		"type":       errType,
		"message":    zh,
		"message_en": en,
	}}
	for k, v := range extras {
		body["error"].(gin.H)[k] = v
	}
	if traceID != "" {
		body["error"].(gin.H)["trace_id"] = traceID
	}
	c.JSON(status, body)
}

// respondErrorf 便捷：仅类型 + traceID（文案取目录）。
func respondErrorf(c *gin.Context, status int, errType, traceID string, extras gin.H) {
	respondError(c, status, errType, "", "", traceID, extras)
}

// checkModelCircuit 检查模型是否处于请求拒绝态；若是则写双语错误并返回 true。
// permanent 与 open（未过冷却）均拒绝；half_open 放行试探。
func (h *Handler) checkModelCircuit(c *gin.Context, model, traceID string) bool {
	if h.store == nil || model == "" {
		return false
	}
	blocked, state, err := h.store.IsModelCircuitOpen(c.Request.Context(), model)
	if err != nil || !blocked {
		return false
	}
	suggested, _, _ := h.store.SuggestBestModel(c.Request.Context(), "")
	errType := "model_circuit_open"
	if state == "permanent" {
		errType = "model_permanently_broken"
	}
	extras := gin.H{"circuit_state": state}
	if suggested != "" {
		extras["suggested_model"] = suggested
	}
	respondErrorf(c, http.StatusServiceUnavailable, errType, traceID, extras)
	return true
}

var _ = http.StatusOK
