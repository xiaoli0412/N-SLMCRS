package metrics

import (
	"strings"
	"testing"
)

// TestExpositionFormat 验证 counter/gauge/histogram 的 Prometheus 文本格式。
// 重点：直方图 le 标签须在标签块内（name_bucket{k="v",le="0.1"}），sum/count 须带标签。
func TestExpositionFormat(t *testing.T) {
	r := NewRegistry()
	reqs := r.NewCounter("nslmcrs_requests_total", "total requests", "status", "model")
	inflight := r.NewGauge("nslmcrs_inflight", "inflight", "model")
	lat := r.NewHistogram("nslmcrs_latency_seconds", "latency",
		[]float64{0.1, 0.5, 1, 5}, "model")

	reqs.Inc("success", "llama")
	reqs.Inc("success", "llama")
	reqs.Add(5, "error", "llama")
	inflight.Set(3, "llama")
	lat.Observe(0.05, "llama")
	lat.Observe(0.3, "llama")
	lat.Observe(2, "llama")

	var b strings.Builder
	r.WriteMetrics(&b)
	out := b.String()

	// counter（标签按注册顺序 status,model）
	if !strings.Contains(out, `nslmcrs_requests_total{status="success",model="llama"} 2`) {
		t.Errorf("counter success line missing/wrong:\n%s", out)
	}
	// gauge
	if !strings.Contains(out, `nslmcrs_inflight{model="llama"} 3`) {
		t.Errorf("gauge line missing/wrong:\n%s", out)
	}
	// histogram bucket le 在标签块内（注册标签在前，le 在后）
	if !strings.Contains(out, `nslmcrs_latency_seconds_bucket{model="llama",le="0.1"} 1`) {
		t.Errorf("histogram bucket le=0.1 line missing/wrong:\n%s", out)
	}
	if !strings.Contains(out, `nslmcrs_latency_seconds_bucket{model="llama",le="+Inf"} 3`) {
		t.Errorf("histogram +Inf bucket missing/wrong:\n%s", out)
	}
	if !strings.Contains(out, `nslmcrs_latency_seconds_sum{model="llama"} 2.35`) {
		t.Errorf("histogram sum missing/wrong:\n%s", out)
	}
	if !strings.Contains(out, `nslmcrs_latency_seconds_count{model="llama"} 3`) {
		t.Errorf("histogram count missing/wrong:\n%s", out)
	}
	// TYPE 行
	if !strings.Contains(out, "# TYPE nslmcrs_requests_total counter") {
		t.Errorf("counter TYPE line missing:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE nslmcrs_latency_seconds histogram") {
		t.Errorf("histogram TYPE line missing:\n%s", out)
	}
}

// TestNoLabelHistogram 验证无标签直方图格式（le 不带前置逗号）。
func TestNoLabelHistogram(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("nslmcrs_latency_seconds", "latency", []float64{0.1, 1})
	h.Observe(0.05)
	h.Observe(2)
	var b strings.Builder
	r.WriteMetrics(&b)
	out := b.String()
	if !strings.Contains(out, `nslmcrs_latency_seconds_bucket{le="0.1"} 1`) {
		t.Errorf("no-label bucket missing:\n%s", out)
	}
	if !strings.Contains(out, `nslmcrs_latency_seconds_bucket{le="+Inf"} 2`) {
		t.Errorf("no-label +Inf missing:\n%s", out)
	}
	if !strings.Contains(out, `nslmcrs_latency_seconds_count 2`) {
		t.Errorf("no-label count missing:\n%s", out)
	}
}

// TestConcurrentInc 验证并发安全（data race 须靠 -race 检测，此处仅冒烟）。
func TestConcurrentInc(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("c", "h", "k")
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			c.Inc("a")
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		c.Inc("a")
	}
	<-done
	var b strings.Builder
	r.WriteMetrics(&b)
	if !strings.Contains(b.String(), `c{k="a"} 2000`) {
		t.Errorf("expected 2000, got:\n%s", b.String())
	}
}
