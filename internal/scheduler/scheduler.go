// Package scheduler 提供请求调度核心：N路并发先到先得、加权选Key、熔断器、降级。
//
// 调度流程：
//  1. 从 Store 获取所有可用上游 Key
//  2. 通过 HealthTracker 过滤熔断/冷却中的 Key
//  3. 按 RateLimitManager 检查余量，选出 N 个有余量的 Key
//  4. 按健康分加权随机排序
//  5. 并发发起 N 个上游请求
//  6. 非流式：首个成功返回即锁定，其余取消
//     流式：首个返回首块 content 的锁定，其余取消
//  7. 记录结果到 Store + HealthTracker + RateLimitManager
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nslmcrs/gateway/internal/data"
	"github.com/nslmcrs/gateway/internal/inflight"
	"github.com/nslmcrs/gateway/internal/ratelimit"
	"github.com/nslmcrs/gateway/internal/upstream"
)

// RuntimeOverrides 由 Auto-Pilot 注入的运行时策略覆盖（可逆调参）。
// 实现方为 internal/autopilot.Runtime；此处用接口避免循环依赖。
// 零值/nil 表示"不覆盖，使用调度器默认"。
type RuntimeOverrides interface {
	// Concurrency 覆盖的并发度（<=0 表示用配置默认）。
	Concurrency() int
	// WeightBoost 某密钥的权重乘子（<1 降权；返回 1.0 表示不变）。
	WeightBoost(keyID int64) float64
}

// Scheduler 请求调度器。
type Scheduler struct {
	store     *data.Store
	client    *upstream.Client
	rl        *ratelimit.Manager
	health    *ratelimit.HealthTracker
	config    SchedulerConfig
	mu        sync.RWMutex      // 保护 config 的运行时可变字段
	runtime   RuntimeOverrides  // 可选：Auto-Pilot 注入（nil=用默认）
	webhook   WebhookEmitter    // 可选：v0.10 事件回调（nil=不发射）
}

// WebhookEmitter 事件回调抽象（避免 scheduler 直接依赖 hooks 包）。
// 与 hooks.Webhook.EmitFields 签名对齐，使 *hooks.Webhook 直接满足本接口。
type WebhookEmitter interface {
	EmitFields(ctx context.Context, typ, traceID, model, keyMask, reason string, status, latency int)
}

// SetRuntime 注入 Auto-Pilot 运行时覆盖（nil=清除，恢复默认）。
func (s *Scheduler) SetRuntime(rt RuntimeOverrides) {
	s.runtime = rt
}

// SetWebhook 注入事件回调（nil=清除，不发射）。
func (s *Scheduler) SetWebhook(w WebhookEmitter) {
	s.webhook = w
}

// Config 返回当前调度配置的快照（线程安全）。
func (s *Scheduler) Config() SchedulerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// UpdateConfig 运行时更新部分调度配置（熔断/并发/超时），零值字段保持不变。
// 仅可变字段（DefaultConcurrency / MaxConcurrency / CircuitThreshold /
// CircuitCooldown / RequestTimeout）受锁保护；HealthWindow 不支持热改。
// 校验：DefaultConcurrency>0、MaxConcurrency>=DefaultConcurrency、
// CircuitThreshold>0、CircuitCooldown>0、RequestTimeout>0。
func (s *Scheduler) UpdateConfig(patch SchedulerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.config
	if patch.DefaultConcurrency > 0 {
		next.DefaultConcurrency = patch.DefaultConcurrency
	}
	if patch.MaxConcurrency > 0 {
		next.MaxConcurrency = patch.MaxConcurrency
	}
	if patch.CircuitThreshold > 0 {
		next.CircuitThreshold = patch.CircuitThreshold
	}
	if patch.CircuitCooldown > 0 {
		next.CircuitCooldown = patch.CircuitCooldown
	}
	if patch.RequestTimeout > 0 {
		next.RequestTimeout = patch.RequestTimeout
	}
	// 校验合并后的一致性
	if next.DefaultConcurrency <= 0 {
		return fmt.Errorf("DefaultConcurrency 必须 > 0")
	}
	if next.MaxConcurrency < next.DefaultConcurrency {
		return fmt.Errorf("MaxConcurrency 必须 >= DefaultConcurrency")
	}
	if next.CircuitThreshold <= 0 {
		return fmt.Errorf("CircuitThreshold 必须 > 0")
	}
	if next.CircuitCooldown <= 0 {
		return fmt.Errorf("CircuitCooldown 必须 > 0")
	}
	if next.RequestTimeout <= 0 {
		return fmt.Errorf("RequestTimeout 必须 > 0")
	}
	s.config = next
	return nil
}

