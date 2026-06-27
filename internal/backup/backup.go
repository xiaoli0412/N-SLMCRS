// Package backup 提供 SQLite 数据库的定时与按需备份能力（v0.8 新增）。
//
// 备份机制：VACUUM INTO '<dir>/nslmcrs-<ts>.db' 产出事务一致快照。
// 相比裸文件拷贝，VACUUM INTO 在 WAL 模式下也能得到一致的、无撕裂的副本
// （SQLite 在一个事务内完成整库拷贝），且不需要停服。
//
// 调度仿 modelmeta.Syncer：后台 ticker + ctx.Done() 退出；
// BackupOnce 同时供 admin API 即时触发。按 retention 数轮转删最旧。
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
)

// 备份文件名前缀与时间格式（文件名按字典序即时间序，便于轮转排序）。
const (
	filePrefix  = "nslmcrs-"
	timeFormat  = "20060102-150405"
	fileSuffix  = ".db"
	maxFileLen  = 64 // 文件名长度上限（含时间戳，防御性）
)

// nameRe 校验备份文件名（路径白名单，防穿越）。仅允许 nslmcrs-<数字/-字母>.db。
var nameRe = regexp.MustCompile(`^nslmcrs-[0-9]{8}-[0-9]{6}\.db$`)

// Service 数据库备份服务。
type Service struct {
	store     *data.Store
	dir       string
	interval  time.Duration
	retention int
}

// New 创建备份服务。dir 为备份目录；interval<=0 禁用定时；retention<=0 不自动清理。
func New(store *data.Store, dir string, interval time.Duration, retention int) *Service {
	return &Service{store: store, dir: dir, interval: interval, retention: retention}
}

// Start 启动周期备份（阻塞，应在 goroutine 中调用）。
// interval<=0 时直接返回（不启用定时备份，仅保留手动 API 触发）。
func (s *Service) Start(ctx context.Context) {
	if s.interval <= 0 {
		log.Println("[backup] 定时备份未启用（BACKUP_INTERVAL<=0），仅支持手动触发")
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[backup] 备份服务停止")
			return
		case <-ticker.C:
			if _, err := s.BackupOnce(ctx); err != nil {
				log.Printf("[backup] 周期备份失败: %v", err)
			}
		}
	}
}

// BackupOnce 立即执行一次备份，返回备份文件名（相对 s.dir）。
// 流程：建目录 → VACUUM INTO → 按 retention 轮转删旧。
func (s *Service) BackupOnce(ctx context.Context) (string, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录 %s: %w", s.dir, err)
	}
	name := filePrefix + time.Now().Format(timeFormat) + fileSuffix
	if len(name) > maxFileLen {
		return "", fmt.Errorf("备份文件名过长: %s", name)
	}
	dst := filepath.Join(s.dir, name)

	// VACUUM INTO 要求目标文件不存在；时间戳唯一，天然满足。
	// 用底层 *sql.DB 执行（Store.DB() 仅供内部场景，此处备份属同源内部）。
	if err := s.execVacuumInto(ctx, dst); err != nil {
		return "", fmt.Errorf("VACUUM INTO 失败: %w", err)
	}
	log.Printf("[backup] 备份完成: %s", name)

	if s.retention > 0 {
		if err := s.prune(ctx); err != nil {
			log.Printf("[backup] 轮转清理失败（不影响本次备份）: %v", err)
		}
	}
	return name, nil
}

// execVacuumInto 执行 VACUUM INTO 'path'。拆出以便测试注入伪 DB。
func (s *Service) execVacuumInto(ctx context.Context, dst string) error {
	// 引号转义防御：路径仅由本服务构造（dir + 受控文件名），但 VACUUM INTO 的
	// 参数是 SQL 字面量无参数绑定，故对单引号做转义以防注入。
	q := fmt.Sprintf("VACUUM INTO %s", quoteSQLString(dst))
	_, err := s.dbExec(ctx, q)
	return err
}

// dbExec 默认走 Store.DB()；测试可替换。
func (s *Service) dbExec(ctx context.Context, query string) (sql.Result, error) {
	return s.store.DB().ExecContext(ctx, query)
}

// quoteSQLString 把字符串转为单引号包裹、内部单引号双写的 SQL 字符串字面量。
func quoteSQLString(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			out = append(out, '\'', '\'')
			continue
		}
		out = append(out, c)
	}
	out = append(out, '\'')
	return string(out)
}

// Info 备份文件元信息（供 admin API 列表展示）。
type Info struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"` // Unix 秒
}

// List 列出备份目录下所有合法备份文件，按时间倒序（新→旧）。
func (s *Service) List() ([]Info, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Info{}, nil
		}
		return nil, fmt.Errorf("读取备份目录: %w", err)
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !nameRe.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Size: fi.Size(), ModTime: fi.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out, nil
}

// Path 返回备份文件的绝对路径；name 非法则返回错误（防穿越）。
func (s *Service) Path(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("非法备份文件名: %s", name)
	}
	return filepath.Join(s.dir, name), nil
}

// Delete 删除指定备份文件。
func (s *Service) Delete(name string) error {
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除备份: %w", err)
	}
	return nil
}

// prune 按 retention 数删除最旧备份，保留最近 retention 份。
func (s *Service) prune(_ context.Context) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	if len(list) <= s.retention {
		return nil
	}
	// List 已按新→旧排序，超出 retention 的尾部即最旧。
	for _, old := range list[s.retention:] {
		if err := os.Remove(filepath.Join(s.dir, old.Name)); err != nil && !os.IsNotExist(err) {
			log.Printf("[backup] 删除旧备份 %s 失败: %v", old.Name, err)
		}
	}
	return nil
}
