# N-SLMCRS Gateway

> NVIDIA Studio LLM **Concurrent Dispatch** Gateway — 聚合多账号 `nvapi-` 密钥、对热模型发起 N 路并发请求并**先到先得**，内置 Auto-Pilot 智能调度与模型广场可用度监控，单二进制交付管理面板。

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25-76b900?logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-19-61dafb?logo=react&logoColor=white">
  <img alt="SQLite" src="https://img.shields.io/badge/SQLite-pure--Go-003b57?logo=sqlite&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/status-v0.11.0-76b900">
  <a href="https://github.com/xiaoli0412/N-SLMCRS/releases"><img alt="Release" src="https://img.shields.io/badge/release-v0.11.0-76b900?logo=github&logoColor=white"></a>
</p>

> 📦 **最新发布 [v0.11.0](https://github.com/xiaoli0412/N-SLMCRS/releases/tag/v0.11.0)** — 内置 Chat 测试台 · Rust 决策计算下沉 · 基础铺路（日志 !BADKEY 修复 + ratelimit/modelhealth 单测 + CI 升级）。完整版本历程见 [Releases](https://github.com/xiaoli0412/N-SLMCRS/releases)。

---

## 🆕 v0.11.0 亮点

- 💬 **内置 Chat 测试台（Playground）**：管理面板新增仿 NVIDIA Studio 的对话测试页——模型选择 + 系统提示 + temperature/max_tokens + 流式开关；管理凭证直调调度器（复用 N 路并发 + 熔断 + 限流，绕过下游凭证），SSE 逐 token 渲染、延迟/token 用量展示。`POST /api/admin/playground/chat`。
- 🦀 **决策计算下沉 Rust sidecar**：kernel-rs 新增无状态端点 `/verdict`（模型健康三态判定）、`/weighted-score`（调度加权评分）、`/circuit-check`（按 Key 熔断阈值检查），与 Go 侧数值对齐；`/verdict` 接入 `modelhealth.Sweeper.applyVerdict`（慢路径，不可达降级回 Go）。新增 `internal/kernelctl` 客户端（1s 超时快失败降级）。热路径（限流桶 / 按 Key 熔断 / weighted-shuffle）保留 Go，留待 v0.12 `/reserve` 批量化。
- 🧪 **基础铺路**：修复 gateway 请求日志 `!BADKEY` 格式 bug（请求行误占 slog key 位）；补 `ratelimit`（TokenBucket/SlidingWindow/HealthTracker）与 `modelhealth`（applyVerdict/nextCooldown/interfacesFor 等）单元测试——此前两块零覆盖，为后续 Rust 化建回归护栏；CI actions 升级（checkout@v5 / setup-node@v5 / setup-go@v6）消除 Node-20 deprecation。

---

## 🆕 v0.10.0 亮点

- 🔗 **集成渠道（new-api / sapi）**：下游中转网关通过 OpenAI 兼容协议接入本网关；管理面板新增渠道管理（CRUD + 启停），创建时生成可粘贴的接入配置（含明文密钥仅此一次，其余脱敏），密钥 bcrypt 哈希存储不下发明文；计费用量回采端点 `/api/admin/hooks/channels/:id/usage`。
- 📡 **Webhook 事件回调**：请求成功 / 失败 / 限流 / 熔断时异步 POST JSON 通知，带 `HMAC-SHA256` 签名（`X-N-SLMCRS-Signature`）；URL / 密钥 / 触发事件（success,error,rate_limited,circuit）均可在 Distribution 页热改并落库；支持一键发送测试事件。调度器经 `WebhookEmitter` 接口接线，成功由 `recordSuccess` 发射、失败/限流由 `recordResult` 发射。
- 📝 **日志文件落盘**：`LOG_FILE` 非空时额外写日志文件（追加、自动建目录），建议指向 D 盘持久路径避免写满系统盘；顺手修复 `SetLevel` 重建 handler 丢失原格式/输出端的旧 bug。
- 🎨 **Distribution 页真实化**：集成渠道表 + Webhook 配置卡替换原 v0.10 占位槽位；渠道密钥掩码、webhook secret 掩码保留。

---

## 🆕 v0.9.0 亮点

- 🛡 **模型级 + 永久熔断**：按模型遍历所有 NVIDIA 推理接口各探 N 次（次数/间隔可由管理面板热改），按成功率判定 `closed / open / permanent`；成功率长期低于 30%（地板可配）连续多轮 → 永久熔断。熔断模型从公开 `/v1/models` 实时隐藏，请求已熔断模型返回双语说明 + 建议替代模型。
- 🌐 **双语错误 + 请求自动转换**：所有错误响应统一 `{message(zh), message_en(en), type, trace_id}`；自动纠正 `messages` 为字符串、`prompt`→`messages`、OpenAI 别名（gpt-4o 等）→ NVIDIA 等价；多模态 `content` 数组透传视觉模型。
- 📊 **日志系统升级**：内置 `log/slog` 结构化日志，扇出 stdout + `app_logs` 表，日志中心页有真实全量数据；`LOG_LEVEL` / `LOG_FORMAT` 可配，trace_id 全链路贯穿。
- 🔬 **模型参数二阶面板**：接入 HuggingFace 模型卡 + OpenRouter + LiteLLM 富化架构/定价/许可证/支持接口，广场卡片点击弹出二阶抽屉（概览/健康/探活/参数四 Tab）。
- 🎨 **UI 全量重构**：Vite 6 + React 19 + shadcn/ui + Tailwind v4 + framer-motion；亮/暗主题切换、沉浸极光广场、3D 倾斜卡片、模型熔断徽标；移除 OCTOPUS/sub2api 占位。
- 🛠 **部署**：`scripts/deploy.sh v0.9.0` 自动构建→推送→服务器拉取→健康检查。

---

## ✨ 特性

- **密钥聚合 + 并发先到先得**：多个 NVIDIA Studio 账号密钥池化，热模型同时发起 N 个请求，返回最先成功的结果。
- **严格不超官方限流**：每 Key 独立令牌桶（默认 40 RPM），并根据上游 `X-RateLimit-Remaining` 实时校准，杜绝浪费。
- **熔断 + 半开探测 + 指数退避**：连续失败自动熔断（按 Key 与按模型双轨），冷却时长指数增长封顶 10 分钟；半开态主动放行探测请求自愈。
- **OpenAI / Claude / Gemini 多协议兼容**：`/v1/chat/completions`、`/v1/completions`、`/v1/models`、`/v1/messages`、`/v1beta/...:generateContent`、`/v1/embeddings`、`/v1/ranking`。
- **流式 SSE 透传 + 流式健康记录**：`stream:true` 原生支持，首字节即锁定获胜上游；流式响应同样写入 request_logs 被动统计。
- **模型广场 + 双路可用度**：24h 自动同步 `/v1/models`；每模型聚合被动可用度评分 + 主动探活，支持单模型 Test 与一键 Probe-All；模型级健康扫描 + 永久熔断。
- **Auto-Pilot 智能调度**：三模式（手动 / 辅助 / 全自动）× 三引擎（自适应 PID·EWMA / 轻量预测 Holt-Winters / LLM Agent）。LLM 引擎已 **agent 化**——ReAct 循环（think→act→observe）调用调度工具，产出可调试推理轨迹。
- **鉴权强化 + 首登强制改密**：默认 `ADMIN` 令牌首登强制修改并写入 bcrypt 哈希；改密前管理 API 全锁定。
- **运维监控面板（shadcn 风）**：Vite + Tailwind 扁平暗色主题，实时成功率 / 请求量 / Token / 每 Key 健康度 / Auto-Pilot 快照 / 推理轨迹全维度图表。
- **下游凭证签发**：向客户端分发 `sk-nv-xxx` 凭证，可配 RPM 限额与允许模型白名单。
- **集成钩子（v0.10）**：new-api/sapi 渠道经 OpenAI 兼容协议接入（CRUD + 接入配置生成 + 计费回采）；Webhook 事件回调（成功/失败/限流/熔断，HMAC-SHA256 签名，热配置 + 测试）。
- **日志文件落盘（v0.10）**：`LOG_FILE` 可选写日志文件（追加、自动建目录），stdout + app_logs 表 + 文件三路扇出。
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
            │                     │  并发档位感知(5/10/50/100)      │
            │                     └─────────────┬──────────────────┘
            │                                   │ HTTP/JSON（降级回 Go）
            │                     ┌─────────────▼──────────────────┐
            │                     │  nslmcrs-kernel (Rust sidecar)  │
            │                     │  Holt-Winters 预测 · 可用度聚合  │
            │                     └────────────────────────────────┘
┌───────────▼───────────────────────────────────────────────────────┐
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

# 2a. 从 ghcr 拉取已发布镜像并启动（生产推荐，秒级）
TAG=v0.9.0 docker compose pull
docker compose up -d

# 2b. 或本地构建（开发用，同时拉起 gateway + kernel，默认注入 v0.9.0 版本号）
docker compose up -d --build

# 3. 访问
#    面板: http://localhost:8787
#    健康: curl http://localhost:8787/health
#    内核: curl http://localhost:8790/healthz
```

**远程服务器一键部署**（本地构建→推 ghcr→服务器拉取→健康检查）：

```bash
make publish TAG=v0.9.0        # 本地构建双镜像推 ghcr（latest + v0.9.0）
bash scripts/deploy.sh v0.9.0  # 服务器 git pull + pull + up -d + 健康检查
```

双标签构建（手动）：
- 网关：`docker build -t ghcr.io/xiaoli0412/n-slmcrs-gateway:latest -t ghcr.io/xiaoli0412/n-slmcrs-gateway:v0.9.0 .`
- 内核：`docker build -f Dockerfile.kernel -t ghcr.io/xiaoli0412/n-slmcrs-kernel:latest -t ghcr.io/xiaoli0412/n-slmcrs-kernel:v0.9.0 .`

数据持久化在 Docker 命名卷 `nslmcrs-data` 中（含数据库与 `/data/backups` 备份目录）。

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
| `GET/POST` | `/api/admin/backup` | 数据库备份列表 / 立即备份（VACUUM INTO 快照） |
| `GET/DELETE` | `/api/admin/backup/:file` | 备份文件下载 / 删除 |
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

**Rust 内核 sidecar（可选）**——默认指向 `http://127.0.0.1:8790`，不配置或不可达时 Auto-Pilot 自动降级回内置 Go 预测/可用度实现（功能不缺，仅数值计算走 Go）：

| 变量 | 说明 |
|------|------|
| `KERNEL_URL` | Rust sidecar 地址（默认 `http://127.0.0.1:8790`，docker compose 已自动注入） |
| `KERNEL_DISABLE` | 设为 `1` 强制禁用 sidecar，纯 Go 运行 |

**数据库备份（v0.8）**——`VACUUM INTO` 事务一致快照 + 定时轮转；docker compose 已将 `BACKUP_DIR` 指向持久卷 `/data/backups`：

| 变量 | 默认 | 说明 |
|------|------|------|
| `BACKUP_DIR` | `data/backups` | 备份存放目录（容器内 `/data/backups` 落持久卷） |
| `BACKUP_INTERVAL` | `24h` | 自动备份间隔；`0`/留空则禁用定时，仅手动触发 |
| `BACKUP_RETENTION` | `7` | 保留最近多少份；`0` 则不自动清理 |

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
│   │                       # model_specs / settings / autopilot_pending / logs
│   ├── ratelimit/          # 令牌桶 + 滑动窗口健康追踪
│   ├── upstream/           # NVIDIA HTTP 客户端 + SSE 解析
│   ├── scheduler/          # N 路并发 + 健康加权 + 熔断半开探测
│   ├── entry/              # HTTP 入口（多协议翻译 + 鉴权 + 嵌入前端 + 在途计数）
│   ├── inflight/           # 全局在途请求 gauge（并发档位感知，原子计数）
│   ├── modelmeta/          # 失效检测 + 24h 同步器 + 主动探活器 Prober
│   ├── modelcatalog/       # 模型能力分类 + 注册表富化（OpenRouter 同步）
│   ├── protocol/           # Claude / Gemini 协议翻译
│   ├── autopilot/          # Auto-Pilot：Runtime + Controller + LLM agent + Tier 档位 + kernel_client
│   ├── backup/             # 数据库备份服务（VACUUM INTO 快照 + 定时轮转，v0.8）
│   └── admin/              # 管理 API（/api/admin/*）
├── kernel-rs/              # Rust 内核 sidecar（nslmcrs-kernel，axum :8790）
│   └── src/main.rs         # Holt-Winters 预测 + 可用度聚合
├── web/                   # React + TS + Vite + Tailwind（shadcn 风）前端
│   └── src/pages/         # 10 模块：概览/运维/日志/模型/模型详情/密钥/分发/调度/备份/设置
├── .github/workflows/     # CI（go vet+test+build + web build）
├── deploy/                # systemd unit + 裸机安装脚本
├── scripts/               # 模型同步 + 负载测试 + 远程部署 deploy.sh
├── Dockerfile             # 多阶段构建（Node → Go → distroless）
├── Dockerfile.kernel      # Rust 内核多阶段构建（rust:alpine → alpine）
├── docker-compose.yml     # gateway + kernel 双服务编排（ghcr 镜像 + 持久卷）
├── Makefile               # 构建/测试/发布/部署一站式入口
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

**v0.7** — Rust 内核 + 并发档位感知 + 模型详情 ✅
- 🦀 Rust 内核 sidecar（nslmcrs-kernel）：Holt-Winters 预测 + 可用度聚合剥离，不可达自动降级回 Go
- 🎚️ Auto-Pilot 并发档位感知（low/mid/high/peak ≈ 5/10/50/100）+ 可用 key 数动态并发度
- 🎛️ 模型二/三级详情页（概览/健康/探活/参数说明），吸收 Operations 模型维度指标
- 📚 注册表富化（OpenRouter 同步落 model_specs）+ 模型已消失契约修复
- 🎨 UI 设计系统重构（动效 + 骨架屏 + 组件原语补齐）

**v0.8** — 数据库备份 + 部署流水线 + 开发完善 ✅
- 💽 数据库备份功能（VACUUM INTO 事务一致快照 + 定时轮转 + 管理 API 下载/删除）
- 🚀 ghcr 镜像流水线：本地构建推 ghcr、服务器 `compose pull` 秒级上线；`.dockerignore` 砍 ~280MB 上下文
- 🛠 `scripts/deploy.sh` 一键远程部署 + 健康检查；Makefile + GitHub Actions CI
- 🔧 接入 `NVIDIA_TEST_KEY` 启动幂等播种；v0.7 新模块测试补齐（inflight/tier/specs/registry/kernel_client）

**后续** — 生态集成
- kernel 可用度聚合 Go 侧接入（`/availability` 端点契约就绪，待跨层打通）+ 注册表 id 归一化收敛
- new-api / sapi / Webhook 集成钩子落地（v0.10）；决策计算下沉 kernel-rs（/verdict 接入慢路径，v0.11）；全量 Rust 控制面（限流桶+熔断+健康聚合权威化、/reserve 批量化、kernel 硬依赖 fail-closed，v0.12）
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

私有项目 · N-SLMCRS · v0.9.0
