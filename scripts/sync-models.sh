#!/usr/bin/env bash
# 模型目录手动同步触发脚本。
#
# 用法：
#   bash scripts/sync-models.sh [ADMIN_TOKEN] [HOST]
#
# 说明：
#   - 直接调用 /api/admin/models/sync 触发后端 SyncOnce
#   - 后台 Syncer 每 24h 自动同步一次，本脚本用于手工触发

set -euo pipefail

ADMIN_TOKEN="${1:-${ADMIN_TOKEN:-}}"
HOST="${2:-http://localhost:8787}"

if [ -z "${ADMIN_TOKEN}" ]; then
  echo "[ERROR] 请通过参数或 ADMIN_TOKEN 环境变量传入管理令牌。"
  echo "  用法: bash scripts/sync-models.sh <ADMIN_TOKEN> [HOST]"
  exit 1
fi

echo "[sync] 触发模型目录同步: ${HOST}/api/admin/models/sync"
RESP=$(curl -sS -X POST "${HOST}/api/admin/models/sync" \
  -H "X-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -w "\nHTTP_STATUS:%{http_code}")

STATUS=$(echo "${RESP}" | grep -o 'HTTP_STATUS:[0-9]*' | cut -d: -f2)
BODY=$(echo "${RESP}" | sed 's/HTTP_STATUS:[0-9]*$//')

echo "[sync] HTTP ${STATUS}"
echo "[sync] 响应: ${BODY}"

if [ "${STATUS}" = "200" ] || [ "${STATUS}" = "202" ]; then
  echo "[sync] ✅ 同步已触发，查看最新列表：curl ${HOST}/api/admin/models"
else
  echo "[sync] ❌ 同步失败"
  exit 1
fi
