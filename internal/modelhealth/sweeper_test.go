package modelhealth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/modelcatalog"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// newTestSweeper 构造带临时 SQLite 的扫描器（client=nil，applyVerdict 不依赖上游）。
func newTestSweeper(t *testing.T) (*Sweeper, *data.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := data.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := Config{
		ProbeCount: 3, ProbeInterval: 2 * time.Second, SweepInterval: 30 * time.Minute,
		SuccessRateFloor: 30, SuccessRateThreshold: 80, BadSweepToPermanent: 3,
		CooldownBase: 30 * time.Second,
	}
	return New(store, nil, cfg), store
}

// seedModel 插入一个模型行（满足 model_circuit 的外键约束）。
func seedModel(t *testing.T, store *data.Store, id string) {
	t.Helper()
	if _, err := store.UpsertModels(context.Background(), []data.Model{
		{ID: id, Object: "model", OwnedBy: "test"},
	}); err != nil {
		t.Fatalf("seed model %s: %v", id, err)
	}
}

func testCfg() Config {
	return Config{
		SuccessRateFloor: 30, SuccessRateThreshold: 80, BadSweepToPermanent: 3,
		CooldownBase: 30 * time.Second,
	}
}

// TestApplyVerdict_ClosedOnHealthy 验证成功率≥阈值 → closed，清零坏扫描。
func TestApplyVerdict_ClosedOnHealthy(t *testing.T) {
	s, store := newTestSweeper(t)
	ctx := context.Background()
	const model = "nvidia/llama-3.1-nemotron-70b-instruct"
	seedModel(t, store, model)

	// 先置于 open + 坏扫描 2
	_ = store.UpsertModelCircuit(ctx, data.ModelCircuit{
		Model: model, State: data.CircuitOpen, BadSweepCount: 2, OpenUntil: time.Now().Unix() + 300,
	})

	if err := s.applyVerdict(ctx, model, 90, testCfg()); err != nil {
		t.Fatalf("applyVerdict: %v", err)
	}
	mc, _ := store.GetModelCircuit(ctx, model)
	if mc.State != data.CircuitClosed {
		t.Fatalf("健康率 90 应转 closed，实为 %s", mc.State)
	}
	if mc.BadSweepCount != 0 {
		t.Fatalf("健康应清零坏扫描，实为 %d", mc.BadSweepCount)
	}
	if mc.OpenUntil != 0 {
		t.Fatalf("closed 应清 OpenUntil，实为 %d", mc.OpenUntil)
	}
}

// TestApplyVerdict_OpenOnMidRange 验证地板≤率<阈值 → 临时 open（指数退避冷却）。
func TestApplyVerdict_OpenOnMidRange(t *testing.T) {
	s, store := newTestSweeper(t)
	ctx := context.Background()
	const model = "m/mid"
	seedModel(t, store, model)

	// 率 50（30≤50<80），新记录 BadSweepCount=0 → 冷却 = base = 30s
	if err := s.applyVerdict(ctx, model, 50, testCfg()); err != nil {
		t.Fatalf("applyVerdict: %v", err)
	}
	mc, _ := store.GetModelCircuit(ctx, model)
	if mc.State != data.CircuitOpen {
		t.Fatalf("率 50 应转 open，实为 %s", mc.State)
	}
	now := time.Now().Unix()
	if mc.OpenUntil < now+25 || mc.OpenUntil > now+35 {
		t.Fatalf("BadSweepCount=0 冷却应≈30s，OpenUntil 偏移 %d", mc.OpenUntil-now)
	}
}