// effectiveConcurrency 返回当前生效并发度：Runtime 覆盖优先于配置默认。
func (s *Scheduler) effectiveConcurrency() int {
	s.mu.RLock()
	maxC := s.config.MaxConcurrency
	defC := s.config.DefaultConcurrency
	s.mu.RUnlock()
	if s.runtime != nil {
		if n := s.runtime.Concurrency(); n > 0 {
			// 钳位到 MaxConcurrency
			if maxC > 0 && n > maxC {
				return maxC
			}
			return n
		}
	}
	return defC
}

// requestTimeout 返回当前请求超时（线程安全读取可变配置）。
func (s *Scheduler) requestTimeout() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.RequestTimeout
}

// circuitConfig 返回当前熔断配置快照（线程安全）。
func (s *Scheduler) circuitConfig() (threshold int, cooldown time.Duration) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.CircuitThreshold, s.config.CircuitCooldown
}

// SchedulerConfig 调度配置。
type SchedulerConfig struct {
	DefaultConcurrency int
	MaxConcurrency     int
	RequestTimeout     time.Duration
	CircuitThreshold   int
	CircuitCooldown    time.Duration
	HealthWindow       time.Duration // 健康统计窗口时长
}

// New 创建调度器。
func New(store *data.Store, client *upstream.Client, rl *ratelimit.Manager, health *ratelimit.HealthTracker, cfg SchedulerConfig) *Scheduler {
	if cfg.HealthWindow == 0 {
		cfg.HealthWindow = 2 * time.Minute
	}
	return &Scheduler{store: store, client: client, rl: rl, health: health, config: cfg}
}

// ScheduleResult 调度结果。
type ScheduleResult struct {
	StatusCode    int
	Body          []byte
	Header        http.Header
	TraceID       string
	LatencyMS     int
	UpstreamKey   string // 命中的 Key（脱敏）
	PromptTokens  int
	CompletionTokens int
	TotalTokens   int
	IsStream      bool
	// 流式模式下，由调用方负责读取
	StreamResp    *http.Response
	StreamCancel  context.CancelFunc
}

// Dispatch 调度一次请求（非流式，默认 Chat 能力）。
func (s *Scheduler) Dispatch(ctx context.Context, traceID, model string, requestBody []byte) (*ScheduleResult, error) {
	return s.DispatchCap(ctx, upstream.CapChat, traceID, model, requestBody)
}

