//! nslmcrs-kernel — Rust 内核 sidecar。
//!
//! 把数值计算与策略决策密集的逻辑从 Go 抽出，作为独立微服务：
//!   - POST /forecast      ：Holt-Winters 三次指数平滑预测下一桶 RPM（v0.7）
//!   - POST /availability  ：按请求样本聚合可用度评分（成功率 65% + 延迟 35%）（v0.7）
//!   - POST /verdict       ：模型健康扫描判定（closed/open/permanent 三态机）（v0.11）
//!   - POST /weighted-score：调度加权评分（成功率 × 连续失败惩罚 × boost）（v0.11）
//!   - POST /circuit-check ：按 Key 熔断阈值检查（指数退避冷却，封顶 10min）（v0.11）
//!
//! Go 主干经 HTTP/JSON 调用本服务；不可达时降级回内置 Go 实现（无单点依赖）。
//! 决策类端点（v0.11）为无状态纯函数，与 Go 侧实现数值对齐，确保降级透明。

use axum::{
    extract::State, http::StatusCode, routing::{get, post}, Json, Router,
};
use serde::{Deserialize, Serialize};
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

/// Holt-Winters 引擎参数（与 Go forecast.go 对齐，确保降级一致）。
struct Engine {
    alpha: f64,
    beta: f64,
    gamma: f64,
    season_len: usize,
}

impl Engine {
    fn new() -> Self {
        Self { alpha: 0.3, beta: 0.1, gamma: 0.2, season_len: 60 }
    }

    /// 三次指数平滑（加法模型）拟合，返回 (level, trend, season_offset_0)。
    fn fit(&self, data: &[f64]) -> Option<(f64, f64, f64)> {
        let n = data.len();
        if n < self.season_len * 2 {
            // 数据不足两季：退化为单次平滑，season=0
            if n == 0 {
                return None;
            }
            let level = data.iter().sum::<f64>() / n as f64;
            let trend = if n > 1 { data[n - 1] - data[0] / (n - 1) as f64 } else { 0.0 };
            return Some((level, trend, 0.0));
        }
        // 初始 level/trend/season（经典分解初始化）
        let mut season = vec![0.0; self.season_len];
        let seasons = n / self.season_len;
        for i in 0..self.season_len {
            let mut s = 0.0;
            let mut cnt = 0;
            for k in 0..seasons {
                let idx = i + k * self.season_len;
                if idx < n {
                    s += data[idx];
                    cnt += 1;
                }
            }
            if cnt > 0 {
                season[i] = s / cnt as f64;
            }
        }
        let level0 = data[0];
        let trend0 = (data[self.season_len] - data[0]) / self.season_len as f64;
        let mut level = level0;
        let mut trend = trend0;
        for i in 0..n {
            let s = season[i % self.season_len];
            let prev_level = level;
            level = self.alpha * (data[i] - s) + (1.0 - self.alpha) * (level + trend);
            trend = self.beta * (level - prev_level) + (1.0 - self.beta) * trend;
            season[i % self.season_len] =
                self.gamma * (data[i] - level) + (1.0 - self.gamma) * s;
        }
        Some((level, trend, season[0]))
    }

    /// 预测下一桶值：level + trend + season[0]（加法模型）。
    fn forecast(&self, data: &[f64]) -> f64 {
        match self.fit(data) {
            Some((level, trend, s0)) => (level + trend + s0).max(0.0),
            None => 0.0,
        }
    }
}

// ─── 请求/响应 DTO（与 Go 客户端契约对齐）──────────────────────────────

#[derive(Deserialize)]
struct ForecastReq {
    /// 时序计数序列（按桶，如每分钟请求数）
    counts: Vec<f64>,
}

#[derive(Serialize)]
struct ForecastResp {
    forecast_next: f64,
    level: f64,
    trend: f64,
}

#[derive(Deserialize)]
struct AvailabilityReq {
    success_rate: f64, // 0..100
    avg_latency_ms: f64,
    total: i64,
}

#[derive(Serialize)]
struct AvailabilityResp {
    score: f64, // 0..100
}

// v0.11：策略决策端点 DTO ──────────────────────────────────────────────

