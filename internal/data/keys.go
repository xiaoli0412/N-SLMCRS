package data

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpstreamKey 上游 NVIDIA 密钥。
type UpstreamKey struct {
	ID              int64
	KeyValue        string // 完整 nvapi-xxx（应用层加密）
	KeyMask         string // 脱敏展示
	Label           string
	Email           string
	RPMOverride     int  // 0=用默认
	Enabled         bool
	Status          string // active|half_open|circuit_open|disabled（half_open=熔断冷却到期的试探态）
	ConsecutiveFail int
	CoolingUntil    int64
	CreatedAt       int64
	UpdatedAt       int64
}

// AddUpstreamKey 新增上游密钥。
func (s *Store) AddUpstreamKey(ctx context.Context, kv, label, email string, rpmOverride int) (*UpstreamKey, error) {
	if kv == "" {
		return nil, errors.New("key 不能为空")
	}
	ts := now()
	k := &UpstreamKey{
		KeyValue:    kv,
		KeyMask:     maskKey(kv),
		Label:       label,
		Email:       email,
		RPMOverride: rpmOverride,
		Enabled:     true,
		Status:      "active",
		CreatedAt:   ts,
		UpdatedAt:   ts,
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO upstream_keys (key_value, key_mask, label, email, rpm_override, enabled, status, created_at, updated_at)
		VALUES (?,?,?,?,?,1,'active',?,?)`,
		k.KeyValue, k.KeyMask, k.Label, k.Email, k.RPMOverride, k.CreatedAt, k.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("新增上游密钥: %w", err)
	}
	k.ID, _ = res.LastInsertId()
	return k, nil
}

// ListUpstreamKeys 列出所有上游密钥（不返回明文 KeyValue 的全部，调用方按需）。
func (s *Store) ListUpstreamKeys(ctx context.Context) ([]UpstreamKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, key_value, key_mask, label, email, rpm_override, enabled, status,
		       consecutive_fail, cooling_until, created_at, updated_at
		FROM upstream_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UpstreamKey
	for rows.Next() {
		var k UpstreamKey
		var enabled int
		if err := rows.Scan(&k.ID, &k.KeyValue, &k.KeyMask, &k.Label, &k.Email, &k.RPMOverride,
			&enabled, &k.Status, &k.ConsecutiveFail, &k.CoolingUntil, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		k.Enabled = enabled == 1
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetUpstreamKey 按 ID 获取。
func (s *Store) GetUpstreamKey(ctx context.Context, id int64) (*UpstreamKey, error) {
	var k UpstreamKey
	var enabled int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, key_value, key_mask, label, email, rpm_override, enabled, status,
		       consecutive_fail, cooling_until, created_at, updated_at
		FROM upstream_keys WHERE id=?`, id).Scan(
		&k.ID, &k.KeyValue, &k.KeyMask, &k.Label, &k.Email, &k.RPMOverride,
		&enabled, &k.Status, &k.ConsecutiveFail, &k.CoolingUntil, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return nil, err
	}
	k.Enabled = enabled == 1
	return &k, nil
}

// UpdateUpstreamKeyStatus 更新密钥状态与熔断信息（调度层调用）。
func (s *Store) UpdateUpstreamKeyStatus(ctx context.Context, id int64, status string, consecFail int, coolingUntil int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE upstream_keys SET status=?, consecutive_fail=?, cooling_until=?, updated_at=? WHERE id=?`,
		status, consecFail, coolingUntil, now(), id)
	return err
}

// SetUpstreamKeyEnabled 启用/停用密钥。
func (s *Store) SetUpstreamKeyEnabled(ctx context.Context, id int64, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE upstream_keys SET enabled=?, updated_at=? WHERE id=?`, e, now(), id)
	return err
}

