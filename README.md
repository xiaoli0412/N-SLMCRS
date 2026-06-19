# N-SLMCRS Gateway

> NVIDIA Studio LLM **Concurrent Dispatch** Gateway — 聚合多账号 `nvapi-` 密钥、对热模型发起 N 路并发请求并**先到先得**，单二进制内置管理面板。

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.24-76b900?logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-18-61dafb?logo=react&logoColor=white">
  <img alt="SQLite" src="https://img.shields.io/badge/SQLite-pure--Go-003b57?logo=sqlite&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/status-v0.2-green">
</p>

---

## ✨ 特性

- **密钥聚合 + 并发先到先得**：多个 NVIDIA Studio 账号密钥池化，热模型同时发起 N 个请求，返回最先成功的结果。
- **严格不超官方限流**：每 Key 独立令牌桶（默认 40 RPM），并根据上游 `X-RateLimit-Remaining` 实时校准，杜绝浪费。
- **熔断 + 指数退避**：连续失败自动熔断，冷却时长 30s→60s→120s 指数增长，封顶 10 分钟。
- **OpenAI 协议兼容**：`POST /v1/chat/completions`、`/v1/completions`、`/v1/models`，OpenAI SDK / curl 直接可用。
- **流式 SSE 透传**：`stream:true` 原生支持，首字节即锁定获胜上游。
- **模型目录 24h 自动同步**：从 `/v1/models` 软更新；请求失效模型时返回官方错误措辞并推荐**成功率最高**的替代模型。
- **运维监控面板**：实时成功率 / 请求量 / Token / 平均延迟 / 每 Key 健康度，全维度图表。
- **下游凭证签发**：向客户端分发 `sk-nv-xxx` 凭证，可配 RPM 限额与允许模型白名单。
- **单二进制**：前端通过 `//go:embed` 打包进 Go 二进制，无外部依赖（纯 Go SQLite，**无 CGO**）。

---

## 🏗 架构

```
┌─────────────────────────────────────────────────────────────────┐
│  客户端（OpenAI SDK / curl / 第三方平台）                          │
└───────────────────────────┬─────────────────────────────────────┘
                            │  Bearer sk-nv-xxx
┌───────────────────────────▼─────────────────────────────────────┐
│  Entry Layer     /v1/chat/completions  /v1/models  TraceID 注入    │
└───────────────────────────┬─────────────────────────────────────┘
┌───────────────────────────▼─────────────────────────────────────┐
│  Scheduler       N 路并发 · 健康加权洗牌 · 先到先得 · 熔断判定       │
└───────────────────────────┬─────────────────────────────────────┘
┌───────────────────────────▼─────────────────────────────────────┐
│  RateLimit       每 Key 令牌桶 40 RPM · X-RateLimit 校准           │
└───────────────────────────┬─────────────────────────────────────┘
┌───────────────────────────▼─────────────────────────────────────┐
│  Upstream        integrate.api.nvidia.com  (Chat)                  │
│                  ai.api.nvidia.com        (Embedding/Rerank)      │
└───────────────────────────┬─────────────────────────────────────┘
┌───────────────────────────▼─────────────────────────────────────┐
│  Data            SQLite (WAL) · 时序日志 · 健康追踪 · 软失效模型     │
└─────────────────────────────────────────────────────────────────┘
```

**分层职责**：`entry`（HTTP/鉴权/Trace）→ `scheduler`（并发/熔断/选路）→ `ratelimit`（令牌桶）→ `upstream`（NVIDIA HTTP 客户端 + SSE）→ `data`（SQLite 持久化）。

---

## 🚀 快速开始

### 方式一：Docker（推荐）

```bash
# 1. 准备配置
cp .env.example .env
# 编辑 .env：填入 ADMIN_TOKEN 和 NVIDIA_TEST_KEY

# 2. 构建并启动
docker compose up -d --build

# 3. 访问
#    面板: http://localhost:8787
#    健康: curl http://localhost:8787/health
```

数据持久化在 Docker 命名卷 `nslmcrs-data` 中。

### 方式二：裸机（systemd）

```bash
# 1. 构建二进制（需 Go 1.24+ 和 Node 20+）
cd web && npm install && npm run build && cd ..   # 构建前端到 internal/entry/dist
go build -o bin/gateway ./cmd/gateway              # 构建后端（自动 embed 前端）

# 2. 一键安装为 systemd 服务
sudo bash deploy/install.sh

# 3. 管理
systemctl status n-slmcrs
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
| `POST` | `/v1/chat/completions` | 对话补全（流式/非流式） |
| `POST` | `/v1/completions` | 文本补全 |
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

### 管理端点（`X-Admin-Token` 鉴权）

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET/POST` | `/api/admin/keys` | 上游密钥 列表 / 新增 |
| `POST` | `/api/admin/keys/bulk` | **批量导入**上游密钥（粘贴多行/逗号分隔，自动去重 + 幂等） |
| `DELETE/PATCH` | `/api/admin/keys/:id` | 删除 / 启停 |
| `GET/POST` | `/api/admin/credentials` | 下游凭证 列表 / 签发 |
| `DELETE` | `/api/admin/credentials/:id` | 删除凭证 |
| `GET` | `/api/admin/metrics?window=1h` | 聚合指标 |
| `GET` | `/api/admin/timeseries?window=1h&bucket=60` | 时序曲线 |
| `GET` | `/api/admin/key-health?window=1h` | 每 Key 健康度 |
| `GET` | `/api/admin/models` | 模型目录（含失效） |
| `POST` | `/api/admin/models/sync` | 手动触发模型同步 |
| `GET` | `/api/admin/logs?level=ERROR&source=scheduler` | 日志查询 |