/// 模型健康扫描判定入参（复刻 modelhealth.applyVerdict + nextCooldown）。
#[derive(Deserialize)]
struct VerdictReq {
    success_rate: i64,        // 0..100
    current_state: String,    // closed|open|half_open|permanent
    bad_sweep_count: i64,     // 当前连续坏扫描数
    floor: i64,               // SuccessRateFloor
    threshold: i64,           // SuccessRateThreshold
    bad_to_perm: i64,         // BadSweepToPermanent
    cooldown_base_sec: i64,   // CooldownBase 秒
}

#[derive(Serialize)]
struct VerdictResp {
    state: String,            // 新状态
    open_until: i64,          // 冷却到期 Unix 秒（open 时有效）
    bad_sweep_count: i64,     // 新坏扫描数
    permanent: bool,
}

/// 调度加权评分入参（复刻 scheduler.weightedShuffle 评分）。
#[derive(Deserialize)]
struct WeightedScoreReq {
    success_rate: f64,    // 0..100
    consecutive_fail: i64,
    weight_boost: f64,    // Auto-Pilot 注入的权重乘子
}

#[derive(Serialize)]
struct WeightedScoreResp {
    score: f64,
}

/// 按 Key 熔断阈值检查入参（复刻 scheduler.checkCircuitBreaker）。
#[derive(Deserialize)]
struct CircuitCheckReq {
    consecutive_fail: i64,
    threshold: i64,
    base_cooldown_sec: i64,
}

#[derive(Serialize)]
struct CircuitCheckResp {
    should_open: bool,
    cooldown_sec: i64,
    cool_until: i64, // Unix 秒
}

/// 可用度评分：成功率 65% + 延迟 35%（延迟 2s+ 视为 0）。无流量返回 0。
fn availability_score(success_rate: f64, avg_latency_ms: f64, total: i64) -> f64 {
    if total <= 0 {
        return 0.0;
    }
    let sf = success_rate / 100.0;
    let mut ln = 1.0 - avg_latency_ms / 2000.0;
    if ln < 0.0 { ln = 0.0; }
    if ln > 1.0 { ln = 1.0; }
    100.0 * (0.65 * sf + 0.35 * ln)
}

/// 当前 Unix 秒。
fn now_unix() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// 指数退避冷却（每次坏扫描翻倍，封顶 10min=600s）。
/// 与 modelhealth.nextCooldown 对齐：迭代 (bad_sweep-1) 次。
/// 循环内提前封顶，避免极端 bad_sweep 下 i64 溢出（Go 借 time.Duration 静默回绕）。
fn next_cooldown(bad_sweep: i64, base_sec: i64) -> i64 {
    let mut cooldown = base_sec;
    for _ in 1..bad_sweep {
        if cooldown >= 600 {
            break;
        }
        cooldown *= 2;
    }
    if cooldown > 600 {
        cooldown = 600;
    }
    cooldown
}

/// 模型健康判定（与 modelhealth.applyVerdict 决策对齐）。
fn verdict(req: &VerdictReq) -> VerdictResp {
    let now = now_unix();
    let mut bad = req.bad_sweep_count;
    let mut state = req.current_state.clone();
    let mut open_until: i64 = 0;

    if req.success_rate >= req.threshold {
        // 健康：闭合（permanent 保留），清零坏扫描
        if state != "permanent" {
            state = "closed".into();
        }
        bad = 0;
    } else if req.success_rate < req.floor {
        // 远低于地板：累加坏扫描，达阈值永久熔断，否则临时 open（扁平冷却）
        bad += 1;
        if bad >= req.bad_to_perm {
            state = "permanent".into();
        } else if state != "permanent" {
            state = "open".into();
            open_until = now + req.cooldown_base_sec;
        }
    } else {
        // 地板≤率<阈值：临时 open（指数退避冷却，用当前 bad_sweep）
        if state != "permanent" {
            state = "open".into();
            open_until = now + next_cooldown(bad, req.cooldown_base_sec);
        }
    }
    VerdictResp {
        permanent: state == "permanent",
        state,
        open_until,
        bad_sweep_count: bad,
    }
}

