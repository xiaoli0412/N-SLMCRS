package data

import (
	"context"
	"time"
)

// RequestLog 一次转发请求的完整记录（时序指标来源）。
type RequestLog struct {
	TraceID          string
	TS               int64
	DownstreamCred   string
	UpstreamKey      string
	Model            string
	Protocol         string
	Status           string // success|error|timeout|rate_limited|circuit
	HTTPStatus       int
	LatencyMS        int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ErrorType        string
	ErrorMessage     string
	Concurrency      int
}

// RecordRequest 写入一条请求记录。error_message 会被截断到 500 字符。
func (s *Store) RecordRequest(ctx context.Context, r RequestLog) error {
	if r.TS == 0 {
		r.TS = now()
	}
	if len(r.ErrorMessage) > 500 {
		r.ErrorMessage = r.ErrorMessage[:500]
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO request_logs
		(trace_id, ts, downstream_cred, upstream_key, model, protocol, status, http_status,
		 latency_ms, prompt_tokens, completion_tokens, total_tokens, error_type, error_message, concurrency)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.TraceID, r.TS, r.DownstreamCred, r.UpstreamKey, r.Model, r.Protocol, r.Status, r.HTTPStatus,
		r.LatencyMS, r.PromptTokens, r.CompletionTokens, r.TotalTokens, r.ErrorType, r.ErrorMessage, r.Concurrency)
	return err
}

// Metrics 时间窗口聚合指标。
type Metrics struct {
	Window           string  // "1h"/"24h"/"7d"
	TotalRequests    int64
	SuccessRequests  int64
	SuccessRate      float64 // 0-100
	ErrorRequests    int64
	RateLimited      int64
	Timeouts         int64
	TotalTokens      int64
	AvgLatencyMS     float64
	CurrentRPM       int64   // 近 60s 请求数估算
	PeakRPM          int64
}

// GetMetrics 按时间窗口聚合指标。
func (s *Store) GetMetrics(ctx context.Context, window time.Duration) (Metrics, error) {
	since := time.Now().Add(-window).Unix()
	m := Metrics{Window: window.String()}

	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='error' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='rate_limited' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='timeout' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(total_tokens),0),
			COALESCE(AVG(latency_ms),0)
		FROM request_logs WHERE ts > ?`, since).Scan(
		&m.TotalRequests, &m.SuccessRequests, &m.ErrorRequests, &m.RateLimited,
		&m.Timeouts, &m.TotalTokens, &m.AvgLatencyMS)
	if err != nil {
		return m, err
	}
	if m.TotalRequests > 0 {
		m.SuccessRate = 100.0 * float64(m.SuccessRequests) / float64(m.TotalRequests)
	}

	// 当前 RPM：近 60s 请求数
	sixtyAgo := time.Now().Add(-60 * time.Second).Unix()
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_logs WHERE ts > ?`, sixtyAgo).Scan(&m.CurrentRPM)

	return m, nil
}

// TimeSeriesPoint 时序曲线上的一个点。
type TimeSeriesPoint struct {
	TS     int64   // 桶起始时间戳
	Count  int64   // 请求数
	Tokens int64   // token 数
	Rate   float64 // 成功率 0-100
}

// GetTimeSeries 按固定桶大小分桶返回时序数据（用于运维面板曲线图）。
// bucketSeconds 为每桶秒数（如 60=每分钟一桶）。
func (s *Store) GetTimeSeries(ctx context.Context, window time.Duration, bucketSeconds int) ([]TimeSeriesPoint, error) {
	if bucketSeconds < 1 {
		bucketSeconds = 60
	}
	since := time.Now().Add(-window).Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			(ts / ?) * ? AS bucket,
			COUNT(*) AS cnt,
			COALESCE(SUM(total_tokens),0) AS tokens,
			CASE WHEN COUNT(*) > 0 THEN 100.0 * SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) / COUNT(*) ELSE 0 END AS rate
		FROM request_logs
		WHERE ts > ?
		GROUP BY bucket
		ORDER BY bucket`, bucketSeconds, bucketSeconds, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		if err := rows.Scan(&p.TS, &p.Count, &p.Tokens, &p.Rate); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// KeyHealthEntry 单个 Key 的健康摘要（面板列表用）。
type KeyHealthEntry struct {
	KeyMask     string
	Status      string
	SuccessRate float64
	AvgLatency  float64
	Requests    int64
}

// GetKeyHealth 近 window 内每个 Key 的健康指标。
func (s *Store) GetKeyHealth(ctx context.Context, window time.Duration) ([]KeyHealthEntry, error) {
	since := time.Now().Add(-window).Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT k.key_mask, k.status,
		       COUNT(r.id),
		       COALESCE(SUM(CASE WHEN r.status='success' THEN 1 ELSE 0 END),0),
		       COALESCE(AVG(r.latency_ms),0)
		FROM upstream_keys k
		LEFT JOIN request_logs r ON r.upstream_key = k.key_mask AND r.ts > ?
		GROUP BY k.id
		ORDER BY k.id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeyHealthEntry
	for rows.Next() {
		var e KeyHealthEntry
		var total int64
		var success int64
		if err := rows.Scan(&e.KeyMask, &e.Status, &total, &success, &e.AvgLatency); err != nil {
			return nil, err
		}
		e.Requests = total
		if total > 0 {
			e.SuccessRate = 100.0 * float64(success) / float64(total)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
