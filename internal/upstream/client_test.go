package upstream

import (
	"net/http"
	"testing"
)

// TestRateLimitRemaining 覆盖 X-RateLimit-Remaining 头解析契约：
// 头缺失/空 → -1（无信号，调用方跳过校准）；合法数字 → 原值；
// 非数字/垃圾头 → -1（旧实现静默返回 0，会把令牌桶校准为 0 误杀健康 Key）。
func TestRateLimitRemaining(t *testing.T) {
	cases := []struct {
		name string
		h    http.Header
		want int
	}{
		{"missing", http.Header{}, -1},
		{"empty", header("X-RateLimit-Remaining", ""), -1},
		{"valid-39", header("X-RateLimit-Remaining", "39"), 39},
		{"valid-0", header("X-RateLimit-Remaining", "0"), 0},
		{"with-space", header("X-RateLimit-Remaining", " 12 "), 12},
		{"garbage", header("X-RateLimit-Remaining", "unlimited"), -1}, // 旧实现会返 0 → 误杀
		{"mixed", header("X-RateLimit-Remaining", "abc39"), -1},       // 旧实现会返 39 → 错误校准
		{"negative", header("X-RateLimit-Remaining", "-5"), -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Response{Header: c.h}
			if got := r.RateLimitRemaining(); got != c.want {
				t.Fatalf("RateLimitRemaining() = %d, want %d", got, c.want)
			}
		})
	}
}

func header(k, v string) http.Header {
	h := http.Header{}
	h.Set(k, v) // Set 规范化键名，与真实 HTTP 响应头一致
	return h
}
