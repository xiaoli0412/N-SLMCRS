// Package metrics 提供轻量 Prometheus 指标注册表与暴露（exposition）文本生成。
//
// 设计目标：零第三方依赖、线程安全、手写 Prometheus 文本格式（text/plain;
// version=0.0.4），足以被 Prometheus / Grafana 直接抓取。仅覆盖单节点网关
// 关心的维度：请求计数/延迟直方图/在途/熔断态/令牌余量/Auto-Pilot 状态。
//
// 不引入 prometheus/client_golang：避免 ~2MB 依赖膨胀，且本网关指标维度有限，
// 手写更可控。格式严格遵循 https://prometheus.io/docs/instrumenting/exposition_formats/。
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const sep = "\x1f" // 单元分隔符，用作 label 值连接键（不会出现在合法 label 值中）

// Registry 指标注册表。线程安全。
type Registry struct {
	mu       sync.Mutex
	counters []*Counter
	gauges   []*Gauge
	hists    []*Histogram
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{}
}

// Collector 高层采集器：预置网关核心指标，供 scheduler/entry 调用。
// 避免各调用方各自持 Counter/Gauge 句柄；统一一个 Collector 注入。
type Collector struct {
	reqs     *Counter
	latency  *Histogram
	inflight *Gauge
}

// NewCollector 在 registry 上注册网关标准指标并返回采集器。
// 标签维度：status(success|error|rate_limited)、model。延迟直方图按模型分线。
func NewCollector(r *Registry, version string) *Collector {
	c := &Collector{
		reqs:     r.NewCounter("nslmcrs_requests_total", "Total dispatched requests by status and model.", "status", "model"),
		latency:  r.NewHistogram("nslmcrs_request_duration_seconds", "End-to-end request latency in seconds.", defaultBuckets, "model"),
		inflight: r.NewGauge("nslmcrs_inflight_requests", "Currently in-flight requests.", "model"),
	}
	// build_info：常量标签，便于 Prometheus 按 version 区分实例。
	bi := r.NewGauge("nslmcrs_build_info", "Build information.", "version")
	bi.Set(1, version)
	return c
}

// 默认延迟桶（秒）：覆盖 LLM 典型区间 50ms~5min。
var defaultBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

// RecordRequest 记录一次请求结果（status + model + 延迟）。供 scheduler 调用。
func (c *Collector) RecordRequest(status, model string, latencySec float64) {
	c.reqs.Inc(status, model)
	c.latency.Observe(latencySec, model)
}

// IncInflight / DecInflight 在途请求仪表增减。
func (c *Collector) IncInflight(model string) { c.inflight.Inc(model) }
func (c *Collector) DecInflight(model string) { c.inflight.Dec(model) }

// NewCounter 注册一个计数器（labels 为各指标的标签名集合，顺序固定）。
func (r *Registry) NewCounter(name, help string, labels ...string) *Counter {
	c := &Counter{name: name, help: help, labels: labels, values: map[string]float64{}}
	r.mu.Lock()
	r.counters = append(r.counters, c)
	r.mu.Unlock()
	return c
}

// NewGauge 注册一个仪表。
func (r *Registry) NewGauge(name, help string, labels ...string) *Gauge {
	g := &Gauge{name: name, help: help, labels: labels, values: map[string]float64{}}
	r.mu.Lock()
	r.gauges = append(r.gauges, g)
	r.mu.Unlock()
	return g
}

// NewHistogram 注册一个直方图。buckets 须升序，不包含 +Inf（自动追加）。
func (r *Registry) NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	b := append([]float64(nil), buckets...)
	sort.Float64s(b)
	h := &Histogram{
		name:    name,
		help:    help,
		labels:  labels,
		buckets: b,
		samples: map[string]*histSample{},
	}
	r.mu.Lock()
	r.hists = append(r.hists, h)
	r.mu.Unlock()
	return h
}

// WriteMetrics 将全部指标按 Prometheus 文本格式写入 w。
func (r *Registry) WriteMetrics(w io.Writer) {
	r.mu.Lock()
	counters := append([]*Counter(nil), r.counters...)
	gauges := append([]*Gauge(nil), r.gauges...)
	hists := append([]*Histogram(nil), r.hists...)
	r.mu.Unlock()

	for _, c := range counters {
		c.write(w)
	}
	for _, g := range gauges {
		g.write(w)
	}
	for _, h := range hists {
		h.write(w)
	}
}

// --- Counter ---

// Counter 单调递增计数器（按标签组合分线）。
type Counter struct {
	name   string
	help   string
	labels []string
	mu     sync.Mutex
	values map[string]float64 // key=join(labelVals)
}

