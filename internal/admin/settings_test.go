package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/config"
	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/scheduler"
	"golang.org/x/crypto/bcrypt"
)

// newTestHandlerWithScheduler 构建绑定临时 SQLite + 真实调度器的 Handler（用于 settings 测试）。
// 调度器配置从 cfg.Scheduler 取初值。
func newTestHandlerWithScheduler(t *testing.T) (*gin.Engine, *data.Store, *scheduler.Scheduler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("data.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8787, AdminToken: config.DefaultAdminToken},
		Scheduler: config.SchedulerConfig{
			DefaultConcurrency: 5,
			MaxConcurrency:     10,
			RequestTimeout:     180 * time.Second,
			CircuitThreshold:   5,
			CircuitCooldown:    30 * time.Second,
		},
	}
	sched := scheduler.New(store, nil, nil, nil, scheduler.SchedulerConfig{
		DefaultConcurrency: cfg.Scheduler.DefaultConcurrency,
		MaxConcurrency:     cfg.Scheduler.MaxConcurrency,
		RequestTimeout:     cfg.Scheduler.RequestTimeout,
		CircuitThreshold:   cfg.Scheduler.CircuitThreshold,
		CircuitCooldown:    cfg.Scheduler.CircuitCooldown,
		HealthWindow:       2 * time.Minute,
	})
	h := New(store, nil, cfg)
	h.SetScheduler(sched)
	r := gin.New()
	h.RegisterRoutes(r)
	return r, store, sched
}

// unlock 模拟「已改密」状态：把一个有效 bcrypt 哈希写入 settings，
// 使 AuthMiddleware 不再触发 must_change_password 锁定，受保护端点可用。
func unlock(t *testing.T, store *data.Store) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("newsecret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := store.SetSetting(context.Background(), "admin:token_hash", string(hash)); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
}

// decodeSettings 解析 GET /api/admin/settings 的顶层对象（非 {settings:...} 包装）。
func decodeSettings(t *testing.T, w *httptest.ResponseRecorder) settingsView {
	t.Helper()
	var s settingsView
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode settings %q: %v", w.Body.String(), err)
	}
	return s
}

// decodeSettingsWrapped 解析 PUT 响应中的 {ok, settings:{...}} 包装。
func decodeSettingsWrapped(t *testing.T, w *httptest.ResponseRecorder) settingsView {
	t.Helper()
	var m struct {
		Settings settingsView `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode settings %q: %v", w.Body.String(), err)
	}
	return m.Settings
}

func TestSettings_GetDefaults(t *testing.T) {
	r, store, _ := newTestHandlerWithScheduler(t)
	unlock(t, store)

	w := doJSON(t, r, "GET", "/api/admin/settings", "newsecret123", nil)
	if w.Code != 200 {
		t.Fatalf("GET settings code=%d body=%s", w.Code, w.Body.String())
	}
	s := decodeSettings(t, w)
	// 应回退到调度器初值（与 cfg.Scheduler 一致）
	if s.CircuitThreshold != 5 {
		t.Errorf("CircuitThreshold=%d, want 5", s.CircuitThreshold)
	}
	if s.CircuitCooldownSec != 30 {
		t.Errorf("CircuitCooldownSec=%d, want 30", s.CircuitCooldownSec)
	}
	if s.DefaultConcurrency != 5 {
		t.Errorf("DefaultConcurrency=%d, want 5", s.DefaultConcurrency)
	}
	if s.MaxConcurrency != 10 {
		t.Errorf("MaxConcurrency=%d, want 10", s.MaxConcurrency)
	}
}

func TestSettings_UpdateHotApplyAndPersist(t *testing.T) {
	r, store, sched := newTestHandlerWithScheduler(t)
	unlock(t, store)

	// PUT：更新熔断阈值与冷却
	w := doJSON(t, r, "PUT", "/api/admin/settings", "newsecret123", map[string]any{
		"circuit_threshold":    8,
		"circuit_cooldown_sec": 60,
	})
	if w.Code != 200 {
		t.Fatalf("PUT settings code=%d body=%s", w.Code, w.Body.String())
	}
	s := decodeSettingsWrapped(t, w)
	if s.CircuitThreshold != 8 || s.CircuitCooldownSec != 60 {
		t.Errorf("after PUT CircuitThreshold=%d Cooldown=%d, want 8/60", s.CircuitThreshold, s.CircuitCooldownSec)
	}

	// 调度器运行时已热生效
	sc := sched.Config()
	if sc.CircuitThreshold != 8 {
		t.Errorf("scheduler CircuitThreshold=%d, want 8", sc.CircuitThreshold)
	}
	if sc.CircuitCooldown != 60*time.Second {
		t.Errorf("scheduler CircuitCooldown=%v, want 60s", sc.CircuitCooldown)
	}

	// 已落库
	v, _ := store.GetSetting(context.Background(), "sched.circuit_threshold")
	if v != "8" {
		t.Errorf("persisted circuit_threshold=%q, want 8", v)
	}
	v, _ = store.GetSetting(context.Background(), "sched.circuit_cooldown_sec")
	if v != "60" {
		t.Errorf("persisted circuit_cooldown_sec=%q, want 60", v)
	}
}

func TestSettings_UpdateValidation_MaxLessThanDefault(t *testing.T) {
	r, store, sched := newTestHandlerWithScheduler(t)
	unlock(t, store)

	// MaxConcurrency < DefaultConcurrency 应被拒（注意：default 默认 5，max 设 3 仍需把 default 抬高以触发）
	w := doJSON(t, r, "PUT", "/api/admin/settings", "newsecret123", map[string]any{
		"default_concurrency": 20,
		"max_concurrency":     5, // < default 20，非法
	})
	if w.Code != 400 {
		t.Fatalf("非法配置应 400, got %d body=%s", w.Code, w.Body.String())
	}
	// 调度器值不应改变
	sc := sched.Config()
	if sc.DefaultConcurrency != 5 {
		t.Errorf("校验失败不应改动配置, DefaultConcurrency=%d want 5", sc.DefaultConcurrency)
	}
}

// TestLoadPersistedSchedulerOverrides_NewStore 验证加载函数：
// 在干净 store 写入 setting 后，新建调度器加载应得到覆盖值。
func TestLoadPersistedSchedulerOverrides_NewStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("data.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// 写入持久化覆盖
	ctx := context.Background()
	if err := store.SetSetting(ctx, "sched.circuit_threshold", "7"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := store.SetSetting(ctx, "sched.default_concurrency", "6"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	sched := scheduler.New(store, nil, nil, nil, scheduler.SchedulerConfig{
		DefaultConcurrency: 5,
		MaxConcurrency:     10,
		RequestTimeout:     180 * time.Second,
		CircuitThreshold:   5,
		CircuitCooldown:    30 * time.Second,
		HealthWindow:       2 * time.Minute,
	})
	if err := LoadPersistedSchedulerOverrides(ctx, sched, store); err != nil {
		t.Fatalf("LoadPersistedSchedulerOverrides: %v", err)
	}
	sc := sched.Config()
	if sc.CircuitThreshold != 7 {
		t.Errorf("CircuitThreshold=%d, want 7", sc.CircuitThreshold)
	}
	if sc.DefaultConcurrency != 6 {
		t.Errorf("DefaultConcurrency=%d, want 6", sc.DefaultConcurrency)
	}
}
