package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/modelhealth"
	"github.com/nslmcrs/gateway/internal/scheduler"
)

// settings 表中调度/熔断配置的 key 前缀。
// 完整 key 形如 "sched.circuit_threshold"，与 config.go 的环境变量语义对齐。
const (
	settingCircuitThreshold   = "sched.circuit_threshold"
	settingCircuitCooldownSec = "sched.circuit_cooldown_sec"
	settingDefaultConcurrency = "sched.default_concurrency"
	settingMaxConcurrency     = "sched.max_concurrency"
	settingRequestTimeoutSec  = "sched.request_timeout_sec"
)

// 模型级健康扫描配置 key（v0.9）。前缀 modelhealth.*，与 config.go 环境变量对齐。
const (
	settingMHProbeCount           = "modelhealth.probe_count"
	settingMHProbeIntervalSec     = "modelhealth.probe_interval_sec"
	settingMHSweepIntervalSec     = "modelhealth.sweep_interval_sec"
	settingMHSuccessRateFloor     = "modelhealth.success_rate_floor"
	settingMHSuccessRateThreshold = "modelhealth.success_rate_threshold"
	settingMHBadSweepToPermanent  = "modelhealth.bad_sweep_to_permanent"
	settingMHCooldownBaseSec      = "modelhealth.cooldown_base_sec"
)

// settingsView 对外契约：熔断/调度运行时配置 + 模型健康扫描配置（GET /api/admin/settings）。
// 时长字段以「秒」暴露，便于前端直观展示与编辑。
type settingsView struct {
	CircuitThreshold   int `json:"circuit_threshold"`
	CircuitCooldownSec int `json:"circuit_cooldown_sec"`
	DefaultConcurrency int `json:"default_concurrency"`
	MaxConcurrency     int `json:"max_concurrency"`
	RequestTimeoutSec  int `json:"request_timeout_sec"`

	// 模型级健康扫描（v0.9）
	MHProbeCount           int `json:"mh_probe_count"`
	MHProbeIntervalSec     int `json:"mh_probe_interval_sec"`
	MHSweepIntervalSec     int `json:"mh_sweep_interval_sec"`
	MHSuccessRateFloor     int `json:"mh_success_rate_floor"`
	MHSuccessRateThreshold int `json:"mh_success_rate_threshold"`
	MHBadSweepToPermanent  int `json:"mh_bad_sweep_to_permanent"`
	MHCooldownBaseSec      int `json:"mh_cooldown_base_sec"`
}

// settingsReq 更新请求体：全部字段可选（零值/缺省表示不改）。
type settingsReq struct {
	CircuitThreshold   *int `json:"circuit_threshold"`
	CircuitCooldownSec *int `json:"circuit_cooldown_sec"`
	DefaultConcurrency *int `json:"default_concurrency"`
	MaxConcurrency     *int `json:"max_concurrency"`
	RequestTimeoutSec  *int `json:"request_timeout_sec"`

	// 模型级健康扫描（v0.9）
	MHProbeCount           *int `json:"mh_probe_count"`
	MHProbeIntervalSec     *int `json:"mh_probe_interval_sec"`
	MHSweepIntervalSec     *int `json:"mh_sweep_interval_sec"`
	MHSuccessRateFloor     *int `json:"mh_success_rate_floor"`
	MHSuccessRateThreshold *int `json:"mh_success_rate_threshold"`
	MHBadSweepToPermanent  *int `json:"mh_bad_sweep_to_permanent"`
	MHCooldownBaseSec      *int `json:"mh_cooldown_base_sec"`
}

// readSettings 从调度器当前运行时快照组装对外视图。
// 调度器未注入时回退到配置默认值。
func (h *Handler) readSettings() settingsView {
	v := settingsView{
		CircuitThreshold:   h.cfg.Scheduler.CircuitThreshold,
		CircuitCooldownSec: int(h.cfg.Scheduler.CircuitCooldown.Seconds()),
		DefaultConcurrency: h.cfg.Scheduler.DefaultConcurrency,
		MaxConcurrency:     h.cfg.Scheduler.MaxConcurrency,
		RequestTimeoutSec:  int(h.cfg.Scheduler.RequestTimeout.Seconds()),
		MHProbeCount:           h.cfg.ModelHealth.ProbeCount,
		MHProbeIntervalSec:     int(h.cfg.ModelHealth.ProbeInterval.Seconds()),
		MHSweepIntervalSec:     int(h.cfg.ModelHealth.SweepInterval.Seconds()),
		MHSuccessRateFloor:     h.cfg.ModelHealth.SuccessRateFloor,
		MHSuccessRateThreshold: h.cfg.ModelHealth.SuccessRateThreshold,
		MHBadSweepToPermanent:  h.cfg.ModelHealth.BadSweepToPermanent,
		MHCooldownBaseSec:      int(h.cfg.ModelHealth.CooldownBase.Seconds()),
	}
	// 调度器注入后，以其当前运行时值为基准（含已热改值）
	if h.sched != nil {
		sc := h.sched.Config()
		v.CircuitThreshold = sc.CircuitThreshold
		v.CircuitCooldownSec = int(sc.CircuitCooldown.Seconds())
		v.DefaultConcurrency = sc.DefaultConcurrency
		v.MaxConcurrency = sc.MaxConcurrency
		v.RequestTimeoutSec = int(sc.RequestTimeout.Seconds())
	}
	// 健康扫描器注入后，以其当前运行时值为基准（含已热改值）
	if h.sweeper != nil {
		mh := h.sweeper.Config()
		v.MHProbeCount = mh.ProbeCount
		v.MHProbeIntervalSec = int(mh.ProbeInterval.Seconds())
		v.MHSweepIntervalSec = int(mh.SweepInterval.Seconds())
		v.MHSuccessRateFloor = mh.SuccessRateFloor
		v.MHSuccessRateThreshold = mh.SuccessRateThreshold
		v.MHBadSweepToPermanent = mh.BadSweepToPermanent
		v.MHCooldownBaseSec = int(mh.CooldownBase.Seconds())
	}
	return v
}

