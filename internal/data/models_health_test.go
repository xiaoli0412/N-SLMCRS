package data

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestModelHealthStats_Aggregation 验证每模型健康聚合：请求数/成功/错误/平均延迟/可用度评分。
func TestModelHealthStats_Aggregation(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	logs := []RequestLog{
		{Model: "a", Status: "success", LatencyMS: 100, TraceID: "t1"},
		{Model: "a", Status: "success", LatencyMS: 200, TraceID: "t2"},
		{Model: "a", Status: "error", TraceID: "t3"},
		{Model: "b", Status: "success", LatencyMS: 1000, TraceID: "t4"},
	}
	for _, l := range logs {
		if err := store.RecordRequest(ctx, l); err != nil {
			t.Fatalf("RecordRequest: %v", err)
		}
	}

	health, err := store.ModelHealthStats(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ModelHealthStats: %v", err)
	}
	a := health["a"]
	if a.RequestCount != 3 || a.SuccessCount != 2 || a.ErrorCount != 1 {
		t.Fatalf("A 统计错: total=%d ok=%d err=%d", a.RequestCount, a.SuccessCount, a.ErrorCount)
	}
	if a.SuccessRate < 66 || a.SuccessRate > 67 {
		t.Fatalf("A 成功率应≈66.7, 得到 %.1f", a.SuccessRate)
	}
	// 成功请求平均延迟 = (100+200)/2 = 150（失败不计入延迟）
	if a.AvgLatencyMS != 150 {
		t.Fatalf("A 平均延迟应为 150, 得到 %d", a.AvgLatencyMS)
	}
	if a.AvailabilityScore <= 0 || a.AvailabilityScore > 100 {
		t.Fatalf("A 可用度评分应 (0,100], 得到 %.1f", a.AvailabilityScore)
	}
	// 无流量的模型不应出现
	if _, ok := health["c"]; ok {
		t.Fatalf("无流量模型不应出现在聚合结果中")
	}
}

// TestAvailabilityScore_NoTraffic 无流量时评分为 0。
func TestAvailabilityScore_NoTraffic(t *testing.T) {
	if got := availabilityScore(0, 0, 0); got != 0 {
		t.Fatalf("无流量可用度应为 0, 得到 %.1f", got)
	}
}

// TestModelProbes_UpsertAndList 探活结果按 model 覆盖写入与读取。
func TestModelProbes_UpsertAndList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	_ = store.UpsertModelProbe(ctx, ProbeResult{ModelID: "a", TS: 1, OK: true, HTTPStatus: 200, LatencyMS: 50, Status: "ok"})
	_ = store.UpsertModelProbe(ctx, ProbeResult{ModelID: "b", TS: 2, OK: false, HTTPStatus: 500, Status: "error", Error: "boom"})
	// 覆盖 a 的最新探活
	_ = store.UpsertModelProbe(ctx, ProbeResult{ModelID: "a", TS: 3, OK: false, Status: "timeout", Error: "超时"})

	probes, err := store.ListModelProbes(ctx)
	if err != nil {
		t.Fatalf("ListModelProbes: %v", err)
	}
	if len(probes) != 2 {
		t.Fatalf("应有 2 条探活记录, 得到 %d", len(probes))
	}
	a := probes["a"]
	if a.OK || a.Status != "timeout" || a.TS != 3 {
		t.Fatalf("A 应为最新覆盖(timeout/ts=3), 得到 %+v", a)
	}
	b := probes["b"]
	if b.OK || b.Status != "error" || b.Error != "boom" {
		t.Fatalf("B 应 error/boom, 得到 %+v", b)
	}
}
