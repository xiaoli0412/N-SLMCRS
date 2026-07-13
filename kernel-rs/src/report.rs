//! /report 反馈端点（v0.12）：批量回传请求结果，更新健康窗/熔断器/令牌校准。
//!
//! 复刻 Go `scheduler.recordSuccess`/`recordResult` 的状态部分 +
//! `checkCircuitBreaker`（按 Key）+ `rl.Calibrate`，收敛为 1 次 RPC/请求。
//! 本地部分（request_logs 写入、webhook）仍由 Go 完成；本端只更新权威状态。
//!
//! 模型级熔断（model_circuit）不在本端处理：其与 30min 健康扫描 sweeper 共驱动
//! （sweeper 计算 success_rate/bad_sweep/permanent），而 sweeper 仍留在 Go 侧
//! （本次范围外）。故模型被动反馈继续由 Go 的 feedbackModelCircuit 落地（其内部
//! RecordModelCircuitFailure/ResetModelCircuitConsecutive 已保留 permanent）。
//! 本端专注按 Key 控制面权威化（准入真正依赖的那一层）。

use axum::{extract::State, http::StatusCode, Json};
use serde::{Deserialize, Serialize};

use crate::compute::{circuit_check, now_unix};
use crate::state::{KeyBreaker, SharedState};
use crate::store;

// ─── DTO（与 Go kernelctl 契约对齐，snake_case）────────────────────────

#[derive(Deserialize)]
pub struct ReportItem {
    pub key_id: i64,
    pub success: bool,
    /// success | error | rate_limited（契约字段，未来按类型差异化退避；当前仅 success 判定）
    #[allow(dead_code)]
    pub status: String,
    pub rate_limit_remaining: Option<i64>, // 上游 X-RateLimit-Remaining
}

#[derive(Deserialize)]
pub struct ReportReq {
    pub trace_id: String,
    pub circuit_threshold: i64,
    pub circuit_cooldown_base_sec: i64,
    pub health_window_sec: i64,
    pub results: Vec<ReportItem>,
}

#[derive(Serialize)]
pub struct KeyBreakerChange {
    pub key_id: i64,
    pub status: String,
    pub consecutive_fail: i64,
    pub cooling_until: i64,
}

#[derive(Serialize)]
pub struct ReportResp {
    /// 本批熔断器变更（开/闭/半开恢复），供 Go echo 回写 upstream_keys
    pub key_breaker_changes: Vec<KeyBreakerChange>,
}

// ─── handler ─────────────────────────────────────────────────────────

pub async fn report_handler(
    State(st): State<SharedState>,
    Json(req): Json<ReportReq>,
) -> Result<Json<ReportResp>, StatusCode> {
    Ok(Json(report_inner(&st, &req)))
}

/// /report 逻辑（同步、可单测）。
pub fn report_inner(st: &SharedState, req: &ReportReq) -> ReportResp {
    let now = now_unix();
    // 按 key 去重的变更集：仅状态发生转换（active↔half_open↔circuit_open）时回显，
    // 与 Go checkCircuitBreaker/markHealthy 只在状态变更时写 upstream_keys 对齐。
    // 连续失败计数在状态未变时不回显（Go 不在 active 态写 consec）。
    let mut change_map: std::collections::HashMap<i64, KeyBreakerChange> =
        std::collections::HashMap::new();

    {
        let store_conn = st.store.lock().unwrap();
        let mut brk = st.key_brk.write().unwrap();
        let mut windows = st.windows.write().unwrap();
        let mut buckets = st.buckets.write().unwrap();

        for r in &req.results {
            // 滑动窗记录（懒建）
            windows
                .entry(r.key_id)
                .or_insert_with(|| crate::state::SlidingWindow::new(req.health_window_sec))
                .record(r.success);

            // 令牌校准（上游更紧时收紧）
            if let Some(rem) = r.rate_limit_remaining {
                buckets
                    .entry(r.key_id)
                    .or_insert_with(|| crate::state::TokenBucket::new(500))
                    .calibrate(rem);
            }

            // 连续失败计数 + 熔断判定（复刻 checkCircuitBreaker）
            let kb = brk.entry(r.key_id).or_default();
            let before_status = kb.status.clone();
            if r.success {
                kb.consecutive_fail = 0;
                // 半开试探成功 → 闭合为 active
                if kb.status == "half_open" {
                    kb.status = "active".into();
                    kb.cooling_until = 0;
                }
            } else {
                kb.consecutive_fail += 1;
                let ck = circuit_check(
                    kb.consecutive_fail,
                    req.circuit_threshold,
                    req.circuit_cooldown_base_sec,
                );
                if ck.should_open {
                    kb.status = "circuit_open".into();
                    kb.cooling_until = ck.cool_until;
                }
            }

            // 状态转换才落库 + 记审计 + echo（与 Go 写 upstream_keys 时机一致）
            if kb.status != before_status {
                let _ = store::upsert_key_breaker(&store_conn, kb, r.key_id, now);
                let kind = if r.success {
                    "circuit_close"
                } else {
                    "circuit_open"
                };
                let _ = store::append_sub_decision(
                    &store_conn,
                    &req.trace_id,
                    now,
                    "report",
                    kind,
                    r.key_id,
                    "",
                    &before_status,
                    &kb.status,
                );
                // 覆盖为最终态（同 key 多次转换取最后一次）
                change_map.insert(r.key_id, snapshot(kb, r.key_id));
            }
        }
    }

    // 按 key_id 升序输出，保证可复现。
    let mut changes: Vec<KeyBreakerChange> = change_map.into_values().collect();
    changes.sort_by_key(|c| c.key_id);

    ReportResp {
        key_breaker_changes: changes,
    }
}

