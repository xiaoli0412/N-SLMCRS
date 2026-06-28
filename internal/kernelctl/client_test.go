package kernelctl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// startFakeKernel 启动一个返回固定响应的 sidecar 桩，返回其客户端。
func startFakeKernel(t *testing.T) (*Client, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/verdict", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(VerdictResult{
			State: "open", OpenUntil: 123, BadSweepCount: 2, Permanent: false,
		})
	})
	mux.HandleFunc("/weighted-score", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]float64{"score": 42.5})
	})
	mux.HandleFunc("/circuit-check", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(CircuitCheckResult{
			ShouldOpen: true, CooldownSec: 60, CoolUntil: 999,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{baseURL: srv.URL, http: &http.Client{Timeout: 1000000000}}, srv.Close
}

func TestVerdict_OK(t *testing.T) {
	c, _ := startFakeKernel(t)
	v, ok := c.Verdict(context.Background(), 50, "closed", 1, 30, 80, 3, 30)
	if !ok {
		t.Fatal("应 ok=true")
	}
	if v.State != "open" || v.OpenUntil != 123 || v.BadSweepCount != 2 || v.Permanent {
		t.Fatalf("解码错误: %+v", v)
	}
}

func TestVerdict_DowngradeOnUnreachable(t *testing.T) {
	c := &Client{baseURL: "http://127.0.0.1:1", http: &http.Client{Timeout: 100000000}} // 闭端口
	_, ok := c.Verdict(context.Background(), 50, "closed", 1, 30, 80, 3, 30)
	if ok {
		t.Fatal("不可达应 ok=false（降级）")
	}
}

func TestWeightedScore_OK(t *testing.T) {
	c, _ := startFakeKernel(t)
	s, ok := c.WeightedScore(context.Background(), 100, 2, 1.0)
	if !ok || s != 42.5 {
		t.Fatalf("应 ok=true score=42.5，得到 ok=%v s=%v", ok, s)
	}
}

func TestWeightedScore_DowngradeOnUnreachable(t *testing.T) {
	c := &Client{baseURL: "http://127.0.0.1:1", http: &http.Client{Timeout: 100000000}}
	_, ok := c.WeightedScore(context.Background(), 100, 0, 1.0)
	if ok {
		t.Fatal("不可达应 ok=false")
	}
}

func TestCircuitCheck_OK(t *testing.T) {
	c, _ := startFakeKernel(t)
	r, ok := c.CircuitCheck(context.Background(), 7, 5, 30)
	if !ok {
		t.Fatal("应 ok=true")
	}
	if !r.ShouldOpen || r.CooldownSec != 60 || r.CoolUntil != 999 {
		t.Fatalf("解码错误: %+v", r)
	}
}

func TestCircuitCheck_DowngradeOnUnreachable(t *testing.T) {
	c := &Client{baseURL: "http://127.0.0.1:1", http: &http.Client{Timeout: 100000000}}
	_, ok := c.CircuitCheck(context.Background(), 7, 5, 30)
	if ok {
		t.Fatal("不可达应 ok=false")
	}
}

func TestNewFromEnv_Disabled(t *testing.T) {
	t.Setenv("KERNEL_DISABLE", "1")
	if c := NewFromEnv(); c != nil {
		t.Fatal("KERNEL_DISABLE=1 应返回 nil")
	}
}

func TestNewFromEnv_OverridesURL(t *testing.T) {
	t.Setenv("KERNEL_DISABLE", "")
	t.Setenv("KERNEL_URL", "http://1.2.3.4:9999")
	c := NewFromEnv()
	if c == nil || c.baseURL != "http://1.2.3.4:9999" {
		t.Fatalf("应使用 KERNEL_URL，得到 %+v", c)
	}
}

func TestNewFromEnv_DefaultURL(t *testing.T) {
	t.Setenv("KERNEL_DISABLE", "")
	t.Setenv("KERNEL_URL", "")
	c := NewFromEnv()
	if c == nil || c.baseURL != "http://127.0.0.1:8790" {
		t.Fatalf("应使用默认 URL，得到 %+v", c)
	}
}

func TestNilClient_Downgrades(t *testing.T) {
	var c *Client // nil
	if _, ok := c.Verdict(context.Background(), 50, "closed", 1, 30, 80, 3, 30); ok {
		t.Fatal("nil 客户端应 ok=false")
	}
	if _, ok := c.WeightedScore(context.Background(), 100, 0, 1.0); ok {
		t.Fatal("nil 客户端应 ok=false")
	}
	if _, ok := c.CircuitCheck(context.Background(), 7, 5, 30); ok {
		t.Fatal("nil 客户端应 ok=false")
	}
}
