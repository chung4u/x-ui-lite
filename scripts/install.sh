#!/usr/bin/env bash
# X-UI RoyLive prebuilt installer.
# Downloads a signed-by-transport release asset and preserves /etc/x-ui/x-ui.db.

set -Eeuo pipefail

REPOSITORY="${XUI_REPOSITORY:-chung4u/x-ui-roylive}"
VERSION="${XUI_VERSION:-latest}"
INSTALL_DIR="${XUI_INSTALL_DIR:-/usr/local/x-ui}"
DATA_DIR="${XUI_DATA_DIR:-/etc/x-ui}"
SERVICE_NAME="${XUI_SERVICE_NAME:-x-ui}"
TOKEN="${XUI_GITHUB_TOKEN:-${GH_TOKEN:-}}"
WORK_DIR=""
AUTH_HEADERS=()
FIRST_INSTALL=0
PANEL_USERNAME=""
PANEL_PASSWORD=""
PANEL_PORT=""
ACCESS_HOST=""
PANEL_BASE_PATH=""

info() { printf '\033[1;34m[信息]\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m[完成]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }
cleanup() { [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]] && rm -rf -- "$WORK_DIR"; }
trap cleanup EXIT

[[ "${EUID}" -eq 0 ]] || fail "请使用 root 或 sudo 执行此安装脚本。"
if [[ -n "$TOKEN" ]]; then AUTH_HEADERS=(-H "Authorization: Bearer ${TOKEN}"); fi

case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "暂不支持的服务器架构：$(uname -m)" ;;
esac

require_command() { command -v "$1" >/dev/null 2>&1 || fail "缺少命令：$1"; }
random_hex() { od -An -N"$1" -tx1 /dev/urandom | tr -d '[:space:]'; }