/// 调度加权评分（与 scheduler.weightedShuffle 评分对齐）。
fn weighted_score(req: &WeightedScoreReq) -> f64 {
    let penalty = 0.5f64.powi(req.consecutive_fail as i32);
    let mut score = req.success_rate * penalty * req.weight_boost;
    if score < 1.0 {
        score = 1.0;
    }
    score
}

/// 按 Key 熔断阈值检查（与 scheduler.checkCircuitBreaker 对齐）。
fn circuit_check(req: &CircuitCheckReq) -> CircuitCheckResp {
    if req.consecutive_fail < req.threshold {
        return CircuitCheckResp { should_open: false, cooldown_sec: 0, cool_until: 0 };
    }
    let mut cooldown = req.base_cooldown_sec;
    // Go: for i:=1; i < consec-threshold+1; i++ → (consec-threshold) 次翻倍
    let excess = req.consecutive_fail - req.threshold + 1;
    for _ in 1..excess {
        if cooldown >= 600 {
            break;
        }
        cooldown *= 2;
    }
    if cooldown > 600 {
        cooldown = 600;
    }
    CircuitCheckResp {
        should_open: true,
        cooldown_sec: cooldown,
        cool_until: now_unix() + cooldown,
    }
}

// ─── handlers ─────────────────────────────────────────────────────────

async fn forecast(
    State(engine): State<Arc<Engine>>,
    Json(req): Json<ForecastReq>,
) -> Result<Json<ForecastResp>, StatusCode> {
    let (level, trend) = match engine.fit(&req.counts) {
        Some((l, t, _)) => (l, t),
        None => (0.0, 0.0),
    };
    let next = engine.forecast(&req.counts);
    Ok(Json(ForecastResp { forecast_next: next, level, trend }))
}

async fn availability(
    Json(req): Json<AvailabilityReq>,
) -> Result<Json<AvailabilityResp>, StatusCode> {
    let score = availability_score(req.success_rate, req.avg_latency_ms, req.total);
    Ok(Json(AvailabilityResp { score }))
}

async fn verdict_handler(
    Json(req): Json<VerdictReq>,
) -> Result<Json<VerdictResp>, StatusCode> {
    Ok(Json(verdict(&req)))
}

async fn weighted_score_handler(
    Json(req): Json<WeightedScoreReq>,
) -> Result<Json<WeightedScoreResp>, StatusCode> {
    Ok(Json(WeightedScoreResp { score: weighted_score(&req) }))
}

async fn circuit_check_handler(
    Json(req): Json<CircuitCheckReq>,
) -> Result<Json<CircuitCheckResp>, StatusCode> {
    Ok(Json(circuit_check(&req)))
}

async fn healthz() -> &'static str { "ok" }

#[tokio::main]
async fn main() {
    let addr = SocketAddr::from(([0, 0, 0, 0], 8790));
    let engine = Arc::new(Engine::new());
    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/forecast", post(forecast))
        .route("/availability", post(availability))
        .route("/verdict", post(verdict_handler))
        .route("/weighted-score", post(weighted_score_handler))
        .route("/circuit-check", post(circuit_check_handler))
        .with_state(engine);

    let listener = tokio::net::TcpListener::bind(addr).await.expect("bind 8790");
    eprintln!("[nslmcrs-kernel] listening on {addr}");
    axum::serve(listener, app).await.expect("serve");
}

#[cfg(test)]
mod tests {
    use super::*;

    fn vreq(rate: i64, state: &str, bad: i64) -> VerdictReq {
        VerdictReq {
            success_rate: rate, current_state: state.into(), bad_sweep_count: bad,
            floor: 30, threshold: 80, bad_to_perm: 3, cooldown_base_sec: 30,
        }
    }

    #[test]
    fn verdict_healthy_closes_and_clears_bad() {
        let r = verdict(&vreq(90, "open", 2));
        assert_eq!(r.state, "closed");
        assert_eq!(r.bad_sweep_count, 0);
        assert_eq!(r.open_until, 0);
        assert!(!r.permanent);
    }