// DispatchCap 按能力调度非流式请求（chat/embedding/rerank 共用 N 路先到先得逻辑）。
func (s *Scheduler) DispatchCap(ctx context.Context, cap upstream.Capability, traceID, model string, requestBody []byte) (*ScheduleResult, error) {
	start := time.Now()
	inflight.Inc() // 在途计数 +1，供 Auto-Pilot 档位感知
	defer inflight.Dec()
	keys, err := s.selectKeys(ctx, model)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("无可用上游密钥")
	}

	// 按能力选择上游路径（rerank 路径含模型，需动态拼接）
	path := capPath(cap, model)

	n := min(len(keys), s.effectiveConcurrency())
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout())
	defer cancel()

	// 上游调用闭包：按能力路由到正确端点
	callUpstream := func(ctx context.Context, key *data.UpstreamKey) (*upstream.Response, error) {
		return s.client.Request(ctx, cap, key.KeyValue, path, requestBody)
	}

	type attempt struct {
		result *upstream.Response
		err    error
		keyID  int64
		index  int
	}

	ch := make(chan attempt, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int, key *data.UpstreamKey) {
			defer wg.Done()
			resp, err := callUpstream(ctx, key)
			ch <- attempt{result: resp, err: err, keyID: key.ID, index: idx}
		}(i, keys[i])
	}

	// 等待第一个成功或全部完成
	var firstSuccess *attempt
	var allErrors []error
	recvCount := 0

	for recvCount < n {
		a := <-ch
		recvCount++

		if a.err != nil {
			allErrors = append(allErrors, a.err)
			s.recordResult(ctx, traceID, model, keys[a.index], "", "error", 0, a.err.Error(), n)
			continue
		}

		resp := a.result
		key := keys[a.index]

		// 更新限流校准
		if rem := resp.RateLimitRemaining(); rem >= 0 {
			s.rl.Calibrate(key.ID, rem)
		}

		if resp.IsSuccess() {
			firstSuccess = &a
			s.recordSuccess(ctx, traceID, model, key, resp, n, start)
			break
		}

		if resp.IsRateLimited() {
			s.recordResult(ctx, traceID, model, key, key.KeyMask, "rate_limited", resp.StatusCode, "429 Too Many Requests", n)
			s.health.Record(key.ID, false, s.config.HealthWindow)
			continue
		}

		// 其他错误（4xx/5xx）
		nvErr := resp.ParseNVIDIAError()
		s.recordResult(ctx, traceID, model, key, key.KeyMask, "error", resp.StatusCode, nvErr.Title, n)
		s.health.Record(key.ID, false, s.config.HealthWindow)
		// 记录实际上游错误，便于在全部失败时透出真实原因（如模型名错误、请求体格式错误）
		detail := nvErr.Detail
		if detail == "" {
			detail = nvErr.Title
		}
		allErrors = append(allErrors, fmt.Errorf("上游 %s HTTP %d: %s", key.KeyMask, resp.StatusCode, detail))
	}

	// 检查熔断
	for _, key := range keys {
		s.checkCircuitBreaker(ctx, key)
	}

	if firstSuccess != nil {
		latency := int(time.Since(start).Milliseconds())
		resp := firstSuccess.result
		tokens := extractTokens(resp.Body)
		return &ScheduleResult{
			StatusCode:    resp.StatusCode,
			Body:          resp.Body,
			Header:        resp.Header,
			TraceID:       traceID,
			LatencyMS:     latency,
			UpstreamKey:   keys[firstSuccess.index].KeyMask,
			PromptTokens:  tokens.Prompt,
			CompletionTokens: tokens.Completion,
			TotalTokens:   tokens.Total,
		}, nil
	}

	return nil, fmt.Errorf("所有上游密钥均失败: %v", allErrors)
}

// capPath 将能力映射到上游端点路径。
// rerank 使用 NVIDIA 检索域的模型专属路径：/retrieval/{owner}/{model}/reranking
// （其中 model 名中的 '.' 需替换为 '_'，以匹配 NVIDIA 路由约定）。
// embedding 与 chat 共用 OpenAI 兼容路径，走 integrate.api.nvidia.com。
func capPath(cap upstream.Capability, model string) string {
	switch cap {
	case upstream.CapEmbedding:
		return "/embeddings"
	case upstream.CapRerank:
		// NVIDIA 检索域路径：/v1/retrieval/{owner}/{model_underscore}/reranking
		// model 形如 "nvidia/llama-3.2-nemoretriever-500m-rerank-v2"
		// → 路径段 "nvidia/llama-3_2-nemoretriever-500m-rerank-v2"
		m := model
		m = strings.ReplaceAll(m, ".", "_")
		m = strings.TrimPrefix(m, "/")
		return "/retrieval/" + m + "/reranking"
	default: // CapChat / CapCompletions
		return "/chat/completions"
	}
}

