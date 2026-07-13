//! 策略引擎（v0.14）：命名策略 = 选择算法 + 扇出 + RPM 头寸 + 熔断参数的相干捆绑。
//!
//! 设计哲学：最优调度策略依赖两个上下文轴——密钥数量 M 与流量形态（突发/延迟敏感 vs
//! 稳态/吞吐敏感）。当前系统把四个旋钮焊死成一条管线（加权随机 + N 路先到先得 +
//! 阈值熔断 + 满容量桶），本模块把它拆成可切换的命名预设，每预设从其场景的矛盾
//! 推导参数（"内化深化底部逻辑"），而非拍脑袋。
//!
//! 权威归属：活跃策略由 Rust 内核持有（state::AppState.active），/reserve 按其选择
//! 算法派发；Go 仅镜像 + 转发。降级路径镜像同一算法保持一致。
//!
//! 五策略 × 场景映射：
//!   - Guardian（保守护航）M≤3：稀缺资源下"可用性">"延迟/吞吐"，StrictPriority +
//!     扇出 1 + 宽容熔断 + 80% 头寸，不死不烧最省。
//!   - Balanced（均衡调度）M≥4 默认：加权随机分散负载又偏向健康，标准熔断，满容量。
//!   - Velocity（极速竞速）M≥8：聚合 RPM>>需求，延迟主导，LeastInflight 降 P99 +
//!     高扇出先到先得。
//!   - Fairshare（均轮分发）批处理/稳态：RoundRobin 最大化聚合 RPM 利用率 + 扇出 1
//!     零浪费。
//!   - Adaptive（智能自适应）：Auto-Pilot 30s 控制环按实时工况调并发与逐键权重。

use serde::Serialize;

// ─── 选择算法 ─────────────────────────────────────────────────────────

/// 密钥选择算法。决定 /reserve 如何从可行候选中排序与取前 N 个。
#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum SelectionAlgo {
    /// 健康加权随机排列（成功率 × 0.5^连续失败 × boost）。现有默认，分散负载又偏向健康。
    WeightedRandom,
    /// 严格轮转：按 key_id 稳定排序后按 rr_counter 轮转起点，均匀分发，最大化聚合 RPM 利用率。
    RoundRobin,
    /// 最少在途优先：优先发给当前排队最少的密钥，降低 P99 排队延迟（带健康加权兜底）。
    LeastInflight,
    /// 严格优先：按评分降序取前 N，永远先打最健康的，失败才回退（稀缺密钥不赌博）。
    StrictPriority,
}

impl SelectionAlgo {
    /// 人类可读的算法标识（UI/审计用）。
    #[allow(dead_code)]
    pub fn label(&self) -> &'static str {
        match self {
            SelectionAlgo::WeightedRandom => "weighted_random",
            SelectionAlgo::RoundRobin => "round_robin",
            SelectionAlgo::LeastInflight => "least_inflight",
            SelectionAlgo::StrictPriority => "strict_priority",
        }
    }
}

// ─── 策略 ─────────────────────────────────────────────────────────────

/// 一个命名策略 = 四旋钮的相干捆绑。全部字段为 &'static str，可 const 构造、Copy。
#[derive(Clone, Copy, Serialize)]
pub struct Strategy {
    pub id: &'static str,
    pub icon: &'static str,
    pub name_zh: &'static str,
    pub name_en: &'static str,
    pub character_zh: &'static str,
    pub character_en: &'static str,
    /// 选择算法（/reserve 按此派发）。
    pub selection: SelectionAlgo,
    /// 扇出覆盖：>0 覆盖请求方 concurrency；=0 用请求方（调度器/Auto-Pilot）值。
    pub fanout: i64,
    /// 按 Key 熔断阈值（推荐值；admin 切换策略时同步到 Settings，/report 经 req 字段落地）。
    pub breaker_threshold: i64,
    /// 熔断基础冷却秒（推荐值，同上）。
    pub breaker_cooldown_sec: i64,
    /// RPM 头寸：桶准入地板 = capacity × (1 - headroom)。1.0=骑满，0.8=留 20% 抗突发。
    pub rpm_headroom: f64,
    /// 推荐密钥数下界（0=无下界）。
    pub min_keys: i64,
    /// 推荐密钥数上界（0=无上界）。
    pub max_keys: i64,
    pub scenario_zh: &'static str,
    pub scenario_en: &'static str,
}

