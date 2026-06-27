package inflight

import "testing"

func TestIncDecGet(t *testing.T) {
	// 重置到 0（防其他测试污染；包级原子变量）
	for Get() > 0 {
		Dec()
	}
	if got := Get(); got != 0 {
		t.Fatalf("初始 Get = %d, want 0", got)
	}

	Inc()
	Inc()
	Inc()
	if got := Get(); got != 3 {
		t.Errorf("三次 Inc 后 Get = %d, want 3", got)
	}

	Dec()
	if got := Get(); got != 2 {
		t.Errorf("一次 Dec 后 Get = %d, want 2", got)
	}
}

func TestDecNegativeGuard(t *testing.T) {
	// 强制清零后再 Dec，不应变负
	for Get() > 0 {
		Dec()
	}
	Dec()
	Dec()
	if got := Get(); got != 0 {
		t.Errorf("负值保护失败：Get = %d, want 0", got)
	}
}
