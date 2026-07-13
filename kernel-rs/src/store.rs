//! 持久化层（v0.12）：熔断器态 + 子决策审计，落到自有 SQLite（/data/kernel.db）。
//!
//! 设计要点：
//!   - 不与 Go 的 nslmcrs.db 互写。Rust 写本库；Go 侧 upstream_keys/model_circuit
//!     由 /reserve、/report 响应回显单路径写入（kernel 在线时跳过 Go 自写）。
//!   - 读走内存镜像（state.rs 的 key_brk），DB 仅在写变更时同步落库 + 启动载入。
//!   - sub_decisions 为持久化子决策审计：每次 half_open 提升、熔断开/闭、桶校准
//!     均追加一行，可按 trace_id 检索，满足「持久化子决策」可追溯要求。

use rusqlite::{params, Connection, Result};

use crate::state::KeyBreaker;

const SCHEMA: &str = "
CREATE TABLE IF NOT EXISTS key_breakers(
    key_id           INTEGER PRIMARY KEY,
    status           TEXT    NOT NULL,
    consecutive_fail INTEGER NOT NULL,
    cooling_until    INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sub_decisions(
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id TEXT,
    ts       INTEGER NOT NULL,
    call     TEXT    NOT NULL,    -- reserve | report
    kind     TEXT    NOT NULL,    -- half_open_promote | circuit_open | circuit_close | calibrate | ...
    key_id   INTEGER,
    model    TEXT,
    before   TEXT,
    after    TEXT
);
CREATE INDEX IF NOT EXISTS idx_sub_dec_trace ON sub_decisions(trace_id);
CREATE INDEX IF NOT EXISTS idx_sub_dec_ts ON sub_decisions(ts);
-- v0.14：策略引擎元数据（active_strategy id 等）
CREATE TABLE IF NOT EXISTS meta(
    k TEXT PRIMARY KEY,
    v TEXT NOT NULL
);
";

/// 打开/创建库并建表。
pub fn open(path: &str) -> Result<Connection> {
    let conn = Connection::open(path)?;
    conn.execute_batch("PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;")?;
    conn.execute_batch(SCHEMA)?;
    Ok(conn)
}

/// 对已打开的连接（如内存库 ":memory:"）建表，测试用。
#[allow(dead_code)]
pub fn open_schema_only(conn: &Connection) -> Result<()> {
    conn.execute_batch(SCHEMA)?;
    Ok(())
}

/// 启动载入：把全部 key_breakers 灌入内存镜像。桶/窗按设计不持久化（重启即空）。
pub fn load_all(conn: &Connection) -> Result<Vec<(i64, KeyBreaker)>> {
    let mut stmt =
        conn.prepare("SELECT key_id, status, consecutive_fail, cooling_until FROM key_breakers")?;
    let rows = stmt.query_map([], |r| {
        Ok((
            r.get::<_, i64>(0)?,
            KeyBreaker {
                status: r.get::<_, String>(1)?,
                consecutive_fail: r.get::<_, i64>(2)?,
                cooling_until: r.get::<_, i64>(3)?,
            },
        ))
    })?;
    let mut out = Vec::new();
    for row in rows {
        out.push(row?);
    }
    Ok(out)
}

/// 写/更新按 Key 熔断器。
pub fn upsert_key_breaker(conn: &Connection, kb: &KeyBreaker, key_id: i64, now: i64) -> Result<()> {
    conn.execute(
        "INSERT INTO key_breakers(key_id,status,consecutive_fail,cooling_until,updated_at)
         VALUES(?,?,?,?,?)
         ON CONFLICT(key_id) DO UPDATE SET
           status=excluded.status, consecutive_fail=excluded.consecutive_fail,
           cooling_until=excluded.cooling_until, updated_at=excluded.updated_at",
        params![
            key_id,
            kb.status,
            kb.consecutive_fail,
            kb.cooling_until,
            now
        ],
    )?;
    Ok(())
}

/// 追加一条子决策审计。
#[allow(clippy::too_many_arguments)]
pub fn append_sub_decision(
    conn: &Connection,
    trace_id: &str,
    now: i64,
    call: &str,
    kind: &str,
    key_id: i64,
    model: &str,
    before: &str,
    after: &str,
) -> Result<()> {
    conn.execute(
        "INSERT INTO sub_decisions(trace_id,ts,call,kind,key_id,model,before,after)
         VALUES(?,?,?,?,?,?,?,?)",
        params![trace_id, now, call, kind, key_id, model, before, after],
    )?;
    Ok(())
}

// ─── v0.14 策略元数据 ────────────────────────────────────────────────

/// 读 meta 键值（如 active_strategy）。不存在返回 None。
pub fn get_meta(conn: &Connection, key: &str) -> Result<Option<String>> {
    let mut stmt = conn.prepare("SELECT v FROM meta WHERE k=?")?;
    let mut rows = stmt.query_map(params![key], |r| r.get::<_, String>(0))?;
    if let Some(row) = rows.next() {
        Ok(Some(row?))
    } else {
        Ok(None)
    }
}

/// 写 meta 键值（upsert）。
pub fn set_meta(conn: &Connection, key: &str, value: &str) -> Result<()> {
    conn.execute(
        "INSERT INTO meta(k,v) VALUES(?,?)
         ON CONFLICT(k) DO UPDATE SET v=excluded.v",
        params![key, value],
    )?;
    Ok(())
}