/// 全部预设（顺序即 UI 展示顺序）。
pub const PRESETS: &[Strategy] = &[
    // 1. 保守护航 — 稀缺密钥（M≤3 或单付费密钥）
    Strategy {
        id: "guardian",
        icon: "🛡️",
        name_zh: "保守护航",
        name_en: "Guardian",
        character_zh: "不死、不烧、最省",
        character_en: "Never die, never burn, most frugal",
        selection: SelectionAlgo::StrictPriority,
        fanout: 1,
        breaker_threshold: 20,
        breaker_cooldown_sec: 60,
        rpm_headroom: 0.80,
        min_keys: 0,
        max_keys: 3,
        scenario_zh: "密钥稀缺（≤3）或单付费密钥。StrictPriority 永远先打最健康的、失败才回退；扇出 1 消除并发 loser 的 RPM 浪费；宽容熔断（阈值 20）避免自残掉 33–50% 容量；80% 头寸留突发余量防 429 烧键。",
        scenario_en: "Scarce keys (≤3) or single paid key. StrictPriority always tries the healthiest first, falling back only on failure; fan-out 1 eliminates concurrent-loser RPM waste; lenient breaker (threshold 20) avoids self-harm; 80% headroom reserves burst buffer against 429s.",
    },
    // 2. 均衡调度 — 默认通用（M≥4）
    Strategy {
        id: "balanced",
        icon: "⚖️",
        name_zh: "均衡调度",
        name_en: "Balanced",
        character_zh: "全面均衡",
        character_en: "Fully balanced",
        selection: SelectionAlgo::WeightedRandom,
        fanout: 0,
        breaker_threshold: 5,
        breaker_cooldown_sec: 30,
        rpm_headroom: 1.0,
        min_keys: 4,
        max_keys: 7,
        scenario_zh: "默认通用。密钥够多能吸收随机性；加权随机分散负载又偏向健康；标准熔断切掉真坏的键；满容量骑 RPM。",
        scenario_en: "Default general-purpose. Enough keys to absorb randomness; weighted random spreads load while favoring healthy keys; standard breaker cuts genuinely broken keys; full-capacity RPM.",
    },
    // 3. 极速竞速 — 延迟敏感（M≥8，chat）
    Strategy {
        id: "velocity",
        icon: "⚡",
        name_zh: "极速竞速",
        name_en: "Velocity",
        character_zh: "低延迟、高并发",
        character_en: "Low latency, high concurrency",
        selection: SelectionAlgo::LeastInflight,
        fanout: 0,
        breaker_threshold: 5,
        breaker_cooldown_sec: 30,
        rpm_headroom: 1.0,
        min_keys: 8,
        max_keys: 0,
        scenario_zh: "密钥≥8，聚合 RPM 远超需求，延迟主导。LeastInflight 把请求发给最闲的密钥降 P99；高扇出让先到先得快速返回；可承受 loser 的 RPM 浪费。",
        scenario_en: "Keys≥8, aggregate RPM far exceeds demand, latency dominates. LeastInflight sends to the least-queued key to cut P99; high fan-out returns first-success fast; can afford loser RPM waste.",
    },
    // 4. 均轮分发 — 吞吐优先（批处理/稳态）
    Strategy {
        id: "fairshare",
        icon: "🎯",
        name_zh: "均轮分发",
        name_en: "Fairshare",
        character_zh: "雨露均沾、最大化总吞吐",
        character_en: "Even spread, max total throughput",
        selection: SelectionAlgo::RoundRobin,
        fanout: 1,
        breaker_threshold: 5,
        breaker_cooldown_sec: 30,
        rpm_headroom: 1.0,
        min_keys: 2,
        max_keys: 0,
        scenario_zh: "批处理/稳态、吞吐优先。RoundRobin 均匀轮转使没有密钥提前触限而其他空闲，最大化聚合 RPM 利用率；扇出 1 零浪费；满容量骑满。",
        scenario_en: "Batch/steady, throughput-first. RoundRobin even rotation prevents any key hitting its limit early while others idle, maximizing aggregate RPM utilization; fan-out 1 zero waste; ride full capacity.",
    },
    // 5. 智能自适应 — AI 自动调优
    Strategy {
        id: "adaptive",
        icon: "🤖",
        name_zh: "智能自适应",
        name_en: "Adaptive",
        character_zh: "AI 自动调优",
        character_en: "AI auto-tuning",
        selection: SelectionAlgo::WeightedRandom,
        fanout: 0,
        breaker_threshold: 5,
        breaker_cooldown_sec: 30,
        rpm_headroom: 1.0,
        min_keys: 0,
        max_keys: 0,
        scenario_zh: "Auto-Pilot 30s 控制环按实时工况调并发档位 + 逐键权重乘子（weight_boost）。流量多变、不想手调时最优——加权随机为底，AI 注入权重。",
        scenario_en: "Auto-Pilot 30s control loop tunes concurrency tier + per-key weight_boost to live conditions. Best for variable traffic when you don't want to hand-tune—weighted random base with AI-injected weights.",
    },
];

