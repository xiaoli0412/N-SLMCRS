//! nslmcrs-kernel — Rust 内核 sidecar。
//!
//! v0.7：数值计算下沉（/forecast、/availability）。
//! v0.11：策略决策下沉（/verdict、/weighted-score、/circuit-check，无状态纯函数）。
//! v0.12：全量 Rust 控制面权威化——令牌桶/健康窗/按 Key 熔断器由本服务持有为
//!   权威状态（内存 + 持久化），新增 /reserve（准入批量选 Key）、/report（反馈
//!   批量更新状态）。Go 主干经 HTTP/JSON 调用；不可达时降级回内置 Go 实现
//!   （KERNEL_FAIL_CLOSED=0）；翻 1 后硬 fail-closed（/reserve 失败即拒绝准入）。
//!
//! 模块：compute（纯函数）/ state（权威状态）/ store（持久化）/
//!       reserve、report（控制面端点）/ testsupport（测试支持）。

mod compute;
mod report;
mod reserve;
mod state;
mod store;
#[cfg(test)]
mod testsupport;

use axum::{
    extract::State,
    http::StatusCode,
    routing::{get, post},
    Json, Router,
};
use std::env;
use std::net::SocketAddr;
use std::sync::Arc;

use compute::{
    availability_score, circuit_check, verdict, weighted_score, AvailabilityReq, AvailabilityResp,
    CircuitCheckReq, CircuitCheckResp, Engine, ForecastReq, ForecastResp, VerdictReq, VerdictResp,
    WeightedScoreReq, WeightedScoreResp,
};
use state::{AppState, SharedState};
use store as kv;

// ─── handlers ─────────────────────────────────────────────────────────

async fn forecast(
    State(st): State<SharedState>,
    Json(req): Json<ForecastReq>,
) -> Result<Json<ForecastResp>, StatusCode> {
    let (level, trend) = match st.engine.fit(&req.counts) {
        Some((l, t, _)) => (l, t),
        None => (0.0, 0.0),
    };
    let next = st.engine.forecast(&req.counts);
    Ok(Json(ForecastResp {
        forecast_next: next,
        level,
        trend,
    }))
}

async fn availability(
    Json(req): Json<AvailabilityReq>,
) -> Result<Json<AvailabilityResp>, StatusCode> {
    let score = availability_score(req.success_rate, req.avg_latency_ms, req.total);
    Ok(Json(AvailabilityResp { score }))
}

async fn verdict_handler(Json(req): Json<VerdictReq>) -> Result<Json<VerdictResp>, StatusCode> {
    Ok(Json(verdict(&req)))
}

async fn weighted_score_handler(
    Json(req): Json<WeightedScoreReq>,
) -> Result<Json<WeightedScoreResp>, StatusCode> {
    Ok(Json(WeightedScoreResp {
        score: weighted_score(req.success_rate, req.consecutive_fail, req.weight_boost),
    }))
}

async fn circuit_check_handler(
    Json(req): Json<CircuitCheckReq>,
) -> Result<Json<CircuitCheckResp>, StatusCode> {
    let r = circuit_check(req.consecutive_fail, req.threshold, req.base_cooldown_sec);
    Ok(Json(r))
}

async fn healthz() -> &'static str {
    "ok"
}

#[tokio::main]
async fn main() {
    let port: u16 = env::var("KERNEL_PORT")
        .ok()
        .and_then(|s| s.parse().ok())
        .unwrap_or(8790);
    let addr = SocketAddr::from(([0, 0, 0, 0], port));

    // v0.12：自有 SQLite 库（熔断器态 + 子决策审计）。
    let db_path = env::var("KERNEL_DB_PATH").unwrap_or_else(|_| "data/kernel.db".into());
    if let Some(parent) = std::path::Path::new(&db_path).parent() {
        let _ = std::fs::create_dir_all(parent);
    }
    let conn = kv::open(&db_path).unwrap_or_else(|e| {
        eprintln!("[nslmcrs-kernel] open kernel db failed ({db_path}): {e}");
        std::process::exit(1);
    });

    // 启动载入熔断器态到内存镜像（桶/窗按设计重启即空）。
    let mut key_brk = std::collections::HashMap::new();
    if let Ok(loaded) = kv::load_all(&conn) {
        for (id, kb) in loaded {
            key_brk.insert(id, kb);
        }
        eprintln!(
            "[nslmcrs-kernel] loaded {} key breaker(s) from {db_path}",
            key_brk.len()
        );
    }

    let app_state = AppState {
        engine: Engine::new(),
        buckets: std::sync::RwLock::new(std::collections::HashMap::new()),
        windows: std::sync::RwLock::new(std::collections::HashMap::new()),
        key_brk: std::sync::RwLock::new(key_brk),
        store: std::sync::Mutex::new(conn),
    };
    let st: SharedState = Arc::new(app_state);

    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/forecast", post(forecast))
        .route("/availability", post(availability))
        .route("/verdict", post(verdict_handler))
        .route("/weighted-score", post(weighted_score_handler))
        .route("/circuit-check", post(circuit_check_handler))
        // v0.12 控制面端点
        .route("/reserve", post(reserve::reserve_handler))
        .route("/report", post(report::report_handler))
        .with_state(st);

    let listener = tokio::net::TcpListener::bind(addr)
        .await
        .expect("bind kernel port");
    eprintln!("[nslmcrs-kernel] listening on {addr} (db={db_path})");
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await
        .expect("serve");
}

async fn shutdown_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c().await.expect("ctrl_c");
    };
    #[cfg(unix)]
    let sigterm = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .expect("sigterm")
            .recv()
            .await;
    };
    #[cfg(not(unix))]
    let sigterm = std::future::pending::<()>();
    tokio::select! {
        _ = ctrl_c => {}
        _ = sigterm => {}
    }
    eprintln!("[nslmcrs-kernel] shutdown signal received");
}
