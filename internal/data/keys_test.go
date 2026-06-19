package data

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBulkAddUpstreamKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// 测试1：混合批量（2合法、1非法、1批内重复）
	res, err := store.BulkAddUpstreamKeys(ctx, []string{
		"nvapi-AAAA1111BBBB2222CCCC3333DDDD4444",
		"nvapi-EEEE5555FFFF6666",
		"not-a-key",
		"nvapi-AAAA1111BBBB2222CCCC3333DDDD4444", // 批内重复
	}, "批次1", "", 0)
	if err != nil {
		t.Fatalf("BulkAdd 1: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2 (去重后唯一有效数)", res.Total)
	}
	if res.Added != 2 {
		t.Errorf("Added = %d, want 2", res.Added)
	}
	if res.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2 (1 invalid + 1 batch-dup)", res.Skipped)
	}
	if len(res.Items) != 4 {
		t.Errorf("Items 数 = %d, want 4 (逐条结果)", len(res.Items))
	}

	// 测试2：幂等 — 重复导入已存在 + 新增
	res2, err := store.BulkAddUpstreamKeys(ctx, []string{
		"nvapi-AAAA1111BBBB2222CCCC3333DDDD4444", // 已存在 -> duplicate
		"nvapi-ZZZZ9999YYYY8888",                  // 新
	}, "批次2", "", 0)
	if err != nil {
		t.Fatalf("BulkAdd 2: %v", err)
	}
	if res2.Added != 1 {
		t.Errorf("幂等 Added = %d, want 1", res2.Added)
	}
	if res2.Skipped != 1 {
		t.Errorf("幂等 Skipped = %d, want 1", res2.Skipped)
	}

	// 校验最终密钥数
	all, err := store.ListUpstreamKeys(ctx)
	if err != nil {
		t.Fatalf("ListUpstreamKeys: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("最终密钥数 = %d, want 3", len(all))
	}

	// 空批次
	res3, err := store.BulkAddUpstreamKeys(ctx, []string{"", "   "}, "", "", 0)
	if err != nil {
		t.Fatalf("BulkAdd 3: %v", err)
	}
	if res3.Total != 0 || res3.Added != 0 {
		t.Errorf("空批次 Total=%d Added=%d, want 0/0", res3.Total, res3.Added)
	}
}
