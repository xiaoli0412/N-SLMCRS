package ratelimit

import (
	"testing"
	"time"
)

// TestTokenBucket_AllowConsumesAndBlocksWhenEmpty 验证令牌桶消费语义：
// 初始满桶允许消费至容量上限，耗尽后拒绝，Available 同步下降。
// 这是限流热路径（scheduler.selectKeys 逐 Key 调 Allow）的核心不变量。
func TestTokenBucket_AllowConsumesAndBlocksWhenEmpty(t *testing.T) {
	b := NewTokenBucket(2) // 容量 2，回填 2/60≈0.033/s（本例不依赖回填）

	if !b.Allow(2) {
		t.Fatal("满桶应允许消费 2")
	}
	if b.AllowOne() {
		t.Fatal("耗尽后应拒绝 AllowOne")
	}
	if av := b.Available(); av > 0.5 {
		t.Fatalf("耗尽后 Available 应≈0，得到 %v", av)
	}
}

// TestTokenBucket_RefillOverTime 验证按时间差回填令牌（refill）。
// rpm=600 → 10 token/s；耗尽后等 ~150ms 应回填 ≥1 个，AllowOne 重新成功。
// 锁定 refill 的时间驱动语义（与 Calibrate/Available 共享同一路径）。
func TestTokenBucket_RefillOverTime(t *testing.T) {
	b := NewTokenBucket(600) // 10 token/s

	if !b.Allow(600) {
		t.Fatal("满桶应允许消费 600")
	}
	if b.AllowOne() {
		t.Fatal("耗尽后应拒绝")
	}

	time.Sleep(150 * time.Millisecond) // 回填 ~1.5 token
	if !b.AllowOne() {
		t.Fatal("回填后应允许消费 1")
	}
}

// TestTokenBucket_PeekDoesNotConsume 验证 Peek（v0.13 B2）只读不消费：
// selectKeys 先 peek 筛候选，再仅对选中的 N 个消费，避免对未选中候选白耗配额。
// 不变式：多次 Peek 不改变 Available；而 Allow 会。
func TestTokenBucket_PeekDoesNotConsume(t *testing.T) {
	b := NewTokenBucket(1) // 容量 1，满桶

	if !b.Peek(1) {
		t.Fatal("满桶 Peek(1) 应 true")
	}
	if !b.Peek(1) {
		t.Fatal("Peek 不应消费，第二次 Peek(1) 仍应 true")
	}
	if av := b.Available(); av < 0.9 {
		t.Fatalf("Peek 不应改变余量，得到 %v", av)
	}
	// 真正消费后才减少
	if !b.Allow(1) {
		t.Fatal("满桶 Allow(1) 应 true")
	}
	if b.Peek(1) {
		t.Fatal("耗尽后 Peek(1) 应 false")
	}
}

// TestManager_HasTokensDoesNotConsume 验证 Manager.HasTokens（peek）不消费：
// 模拟 selectKeys 的 peek→选中 N 个→对 N 个 Allow 的流程，未选中的令牌不减少。
func TestManager_HasTokensDoesNotConsume(t *testing.T) {
	m := NewManager(40)
	m.Register(1, 1) // 容量 1
	m.Register(2, 1)

	// peek 两个候选都有余量
	if !m.HasTokens(1, 1) || !m.HasTokens(2, 1) {
		t.Fatal("两个 Key 初始都应有余量")
	}
	// 只消费 key 1（被选中），key 2 未选中不应被消费
	if !m.Allow(1, 1) {
		t.Fatal("key 1 Allow(1) 应成功")
	}
	// key 2 仍满 → Allow 应成功
	if !m.Allow(2, 1) {
		t.Fatal("key 2 未被选中，令牌不应被 peek 消费，Allow(1) 应仍成功（B2）")
	}
}

// TestTokenBucket_HasAdmissionHeadroom 验证 v0.14 策略头寸准入：
// rpm=10 桶容量 10，headroom=0.8 → 地板=2，须 tokens≥3 才准入（消费后仍≥2）。
// 满桶 10→3 共 8 次准入（=80%），第 9 次 tokens=2<3 阻塞。
// headroom=1.0 时骑满（10 次准入）。与 Rust has_admission 行为对齐。
func TestTokenBucket_HasAdmissionHeadroom(t *testing.T) {
	b := NewTokenBucket(10) // 容量 10

	// headroom=0.8：地板 2，须 ≥3 → 8 次准入（10→2）
	for i := 1; i <= 8; i++ {
		if !b.HasAdmission(0.8) {
			t.Fatalf("第 %d 次应准入（headroom 0.8）", i)
		}
		b.Allow(1)
	}
	if b.HasAdmission(0.8) {
		t.Fatal("第 9 次 tokens=2 应被头寸阻塞（须 ≥3）")
	}

	// headroom=1.0：骑满，10 次准入
	b2 := NewTokenBucket(10)
	for i := 1; i <= 10; i++ {
		if !b2.HasAdmission(1.0) {
			t.Fatalf("headroom 1.0 第 %d 次应准入", i)
		}
		b2.Allow(1)
	}
	if b2.HasAdmission(1.0) {
		t.Fatal("headroom 1.0 第 11 次 tokens=0 应阻塞")
	}
}

