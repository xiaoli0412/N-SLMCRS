// Package ratelimit 提供每 Key 令牌桶与滑动窗口限流。
//
// 采用令牌桶算法（业界验证：AWS API Gateway / Solo.io / Portkey 同款）：
// 每个 Key 一个桶，容量 = RPM，按 RPM/60 每秒回填，允许短突发但平均不超过限额。
// 上游返回的 X-RateLimit-Remaining 头用于校准真实余量，减少保守浪费。
package ratelimit

import (
	"sync"
	"time"
)

// TokenBucket 单 Key 的令牌桶。
type TokenBucket struct {
	mu sync.Mutex

	capacity   float64       // 桶容量（= RPM 上限）
	tokens     float64       // 当前令牌数
	refillRate float64       // 每秒回填速率（RPM/60）
	lastRefill time.Time     // 上次回填时间
}

// NewTokenBucket 创建令牌桶。rpm 为每分钟请求数上限。
func NewTokenBucket(rpm int) *TokenBucket {
	return &TokenBucket{
		capacity:   float64(rpm),
		tokens:     float64(rpm), // 初始满桶，允许启动突发
		refillRate: float64(rpm) / 60.0,
		lastRefill: time.Now(),
	}
}

// Allow 尝试消费 n 个令牌。成功返回 true。
func (b *TokenBucket) Allow(n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens >= float64(n) {
		b.tokens -= float64(n)
		return true
	}
	return false
}

// AllowOne 消费 1 个令牌的快捷方法。
func (b *TokenBucket) AllowOne() bool { return b.Allow(1) }

// Available 返回当前可用令牌数（触发回填后）。
func (b *TokenBucket) Available() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return b.tokens
}

// Calibrate 用上游 X-RateLimit-Remaining 校准真实余量。
// 上游最清楚自己的计数，比本地估算更准确，能减少保守浪费。
func (b *TokenBucket) Calibrate(remaining int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if float64(remaining) < b.tokens {
		b.tokens = float64(remaining) // 上游更紧，听上游的
	}
}

// refill 按时间差回填令牌（调用方需持锁）。
func (b *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.lastRefill = now
}

// Manager 管理所有 Key 的令牌桶。
type Manager struct {
	mu       sync.RWMutex
	buckets  map[int64]*TokenBucket // keyID → bucket
	defaultRPM int
}

// NewManager 创建限流管理器。defaultRPM 为新 Key 的默认 RPM。
func NewManager(defaultRPM int) *Manager {
	return &Manager{
		buckets:    make(map[int64]*TokenBucket),
		defaultRPM: defaultRPM,
	}
}

// Register 注册/更新一个 Key 的桶。rpm<=0 时用默认值。
func (m *Manager) Register(keyID int64, rpm int) {
	if rpm <= 0 {
		rpm = m.defaultRPM
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buckets[keyID] = NewTokenBucket(rpm)
}

// Unregister 移除一个 Key 的桶。
func (m *Manager) Unregister(keyID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.buckets, keyID)
}

// bucket 取桶（不存在则按默认值懒创建）。
func (m *Manager) bucket(keyID int64) *TokenBucket {
	m.mu.RLock()
	b, ok := m.buckets[keyID]
	m.mu.RUnlock()
	if ok {
		return b
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// 双检
	if b, ok := m.buckets[keyID]; ok {
		return b
	}
	b = NewTokenBucket(m.defaultRPM)
	m.buckets[keyID] = b
	return b
}

// Allow 检查 keyID 是否允许消费 n 个令牌。
func (m *Manager) Allow(keyID int64, n int) bool {
	return m.bucket(keyID).Allow(n)
}

// AllowKeys 从候选 keyIDs 中选出当前有余量的（用于调度层选 N 路并发）。
func (m *Manager) AllowKeys(keyIDs []int64, n int) []int64 {
	var allowed []int64
	for _, id := range keyIDs {
		if m.Allow(id, 1) {
			allowed = append(allowed, id)
			if len(allowed) >= n {
				break
			}
		}
	}
	return allowed
}

// Calibrate 校准指定 Key 的桶。
func (m *Manager) Calibrate(keyID int64, remaining int) {
	if b := m.bucket(keyID); b != nil {
		b.Calibrate(remaining)
	}
}

// Snapshot 返回所有 Key 的可用令牌数（面板展示用）。
func (m *Manager) Snapshot() map[int64]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[int64]float64, len(m.buckets))
	for id, b := range m.buckets {
		out[id] = b.Available()
	}
	return out
}
