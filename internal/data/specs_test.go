package data

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUpsertAndListModelSpecs(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	r := ModelSpecRow{
		Model:           "meta/llama-3.1-8b-instruct",
		MaxTokens:       8192,
		PricingIn:       "0.0001",
		PricingOut:      "0.0002",
		License:         "llama3.1",
		InputModalities: "text,image",
		ReleaseDate:     "2024-07-23",
		CardURL:         "https://openrouter.ai/meta/llama-3.1-8b-instruct",
	}
	if err := store.UpsertModelSpec(ctx, r); err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}

	// 覆盖写：同 model 不同值
	r2 := r
	r2.MaxTokens = 16384
	r2.PricingIn = "0.0003"
	if err := store.UpsertModelSpec(ctx, r2); err != nil {
		t.Fatalf("Upsert 2 (覆盖): %v", err)
	}

	// 第二个模型
	r3 := ModelSpecRow{Model: "qwen/qwen2-7b", MaxTokens: 32768, License: "qwen"}
	if err := store.UpsertModelSpec(ctx, r3); err != nil {
		t.Fatalf("Upsert 3: %v", err)
	}

	specs, err := store.ListModelSpecs(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("specs 数 = %d, want 2", len(specs))
	}

	got, ok := specs["meta/llama-3.1-8b-instruct"]
	if !ok {
		t.Fatal("未找到 llama 规格")
	}
	if got.MaxTokens != 16384 {
		t.Errorf("覆盖后 MaxTokens = %d, want 16384", got.MaxTokens)
	}
	if got.PricingIn != "0.0003" {
		t.Errorf("覆盖后 PricingIn = %s, want 0.0003", got.PricingIn)
	}
	if got.SyncedAt == 0 {
		t.Error("SyncedAt 未写入")
	}
}