// getSettings 返回当前熔断/调度运行时配置。
func (h *Handler) getSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.readSettings())
}

// putSettings 更新熔断/调度运行时配置：落库 + 热应用到调度器。
// 校验失败返回 400；调度器未注入时仅落库（下次启动生效）。
func (h *Handler) putSettings(c *gin.Context) {
	var req settingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 组装 patch（零值字段不覆盖）
	patch := scheduler.SchedulerConfig{}
	if req.CircuitThreshold != nil {
		patch.CircuitThreshold = *req.CircuitThreshold
	}
	if req.CircuitCooldownSec != nil && *req.CircuitCooldownSec > 0 {
		patch.CircuitCooldown = time.Duration(*req.CircuitCooldownSec) * time.Second
	}
	if req.DefaultConcurrency != nil {
		patch.DefaultConcurrency = *req.DefaultConcurrency
	}
	if req.MaxConcurrency != nil {
		patch.MaxConcurrency = *req.MaxConcurrency
	}
	if req.RequestTimeoutSec != nil && *req.RequestTimeoutSec > 0 {
		patch.RequestTimeout = time.Duration(*req.RequestTimeoutSec) * time.Second
	}

	// 热应用到调度器（含一致性校验，失败则整体不生效）
	if h.sched != nil {
		if err := h.sched.UpdateConfig(patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	// 热应用到模型健康扫描器（v0.9）
	mhPatch := h.buildMHPatch(req)
	if h.sweeper != nil {
		h.sweeper.UpdateConfig(mhPatch)
	}

	// 落库持久化（即便调度器/扫描器未注入也存，下次启动加载）
	if err := h.persistSettings(c.Request.Context(), req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "settings": h.readSettings()})
}

// buildMHPatch 由请求体组装模型健康扫描配置 patch（零值字段不覆盖）。
func (h *Handler) buildMHPatch(req settingsReq) modelhealth.Config {
	p := modelhealth.Config{}
	if req.MHProbeCount != nil {
		p.ProbeCount = *req.MHProbeCount
	}
	if req.MHProbeIntervalSec != nil && *req.MHProbeIntervalSec > 0 {
		p.ProbeInterval = time.Duration(*req.MHProbeIntervalSec) * time.Second
	}
	if req.MHSweepIntervalSec != nil && *req.MHSweepIntervalSec > 0 {
		p.SweepInterval = time.Duration(*req.MHSweepIntervalSec) * time.Second
	}
	if req.MHSuccessRateFloor != nil {
		p.SuccessRateFloor = *req.MHSuccessRateFloor
	}
	if req.MHSuccessRateThreshold != nil {
		p.SuccessRateThreshold = *req.MHSuccessRateThreshold
	}
	if req.MHBadSweepToPermanent != nil {
		p.BadSweepToPermanent = *req.MHBadSweepToPermanent
	}
	if req.MHCooldownBaseSec != nil && *req.MHCooldownBaseSec > 0 {
		p.CooldownBase = time.Duration(*req.MHCooldownBaseSec) * time.Second
	}
	return p
}

// persistSettings 把更新写入 settings 表。
func (h *Handler) persistSettings(ctx context.Context, req settingsReq) error {
	set := func(k, v string) error {
		if v == "" {
			return nil
		}
		return h.store.SetSetting(ctx, k, v)
	}
	if req.CircuitThreshold != nil {
		if err := set(settingCircuitThreshold, strconv.Itoa(*req.CircuitThreshold)); err != nil {
			return err
		}
	}
	if req.CircuitCooldownSec != nil && *req.CircuitCooldownSec > 0 {
		if err := set(settingCircuitCooldownSec, strconv.Itoa(*req.CircuitCooldownSec)); err != nil {
			return err
		}
	}
	if req.DefaultConcurrency != nil {
		if err := set(settingDefaultConcurrency, strconv.Itoa(*req.DefaultConcurrency)); err != nil {
			return err
		}
	}
	if req.MaxConcurrency != nil {
		if err := set(settingMaxConcurrency, strconv.Itoa(*req.MaxConcurrency)); err != nil {
			return err
		}
	}
	if req.RequestTimeoutSec != nil && *req.RequestTimeoutSec > 0 {
		if err := set(settingRequestTimeoutSec, strconv.Itoa(*req.RequestTimeoutSec)); err != nil {
			return err
		}
	}

	// 模型健康扫描（v0.9）
	if req.MHProbeCount != nil {
		if err := set(settingMHProbeCount, strconv.Itoa(*req.MHProbeCount)); err != nil {
			return err
		}
	}
	if req.MHProbeIntervalSec != nil && *req.MHProbeIntervalSec > 0 {
		if err := set(settingMHProbeIntervalSec, strconv.Itoa(*req.MHProbeIntervalSec)); err != nil {
			return err
		}
	}
	if req.MHSweepIntervalSec != nil && *req.MHSweepIntervalSec > 0 {
		if err := set(settingMHSweepIntervalSec, strconv.Itoa(*req.MHSweepIntervalSec)); err != nil {
			return err
		}
	}
	if req.MHSuccessRateFloor != nil {
		if err := set(settingMHSuccessRateFloor, strconv.Itoa(*req.MHSuccessRateFloor)); err != nil {
			return err
		}
	}
	if req.MHSuccessRateThreshold != nil {
		if err := set(settingMHSuccessRateThreshold, strconv.Itoa(*req.MHSuccessRateThreshold)); err != nil {
			return err
		}
	}
	if req.MHBadSweepToPermanent != nil {
		if err := set(settingMHBadSweepToPermanent, strconv.Itoa(*req.MHBadSweepToPermanent)); err != nil {
			return err
		}
	}
	if req.MHCooldownBaseSec != nil && *req.MHCooldownBaseSec > 0 {
		if err := set(settingMHCooldownBaseSec, strconv.Itoa(*req.MHCooldownBaseSec)); err != nil {
			return err
		}
	}
	return nil
}

// LoadPersistedSchedulerOverrides 从 settings 表读取已持久化的调度/熔断覆盖，
// 应用到调度器（main.go 启动时调用）。未注入调度器或无覆盖时无操作。
func LoadPersistedSchedulerOverrides(ctx context.Context, s *scheduler.Scheduler, store *data.Store) error {
	if s == nil || store == nil {
		return nil
	}
	patch := scheduler.SchedulerConfig{}
	changed := false
	if v, ok := getSettingStr(ctx, store, settingCircuitThreshold); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.CircuitThreshold = n
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingCircuitCooldownSec); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.CircuitCooldown = time.Duration(n) * time.Second
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingDefaultConcurrency); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.DefaultConcurrency = n
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingMaxConcurrency); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.MaxConcurrency = n
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingRequestTimeoutSec); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.RequestTimeout = time.Duration(n) * time.Second
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.UpdateConfig(patch)
}

