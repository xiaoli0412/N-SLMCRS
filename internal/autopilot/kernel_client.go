package autopilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// kernelClient 调用 Rust sidecar（nslmcrs-kernel）做数值密集计算。
// sidecar 不可达时调用方降级回内置 Go 实现（无单点依赖）。
type kernelClient struct {
	baseURL string
	http    *http.Client
}

// newKernelClient 按 KERNEL_URL 环境变量创建客户端；未配置返回 nil（走纯 Go）。
func newKernelClient() *kernelClient {
	url := os.Getenv("KERNEL_URL")
	if url == "" {
		url = "http://127.0.0.1:8790"
	}
	// 仅当显式禁用时返回 nil
	if os.Getenv("KERNEL_DISABLE") == "1" {
		return nil
	}
	return &kernelClient{
		baseURL: url,
		http:    &http.Client{Timeout: 3 * time.Second},
	}
}

type kernelForecastReq struct {
	Counts []float64 `json:"counts"`
}
type kernelForecastResp struct {
	ForecastNext float64 `json:"forecast_next"`
	Level        float64 `json:"level"`
	Trend        float64 `json:"trend"`
}

// forecast 调用 sidecar /forecast；失败返回 (0,false) 让调用方降级。
func (k *kernelClient) forecast(ctx context.Context, counts []float64) (forecastNext float64, ok bool) {
	if k == nil {
		return 0, false
	}
	body, _ := json.Marshal(kernelForecastReq{Counts: counts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.baseURL+"/forecast", bytes.NewReader(body))
	if err != nil {
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.http.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var r kernelForecastResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return 0, false
	}
	return r.ForecastNext, true
}

type kernelAvailabilityReq struct {
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	Total        int64   `json:"total"`
}
type kernelAvailabilityResp struct {
	Score float64 `json:"score"`
}

// availability 调用 sidecar /availability；失败返回 (0,false)。
func (k *kernelClient) availability(ctx context.Context, successRate float64, avgLatencyMS float64, total int64) (score float64, ok bool) {
	if k == nil {
		return 0, false
	}
	body, _ := json.Marshal(kernelAvailabilityReq{SuccessRate: successRate, AvgLatencyMS: avgLatencyMS, Total: total})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.baseURL+"/availability", bytes.NewReader(body))
	if err != nil {
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := k.http.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var r kernelAvailabilityResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return 0, false
	}
	return r.Score, true
}

// 防止 fmt 未使用告警（保留给未来调试日志）
var _ = fmt.Sprintf
