package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/kernelctl"
	"github.com/nslmcrs/gateway/internal/ratelimit"
)

// fakeRuntime 测试用 RuntimeOverrides 桩。
type fakeRuntime struct {
	conc int
	wb   float64
}

func (f fakeRuntime) Concurrency() int          { return f.conc }
func (f fakeRuntime) WeightBoost(int64) float64 { return f.wb }

// TestEffectiveConcurrency_Precedence 验证并发度生效优先级（Phase 1 回归）：
// Runtime 覆盖优先于配置默认，且钳位到 MaxConcurrency；未覆盖时回退配置默认。
//
// 这锁定 UpdateConfig（落库的配置默认）与 Runtime.SetConcurrency（Auto-Pilot 内存覆盖）
// 两套并发度通道的读时优先级：effectiveConcurrency() 始终 Runtime 优先、回退配置默认，
// 二者不会"互踩"——admin 改配置默认不会被 Runtime 覆盖掩盖（除非 autopilot 主动设置覆盖）。
func TestEffectiveConcurrency_Precedence(t *testing.T) {
	s := &Scheduler{config: SchedulerConfig{
		DefaultConcurrency: 5,
		MaxConcurrency:     10,
		RequestTimeout:     180 * time.Second,
		CircuitThreshold:   5,
		CircuitCooldown:    30 * time.Second,
		HealthWindow:       2 * time.Minute,
	}}

	// 无 Runtime 覆盖 → 配置默认 5
	if got := s.effectiveConcurrency(); got != 5 {
		t.Fatalf("无覆盖时应为配置默认 5，得到 %d", got)
	}

	// Runtime 覆盖 7 → 生效（优先于配置默认）
	s.runtime = fakeRuntime{conc: 7}
	if got := s.effectiveConcurrency(); got != 7 {
		t.Fatalf("Runtime 覆盖 7 应优先生效，得到 %d", got)
	}

	// Runtime 覆盖超过 Max(10) → 钳位到 10
	s.runtime = fakeRuntime{conc: 99}
	if got := s.effectiveConcurrency(); got != 10 {
		t.Fatalf("超过上限应钳位到 10，得到 %d", got)
	}

	// Runtime 返回 <=0（未覆盖）→ 回退配置默认
	s.runtime = fakeRuntime{conc: 0}
	if got := s.effectiveConcurrency(); got != 5 {
		t.Fatalf("Runtime<=0 应回退配置默认 5，得到 %d", got)
	}
}

// newTestScheduler 构造带临时 SQLite 的调度器（client=nil，selectKeys/markHealthy 不依赖上游）。
func newTestScheduler(t *testing.T) (*Scheduler, *data.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := data.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	health := ratelimit.NewHealthTracker(2 * time.Minute)
	rl := ratelimit.NewManager(40)
	s := New(store, nil, rl, health, SchedulerConfig{
		DefaultConcurrency: 5,
		MaxConcurrency:     10,
		RequestTimeout:     180 * time.Second,
		CircuitThreshold:   5,
		CircuitCooldown:    30 * time.Second,
		HealthWindow:       2 * time.Minute,
	})
	return s, store
}

// addKey 添加一个密钥并注册到限流器，返回其 ID。
func addKey(t *testing.T, s *Scheduler, store *data.Store, kv string) int64 {
	t.Helper()
	k, err := store.AddUpstreamKey(context.Background(), kv, "t", "e", 40)
	if err != nil {
		t.Fatalf("AddUpstreamKey: %v", err)
	}
	s.rl.Register(k.ID, 0)
	return k.ID
}

