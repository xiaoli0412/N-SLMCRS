# N-SLMCRS 网关多阶段构建。
#
# 阶段一：构建前端（Node）
# 阶段二：构建 Go 后端（含 //go:embed 前端资源）
# 阶段三：最终运行镜像（distroless，仅含静态二进制 + ca-certificates）
#
# 用法：
#   docker build -t ghcr.io/xiaoli0412/n-slmcrs-gateway:latest -t ghcr.io/xiaoli0412/n-slmcrs-gateway:v0.12.0 .
#   docker run -p 8787:8787 --env-file .env -v nslmcrs-data:/data ghcr.io/xiaoli0412/n-slmcrs-gateway:latest
#
# Rust 内核 sidecar（可选，数值密集计算加速）单独构建：
#   docker build -f Dockerfile.kernel -t ghcr.io/xiaoli0412/n-slmcrs-kernel:latest -t ghcr.io/xiaoli0412/n-slmcrs-kernel:v0.12.0 .

# ───────────────── 阶段一：前端构建 ─────────────────
FROM node:20-alpine AS web-builder
WORKDIR /web

# 先拷依赖描述以利用 Docker 层缓存
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund

# 再拷源码并构建（产物输出到 /web/../internal/entry/dist）
COPY web/ ./
RUN npm run build

# ───────────────── 阶段二：Go 构建 ─────────────────
# go.mod 要求 go >= 1.25.0（GOTOOLCHAIN=local 避免 BuildKit 拉取意外工具链）
# VERSION 通过 --build-arg 注入主版本号，默认 v0.12.0；与前端 package.json 对齐。
FROM golang:1.25-alpine AS go-builder
ARG VERSION=v0.12.0
WORKDIR /src

# 依赖缓存
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=local go mod download

# 源码
COPY . .

# 把阶段一的前端产物放进 embed 期望位置
COPY --from=web-builder /web/../internal/entry/dist ./internal/entry/dist

# 静态编译（CGO 关闭，因为使用 modernc.org/sqlite 纯 Go 实现）
# -X main.version 注入版本号（/health 与 -version 输出一致）
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/gateway ./cmd/gateway

# ───────────────── 阶段三：运行时 ─────────────────
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /

# 拷贝二进制
COPY --from=go-builder /out/gateway /gateway

# 数据卷挂载点（SQLite 持久化）
# /data 由非 root 用户可写
ENV SQLITE_PATH=/data/nslmcrs.db \
    GIN_MODE=release

EXPOSE 8787

# distroless 没有 shell，直接 exec 二进制
ENTRYPOINT ["/gateway"]
