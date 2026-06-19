# N-SLMCRS 设计文档

> **N-SLMCRS** — NVIDIA Studio LLM 并发调度网关
> 版本：v0.1（Phase 1 骨架） ｜ 日期：2026-06-19 ｜ 状态：已批准，实现中

## 1. 项目定位

针对 NVIDIA AI Studio（build.nvidia.com / integrate API）的**高并发调度网关**。

- 聚合多个上游 `nvapi-` 密钥，把多个账号的配额（每 Key 40 RPM）汇聚成更高吞吐池。
- 对热门模型做 **N 路并发·先到先得**：同一请求并发分发到 N 个 Key，哪个最先返回就用哪个结果，其余取消。
- 严格保证不超过「每 Key 40 RPM」官方限额（令牌桶 + 滑动窗口）。
- 同时兼容 **OpenAI / Claude / Gemini** 三种客户端协议。
- 内置**运维面板**（玻璃拟态大厂风）与可切换的**智能调度引擎**。
- Docker 与裸机（单二进制 + systemd）双形态部署。

## 2. 目标与非目标

**目标**
- 多 Key 聚合，吞吐线性扩展（10 Key ≈ 400 RPM 池）。
- 热门模型高成功率：N 路并发抵消单 Key 429 / 抖动。
- 不超 NVIDIA 限额：每 Key 令牌桶硬约束。
- 协议全覆盖：OpenAI / Claude / Gemini 客户端零改接入。
- 全链路可观测：成功率 / 请求量 / Token / 延迟 / Trace ID。
- 智能调度可接管：三引擎 + 三档模式。

**非目标（Phase 1）**
- 不自建模型推理（纯转发网关）。
- 不做计费/多租户组织（个人/小团队自用为主）。
- 不做高可用集群（单节点，SQLite 足够）。

## 3. 技术栈

| 层 | 选型 | 理由 |
|----|------|------|
| 后端语言 | Go 1.22+ | 高并发原生、单二进制、Portkey 等同款 |
| Web 框架 | Gin | 成熟、SSE 友好、生态丰富 |
| 数据存储 | SQLite（默认）+ 可选 Postgres | 零依赖启动，规模化可升级 |
| 时序指标 | 内嵌轻量时序（基于 SQLite） | 高频成功率/延迟/Token |
| 前端 | React 18 + TypeScript + Vite | 组件生态、类型安全、构建快 |
| UI | Tailwind + Radix + Framer Motion | 原子样式 + 无障碍 + 动效引擎 |
| 国际化 | i18next | 中/英切换 |
| 主题 | 明暗双主题 | 暗色为主基调（NVIDIA 黑） |
| 部署 | Docker（多阶段 distroless）+ 二进制 + systemd | 双形态 |

## 4. NVIDIA API 契约（已联网调研确认）

### 4.1 Base URL 与域名
| 能力 | Base URL | 端点 |
|------|----------|------|
| 对话/补全 | `https://integrate.api.nvidia.com/v1` | `/chat/completions`, `/completions` |
| 模型列表 | `https://integrate.api.nvidia.com/v1` | `/models` |
| 嵌入 | `https://ai.api.nvidia.com/v1` ⚠️ | `/embeddings/{model}` |
| 重排序 | `https://ai.api.nvidia.com/v1` ⚠️ | `/ranking/{model}` 或 `/retrieval/{model}` |

> **关键**：嵌入/重排序在**不同域名**（`ai.api` 而非 `integrate.api`），网关需多上游路由表。

### 4.2 认证
- 头格式：`Authorization: Bearer nvapi-xxxxxxxx`
- 标准 Bearer，非自定义头。

### 4.3 关键约束与陷阱
- 对话/补全/模型列表**完全 OpenAI 兼容**，可复用 OpenAI 类型。
- `max_tokens` 对 Llama 类模型**必填**，省略会失败 → 网关兜底默认值（如 1024）。
- 推理模型（Qwen/Nemotron）响应多 `reasoning_content` 字段 → 透传。
- 401/403 + `/models` 能用 = **该 Key 无此模型权限**（非 key 失效），区分报错。
- 429 响应体**可能为空**，必须防御性处理，不能假设有 JSON。
- 限流信号靠 `X-RateLimit-Remaining` / `X-RateLimit-Reset`，**不依赖 Retry-After**。
- 默认 40 RPM（per-model，免费层）。无单独 TPM / 并发文档限制。

### 4.4 `/v1/models` 响应
```json
{
  "object": "list",
  "data": [
    {"id": "meta/llama-3.1-8b-instruct", "object": "model", "created": 1724796510, "owned_by": "system", "root": "meta/llama-3.1-8b-instruct"}
  ]
}
```
> **不含**上下文长度 / 参数量 / 能力类型。模型详情需从 build.nvidia.com 模型卡 + HuggingFace 补充。