// TestManager_HasAdmission 验证 Manager.HasAdmission 头寸准入。
func TestManager_HasAdmission(t *testing.T) {
	m := NewManager(40)
	m.Register(1, 10) // 容量 10

	// headroom 0.8 → 地板 2，须 ≥3；满桶准入
	if !m.HasAdmission(1, 0.8) {
		t.Fatal("满桶应满足 headroom 0.8 准入")
	}
	m.Allow(1, 8) // 10→2
	if m.HasAdmission(1, 0.8) {
		t.Fatal("tokens=2 应不满足 headroom 0.8（须 ≥3）")
	}
}

// TestTokenBucket_CalibrateOnlyTightens 验证上游 X-RateLimit-Remaining 校准只取紧不取松：
// 上游余量小于本地估算时下调；上游余量大于本地时不变（不盲目放宽，避免超限）。
func TestTokenBucket_CalibrateOnlyTightens(t *testing.T) {
	b := NewTokenBucket(1000) // 容量 1000，初始满桶

	// 上游余量 5 < 本地 1000 → 下调到 5
	b.Calibrate(5)
	if av := b.Available(); av > 5.5 || av < 4.5 {
		t.Fatalf("Calibrate(5) 后应≈5，得到 %v", av)
	}

	// 上游余量 500 > 本地 5 → 不放宽，仍为 5
	b.Calibrate(500)
	if av := b.Available(); av > 5.5 || av < 4.5 {
		t.Fatalf("Calibrate(500) 不应放宽，应仍≈5，得到 %v", av)
	}
}

// TestTokenBucket_AvailableClampsToCapacity 验证回填不超过容量上限。
// 耗尽后等待，Available 不应超过 capacity（钳位）。
func TestTokenBucket_AvailableClampsToCapacity(t *testing.T) {
	const rpm = 120 // 2 token/s, 容量 120
	b := NewTokenBucket(rpm)

	// 耗尽绝大部分，留 1 个
	if !b.Allow(rpm - 1) {
		t.Fatal("应允许消费 capacity-1")
	}
	// 等待回填若干（不会超过 capacity）
	time.Sleep(150 * time.Millisecond)
	if av := b.Available(); av > float64(rpm)+0.01 {
		t.Fatalf("Available 不应超过容量 %d，得到 %v", rpm, av)
	}
}

// TestManager_AllowAndLazyCreate 验证 Manager 的逐 Key 消费 + 未注册 Key 按默认 RPM 懒创建。
func TestManager_AllowAndLazyCreate(t *testing.T) {
	m := NewManager(40)
	m.Register(1, 2) // key 1: 容量 2

	// key 1 注册桶：允许 2，第 3 次拒绝
	if !m.Allow(1, 2) {
		t.Fatal("key 1 应允许消费 2")
	}
	if m.Allow(1, 1) {
		t.Fatal("key 1 耗尽后应拒绝")
	}

	// key 2 未注册 → 按默认 40 懒创建，允许消费
	if !m.Allow(2, 1) {
		t.Fatal("未注册 key 2 应按默认懒创建并允许")
	}
}

// TestManager_AllowKeysPicksFirstNWithCapacity 验证从候选集中选出前 n 个有余量的 Key
// （scheduler 选 N 路并发依赖此）。
func TestManager_AllowKeysPicksFirstNWithCapacity(t *testing.T) {
	m := NewManager(40)
	m.Register(1, 1) // key 1: 容量 1
	m.Register(2, 1)
	m.Register(3, 1)

	// 先耗尽 key 1
	m.Allow(1, 1)

	got := m.AllowKeys([]int64{1, 2, 3}, 2)
	// key 1 无余量被跳过，应返回 key 2、3
	if len(got) != 2 {
		t.Fatalf("应选出 2 个有余量 Key，得到 %d: %v", len(got), got)
	}
	for _, id := range got {
		if id == 1 {
			t.Error("耗尽的 key 1 不应被选中")
		}
	}
}

// TestManager_UnregisterThenReRegisterDefaults 验证移除后再次访问按默认懒创建。
func TestManager_UnregisterThenReRegisterDefaults(t *testing.T) {
	m := NewManager(40)
	m.Register(1, 1) // 容量 1
	m.Unregister(1)

	// 移除后 Allow → bucket() 懒创建为默认 40，满桶允许
	if !m.Allow(1, 1) {
		t.Fatal("Unregister 后再次访问应按默认懒创建并允许")
	}
}

// TestManager_Snapshot 验证面板快照（运维面板展示用）：注册的 Key 全部出现，余量随消费下降。
func TestManager_Snapshot(t *testing.T) {
	m := NewManager(40)
	m.Register(1, 10)
	m.Register(2, 10)
	m.Allow(1, 4) // key 1 余 6

	snap := m.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("快照应含 2 个 Key，得到 %d: %v", len(snap), snap)
	}
	if _, ok := snap[1]; !ok {
		t.Error("快照缺 key 1")
	}
	if _, ok := snap[2]; !ok {
		t.Error("快照缺 key 2")
	}
	// key 1 消费 4 后余量应 < key 2
	if snap[1] >= snap[2] {
		t.Fatalf("key 1 消费后余量应 < key 2，得到 k1=%v k2=%v", snap[1], snap[2])
	}
}
