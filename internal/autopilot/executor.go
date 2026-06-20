package autopilot

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
)

// minConfidence 破坏性动作的置信度门槛。
const minConfidence = 0.7

// pendingPrefix assisted 模式 pending 建议在 settings 表中的 key 前缀。
const pendingPrefix = "autopilot:pending:"

// Executor 按 mode 决定动作执行边界。
//
//	manual   → 仅写审计(app_logs)，不执行任何变更
//	assisted → 可逆调参(set_concurrency/set_weight_boost)即时写 Runtime 生效；
//	           破坏性动作(disable_key/open_circuit/revoke_credential)写 pending(settings) 等人工确认
//	fullauto → 直接执行全部动作（调参写 Runtime，破坏性落库）
type Executor struct {
	store   *data.Store
	runtime *Runtime

	// 执行统计（近 60s 滑动计数）
	// 由 Controller 读写；这里只做原子累加与统计
	statsMu      sync.Mutex
	decisionTS   []int64 // 决策时间戳（近 60s）
	interventions int    // 累计执行动作数
	recentEvents []EventRecord // 环形缓冲（最新 N 条）
}

// NewExecutor 创建执行器。
func NewExecutor(store *data.Store, rt *Runtime) *Executor {
	return &Executor{store: store, runtime: rt}
}

// Apply 应用一批动作。返回该批实际执行的动作数（用于统计）。
func (e *Executor) Apply(ctx context.Context, mode Mode, actions []Action) int {
	if len(actions) == 0 {
		return 0
	}

	applied := 0
	for _, a := range actions {
		switch mode {
		case ModeManual:
			// 仅观察记录
			e.recordEvent(ctx, a, false)
			e.logAudit(ctx, a, false, "manual:仅观察")

		case ModeAssisted:
			if isDestructive(a.Kind) {
				// 破坏性动作写 pending 待人工确认
				if err := e.writePending(ctx, a); err == nil {
					e.logAudit(ctx, a, false, "assisted:破坏性动作已写入pending")
				}
			} else {
				// 可逆调参：即时写 Runtime 生效（仍记录审计，便于追溯）
				if err := e.execute(ctx, a); err != nil {
					e.logAudit(ctx, a, false, fmt.Sprintf("assisted:可逆调参执行失败 %v", err))
					continue
				}
				applied++
				e.recordEvent(ctx, a, true)
				e.logAudit(ctx, a, true, "assisted:可逆调参即时生效")
			}

		case ModeFullAuto:
			// 破坏性动作需过置信度门槛
			if isDestructive(a.Kind) && a.Confidence < minConfidence {
				e.recordEvent(ctx, a, false)
				e.logAudit(ctx, a, false, fmt.Sprintf("fullauto:置信度%.2f<%.2f跳过", a.Confidence, minConfidence))
				continue
			}
			if err := e.execute(ctx, a); err != nil {
				e.logAudit(ctx, a, false, fmt.Sprintf("fullauto:执行失败 %v", err))
				continue
			}
			applied++
			e.recordEvent(ctx, a, true)
			e.logAudit(ctx, a, true, "fullauto:已执行")
		}
	}

	// 统计决策频率（assisted/manual 也算一次"决策"）
	e.bumpDecision()
	return applied
}

// execute 执行单个动作（fullauto 直接落库/写 Runtime）。
func (e *Executor) execute(ctx context.Context, a Action) error {
	switch a.Kind {
	case ActSetConcurrency:
		n := int(a.Value)
		if n < 1 {
			n = 1
		}
		e.runtime.SetConcurrency(n)
		return nil

	case ActSetWeightBoost:
		e.runtime.SetWeightBoost(a.KeyID, a.Value)
		return nil

	case ActDisableKey:
		return e.store.SetUpstreamKeyEnabled(ctx, a.KeyID, false)

	case ActOpenCircuit:
		seconds := int(a.Value)
		if seconds <= 0 {
			seconds = 60
		}
		coolUntil := time.Now().Add(time.Duration(seconds) * time.Second).Unix()
		return e.store.UpdateUpstreamKeyStatus(ctx, a.KeyID, "circuit_open", 0, coolUntil)

	case ActRevokeCredential:
		return e.store.DeleteDownstreamCredential(ctx, a.CredID)
	}
	return fmt.Errorf("未知动作类型: %s", a.Kind)
}