### 4.5 流式 SSE 格式（OpenAI 兼容）
- `Content-Type: text/event-stream`
- 每块前缀 `data: `，块间 `\n\n`
- 首块 `delta: {"role":"assistant"}`，后续 `delta.content` 分片
- 终止事件：`data: [DONE]`（字面量，非 JSON）
- `stream_options: {include_usage: true}` → `[DONE]` 前出现 usage 块

## 5. 三协议映射（网关翻译核心）

内部统一规范为 **OpenAI 格式**转发，响应再逆向翻译回客户端协议。

| 维度 | OpenAI | Claude | Gemini |
|------|--------|--------|--------|
| 端点 | `/v1/chat/completions` | `/v1/messages` | `/v1beta/models/{m}:generateContent` |
| 消息 | `messages[{role,content}]` | `messages[{role,content}]` | `contents[{role,parts[{text}]}]` |
| 系统提示 | messages 首条 system | 顶层 `system` 字段 | `systemInstruction` |
| 最大 token | `max_tokens` | `max_tokens`（必填） | `generationConfig.maxOutputTokens` |
| 温度 | `temperature` | `temperature` | `generationConfig.temperature` |
| 认证 | Bearer | `x-api-key` + `anthropic-version` | `?key=` 或 `x-goog-api-key` |
| 工具调用 | `tools/tool_calls` | `tools`（不同结构） | `tools[].functionDeclarations` |
| 流式 | `stream:true` + SSE | `stream:true` + SSE（事件类型） | `:streamGenerateContent` |

## 6. 后端五层架构

```
┌─ 入口层 Entry ──────────────────────────────┐
│ 多协议适配(OpenAI/Claude/Gemini→内部OpenAI规范) │
│ 下游凭证认证 · 路由分发 · CORS · Trace ID 注入   │
└──────────────────┬─────────────────────────┘
                   ▼
┌─ 调度层 Scheduler ──────────────────────────┐
│ N 路并发 · 先到先得 · 加权选 Key · 熔断器      │
│ 失败转移 · 模型降级 · 超时控制                  │
└──────────────────┬─────────────────────────┘
                   ▼
┌─ 限流层 Rate Limit ─────────────────────────┐
│ 令牌桶(每 Key 40 RPM) · 多维 RPM/TPM/并发     │
│ 滑动窗口统计 · 配额预算分配                    │
└──────────────────┬─────────────────────────┘
                   ▼
┌─ 上游层 Upstream ───────────────────────────┐
│ 多域名路由(integrate.api / ai.api) · 连接池    │
│ 流式转发 · 错误归一化 · 退避重试                │
└──────────────────┬─────────────────────────┘
                   ▼
┌─ 数据层 Data ───────────────────────────────┐
│ SQLite · 时序指标 · 配置 · 日志聚合 · 模型缓存  │
└─────────────────────────────────────────────┘
```

每层职责单一、接口清晰、可独立测试。上层依赖下层接口（非具体实现）。

## 7. 核心算法

### 7.1 令牌桶（每 Key）
- 每 Key 一个桶，容量 = RPM（默认 40），每 1.5s（60s/40）回填 1 个 token。
- 允许短突发（桶满可瞬间用完）。
- 基于 `X-RateLimit-Remaining` 头校准真实余量（减少保守浪费）。

### 7.2 N 路并发先到先得
- 客户端请求进入 → 调度器选 N 个健康且有余量的 Key。
- 非流式：N 个上游请求并行，**第一个成功返回**即作为结果，其余通过 context 取消。
- 流式：**首个返回首个 SSE chunk 的**请求锁定，其余取消，已开始流式的请求持续转发到客户端。
- N 可全局配置，也可按模型覆盖（Auto-Pilot 动态调节）。

### 7.3 加权选 Key
- 按 Key 健康分（成功率 EWMA + 延迟）加权随机选择。
- 冷却 / 熔断状态的 Key 不参与选择。

### 7.4 熔断器（基础版）
- 连续 N 次失败（含 429/5xx/超时）→ 开启熔断，Key 进入冷却。
- 冷却时长指数退避（30s → 60s → 120s）。
- 冷却结束 → 半开探测：放 1 个请求试探，成功则恢复，失败则继续冷却。

### 7.5 模型失效检测
- 每 24h 同步 `/v1/models`，与本地缓存比对。
- 请求到的模型不在最新列表 → 返回结构化错误 + 推荐当前成功率最高替代。
- 推荐逻辑：同能力类型中按近 1h 成功率排序取第一。

