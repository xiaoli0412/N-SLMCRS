package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// startFakeKernel 启动一个模拟 Rust sidecar 的 httptest 服务。
// forecast 返回固定 next/level/trend；availability 返回固定 score。
func startFakeKernel(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"forecast_next": 42.5, "level": 40.0, "trend": 2.5,
		})
	})
	mux.HandleFunc("/availability", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"score": 88.0})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestKernelClientForecastOK(t *testing.T) {
	srv := startFakeKernel(t)
	t.Setenv("KERNEL_URL", srv.URL)
	t.Setenv("KERNEL_DISABLE", "")
	k := newKernelClient()
	if k == nil {
		t.Fatal("newKernelClient 返回 nil")
	}
	next, ok := k.forecast(context.Background(), []float64{1, 2, 3})
	if !ok {
		t.Fatal("forecast ok=false, want true")
	}
	if next != 42.5 {
		t.Errorf("forecast next = %v, want 42.5", next)
	}
}

func TestKernelClientForecastDowngradeOnDown(t *testing.T) {
	// 指向一个必失败的端口，验证降级返回 (0,false)
	t.Setenv("KERNEL_URL", "http://127.0.0.1:1") // :1 通常拒绝连接
	t.Setenv("KERNEL_DISABLE", "")
	k := newKernelClient()
	if k == nil {
		t.Fatal("newKernelClient 返回 nil")
	}
	next, ok := k.forecast(context.Background(), []float64{1, 2, 3})
	if ok {
		t.Error("不可达时应降级 ok=false")
	}
	if next != 0 {
		t.Errorf("降级 next = %v, want 0", next)
	}
}

func TestKernelClientAvailabilityOK(t *testing.T) {
	srv := startFakeKernel(t)
	t.Setenv("KERNEL_URL", srv.URL)
	t.Setenv("KERNEL_DISABLE", "")
	k := newKernelClient()
	score, ok := k.availability(context.Background(), 95.0, 200.0, 1000)
	if !ok {
		t.Fatal("availability ok=false, want true")
	}
	if score != 88.0 {
		t.Errorf("availability score = %v, want 88.0", score)
	}
}

func TestKernelClientDisabledReturnsNil(t *testing.T) {
	t.Setenv("KERNEL_URL", "http://127.0.0.1:8790")
	t.Setenv("KERNEL_DISABLE", "1")
	k := newKernelClient()
	if k != nil {
		t.Fatal("KERNEL_DISABLE=1 时 newKernelClient 应返回 nil")
	}
}