// isDestructive 判断动作是否破坏性（需置信度门槛）。
func isDestructive(k ActionKind) bool {
	switch k {
	case ActDisableKey, ActOpenCircuit, ActRevokeCredential:
		return true
	}
	return false
}

// writePending 把 assisted 模式的建议写入 settings 表（等人工确认）。
func (e *Executor) writePending(ctx context.Context, a Action) error {
	key := fmt.Sprintf("%s%d", pendingPrefix, time.Now().UnixNano())
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return e.store.SetSetting(ctx, key, string(b))
}

// ApprovePending 批准一个 pending 建议（按 settings key）。
func (e *Executor) ApprovePending(ctx context.Context, settingKey string) error {
	v, err := e.store.GetSetting(ctx, settingKey)
	if err != nil {
		return err
	}
	var a Action
	if err := json.Unmarshal([]byte(v), &a); err != nil {
		return fmt.Errorf("解析 pending 建议: %w", err)
	}
	if err := e.execute(ctx, a); err != nil {
		return err
	}
	e.logAudit(ctx, a, true, "assisted:人工批准后执行")
	e.store.DeleteSetting(ctx, settingKey)
	return nil
}

// RejectPending 驳回一个 pending 建议。
func (e *Executor) RejectPending(ctx context.Context, settingKey string) error {
	v, _ := e.store.GetSetting(ctx, settingKey)
	var a Action
	_ = json.Unmarshal([]byte(v), &a)
	e.logAudit(ctx, a, false, "assisted:人工驳回")
	return e.store.DeleteSetting(ctx, settingKey)
}

// logAudit 写一条审计日志到 app_logs。
func (e *Executor) logAudit(ctx context.Context, a Action, applied bool, note string) {
	level := "info"
	if isDestructive(a.Kind) {
		level = "warn"
	}
	ctxJSON, _ := json.Marshal(map[string]any{
		"action":     a,
		"applied":    applied,
		"note":       note,
		"kind":       string(a.Kind),
		"confidence": a.Confidence,
	})
	_ = e.store.WriteLog(ctx, level, "autopilot", "",
		fmt.Sprintf("%s｜%s｜%s", string(a.Kind), a.Reason, note), string(ctxJSON))
}

// recordEvent 追加到环形缓冲（最近事件，前端展示，保留 50 条）。
func (e *Executor) recordEvent(_ context.Context, a Action, applied bool) {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.recentEvents = append(e.recentEvents, EventRecord{
		TS:         time.Now().Unix(),
		Engine:     a.Source,
		Mode:       "", // 由 Controller 在调用时已确定；这里仅动作来源
		Kind:       a.Kind,
		Detail:     a.Reason,
		Reason:     a.Reason,
		Confidence: a.Confidence,
		Applied:    applied,
	})
	if len(e.recentEvents) > 50 {
		e.recentEvents = e.recentEvents[len(e.recentEvents)-50:]
	}
}

// bumpDecision 记录一次决策时间戳（用于 decisions/min 统计）。
func (e *Executor) bumpDecision() {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	now := time.Now().Unix()
	e.decisionTS = append(e.decisionTS, now)
	// 清理 60s 前
	cutoff := now - 60
	i := 0
	for i < len(e.decisionTS) && e.decisionTS[i] < cutoff {
		i++
	}
	e.decisionTS = e.decisionTS[i:]
}

// Stats 返回决策频率、累计干预数、最近事件。
func (e *Executor) Stats() (decisionsPerMin, interventions int, events []EventRecord) {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	dpm := len(e.decisionTS)
	// 复制事件避免外部修改
	ev := make([]EventRecord, len(e.recentEvents))
	copy(ev, e.recentEvents)
	// 倒序（最新在前）
	for i, j := 0, len(ev)-1; i < j; i, j = i+1, j-1 {
		ev[i], ev[j] = ev[j], ev[i]
	}
	return dpm, e.interventions, ev
}

// AddIntervention 累加干预计数（fullauto 执行成功后由 Controller 调用）。
func (e *Executor) AddIntervention(n int) {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	e.interventions += n
}

// ListPending 列出所有 assisted 待审建议。
func (e *Executor) ListPending(ctx context.Context) ([]data.SettingEntry, error) {
	return e.store.ListSettingsByPrefix(ctx, pendingPrefix)
}

// CountPending 返回 pending 数量。
func (e *Executor) CountPending(ctx context.Context) int {
	entries, _ := e.ListPending(ctx)
	return len(entries)
}
