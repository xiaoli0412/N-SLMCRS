// Package autopilot 实现 AI 动态调度（Auto-Pilot）。
//
// 设计：三层可切换的决策引擎 + 三种执行模式叠加在既有调度核心之上，
// 不重写 selectKeys/熔断/令牌桶，仅做"策略注入"。
//
//	Controller (30s 周期)
//	  └─ activeEngine.Decide(snapshot) → []Action
//	       ├─ Adaptive  (PID + EWMA)
//	       ├─ Forecast  (Holt-Winters)
//	       └─ LLM       (stub→真 LLM)
//	  └─ Executor.apply(actions, mode)
//	       manual   → 仅记录(observe)
//	       assisted → 写 pending 待人工确认
//	       fullauto → 直接执行（调参/启停/熔断/吊销）
package autopilot

import (
	"context"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
)

// Mode 运行模式。
type Mode string

const (
	ModeManual   Mode = "manual"   // 仅观察，不执行
	ModeAssisted Mode = "assisted" // 建议写 pending，等人工确认
	ModeFullAuto Mode = "fullauto" // 全权接管
)

// EngineID 引擎标识。
type EngineID string

const (
	EngineAdaptive EngineID = "adaptive" // PID · EWMA
	EngineForecast EngineID = "predict" // Holt-Winters
	EngineLLM      EngineID = "llm"     // LLM 决策（stub→真）
)

// KeySnap 单个上游密钥的只读快照（喂给引擎决策）。
type KeySnap struct {
	ID           int64
	Mask         string
	Enabled      bool
	Status       string  // active|cooling|circuit_open|disabled
	SuccessRate  float64 // 0..1
	ConsecFail   int
	RPMRemaining int // 令牌桶剩余（-1 表示未知）
}

// Snapshot 引擎决策所需的只读现状。
type Snapshot struct {
	Keys              []KeySnap
	Metrics           data.Metrics           // 近 1h 聚合
	Series            []data.TimeSeriesPoint // 近 24h 每分钟一桶（Holt-Winters 用）
	CurrentConcurrency int
	MaxConcurrency    int
	DefaultConcurrency int
}

// ActionKind 动作类型。
type ActionKind string

const (
	ActSetConcurrency  ActionKind = "set_concurrency"  // Value = 目标并发度
	ActSetWeightBoost  ActionKind = "set_weight_boost" // Value = 权重乘子（0..1 降权，>1 加权）
	ActDisableKey      ActionKind = "disable_key"      // 启用=0
	ActOpenCircuit     ActionKind = "open_circuit"     // Value = 冷却秒数
	ActRevokeCredential ActionKind = "revoke_credential"
)

// Action 单条调度建议/动作。
type Action struct {
	Kind       ActionKind `json:"Kind"`
	KeyID      int64      `json:"KeyID,omitempty"`
	CredID     int64      `json:"CredID,omitempty"`
	Value      float64    `json:"Value,omitempty"`
	Reason     string     `json:"Reason"`      // 人类可读根因（审计用）
	Confidence float64    `json:"Confidence"`  // 0..1
	Source     EngineID   `json:"Source"`
}

// Engine 决策引擎接口。实现必须线程安全（状态保存在结构体内）。
type Engine interface {
	ID() EngineID
	// Decide 依据快照给出 0..N 条建议动作。无历史数据时应返回空切片（不误动）。
	Decide(ctx context.Context, snap Snapshot) ([]Action, error)
}

// EventRecord 单条审计事件（写 app_logs，前端"最近事件"展示）。
type EventRecord struct {
	TS        int64   `json:"TS"`
	Engine    EngineID `json:"Engine"`
	Mode      Mode    `json:"Mode"`
	Kind      ActionKind `json:"Kind"`
	Detail    string  `json:"Detail"`
	Reason    string  `json:"Reason"`
	Confidence float64 `json:"Confidence"`
	Applied   bool    `json:"Applied"` // true=已执行，false=仅记录/pending
}

// State 完整状态（GET /api/admin/autopilot/state 返回）。
type State struct {
	Mode            Mode          `json:"Mode"`
	Engine          EngineID      `json:"Engine"`
	RuntimeConcurrency int         `json:"RuntimeConcurrency"` // 0=用默认
	DefaultConcurrency int        `json:"DefaultConcurrency"`
	MaxConcurrency  int           `json:"MaxConcurrency"`
	DecisionsPerMin int           `json:"DecisionsPerMin"` // 决策频率（统计近 60s）
	Interventions   int           `json:"Interventions"`   // 累计执行动作数
	PendingCount    int           `json:"PendingCount"`    // assisted 待审建议数
	RecentEvents    []EventRecord `json:"RecentEvents"`
}

// Sanitize 返回快照中某密钥的剩余 RPM 估值（未知返回 -1）。
func rpmOf(k KeySnap) int { return k.RPMRemaining }

// sinceSeconds 把"距今多少秒"转为截止时间戳。
func sinceSeconds(d time.Duration) int64 { return time.Now().Add(-d).Unix() }