// DispatchStream 调度一次流式请求。返回首个响应的 response body（调用方负责 SSE 读取）。
func (s *Scheduler) DispatchStream(ctx context.Context, traceID, model string, requestBody []byte) (*ScheduleResult, error) {
	start := time.Now()
	inflight.Inc() // 流式在途计数 +1；调用方读完 StreamCancel 时 -1
	keys, err := s.selectKeys(ctx, model)
	if err != nil {
		inflight.Dec()
		return nil, err
	}
	if len(keys) == 0 {
		inflight.Dec()
		return nil, fmt.Errorf("无可用上游密钥")
	}

	n := min(len(keys), s.effectiveConcurrency())
	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout())
	// 包裹 cancel：调用方关闭流时一并扣减在途计数。
	streamCancel := func() {
		cancel()
		inflight.Dec()
	}

	type streamAttempt struct {
		resp  *http.Response
		err   error
		keyID int64
		index int
	}

	ch := make(chan streamAttempt, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int, key *data.UpstreamKey) {
			defer wg.Done()
			resp, err := s.client.ChatCompletionStream(ctx, key.KeyValue, requestBody)
			ch <- streamAttempt{resp: resp, err: err, keyID: key.ID, index: idx}
		}(i, keys[i])
	}

	// 等待首个成功连接（即收到 HTTP 响应头）
	// 流式场景：先连接成功的先开始输出
	var firstConnected *streamAttempt
	var allErrors []error
	recvCount := 0

	for recvCount < n {
		a := <-ch
		recvCount++

		if a.err != nil {
			allErrors = append(allErrors, a.err)
			continue
		}

		if a.resp.StatusCode == 200 {
			firstConnected = &a
			break
		}

		// 429 或其他错误
		_ = a.resp.Body.Close()
		key := keys[a.index]
		if a.resp.StatusCode == 429 {
			s.recordResult(ctx, traceID, model, key, key.KeyMask, "rate_limited", 429, "429", n)
			s.health.Record(key.ID, false, s.config.HealthWindow)
		} else {
			s.recordResult(ctx, traceID, model, key, key.KeyMask, "error", a.resp.StatusCode, "", n)
			s.health.Record(key.ID, false, s.config.HealthWindow)
		}
	}

	if firstConnected != nil {
		key := keys[firstConnected.index]
		// 其余连接会被 context cancel 自动清理
		latency := int(time.Since(start).Milliseconds())
		s.markHealthy(ctx, key)
		s.recordResult(ctx, traceID, model, key, key.KeyMask, "success", 200, "", n)
		return &ScheduleResult{
			StatusCode:   200,
			TraceID:      traceID,
			LatencyMS:    latency,
			UpstreamKey:  key.KeyMask,
			IsStream:     true,
			StreamResp:   firstConnected.resp,
			StreamCancel: streamCancel, // 调用方读完关闭（内含在途 -1）
			}, nil
		}

	streamCancel()
	return nil, fmt.Errorf("所有上游密钥流式连接均失败: %v", allErrors)
}