// getSettingStr 取一条设置，返回去空格后的值与是否存在标志。
func getSettingStr(ctx context.Context, store *data.Store, key string) (string, bool) {
	v, err := store.GetSetting(ctx, key)
	if err != nil {
		return "", false
	}
	if v = strings.TrimSpace(v); v == "" {
		return "", false
	}
	return v, true
}

// LoadPersistedModelHealthOverrides 从 settings 表读取已持久化的模型健康扫描覆盖，
// 应用到扫描器（main.go 启动时调用）。未注入扫描器或无覆盖时无操作。
func LoadPersistedModelHealthOverrides(ctx context.Context, s *modelhealth.Sweeper, store *data.Store) error {
	if s == nil || store == nil {
		return nil
	}
	patch := modelhealth.Config{}
	changed := false
	if v, ok := getSettingStr(ctx, store, settingMHProbeCount); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.ProbeCount = n
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingMHProbeIntervalSec); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.ProbeInterval = time.Duration(n) * time.Second
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingMHSweepIntervalSec); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.SweepInterval = time.Duration(n) * time.Second
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingMHSuccessRateFloor); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.SuccessRateFloor = n
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingMHSuccessRateThreshold); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.SuccessRateThreshold = n
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingMHBadSweepToPermanent); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.BadSweepToPermanent = n
			changed = true
		}
	}
	if v, ok := getSettingStr(ctx, store, settingMHCooldownBaseSec); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			patch.CooldownBase = time.Duration(n) * time.Second
			changed = true
		}
	}
	if !changed {
		return nil
	}
	s.UpdateConfig(patch)
	return nil
}
