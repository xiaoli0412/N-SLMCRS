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

// UpsertModelProbe 写入/覆盖某模型的最近一次探活结果。
func (s *Store) UpsertModelProbe(ctx context.Context, p ProbeResult) error {
	ok := 0
	if p.OK {
		ok = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_probes (model, ts, ok, http_status, latency_ms, status, error)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(model) DO UPDATE SET
			ts=excluded.ts, ok=excluded.ok, http_status=excluded.http_status,
			latency_ms=excluded.latency_ms, status=excluded.status, error=excluded.error`,
		p.ModelID, p.TS, ok, p.HTTPStatus, p.LatencyMS, p.Status, p.Error)
	return err
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
