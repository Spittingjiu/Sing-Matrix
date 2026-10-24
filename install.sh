#!/usr/bin/env bash
#
# S-Matrix one-click installer
#   curl -fsSL https://raw.githubusercontent.com/Spittingjiu/Sing-Matrix/main/install.sh | bash
#
# Options (env vars work too):
#   --port <n>          panel port                     (default 19088)
#   --listen <addr>     bind address                   (default 0.0.0.0)
#   --dir <path>        install dir                    (default /opt/sing-matrix)
#   --version <tag>     release tag                    (default latest)
#   --no-singbox        skip installing the sing-box core
#   --no-service        install files only, do not touch systemd
#   --behind-proxy      bind 127.0.0.1 and print an nginx/Caddy snippet
#   --webroot <path>    ACME webroot for HTTP-01       (default /var/www/smatrix-acme)
#   --local-bin <file>  install from a local binary instead of downloading
#   --uninstall         remove the service and files (keeps data unless --purge)
#
set -euo pipefail

REPO="Spittingjiu/Sing-Matrix"
INSTALL_DIR="${SMATRIX_INSTALL_DIR:-/opt/sing-matrix}"
PORT="${SMATRIX_PORT:-19088}"
LISTEN_ADDR="${SMATRIX_LISTEN:-0.0.0.0}"
VERSION="${SMATRIX_VERSION:-latest}"
WEBROOT="${SMATRIX_WEBROOT:-/var/www/smatrix-acme}"
WITH_SINGBOX="${SMATRIX_WITH_SINGBOX:-1}"
WITH_SERVICE="${SMATRIX_SYSTEMD:-1}"
LOCAL_BIN="${SMATRIX_LOCAL_BIN:-}"
BEHIND_PROXY=0
ACTION=install
PURGE=0

C_OK=$'\033[0;32m'; C_ERR=$'\033[0;31m'; C_WARN=$'\033[0;33m'; C_DIM=$'\033[0;36m'; C_OFF=$'\033[0m'
say()  { printf '%s\n' "$*"; }
ok()   { printf '%s✓%s %s\n' "$C_OK" "$C_OFF" "$*"; }
warn() { printf '%s!%s %s\n' "$C_WARN" "$C_OFF" "$*"; }
die()  { printf '%s✗%s %s\n' "$C_ERR" "$C_OFF" "$*" >&2; exit 1; }
step() { printf '\n%s▸%s %s\n' "$C_DIM" "$C_OFF" "$*"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --listen) LISTEN_ADDR="$2"; shift 2 ;;
    --dir) INSTALL_DIR="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --webroot) WEBROOT="$2"; shift 2 ;;
    --local-bin) LOCAL_BIN="$2"; shift 2 ;;
    --no-singbox) WITH_SINGBOX=0; shift ;;
    --no-service) WITH_SERVICE=0; shift ;;
    --behind-proxy) BEHIND_PROXY=1; LISTEN_ADDR=127.0.0.1; WITH_SERVICE=${SMATRIX_SYSTEMD:-1}; shift ;;
    --uninstall) ACTION=uninstall; shift ;;
    --purge) PURGE=1; shift ;;
    --upgrade) ACTION=upgrade; shift ;;
    --status) ACTION=status; shift ;;
    -h|--help) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

SERVICE=sing-matrix
BIN="${INSTALL_DIR}/backend/smatrix"
DATA_DIR="${INSTALL_DIR}/data"
CERT_DIR="${INSTALL_DIR}/certs"
UNIT="/etc/systemd/system/${SERVICE}.service"

need_root() { [ "$(id -u)" = "0" ] || die "请以 root 运行（curl ... | sudo bash）"; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "不支持的架构: $(uname -m)（目前只发布 amd64 / arm64）" ;;
  esac
}

detect_os() {
  if [ -f /etc/os-release ]; then . /etc/os-release; echo "${ID:-unknown}"; else echo unknown; fi
}

install_deps() {
  local need=()
  command -v curl >/dev/null 2>&1 || need+=(curl)
  command -v tar  >/dev/null 2>&1 || need+=(tar)
  [ ${#need[@]} -eq 0 ] && return 0
  step "安装依赖: ${need[*]}"
  case "$(detect_os)" in
    debian|ubuntu) apt-get update -qq && apt-get install -y -qq "${need[@]}" ca-certificates >/dev/null ;;
    alpine) apk add --no-cache "${need[@]}" ca-certificates >/dev/null ;;
    centos|rhel|rocky|almalinux|fedora) (command -v dnf >/dev/null && dnf install -y -q "${need[@]}") || yum install -y -q "${need[@]}" >/dev/null ;;
    *) command -v "${need[0]}" >/dev/null 2>&1 || die "缺少 ${need[*]}，请先手动安装" ;;
  esac
  ok "依赖就绪"
}

