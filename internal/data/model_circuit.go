package data

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ModelCircuit 模型级熔断状态。
//
// 与按 Key 熔断（upstream_keys.status）互补：按模型聚合成功率，
// 失败模型从 /v1/models 隐藏并对请求返回熔断说明。permanent=1 表示
// 永久熔断（连续多次健康扫描低于地板），仅人工复位解除。
type ModelCircuit struct {
	Model            string
	State            string // closed|open|half_open|permanent
	OpenUntil        int64  // 冷却到期 Unix 秒
	ConsecutiveFail  int
	SuccessRatePct   int
	BadSweepCount    int
	Permanent        bool
	LastSweepAt      int64
	UpdatedAt        int64
}

// ModelCircuitState 熔断状态常量。
const (
	CircuitClosed     = "closed"
	CircuitOpen       = "open"
	CircuitHalfOpen   = "half_open"
	CircuitPermanent  = "permanent"
)

// GetModelCircuit 取模型熔断状态；无记录返回 nil（视为 closed）。
func (s *Store) GetModelCircuit(ctx context.Context, model string) (*ModelCircuit, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT model, state, open_until, consecutive_fail, success_rate_pct,
		       bad_sweep_count, permanent, last_sweep_at, updated_at
		FROM model_circuit WHERE model=?`, model)
	mc, err := scanCircuit(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return mc, err
}

// ListModelCircuitByState 列出处于任一指定状态的模型熔断记录。
func (s *Store) ListModelCircuitByState(ctx context.Context, states ...string) ([]ModelCircuit, error) {
	if len(states) == 0 {
		return s.listModelCircuitAll(ctx)
	}
	q := `SELECT model, state, open_until, consecutive_fail, success_rate_pct,
	             bad_sweep_count, permanent, last_sweep_at, updated_at
	      FROM model_circuit WHERE state IN (` + placeholders(len(states)) + `)
	      ORDER BY state, model`
	args := make([]any, len(states))
	for i, st := range states {
		args[i] = st
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCircuits(rows)
}

// ListModelCircuitAll 列出全部熔断记录。
func (s *Store) ListModelCircuitAll(ctx context.Context) ([]ModelCircuit, error) {
	return s.listModelCircuitAll(ctx)
}

func (s *Store) listModelCircuitAll(ctx context.Context) ([]ModelCircuit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT model, state, open_until, consecutive_fail, success_rate_pct,
		       bad_sweep_count, permanent, last_sweep_at, updated_at
		FROM model_circuit ORDER BY state, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCircuits(rows)
}

// UpsertModelCircuit 写入/更新模型熔断状态。零值字段保持语义由调用方组装。
func (s *Store) UpsertModelCircuit(ctx context.Context, mc ModelCircuit) error {
	perm := 0
	if mc.Permanent {
		perm = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_circuit
		    (model, state, open_until, consecutive_fail, success_rate_pct,
		     bad_sweep_count, permanent, last_sweep_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(model) DO UPDATE SET
		    state=excluded.state,
		    open_until=excluded.open_until,
		    consecutive_fail=excluded.consecutive_fail,
		    success_rate_pct=excluded.success_rate_pct,
		    bad_sweep_count=excluded.bad_sweep_count,
		    permanent=excluded.permanent,
		    last_sweep_at=excluded.last_sweep_at,
		    updated_at=excluded.updated_at`,
		mc.Model, defaultStr(mc.State, CircuitClosed), mc.OpenUntil, mc.ConsecutiveFail,
		mc.SuccessRatePct, mc.BadSweepCount, perm, mc.LastSweepAt, now())
	return err
}

// RecordModelCircuitFailure 被动路径：模型请求失败时累加连续失败，达阈值转 open。
// threshold<=0 时不熔断；成功由 ResetModelCircuitConsecutive 处理。
func (s *Store) RecordModelCircuitFailure(ctx context.Context, model string, threshold int, cooldownSec int64) error {
	if threshold <= 0 {
		return nil
	}
	mc, err := s.GetModelCircuit(ctx, model)
	if err != nil {
		return err
	}
	if mc == nil {
		mc = &ModelCircuit{Model: model, State: CircuitClosed, SuccessRatePct: 100}
	}
	if mc.State == CircuitPermanent {
		return nil // 永久熔断不因被动失败变化
	}
	mc.ConsecutiveFail++
	if mc.ConsecutiveFail >= threshold {
		mc.State = CircuitOpen
		mc.OpenUntil = time.Now().Unix() + cooldownSec
	}
	mc.UpdatedAt = now()
	return s.UpsertModelCircuit(ctx, *mc)
}

