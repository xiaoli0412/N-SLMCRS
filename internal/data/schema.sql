-- N-SLMCRS 数据库 Schema (SQLite)
-- Phase 1：keys / credentials / models / requests(时序指标) / logs / settings

-- 上游密钥（NVIDIA nvapi-）
CREATE TABLE IF NOT EXISTS upstream_keys (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    key_value       TEXT    NOT NULL UNIQUE,          -- 完整 nvapi-xxx（应用层加密存储）
    key_mask        TEXT    NOT NULL,                 -- 脱敏展示用，如 nvapi-...3Rk8L
    label           TEXT    DEFAULT '',               -- 用户备注/标签
    email           TEXT    DEFAULT '',               -- 注册邮箱（可选）
    rpm_override    INTEGER DEFAULT 0,                -- 0 表示用全局默认，否则覆盖
    enabled         INTEGER DEFAULT 1,                -- 0/1 软启用开关
    status          TEXT    DEFAULT 'active',         -- active|cooling|circuit_open|disabled
    consecutive_fail INTEGER DEFAULT 0,               -- 连续失败计数（熔断用）
    cooling_until   INTEGER DEFAULT 0,                -- 冷却到期 Unix 时间戳（秒）
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

-- 下游凭证（签发给客户端）
CREATE TABLE IF NOT EXISTS downstream_credentials (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    credential      TEXT    NOT NULL UNIQUE,          -- sk-nv-xxxx
    credential_mask TEXT    NOT NULL,
    name            TEXT    DEFAULT '',               -- 凭证名称
    enabled         INTEGER DEFAULT 1,
    rpm_limit       INTEGER DEFAULT 0,                -- 0=不限
    allowed_models  TEXT    DEFAULT '',               -- 逗号分隔模型 id，空=全部
    total_requests  INTEGER DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

-- 模型目录（每 24h 从 /v1/models 同步）
CREATE TABLE IF NOT EXISTS models (
    id              TEXT PRIMARY KEY,                 -- 模型 id，如 meta/llama-3.1-8b-instruct
    object          TEXT    DEFAULT 'model',
    created         INTEGER DEFAULT 0,                -- NVIDIA 返回的 created
    owned_by        TEXT    DEFAULT '',
    root            TEXT    DEFAULT '',
    -- 增强元数据（从模型卡/HuggingFace 补充，Phase 2 完善）
    param_count     TEXT    DEFAULT '',               -- 如 "8B"
    context_length  INTEGER DEFAULT 0,                -- 上下文长度
    capability      TEXT    DEFAULT '',               -- chat|embedding|rerank|reasoning
    description     TEXT    DEFAULT '',
    is_active       INTEGER DEFAULT 1,                -- 0=已失效/下线
    status          TEXT    DEFAULT 'active',         -- active|gone(上游已删除)|disabled(人工下线)
    last_seen_active_at INTEGER DEFAULT 0,            -- 最近一次同步仍存在的时刻（秒）
    synced_at       INTEGER NOT NULL                  -- 最后同步时间
);

-- 模型主动探活结果（每模型最近一次 ping 测试，供模型广场可用度展示）
CREATE TABLE IF NOT EXISTS model_probes (
    model          TEXT PRIMARY KEY,                  -- 模型 id
    ts             INTEGER NOT NULL,                  -- 探活时间戳（秒）
    ok             INTEGER NOT NULL,                  -- 0=失败 1=成功
    http_status    INTEGER DEFAULT 0,                 -- 上游返回的 HTTP 状态码
    latency_ms     INTEGER DEFAULT 0,                 -- 探活端到端延迟
    status         TEXT DEFAULT '',                   -- ok|error|timeout
    error          TEXT DEFAULT ''                    -- 错误简述
);

-- 模型探活历史（每次探活留痕，供模型详情页探活趋势图）
CREATE TABLE IF NOT EXISTS model_probe_history (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    model          TEXT NOT NULL,
    ts             INTEGER NOT NULL,
    ok             INTEGER NOT NULL,
    http_status    INTEGER DEFAULT 0,
    latency_ms     INTEGER DEFAULT 0,
    status         TEXT DEFAULT '',
    error          TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_probe_history_model ON model_probe_history(model, ts);

-- 模型扩展规格（来自 OpenRouter/LiteLLM/HuggingFace 注册表，v0.7 模型广场三级"参数说明"页）
CREATE TABLE IF NOT EXISTS model_specs (
    model            TEXT PRIMARY KEY,             -- 模型 id
    max_tokens       INTEGER DEFAULT 0,
    pricing_in       TEXT DEFAULT '',
    pricing_out      TEXT DEFAULT '',
    license          TEXT DEFAULT '',
    input_modalities TEXT DEFAULT '',               -- 逗号分隔
    release_date     TEXT DEFAULT '',
    card_url         TEXT DEFAULT '',
    architecture     TEXT DEFAULT '',               -- 架构（v0.9：HF 富化，如 LlamaForCausalLM）
    supported_interfaces TEXT DEFAULT '',           -- 支持的推理接口（v0.9：chat/embeddings/rerank，逗号分隔）
    synced_at        INTEGER NOT NULL
);

-- 请求记录（时序指标来源）— 按时间查询为主，建索引
CREATE TABLE IF NOT EXISTS request_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id        TEXT    NOT NULL,                 -- 全链路追踪 ID
    ts              INTEGER NOT NULL,                 -- 请求时间戳（秒）
    downstream_cred TEXT    DEFAULT '',               -- 使用的下游凭证（脱敏）
    upstream_key    TEXT    DEFAULT '',               -- 实际命中的上游 Key（脱敏）
    model           TEXT    NOT NULL,
    protocol        TEXT    DEFAULT 'openai',         -- openai|claude|gemini
    status          TEXT    NOT NULL,                 -- success|error|timeout|rate_limited|circuit
    http_status     INTEGER DEFAULT 0,                -- 上游返回的 HTTP 状态码
    latency_ms      INTEGER DEFAULT 0,                -- 端到端延迟
    prompt_tokens   INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    total_tokens    INTEGER DEFAULT 0,
    error_type      TEXT    DEFAULT '',               -- 错误分类
    error_message   TEXT    DEFAULT '',               -- 错误简述（截断）
    concurrency     INTEGER DEFAULT 1                 -- 本次并发尝试的 Key 数
);

-- 时序指标按 ts 查询为主
CREATE INDEX IF NOT EXISTS idx_request_logs_ts ON request_logs(ts);
CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model, ts);
CREATE INDEX IF NOT EXISTS idx_request_logs_trace ON request_logs(trace_id);
CREATE INDEX IF NOT EXISTS idx_request_logs_status ON request_logs(status, ts);

-- Key 级别的实时健康（滚动统计快照，由后台/请求写入维护）
CREATE TABLE IF NOT EXISTS key_health (
    key_id          INTEGER PRIMARY KEY REFERENCES upstream_keys(id),
    success_count   INTEGER DEFAULT 0,
    error_count     INTEGER DEFAULT 0,
    window_start    INTEGER DEFAULT 0,                -- 当前统计窗口起始
    last_success_ts INTEGER DEFAULT 0,
    last_error_ts   INTEGER DEFAULT 0,
    avg_latency_ms  INTEGER DEFAULT 0
);

-- 结构化日志（日志中心）
CREATE TABLE IF NOT EXISTS app_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              INTEGER NOT NULL,
    trace_id        TEXT    DEFAULT '',
    level           TEXT    NOT NULL,                 -- debug|info|warn|error
    source          TEXT    DEFAULT '',               -- entry|scheduler|ratelimit|upstream|data
    message         TEXT    NOT NULL,
    context         TEXT    DEFAULT ''                -- JSON 附加上下文
);
CREATE INDEX IF NOT EXISTS idx_app_logs_ts ON app_logs(ts);
CREATE INDEX IF NOT EXISTS idx_app_logs_trace ON app_logs(trace_id);
CREATE INDEX IF NOT EXISTS idx_app_logs_level ON app_logs(level, ts);

