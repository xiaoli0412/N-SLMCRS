package ratelimit

import (
	"testing"
	"time"
)

// TestSlidingWindow_RecordAndStats 验证窗口内计数与成功率统计。
// 这是熔断器决策依据（成功率驱动 open/closed 判定）。
func TestSlidingWindow_RecordAndStats(t *testing.T) {
	w := NewSlidingWindow(200 * time.Millisecond)

	w.Record(true)
	w.Record(true)
	w.Record(true)
	w.Record(false)

	s := w.Stats()
	if s.Total != 4 || s.Success != 3 || s.Failed != 1 {
		t.Fatalf("计数错误: total=%d success=%d failed=%d", s.Total, s.Success, s.Failed)
	}
	if got := s.SuccessRate; got < 74.9 || got > 75.1 {
		t.Fatalf("成功率应为 75，得到 %v", got)
	}
}

// TestSlidingWindow_EvictsExpired 验证过期项淘汰：
// 窗口外的旧记录被剔除，仅保留窗口内的新记录。
func TestSlidingWindow_EvictsExpired(t *testing.T) {
	w := NewSlidingWindow(100 * time.Millisecond)

	w.Record(true) // 旧成功
	time.Sleep(120 * time.Millisecond)
	w.Record(false) // 新失败

	s := w.Stats()
	if s.Total != 1 {
		t.Fatalf("旧项应被淘汰，total 应为 1，得到 %d", s.Total)
	}
	if s.Success != 0 || s.Failed != 1 {
		t.Fatalf("仅保留新失败: success=%d failed=%d", s.Success, s.Failed)
	}
	if s.SuccessRate != 0 {
		t.Fatalf("成功率应为 0，得到 %v", s.SuccessRate)
	}
}

// TestSlidingWindow_EmptyStatsZero 验证空窗口统计为零值（无除零）。
func TestSlidingWindow_EmptyStatsZero(t *testing.T) {
	w := NewSlidingWindow(200 * time.Millisecond)
	s := w.Stats()
	if s.Total != 0 || s.SuccessRate != 0 {
		t.Fatalf("空窗口应为零值，得到 %+v", s)
	}
}

// TestHealthTracker_RecordAndConsecutive 验证按 Key 健康跟踪：
// 成功率窗口 + 连续失败计数，二者独立维护；成功清零连续失败。
func TestHealthTracker_RecordAndConsecutive(t *testing.T) {
	h := NewHealthTracker(200 * time.Millisecond)
	const key int64 = 1
	const win = 200 * time.Millisecond

	// 3 成功
	for i := 0; i < 3; i++ {
		h.Record(key, true, win)
	}
	if got := h.SuccessRate(key, win); got < 99.9 {
		t.Fatalf("3 成功后成功率应≈100，得到 %v", got)
	}
	if h.ConsecutiveFailures(key) != 0 {
		t.Fatalf("成功后连续失败应为 0，得到 %d", h.ConsecutiveFailures(key))
	}

	// 2 失败：连续失败 2，成功率 3/5=60
	for i := 0; i < 2; i++ {
		h.Record(key, false, win)
	}
	if h.ConsecutiveFailures(key) != 2 {
		t.Fatalf("连续失败应为 2，得到 %d", h.ConsecutiveFailures(key))
	}
	if got := h.SuccessRate(key, win); got < 59.9 || got > 60.1 {
		t.Fatalf("3 成功 2 失败成功率应为 60，得到 %v", got)
	}

	// 成功 → 连续失败清零（成功率窗口不变，仍含历史）
	h.Record(key, true, win)
	if h.ConsecutiveFailures(key) != 0 {
		t.Fatalf("成功后连续失败应清零，得到 %d", h.ConsecutiveFailures(key))
	}
}

// TestHealthTracker_ResetConsecutive 验证半开探测清旧失败数：
// 给试探请求干净起点，否则 checkCircuitBreaker 会凭旧失败数立即重新熔断。
func TestHealthTracker_ResetConsecutive(t *testing.T) {
	h := NewHealthTracker(200 * time.Millisecond)
	const key int64 = 7
	const win = 200 * time.Millisecond

	for i := 0; i < 5; i++ {
		h.Record(key, false, win)
	}
	if h.ConsecutiveFailures(key) != 5 {
		t.Fatalf("连续失败应为 5，得到 %d", h.ConsecutiveFailures(key))
	}

	h.ResetConsecutive(key)
	if h.ConsecutiveFailures(key) != 0 {
		t.Fatalf("ResetConsecutive 后应为 0，得到 %d", h.ConsecutiveFailures(key))
	}
	// 连续失败清零，但成功率窗口历史保留
	if got := h.SuccessRate(key, win); got > 0.1 {
		t.Fatalf("ResetConsecutive 不应影响成功率窗口，仍应为 0（全失败），得到 %v", got)
	}
}

// TestHealthTracker_KeysIndependent 验证不同 Key 的健康状态互不干扰。
func TestHealthTracker_KeysIndependent(t *testing.T) {
	h := NewHealthTracker(200 * time.Millisecond)
	const win = 200 * time.Millisecond

	h.Record(1, false, win)
	h.Record(1, false, win)
	h.Record(2, true, win)

	if h.ConsecutiveFailures(1) != 2 {
		t.Fatalf("key 1 连续失败应为 2，得到 %d", h.ConsecutiveFailures(1))
	}
	if h.ConsecutiveFailures(2) != 0 {
		t.Fatalf("key 2 连续失败应为 0，得到 %d", h.ConsecutiveFailures(2))
	}
	if got := h.SuccessRate(2, win); got < 99.9 {
		t.Fatalf("key 2 成功率应≈100，得到 %v", got)
	}
}
