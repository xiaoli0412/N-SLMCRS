// Package data — audit.go 管理操作审计日志（v0.13 企业合规）。
//
// 敏感写操作（密钥增删改、凭证签发/删除、设置变更、改密、渠道增删、熔断复位）
// 经 Store.RecordAudit 留痕，便于事后追溯 who/what/when。审计写入为 best-effort：
// 失败仅记日志不阻断主操作（审计不应成为业务路径的单点）。
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AuditEntry 审计日志条目。
type AuditEntry struct {
	ID     int64  `json:"id"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Detail string `json:"detail"`
	IP     string `json:"ip"`
}

// MaskToken 把 admin 令牌脱敏为掩码（前 6 + … + 后 4），用于审计 actor 字段。
// 短令牌（<=10）整体掩为 ****，避免泄露有效长度信息。
func MaskToken(token string) string {
	t := strings.TrimSpace(token)
	if len(t) <= 10 {
		return "****"
	}
	return t[:6] + "…" + t[len(t)-4:]
}

// RecordAudit 追加一条审计日志。detail 可为任意可序列化值（JSON 落库）。
// 失败返回 error 但调用方应忽略（best-effort），不阻断主操作。
func (s *Store) RecordAudit(ctx context.Context, actor, action, ip string, detail any) error {
	detailStr := ""
	if detail != nil {
		b, err := json.Marshal(detail)
		if err == nil {
			detailStr = string(b)
		} else {
			detailStr = fmt.Sprintf("%v", detail)
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log(ts, actor, action, detail, ip) VALUES(?,?,?,?,?)`,
		time.Now().Unix(), actor, action, detailStr, ip)
	return err
}

// ListAudit 按时间倒序返回审计记录（游标分页，v0.14）。
// beforeTS/beforeID > 0 时取该游标之前的行（"加载更多"）。默认上限 200。
func (s *Store) ListAudit(ctx context.Context, beforeTS, beforeID int64, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT id, ts, actor, action, detail, ip FROM audit_log WHERE 1=1`
	args := []any{}
	if beforeTS > 0 {
		if beforeID > 0 {
			q += ` AND (ts < ? OR (ts = ? AND id < ?))`
			args = append(args, beforeTS, beforeTS, beforeID)
		} else {
			q += ` AND ts < ?`
			args = append(args, beforeTS)
		}
	}
	q += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Detail, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAudit 删除 ts 早于 beforeTS 的审计记录（留存清理，v0.14）。
func (s *Store) PruneAudit(ctx context.Context, beforeTS int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE ts < ?`, beforeTS)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
