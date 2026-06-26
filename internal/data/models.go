package data

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Model 模型目录条目。
type Model struct {
	ID             string
	Object         string
	Created        int64
	OwnedBy        string
	Root           string
	ParamCount     string // 增强：参数量（如 8B），Phase 2 从模型卡补充
	ContextLength  int    // 增强：上下文长度
	Capability     string // 增强：chat|embedding|rerank|reasoning
	Description    string
	IsActive       bool
	SyncedAt       int64
}

// UpsertModels 批量同步模型目录（来自 /v1/models）。
// 采用「软失效」策略：先标记全部为 inactive，再把本次返回的重新激活，
// 从而识别「上次有、本次没有」的下线模型。
func (s *Store) UpsertModels(ctx context.Context, models []Model) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	ts := now()
	// 标记全部为失效，后续 upsert 时覆盖
	if _, err := tx.ExecContext(ctx, `UPDATE models SET is_active=0`); err != nil {
		return 0, err
	}

	active := 0
	for _, m := range models {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO models (id, object, created, owned_by, root, capability, param_count, context_length, description, is_active, synced_at)
			VALUES (?,?,?,?,?,?,?,?,?,1,?)
			ON CONFLICT(id) DO UPDATE SET
				object=excluded.object,
				created=excluded.created,
				owned_by=excluded.owned_by,
				root=excluded.root,
				capability=excluded.capability,
				param_count=excluded.param_count,
				context_length=excluded.context_length,
				description=excluded.description,
				is_active=1,
				synced_at=excluded.synced_at`,
			m.ID, defaultStr(m.Object, "model"), m.Created, m.OwnedBy, m.Root,
			defaultStr(m.Capability, "chat"), m.ParamCount, m.ContextLength, m.Description, ts)
		if err != nil {
			return 0, fmt.Errorf("upsert 模型 %s: %w", m.ID, err)
		}
		active++
	}
	return active, tx.Commit()
}

// ListActiveModels 列出当前可用模型。
func (s *Store) ListActiveModels(ctx context.Context) ([]Model, error) {
	return s.queryModels(ctx, `WHERE is_active=1 ORDER BY id`)
}

// ListActiveModelsByCapability 列出指定能力的可用模型。
// capability 为空时等价于 ListActiveModels（供公开 /v1/models 按 chat 能力过滤）。
func (s *Store) ListActiveModelsByCapability(ctx context.Context, capability string) ([]Model, error) {
	if capability == "" {
		return s.ListActiveModels(ctx)
	}
	return s.queryModels(ctx, `WHERE is_active=1 AND capability=? ORDER BY id`, capability)
}

// ListAllModels 列出全部模型（含已失效）。
func (s *Store) ListAllModels(ctx context.Context) ([]Model, error) {
	return s.queryModels(ctx, `ORDER BY is_active DESC, id`)
}

// GetModel 按 ID 获取模型。
func (s *Store) GetModel(ctx context.Context, id string) (*Model, error) {
	ms, err := s.queryModels(ctx, `WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, nil
	}
	return &ms[0], nil
}

func (s *Store) queryModels(ctx context.Context, where string, args ...any) ([]Model, error) {
	q := fmt.Sprintf(`
		SELECT id, object, created, owned_by, root, param_count, context_length, capability, description, is_active, synced_at
		FROM models %s`, where)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		var m Model
		var active int
		if err := rows.Scan(&m.ID, &m.Object, &m.Created, &m.OwnedBy, &m.Root,
			&m.ParamCount, &m.ContextLength, &m.Capability, &m.Description, &active, &m.SyncedAt); err != nil {
			return nil, err
		}
		m.IsActive = active == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

// defaultStr 若 s 为空返回 d。
func defaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// SuggestBestModel 推荐当前成功率最高的可用模型（失效模型时建议切换用）。
// 规则：与 targetCapability 同能力（或全部）中，按近 1h 成功率降序取第一。
func (s *Store) SuggestBestModel(ctx context.Context, targetCapability string) (string, float64, error) {
	args := []any{}
	capFilter := ""
	if targetCapability != "" {
		capFilter = `AND m.capability=?`
		args = append(args, targetCapability)
	}
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT m.id,
		       CASE WHEN total > 0 THEN 100.0 * success / total ELSE 0 END AS rate
		FROM models m
		LEFT JOIN (
			SELECT model,
			       SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) AS success,
			       COUNT(*) AS total
			FROM request_logs
			WHERE ts > ?
			GROUP BY model
		) r ON r.model = m.id
		WHERE m.is_active=1 %s
		ORDER BY rate DESC, m.id ASC
		LIMIT 1`, capFilter), append([]any{time.Now().Add(-time.Hour).Unix()}, args...)...)

	var id string
	var rate float64
	if err := row.Scan(&id, &rate); err != nil {
		// 无数据时回退：取第一个可用模型
		ms, e := s.ListActiveModels(ctx)
		if e != nil || len(ms) == 0 {
			return "", 0, err
		}
		return ms[0].ID, 0, nil
	}
	return id, rate, nil
}

// IsModelActive 检查模型是否可用（用于失效检测）。
func (s *Store) IsModelActive(ctx context.Context, modelID string) (bool, error) {
	m, err := s.GetModel(ctx, modelID)
	if err != nil {
		return false, err
	}
	return m != nil && m.IsActive, nil
}

// ModelHasData 模型目录是否已有数据（决定是否需要立即同步）。
func (s *Store) ModelHasData(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM models`).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ModelUsage 单个模型的用量统计（近 window 时长内）。
type ModelUsage struct {
	ModelID     string
	RequestCount int64
	SuccessRate float64 // 0..100
}

