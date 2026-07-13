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
use std::sync::atomic::Ordering;

use crate::compute::{now_unix, weighted_score};
use crate::state::{KeyBreaker, SharedState};
use crate::store;
use crate::strategy::SelectionAlgo;

// ─── DTO（与 Go kernelctl 契约对齐，snake_case）────────────────────────

#[derive(Deserialize)]
pub struct Candidate {
    pub key_id: i64,
    pub rpm: i64,
    pub weight_boost: f64, // Auto-Pilot 注入的权重乘子
    /// 该 Key 当前在途请求数（Go 调度器维护，LeastInflight 排序用；0=未跟踪）
    #[serde(default)]
    pub inflight: i64,
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
    // v0.14：读活跃策略——选择算法/扇出/RPM 头寸由此派发。
    let strat = st.active_strategy();
    // (key_id, score, inflight)：inflight 供 LeastInflight 排序。
    let mut viable: Vec<(i64, f64, i64)> = Vec::new();
    let mut half_open_allowed: i64 = 1;

    // peek→选→消费 在同一锁域内完成，保证原子：避免并发 /reserve 在 peek 与消费之间
    // 双取同一「末令牌」Key。锁粒度优化（移 SQLite 写出临界区）留待后续。
    let reserved: Vec<i64> = {
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
            // 令牌余量（懒建桶，用入参 rpm）—— v0.14：按策略头寸 peek（headroom<1 时
            // 保留地板，切策略即刻收紧/放宽准入，无需重建桶）。
            let bucket = buckets
                .entry(c.key_id)
                .or_insert_with(|| crate::state::TokenBucket::new(c.rpm));
            if !bucket.has_admission(strat.rpm_headroom) {
                continue;
            }
            // 加权评分：成功率（窗）× 0.5^连续失败 × boost，下限 1
            let rate = windows
                .entry(c.key_id)
                .or_insert_with(|| crate::state::SlidingWindow::new(req.health_window_sec))
                .success_rate();
            let score = weighted_score(rate, kb.consecutive_fail, c.weight_boost);
            viable.push((c.key_id, score, c.inflight));
        }

        if viable.is_empty() {
            return ReserveResp {
                trace_id: req.trace_id.clone(),
                reserved: Vec::new(),
                key_breaker_changes: changes,
            };
        }

        // v0.14：按策略选择算法排序候选。
        select_by_algo(st, strat.selection, &mut viable);

        // 扇出：策略 fanout>0 覆盖请求方 concurrency（Guardian/Fairshare 固定 1）。
        let cap = if strat.fanout > 0 {
            strat.fanout as usize
        } else {
            req.concurrency.max(1) as usize
        };
        let take = cap.min(viable.len());
        let reserved: Vec<i64> = viable.iter().take(take).map(|(id, _, _)| *id).collect();
        // v0.13 (B2)：仅对最终保留的 N 个消费令牌（旧实现对全部 viable 消费，白耗配额）
        for id in &reserved {
            if let Some(b) = buckets.get_mut(id) {
                b.allow(1);
            }
        }
        reserved
    };

    ReserveResp {
        trace_id: req.trace_id.clone(),
        reserved,
        key_breaker_changes: changes,
    }
}

/// 按策略选择算法对可行候选排序（原地）。
///   - WeightedRandom：健康加权随机排列（成功率 × 0.5^连续失败 × boost 加权蓄水池）。
///   - RoundRobin：按 key_id 稳定排序后按 rr_counter 轮转起点，均匀分发。
///   - LeastInflight：在途数升序，同分按评分降序（排队最少的优先）。
///   - StrictPriority：评分降序取前 N（永远先打最健康的）。
fn select_by_algo(st: &SharedState, algo: SelectionAlgo, viable: &mut Vec<(i64, f64, i64)>) {
    match algo {
        SelectionAlgo::WeightedRandom => weighted_shuffle(viable),
        SelectionAlgo::RoundRobin => {
            // 稳定基准序：按 key_id 升序，使轮转起点可复现。
            viable.sort_by_key(|v| v.0);
            let n = viable.len() as u64;
            if n > 0 {
                let rot = (st.rr_counter.load(Ordering::Relaxed) % n) as usize;
                if rot != 0 {
                    viable.rotate_left(rot);
                }
                // 推进指针：每轮 reserve 按实际取用数推进，保持均匀。
                // take 在调用方已知，但此处泛化按 1 推进（fanout=1 时为完美轮转）；
                // fanout>1 时上层取前 N，指针仍单调推进，长期仍趋均匀。
                let take = viable.len().min(1) as u64; // 至少推进 1（实际取用由调用方）
                let _ = st.rr_counter.fetch_add(take.max(1), Ordering::Relaxed);
            }
        }
        SelectionAlgo::LeastInflight => {
            // 在途数升序，同分按评分降序（排队最少的优先；并列时健康的优先）
            viable.sort_by(|a, b| {
                a.2.cmp(&b.2)
                    .then_with(|| b.1.partial_cmp(&a.1).unwrap_or(std::cmp::Ordering::Equal))
            });
        }
        SelectionAlgo::StrictPriority => {
            // 评分降序取前 N（永远先打最健康的）
            viable.sort_by(|a, b| b.1.partial_cmp(&a.1).unwrap_or(std::cmp::Ordering::Equal));
        }
    }
}

