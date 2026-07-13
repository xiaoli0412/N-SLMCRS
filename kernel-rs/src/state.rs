//! 权威控制面状态（v0.12）。
//!
//! 打破 v0.11 的「无状态纯函数」设计：令牌桶、滑动健康窗、按 Key 熔断器
//! 由本服务持有为权威状态（内存），熔断器态持久化到 store（重启载入）。
//! 桶/窗按现有 Go 设计为内存态，重启即空（sliding.go:11「重启后重新统计即可」），
//! 熔断器态（status/cooling_until）持久化，重启不丢——避免重启后误放行已熔断 Key。
//!
//! 数值与 Go `ratelimit/tokenbucket.go`、`ratelimit/sliding.go`、
//! `internal/data/keys.go`、`internal/data/model_circuit.go` 对齐，确保降级透明。

use rusqlite::Connection;
use std::collections::HashMap;
use std::sync::atomic::AtomicU64;
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, Instant};

use crate::compute::Engine;
use crate::strategy::{self, Strategy};

// ─── 令牌桶（复刻 ratelimit.TokenBucket）──────────────────────────────

/// 单 Key 令牌桶：容量=RPM，按 RPM/60 每秒回填，允许短突发但平均不超限额。
/// 上游 X-RateLimit-Remaining 用于校准真实余量（仅当上游更紧时收紧）。
pub struct TokenBucket {
    capacity: f64,
    tokens: f64,
    refill_rate: f64, // 每秒回填
    last_refill: Instant,
}

impl TokenBucket {
    pub fn new(rpm: i64) -> Self {
        let cap = rpm.max(1) as f64;
        Self {
            capacity: cap,
            tokens: cap, // 初始满桶，允许启动突发
            refill_rate: cap / 60.0,
            last_refill: Instant::now(),
        }
    }

    fn refill(&mut self) {
        let now = Instant::now();
        let elapsed = now.duration_since(self.last_refill).as_secs_f64();
        if elapsed <= 0.0 {
            return;
        }
        self.tokens += elapsed * self.refill_rate;
        if self.tokens > self.capacity {
            self.tokens = self.capacity;
        }
        self.last_refill = now;
    }

    /// 尝试消费 n 个令牌。成功返回 true。
    pub fn allow(&mut self, n: i64) -> bool {
        self.refill();
        if self.tokens >= n as f64 {
            self.tokens -= n as f64;
            return true;
        }
        false
    }

    /// 检查是否有 n 个令牌但**不消费**（准入筛选用）。
    /// v0.13 (B2)：/reserve 先 peek 筛出有余量的候选，再仅对最终选中的 N 个消费，
    /// 避免旧实现对全部候选 allow(1) 消费、却只取 N 个、白耗 (候选数−N) 个官方配额。
    #[allow(dead_code)]
    pub fn has_tokens(&mut self, n: i64) -> bool {
        self.refill();
        self.tokens >= n as f64
    }

    /// v0.14 策略：带 RPM 头寸的准入检查。headroom∈(0,1]：
    ///   - headroom=1.0 → 地板 0 → tokens≥1（骑满容量，可消费到空）；
    ///   - headroom=0.8 → 地板=capacity×20%，须消费后仍 ≥ 地板，即 tokens ≥ 地板+1。
    ///
    /// 内化策略头寸：切到 Guardian(0.8) 后即刻收紧准入，无需重建桶。
    /// 用「地板+1」而非「> 地板」，给 refill 留 1 token 硬边界，避免边界被毫秒级
    /// 回填越过导致准入抖动。
    pub fn has_admission(&mut self, headroom: f64) -> bool {
        self.refill();
        let floor = self.capacity * (1.0 - headroom); // 保留地板
        self.tokens >= floor + 1.0
    }

    /// 用上游 X-RateLimit-Remaining 校准（仅当上游更紧时收紧）。
    pub fn calibrate(&mut self, remaining: i64) {
        self.refill();
        let r = remaining as f64;
        if r < self.tokens {
            self.tokens = r;
        }
    }
}

// ─── 滑动健康窗（复刻 ratelimit.SlidingWindow + 连续失败计数）─────────