/// 默认策略 id。
pub const DEFAULT_ID: &str = "balanced";

/// 按 id 查预设。
pub fn by_id(id: &str) -> Option<&'static Strategy> {
    PRESETS.iter().find(|s| s.id == id)
}

/// 默认策略（balanced）。
pub fn default_strategy() -> &'static Strategy {
    by_id(DEFAULT_ID).expect("balanced preset exists")
}

/// 按密钥数推荐策略 id（UI"推荐"徽章用）。
/// M≤3→guardian，4–7→balanced，≥8→velocity。
pub fn recommend(key_count: usize) -> &'static str {
    match key_count {
        0..=3 => "guardian",
        4..=7 => "balanced",
        _ => "velocity",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn presets_have_unique_ids() {
        let mut ids: Vec<&str> = PRESETS.iter().map(|s| s.id).collect();
        ids.sort();
        let n = ids.len();
        ids.dedup();
        assert_eq!(ids.len(), n, "策略 id 重复");
    }

    #[test]
    fn by_id_resolves_all_presets() {
        for s in PRESETS {
            assert!(by_id(s.id).is_some(), "{} 未找到", s.id);
            assert_eq!(by_id(s.id).unwrap().id, s.id);
        }
        assert!(by_id("nonexistent").is_none());
    }

    #[test]
    fn default_is_balanced() {
        assert_eq!(default_strategy().id, "balanced");
        assert_eq!(DEFAULT_ID, "balanced");
    }

    #[test]
    fn recommend_by_key_count() {
        assert_eq!(recommend(0), "guardian");
        assert_eq!(recommend(1), "guardian");
        assert_eq!(recommend(3), "guardian");
        assert_eq!(recommend(4), "balanced");
        assert_eq!(recommend(7), "balanced");
        assert_eq!(recommend(8), "velocity");
        assert_eq!(recommend(100), "velocity");
    }

    #[test]
    fn guardian_is_conservative() {
        let g = by_id("guardian").unwrap();
        assert_eq!(g.selection, SelectionAlgo::StrictPriority);
        assert_eq!(g.fanout, 1, "扇出 1 消除 loser 浪费");
        assert!(g.breaker_threshold >= 15, "宽容熔断");
        assert!(g.rpm_headroom < 1.0, "留头寸");
    }

    #[test]
    fn fairshare_is_round_robin_low_fanout() {
        let f = by_id("fairshare").unwrap();
        assert_eq!(f.selection, SelectionAlgo::RoundRobin);
        assert_eq!(f.fanout, 1, "零浪费");
    }

    #[test]
    fn velocity_is_least_inflight() {
        let v = by_id("velocity").unwrap();
        assert_eq!(v.selection, SelectionAlgo::LeastInflight);
    }

    #[test]
    fn balanced_is_weighted_random_full() {
        let b = by_id("balanced").unwrap();
        assert_eq!(b.selection, SelectionAlgo::WeightedRandom);
        assert_eq!(b.fanout, 0, "用调用方并发");
        assert!((b.rpm_headroom - 1.0).abs() < 1e-9, "满容量");
    }

    #[test]
    fn selection_algo_label_snake() {
        assert_eq!(SelectionAlgo::WeightedRandom.label(), "weighted_random");
        assert_eq!(SelectionAlgo::RoundRobin.label(), "round_robin");
        assert_eq!(SelectionAlgo::LeastInflight.label(), "least_inflight");
        assert_eq!(SelectionAlgo::StrictPriority.label(), "strict_priority");
    }
}