// ModelUsageStats 聚合每个模型在近 window 内的请求量与成功率。
// 返回 modelID → ModelUsage 映射，供模型广场 / 管理端点拼装富视图。
func (s *Store) ModelUsageStats(ctx context.Context, window time.Duration) (map[string]ModelUsage, error) {
	cutoff := time.Now().Add(-window).Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT model,
		       COUNT(*) AS total,
		       SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) AS ok
		FROM request_logs
		WHERE ts > ?
		GROUP BY model`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ModelUsage)
	for rows.Next() {
		var id string
		var total, ok int64
		if err := rows.Scan(&id, &total, &ok); err != nil {
			return nil, err
		}
		var rate float64
		if total > 0 {
			rate = 100.0 * float64(ok) / float64(total)
		}
		out[id] = ModelUsage{ModelID: id, RequestCount: total, SuccessRate: rate}
	}
	return out, rows.Err()
}

// 触碰 strings 防止未用
var _ = strings.TrimSpace

// ModelHealth 单个模型在近 window 内的综合健康聚合（供模型广场可用度展示）。
// 数据来源 request_logs（被动流量统计），与主动探活(model_probes)互补。
type ModelHealth struct {
	ModelID           string
	RequestCount      int64
	SuccessCount      int64
	ErrorCount        int64
	AvgLatencyMS      int64   // 成功请求的平均延迟（毫秒）
	SuccessRate       float64 // 0..100
	AvailabilityScore float64 // 0..100 综合可用度评分
}

// ModelHealthStats 聚合每个模型在近 window 内的健康指标（成功率+延迟+错误），
// 返回 modelID → ModelHealth 映射，供模型广场拼装可用度视图。
func (s *Store) ModelHealthStats(ctx context.Context, window time.Duration) (map[string]ModelHealth, error) {
	cutoff := time.Now().Add(-window).Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT model,
		       COUNT(*) AS total,
		       SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) AS ok,
		       SUM(CASE WHEN status!='success' THEN 1 ELSE 0 END) AS err,
		       COALESCE(CAST(AVG(CASE WHEN status='success' AND latency_ms>0 THEN latency_ms END) AS INTEGER),0) AS avg_lat
		FROM request_logs
		WHERE ts > ?
		GROUP BY model`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ModelHealth)
	for rows.Next() {
		var h ModelHealth
		if err := rows.Scan(&h.ModelID, &h.RequestCount, &h.SuccessCount, &h.ErrorCount, &h.AvgLatencyMS); err != nil {
			return nil, err
		}
		if h.RequestCount > 0 {
			h.SuccessRate = 100.0 * float64(h.SuccessCount) / float64(h.RequestCount)
		}
		h.AvailabilityScore = availabilityScore(h.SuccessRate, h.AvgLatencyMS, h.RequestCount)
		out[h.ModelID] = h
	}
	return out, rows.Err()
}

// availabilityScore 综合可用度评分（0..100）：
// 成功率 65% 权重 + 延迟 35% 权重（延迟 2s+ 视为 0 分）。
// 无流量时返回 0（前端显示"无数据"）。
func availabilityScore(successRate float64, avgLatencyMS int64, total int64) float64 {
	if total <= 0 {
		return 0
	}
	successFraction := successRate / 100.0
	latencyNorm := 1.0 - float64(avgLatencyMS)/2000.0
	if latencyNorm < 0 {
		latencyNorm = 0
	} else if latencyNorm > 1 {
		latencyNorm = 1
	}
	return 100.0 * (0.65*successFraction + 0.35*latencyNorm)
}
