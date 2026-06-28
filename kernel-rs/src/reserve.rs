//! /reserve 准入端点（v0.12）：批量选 Key + 令牌消费 + 加权随机排序。
//!
//! 复刻 Go `scheduler.selectKeys`（熔断过滤 + 半开试探 + 令牌余量）与
//! `scheduler.weightedShuffle`（成功率 × 0.5^连续失败 × boost，加权随机排列）。
//! 把 N 次 RPC/请求（逐 Key weighted-score + circuit-check）收敛为 1 次 RPC。
//!
//! 关键不变量：
//!   - 仅返回 key_id（不接触密钥明文），Go 调用方映射回本地 key 结构。
//!   - 每个保留 key 消费 1 令牌（与 Go selectKeys 的 rl.Allow 语义一致）。
//!   - 每轮仅放行 1 个半开试探（half_open），避免同时试探多个待恢复 Key。

use axum::{extract::State, http::StatusCode, Json};
use rand::Rng;
use serde::{Deserialize, Serialize};

use crate::compute::{now_unix, weighted_score};
use crate::state::{KeyBreaker, SharedState};
use crate::store;

// ─── DTO（与 Go kernelctl 契约对齐，snake_case）────────────────────────

#[derive(Deserialize)]
pub struct Candidate {
    pub key_id: i64,
    pub rpm: i64,
    pub weight_boost: f64, // Auto-Pilot 注入的权重乘子
}

#[derive(Deserialize)]
pub struct ReserveReq {
    pub trace_id: String,
    pub model: String,
    pub concurrency: i64,
    /// 按 Key 熔断阈值（契约字段；/report 用于判定开熔断，/reserve 透传不读）
    #[allow(dead_code)]
    pub circuit_threshold: i64,
    #[allow(dead_code)]
    pub circuit_cooldown_base_sec: i64,
    pub health_window_sec: i64,
    pub candidates: Vec<Candidate>,
}

#[derive(Serialize)]
pub struct KeyBreakerChange {
    pub key_id: i64,
    pub status: String,
    pub consecutive_fail: i64,
    pub cooling_until: i64,
}

#[derive(Serialize)]
pub struct ReserveResp {
    pub trace_id: String,
    /// 有序 key_id（已消费令牌，按加权随机排列，取前 concurrency 个）
    pub reserved: Vec<i64>,
    /// 本轮熔断器变更（半开提升等），供 Go echo 回写 upstream_keys
    pub key_breaker_changes: Vec<KeyBreakerChange>,
}

// ─── handler ─────────────────────────────────────────────────────────

pub async fn reserve_handler(
    State(st): State<SharedState>,
    Json(req): Json<ReserveReq>,
) -> Result<Json<ReserveResp>, StatusCode> {
    Ok(Json(reserve_inner(&st, &req)))
}

/// /reserve 逻辑（同步、可单测）。
pub fn reserve_inner(st: &SharedState, req: &ReserveReq) -> ReserveResp {
    let now = now_unix();
    let mut changes: Vec<KeyBreakerChange> = Vec::new();
    let mut viable: Vec<(i64, f64)> = Vec::new(); // (key_id, score)
    let mut half_open_allowed: i64 = 1;

    {
        let store_conn = st.store.lock().unwrap();
        let mut brk = st.key_brk.write().unwrap();
        let mut buckets = st.buckets.write().unwrap();
        let mut windows = st.windows.write().unwrap();
        for c in &req.candidates {
            // 懒建熔断器（新 Key 默认 active）
            let kb = brk.entry(c.key_id).or_default();
            if kb.status == "circuit_open" {
                if kb.cooling_until > now {
                    continue; // 仍在冷却期
                }
                // 冷却到期 → 转半开，放行一个试探
                kb.status = "half_open".into();
                kb.consecutive_fail = 0;
                kb.cooling_until = 0;
                let _ = store::upsert_key_breaker(&store_conn, kb, c.key_id, now);
                let _ = store::append_sub_decision(
                    &store_conn,
                    &req.trace_id,
                    now,
                    "reserve",
                    "half_open_promote",
                    c.key_id,
                    &req.model,
                    "circuit_open",
                    "half_open",
                );
                changes.push(snapshot(kb, c.key_id));
            }
            if kb.status == "half_open" {
                if half_open_allowed <= 0 {
                    continue; // 本轮已有半开试探
                }
                half_open_allowed -= 1;
            }
            // 令牌余量（懒建桶，用入参 rpm）
            let bucket = buckets
                .entry(c.key_id)
                .or_insert_with(|| crate::state::TokenBucket::new(c.rpm));
            if !bucket.allow(1) {
                continue;
            }
            // 加权评分：成功率（窗）× 0.5^连续失败 × boost，下限 1
            let rate = windows
                .entry(c.key_id)
                .or_insert_with(|| crate::state::SlidingWindow::new(req.health_window_sec))
                .success_rate();
            let score = weighted_score(rate, kb.consecutive_fail, c.weight_boost);
            viable.push((c.key_id, score));
        }
    }

    if viable.is_empty() {
        return ReserveResp {
            trace_id: req.trace_id.clone(),
            reserved: Vec::new(),
            key_breaker_changes: changes,
        };
    }

    // 加权随机排列（复刻 weightedShuffle 蓄水池变体）
    weighted_shuffle(&mut viable);

    // 取前 concurrency 个
    let take = (req.concurrency.max(1) as usize).min(viable.len());
    let reserved: Vec<i64> = viable.iter().take(take).map(|(id, _)| *id).collect();

    ReserveResp {
        trace_id: req.trace_id.clone(),
        reserved,
        key_breaker_changes: changes,
    }
}

