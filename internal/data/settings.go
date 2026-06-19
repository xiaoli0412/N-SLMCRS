package data

import (
	"context"
	"time"
)

// GetSetting 读取一条动态设置。不存在时返回 ("", nil)。
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		// 不存在视为空值
		return "", nil
	}
	return v, nil
}

// SetSetting 写入（或更新）一条动态设置。
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

// DeleteSetting 删除一条动态设置（不存在不报错）。
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

// ListSettingsByPrefix 列出所有 key 以 prefix 开头的设置（用于 pending 建议等批量读取）。
func (s *Store) ListSettingsByPrefix(ctx context.Context, prefix string) ([]SettingEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, updated_at FROM settings WHERE key LIKE ?`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SettingEntry
	for rows.Next() {
		var e SettingEntry
		if err := rows.Scan(&e.Key, &e.Value, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SettingEntry 单条动态设置。
type SettingEntry struct {
	Key       string
	Value     string
	UpdatedAt int64
}