// Inc 对给定标签值组合 +1。
func (c *Counter) Inc(labelVals ...string) { c.Add(1, labelVals...) }

// Add 对给定标签值组合加 v（v 可为负以修正，但 Prometheus 语义为单调）。
func (c *Counter) Add(v float64, labelVals ...string) {
	k := joinKey(labelVals)
	c.mu.Lock()
	c.values[k] += v
	c.mu.Unlock()
}

func (c *Counter) write(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	c.mu.Lock()
	keys := sortedKeys(c.values)
	for _, k := range keys {
		lv := strings.Split(k, sep)
		fmt.Fprintf(w, "%s%s %s\n", c.name, labelString(c.labels, lv), formatFloat(c.values[k]))
	}
	c.mu.Unlock()
}

// --- Gauge ---

// Gauge 可增可减的仪表（当前值语义）。
type Gauge struct {
	name   string
	help   string
	labels []string
	mu     sync.Mutex
	values map[string]float64
}

// Set 设置给定标签值组合的当前值。
func (g *Gauge) Set(v float64, labelVals ...string) {
	k := joinKey(labelVals)
	g.mu.Lock()
	g.values[k] = v
	g.mu.Unlock()
}

// Inc / Add / Dec 便捷方法。
func (g *Gauge) Inc(labelVals ...string) { g.Add(1, labelVals...) }
func (g *Gauge) Dec(labelVals ...string) { g.Add(-1, labelVals...) }
func (g *Gauge) Add(v float64, labelVals ...string) {
	k := joinKey(labelVals)
	g.mu.Lock()
	g.values[k] += v
	g.mu.Unlock()
}

func (g *Gauge) write(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
	g.mu.Lock()
	keys := sortedKeys(g.values)
	for _, k := range keys {
		lv := strings.Split(k, sep)
		fmt.Fprintf(w, "%s%s %s\n", g.name, labelString(g.labels, lv), formatFloat(g.values[k]))
	}
	g.mu.Unlock()
}

// --- Histogram ---

type histSample struct {
	count   uint64
	sum     float64
	buckets []uint64 // 与 h.buckets 对齐，最后为 +Inf
}

// Histogram 延迟分布。Observe 记录一次观测值。
type Histogram struct {
	name    string
	help    string
	labels  []string
	buckets []float64
	mu      sync.Mutex
	samples map[string]*histSample
}

// Observe 记录一次观测值 v（秒）。
func (h *Histogram) Observe(v float64, labelVals ...string) {
	k := joinKey(labelVals)
	h.mu.Lock()
	s, ok := h.samples[k]
	if !ok {
		s = &histSample{buckets: make([]uint64, len(h.buckets)+1)}
		h.samples[k] = s
	}
	s.count++
	s.sum += v
	for i, b := range h.buckets {
		if v <= b {
			s.buckets[i]++
		}
	}
	s.buckets[len(h.buckets)]++ // +Inf
	h.mu.Unlock()
}

func (h *Histogram) write(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	h.mu.Lock()
	keys := sortedHistKeys(h.samples)
	for _, k := range keys {
		lv := strings.Split(k, sep)
		s := h.samples[k]
		existing := labelString(h.labels, lv) // {k="v"} 或 ""（无标签）
		// Prometheus 直方图格式：name_bucket{...,le="b"}。le 始终追加在标签块内。
		// leStart 形如 name_bucket{le=  或  name_bucket{k="v",le=
		var leStart string
		if existing == "" {
			leStart = h.name + "_bucket{le="
		} else {
			leStart = h.name + "_bucket" + strings.TrimSuffix(existing, "}") + ",le="
		}
		for i, b := range h.buckets {
			fmt.Fprintf(w, "%s%q} %d\n", leStart, formatFloat(b), s.buckets[i])
		}
		fmt.Fprintf(w, "%s\"+Inf\"} %d\n", leStart, s.buckets[len(h.buckets)])
		fmt.Fprintf(w, "%s_sum%s %s\n", h.name, existing, formatFloat(s.sum))
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, existing, s.count)
	}
	h.mu.Unlock()
}

// --- 辅助 ---

func joinKey(labelVals []string) string {
	return strings.Join(labelVals, sep)
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedHistKeys(m map[string]*histSample) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// labelString 构造 {k1="v1",k2="v2"}；无标签时返回空串。
func labelString(names, vals []string) string {
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(n)
		b.WriteString("=\"")
		b.WriteString(escapeLabelVal(vals[i]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func escapeLabelVal(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", "\\n")
	return v
}

func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