// TestSelectKeys_HalfOpenPromotion 熔断冷却到期 → 转半开放行，且重置旧连续失败计数。
// 回归旧实现缺陷：circuit_open 一旦设置便永久跳过，CoolingUntil/退避形同虚设。
func TestSelectKeys_HalfOpenPromotion(t *testing.T) {
	s, store := newTestScheduler(t)
	ctx := context.Background()
	id := addKey(t, s, store, "nvapi-half1")
	store.UpdateUpstreamKeyStatus(ctx, id, "circuit_open", 5, time.Now().Unix()-1) // 冷却到期
	for i := 0; i < 5; i++ {
		s.health.Record(id, false, 2*time.Minute) // 旧连续失败 5
	}
	if c := s.health.ConsecutiveFailures(id); c != 5 {
		t.Fatalf("预设 consec=5, 得到 %d", c)
	}

	cands, err := s.selectKeys(ctx, "meta/x")
	if err != nil {
		t.Fatalf("selectKeys: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("应放行 1 个半开试探, 得到 %d 候选", len(cands))
	}
	if cands[0].Status != "half_open" {
		t.Fatalf("候选应为 half_open, 得到 %s", cands[0].Status)
	}
	if c := s.health.ConsecutiveFailures(id); c != 0 {
		t.Fatalf("半开应重置 consec=0（干净试探起点）, 得到 %d", c)
	}
	dbk, _ := store.GetUpstreamKey(ctx, id)
	if dbk.Status != "half_open" {
		t.Fatalf("DB 状态应为 half_open, 得到 %s", dbk.Status)
	}
}

// TestSelectKeys_CircuitOpenStillCooling 冷却未到期应继续跳过，不转半开。
func TestSelectKeys_CircuitOpenStillCooling(t *testing.T) {
	s, store := newTestScheduler(t)
	ctx := context.Background()
	id := addKey(t, s, store, "nvapi-cool")
	store.UpdateUpstreamKeyStatus(ctx, id, "circuit_open", 5, time.Now().Unix()+60) // 仍在冷却

	cands, err := s.selectKeys(ctx, "meta/x")
	if err != nil {
		t.Fatalf("selectKeys: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("冷却中应不放行, 得到 %d 候选", len(cands))
	}
	dbk, _ := store.GetUpstreamKey(ctx, id)
	if dbk.Status != "circuit_open" {
		t.Fatalf("冷却中应保持 circuit_open, 得到 %s", dbk.Status)
	}
}

// TestSelectKeys_OnlyOneHalfOpenTrial 多个冷却到期的密钥每轮只放行 1 个半开试探。
func TestSelectKeys_OnlyOneHalfOpenTrial(t *testing.T) {
	s, store := newTestScheduler(t)
	ctx := context.Background()
	id1 := addKey(t, s, store, "nvapi-ho1")
	id2 := addKey(t, s, store, "nvapi-ho2")
	past := time.Now().Unix() - 1
	store.UpdateUpstreamKeyStatus(ctx, id1, "circuit_open", 5, past)
	store.UpdateUpstreamKeyStatus(ctx, id2, "circuit_open", 5, past)

	cands, err := s.selectKeys(ctx, "meta/x")
	if err != nil {
		t.Fatalf("selectKeys: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("应只放行 1 个半开试探, 得到 %d 候选", len(cands))
	}
}

// TestMarkHealthy_HalfOpenToActive 半开试探成功 → 闭合为 active。
func TestMarkHealthy_HalfOpenToActive(t *testing.T) {
	s, store := newTestScheduler(t)
	ctx := context.Background()
	id := addKey(t, s, store, "nvapi-mh")
	store.UpdateUpstreamKeyStatus(ctx, id, "half_open", 0, 0)
	key, _ := store.GetUpstreamKey(ctx, id) // Status=half_open

	s.markHealthy(ctx, key)
	if key.Status != "active" {
		t.Fatalf("markHealthy 后应 active, 得到 %s", key.Status)
	}
	dbk, _ := store.GetUpstreamKey(ctx, id)
	if dbk.Status != "active" {
		t.Fatalf("DB 应 active, 得到 %s", dbk.Status)
	}
}

// ─── v0.12 Rust 控制面接线（/reserve + /report）─────────────────────────

// kernelClient 构造指向 url 的客户端；failClosed 私有字段经 KERNEL_FAIL_CLOSED env 置位。
func kernelClient(t *testing.T, url string, failClosed bool) *kernelctl.Client {
	t.Helper()
	t.Setenv("KERNEL_DISABLE", "")
	t.Setenv("KERNEL_URL", url)
	if failClosed {
		t.Setenv("KERNEL_FAIL_CLOSED", "1")
	} else {
		t.Setenv("KERNEL_FAIL_CLOSED", "0")
	}
	return kernelctl.NewFromEnv()
}

// fakeReserveKernel 启动桩 sidecar：/reserve 返回固定有序 key_id + 半开变更。
func fakeReserveKernel(t *testing.T, reserved []int64, changes []kernelctl.KeyBreakerChange) (*kernelctl.Client, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/reserve", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(kernelctl.ReserveResp{Reserved: reserved, KeyBreakerChanges: changes})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return kernelClient(t, srv.URL, false), srv.Close
}

// TestSelectKeys_FailOpenDegradesToGo kernel 不可达 + fail-open → 降级回 Go 路径，
// 既有半开提升逻辑仍生效（回归降级透明）。
func TestSelectKeys_FailOpenDegradesToGo(t *testing.T) {
	s, store := newTestScheduler(t)
	c := kernelClient(t, "http://127.0.0.1:1", false) // 闭端口 + fail-open
	s.SetKernel(c)
	ctx := context.Background()
	id := addKey(t, s, store, "nvapi-fo")
	store.UpdateUpstreamKeyStatus(ctx, id, "circuit_open", 5, time.Now().Unix()-1) // 冷却到期

	cands, err := s.selectKeys(ctx, "meta/x")
	if err != nil {
		t.Fatalf("fail-open 应降级不报错, 得到 %v", err)
	}
	if len(cands) != 1 || cands[0].Status != "half_open" {
		t.Fatalf("降级应走 Go 半开提升, 得到 %d 候选", len(cands))
	}
}

// TestSelectKeys_FailClosedDeniesOnUnreachable kernel 不可达 + fail-closed → 拒绝准入。
func TestSelectKeys_FailClosedDeniesOnUnreachable(t *testing.T) {
	s, store := newTestScheduler(t)
	c := kernelClient(t, "http://127.0.0.1:1", true) // 闭端口 + fail-closed
	s.SetKernel(c)
	addKey(t, s, store, "nvapi-fc")

	_, err := s.selectKeys(context.Background(), "meta/x")
	if err == nil {
		t.Fatal("fail-closed 应返回错误")
	}
}

// TestReportResults_EchoWritesBreakerState /report 返回的熔断变更 echo 回写 upstream_keys。
func TestReportResults_EchoWritesBreakerState(t *testing.T) {
	s, store := newTestScheduler(t)
	ctx := context.Background()
	id := addKey(t, s, store, "nvapi-echo")

	// 桩 /report 返回该 id 的 circuit_open 变更
	mux := http.NewServeMux()
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(kernelctl.ReportResp{
			KeyBreakerChanges: []kernelctl.KeyBreakerChange{
				{KeyID: id, Status: "circuit_open", ConsecutiveFail: 5, CoolingUntil: 12345},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s.SetKernel(kernelClient(t, srv.URL, false))

	s.reportResults(ctx, "trace", []kernelctl.ReportItem{
		{KeyID: id, Success: false, Status: "error"},
	})
	dbk, _ := store.GetUpstreamKey(ctx, id)
	if dbk.Status != "circuit_open" || dbk.ConsecutiveFail != 5 || dbk.CoolingUntil != 12345 {
		t.Fatalf("echo 应回写 circuit_open/5/12345, 得到 %s/%d/%d",
			dbk.Status, dbk.ConsecutiveFail, dbk.CoolingUntil)
	}
}

// TestSelectKeys_ReserveUsesKernelOrdering kernel 在线 → 用 /reserve 返回的顺序，
// 跳过 Go rl.Allow/weightedShuffle。
func TestSelectKeys_ReserveUsesKernelOrdering(t *testing.T) {
	s, store := newTestScheduler(t)
	ctx := context.Background()
	id1 := addKey(t, s, store, "nvapi-r1")
	id2 := addKey(t, s, store, "nvapi-r2")
	// 桩返回反序 [id2, id1]
	c, _ := fakeReserveKernel(t, []int64{id2, id1}, nil)
	s.SetKernel(c)

	cands, err := s.selectKeys(ctx, "meta/x")
	if err != nil {
		t.Fatalf("selectKeys: %v", err)
	}
	if len(cands) != 2 || cands[0].ID != id2 || cands[1].ID != id1 {
		t.Fatalf("应按 /reserve 返回顺序 [id2,id1], 得到 %v", cands)
	}
}
