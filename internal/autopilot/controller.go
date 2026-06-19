package autopilot

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/ratelimit"
)

const (
	tickInterval = 30 * time.Second

	settingKeyMode   = "autopilot:mode"
	settingKeyEngine = "autopilot:engine"

	// 默认值（与前端 useState 初值一致）
	defaultMode   = ModeAssisted
	defaultEngine = EngineAdaptive
)

// Controller Auto-Pilot 总控：30s 周期采集快照 → 引擎决策 → 执行器应用。
type Controller struct {
	store   *data.Store
	health *ratelimit.HealthTracker
	rl     *ratelimit.Manager
	exec   *Executor
	runtime *Runtime // 调度策略覆盖（与 Scheduler 共享同一实例）

	mu          sync.RWMutex
	mode        Mode
	activeEngine Engine
	engines     map[EngineID]Engine

	healthWindow time.Duration
	maxConcurrency    int
	defaultConcurrency int
}

// NewController 创建总控。
// maxConcurrency/defaultConcurrency 用于快照与并发决策钳位。
func NewController(store *data.Store, health *ratelimit.HealthTracker, rl *ratelimit.Manager,
	rt *Runtime, healthWindow time.Duration, defaultConcurrency, maxConcurrency int) *Controller {
	exec := NewExecutor(store, rt)
	engines := map[EngineID]Engine{
		EngineAdaptive: NewAdaptiveEngine(),
		EngineForecast: NewForecastEngine(),
		EngineLLM:      NewLLMEngine(nil), // nil → stubBackend
	}
	return &Controller{
		store:   store,
		health:  health,
		rl:      rl,
		exec:     exec,
		runtime:  rt,
		mode:     defaultMode,
		activeEngine: engines[defaultEngine],
		engines:  engines,
		healthWindow:      healthWindow,
		maxConcurrency:    maxConcurrency,
		defaultConcurrency: defaultConcurrency,
	}
}