// selectKeys 从 Store 获取可用 Key，过滤熔断/冷却/无余量的，按健康分加权随机排序。
//
// 熔断半开探测：circuit_open 且冷却到期（CoolingUntil<=now）的 Key 转为 half_open
// 重新放行——每轮仅放行 1 个半开试探，避免同时试探多个待恢复密钥；试探成功(recordSuccess)
// 闭合为 active，失败则由 checkCircuitBreaker 重新熔断。旧实现一旦 circuit_open 便永久跳过，
// CoolingUntil/指数退避形同虚设——此处补齐自动恢复。
func (s *Scheduler) selectKeys(ctx context.Context, model string) ([]*data.UpstreamKey, error) {
	allKeys, err := s.store.ListUpstreamKeys(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	var candidates []*data.UpstreamKey
	halfOpenAllowed := 1 // 每轮只放行 1 个半开试探
	for i := range allKeys {
		k := &allKeys[i]
		if !k.Enabled {
			continue
		}
		if k.Status == "circuit_open" {
			if k.CoolingUntil > now {
				continue // 仍在熔断冷却期
			}
			// 冷却到期 → 转半开，放行一个试探请求
			k.Status = "half_open"
			k.ConsecutiveFail = 0
			k.CoolingUntil = 0
			_ = s.store.UpdateUpstreamKeyStatus(ctx, k.ID, "half_open", 0, 0)
			s.health.ResetConsecutive(k.ID) // 清旧失败数，给试探干净起点
		}
		if k.Status == "half_open" {
			if halfOpenAllowed <= 0 {
				continue // 本轮已有半开试探，其余半开暂不放行
			}
			halfOpenAllowed--
		}
		// 限流余量检查
		if !s.rl.Allow(k.ID, 1) {
			continue
		}
		candidates = append(candidates, k)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// 按健康分加权随机排序
	s.weightedShuffle(candidates)
	return candidates, nil
}

// weightedShuffle 按健康分加权随机排列。健康分越高，越靠前。
func (s *Scheduler) weightedShuffle(keys []*data.UpstreamKey) {
	type scored struct {
		key  *data.UpstreamKey
		score float64
	}
	items := make([]scored, len(keys))
	totalWeight := 0.0
	for i, k := range keys {
		// 基础分 = 成功率，连续失败惩罚
		rate := s.health.SuccessRate(k.ID, s.config.HealthWindow)
		consec := s.health.ConsecutiveFailures(k.ID)
		penalty := math.Pow(0.5, float64(consec)) // 每次连续失败减半
		score := rate * penalty
		// Auto-Pilot 注入的权重乘子（降权/加权）
		if s.runtime != nil {
			score *= s.runtime.WeightBoost(k.ID)
		}
		// 最低 1 分保证有机会
		if score < 1 {
			score = 1
		}
		items[i] = scored{key: k, score: score}
		totalWeight += score
	}

	// 加权随机排列（蓄水池采样变体）
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	result := make([]*data.UpstreamKey, 0, len(keys))
	remaining := make([]scored, len(items))
	copy(remaining, items)

	for len(remaining) > 0 {
		// 计算剩余总权重
		remWeight := 0.0
		for _, item := range remaining {
			remWeight += item.score
		}
		// 随机选一个
		pick := rng.Float64() * remWeight
		accum := 0.0
		chosenIdx := 0
		for i, item := range remaining {
			accum += item.score
			if accum >= pick {
				chosenIdx = i
				break
			}
		}
		result = append(result, remaining[chosenIdx].key)
		remaining = append(remaining[:chosenIdx], remaining[chosenIdx+1:]...)
	}

	copy(keys, result)
}

// checkCircuitBreaker 检查是否需要触发熔断。
func (s *Scheduler) checkCircuitBreaker(ctx context.Context, key *data.UpstreamKey) {
	consec := s.health.ConsecutiveFailures(key.ID)
	threshold, baseCooldown := s.circuitConfig()
	if consec >= threshold {
		// 计算冷却时长（指数退避）
		cooldown := baseCooldown
		for i := 1; i < consec-threshold+1; i++ {
			cooldown *= 2
		}
		// 上限 10 分钟
		if cooldown > 10*time.Minute {
			cooldown = 10 * time.Minute
		}
		coolUntil := time.Now().Add(cooldown).Unix()
		_ = s.store.UpdateUpstreamKeyStatus(ctx, key.ID, "circuit_open", consec, coolUntil)
	}
}

// markHealthy 记录一次成功并闭合半开熔断（流式/非流式共用）。
// 旧实现在流式成功路径未调用 health.Record(true)，导致流式成功不改善健康度——一并修正。
func (s *Scheduler) markHealthy(ctx context.Context, key *data.UpstreamKey) {
	s.health.Record(key.ID, true, s.config.HealthWindow)
	// 半开试探成功 → 熔断闭合，转回 active
	if key.Status == "half_open" {
		key.Status = "active"
		_ = s.store.UpdateUpstreamKeyStatus(ctx, key.ID, "active", 0, 0)
	}
}

// recordSuccess 记录成功请求。
func (s *Scheduler) recordSuccess(ctx context.Context, traceID, model string, key *data.UpstreamKey, resp *upstream.Response, concurrency int, start time.Time) {
	latency := int(time.Since(start).Milliseconds())
	tokens := extractTokens(resp.Body)
	s.markHealthy(ctx, key)
	s.feedbackModelCircuit(ctx, model, true) // 被动路径：成功清零连续失败
	s.emitWebhook(ctx, "success", traceID, model, key.KeyMask, "", resp.StatusCode, latency)
	s.store.RecordRequest(ctx, data.RequestLog{
		TraceID:          traceID,
		DownstreamCred:   "",
		UpstreamKey:      key.KeyMask,
		Model:            model,
		Status:           "success",
		HTTPStatus:       resp.StatusCode,
		LatencyMS:        latency,
		PromptTokens:     tokens.Prompt,
		CompletionTokens: tokens.Completion,
		TotalTokens:      tokens.Total,
		Concurrency:      concurrency,
	})
	if rem := resp.RateLimitRemaining(); rem >= 0 {
		s.rl.Calibrate(key.ID, rem)
	}
}

// recordResult 记录请求结果（成功/失败）。
func (s *Scheduler) recordResult(ctx context.Context, traceID, model string, key *data.UpstreamKey, upstreamMask, status string, httpStatus int, errMsg string, concurrency int) {
	// 被动路径：成功清零，失败累加（达阈值转 open）
	s.feedbackModelCircuit(ctx, model, status == "success")
	// 事件回调：失败/限流时发射 webhook（成功由 recordSuccess 单独发射）
	if status != "success" {
		evType := "error"
		if status == "rate_limited" {
			evType = "rate_limited"
		}
		s.emitWebhook(ctx, evType, traceID, model, upstreamMask, errMsg, httpStatus, 0)
	}
	s.store.RecordRequest(ctx, data.RequestLog{
		TraceID:        traceID,
		UpstreamKey:   upstreamMask,
		Model:         model,
		Status:        status,
		HTTPStatus:    httpStatus,
		ErrorMessage:  errMsg,
		Concurrency:   concurrency,
	})
}

// emitWebhook 异步发射事件回调（webhook 未注入时无操作）。
func (s *Scheduler) emitWebhook(ctx context.Context, typ, traceID, model, keyMask, reason string, status, latency int) {
	if s.webhook == nil {
		return
	}
	s.webhook.EmitFields(ctx, typ, traceID, model, keyMask, reason, status, latency)
}

// feedbackModelCircuit 被动反馈模型熔断状态（与 modelhealth 主动扫描互补）。
// 成功清零连续失败并闭合临时熔断；失败累加，达阈值转 open（指数退避冷却）。
// 永久熔断不受被动反馈影响。复用按 Key 的熔断阈值/冷却配置。
func (s *Scheduler) feedbackModelCircuit(ctx context.Context, model string, success bool) {
	if model == "" {
		return
	}
	threshold, cooldown := s.circuitConfig()
	if success {
		_ = s.store.ResetModelCircuitConsecutive(ctx, model)
	} else {
		_ = s.store.RecordModelCircuitFailure(ctx, model, threshold, int64(cooldown.Seconds()))
	}
}

// tokenUsage 从响应中提取 token 用量。
type tokenUsage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion  int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
}

type chatResponse struct {
	Usage tokenUsage `json:"usage"`
}

func extractTokens(body []byte) tokenUsage {
	var r chatResponse
	if json.Unmarshal(body, &r) == nil {
		return r.Usage
	}
	return tokenUsage{}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 确保 io 用于 SSE
var _ = io.EOF