// ResetModelCircuitConsecutive 被动路径：模型请求成功时清零连续失败，
// open/half_open 态回退 closed（永久熔断不受影响）。
func (s *Store) ResetModelCircuitConsecutive(ctx context.Context, model string) error {
	mc, err := s.GetModelCircuit(ctx, model)
	if err != nil || mc == nil {
		return err
	}
	if mc.State == CircuitPermanent {
		return nil
	}
	mc.ConsecutiveFail = 0
	if mc.State == CircuitOpen || mc.State == CircuitHalfOpen {
		mc.State = CircuitClosed
		mc.OpenUntil = 0
	}
	mc.UpdatedAt = now()
	return s.UpsertModelCircuit(ctx, *mc)
}

// ResetModelCircuit 手动复位模型熔断（解除永久/临时熔断）。
func (s *Store) ResetModelCircuit(ctx context.Context, model string) error {
	mc, err := s.GetModelCircuit(ctx, model)
	if err != nil {
		return err
	}
	if mc == nil {
		return nil
	}
	mc.State = CircuitClosed
	mc.OpenUntil = 0
	mc.ConsecutiveFail = 0
	mc.BadSweepCount = 0
	mc.Permanent = false
	mc.UpdatedAt = now()
	return s.UpsertModelCircuit(ctx, *mc)
}

// IsModelCircuitOpen 模型是否处于请求拒绝态（open 未过冷却 或 permanent）。
// half_open 允许试探放行，返回 false。
func (s *Store) IsModelCircuitOpen(ctx context.Context, model string) (bool, string, error) {
	mc, err := s.GetModelCircuit(ctx, model)
	if err != nil || mc == nil {
		return false, CircuitClosed, err
	}
	if mc.State == CircuitPermanent {
		return true, CircuitPermanent, nil
	}
	if mc.State == CircuitOpen && mc.OpenUntil > time.Now().Unix() {
		return true, CircuitOpen, nil
	}
	return false, mc.State, nil
}

// ListCircuitHiddenModels 返回应对 /v1/models 隐藏的模型 id 集合
// （open 未过冷却 + permanent）。half_open 与 closed 不隐藏。
func (s *Store) ListCircuitHiddenModels(ctx context.Context) (map[string]string, error) {
	nows := time.Now().Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT model, state FROM model_circuit
		WHERE permanent=1 OR (state=? AND open_until>?)`, CircuitOpen, nows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var model, state string
		if err := rows.Scan(&model, &state); err != nil {
			return nil, err
		}
		out[model] = state
	}
	return out, rows.Err()
}

// --- 扫描辅助 ---

type circuitScanner interface {
	Scan(dest ...any) error
}

func scanCircuit(sc circuitScanner) (*ModelCircuit, error) {
	var mc ModelCircuit
	var perm int
	if err := sc.Scan(&mc.Model, &mc.State, &mc.OpenUntil, &mc.ConsecutiveFail,
		&mc.SuccessRatePct, &mc.BadSweepCount, &perm, &mc.LastSweepAt, &mc.UpdatedAt); err != nil {
		return nil, err
	}
	mc.Permanent = perm == 1
	return &mc, nil
}

func scanCircuits(rows *sql.Rows) ([]ModelCircuit, error) {
	var out []ModelCircuit
	for rows.Next() {
		var mc ModelCircuit
		var perm int
		if err := rows.Scan(&mc.Model, &mc.State, &mc.OpenUntil, &mc.ConsecutiveFail,
			&mc.SuccessRatePct, &mc.BadSweepCount, &perm, &mc.LastSweepAt, &mc.UpdatedAt); err != nil {
			return nil, err
		}
		mc.Permanent = perm == 1
		out = append(out, mc)
	}
	return out, rows.Err()
}

// placeholders 生成 n 个问号占位（用于 IN 子句）。
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for i := 1; i < n; i++ {
		out += ",?"
	}
	return out
}

var _ = fmt.Sprintf
