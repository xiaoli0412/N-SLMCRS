// Package kernelctl 提供 Go 主干调用 Rust 内核 sidecar（nslmcrs-kernel）的客户端。
//
// v0.11：策略决策类端点（/verdict、/weighted-score、/circuit-check）为无状态纯函数，
// 与 Go 侧实现数值对齐。sidecar 不可达或返回异常时方法返回 ok=false，调用方降级回
// 内置 Go 实现（无单点依赖）。
// v0.12：全量 Rust 控制面权威化——新增 /reserve（准入批量选 Key）、/report（反馈
// 批量更新状态）。热路径强依赖 kernel：KERNEL_FAIL_CLOSED=1 时 /reserve 不可达即
// 拒绝准入（fail-closed）；=0 时降级回 Go（fail-open，与 v0.11 一致）。
//
// 接线现状（v0.12）：
//   - /verdict 已接入 modelhealth.Sweeper.applyVerdict（慢路径，30min 扫描）。
//   - /reserve、/report 接入 scheduler 热路径（selectKeys / 循环后反馈）。
//   - /weighted-score、/circuit-check 为 v0.11 遗留单 Key 端点，已被 /reserve 批量化
//     取代，保留客户端方法供降级路径与测试复用。
package kernelctl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

// Client 调用 Rust sidecar 做策略决策计算。
type Client struct {
	baseURL     string
	http        *http.Client
	failClosed bool // KERNEL_FAIL_CLOSED=1：/reserve 不可达即拒绝准入（硬依赖）
}

// FailClosed 是否启用 fail-closed（热路径强依赖 kernel）。
func (c *Client) FailClosed() bool {
	if c == nil {
		return false
	}
	return c.failClosed
}

// VerdictResult 模型健康判定结果（与 kernel-rs VerdictResp 契约对齐）。
type VerdictResult struct {
	State         string `json:"state"`
	OpenUntil     int64  `json:"open_until"`
	BadSweepCount int    `json:"bad_sweep_count"`
	Permanent     bool   `json:"permanent"`
}

// CircuitCheckResult 按 Key 熔断检查结果（与 kernel-rs CircuitCheckResp 契约对齐）。
type CircuitCheckResult struct {
	ShouldOpen  bool  `json:"should_open"`
	CooldownSec int64 `json:"cooldown_sec"`
	CoolUntil   int64 `json:"cool_until"`
}

// NewFromEnv 按 KERNEL_URL 创建客户端；KERNEL_DISABLE=1 或未配置时返回 nil（走纯 Go）。
// 超时 1s：决策类调用在 localhost <1ms；仅在 sidecar 挂起时快速失败降级。
// KERNEL_FAIL_CLOSED=1 标记热路径硬依赖（/reserve 不可达即拒绝准入）。
func NewFromEnv() *Client {
	if os.Getenv("KERNEL_DISABLE") == "1" {
		return nil
	}
	url := os.Getenv("KERNEL_URL")
	if url == "" {
		url = "http://127.0.0.1:8790"
	}
	return &Client{
		baseURL:     url,
		http:        &http.Client{Timeout: 1 * time.Second},
		failClosed: os.Getenv("KERNEL_FAIL_CLOSED") == "1",
	}
}