/// 加权随机排列：每轮按 score 加权随机抽一个，追加到结果尾，从剩余移除。
/// 与 Go scheduler.weightedShuffle 数值流程对齐（时间种子 RNG，非确定性）。
/// v0.14：viable 增 inflight 维度（.2），加权只看 score（.1），inflight 跟随迁移。
fn weighted_shuffle(items: &mut Vec<(i64, f64, i64)>) {
    let mut rng = rand::thread_rng();
    let mut result: Vec<(i64, f64, i64)> = Vec::with_capacity(items.len());
    while !items.is_empty() {
        let rem_weight: f64 = items.iter().map(|(_, s, _)| *s).sum();
        let pick = rng.gen::<f64>() * rem_weight;
        let mut accum = 0.0;
        let mut chosen = 0usize;
        for (i, (_, s, _)) in items.iter().enumerate() {
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
                inflight: 0,
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
                inflight: 0,
            }]),
        );
        let r2 = reserve_inner(
            &st,
            &req(vec![Candidate {
                key_id: 1,
                rpm: 2,
                weight_boost: 1.0,
                inflight: 0,
            }]),
        );
        let r3 = reserve_inner(
            &st,
            &req(vec![Candidate {
                key_id: 1,
                rpm: 2,
                weight_boost: 1.0,
                inflight: 0,
            }]),
        );
        assert_eq!(r1.reserved.len(), 1);
        assert_eq!(r2.reserved.len(), 1);
        // 第三次：桶空（2 个令牌已消费，瞬时未回填），不再保留
        assert!(r3.reserved.is_empty());
    }

    /// v0.13 (B2)：仅对选中的 N 个消费令牌——未选中候选的令牌不减少。
    /// 2 候选各 rpm=1（容量 1），concurrency=1 → 只选 1 个并消费 1；
    /// 另一个未被选中，其桶应仍满，再单独 reserve 它应成功（旧实现对全部候选
    /// allow(1) 消费，会把第二个的令牌也吃掉，单独再 reserve 会失败）。
    #[test]
    fn reserve_consumes_only_selected_tokens() {
        let st = crate::testsupport::state();
        let two = vec![
            Candidate {
                key_id: 1,
                rpm: 1,
                weight_boost: 1.0,
                inflight: 0,
            },
            Candidate {
                key_id: 2,
                rpm: 1,
                weight_boost: 1.0,
                inflight: 0,
            },
        ];
        let r = reserve_inner(
            &st,
            &ReserveReq {
                trace_id: "t".into(),
                model: "m".into(),
                concurrency: 1,
                circuit_threshold: 5,
                circuit_cooldown_base_sec: 30,
                health_window_sec: 120,
                candidates: two,
            },
        );
        assert_eq!(r.reserved.len(), 1);
        let not_selected = if r.reserved[0] == 1 { 2 } else { 1 };
        // 未被选中的 Key 桶仍满 → 单独 reserve 应成功
        let r2 = reserve_inner(
            &st,
            &req(vec![Candidate {
                key_id: not_selected,
                rpm: 1,
                weight_boost: 1.0,
                inflight: 0,
            }]),
        );
        assert_eq!(r2.reserved.len(), 1, "未被选中的候选令牌不应被消费（B2）");
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

    // ─── v0.14 策略选择算法测试 ──────────────────────────────────────

    /// StrictPriority（Guardian）：永远先打评分最高的密钥，不赌博稀缺资源。
    #[test]
    fn strict_priority_picks_healthiest() {
        let st = crate::testsupport::state();
        st.set_strategy(*crate::strategy::by_id("guardian").unwrap());
        // 预置：key 1 健康（成功率 100），key 2 无流量（score 地板 1）
        {
            let mut w = st.windows.write().unwrap();
            for _ in 0..3 {
                w.entry(1)
                    .or_insert_with(|| crate::state::SlidingWindow::new(120))
                    .record(true);
            }
        }
        // 候选传入顺序 2,1，但 StrictPriority 按评分降序 → key 1（健康）优先
        let r = reserve_inner(
            &st,
            &req(vec![
                Candidate {
                    key_id: 2,
                    rpm: 500,
                    weight_boost: 1.0,
                    inflight: 0,
                },
                Candidate {
                    key_id: 1,
                    rpm: 500,
                    weight_boost: 1.0,
                    inflight: 0,
                },
            ]),
        );
        assert_eq!(r.reserved, vec![1]);
    }

    /// RoundRobin（Fairshare）：按 key_id 稳定序 + rr_counter 轮转，均匀分发。
    #[test]
    fn round_robin_rotates_start() {
        let st = crate::testsupport::state();
        st.set_strategy(*crate::strategy::by_id("fairshare").unwrap());
        // fairshare fanout=1 → 每次 reserve 取 1 个，rr_counter 推进
        let mut seq = Vec::new();
        for _ in 0..3 {
            let r = reserve_inner(&st, &req(candidates(3)));
            seq.push(r.reserved[0]);
        }
        assert_eq!(seq, vec![1, 2, 3], "应轮转 1→2→3");
        // 第 4 次回到 1（3%3=0）
        let r = reserve_inner(&st, &req(candidates(3)));
        assert_eq!(r.reserved[0], 1);
    }

    /// LeastInflight（Velocity）：优先发给在途最少的密钥降 P99。
    #[test]
    fn least_inflight_picks_lowest_queue() {
        let st = crate::testsupport::state();
        st.set_strategy(*crate::strategy::by_id("velocity").unwrap());
        let r = reserve_inner(
            &st,
            &req(vec![
                Candidate {
                    key_id: 1,
                    rpm: 500,
                    weight_boost: 1.0,
                    inflight: 5,
                },
                Candidate {
                    key_id: 2,
                    rpm: 500,
                    weight_boost: 1.0,
                    inflight: 0,
                },
            ]),
        );
        // key 2 在途 0 < key 1 在途 5 → key 2 排首位
        assert_eq!(r.reserved[0], 2);
    }

    /// Guardian 扇出固定 1：即使 concurrency=3 也只取 1 个（消除 loser RPM 浪费）。
    #[test]
    fn guardian_fanout_overrides_concurrency() {
        let st = crate::testsupport::state();
        st.set_strategy(*crate::strategy::by_id("guardian").unwrap());
        let r = reserve_inner(&st, &req(candidates(4))); // req 设 concurrency=3
        assert_eq!(r.reserved.len(), 1, "Guardian 扇出 1 覆盖 concurrency=3");
    }

    /// Guardian RPM 头寸 0.8：rpm=10 桶容量 10，地板=10×0.2=2；
    /// 满→消费到 tokens=2 时阻塞（>2 才放行），即 8 次后第 9 次阻塞（headroom=1.0 时可 10 次）。
    /// 内化头寸：切策略即刻收紧准入，无需重建桶。
    #[test]
    fn guardian_headroom_blocks_at_floor() {
        let st = crate::testsupport::state();
        st.set_strategy(*crate::strategy::by_id("guardian").unwrap());
        let one = |id: i64| Candidate {
            key_id: id,
            rpm: 10,
            weight_boost: 1.0,
            inflight: 0,
        };
        for i in 1..=8 {
            let r = reserve_inner(&st, &req(vec![one(1)]));
            assert_eq!(r.reserved.len(), 1, "第 {i} 次应放行");
        }
        let r = reserve_inner(&st, &req(vec![one(1)]));
        assert!(
            r.reserved.is_empty(),
            "第 9 次应被头寸阻塞（tokens=2 ≤ 地板 2）"
        );
    }

    /// 默认 Balanced（headroom 1.0）骑满：同桶可消费满容量（10 次），无地板保留。
    #[test]
    fn balanced_rides_full_capacity() {
        let st = crate::testsupport::state(); // 默认 balanced
        let one = |id: i64| Candidate {
            key_id: id,
            rpm: 10,
            weight_boost: 1.0,
            inflight: 0,
        };
        let mut ok = 0;
        for _ in 0..12 {
            let r = reserve_inner(&st, &req(vec![one(1)]));
            if !r.reserved.is_empty() {
                ok += 1;
            }
        }
        assert_eq!(ok, 10, "balanced 满容量应放行 10 次");
    }
}
