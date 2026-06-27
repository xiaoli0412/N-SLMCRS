#!/usr/bin/env bash
# N-SLMCRS 远程部署脚本（v0.8.0）。
#
# 流程：本地 make publish（构建+推 ghcr）→ ssh 服务器 git pull + compose pull + up -d + 健康检查。
# 本脚本可安全提交：不含任何密钥，传输委托 gitignored 的 scripts/rssh.sh。
#
# 用法：
#   bash scripts/deploy.sh v0.8.0
#   bash scripts/deploy.sh v0.8.0 --no-build   # 跳过本地构建+推送（镜像已发布）
#
# 环境变量（可选）：
#   REMOTE_DIR   服务器上仓库克隆目录（未设则自动探测）
#   GHCR_PAT     首次部署私有镜像时用于服务器 docker login（公开镜像无需）
#   REMOTE_PORT  ssh 端口（rssh.sh 默认 22）
set -euo pipefail

TAG="${1:?用法: bash scripts/deploy.sh <tag> [--no-build]}"
NO_BUILD=0
[[ "${2:-}" == "--no-build" ]] && NO_BUILD=1

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RSSH="$ROOT_DIR/scripts/rssh.sh"
REMOTE_HOST="186.241.94.121"
GATEWAY_IMG="ghcr.io/xiaoli0412/n-slmcrs-gateway"
KERNEL_IMG="ghcr.io/xiaoli0412/n-slmcrs-kernel"
REMOTE_DIR="${REMOTE_DIR:-}"

# ─── 前置检查 ─────────────────────────────────────────────
if [[ ! -f "$RSSH" ]]; then
  echo "✗ 找不到 scripts/rssh.sh（gitignored，含服务器凭据）。请在本地放置后再部署。" >&2
  exit 1
fi
ssh_ok() { bash "$RSSH" "$@"; }

echo "▸ 部署目标: http://$REMOTE_HOST:8787  (tag=$TAG)"

# ─── 1. 本地构建 + 推送 ghcr ─────────────────────────────
if [[ "$NO_BUILD" -eq 0 ]]; then
  echo "▸ [1/4] 本地构建并推送镜像到 ghcr（make publish TAG=$TAG）…"
  (cd "$ROOT_DIR" && make publish TAG="$TAG")
else
  echo "▸ [1/4] 跳过本地构建（--no-build），假定镜像已发布。"
fi

# ─── 2. 定位服务器仓库目录 ───────────────────────────────
echo "▸ [2/4] 连接服务器并定位仓库目录…"
if [[ -z "$REMOTE_DIR" ]]; then
  REMOTE_DIR=$(ssh_ok "find /root /opt /home /srv -maxdepth 3 -type f -name docker-compose.yml 2>/dev/null | grep -i n-slmcrs | head -1 | xargs dirname 2>/dev/null || true")
  if [[ -z "$REMOTE_DIR" ]]; then
    # 退而求其次：按目录名匹配
    REMOTE_DIR=$(ssh_ok "find /root /opt /home /srv -maxdepth 3 -type d -iname '*n-slmcrs*' -o -iname '*nslmcrs*' 2>/dev/null | head -1 || true")
  fi
fi
if [[ -z "$REMOTE_DIR" ]]; then
  echo "✗ 未能自动定位服务器上的仓库目录。请显式设置 REMOTE_DIR 环境变量后重试。" >&2
  exit 1
fi
echo "  仓库目录: $REMOTE_DIR"

# ─── 3. 服务器：git pull + ghcr 登录(若需) + compose pull + up ──
echo "▸ [3/4] 服务器拉取新代码与镜像…"
if [[ -n "${GHCR_PAT:-}" ]]; then
  echo "  GHCR_PAT 已提供，执行一次性 docker login…"
  echo "$GHCR_PAT" | ssh_ok "docker login ghcr.io -u xiaoli0412 --password-stdin" >/dev/null 2>&1 || true
fi

ssh_ok "set -e; cd '$REMOTE_DIR'; \
  git fetch --tags && git checkout $TAG 2>/dev/null || git pull --ff-only; \
  export TAG=$TAG; \
  docker compose pull; \
  docker compose up -d"

# ─── 4. 健康检查 ─────────────────────────────────────────
echo "▸ [4/4] 健康检查…"
ok=0
for _ in $(seq 1 30); do
  g=$(ssh_ok "curl -fsS http://localhost:8787/health 2>/dev/null || true")
  k=$(ssh_ok "curl -fsS http://localhost:8790/healthz 2>/dev/null || true")
  if [[ "$g" == *'"status":"ok"'* && "$k" == "ok" ]]; then
    echo "  gateway /health: $g"
    echo "  kernel   /healthz: $k"
    ok=1
    break
  fi
  sleep 2
done

echo
ssh_ok "cd '$REMOTE_DIR' && docker compose ps"
if [[ "$ok" -ne 1 ]]; then
  echo "✗ 健康检查未通过，输出最近日志：" >&2
  ssh_ok "cd '$REMOTE_DIR' && docker compose logs --tail=80" >&2 || true
  exit 1
fi
echo "✓ 部署完成：http://$REMOTE_HOST:8787  (tag=$TAG)"