// TestApplyVerdict_PermanentAfterRepeatedBadSweeps 验证率<地板连续达阈值 → 永久熔断。
func TestApplyVerdict_PermanentAfterRepeatedBadSweeps(t *testing.T) {
	s, store := newTestSweeper(t)
	ctx := context.Background()
	const model = "m/bad"
	seedModel(t, store, model)
	cfg := testCfg() // BadSweepToPermanent=3

	for i := 1; i <= 3; i++ {
		if err := s.applyVerdict(ctx, model, 10, cfg); err != nil { // 10 < floor 30
			t.Fatalf("apply #%d: %v", i, err)
		}
		mc, _ := store.GetModelCircuit(ctx, model)
		switch i {
		case 1, 2:
			if mc.State != data.CircuitOpen {
				t.Fatalf("第 %d 次坏扫描应仍 open，实为 %s", i, mc.State)
			}
			if mc.BadSweepCount != i {
				t.Fatalf("坏扫描应=%d，实为 %d", i, mc.BadSweepCount)
			}
		case 3:
			if mc.State != data.CircuitPermanent || !mc.Permanent {
				t.Fatalf("第 3 次应永久熔断，实为 %s permanent=%v", mc.State, mc.Permanent)
			}
		}
	}
}

// TestApplyVerdict_PermanentNotReopenedByGoodRate 验证永久熔断仅人工复位解除：
// 即便后续成功率恢复，也不自动转 closed（仅清坏扫描计数）。
func TestApplyVerdict_PermanentNotReopenedByGoodRate(t *testing.T) {
	s, store := newTestSweeper(t)
	ctx := context.Background()
	const model = "m/perm"
	seedModel(t, store, model)

	_ = store.UpsertModelCircuit(ctx, data.ModelCircuit{
		Model: model, State: data.CircuitPermanent, Permanent: true, BadSweepCount: 3,
	})

	if err := s.applyVerdict(ctx, model, 95, testCfg()); err != nil {
		t.Fatalf("applyVerdict: %v", err)
	}
	mc, _ := store.GetModelCircuit(ctx, model)
	if mc.State != data.CircuitPermanent {
		t.Fatalf("永久熔断不应被高成功率解除，实为 %s", mc.State)
	}
	if mc.BadSweepCount != 0 {
		t.Fatalf("高成功率应清零坏扫描计数，实为 %d", mc.BadSweepCount)
	}
}

// TestNextCooldown_ExponentialBackoffAndCap 验证指数退避（每次坏扫描翻倍，封顶 10min）。
func TestNextCooldown_ExponentialBackoffAndCap(t *testing.T) {
	cfg := testCfg() // CooldownBase=30s
	cases := []struct {
		name    string
		badSwp  int
		wantSec int64 // 期望冷却秒数（OpenUntil-now 的近似）
	}{
		{"zero", 0, 30},
		{"one", 1, 30},  // for i:=1; i<1 不执行
		{"two", 2, 60},  // i=1 → *2
		{"three", 3, 120}, // i=1,2 → *4
		{"five", 5, 480}, // i=1..4 → *16 = 480
		{"six_cap", 6, 600}, // 960 > 600 → 封顶 10min
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mc := data.ModelCircuit{BadSweepCount: c.badSwp}
			got := nextCooldown(&mc, cfg)
			now := time.Now().Unix()
			diff := got - now
			if diff < c.wantSec-3 || diff > c.wantSec+3 {
				t.Fatalf("badSweep=%d 冷却应≈%ds，得到 %d", c.badSwp, c.wantSec, diff)
			}
		})
	}
}

// TestRatePct 验证成功率百分比计算（含除零保护）。
func TestRatePct(t *testing.T) {
	if got := ratePct(3, 4); got != 75 {
		t.Fatalf("ratePct(3,4) 应 75，得到 %d", got)
	}
	if got := ratePct(0, 0); got != 0 {
		t.Fatalf("ratePct(0,0) 应 0，得到 %d", got)
	}
	if got := ratePct(5, 0); got != 0 {
		t.Fatalf("ratePct(5,0) 应 0（除零保护），得到 %d", got)
	}
	if got := ratePct(0, 5); got != 0 {
		t.Fatalf("ratePct(0,5) 应 0，得到 %d", got)
	}
}

