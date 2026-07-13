package admin

// 策略引擎管理端点（v0.14）：GET/PUT /api/admin/strategy。
//
// 活跃策略权威在 Rust 内核（/reserve 按其选择算法派发）；Go 侧维护镜像供
// kernel 不可达的降级路径 + 重启恢复。切换策略时：
//   - 镜像到调度器（降级 selectKeysGo 据此选算法/扇出/头寸）；
//   - 应用策略熔断参数到调度器配置（degrade 路径 + Settings UI 一致）；
//   - 持久化 strategy.active + 熔断参数到 settings（重启恢复）；
//   - 推送到内核（kernel 权威 + 持久化到 kernel meta）；
//   - 记审计。

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/kernelctl"
	"github.com/nslmcrs/gateway/internal/scheduler"
)

// getStrategy 返回活跃策略 + 全部预设 + 按密钥数推荐。
// 活跃 id 优先取内核（权威）；内核不可达读 settings 镜像。
func (h *Handler) getStrategy(c *gin.Context) {
	presets := kernelctl.Presets()
	activeID := "balanced"
	kernelOnline := false
	if h.kernel != nil {
		if st, ok := h.kernel.GetStrategy(c.Request.Context()); ok {
			kernelOnline = true
			activeID = st.Active.ID
		}
	}
	if !kernelOnline {
		if v, ok := getSettingStr(c.Request.Context(), h.store, settingKeyStrategy); ok {
			activeID = v
		}
	}
	active, _ := kernelctl.PresetByID(activeID)
	keyCount := h.enabledKeyCount(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"active":        active,
		"presets":       presets,
		"recommended":   kernelctl.Recommend(keyCount),
		"key_count":     keyCount,
		"kernel_online": kernelOnline,
	})
}

// putStrategy 切换活跃策略：镜像 + 应用熔断参数 + 持久化 + 推送内核 + 审计。
func (h *Handler) putStrategy(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, ok := kernelctl.PresetByID(req.ID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知策略 id: " + req.ID})
		return
	}

	ctx := c.Request.Context()

	// 1+2. 镜像到调度器 + 应用策略熔断参数到调度器配置（degrade 路径 + Settings 一致）
	if h.sched != nil {
		h.sched.SetStrategy(p)
		patch := scheduler.SchedulerConfig{
			CircuitThreshold: p.BreakerThreshold,
			CircuitCooldown:  time.Duration(p.BreakerCooldownSec) * time.Second,
		}
		if err := h.sched.UpdateConfig(patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// 3. 持久化 strategy.active + 熔断参数到 settings（重启恢复）
	_ = h.store.SetSetting(ctx, settingKeyStrategy, p.ID)
	_ = h.store.SetSetting(ctx, settingCircuitThreshold, strconv.Itoa(p.BreakerThreshold))
	_ = h.store.SetSetting(ctx, settingCircuitCooldownSec, strconv.FormatInt(p.BreakerCooldownSec, 10))

	// 4. 推送到内核（kernel 权威 + 持久化到 kernel meta）。best-effort。
	kernelOnline := false
	if h.kernel != nil {
		if _, ok := h.kernel.SetStrategy(ctx, p.ID); ok {
			kernelOnline = true
		}
	}

	// 5. 审计
	h.audit(c, "strategy.set", gin.H{"id": p.ID, "name_zh": p.NameZh, "name_en": p.NameEn})

	keyCount := h.enabledKeyCount(ctx)
	c.JSON(http.StatusOK, gin.H{
		"active":        p,
		"presets":       kernelctl.Presets(),
		"recommended":   kernelctl.Recommend(keyCount),
		"key_count":     keyCount,
		"kernel_online": kernelOnline,
	})
}

// enabledKeyCount 返回已启用上游密钥数（推荐徽章用）。
func (h *Handler) enabledKeyCount(ctx context.Context) int {
	keys, err := h.store.ListUpstreamKeys(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, k := range keys {
		if k.Enabled {
			n++
		}
	}
	return n
}

// LoadPersistedStrategy 从 settings 表读取已持久化的活跃策略 id，镜像到调度器
// （main.go 启动时调用，降级路径据此选择算法）。内核在线时内核为权威，自载 meta。
func LoadPersistedStrategy(ctx context.Context, s *scheduler.Scheduler, store *data.Store) error {
	if s == nil || store == nil {
		return nil
	}
	v, ok := getSettingStr(ctx, store, settingKeyStrategy)
	if !ok {
		return nil
	}
	p, found := kernelctl.PresetByID(v)
	if !found {
		return nil
	}
	s.SetStrategy(p)
	return nil
}