fn snapshot(kb: &KeyBreaker, key_id: i64) -> KeyBreakerChange {
    KeyBreakerChange {
        key_id,
        status: kb.status.clone(),
        consecutive_fail: kb.consecutive_fail,
        cooling_until: kb.cooling_until,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn req(items: Vec<ReportItem>) -> ReportReq {
        ReportReq {
            trace_id: "t".into(),
            circuit_threshold: 3,
            circuit_cooldown_base_sec: 30,
            health_window_sec: 120,
            results: items,
        }
    }

    fn fail(id: i64) -> ReportItem {
        ReportItem {
            key_id: id,
            success: false,
            status: "error".into(),
            rate_limit_remaining: None,
        }
    }
    fn ok(id: i64) -> ReportItem {
        ReportItem {
            key_id: id,
            success: true,
            status: "success".into(),
            rate_limit_remaining: None,
        }
    }

    /// 连续失败达阈值 → circuit_open，返回变更。
    #[test]
    fn report_opens_circuit_at_threshold() {
        let st = crate::testsupport::state();
        // threshold=3 → 3 次失败开熔断
        let r1 = report_inner(&st, &req(vec![fail(1), fail(1), fail(1)]));
        assert_eq!(r1.key_breaker_changes.len(), 1);
        let c = &r1.key_breaker_changes[0];
        assert_eq!(c.status, "circuit_open");
        assert_eq!(c.consecutive_fail, 3);
        assert!(c.cooling_until > 0);
    }

    /// 成功清零连续失败；半开试探成功 → 闭合 active。
    #[test]
    fn report_success_closes_half_open() {
        let st = crate::testsupport::state();
        // 先开熔断
        report_inner(&st, &req(vec![fail(1), fail(1), fail(1)]));
        // 手动置 half_open（模拟 /reserve 提升）后报成功
        {
            let mut brk = st.key_brk.write().unwrap();
            brk.get_mut(&1).unwrap().status = "half_open".into();
            brk.get_mut(&1).unwrap().consecutive_fail = 0;
        }
        let r = report_inner(&st, &req(vec![ok(1)]));
        assert_eq!(r.key_breaker_changes.len(), 1);
        assert_eq!(r.key_breaker_changes[0].status, "active");
        assert_eq!(r.key_breaker_changes[0].consecutive_fail, 0);
    }

    /// 未达阈值 → 不产生变更。
    #[test]
    fn report_below_threshold_no_change() {
        let st = crate::testsupport::state();
        let r = report_inner(&st, &req(vec![fail(1), fail(1)])); // 2 < 3
        assert!(r.key_breaker_changes.is_empty());
    }

    /// 令牌校准：上游 remaining 更紧时收紧桶余量。
    #[test]
    fn report_calibrates_bucket_down() {
        let st = crate::testsupport::state();
        // 先消费 1 令牌建立桶（rpm=500 → 满桶 500）
        let _ = crate::reserve::reserve_inner(
            &st,
            &crate::reserve::ReserveReq {
                trace_id: "t".into(),
                model: "m".into(),
                concurrency: 1,
                circuit_threshold: 3,
                circuit_cooldown_base_sec: 30,
                health_window_sec: 120,
                candidates: vec![crate::reserve::Candidate {
                    key_id: 1,
                    rpm: 500,
                    weight_boost: 1.0,
                    inflight: 0,
                }],
            },
        );
        // 上游报 remaining=10 → 桶收紧到 10
        report_inner(
            &st,
            &req(vec![ReportItem {
                key_id: 1,
                success: true,
                status: "success".into(),
                rate_limit_remaining: Some(10),
            }]),
        );
        // 行为断言：余量收紧到 10，可再消费 10 次，第 11 次失败
        let mut ok_count = 0;
        for _ in 0..15 {
            let got = st.buckets.write().unwrap().get_mut(&1).unwrap().allow(1);
            if got {
                ok_count += 1;
            }
        }
        assert_eq!(ok_count, 10);
    }
}
