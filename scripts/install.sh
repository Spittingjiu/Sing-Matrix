#!/usr/bin/env bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RESET='\033[0m'

if [[ ${EUID} -ne 0 ]]; then
  echo -e "${RED}[FATAL] S-Matrix installer must run as root.${RESET}"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="${SCRIPT_DIR}/s-matrix"
SERVICE_SRC="${SCRIPT_DIR}/s-matrix.service"
INSTALL_BIN="/usr/local/bin/s-matrix"
CONFIG_DIR="/etc/s-matrix"
SERVICE_DST="/etc/systemd/system/s-matrix.service"

if [[ ! -f "${BIN_SRC}" ]]; then
  echo -e "${RED}[FATAL] Missing binary: ${BIN_SRC}${RESET}"
  echo -e "${YELLOW}Build first: go build -o scripts/s-matrix ./s-matrix-backend/cmd/server${RESET}"
  exit 1
fi
if [[ ! -f "${SERVICE_SRC}" ]]; then
  echo -e "${RED}[FATAL] Missing service template: ${SERVICE_SRC}${RESET}"
  exit 1
fi

if systemctl list-unit-files | grep -q '^s-matrix.service'; then
  systemctl stop s-matrix.service >/dev/null 2>&1 || true
fi
pkill -f '/usr/local/bin/s-matrix' >/dev/null 2>&1 || true

mkdir -p "${CONFIG_DIR}"
chmod 700 "${CONFIG_DIR}"
if [[ ! -f "${CONFIG_DIR}/config.json" ]]; then
  cat > "${CONFIG_DIR}/config.json" <<'JSON'
{
  "log": { "level": "info", "timestamp": true },
  "inbounds": [],
  "outbounds": [ { "type": "direct", "tag": "direct" } ],
  "route": { "final": "direct", "rules": [] }
}
JSON
fi
if [[ ! -f "${CONFIG_DIR}/matrix.db" ]]; then
  touch "${CONFIG_DIR}/matrix.db"
fi
chmod 600 "${CONFIG_DIR}/config.json" "${CONFIG_DIR}/matrix.db"

install -m 0755 "${BIN_SRC}" "${INSTALL_BIN}"
install -m 0644 "${SERVICE_SRC}" "${SERVICE_DST}"
systemctl daemon-reload
systemctl enable --now s-matrix.service
sleep 1

PUBLIC_IP="$(curl -4 -fsS --max-time 3 https://ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')"
STATUS="$(systemctl is-active s-matrix.service || true)"

echo -e "${CYAN}"
echo '  ______        __  ___      __       _      '
echo ' / __/ /  ___ _/  |/  /___ _/ /_____ (_)__ __'
echo '_\ \/ _ \/ _ `/ /|_/ / __ `/ __/ __ `/ /\ \ /'
echo '/___/_//_/\_,_/_/  /_/\_,_/\__/\_,_/_//_\_\ '
echo -e "${RESET}"
if [[ "${STATUS}" == "active" ]]; then
  echo -e "${GREEN}S-Matrix 核心系统已成功挂载！${RESET}"
else
  echo -e "${YELLOW}S-Matrix 已安装，但服务状态为：${STATUS}。请执行 journalctl -u s-matrix -n 80 --no-pager 查看。${RESET}"
fi
echo -e "${GREEN}请访问 http://${PUBLIC_IP}:19088 ，默认口令：admin/admin${RESET}"
echo -e "${CYAN}Config: ${CONFIG_DIR}/config.json | DB: ${CONFIG_DIR}/matrix.db | Service: s-matrix.service${RESET}"
