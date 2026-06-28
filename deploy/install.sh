#!/usr/bin/env bash
# N-SLMCRS 裸机一键安装脚本（Linux）。
#
# 用法：
#   sudo bash deploy/install.sh
#
# 完成：
#   1. 创建 nslmcrs 系统用户
#   2. 安装二进制到 /opt/n-slmcrs/gateway
#   3. 创建数据目录 /opt/n-slmcrs/data + /opt/n-slmcrs/kernel-data（v0.12 Rust 控制面持久化）
#   4. 拷贝 .env（如不存在则从 .env.example 生成并提示填写）
#   5. 安装 systemd 单元并启动

set -euo pipefail

INSTALL_DIR=/opt/n-slmcrs
DATA_DIR=${INSTALL_DIR}/data
KERNEL_DATA_DIR=${INSTALL_DIR}/kernel-data
LOG_DIR=${INSTALL_DIR}/logs
SERVICE_FILE=/etc/systemd/system/n-slmcrs.service
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "${SCRIPT_DIR}")"

echo "================================================"
echo "  N-SLMCRS 网关安装程序"
echo "================================================"

# 1. root 检查
if [ "$EUID" -ne 0 ]; then
  echo "[ERROR] 请使用 root 或 sudo 执行。"
  exit 1
fi

# 2. 系统用户
if ! id -u nslmcrs >/dev/null 2>&1; then
  echo "[1/6] 创建系统用户 nslmcrs ..."
  useradd --system --no-create-home --shell /usr/sbin/nologin nslmcrs
else
  echo "[1/6] 用户 nslmcrs 已存在，跳过。"
fi

# 3. 安装目录
echo "[2/6] 创建安装目录 ${INSTALL_DIR} ..."
mkdir -p "${DATA_DIR}" "${KERNEL_DATA_DIR}" "${LOG_DIR}"

# 4. 构建二进制（若不存在则现场构建）
BIN_PATH=${INSTALL_DIR}/gateway
if [ -f "${REPO_DIR}/bin/gateway" ]; then
  echo "[3/6] 拷贝预构建二进制 ..."
  cp "${REPO_DIR}/bin/gateway" "${BIN_PATH}"
else
  echo "[3/6] 未发现预构建二进制，现场编译 ..."
  if ! command -v go >/dev/null 2>&1; then
    echo "[ERROR] 未找到 go 编译器，请先安装 Go 1.24+，或预编译后重试。"
    exit 1
  fi
  (cd "${REPO_DIR}" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "${BIN_PATH}" ./cmd/gateway)
fi
chmod 0755 "${BIN_PATH}"

# 5. .env 配置
echo "[4/6] 准备配置文件 ..."
if [ ! -f "${INSTALL_DIR}/.env" ]; then
  if [ -f "${REPO_DIR}/.env" ]; then
    cp "${REPO_DIR}/.env" "${INSTALL_DIR}/.env"
    echo "      已拷贝现有 .env"
  else
    cp "${REPO_DIR}/.env.example" "${INSTALL_DIR}/.env"
    ADMIN_TOKEN=$(head -c 24 /dev/urandom | base64 | tr -d '/+=' | cut -c1-24)
    sed -i "s/^ADMIN_TOKEN=.*/ADMIN_TOKEN=${ADMIN_TOKEN}/" "${INSTALL_DIR}/.env"
    # 默认启用日志文件落盘（写到安装盘 /opt/n-slmcrs/logs，避免只靠 journal 丢失历史）
    echo "LOG_FILE=${LOG_DIR}/gateway.log" >> "${INSTALL_DIR}/.env"
    echo "      已生成新 .env，ADMIN_TOKEN=${ADMIN_TOKEN}"
    echo "      ⚠ 请编辑 ${INSTALL_DIR}/.env 填入 NVIDIA_TEST_KEY"
  fi
fi
chmod 0640 "${INSTALL_DIR}/.env"

# 6. 权限
echo "[5/6] 设置目录归属 ..."
chown -R nslmcrs:nslmcrs "${INSTALL_DIR}"

# 7. systemd
echo "[6/6] 安装 systemd 服务 ..."
cp "${SCRIPT_DIR}/n-slmcrs.service" "${SERVICE_FILE}"
systemctl daemon-reload
systemctl enable n-slmcrs >/dev/null
systemctl restart n-slmcrs

sleep 2
if systemctl is-active --quiet n-slmcrs; then
  echo ""
  echo "================================================"
  echo "  ✅ 安装完成，服务已启动"
  echo "================================================"
  echo "  状态:   systemctl status n-slmcrs"
  echo "  日志:   journalctl -u n-slmcrs -f  或  tail -f ${LOG_DIR}/gateway.log"
  echo "  端口:   http://localhost:8787"
  echo "  配置:   ${INSTALL_DIR}/.env"
echo "  数据:   ${DATA_DIR}/"
echo "  kernel: ${KERNEL_DATA_DIR}/  (Rust 控制面持久化, v0.12+)"
echo "  日志文件: ${LOG_DIR}/"
  echo "================================================"
else
  echo "[ERROR] 服务未启动成功，请查看：journalctl -u n-slmcrs -n 50"
  exit 1
fi
