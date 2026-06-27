package backup

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nslmcrs/gateway/internal/data"
)

// newStore 打开一个临时库并写入一行密钥，供备份有实际内容。
func newStore(t *testing.T) (*data.Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nslmcrs.db")
	store, err := data.Open(dbPath)
	if err != nil {
		t.Fatalf("data.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if _, err := store.BulkAddUpstreamKeys(ctx, []string{"nvapi-AAAA1111BBBB2222CCCC3333DDDD4444"}, "t", "", 0); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}
	return store, dir
}

// openBackup 用纯驱动只读打开备份文件，验证其可查询且数据一致。
func openBackup(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=query_only(1)")
	if err != nil {
		t.Fatalf("打开备份: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestBackupOnceProducesValidSnapshot(t *testing.T) {
	store, dir := newStore(t)
	bdir := filepath.Join(dir, "backups")
	svc := New(store, bdir, 0, 0) // 不启用定时/轮转

	name, err := svc.BackupOnce(context.Background())
	if err != nil {
		t.Fatalf("BackupOnce: %v", err)
	}
	if !nameRe.MatchString(name) {
		t.Fatalf("文件名非法: %s", name)
	}

	// 备份文件应存在且可查到刚写入的密钥
	p, err := svc.Path(name)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	db := openBackup(t, p)
	var n int
	if err := db.QueryRow("SELECT count(*) FROM upstream_keys").Scan(&n); err != nil {
		t.Fatalf("查询备份 upstream_keys: %v", err)
	}
	if n != 1 {
		t.Errorf("备份中密钥数 = %d, want 1", n)
	}
}

func TestListAndOrder(t *testing.T) {
	store, dir := newStore(t)
	svc := New(store, filepath.Join(dir, "backups"), 0, 0)

	// 连续两份备份，名字时间戳应递增
	n1, _ := svc.BackupOnce(context.Background())
	time.Sleep(1100 * time.Millisecond) // 文件名精度到秒，需错开
	n2, _ := svc.BackupOnce(context.Background())

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("列表数 = %d, want 2", len(list))
	}
	// 倒序：新在前
	if list[0].Name != n2 || list[1].Name != n1 {
		t.Errorf("排序错误: got %s,%s want %s,%s", list[0].Name, list[1].Name, n2, n1)
	}
	if list[0].Size <= 0 {
		t.Errorf("Size 非正: %d", list[0].Size)
	}
}

func TestRetentionPrunesOldest(t *testing.T) {
	store, dir := newStore(t)
	svc := New(store, filepath.Join(dir, "backups"), 0, 2) // 仅保留 2 份

	for i := 0; i < 4; i++ {
		if _, err := svc.BackupOnce(context.Background()); err != nil {
			t.Fatalf("BackupOnce #%d: %v", i, err)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	list, _ := svc.List()
	if len(list) != 2 {
		t.Errorf("轮转后列表数 = %d, want 2", len(list))
	}
}

func TestDeleteAndPathTraversalGuard(t *testing.T) {
	store, dir := newStore(t)
	svc := New(store, filepath.Join(dir, "backups"), 0, 0)
	name, _ := svc.BackupOnce(context.Background())

	if _, err := svc.Path("../evil.db"); err == nil {
		t.Error("路径穿越未拦截")
	}
	if _, err := svc.Path("not-a-backup.txt"); err == nil {
		t.Error("非法文件名未拦截")
	}
	if err := svc.Delete(name); err != nil {
		t.Errorf("Delete: %v", err)
	}
	list, _ := svc.List()
	if len(list) != 0 {
		t.Errorf("删除后列表数 = %d, want 0", len(list))
	}
}

func TestStartNoopWhenIntervalZero(t *testing.T) {
	store, dir := newStore(t)
	svc := New(store, filepath.Join(dir, "backups"), 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消，确保 Start 能及时返回而非阻塞
	svc.Start(ctx) // interval<=0 直接返回
}