## 8. 智能调度 Auto-Pilot（三引擎可切换，Phase 3）

| 引擎 | 机制 | 延迟 | 适用 |
|------|------|------|------|
| A 自适应算法 | PID + EWMA + 滑动窗口 | 毫秒 | 默认，实时调度 |
| B 轻量预测 | Holt-Winters 时序预测 | 毫秒 | 前瞻抗突发 |
| C LLM 决策 | 指标喂小 LLM 输出决策 | 秒级 | 最「智能」，可选带降级 |

三档模式：Manual（人工配）/ Assisted（建议需确认）/ Full-Auto（完全接管，带人工复核开关防误摘好 Key）。

6 大自动化能力：并发自调节、密钥熔断、配额预算、模型降级、学习引擎、异常自愈。

## 9. 前端 8 模块

| 模块 | 中文 | 职责 |
|------|------|------|
| Overview | 概览 | 全局健康度、KPI、告警入口 |
| Operations | 运维监控 | 成功率/请求量/Token/延迟曲线、Key 健康、告警、压测 |
| Logs | 日志中心 | 结构化日志、Trace ID 检索 |
| Model Catalog | 模型目录 | 24h 同步、参数展示、失效检测+替代推荐 |
| Upstream Keys | 上游密钥 | NVIDIA nvapi- Key 精细配置 |
| Distribution | 接入分发 | 签发下游凭证 + new-api/OCTOPUS/Webhook 钩子 |
| Auto-Pilot | 智能调度 | 三引擎切换、三档模式、策略可视化 |
| Settings | 系统设置 | 端口/认证/上游 URL/同步周期/协议/日志/持久化 |

设计语言：玻璃拟态（半透明 + backdrop-blur + 内发光）、NVIDIA 黑底 + #76b900 绿、单指标聚焦 KPI、⌘K 命令面板、脉冲状态点、hover 上浮、SVG 折线图发光描边。中英切换（专有名词保留英文）+ 明暗双主题。

## 10. 失效模型处理（官方话术）

```json
HTTP 404
{
  "error": {
    "message": "该模型已下线或当前不可用。建议切换至当前成功率最高的可用模型：deepseek-ai/deepseek-v4-flash（成功率 98.4%）。",
    "type": "model_unavailable",
    "suggested_model": "deepseek-ai/deepseek-v4-flash"
  }
}
```

## 11. 分阶段交付

### Phase 1（本轮）— 核心骨架
1. Go 模块 + 目录结构 + 配置加载
2. 上游层 NVIDIA 客户端（chat/completions/models）+ SSE
3. 限流层令牌桶 + 滑动窗口
4. 调度层 N 路并发 + 先到先得 + 加权选 Key + 基础熔断
5. 入口层 OpenAI 协议端点 + Trace ID + 下游凭证认证
6. 数据层 SQLite schema + 时序指标
7. 模型目录 24h 同步 + 失效检测
8. 前端概览 + 上游密钥 + 接入分发基础页
9. Dockerfile + 二进制 + systemd

**验收**：配 1 Key → OpenAI SDK 调网关 → 5 路并发先到先得 → 面板见成功率/RPM/Token。

### Phase 2 — 协议完整 + 面板深化
Claude/Gemini 适配、统一翻译层、模型元数据增强、运维监控页、日志中心、响应缓存、健康探针、Webhook、⌘K、OpenAPI 文档。

### Phase 3 — 智能调度 + 集成 + 打磨
Auto-Pilot 三引擎、new-api/OCTOPUS/Webhook 钩子、前端动效光感打磨、明暗/中英全覆盖。

## 12. 安全要点

- 测试 Key 存 `.env`（已 gitignore），测试后到 build.nvidia.com 撤销重发。
- 下游凭证独立签发（`sk-nv-` 前缀），与上游 `nvapi-` 隔离。
- 配置中敏感字段加密存储。
- 管理 API（增删 Key）独立鉴权，与转发 API 分离。

## 13. 仓库结构

```
N-SLMCRS/
├── cmd/gateway/           # main 入口
├── internal/
│   ├── entry/             # 入口层
│   ├── scheduler/         # 调度层
│   ├── ratelimit/         # 限流层
│   ├── upstream/          # 上游层
│   ├── data/              # 数据层
│   ├── autopilot/         # 智能调度（Phase 3）
│   ├── config/            # 配置
│   └── modelmeta/         # 模型元数据
├── web/                   # React 前端
├── docker/                # Dockerfile + compose
├── deploy/                # systemd + 安装脚本
├── docs/specs/            # 设计文档
└── scripts/               # 同步/压测脚本
```
