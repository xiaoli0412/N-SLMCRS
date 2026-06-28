//! 测试支持：构造带临时 SQLite 的 SharedState（仅 #[cfg(test)]）。

use crate::compute::Engine;
use crate::state::{AppState, SharedState};
use std::sync::Arc;

pub fn state() -> SharedState {
    // 临时文件库：rusqlite 内存库（":memory:"）即可，无需磁盘。
    let conn = rusqlite::Connection::open_in_memory().unwrap();
    crate::store::open_schema_only(&conn).unwrap();
    Arc::new(AppState::new(Engine::new(), conn))
}