---

## ⚙️ 配置

所有配置通过环境变量 / `.env` 注入（见 `.env.example`）。关键项：

| 变量 | 默认 | 说明 |
|------|------|------|
| `PORT` | `8787` | 监听端口 |
| `ADMIN_TOKEN` | （空） | 管理 API 令牌，**必填** |
| `NVIDIA_TEST_KEY` | （空） | 启动时自动注册的首个上游密钥 |
| `DEFAULT_RPM` | `40` | 每密钥官方 RPM 上限 |
| `DEFAULT_CONCURRENCY` | `5` | 热模型 N 路并发度 |
| `MAX_CONCURRENCY` | `10` | 最大并发上限 |
| `CIRCUIT_THRESHOLD` | `5` | 连续失败触发熔断次数 |
| `CIRCUIT_COOLDOWN` | `30s` | 熔断初始冷却（指数退避） |
| `MODEL_SYNC_INTERVAL` | `24h` | 模型目录同步周期 |
| `SQLITE_PATH` | `data/nslmcrs.db` | 数据库路径（自动建父目录） |

运行时可通过管理面板「系统设置」预览覆盖（持久化在后续阶段接入 `settings` 表）。

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

# 2. 同步模型目录
curl -X POST http://localhost:8787/api/admin/models/sync -H "X-Admin-Token: $ADMIN_TOKEN"

# 3. 签发下游凭证
curl -X POST http://localhost:8787/api/admin/credentials \
  -H "X-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"测试客户端"}'

# 4. 并发负载测试（验证 N 路先到先得 + 限流）
pip install httpx
python scripts/load-test.py --url http://localhost:8787 \
  --token sk-nv-xxx --n 50 --concurrency 10 \
  --model meta/llama-3.1-8b-instruct
```

---

## 📁 项目结构

```
.
├── cmd/gateway/            # 主入口，组装各层并启动 HTTP 服务
├── internal/
│   ├── config/             # 环境变量 + .env 加载与校验
│   ├── data/               # SQLite (modernc.org/sqlite, 纯 Go) schema + CRUD
│   │   └── schema.sql      # 表：upstream_keys / downstream_credentials /
│   │                       #       models / request_logs / key_health / logs
│   ├── ratelimit/          # 令牌桶 + 滑动窗口健康追踪
│   ├── upstream/           # NVIDIA HTTP 客户端 + SSE 解析
│   ├── scheduler/          # N 路并发 + 健康加权 + 熔断
│   ├── entry/              # HTTP 入口（OpenAI 兼容 + 鉴权 + 嵌入前端）
│   ├── modelmeta/         # 模型失效检测 + 24h 同步器
│   └── admin/             # 管理 API（/api/admin/*）
├── web/                   # React + TS + Vite + Tailwind 前端
│   └── src/pages/         # 8 个模块：概览/运维/日志/模型/密钥/分发/调度/设置
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
- 🔑 **批量导入上游密钥**：粘贴多行 / 逗号 / 分号分隔，自动批内去重 + 数据库幂等（重复导入不报错）
- 📊 导入结果逐条明细（added / duplicate / invalid）+ 实时解析预览
- 🎨 密钥页 UI 优化：Toast 反馈、连续失败列、活跃密钥计数徽标
- 🐛 修复 `.gitignore` 误伤 `internal/data` 源码包导致仓库无法克隆构建的严重问题
- ✅ 新增 `BulkAddUpstreamKeys` 单元测试覆盖去重 / 幂等 / 非法格式

**Phase 2** — 协议与调度扩展
- Claude（`/v1/messages`）+ Gemini（`/v1beta/...:generateContent`）协议
- 嵌入（`/v1/embeddings`）+ 重排序端点
- Auto-Pilot 三引擎落地：自适应（PID/EWMA）/ 轻量预测（Holt-Winters）/ LLM 决策
- 三模式：手动 / 辅助 / 全自动

**Phase 3** — 生态集成
- new-api / OCTOPUS / Webhook 集成钩子
- 设置持久化到 `settings` 表
- 内置 Chat 测试台（仿 NVIDIA Studio）

---

## ⚠️ 安全提醒

- `.env` 含真实密钥，**切勿提交**（已在 `.gitignore` 排除）。
- 生产环境务必设置 `ADMIN_TOKEN`，否则管理 API 无鉴权。
- 上游密钥明文存储于 SQLite；高安全场景请叠加应用层加密（后续接入）。
- NVIDIA 免费层限流 40 RPM/Key；本网关严格不超，但仍需遵守 [NVIDIA 服务条款](https://docs.api.nvidia.com)。

---

## 📄 许可

私有项目 · N-SLMCRS · v0.2
