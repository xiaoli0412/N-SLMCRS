// Package data 提供 SQLite 数据存储与时序指标能力。
//
// Store 封装所有数据库访问。上层（scheduler/entry/modelmeta）通过 Store 读写
// 上游密钥、下游凭证、模型目录、请求记录与日志。时序指标基于 request_logs 表
// 按时间窗口聚合查询。
package data

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无需 CGO
)

//go:embed schema.sql
var schemaSQL string

// Store SQLite 数据存储。
type Store struct {
	db *sql.DB
}

// Open 打开/创建数据库并初始化 schema。path 为 SQLite 文件路径。
func Open(path string) (*Store, error) {
	// 自动创建数据目录，避免 "unable to open database file (14)"
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录 %s: %w", dir, err)
		}
	}
	// busy_timeout 缓解并发写竞争；journal=WAL 提升读写并发
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}
	// SQLite 单写者，限制连接池避免 lock 冲突
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// init 创建表结构并执行运行时迁移（兼容旧库）。
func (s *Store) init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("初始化 schema: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		return fmt.Errorf("运行时迁移: %w", err)
	}
	return nil
}

// migrate 对已存在的旧库补列。SQLite 不支持 ADD COLUMN IF NOT EXISTS，
// 故用 pragma_table_info 探测后 ALTER，保证向前兼容（v0.6 及更早库升级不报错）。
// 新增列一律允许 NULL/DEFAULT，不破坏旧行。
func (s *Store) migrate(ctx context.Context) error {
	// models 表：status（active|gone|disabled）+ last_seen_active_at
	if err := s.addColumnIfMissing(ctx, "models", "status", "TEXT DEFAULT 'active'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "models", "last_seen_active_at", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

// addColumnIfMissing 若表已有该列则跳过，否则 ALTER TABLE ADD COLUMN。
func (s *Store) addColumnIfMissing(ctx context.Context, table, column, decl string) error {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&name)
	if err == nil {
		return nil // 列已存在
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("探测列 %s.%s: %w", table, column, err)
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl))
	return err
}

// Close 关闭数据库。
func (s *Store) Close() error {
	return s.db.Close()
}

// DB 暴露底层 *sql.DB（仅限同包或需要原始查询的内部场景）。
func (s *Store) DB() *sql.DB { return s.db }

// now 返回当前 Unix 秒。
func now() int64 { return time.Now().Unix() }