// TestInterfacesFor 验证能力→探测接口映射（不存在的接口不返回）。
func TestInterfacesFor(t *testing.T) {
	cases := []struct {
		cap  string
		want int
	}{
		{modelcatalog.CapChat, 1},
		{modelcatalog.CapReasoning, 1},
		{modelcatalog.CapCode, 1},
		{modelcatalog.CapVision, 1},
		{modelcatalog.CapEmbedding, 1},
		{modelcatalog.CapRerank, 1},
		{"safety", 0},     // 无稳定探测路径
		{"reward", 0},
		{"nonexistent", 0},
	}
	for _, c := range cases {
		got := interfacesFor(c.cap)
		if len(got) != c.want {
			t.Errorf("interfacesFor(%s) 应 %d 个接口，得到 %d", c.cap, c.want, len(got))
		}
	}
	// chat 类应走 CapChat
	if ifaces := interfacesFor(modelcatalog.CapChat); len(ifaces) != 1 || ifaces[0].cap != upstream.CapChat {
		t.Errorf("CapChat 应映射到 upstream.CapChat")
	}
	// embedding 类应走 CapEmbedding
	if ifaces := interfacesFor(modelcatalog.CapEmbedding); len(ifaces) != 1 || ifaces[0].cap != upstream.CapEmbedding {
		t.Errorf("CapEmbedding 应映射到 upstream.CapEmbedding")
	}
}

// TestApplyDefaults 验证零值字段填充默认值。
func TestApplyDefaults(t *testing.T) {
	var cfg Config
	applyDefaults(&cfg)
	if cfg.ProbeCount != 3 {
		t.Errorf("ProbeCount 默认应 3，得到 %d", cfg.ProbeCount)
	}
	if cfg.ProbeInterval != 2*time.Second {
		t.Errorf("ProbeInterval 默认应 2s，得到 %v", cfg.ProbeInterval)
	}
	if cfg.SweepInterval != 30*time.Minute {
		t.Errorf("SweepInterval 默认应 30min，得到 %v", cfg.SweepInterval)
	}
	if cfg.SuccessRateFloor != 30 {
		t.Errorf("Floor 默认应 30，得到 %d", cfg.SuccessRateFloor)
	}
	if cfg.SuccessRateThreshold != 80 {
		t.Errorf("Threshold 默认应 80，得到 %d", cfg.SuccessRateThreshold)
	}
	if cfg.BadSweepToPermanent != 3 {
		t.Errorf("BadSweepToPermanent 默认应 3，得到 %d", cfg.BadSweepToPermanent)
	}
	if cfg.CooldownBase != 30*time.Second {
		t.Errorf("CooldownBase 默认应 30s，得到 %v", cfg.CooldownBase)
	}

	// 非零字段不被覆盖
	cfg2 := Config{ProbeCount: 9, SuccessRateThreshold: 95}
	applyDefaults(&cfg2)
	if cfg2.ProbeCount != 9 || cfg2.SuccessRateThreshold != 95 {
		t.Errorf("非零字段不应被覆盖: ProbeCount=%d Threshold=%d", cfg2.ProbeCount, cfg2.SuccessRateThreshold)
	}
}

// TestSweeper_UpdateConfig 验证运行时热改仅覆盖非零字段，零值字段保持不变。
func TestSweeper_UpdateConfig(t *testing.T) {
	s, _ := newTestSweeper(t)
	orig := s.Config()

	s.UpdateConfig(Config{ProbeCount: 9, SuccessRateThreshold: 90})
	got := s.Config()
	if got.ProbeCount != 9 {
		t.Errorf("ProbeCount 应更新为 9，得到 %d", got.ProbeCount)
	}
	if got.SuccessRateThreshold != 90 {
		t.Errorf("Threshold 应更新为 90，得到 %d", got.SuccessRateThreshold)
	}
	// 未改字段保持
	if got.ProbeInterval != orig.ProbeInterval {
		t.Errorf("ProbeInterval 不应变，原 %v 现 %v", orig.ProbeInterval, got.ProbeInterval)
	}
	if got.BadSweepToPermanent != orig.BadSweepToPermanent {
		t.Errorf("BadSweepToPermanent 不应变，原 %d 现 %d", orig.BadSweepToPermanent, got.BadSweepToPermanent)
	}
}