/// 单 Key 滑动窗口：维护时间窗内的请求结果序列，实时算成功率。
/// 内存型，不做持久化（重启后重新统计即可，足够熔断决策）。
pub struct SlidingWindow {
    window: Duration,
    entries: Vec<(Instant, bool)>,
}

impl SlidingWindow {
    pub fn new(window_sec: i64) -> Self {
        Self {
            window: Duration::from_secs(window_sec.max(1) as u64),
            entries: Vec::new(),
        }
    }

    fn evict(&mut self) {
        let cutoff = Instant::now() - self.window;
        let mut i = 0;
        while i < self.entries.len() && self.entries[i].0 < cutoff {
            i += 1;
        }
        if i > 0 {
            self.entries.drain(0..i);
        }
    }

    /// 记录一次请求结果。
    pub fn record(&mut self, success: bool) {
        self.entries.push((Instant::now(), success));
        self.evict();
    }

    /// 当前窗口成功率（0..100）。无流量返回 0（与 Go Stats 一致）。
    pub fn success_rate(&mut self) -> f64 {
        self.evict();
        if self.entries.is_empty() {
            return 0.0;
        }
        let ok = self.entries.iter().filter(|(_, s)| *s).count();
        100.0 * ok as f64 / self.entries.len() as f64
    }
}

// ─── 按 Key 熔断器（复刻 upstream_keys 的 status/consecutive_fail/cooling_until）─

/// 按 Key 熔断器状态。consecutive_fail 既是实时决策计数器，也是持久化字段
/// （统一 Go HealthTracker.consecutiveFail 与 upstream_keys.consecutive_fail）。
/// status: active | half_open | circuit_open（disabled 由 Go 侧过滤，不进本服务）。
#[derive(Clone)]
pub struct KeyBreaker {
    pub status: String,
    pub consecutive_fail: i64,
    pub cooling_until: i64, // Unix 秒；circuit_open 时有效
}

impl Default for KeyBreaker {
    fn default() -> Self {
        Self {
            status: "active".into(),
            consecutive_fail: 0,
            cooling_until: 0,
        }
    }
}

// ─── AppState ─────────────────────────────────────────────────────────

/// 全局权威状态，注入 axum State（Arc 包裹，handler 共享）。
pub struct AppState {
    pub engine: Engine,
    /// key_id → 令牌桶（内存，重启即空，懒建）
    pub buckets: RwLock<HashMap<i64, TokenBucket>>,
    /// key_id → 滑动健康窗（内存，重启即空，懒建）
    pub windows: RwLock<HashMap<i64, SlidingWindow>>,
    /// key_id → 熔断器（内存镜像，启动从 DB 载入；写时同步落库）
    pub key_brk: RwLock<HashMap<i64, KeyBreaker>>,
    /// /data/kernel.db 连接（仅写；读走内存镜像）。Connection 非 Sync，用 Mutex 包裹。
    pub store: Mutex<Connection>,
    /// v0.14 活跃策略（/reserve 按其选择算法/扇出/头寸派发）。
    pub active: RwLock<Strategy>,
    /// RoundRobin 轮转指针（每轮 reserve 后推进，均匀分发）。
    pub rr_counter: AtomicU64,
}

impl AppState {
    #[allow(dead_code)]
    pub fn new(engine: Engine, conn: Connection) -> Self {
        Self {
            engine,
            buckets: RwLock::new(HashMap::new()),
            windows: RwLock::new(HashMap::new()),
            key_brk: RwLock::new(HashMap::new()),
            store: Mutex::new(conn),
            active: RwLock::new(*strategy::default_strategy()),
            rr_counter: AtomicU64::new(0),
        }
    }

    /// 取活跃策略（Copy 出，无锁风险）。
    pub fn active_strategy(&self) -> Strategy {
        *self.active.read().unwrap()
    }

    /// 设活跃策略（PUT /strategy 调用）。
    pub fn set_strategy(&self, s: Strategy) {
        *self.active.write().unwrap() = s;
    }
}

pub type SharedState = Arc<AppState>;
