// Auto-Pilot 管理 API 处理器（Phase 3 Step 6）。
//
// 端点（均需 ADMIN_TOKEN，挂在 /api/admin/autopilot 下）：
//   GET    /autopilot/state                 完整状态（mode/engine/runtime/统计/最近事件）
//   GET    /autopilot/snapshot              决策快照（密钥健康/限流/时序）
//   PUT    /autopilot/mode                  热切换模式   {mode:"manual|assisted|fullauto"}
//   PUT    /autopilot/engine                热切换引擎   {engine:"adaptive|predict|llm"}
//   GET    /autopilot/pending               列出 assisted 待审建议
//   POST   /autopilot/pending/:key/approve  批准并执行
//   POST   /autopilot/pending/:key/reject   驳回
//
// Controller 由 main.go 在装配后通过 SetAutopilot 注入（避免装配顺序耦合）。

package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/autopilot"
)

// SetAutopilot 注入 Auto-Pilot 总控（main.go 装配后调用）。
func (h *Handler) SetAutopilot(ctrl *autopilot.Controller) {
	h.ap = ctrl
}

// autopilotOK 总控未注入时返回统一错误。
func (h *Handler) autopilotOK(c *gin.Context) (*autopilot.Controller, bool) {
	if h.ap == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Auto-Pilot 未启用"})
		return nil, false
	}
	return h.ap, true
}

type setModeReq struct {
	Mode string `json:"mode" binding:"required"`
}

func (h *Handler) apSetMode(c *gin.Context) {
	ctrl, ok := h.autopilotOK(c)
	if !ok {
		return
	}
	var req setModeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ctrl.SetMode(c.Request.Context(), autopilot.Mode(req.Mode)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "mode": req.Mode})
}

type setEngineReq struct {
	Engine string `json:"engine" binding:"required"`
}

func (h *Handler) apSetEngine(c *gin.Context) {
	ctrl, ok := h.autopilotOK(c)
	if !ok {
		return
	}
	var req setEngineReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ctrl.SetEngine(c.Request.Context(), autopilot.EngineID(req.Engine)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "engine": req.Engine})
}

func (h *Handler) apState(c *gin.Context) {
	ctrl, ok := h.autopilotOK(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, ctrl.State(c.Request.Context()))
}

func (h *Handler) apSnapshot(c *gin.Context) {
	ctrl, ok := h.autopilotOK(c)
	if !ok {
		return
	}
	snap, err := ctrl.Snapshot(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, snap)
}

func (h *Handler) apListPending(c *gin.Context) {
	ctrl, ok := h.autopilotOK(c)
	if !ok {
		return
	}
	entries, err := ctrl.ListPending(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": entries})
}

func (h *Handler) apApprovePending(c *gin.Context) {
	ctrl, ok := h.autopilotOK(c)
	if !ok {
		return
	}
	key := c.Param("key")
	if err := ctrl.ApprovePending(c.Request.Context(), key); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) apRejectPending(c *gin.Context) {
	ctrl, ok := h.autopilotOK(c)
	if !ok {
		return
	}
	key := c.Param("key")
	if err := ctrl.RejectPending(c.Request.Context(), key); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