choose_panel_port() {
    local attempt=0 hex candidate
    if [[ -n "${XUI_PORT:-}" ]]; then
        [[ "$XUI_PORT" =~ ^[0-9]+$ && "$XUI_PORT" -ge 1024 && "$XUI_PORT" -le 65535 ]] || fail "XUI_PORT 必须是 1024–65535 的端口号。"
        printf '%s' "$XUI_PORT"; return
    fi
    while [[ "$attempt" -lt 20 ]]; do
        hex="$(random_hex 2)"
        candidate=$((20000 + (16#${hex} % 40000)))
        if ! command -v ss >/dev/null 2>&1 || ! ss -lnt "sport = :${candidate}" | grep -q LISTEN; then
            printf '%s' "$candidate"; return
        fi
        attempt=$((attempt + 1))
    done
    fail "未能找到可用的随机面板端口，请通过 XUI_PORT 指定端口后重试。"
}

resolve_access_host() {
    [[ -n "${XUI_ACCESS_HOST:-}" ]] && { printf '%s' "$XUI_ACCESS_HOST"; return; }
    curl --fail --silent --show-error --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}'
}

show_first_install_access() {
    printf '\n\033[1;32m========================================\n          X-UI 首次安装完成\n========================================\033[0m\n'
    printf '访问地址：\033[1;36mhttp://%s:%s%s\033[0m\n用户名：  \033[1;36m%s\033[0m\n密码：    \033[1;36m%s\033[0m\n' "$ACCESS_HOST" "$PANEL_PORT" "$PANEL_BASE_PATH" "$PANEL_USERNAME" "$PANEL_PASSWORD"
    printf '\n请确认防火墙已放行 TCP %s，并在首次登录后妥善保存或更新凭据。\n' "$PANEL_PORT"
}

require_command curl
require_command tar
[[ -f "${DATA_DIR}/x-ui.db" ]] || FIRST_INSTALL=1
WORK_DIR="$(mktemp -d /tmp/x-ui-install.XXXXXX)"
ARCHIVE="$WORK_DIR/x-ui-linux-${ARCH}.tar.gz"

if [[ "$VERSION" == "latest" ]]; then
    VERSION="$(curl --fail --silent --show-error --location "${AUTH_HEADERS[@]}" -H 'Accept: application/vnd.github+json' "https://api.github.com/repos/${REPOSITORY}/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
    [[ -n "$VERSION" ]] || fail "未找到可用 Release，请稍后再试或设置 XUI_VERSION。"
fi

info "下载已编译版本 ${VERSION}（Linux ${ARCH}）"
curl --fail --silent --show-error --location --retry 3 \
    "${AUTH_HEADERS[@]}" \
    "https://github.com/${REPOSITORY}/releases/download/${VERSION}/x-ui-linux-${ARCH}.tar.gz" \
    -o "$ARCHIVE"
tar -xzf "$ARCHIVE" -C "$WORK_DIR"
[[ -x "$WORK_DIR/x-ui" && -x "$WORK_DIR/bin/xray-linux-${ARCH}" ]] || fail "安装包内容不完整。"

BACKUP_DIR="${INSTALL_DIR}/backups/$(date +%Y%m%d-%H%M%S)"
if [[ -d "$INSTALL_DIR" ]]; then
    info "备份当前运行文件到 ${BACKUP_DIR}（不会修改面板数据库）"
    mkdir -p "$BACKUP_DIR"
    for item in x-ui bin x-ui.sh install.sh; do
        [[ -e "$INSTALL_DIR/$item" ]] && cp -a "$INSTALL_DIR/$item" "$BACKUP_DIR/"
    done
fi

info "安装程序文件；面板数据保留在 ${DATA_DIR}"
mkdir -p "$INSTALL_DIR/bin" "$DATA_DIR"
install -m 0755 "$WORK_DIR/x-ui" "$INSTALL_DIR/x-ui"
install -m 0755 "$WORK_DIR/bin/xray-linux-${ARCH}" "$INSTALL_DIR/bin/xray-linux-${ARCH}"
install -m 0644 "$WORK_DIR/bin/geoip.dat" "$INSTALL_DIR/bin/geoip.dat"
install -m 0644 "$WORK_DIR/bin/geosite.dat" "$INSTALL_DIR/bin/geosite.dat"
install -m 0755 "$WORK_DIR/x-ui.sh" "$INSTALL_DIR/x-ui.sh"
install -m 0755 "$WORK_DIR/install.sh" "$INSTALL_DIR/install.sh"
install -m 0644 "$WORK_DIR/x-ui.service" "/etc/systemd/system/${SERVICE_NAME}.service"
ln -sfn "$INSTALL_DIR/x-ui.sh" /usr/local/bin/x-ui

if [[ "$FIRST_INSTALL" == "1" ]]; then
    PANEL_USERNAME="${XUI_USERNAME:-admin}"
    PANEL_PASSWORD="${XUI_PASSWORD:-admin}"
    PANEL_PORT="$(choose_panel_port)"
    PANEL_BASE_PATH="/${XUI_BASE_PATH:-$(random_hex 8)}/"
    ACCESS_HOST="$(resolve_access_host)"
    [[ -n "$ACCESS_HOST" ]] || ACCESS_HOST="<服务器公网 IP>"
    info "初始化首次安装的访问凭据"
    "$INSTALL_DIR/x-ui" setting -username "$PANEL_USERNAME" -password "$PANEL_PASSWORD"
    "$INSTALL_DIR/x-ui" setting -port "$PANEL_PORT"
    "$INSTALL_DIR/x-ui" setting -base-path "$PANEL_BASE_PATH"
fi

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
if [[ "${XUI_NO_RESTART:-0}" == "1" ]]; then
    success "文件已安装。请确认后手动执行：systemctl restart ${SERVICE_NAME}"
else
    info "重启面板服务（不会修改服务器网络、VPN 或面板数据库）"
    systemctl restart "$SERVICE_NAME"
    systemctl --no-pager --full status "$SERVICE_NAME"
    success "安装完成。数据目录保持为 ${DATA_DIR}。"
fi
[[ "$FIRST_INSTALL" == "1" ]] && show_first_install_access
