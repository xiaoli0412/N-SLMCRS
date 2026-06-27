//! nslmcrs-kernel — Rust 内核 sidecar（v0.7）。
//!
//! 把数值计算密集的两块从 Go 抽出，作为独立微服务：
//!   - POST /forecast   ：Holt-Winters 三次指数平滑预测下一桶 RPM
//!   - POST /availability：按请求样本聚合可用度评分（成功率 65% + 延迟 35%）
//!
//! Go 主干经 HTTP/JSON 调用本服务；不可达时降级回内置 Go 实现（无单点依赖）。
//! 保留 Go 单二进制主干与 distroless 优势，Rust 仅承担真正 CPU 密集的数值计算。

use axum::{
    extract::State, http::StatusCode, routing::{get, post}, Json, Router,
};
use serde::{Deserialize, Serialize};
use std::net::SocketAddr;
use std::sync::Arc;

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

async fn healthz() -> &'static str { "ok" }

#[tokio::main]
async fn main() {
    let addr = SocketAddr::from(([0, 0, 0, 0], 8790));
    let engine = Arc::new(Engine::new());
    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/forecast", post(forecast))
        .route("/availability", post(availability))
        .with_state(engine);

    let listener = tokio::net::TcpListener::bind(addr).await.expect("bind 8790");
    eprintln!("[nslmcrs-kernel] listening on {addr}");
    axum::serve(listener, app).await.expect("serve");
}
