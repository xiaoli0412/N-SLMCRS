// Package modelmeta 提供模型目录同步与失效检测。
//
// 职责：
//   - 周期性（默认 24h）从 NVIDIA /v1/models 同步模型列表
//   - 软失效：标记「上次有、本次没有」的下线模型
//   - 失效检测：请求到失效模型时，推荐当前成功率最高的替代模型
package modelmeta

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// StalenessChecker 模型失效检测器。
type StalenessChecker struct {
	store *data.Store
}

// NewStalenessChecker 创建失效检测器。
func NewStalenessChecker(store *data.Store) *StalenessChecker {
	return &StalenessChecker{store: store}
}

// CheckResult 失效检测结果。
type CheckResult struct {
	Stale          bool
	SuggestedModel string
	SuggestedRate  float64
}

// Check 检查模型是否已失效。
// stale=true 表示模型已下线或不可用，同时返回建议的替代模型。
func (s *StalenessChecker) Check(ctx context.Context, modelID string) (stale bool, suggestedModel string, suggestedRate float64) {
	if modelID == "" {
		return false, "", 0
	}
	m, err := s.store.GetModel(ctx, modelID)
	if err != nil {
		// 查询出错，不阻塞，放行让上游决定
		return false, "", 0
	}
	if m == nil {
		// 模型不在目录中：可能是从未同步过，或确实不存在
		// 推荐 chat 类最高成功率模型
		alt, rate, _ := s.store.SuggestBestModel(ctx, "chat")
		return true, alt, rate
	}
	if !m.IsActive {
		alt, rate, _ := s.store.SuggestBestModel(ctx, m.Capability)
		return true, alt, rate
	}
	return false, "", 0
}

// Syncer 模型目录同步器。
type Syncer struct {
	store    *data.Store
	client   *upstream.Client
	interval time.Duration
	// 同步用的 Key（取第一个可用上游 Key）
}

// NewSyncer 创建同步器。
func NewSyncer(store *data.Store, client *upstream.Client, interval time.Duration) *Syncer {
	return &Syncer{store: store, client: client, interval: interval}
}

// Start 启动周期同步（阻塞调用方，应在 goroutine 中调用）。
// 若数据库中尚无模型数据，先立即同步一次。
func (s *Syncer) Start(ctx context.Context) {
	// 启动时若数据库为空，立即同步
	hasData, _ := s.store.ModelHasData(ctx)
	if !hasData {
		log.Println("[modelmeta] 数据库无模型数据，启动时立即同步")
		if err := s.SyncOnce(ctx); err != nil {
			log.Printf("[modelmeta] 启动同步失败: %v", err)
		}
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[modelmeta] 同步器停止")
			return
		case <-ticker.C:
			if err := s.SyncOnce(ctx); err != nil {
				log.Printf("[modelmeta] 周期同步失败: %v", err)
			}
		}
	}
}

// SyncOnce 执行一次同步。
func (s *Syncer) SyncOnce(ctx context.Context) error {
	// 取第一个可用上游 Key 用于同步
	keys, err := s.store.ListUpstreamKeys(ctx)
	if err != nil {
		return fmt.Errorf("获取上游密钥: %w", err)
	}
	var syncKey string
	for _, k := range keys {
		if k.Enabled && k.Status != "circuit_open" && k.Status != "disabled" {
			syncKey = k.KeyValue
			break
		}
	}
	if syncKey == "" {
		return fmt.Errorf("无可用上游密钥用于同步模型目录")
	}

	resp, err := s.client.ListModels(ctx, syncKey)
	if err != nil {
		return fmt.Errorf("拉取模型列表: %w", err)
	}

	models := make([]data.Model, len(resp.Data))
	for i, m := range resp.Data {
		models[i] = data.Model{
			ID:       m.ID,
			Object:   m.Object,
			Created:  m.Created,
			OwnedBy:  m.OwnedBy,
			Root:     m.Root,
			// capability 默认 chat，Phase 2 从模型卡增强
			Capability: "chat",
		}
	}

	active, err := s.store.UpsertModels(ctx, models)
	if err != nil {
		return fmt.Errorf("写入模型目录: %w", err)
	}
	log.Printf("[modelmeta] 同步完成: %d 个模型可用", active)
	return nil
}
