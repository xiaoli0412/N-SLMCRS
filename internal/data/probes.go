package data

import "context"

// ProbeResult 一次主动探活的结果（写入 model_probes 表）。
type ProbeResult struct {
	ModelID    string `json:"model_id"`
	TS         int64  `json:"ts"`
	OK         bool   `json:"ok"`
	HTTPStatus int    `json:"http_status"`
	LatencyMS  int    `json:"latency_ms"`
	Status     string `json:"status"` // ok|error|timeout
	Error      string `json:"error,omitempty"`
}

// UpsertModelProbe 写入/覆盖某模型的最近一次探活结果，并追加一条历史记录。
func (s *Store) UpsertModelProbe(ctx context.Context, p ProbeResult) error {
	ok := 0
	if p.OK {
		ok = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO model_probes (model, ts, ok, http_status, latency_ms, status, error)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(model) DO UPDATE SET
			ts=excluded.ts, ok=excluded.ok, http_status=excluded.http_status,
			latency_ms=excluded.latency_ms, status=excluded.status, error=excluded.error`,
		p.ModelID, p.TS, ok, p.HTTPStatus, p.LatencyMS, p.Status, p.Error); err != nil {
		return err
	}
	// 追加历史（供模型详情页探活趋势）
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO model_probe_history (model, ts, ok, http_status, latency_ms, status, error)
		VALUES (?,?,?,?,?,?,?)`,
		p.ModelID, p.TS, ok, p.HTTPStatus, p.LatencyMS, p.Status, p.Error); err != nil {
		return err
	}
	return tx.Commit()
}

// ListModelProbes 返回全部模型的最近探活结果（modelID → ProbeResult）。
func (s *Store) ListModelProbes(ctx context.Context) (map[string]ProbeResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT model, ts, ok, http_status, latency_ms, status, error FROM model_probes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ProbeResult)
	for rows.Next() {
		var p ProbeResult
		var ok int
		if err := rows.Scan(&p.ModelID, &p.TS, &ok, &p.HTTPStatus, &p.LatencyMS, &p.Status, &p.Error); err != nil {
			return nil, err
		}
		p.OK = ok == 1
		out[p.ModelID] = p
	}
	return out, rows.Err()
}

// ListModelProbeHistory 返回某模型近 limit 次探活历史（按时间升序，供详情页趋势图）。
func (s *Store) ListModelProbeHistory(ctx context.Context, model string, limit int) ([]ProbeResult, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT model, ts, ok, http_status, latency_ms, status, error
		FROM model_probe_history WHERE model = ?
		ORDER BY ts DESC LIMIT ?`, model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProbeResult
	for rows.Next() {
		var p ProbeResult
		var ok int
		if err := rows.Scan(&p.ModelID, &p.TS, &ok, &p.HTTPStatus, &p.LatencyMS, &p.Status, &p.Error); err != nil {
			return nil, err
		}
		p.OK = ok == 1
		out = append(out, p)
	}
	// 反转为升序，便于图表按时间从左到右
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
