# N-SLMCRS Gateway

> NVIDIA Studio LLM **Concurrent Dispatch** Gateway — 聚合多账号 `nvapi-` 密钥、对热模型发起 N 路并发请求并**先到先得**，内置 Auto-Pilot 智能调度与模型广场可用度监控，单二进制交付管理面板。

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25-76b900?logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-18-61dafb?logo=react&logoColor=white">
  <img alt="SQLite" src="https://img.shields.io/badge/SQLite-pure--Go-003b57?logo=sqlite&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/status-v0.6.0-76b900">
  <a href="https://github.com/xiaoli0412/N-SLMCRS/releases"><img alt="Release" src="https://img.shields.io/badge/release-v0.6.0-76b900?logo=github&logoColor=white"></a>
</p>

> 📦 **最新发布 [v0.6.0](https://github.com/xiaoli0412/N-SLMCRS/releases/tag/v0.6.0)** — UI 换风 shadcn · 模型广场可用度 · Auto-Pilot agent 化 · 真实环境验证。完整版本历程见 [Releases](https://github.com/xiaoli0412/N-SLMCRS/releases)。

---

## 🆕 v0.6.0 亮点

- 🎨 **shadcn 风扁平暗色主题**：CVA `Button`/`Badge` 原语 + Tailwind `surface` 色板，全站 8 模块统一观感，Vite 实时构建嵌入 Go 二进制
- 📊 **模型广场双路可用度（仿 new-api）**：每模型被动聚合评分（availability_score / avg_latency / 错误数）+ 主动探活（probe_ok / 延迟），单模型 Test 与一键 Probe-All
- 🧠 **Auto-Pilot agent 化**：LLM 引擎 ReAct 循环（think→act→observe）+ function-calling 调度工具 + 可调试推理轨迹 + `LLMBackendMode`（stub/gateway）徽标
- 🔌 **熔断半开探测自愈** + 流式响应健康记录补全
- 🐳 **Docker 部署就绪**：`ARG VERSION` 经 ldflags 注入（`/health` 与 `-version` 一致），支持 `latest` + `v0.6.0` 双标签构建

---

## ✨ 特性

- **密钥聚合 + 并发先到先得**：多个 NVIDIA Studio 账号密钥池化，热模型同时发起 N 个请求，返回最先成功的结果。
- **严格不超官方限流**：每 Key 独立令牌桶（默认 40 RPM），并根据上游 `X-RateLimit-Remaining` 实时校准，杜绝浪费。
- **熔断 + 半开探测 + 指数退避**：连续失败自动熔断，冷却时长 30s→60s→120s 指数增长封顶 10 分钟；半开态主动放行探测请求自愈。
- **OpenAI / Claude / Gemini 多协议兼容**：`/v1/chat/completions`、`/v1/completions`、`/v1/models`、`/v1/messages`、`/v1beta/...:generateContent`、`/v1/embeddings`、`/v1/ranking`。
- **流式 SSE 透传 + 流式健康记录**：`stream:true` 原生支持，首字节即锁定获胜上游；流式响应同样写入 request_logs 被动统计。
- **模型广场 + 双路可用度（仿 new-api）**：24h 自动同步 `/v1/models`；每模型聚合被动可用度评分（availability_score / avg_latency / 错误数）+ 主动探活（probe_ok / 延迟），支持单模型 Test 与一键 Probe-All。
- **Auto-Pilot 智能调度**：三模式（手动 / 辅助 / 全自动）× 三引擎（自适应 PID·EWMA / 轻量预测 Holt-Winters / LLM Agent）。LLM 引擎已 **agent 化**——ReAct 循环（think→act→observe）调用调度工具，产出可调试推理轨迹。
- **鉴权强化 + 首登强制改密**：默认 `ADMIN` 令牌首登强制修改并写入 bcrypt 哈希；改密前管理 API 全锁定。
- **运维监控面板（shadcn 风）**：Vite + Tailwind 扁平暗色主题，实时成功率 / 请求量 / Token / 每 Key 健康度 / Auto-Pilot 快照 / 推理轨迹全维度图表。
- **下游凭证签发**：向客户端分发 `sk-nv-xxx` 凭证，可配 RPM 限额与允许模型白名单。
- **熔断/调度配置可持久化**：管理面板「系统设置」改值即时热生效并落库 `settings` 表，重启不丢失。
- **单二进制**：前端通过 `//go:embed` 打包进 Go 二进制，无外部依赖（纯 Go SQLite，**无 CGO**）。

---

## 🏗 架构

```
┌─────────────────────────────────────────────────────────────────┐
│  客户端（OpenAI / Claude / Gemini SDK / curl / 第三方平台）        │
└───────────────────────────┬─────────────────────────────────────┘
                            │  Bearer sk-nv-xxx
┌───────────────────────────▼─────────────────────────────────────┐
│  Entry Layer     /v1/* 多协议翻译  TraceID 注入  下游凭证鉴权       │
└───────────────────────────┬─────────────────────────────────────┘
┌───────────────────────────▼─────────────────────────────────────┐
│  Scheduler       N 路并发 · 健康加权洗牌 · 先到先得 · 熔断半开探测   │
└───────────┬───────────────────────────────────┬─────────────────┘
            │                                   │
┌───────────▼──────────────────┐  ┌─────────────▼──────────────────┐
│  RateLimit  每 Key 令牌桶 40RPM│  │  Auto-Pilot  三模式×三引擎决策   │
│             X-RateLimit 校准   │  │  Controller→Executor→Runtime   │
└───────────┬──────────────────┘  │  LLM 引擎 ReAct agent 循环      │
            │                     └─────────────┬──────────────────┘
┌───────────▼───────────────────────────────────▼──────────────────┐
│  Upstream   integrate.api.nvidia.com (Chat)  ai.api.nvidia.com    │
│             (Embedding/Rerank)  + SSE 流式                         │
└───────────────────────────┬─────────────────────────────────────┘
┌───────────────────────────▼─────────────────────────────────────┐
│  Data       SQLite (WAL) · 时序日志 · 健康追踪 · 模型目录 ·        │
│             模型健康聚合 · settings 持久化 · autopilot pending     │
└───────────────────────────┬─────────────────────────────────────┘
┌───────────────────────────▼─────────────────────────────────────┐
│  ModelMeta   24h 同步器 + 主动探活器 Prober + 失效检测 + 替代推荐   │
└─────────────────────────────────────────────────────────────────┘
```

**分层职责**：`entry`（HTTP/多协议/鉴权/Trace）→ `scheduler`（并发/熔断半开/选路）→ `ratelimit`（令牌桶）+ `autopilot`（智能决策）→ `upstream`（NVIDIA HTTP + SSE）→ `data`（SQLite 持久化）+ `modelmeta`（同步/探活/失效检测）。

---

## 🚀 快速开始

### 方式一：Docker（推荐）

```bash
# 1. 准备配置
cp .env.example .env
# 编辑 .env：填入 ADMIN_TOKEN 和 NVIDIA_TEST_KEY

# 2. 构建并启动（默认注入 v0.6.0 版本号）
docker compose up -d --build

# 3. 访问
#    面板: http://localhost:8787
#    健康: curl http://localhost:8787/health
```

双标签构建：`docker build -t nslmcrs/gateway:latest -t nslmcrs/gateway:v0.6.0 .`

数据持久化在 Docker 命名卷 `nslmcrs-data` 中。

### 方式二：裸机（systemd）

```bash
# 1. 构建二进制（需 Go 1.25+ 和 Node 20+）
cd web && npm install && npm run build && cd ..   # 构建前端到 internal/entry/dist
go build -o bin/gateway ./cmd/gateway              # 构建后端（自动 embed 前端）

# 2. 一键安装为 systemd 服务
sudo bash deploy/install.sh

# 3. 管理
systemctl status nslmcrs
journalctl -u n-slmcrs -f
```

安装位置：二进制 `/opt/n-slmcrs/gateway`，配置 `/opt/n-slmcrs/.env`，数据 `/opt/n-slmcrs/data/`。

### 方式三：开发模式

```bash
# 终端 1：后端（热重载可选 air）
cp .env.example .env && 填值
go run ./cmd/gateway

# 终端 2：前端（Vite 热更新，自动代理到 :8787）
cd web && npm install && npm run dev
# 访问 http://localhost:5173
```

---

## 📡 API 参考

### 转发端点（下游凭证鉴权 `Bearer sk-nv-xxx`）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/chat/completions` | 对话补全（流式/非流式，OpenAI 兼容） |
| `POST` | `/v1/completions` | 文本补全 |
| `POST` | `/v1/messages` | Claude（Anthropic）协议翻译 |
| `POST` | `/v1beta/models/:m:generateContent` | Gemini（Google）协议翻译 |
| `POST` | `/v1/embeddings` | 向量嵌入（路由 ai.api.nvidia.com） |
| `POST` | `/v1/ranking` | 重排序 |
| `GET`  | `/v1/models` | 可用模型列表（匿名） |

```bash
# 调用示例
curl http://localhost:8787/v1/chat/completions \
  -H "Authorization: Bearer sk-nv-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "meta/llama-3.1-8b-instruct",
    "messages": [{"role":"user","content":"你好"}],
    "max_tokens": 128,
    "stream": false
  }'
```

响应携带 `X-Trace-ID`，可用于在「日志中心」全链路追踪。

### 管理端点（`X-Admin-Token` 鉴权，首登强制改密）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET/POST` | `/api/admin/auth/status` `/login` `/change-password` | 鉴权状态 / 登录 / 改密 |
| `GET/POST` | `/api/admin/keys` | 上游密钥 列表 / 新增 |
| `POST` | `/api/admin/keys/bulk` | **批量导入**上游密钥（自动去重 + 幂等） |
| `DELETE/PATCH` | `/api/admin/keys/:id` | 删除 / 启停 |
| `GET/POST` | `/api/admin/credentials` | 下游凭证 列表 / 签发 |
| `DELETE` | `/api/admin/credentials/:id` | 删除凭证 |
| `GET` | `/api/admin/metrics?window=1h` | 聚合指标 |
| `GET` | `/api/admin/timeseries?window=1h&bucket=60` | 时序曲线 |
| `GET` | `/api/admin/key-health?window=1h` | 每 Key 健康度 |
| `GET` | `/api/admin/models` `/models/plaza` | 模型目录 / 广场视图（可用度+能力过滤） |
| `POST` | `/api/admin/models/sync` | 手动触发模型同步 |
| `POST` | `/api/admin/models/test` | 单模型探活（可用度 Test） |
| `POST` | `/api/admin/models/probe-all` | 一键探活全部 chat 模型 |
| `GET/PUT` | `/api/admin/settings` | 熔断/调度运行时配置（GET 取值 / PUT 热生效+落库） |
| `GET` | `/api/admin/logs?level=ERROR&source=scheduler` | 日志查询 |
| `GET/PUT` | `/api/admin/autopilot/state` `/mode` `/engine` | Auto-Pilot 状态 / 模式 / 引擎切换 |
| `GET` | `/api/admin/autopilot/snapshot` | 决策输入快照（密钥健康+指标+时序） |
| `GET/POST` | `/api/admin/autopilot/pending/:key/approve\|reject` | 辅助模式待审建议批准/驳回 |

---

## ⚙️ 配置

所有配置通过环境变量 / `.env` 注入（见 `.env.example`）。关键项：

| 变量 | 默认 | 说明 |
|------|------|------|
| `PORT` | `8787` | 监听端口 |
| `ADMIN_TOKEN` | `ADMIN` | 管理令牌，首登强制改密并写 bcrypt 哈希 |
| `NVIDIA_TEST_KEY` | （空） | 启动时自动注册的首个上游密钥 |
| `DEFAULT_RPM` | `40` | 每密钥官方 RPM 上限 |
| `DEFAULT_CONCURRENCY` | `5` | 热模型 N 路并发度 |
| `MAX_CONCURRENCY` | `10` | 最大并发上限 |
| `CIRCUIT_THRESHOLD` | `5` | 连续失败触发熔断次数 |
| `CIRCUIT_COOLDOWN` | `30s` | 熔断初始冷却（指数退避） |
| `MODEL_SYNC_INTERVAL` | `24h` | 模型目录同步周期 |
| `SQLITE_PATH` | `data/nslmcrs.db` | 数据库路径（自动建父目录） |

**Auto-Pilot LLM 引擎（可选）**——三者齐全时 LLM 引擎调用真实大模型（gateway 模式），留空则回退确定性 stub（仍可产出动作与推理轨迹）：

| 变量 | 说明 |
|------|------|
| `LLM_BASE_URL` | OpenAI 兼容端点（可指向网关自身 `http://localhost:8787/v1`） |
| `LLM_API_KEY` | 下游凭证 `sk-nv-xxx`（需先在面板签发） |
| `LLM_MODEL` | 目标模型，如 `meta/llama-3.1-8b-instruct` |

运行时熔断/调度配置可通过管理面板「系统设置」修改，**保存即热生效并持久化**到 `settings` 表（重启不丢失）。

---

## 🧪 验证

```bash
# 1. 添加上游密钥
curl -X POST http://localhost:8787/api/admin/keys \
  -H "X-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"key_value":"nvapi-xxx","label":"主账号"}'

# 1b. 批量导入上游密钥（粘贴多行 / 逗号 / 分号分隔，自动去重 + 幂等）
curl -X POST http://localhost:8787/api/admin/keys/bulk \
  -H "X-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"raw":"nvapi-aaa...\nnvapi-bbb...\nnvapi-ccc...","label":"2026-06 批量"}'
# 响应：{ "total":3, "added":3, "skipped":0, "items":[{...}] }
# 重复导入同一批不会报错 —— 已存在的密钥标记为 duplicate 跳过

# 2. 同步模型目录 + 一键探活
curl -X POST http://localhost:8787/api/admin/models/sync -H "X-Admin-Token: $ADMIN_TOKEN"
curl -X POST http://localhost:8787/api/admin/models/probe-all -H "X-Admin-Token: $ADMIN_TOKEN"

# 3. 签发下游凭证
curl -X POST http://localhost:8787/api/admin/credentials \
  -H "X-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"测试客户端"}'

# 4. 并发负载测试（验证 N 路先到先得 + 限流 + 被动统计）
pip install httpx
python scripts/load-test.py --url http://localhost:8787 \
  --token sk-nv-xxx --n 50 --concurrency 10 \
  --model meta/llama-3.1-8b-instruct

# 5. 切换 Auto-Pilot 到 LLM agent 引擎（需先配 LLM_* 三件套）
curl -X PUT http://localhost:8787/api/admin/autopilot/engine \
  -H "X-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"engine":"llm"}'
curl http://localhost:8787/api/admin/autopilot/state -H "X-Admin-Token: $ADMIN_TOKEN"
# RecentTrace 字段返回 ReAct 推理轨迹（think/act/observe）
```

---

## 📁 项目结构

```
.
├── cmd/gateway/            # 主入口，组装各层并启动 HTTP 服务
├── internal/
│   ├── config/             # 环境变量 + .env 加载与校验
│   ├── data/               # SQLite (modernc.org/sqlite, 纯 Go) schema + CRUD
│   │   └── schema.sql      # upstream_keys / downstream_credentials / models /
│   │                       # request_logs / key_health / model_health_stats /
│   │                       # settings / autopilot_pending / logs
│   ├── ratelimit/          # 令牌桶 + 滑动窗口健康追踪
│   ├── upstream/           # NVIDIA HTTP 客户端 + SSE 解析
│   ├── scheduler/          # N 路并发 + 健康加权 + 熔断半开探测
│   ├── entry/              # HTTP 入口（多协议翻译 + 鉴权 + 嵌入前端）
│   ├── modelmeta/          # 失效检测 + 24h 同步器 + 主动探活器 Prober
│   ├── modelcatalog/       # 模型能力分类（chat/reasoning/embedding/...）
│   ├── protocol/           # Claude / Gemini 协议翻译
│   ├── autopilot/          # Auto-Pilot：Runtime + Controller + LLM agent
│   └── admin/              # 管理 API（/api/admin/*）
├── web/                   # React + TS + Vite + Tailwind（shadcn 风）前端
│   └── src/pages/         # 8 模块：概览/运维/日志/模型/密钥/分发/调度/设置
├── deploy/                # systemd unit + 裸机安装脚本
├── scripts/               # 模型同步触发 + 并发负载测试
├── Dockerfile             # 多阶段构建（Node → Go → distroless）
├── docker-compose.yml
└── docs/specs/            # 设计文档
```

---

## 🗺 路线图

**Phase 1** — 核心骨架 ✅
- 多密钥聚合 + N 路并发先到先得
- OpenAI 协议（chat/completions/models）
- 模型目录 24h 同步 + 失效推荐
- 运维监控面板（8 模块）
- Docker + systemd 部署

**v0.2** — 密钥管理增强 ✅
- 🔑 批量导入上游密钥（自动去重 + 数据库幂等）
- 📊 导入结果逐条明细 + 实时解析预览
- 🐛 修复 `.gitignore` 误伤 `internal/data` 源码包

**v0.3** — 鉴权强化 ✅
- 🔐 首登强制改密 + bcrypt 哈希存储
- 默认 `ADMIN` 令牌改密前管理 API 全锁定

**v0.4** — 模型广场契约对齐 ✅
- 模型广场视图（能力过滤 + 仅可用）
- 熔断/调度配置可持久化（`settings` 表，热生效）
- Auto-Pilot 底层 Runtime/Controller/Executor 打通

**v0.5** — 智能调度 agent 化 ✅
- 🧠 **AI 动态 agent 化**：LLM 引擎 ReAct 循环（think→act→observe）+ function-calling 调度工具 + 可调试推理轨迹
- 🔌 策略动态修复：熔断半开探测自愈 + 流式健康记录补全
- 📊 **模型广场可用度**：每模型被动聚合（availability_score/avg_latency/错误数）+ 主动探活 Prober + Test/Probe-All 端点

**v0.6** — UI 换风 shadcn + 实时 ✅
- 🎨 shadcn 风扁平暗色主题（CVA Button/Badge + Tailwind surface 色板）
- 📈 Models 可用度评分卡 + Test 按钮；AutoPilot snapshot + 推理轨迹 + LLMBackendMode 徽标
- 🔄 全站页面主题统一迁移，Vite 实时构建嵌入 Go 二进制

**后续** — 生态集成
- new-api / OCTOPUS / Webhook 集成钩子落地
- 内置 Chat 测试台（仿 NVIDIA Studio）
- 上游密钥应用层加密

---

## ⚠️ 安全提醒

- `.env` 含真实密钥，**切勿提交**（已在 `.gitignore` 排除）。
- 默认 `ADMIN` 令牌仅用于首登；生产环境务必立即改密为强令牌。
- 上游密钥明文存储于 SQLite；高安全场景请叠加应用层加密（后续接入）。
- NVIDIA 免费层限流 40 RPM/Key；本网关严格不超，但仍需遵守 [NVIDIA 服务条款](https://docs.api.nvidia.com)。

---

## 📄 许可

私有项目 · N-SLMCRS · v0.6.0