/// 加权随机排列：每轮按 score 加权随机抽一个，追加到结果尾，从剩余移除。
/// 与 Go scheduler.weightedShuffle 数值流程对齐（时间种子 RNG，非确定性）。
fn weighted_shuffle(items: &mut Vec<(i64, f64)>) {
    let mut rng = rand::thread_rng();
    let mut result: Vec<(i64, f64)> = Vec::with_capacity(items.len());
    while !items.is_empty() {
        let rem_weight: f64 = items.iter().map(|(_, s)| *s).sum();
        let pick = rng.gen::<f64>() * rem_weight;
        let mut accum = 0.0;
        let mut chosen = 0usize;
        for (i, (_, s)) in items.iter().enumerate() {
            accum += *s;
            if accum >= pick {
                chosen = i;
                break;
            }
        }
        result.push(items.remove(chosen));
    }
    *items = result;
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

    fn candidates(n: i64) -> Vec<Candidate> {
        (1..=n)
            .map(|i| Candidate {
                key_id: i,
                rpm: 500,
                weight_boost: 1.0,
            })
            .collect()
    }

    fn req(cands: Vec<Candidate>) -> ReserveReq {
        ReserveReq {
            trace_id: "t".into(),
            model: "m".into(),
            concurrency: 3,
            circuit_threshold: 5,
            circuit_cooldown_base_sec: 30,
            health_window_sec: 120,
            candidates: cands,
        }
    }

    /// 无熔断时，全部候选应被保留并消费令牌；返回顺序为合法置换。
    #[test]
    fn reserve_returns_permutation_within_concurrency() {
        let st = crate::testsupport::state();
        let r = reserve_inner(&st, &req(candidates(4)));
        assert_eq!(r.reserved.len(), 3); // concurrency=3
        let mut sorted = r.reserved.clone();
        sorted.sort();
        // 三个不同 key
        assert_eq!(
            sorted.len(),
            sorted
                .iter()
                .collect::<std::collections::HashSet<_>>()
                .len()
        );
    }

    /// 令牌耗尽后该 Key 不再被保留。
    #[test]
    fn reserve_skips_keys_with_empty_bucket() {
        let st = crate::testsupport::state();
        // 先把 key 1 的桶抽干（rpm=2 → 容量 2，消费 2 次即空且未回填）
        let r1 = reserve_inner(
            &st,
            &req(vec![Candidate {
                key_id: 1,
                rpm: 2,
                weight_boost: 1.0,
            }]),
        );
        let r2 = reserve_inner(
            &st,
            &req(vec![Candidate {
                key_id: 1,
                rpm: 2,
                weight_boost: 1.0,
            }]),
        );
        let r3 = reserve_inner(
            &st,
            &req(vec![Candidate {
                key_id: 1,
                rpm: 2,
                weight_boost: 1.0,
            }]),
        );
        assert_eq!(r1.reserved.len(), 1);
        assert_eq!(r2.reserved.len(), 1);
        // 第三次：桶空（2 个令牌已消费，瞬时未回填），不再保留
        assert!(r3.reserved.is_empty());
    }

    /// circuit_open 且冷却未到期 → 跳过；冷却到期 → 半开放行（每轮 1 个试探）。
    /// 复刻 Go selectKeys：所有冷却到期的 circuit_open 都提升为 half_open 并持久化，
    /// 但每轮仅放行 1 个 half_open 试探（其余跳过）。
    #[test]
    fn reserve_half_open_promotion_one_per_round() {
        let st = crate::testsupport::state();
        // 手动置两个 Key 为 circuit_open，冷却已到期（cooling_until=1 < now）
        {
            let mut brk = st.key_brk.write().unwrap();
            for id in 1..=2 {
                brk.insert(
                    id,
                    KeyBreaker {
                        status: "circuit_open".into(),
                        consecutive_fail: 5,
                        cooling_until: 1,
                    },
                );
            }
        }
        let r = reserve_inner(&st, &req(candidates(2)));
        // 两个都提升为 half_open（Go selectKeys 对所有冷却到期者持久化 half_open）
        assert_eq!(r.key_breaker_changes.len(), 2);
        for c in &r.key_breaker_changes {
            assert_eq!(c.status, "half_open");
        }
        // 但仅放行 1 个试探
        assert_eq!(r.reserved.len(), 1);
    }
}