    #[test]
    fn verdict_midrange_open_backoff() {
        // 50（30≤50<80），bad=2 → next_cooldown(2,30)=60
        let r = verdict(&vreq(50, "closed", 2));
        assert_eq!(r.state, "open");
        assert!(!r.permanent);
        let now = now_unix();
        assert!(r.open_until >= now + 57 && r.open_until <= now + 63);
    }

    #[test]
    fn verdict_bad_floor_increments_and_permanent_at_threshold() {
        // rate=10 < floor=30：bad 0→1 open
        let r1 = verdict(&vreq(10, "closed", 0));
        assert_eq!(r1.state, "open");
        assert_eq!(r1.bad_sweep_count, 1);
        // bad 2→3 → permanent
        let r3 = verdict(&vreq(10, "open", 2));
        assert_eq!(r3.state, "permanent");
        assert!(r3.permanent);
        assert_eq!(r3.bad_sweep_count, 3);
    }

    #[test]
    fn verdict_permanent_stays_on_healthy() {
        let r = verdict(&vreq(95, "permanent", 3));
        assert_eq!(r.state, "permanent");
        assert!(r.permanent);
        // 健康清零坏扫描，但不解除永久
        assert_eq!(r.bad_sweep_count, 0);
    }

    #[test]
    fn next_cooldown_backoff_and_cap() {
        assert_eq!(next_cooldown(0, 30), 30);
        assert_eq!(next_cooldown(1, 30), 30);
        assert_eq!(next_cooldown(2, 30), 60);
        assert_eq!(next_cooldown(3, 30), 120);
        assert_eq!(next_cooldown(5, 30), 480);
        assert_eq!(next_cooldown(6, 30), 600); // 960>600 封顶
        assert_eq!(next_cooldown(99, 30), 600);
    }

    #[test]
    fn weighted_score_basic_and_clamp() {
        // 100 * 0.5^0(=1) * 1.0 = 100
        let s = weighted_score(&WeightedScoreReq { success_rate: 100.0, consecutive_fail: 0, weight_boost: 1.0 });
        assert!((s - 100.0).abs() < 1e-9);
        // 100 * 0.5^2(=0.25) * 1.0 = 25
        let s = weighted_score(&WeightedScoreReq { success_rate: 100.0, consecutive_fail: 2, weight_boost: 1.0 });
        assert!((s - 25.0).abs() < 1e-9);
        // 下限 1：0 * ... = 0 → 钳到 1
        let s = weighted_score(&WeightedScoreReq { success_rate: 0.0, consecutive_fail: 5, weight_boost: 1.0 });
        assert!((s - 1.0).abs() < 1e-9);
        // boost 降权：50 * 1 * 0.5 = 25
        let s = weighted_score(&WeightedScoreReq { success_rate: 50.0, consecutive_fail: 0, weight_boost: 0.5 });
        assert!((s - 25.0).abs() < 1e-9);
    }

    #[test]
    fn circuit_check_below_threshold_no_open() {
        let r = circuit_check(&CircuitCheckReq { consecutive_fail: 2, threshold: 5, base_cooldown_sec: 30 });
        assert!(!r.should_open);
        assert_eq!(r.cooldown_sec, 0);
        assert_eq!(r.cool_until, 0);
    }

    #[test]
    fn circuit_check_at_threshold_base_cooldown() {
        let r = circuit_check(&CircuitCheckReq { consecutive_fail: 5, threshold: 5, base_cooldown_sec: 30 });
        assert!(r.should_open);
        assert_eq!(r.cooldown_sec, 30); // excess=1 → 1..1 空 → base
        let now = now_unix();
        assert!(r.cool_until >= now + 27 && r.cool_until <= now + 33);
    }

    #[test]
    fn circuit_check_above_threshold_backoff_and_cap() {
        // consec=7, threshold=5 → excess=3 → 1..3 = 2 次翻倍 → 30*4=120
        let r = circuit_check(&CircuitCheckReq { consecutive_fail: 7, threshold: 5, base_cooldown_sec: 30 });
        assert!(r.should_open);
        assert_eq!(r.cooldown_sec, 120);
        // 大值封顶 600
        let r = circuit_check(&CircuitCheckReq { consecutive_fail: 99, threshold: 5, base_cooldown_sec: 30 });
        assert_eq!(r.cooldown_sec, 600);
    }
}
