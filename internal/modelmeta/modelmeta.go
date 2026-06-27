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
	"strings"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/modelcatalog"
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
	Gone           bool   // true=模型已从上游消失（status=gone）；false=不在目录或临时不可用
	SuggestedModel string
	SuggestedRate  float64
}

// Check 检查模型是否已失效。
// stale=true 表示模型已下线或不可用，同时返回建议的替代模型。
// Gone=true 进一步表示该模型曾存在但已从上游 /v1/models 消失（用于"已消失"提示）。
func (s *StalenessChecker) Check(ctx context.Context, modelID string) CheckResult {
	if modelID == "" {
		return CheckResult{}
	}
	m, err := s.store.GetModel(ctx, modelID)
	if err != nil {
		// 查询出错，不阻塞，放行让上游决定
		return CheckResult{}
	}
	if m == nil {
		// 模型不在目录中：可能是从未同步过，或确实不存在
		alt, rate, _ := s.store.SuggestBestModel(ctx, "chat")
		return CheckResult{Stale: true, Gone: false, SuggestedModel: alt, SuggestedRate: rate}
	}
	if !m.IsActive {
		alt, rate, _ := s.store.SuggestBestModel(ctx, m.Capability)
		// status=gone 表示曾存在现已消失；disabled 等其它视为临时不可用
		return CheckResult{Stale: true, Gone: m.Status == "gone", SuggestedModel: alt, SuggestedRate: rate}
	}
	return CheckResult{}
}

// StaleMessage 失效模型的中英文提示文案。
// gone=true 用"已消失"措辞，否则用"已下线/不可用"。
// 调用方（entry 三协议入口）据此统一构造错误体，消除历史文案不一致。
func StaleMessage(model string, r CheckResult) (zh, en string) {
	if r.Gone {
		zh = fmt.Sprintf("该模型 %s 已从上游消失，不再可用。", model)
		en = fmt.Sprintf("Model %s has been removed from the upstream and is no longer available.", model)
	} else {
		zh = fmt.Sprintf("该模型 %s 已下线或当前不可用。", model)
		en = fmt.Sprintf("Model %s is offline or currently unavailable.", model)
	}
	if r.SuggestedModel != "" {
		zh += fmt.Sprintf(" 建议切换至当前成功率最高的可用模型：%s（成功率 %.1f%%）。", r.SuggestedModel, r.SuggestedRate)
		en += fmt.Sprintf(" Suggested alternative with the highest success rate: %s (%.1f%% success rate).", r.SuggestedModel, r.SuggestedRate)
	}
	return zh, en
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
		// 富元数据补齐：策展表精确命中优先，否则启发式分类 + 参数解析。
		// NVIDIA /v1/models 不返回能力/上下文/参数量，全靠 modelcatalog 推导。
		em := modelcatalog.Enrich(m.ID)
		models[i] = data.Model{
			ID:            m.ID,
			Object:        m.Object,
			Created:       m.Created,
			OwnedBy:       m.OwnedBy,
			Root:          m.Root,
			Capability:    em.Capability,
			ParamCount:    em.ParamCount,
			ContextLength: em.ContextLength,
			Description:   em.Description,
		}
	}

	active, err := s.store.UpsertModels(ctx, models)
	if err != nil {
		return fmt.Errorf("写入模型目录: %w", err)
	}

	// 拉取远程注册表富化扩展规格（max_tokens/定价/许可证/模态/模型卡URL）。
	// 失败不阻断同步——降级用内置策展表，前端 spec 字段显示"—"。
	if specs, err := modelcatalog.SyncRegistry(ctx); err == nil && len(specs) > 0 {
		matched := 0
		for _, m := range models {
			sp, ok := specs[m.ID]
			if !ok {
				continue
			}
			mods := ""
			if len(sp.InputModalities) > 0 {
				mods = strings.Join(sp.InputModalities, ",")
			}
			// v0.9：HuggingFace 富化架构/许可证（失败降级，不阻断）
			if hf, err := modelcatalog.FetchHuggingFace(ctx, m.ID); err == nil {
				if sp.Architecture == "" && hf.Architecture != "" {
					sp.Architecture = hf.Architecture
				}
				if sp.License == "" && hf.License != "" {
					sp.License = hf.License
				}
				if sp.CardURL == "" && hf.CardURL != "" {
					sp.CardURL = hf.CardURL
				}
			}
			// 能力推导的支持接口
			ifaces := modelcatalog.SupportedInterfacesFor(m.Capability)
			row := data.ModelSpecRow{
				Model:           m.ID,
				MaxTokens:       sp.MaxTokens,
				PricingIn:       sp.PricingIn,
				PricingOut:      sp.PricingOut,
				License:         sp.License,
				InputModalities: mods,
				ReleaseDate:     sp.ReleaseDate,
				CardURL:         sp.CardURL,
				Architecture:    sp.Architecture,
			}
			if len(ifaces) > 0 {
				row.SupportedInterfaces = strings.Join(ifaces, ",")
			}
			_ = s.store.UpsertModelSpec(ctx, row)
			matched++
		}
		log.Printf("[modelmeta] 注册表富化: %d/%d 模型命中扩展规格", matched, len(models))
	} else if err != nil {
		log.Printf("[modelmeta] 注册表富化跳过（降级内置策展表）: %v", err)
	}

	log.Printf("[modelmeta] 同步完成: %d 个模型可用", active)
	return nil
}
