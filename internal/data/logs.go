package data

import (
	"context"
	"strings"
)

// AppLog 结构化日志条目。
type AppLog struct {
	ID      int64
	TS      int64
	TraceID string
	Level   string // debug|info|warn|error（统一小写存储）
	Source  string // entry|scheduler|server|upstream|autopilot|modelhealth|ratelimit|data
	Message string
	Context string // JSON 串
}

// LogInsert 批量写入单元（v0.14：单写者 drain 批量落库）。
type LogInsert struct {
	Level   string
	Source  string
	TraceID string
	Message string
	Context string
}

// WriteLog 写入一条应用日志。level 统一转小写落库（v0.14：根治大小写不一致
// 导致级别筛选失效的 bug——slog 存大写 INFO、autopilot 存小写 info，混存使
// 前端小写级别筛选只命中部分行）。
func (s *Store) WriteLog(ctx context.Context, level, source, traceID, message, ctxJSON string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_logs (ts, trace_id, level, source, message, context)
		VALUES (?,?,?,?,?,?)`,
		now(), traceID, strings.ToLower(level), source, message, ctxJSON)
	return err
}

// WriteLogBatch 批量写入日志（单事务，单写者 drain 调用，降 SQLite 写争用）。
func (s *Store) WriteLogBatch(ctx context.Context, entries []LogInsert) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO app_logs (ts, trace_id, level, source, message, context) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	ts := now()
	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, ts, e.TraceID, strings.ToLower(e.Level), e.Source, e.Message, e.Context); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// QueryLogs 查询日志（支持 trace_id/level/source 过滤 + 游标分页，按时间倒序）。
// 游标：(beforeTS, beforeID)——取该点之前（更早）的行，实现"加载更多"。
// level 查询时统一小写，兼容历史大小写混存数据（v0.14）。
func (s *Store) QueryLogs(ctx context.Context, traceID, level, source string, beforeTS, beforeID, limit int64) ([]AppLog, error) {
	q := `SELECT id, ts, trace_id, level, source, message, context FROM app_logs WHERE 1=1`
	args := []any{}
	if traceID != "" {
		q += ` AND trace_id=?`
		args = append(args, traceID)
	}
	if level != "" {
		// 统一小写比较：历史数据可能大写，用 lower(level) 兼容
		q += ` AND lower(level)=?`
		args = append(args, strings.ToLower(level))
	}
	if source != "" {
		q += ` AND source=?`
		args = append(args, source)
	}
	if beforeTS > 0 {
		// 游标分页：取 (ts,id) 严格小于游标的行（倒序下即更早的）
		if beforeID > 0 {
			q += ` AND (ts < ? OR (ts = ? AND id < ?))`
			args = append(args, beforeTS, beforeTS, beforeID)
		} else {
			q += ` AND ts < ?`
			args = append(args, beforeTS)
		}
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
	out := make([]AppLog, 0, limit)
	for rows.Next() {
		var l AppLog
		if err := rows.Scan(&l.ID, &l.TS, &l.TraceID, &l.Level, &l.Source, &l.Message, &l.Context); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PruneLogs 删除 ts 早于 beforeTS 的日志（留存清理，v0.14）。
func (s *Store) PruneLogs(ctx context.Context, beforeTS int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM app_logs WHERE ts < ?`, beforeTS)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
