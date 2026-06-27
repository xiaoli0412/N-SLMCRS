package data

import (
	"context"
	"database/sql"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Channel 集成渠道（new-api / sapi 等下游中转网关对接）。
//
// 渠道密钥经 bcrypt 哈希存储（api_key_hash），不下发明文；渠道通过 OpenAI 兼容
// 协议（/v1/*）接入本网关，模型列表走 /v1/models，计费回采走 /api/admin/hooks/channels/:id/usage。
type Channel struct {
	ID            int64
	Name          string
	Type          string // newapi | sapi
	BaseURL       string
	APIKeyMask    string
	APIKeyHash    string
	Enabled       bool
	LastSyncAt    int64
	TotalRequests int64
	CreatedAt     int64
	UpdatedAt     int64
}

// AddChannel 新增渠道。apiKey 明文入参，内部仅存 mask + bcrypt hash。
func (s *Store) AddChannel(ctx context.Context, name, typ, baseURL, apiKey string) (*Channel, error) {
	ts := now()
	mask := maskChannelKey(apiKey)
	hash, err := bcrypt.GenerateFromPassword([]byte(apiKey), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("哈希渠道密钥: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO channels (name, type, base_url, api_key_mask, api_key_hash, enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,1,?,?)`,
		name, typ, baseURL, mask, hash, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Channel{ID: id, Name: name, Type: typ, BaseURL: baseURL, APIKeyMask: mask, APIKeyHash: string(hash), Enabled: true, CreatedAt: ts, UpdatedAt: ts}, nil
}

// ListChannels 列出全部渠道。
func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, base_url, api_key_mask, enabled, last_sync_at, total_requests, created_at, updated_at
		FROM channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		var enabled int
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.BaseURL, &c.APIKeyMask, &enabled, &c.LastSyncAt, &c.TotalRequests, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Enabled = enabled == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChannel 取单个渠道。
func (s *Store) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	var c Channel
	var enabled int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, base_url, api_key_mask, api_key_hash, enabled, last_sync_at, total_requests, created_at, updated_at
		FROM channels WHERE id=?`, id).Scan(
		&c.ID, &c.Name, &c.Type, &c.BaseURL, &c.APIKeyMask, &c.APIKeyHash, &enabled, &c.LastSyncAt, &c.TotalRequests, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Enabled = enabled == 1
	return &c, nil
}

// DeleteChannel 删除渠道。
func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id=?`, id)
	return err
}

// ToggleChannel 启用/停用渠道。
func (s *Store) ToggleChannel(ctx context.Context, id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET enabled=?, updated_at=? WHERE id=?`, v, now(), id)
	return err
}

// IncChannelRequests 渠道请求计数 +1（计费回采用）。
func (s *Store) IncChannelRequests(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET total_requests=total_requests+1, updated_at=? WHERE id=?`, now(), id)
	return err
}

// maskChannelKey 渠道密钥脱敏：保留前4后4（与 keys.go 的 nvapi 脱敏规则不同，单独命名避免冲突）。
func maskChannelKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "..." + k[len(k)-4:]
}
