package ratelimit

import (
	"sync"
	"time"
)

// SlidingWindow 滑动窗口计数器，用于实时成功率统计（熔断器决策依据）。
//
// 维护一个时间窗口内的请求结果序列，按时间淘汰过期项，实时计算成功率。
// 内存型，不做持久化（重启后重新统计即可，足够熔断决策）。
type SlidingWindow struct {
	mu      sync.Mutex
	window  time.Duration
	entries []windowEntry
}

type windowEntry struct {
	ts      time.Time
	success bool
}

// NewSlidingWindow 创建滑动窗口。window 为统计时长（如 2 分钟）。
func NewSlidingWindow(window time.Duration) *SlidingWindow {
	return &SlidingWindow{window: window}
}

// Record 记录一次请求结果。
func (w *SlidingWindow) Record(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, windowEntry{ts: time.Now(), success: success})
	w.evict()
}

// Stats 返回窗口内的统计。
type Stats struct {
	Total        int
	Success      int
	Failed       int
	SuccessRate  float64 // 0-100
}

// Stats 计算当前窗口统计。
func (w *SlidingWindow) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evict()
	var s Stats
	s.Total = len(w.entries)
	for _, e := range w.entries {
		if e.success {
			s.Success++
		} else {
			s.Failed++
		}
	}
	if s.Total > 0 {
		s.SuccessRate = 100.0 * float64(s.Success) / float64(s.Total)
	}
	return s
}

// evict 淘汰过期项（调用方持锁）。
func (w *SlidingWindow) evict() {
	cutoff := time.Now().Add(-w.window)
	i := 0
	for i < len(w.entries) && w.entries[i].ts.Before(cutoff) {
		i++
	}
	if i > 0 {
		w.entries = w.entries[i:]
	}
}

// HealthTracker 跟踪每个 Key 的实时健康（成功率 + 连续失败）。
// 熔断器据此决策。
type HealthTracker struct {
	mu             sync.Mutex
	successWindow  map[int64]*SlidingWindow // keyID → 成功率窗口
	consecutiveFail map[int64]int           // keyID → 连续失败数
}

// NewHealthTracker 创建健康追踪器。
func NewHealthTracker(window time.Duration) *HealthTracker {
	// 注意：window 通过每个 window 持有；这里只持有 map
	_ = window
	return &HealthTracker{
		successWindow:  make(map[int64]*SlidingWindow),
		consecutiveFail: make(map[int64]int),
	}
}

// window 取（或懒创建）某 Key 的成功率窗口。
func (h *HealthTracker) window(keyID int64, w time.Duration) *SlidingWindow {
	h.mu.Lock()
	defer h.mu.Unlock()
	sw, ok := h.successWindow[keyID]
	if !ok {
		sw = NewSlidingWindow(w)
		h.successWindow[keyID] = sw
	}
	return sw
}

// Record 记录 Key 的请求结果。window 为统计窗口时长。
func (h *HealthTracker) Record(keyID int64, success bool, window time.Duration) {
	sw := h.window(keyID, window)
	sw.Record(success)
	h.mu.Lock()
	if success {
		h.consecutiveFail[keyID] = 0
	} else {
		h.consecutiveFail[keyID]++
	}
	h.mu.Unlock()
}

// SuccessRate 返回 Key 的实时成功率。
func (h *HealthTracker) SuccessRate(keyID int64, window time.Duration) float64 {
	return h.window(keyID, window).Stats().SuccessRate
}

// ConsecutiveFailures 返回 Key 的连续失败数。
func (h *HealthTracker) ConsecutiveFailures(keyID int64) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.consecutiveFail[keyID]
}