// DeleteUpstreamKey 删除上游密钥。
func (s *Store) DeleteUpstreamKey(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM upstream_keys WHERE id=?`, id)
	return err
}

// BulkAddUpstreamKeyResult 单条密钥导入结果。
type BulkAddUpstreamKeyResult struct {
	KeyMask string `json:"key_mask"`           // 成功时为脱敏值
	Status  string `json:"status"`             // added|duplicate|invalid
	Reason  string `json:"reason,omitempty"`    // 跳过原因
}

// BulkAddResult 批量导入汇总。
type BulkAddResult struct {
	Total     int                        `json:"total"`      // 解析得到的去重后总数
	Added     int                        `json:"added"`      // 实际新增
	Skipped   int                        `json:"skipped"`    // 跳过（重复或无效）
	Items     []BulkAddUpstreamKeyResult `json:"items"`      // 逐条结果
	AddedIDs  []int64                    `json:"added_ids"`  // 新增 ID 列表
}

// BulkAddUpstreamKeys 批量导入上游密钥。
//
// 行为：
//   - 在单个事务内插入，保证原子可见性
//   - 同一批次内去重（保留首次出现）
//   - 跳过格式不合法（空或非 nvapi- 前缀）
//   - 数据库 UNIQUE 冲突视为已存在（跳过，不报错）
//   - label/email/rpmOverride 对整批统一应用
//
// Items 按输入顺序逐条返回结果，便于前端逐行展示。
func (s *Store) BulkAddUpstreamKeys(ctx context.Context, keys []string, label, email string, rpmOverride int) (*BulkAddResult, error) {
	res := &BulkAddResult{Items: []BulkAddUpstreamKeyResult{}}
	seen := make(map[string]struct{}, len(keys)) // 批内首次出现的有效密钥

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开启事务: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // 提交后回滚为 no-op

	ts := now()
	for _, raw := range keys {
		kv := strings.TrimSpace(raw)
		if kv == "" {
			continue // 空行静默忽略
		}
		if !strings.HasPrefix(kv, "nvapi-") {
			res.Skipped++
			res.Items = append(res.Items, BulkAddUpstreamKeyResult{Status: "invalid", Reason: "格式无效（必须以 nvapi- 开头）"})
			continue
		}
		if _, dup := seen[kv]; dup {
			res.Skipped++
			res.Items = append(res.Items, BulkAddUpstreamKeyResult{KeyMask: maskKey(kv), Status: "duplicate", Reason: "批次内重复"})
			continue
		}
		seen[kv] = struct{}{}
		res.Total++

		mask := maskKey(kv)
		r, err := tx.ExecContext(ctx, `
			INSERT INTO upstream_keys (key_value, key_mask, label, email, rpm_override, enabled, status, created_at, updated_at)
			VALUES (?,?,?,?,?,1,'active',?,?)`,
			kv, mask, label, email, rpmOverride, ts, ts)
		if err != nil {
			// UNIQUE 冲突等视为已存在（幂等导入不报错）
			res.Skipped++
			res.Items = append(res.Items, BulkAddUpstreamKeyResult{KeyMask: mask, Status: "duplicate", Reason: "已存在"})
			continue
		}
		id, _ := r.LastInsertId()
		res.Added++
		res.AddedIDs = append(res.AddedIDs, id)
		res.Items = append(res.Items, BulkAddUpstreamKeyResult{KeyMask: mask, Status: "added"})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务: %w", err)
	}
	return res, nil
}

// maskKey 脱敏：nvapi-Vkuz...9eR → nvapi-...9eR（保留前缀和末尾 4 位）。
func maskKey(kv string) string {
	if len(kv) <= 12 {
		return kv
	}
	return kv[:6] + "..." + kv[len(kv)-4:]
}

// DownstreamCredential 下游凭证（签发给客户端）。
type DownstreamCredential struct {
	ID             int64
	Credential     string
	CredentialMask string
	Name           string
	Enabled        bool
	RPMLimit       int
	AllowedModels  string
	TotalRequests  int64
	CreatedAt      int64
	UpdatedAt      int64
}

// AddDownstreamCredential 新增下游凭证。
func (s *Store) AddDownstreamCredential(ctx context.Context, cred, name string, rpmLimit int, allowedModels string) (*DownstreamCredential, error) {
	ts := now()
	c := &DownstreamCredential{
		Credential:    cred,
		CredentialMask: maskKey(cred),
		Name:          name,
		Enabled:       true,
		RPMLimit:      rpmLimit,
		AllowedModels: allowedModels,
		CreatedAt:     ts,
		UpdatedAt:     ts,
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO downstream_credentials (credential, credential_mask, name, enabled, rpm_limit, allowed_models, total_requests, created_at, updated_at)
		VALUES (?,?,?,1,?,?,0,?,?)`,
		c.Credential, c.CredentialMask, c.Name, c.RPMLimit, c.AllowedModels, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.ID, _ = res.LastInsertId()
	return c, nil
}

// ListDownstreamCredentials 列出下游凭证。
func (s *Store) ListDownstreamCredentials(ctx context.Context) ([]DownstreamCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, credential, credential_mask, name, enabled, rpm_limit, allowed_models, total_requests, created_at, updated_at
		FROM downstream_credentials ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DownstreamCredential
	for rows.Next() {
		var c DownstreamCredential
		var en int
		if err := rows.Scan(&c.ID, &c.Credential, &c.CredentialMask, &c.Name, &en, &c.RPMLimit,
			&c.AllowedModels, &c.TotalRequests, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Enabled = en == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetDownstreamCredentialByValue 按凭证原文查找（鉴权用）。
func (s *Store) GetDownstreamCredentialByValue(ctx context.Context, cred string) (*DownstreamCredential, error) {
	var c DownstreamCredential
	var en int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, credential, credential_mask, name, enabled, rpm_limit, allowed_models, total_requests, created_at, updated_at
		FROM downstream_credentials WHERE credential=?`, cred).Scan(
		&c.ID, &c.Credential, &c.CredentialMask, &c.Name, &en, &c.RPMLimit,
		&c.AllowedModels, &c.TotalRequests, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.Enabled = en == 1
	return &c, nil
}

// DeleteDownstreamCredential 删除下游凭证。
func (s *Store) DeleteDownstreamCredential(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM downstream_credentials WHERE id=?`, id)
	return err
}

// IncrementCredentialRequests 增加凭证请求计数。
func (s *Store) IncrementCredentialRequests(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE downstream_credentials SET total_requests=total_requests+1, updated_at=? WHERE id=?`, now(), id)
	return err
}

// 防止 time 未使用警告（健康快照时间戳转换预留）
var _ = time.Now
