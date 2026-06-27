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

// GetMetrics 按时间窗口聚合指标。model 为空时聚合全部模型（全局指标），
// 非空时仅统计该模型（模型详情页用）。
func (s *Store) GetMetrics(ctx context.Context, window time.Duration, model string) (Metrics, error) {
	since := time.Now().Add(-window).Unix()
	m := Metrics{Window: window.String()}

	query := `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='error' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='rate_limited' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='timeout' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(total_tokens),0),
			COALESCE(AVG(latency_ms),0)
		FROM request_logs WHERE ts > ?`
	args := []any{since}
	if model != "" {
		query += ` AND model = ?`
		args = append(args, model)
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
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
	rpmQuery := `SELECT COUNT(*) FROM request_logs WHERE ts > ?`
	rpmArgs := []any{sixtyAgo}
	if model != "" {
		rpmQuery += ` AND model = ?`
		rpmArgs = append(rpmArgs, model)
	}
	_ = s.db.QueryRowContext(ctx, rpmQuery, rpmArgs...).Scan(&m.CurrentRPM)

	return m, nil
}

// TimeSeriesPoint 时序曲线上的一个点。
// json 标签对齐前端契约（snake_case），OkCount 用于成功/失败双面积图拆分。
type TimeSeriesPoint struct {
	TS      int64   `json:"ts"`       // 桶起始时间戳
	Count   int64   `json:"count"`    // 请求数
	OkCount int64   `json:"ok_count"` // 成功请求数（Count-OkCount=失败）
	Tokens  int64   `json:"tokens"`   // token 数
	Rate    float64 `json:"rate"`     // 成功率 0-100
}

// GetTimeSeries 按固定桶大小分桶返回时序数据（用于运维面板曲线图）。
// bucketSeconds 为每桶秒数（如 60=每分钟一桶）。model 为空时聚合全部模型。
func (s *Store) GetTimeSeries(ctx context.Context, window time.Duration, bucketSeconds int, model string) ([]TimeSeriesPoint, error) {
	if bucketSeconds < 1 {
		bucketSeconds = 60
	}
	since := time.Now().Add(-window).Unix()
	// model 过滤：非空时仅统计该模型的时序（模型详情页用）。
	query := `
		SELECT
			(ts / ?) * ? AS bucket,
			COUNT(*) AS cnt,
			COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END),0) AS ok,
			COALESCE(SUM(total_tokens),0) AS tokens,
			CASE WHEN COUNT(*) > 0 THEN 100.0 * SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) / COUNT(*) ELSE 0 END AS rate
		FROM request_logs
		WHERE ts > ?`
	args := []any{bucketSeconds, bucketSeconds, since}
	if model != "" {
		query += ` AND model = ?`
		args = append(args, model)
	}
	query += ` GROUP BY bucket ORDER BY bucket`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		if err := rows.Scan(&p.TS, &p.Count, &p.OkCount, &p.Tokens, &p.Rate); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// KeyHealthEntry 单个 Key 的健康摘要（面板列表用）。
// json 标签对齐前端契约（snake_case）；ConsecutiveFail 来自 upstream_keys 表；
// EWMA 当窗口内的成功率（SQL 无内存 EWMA，以窗口成功率近似，前端据此画健康度条）。
type KeyHealthEntry struct {
	KeyMask         string  `json:"key_mask"`
	Status          string  `json:"status"`
	TotalRequests   int64   `json:"total_requests"`
	SuccessRate     float64 `json:"success_rate"` // 0-100
	AvgLatencyMS    float64 `json:"avg_latency_ms"`
	ConsecutiveFail int     `json:"consecutive_fail"`
	EWMARate        float64 `json:"ewma_rate"` // 近窗口成功率，供健康度条
}

// GetKeyHealth 近 window 内每个 Key 的健康指标。model 为空时聚合全部模型。
func (s *Store) GetKeyHealth(ctx context.Context, window time.Duration, model string) ([]KeyHealthEntry, error) {
	since := time.Now().Add(-window).Unix()
	// 按 key_mask 关联（request_logs.upstream_key 存的是脱敏 key_mask）。
	// 模型过滤仅作用于请求聚合列，不影响密钥行数（LEFT JOIN 保留所有密钥）。
	modelClause := ""
	if model != "" {
		modelClause = ` AND r.model = ?`
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT k.key_mask, k.status, k.consecutive_fail,
		       COUNT(r.id),
		       COALESCE(SUM(CASE WHEN r.status='success' THEN 1 ELSE 0 END),0),
		       COALESCE(AVG(r.latency_ms),0)
		FROM upstream_keys k
		LEFT JOIN request_logs r ON r.upstream_key = k.key_mask AND r.ts > ?`+modelClause+`
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
		if err := rows.Scan(&e.KeyMask, &e.Status, &e.ConsecutiveFail, &total, &success, &e.AvgLatencyMS); err != nil {
			return nil, err
		}
		e.TotalRequests = total
		if total > 0 {
			e.SuccessRate = 100.0 * float64(success) / float64(total)
			e.EWMARate = e.SuccessRate
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