-- 模型级熔断状态（v0.9：按模型熔断 + 永久熔断，与按 Key 熔断互补）
CREATE TABLE IF NOT EXISTS model_circuit (
    model              TEXT PRIMARY KEY REFERENCES models(id),
    state              TEXT DEFAULT 'closed',   -- closed|open|half_open|permanent
    open_until         INTEGER DEFAULT 0,       -- 冷却到期 Unix 秒；open 态有效
    consecutive_fail   INTEGER DEFAULT 0,       -- 被动连续失败计数（dispatch 失败累加）
    success_rate_pct   INTEGER DEFAULT 100,     -- 最近一次健康扫描成功率
    bad_sweep_count    INTEGER DEFAULT 0,       -- 连续低于地板的扫描次数（永久熔断判定）
    permanent          INTEGER DEFAULT 0,       -- 1=永久熔断（手动或自动，需手动复位）
    last_sweep_at      INTEGER DEFAULT 0,
    updated_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_model_circuit_state ON model_circuit(state);

-- 集成渠道（v0.10：new-api / sapi 等下游中转网关对接，模型同步 + 计费回采）
CREATE TABLE IF NOT EXISTS channels (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    type            TEXT    NOT NULL,                 -- newapi | sapi
    base_url        TEXT    DEFAULT '',               -- 渠道接入本网关的地址（如 http://host:8787/v1）
    api_key_mask    TEXT    DEFAULT '',               -- 脱敏展示用
    api_key_hash    TEXT    DEFAULT '',               -- bcrypt 哈希（渠道密钥，不下发明文）
    enabled         INTEGER DEFAULT 1,
    last_sync_at    INTEGER DEFAULT 0,
    total_requests  INTEGER DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

-- 动态设置（管理 API 可改）
CREATE TABLE IF NOT EXISTS settings (
    key             TEXT PRIMARY KEY,
    value           TEXT NOT NULL,
    updated_at      INTEGER NOT NULL
);
