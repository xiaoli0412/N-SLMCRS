package data

import "context"

// AppLog 结构化日志条目。
type AppLog struct {
	TS      int64
	TraceID string
	Level   string // debug|info|warn|error
	Source  string // entry|scheduler|ratelimit|upstream|data
	Message string
	Context string // JSON 串
}

// WriteLog 写入一条应用日志。
func (s *Store) WriteLog(ctx context.Context, level, source, traceID, message, ctxJSON string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_logs (ts, trace_id, level, source, message, context)
		VALUES (?,?,?,?,?,?)`,
		now(), traceID, level, source, message, ctxJSON)
	return err
}

// QueryLogs 查询日志（支持 trace_id/level/source 过滤，按时间倒序）。
func (s *Store) QueryLogs(ctx context.Context, traceID, level, source string, since, limit int64) ([]AppLog, error) {
	q := `SELECT ts, trace_id, level, source, message, context FROM app_logs WHERE 1=1`
	args := []any{}
	if traceID != "" {
		q += ` AND trace_id=?`
		args = append(args, traceID)
	}
	if level != "" {
		q += ` AND level=?`
		args = append(args, level)
	}
	if source != "" {
		q += ` AND source=?`
		args = append(args, source)
	}
	if since > 0 {
		q += ` AND ts>?`
		args = append(args, since)
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppLog
	for rows.Next() {
		var l AppLog
		if err := rows.Scan(&l.TS, &l.TraceID, &l.Level, &l.Source, &l.Message, &l.Context); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