latest_tag() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
    | grep -oE '"tag_name": *"[^"]+"' | head -1 | sed -E 's/.*"([^"]+)"/\1/'
}

install_singbox() {
  if [ -x /usr/local/bin/sing-box ]; then
    ok "已检测到 sing-box: $(/usr/local/bin/sing-box version 2>/dev/null | head -1)"
    return 0
  fi
  local arch tag asset tmp
  arch="$(detect_arch)"
  step "安装 sing-box core"
  tag="$(curl -fsSL https://api.github.com/repos/SagerNet/sing-box/releases/latest \
        | grep -oE '"tag_name": *"[^"]+"' | head -1 | sed -E 's/.*"([^"]+)"/\1/')"
  [ -n "${tag}" ] || { warn "无法获取 sing-box 版本，跳过（可稍后在面板内安装）"; return 0; }
  asset="sing-box-${tag#v}-linux-${arch}.tar.gz"
  tmp="$(mktemp -d)"
  curl -fsSL "https://github.com/SagerNet/sing-box/releases/download/${tag}/${asset}" -o "${tmp}/sb.tgz" \
    || { warn "sing-box 下载失败，跳过"; rm -rf "${tmp}"; return 0; }
  tar -xzf "${tmp}/sb.tgz" -C "${tmp}"
  install -m755 "${tmp}/sing-box-${tag#v}-linux-${arch}/sing-box" /usr/local/bin/sing-box
  rm -rf "${tmp}"
  ok "sing-box 已安装: $(/usr/local/bin/sing-box version | head -1)"
}

install_binary() {
  local arch tag url tmp
  mkdir -p "${INSTALL_DIR}/backend" "${DATA_DIR}" "${CERT_DIR}"
  if [ -n "${LOCAL_BIN}" ] && [ -f "${LOCAL_BIN}" ]; then
    step "安装本地二进制"
    install -m755 "${LOCAL_BIN}" "${BIN}"
    ok "已安装 $(basename "${LOCAL_BIN}")"
    return 0
  fi
  arch="$(detect_arch)"
  tag="${VERSION}"
  [ "${tag}" = "latest" ] && tag="$(latest_tag)"
  [ -n "${tag}" ] || die "无法解析最新版本，请用 --version vX.Y.Z 指定"
  step "下载 S-Matrix ${tag} (linux-${arch})"
  url="https://github.com/${REPO}/releases/download/${tag}/smatrix-linux-${arch}"
  tmp="$(mktemp)"
  curl -fsSL "${url}" -o "${tmp}" || die "下载失败: ${url}"
  install -m755 "${tmp}" "${BIN}"
  rm -f "${tmp}"
  ok "已安装到 ${BIN}"
}

gen_secret() { head -c 24 /dev/urandom | base64 | tr -d '/+=\n' | cut -c1-24; }
gen_jwt()    { head -c 48 /dev/urandom | base64 | tr -d '/+=\n' | cut -c1-43; }

seed_credentials() {
  local creds="${DATA_DIR}/panel_creds.json" pass
  if [ -f "${creds}" ]; then
    ok "保留已有面板账号（${creds}）"
    PANEL_PASS="$(sed -n 's/.*"password"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${creds}")"
    PANEL_USER="$(sed -n 's/.*"username"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${creds}")"
    return 0
  fi
  PANEL_USER="admin"
  PANEL_PASS="$(gen_secret)"
  printf '{\n  "username": "%s",\n  "password": "%s"\n}\n' "${PANEL_USER}" "${PANEL_PASS}" > "${creds}"
  chmod 600 "${creds}"
  ok "已生成面板账号"
}

write_env() {
  local envf="${DATA_DIR}/env"
  mkdir -p "${DATA_DIR}"
  if [ ! -f "${envf}" ]; then
    printf 'SMATRIX_JWT_SECRET=%s\n' "$(gen_jwt)" > "${envf}"
    chmod 600 "${envf}"
    ok "已生成 JWT 密钥"
  else
    ok "保留已有 JWT 密钥"
  fi
}

install_unit() {
  cat > "${UNIT}" <<EOF
[Unit]
Description=S-Matrix sing-box panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
Environment=SMATRIX_ADDR=${LISTEN_ADDR}:${PORT}
Environment=SMATRIX_DB=${DATA_DIR}/s-matrix.db
Environment=SMATRIX_CONFIG=${DATA_DIR}/config.json
Environment=SMATRIX_DATA_DIR=${DATA_DIR}
Environment=SMATRIX_CERT_DIR=${CERT_DIR}
Environment=SMATRIX_SINGBOX_LOG=${DATA_DIR}/singbox.log
Environment=SMATRIX_SINGBOX_BIN=/usr/local/bin/sing-box
Environment=SMATRIX_ACME_WEBROOT=${WEBROOT}
Environment=GIN_MODE=release
EnvironmentFile=-${DATA_DIR}/env
ExecStart=${BIN}
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable "${SERVICE}" >/dev/null 2>&1 || true
  systemctl restart "${SERVICE}"
  sleep 2
  systemctl is-active --quiet "${SERVICE}" || {
    journalctl -u "${SERVICE}" -n 20 --no-pager || true
    die "服务启动失败"
  }
  ok "服务已启动: ${SERVICE}"
}

