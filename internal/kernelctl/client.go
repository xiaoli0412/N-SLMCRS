// Package kernelctl 提供 Go 主干调用 Rust 内核 sidecar（nslmcrs-kernel）的客户端。
//
// v0.11：策略决策类端点（/verdict、/weighted-score、/circuit-check）为无状态纯函数，
// 与 Go 侧实现数值对齐。sidecar 不可达或返回异常时方法返回 ok=false，调用方降级回
// 内置 Go 实现（无单点依赖）。
//
// 接线现状（v0.11）：
//   - /verdict 已接入 modelhealth.Sweeper.applyVerdict（慢路径，30min 扫描，无热路径回归）。
//   - /weighted-score、/circuit-check 的客户端方法已就绪并测试，但暂未接入 scheduler
//     热路径（二者均为逐 Key 调用 = N 次 RPC/请求，属热路径回归）；留待 v0.12 的
//     /reserve 端点批量调用（1 次 RPC/请求）时启用。
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
	baseURL string
	http    *http.Client
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
func NewFromEnv() *Client {
	if os.Getenv("KERNEL_DISABLE") == "1" {
		return nil
	}
	url := os.Getenv("KERNEL_URL")
	if url == "" {
		url = "http://127.0.0.1:8790"
	}
	return &Client{baseURL: url, http: &http.Client{Timeout: 1 * time.Second}}
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