// Start 启动周期循环（阻塞调用方，应在 goroutine 中调用）。
// 首屏从 settings 读取持久化的 mode/engine；之后每 30s 采集并决策。
func (c *Controller) Start(ctx context.Context) {
	c.loadPersisted(ctx)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	log.Printf("[autopilot] 启动：mode=%s engine=%s tick=%s", c.mode, c.activeEngine.ID(), tickInterval)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[autopilot] 停止")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

// loadPersisted 从 settings 表加载持久化的 mode/engine。
func (c *Controller) loadPersisted(ctx context.Context) {
	if m, _ := c.store.GetSetting(ctx, settingKeyMode); m != "" {
		if err := c.SetMode(ctx, Mode(m)); err == nil {
			// 仅内存切换，不再回写
		}
	}
	if e, _ := c.store.GetSetting(ctx, settingKeyEngine); e != "" {
		if err := c.SetEngine(ctx, EngineID(e)); err == nil {
		}
	}
}

// tick 单次决策：采集快照 → 引擎决策 → 执行。
func (c *Controller) tick(ctx context.Context) {
	snap, err := c.snapshot(ctx)
	if err != nil {
		log.Printf("[autopilot] 快照采集失败: %v", err)
		return
	}

	c.mu.RLock()
	engine := c.activeEngine
	mode := c.mode
	c.mu.RUnlock()

	actions, err := engine.Decide(ctx, snap)
	if err != nil {
		log.Printf("[autopilot] %s 决策失败: %v", engine.ID(), err)
		return
	}
	applied := c.exec.Apply(ctx, mode, actions)
	if applied > 0 {
		c.exec.AddIntervention(applied)
	}
}

// snapshot 采集决策所需的只读快照。
func (c *Controller) snapshot(ctx context.Context) (Snapshot, error) {
	keys, err := c.store.ListUpstreamKeys(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	rpmSnap := c.rl.Snapshot() // map[keyID]float64 可用令牌

	snaps := make([]KeySnap, 0, len(keys))
	for _, k := range keys {
		ks := KeySnap{
			ID:           k.ID,
			Mask:         k.KeyMask,
			Enabled:      k.Enabled,
			Status:       k.Status,
			SuccessRate:  c.health.SuccessRate(k.ID, c.healthWindow),
			ConsecFail:   c.health.ConsecutiveFailures(k.ID),
			RPMRemaining: int(rpmSnap[k.ID]),
		}
		snaps = append(snaps, ks)
	}

	metrics, _ := c.store.GetMetrics(ctx, time.Hour)
	series, _ := c.store.GetTimeSeries(ctx, 24*time.Hour, 60)

	return Snapshot{
		Keys:               snaps,
		Metrics:            metrics,
		Series:             series,
		CurrentConcurrency:  c.effectiveConcurrency(),
		MaxConcurrency:     c.maxConcurrency,
		DefaultConcurrency: c.defaultConcurrency,
	}, nil
}

// effectiveConcurrency 返回当前生效并发度：Runtime 覆盖优先于配置默认，
// 与 Scheduler 的钳位规则保持一致（钳到 maxConcurrency）。
func (c *Controller) effectiveConcurrency() int {
	if c.runtime != nil {
		if n := c.runtime.Concurrency(); n > 0 {
			if c.maxConcurrency > 0 && n > c.maxConcurrency {
				return c.maxConcurrency
			}
			return n
		}
	}
	return c.defaultConcurrency
}

// State 返回完整状态（GET /api/admin/autopilot/state）。
func (c *Controller) State(ctx context.Context) State {
	c.mu.RLock()
	mode := c.mode
	engine := c.activeEngine.ID()
	c.mu.RUnlock()

	dpm, interventions, events := c.exec.Stats()
	pending := c.exec.CountPending(ctx)

	// 标记每条事件的 mode
	for i := range events {
		events[i].Mode = mode
	}

	rt := State{
		Mode:               mode,
		Engine:             engine,
		RuntimeConcurrency: c.runtime.Concurrency(),
		DefaultConcurrency: c.defaultConcurrency,
		MaxConcurrency:     c.maxConcurrency,
		DecisionsPerMin:    dpm,
		Interventions:      interventions,
		PendingCount:       pending,
		RecentEvents:       events,
	}
	return rt
}

// SetMode 热切换模式（持久化到 settings）。
func (c *Controller) SetMode(ctx context.Context, m Mode) error {
	if m != ModeManual && m != ModeAssisted && m != ModeFullAuto {
		return errInvalidMode
	}
	c.mu.Lock()
	c.mode = m
	c.mu.Unlock()
	_ = c.store.SetSetting(ctx, settingKeyMode, string(m))
	log.Printf("[autopilot] 模式切换 → %s", m)
	return nil
}

// SetEngine 热切换引擎（持久化到 settings）。
func (c *Controller) SetEngine(ctx context.Context, e EngineID) error {
	eng, ok := c.engines[e]
	if !ok {
		return errUnknownEngine
	}
	c.mu.Lock()
	c.activeEngine = eng
	c.mu.Unlock()
	_ = c.store.SetSetting(ctx, settingKeyEngine, string(e))
	log.Printf("[autopilot] 引擎切换 → %s", e)
	return nil
}

// Snapshot 返回决策快照（GET /api/admin/autopilot/snapshot，调试/前端雷达用）。
func (c *Controller) Snapshot(ctx context.Context) (Snapshot, error) {
	return c.snapshot(ctx)
}

// ApprovePending / RejectPending 透传给执行器。
func (c *Controller) ApprovePending(ctx context.Context, key string) error { return c.exec.ApprovePending(ctx, key) }
func (c *Controller) RejectPending(ctx context.Context, key string) error  { return c.exec.RejectPending(ctx, key) }
func (c *Controller) ListPending(ctx context.Context) ([]data.SettingEntry, error) {
	return c.exec.ListPending(ctx)
}

var (
	errInvalidMode   = &autopilotErr{"无效模式，须为 manual/assisted/fullauto"}
	errUnknownEngine = &autopilotErr{"未知引擎，须为 adaptive/predict/llm"}
)

type autopilotErr struct{ msg string }

func (e *autopilotErr) Error() string { return e.msg }