prepare_webroot() {
  mkdir -p "${WEBROOT}/.well-known/acme-challenge"
  chmod -R 755 "${WEBROOT}"
}

wait_health() {
  local url="http://127.0.0.1:${PORT}/health" i
  for i in $(seq 1 30); do
    if curl -fsS -m 2 "${url}" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}

firewall_hint() {
  command -v ufw >/dev/null 2>&1 || return 0
  ufw status 2>/dev/null | grep -q "Status: active" || return 0
  warn "ufw 已启用，如需外网访问请放行: ufw allow ${PORT}/tcp"
}

print_result() {
  local ip
  ip="$(curl -fsS -m 5 https://api.ipify.org 2>/dev/null || true)"
  cat <<EOF

$(printf '%s' "${C_OK}")安装完成$(printf '%s' "${C_OFF}")

  面板地址   http://${ip:-<服务器IP>}:${PORT}/
  用户名     ${PANEL_USER}
  密码       ${PANEL_PASS}

  安装目录   ${INSTALL_DIR}
  数据目录   ${DATA_DIR}
  证书目录   ${CERT_DIR}
  服务管理   systemctl {status,restart,stop} ${SERVICE}
  查看日志   journalctl -u ${SERVICE} -f

下一步：
  1. 登录面板，点「新建节点」→「AnyTLS · 域名证书」
  2. 把域名 A 记录指向本机（DNS-only，别开 CDN），点「校验域名」
  3. 校验通过后自动签发 Let's Encrypt 证书并生成节点链接
EOF
  [ "${BEHIND_PROXY}" = "1" ] && cat <<EOF

反向代理示例（面板已只监听 127.0.0.1:${PORT}）：

  nginx:  location / { proxy_pass http://127.0.0.1:${PORT}; proxy_set_header Host \$host;
                       proxy_set_header X-Forwarded-Proto \$scheme; }
  Caddy:  panel.example.com { reverse_proxy 127.0.0.1:${PORT} }

EOF
  say ""
}

do_install() {
  need_root
  install_deps
  [ "${WITH_SINGBOX}" = "1" ] && install_singbox
  install_binary
  seed_credentials
  write_env
  prepare_webroot
  if [ "${WITH_SERVICE}" = "1" ]; then
    install_unit
    wait_health || warn "健康检查未通过，请查看 journalctl -u ${SERVICE} -n 50"
  else
    warn "已跳过 systemd（--no-service），手动启动: ${BIN}"
  fi
  firewall_hint
  print_result
}

do_upgrade() {
  need_root
  [ -x "${BIN}" ] || die "未检测到已安装的面板，请先安装"
  local old backup
  old="$("${BIN}" --version 2>/dev/null || true)"
  [ -n "${old}" ] && say "当前版本: ${old}"
  backup="${BIN}.bak-$(date +%Y%m%d%H%M%S)"
  cp -a "${BIN}" "${backup}"
  install_binary
  if [ "${WITH_SERVICE}" = "1" ] && systemctl list-unit-files | grep -q "^${SERVICE}.service"; then
    systemctl restart "${SERVICE}"
    sleep 2
    systemctl is-active --quiet "${SERVICE}" || die "升级后服务未启动，旧版已备份在 ${BIN}.bak-*"
    ok "已升级并重启（旧版备份 ${backup}）"
  else
    ok "二进制已更新"
  fi
}

do_uninstall() {
  need_root
  systemctl stop "${SERVICE}" 2>/dev/null || true
  systemctl disable "${SERVICE}" 2>/dev/null || true
  rm -f "${UNIT}"
  systemctl daemon-reload 2>/dev/null || true
  if [ "${PURGE}" = "1" ]; then
    rm -rf "${INSTALL_DIR}"
    ok "已卸载并删除 ${INSTALL_DIR}"
  else
    rm -rf "${INSTALL_DIR}/backend"
    ok "已卸载（数据保留在 ${DATA_DIR}，加 --purge 可一并删除）"
  fi
}

do_status() {
  if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q "^${SERVICE}.service"; then
    systemctl status "${SERVICE}" --no-pager -l | head -20
  else
    warn "未发现 ${SERVICE} 服务"
  fi
  [ -x "${BIN}" ] && say "二进制: ${BIN}"
  [ -f "${DATA_DIR}/panel_creds.json" ] && ok "数据目录: ${DATA_DIR}"
}

case "${ACTION}" in
  install) do_install ;;
  upgrade) do_upgrade ;;
  uninstall) do_uninstall ;;
  status) do_status ;;
esac