// post 通用 POST JSON；返回 ok=false 表示应降级。
func (c *Client) post(ctx context.Context, path string, req, resp any) bool {
	if c == nil {
		return false
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return false
	}
	httpReq.Header.Set("Content-Type", "application/json")
	r, err := c.http.Do(httpReq)
	if err != nil {
		return false
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(r.Body).Decode(resp) == nil
}

// Verdict 模型健康扫描判定（closed/open/permanent 三态机）。失败返回 ok=false。
func (c *Client) Verdict(ctx context.Context, successRate int, currentState string, badSweep int,
	floor, threshold, badToPerm int, cooldownBaseSec int64) (VerdictResult, bool) {
	var resp VerdictResult
	req := struct {
		SuccessRate      int    `json:"success_rate"`
		CurrentState     string `json:"current_state"`
		BadSweepCount    int    `json:"bad_sweep_count"`
		Floor            int    `json:"floor"`
		Threshold        int    `json:"threshold"`
		BadToPerm        int    `json:"bad_to_perm"`
		CooldownBaseSec  int64  `json:"cooldown_base_sec"`
	}{
		SuccessRate: successRate, CurrentState: currentState, BadSweepCount: badSweep,
		Floor: floor, Threshold: threshold, BadToPerm: badToPerm, CooldownBaseSec: cooldownBaseSec,
	}
	if !c.post(ctx, "/verdict", req, &resp) {
		return VerdictResult{}, false
	}
	return resp, true
}

// WeightedScore 调度加权评分（v0.12 /reserve 批量启用；v0.11 暂未接入热路径）。
func (c *Client) WeightedScore(ctx context.Context, successRate float64, consec int, boost float64) (float64, bool) {
	var resp struct {
		Score float64 `json:"score"`
	}
	req := struct {
		SuccessRate    float64 `json:"success_rate"`
		ConsecutiveFail int    `json:"consecutive_fail"`
		WeightBoost    float64 `json:"weight_boost"`
	}{SuccessRate: successRate, ConsecutiveFail: consec, WeightBoost: boost}
	if !c.post(ctx, "/weighted-score", req, &resp) {
		return 0, false
	}
	return resp.Score, true
}

// CircuitCheck 按 Key 熔断阈值检查（v0.12 /reserve 批量启用；v0.11 暂未接入热路径）。
func (c *Client) CircuitCheck(ctx context.Context, consec, threshold int, baseCooldownSec int64) (CircuitCheckResult, bool) {
	var resp CircuitCheckResult
	req := struct {
		ConsecutiveFail  int   `json:"consecutive_fail"`
		Threshold        int   `json:"threshold"`
		BaseCooldownSec  int64 `json:"base_cooldown_sec"`
		}{ConsecutiveFail: consec, Threshold: threshold, BaseCooldownSec: baseCooldownSec}
	if !c.post(ctx, "/circuit-check", req, &resp) {
		return CircuitCheckResult{}, false
	}
	return resp, true
}

// ─── v0.12 全量 Rust 控制面：/reserve（准入）、/report（反馈）──────────────

// Candidate /reserve 候选 Key（不接触密钥明文，仅 key_id + rpm + 权重乘子）。
type Candidate struct {
	KeyID       int64   `json:"key_id"`
	RPM         int     `json:"rpm"`
	WeightBoost float64 `json:"weight_boost"`
}

// ReserveReq /reserve 入参（与 kernel-rs ReserveReq 契约对齐）。
type ReserveReq struct {
	TraceID                string      `json:"trace_id"`
	Model                  string      `json:"model"`
	Concurrency            int         `json:"concurrency"`
	CircuitThreshold       int         `json:"circuit_threshold"`
	CircuitCooldownBaseSec int64       `json:"circuit_cooldown_base_sec"`
	HealthWindowSec        int64       `json:"health_window_sec"`
	Candidates             []Candidate `json:"candidates"`
}

// KeyBreakerChange 熔断器状态变更（供 Go echo 回写 upstream_keys）。
type KeyBreakerChange struct {
	KeyID           int64  `json:"key_id"`
	Status          string `json:"status"`
	ConsecutiveFail int    `json:"consecutive_fail"`
	CoolingUntil    int64  `json:"cooling_until"`
}

// ReserveResp /reserve 响应（与 kernel-rs ReserveResp 契约对齐）。
type ReserveResp struct {
	TraceID             string              `json:"trace_id"`
	Reserved            []int64             `json:"reserved"` // 有序 key_id（已消费令牌）
	KeyBreakerChanges   []KeyBreakerChange  `json:"key_breaker_changes"`
}

// Reserve 批量准入：选 Key + 令牌消费 + 加权随机排序（1 次 RPC/请求）。
// ok=false 表示 sidecar 不可达，调用方按 FailClosed() 决定降级或拒绝。
func (c *Client) Reserve(ctx context.Context, req ReserveReq) (ReserveResp, bool) {
	var resp ReserveResp
	if !c.post(ctx, "/reserve", req, &resp) {
		return ReserveResp{}, false
	}
	return resp, true
}

// ReportItem /report 单 Key 结果。
type ReportItem struct {
	KeyID               int64  `json:"key_id"`
	Success             bool   `json:"success"`
	Status              string `json:"status"` // success | error | rate_limited
	RateLimitRemaining  *int64 `json:"rate_limit_remaining,omitempty"`
}

// ReportReq /report 入参（与 kernel-rs ReportReq 契约对齐）。
type ReportReq struct {
	TraceID                string        `json:"trace_id"`
	CircuitThreshold       int           `json:"circuit_threshold"`
	CircuitCooldownBaseSec int64         `json:"circuit_cooldown_base_sec"`
	HealthWindowSec        int64         `json:"health_window_sec"`
	Results                []ReportItem  `json:"results"`
}

// ReportResp /report 响应（与 kernel-rs ReportResp 契约对齐）。
type ReportResp struct {
	KeyBreakerChanges []KeyBreakerChange `json:"key_breaker_changes"`
}

// Report 批量反馈：更新健康窗/熔断器/令牌校准（1 次 RPC/请求）。
// ok=false 表示 sidecar 不可达；反馈丢失非致命（状态由后续 /report 重收敛），
// 故 /report 失败始终 best-effort 降级，不受 FailClosed 影响。
func (c *Client) Report(ctx context.Context, req ReportReq) (ReportResp, bool) {
	var resp ReportResp
	if !c.post(ctx, "/report", req, &resp) {
		return ReportResp{}, false
	}
	return resp, true
}
