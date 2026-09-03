#!/usr/bin/env bash
# X-UI Lite prebuilt installer.
# Installs the current X-UI Lite 1.0 release and preserves /etc/x-ui/x-ui.db.

set -Eeuo pipefail

REPOSITORY="${XUI_REPOSITORY:-chung4u/x-ui-lite}"
VERSION="v1.0.2"
INSTALL_DIR="${XUI_INSTALL_DIR:-/usr/local/x-ui}"
DATA_DIR="${XUI_DATA_DIR:-/etc/x-ui}"
SERVICE_NAME="${XUI_SERVICE_NAME:-x-ui}"
XRAY_CORE_VERSION="26.6.27"
TOKEN="${XUI_GITHUB_TOKEN:-${GH_TOKEN:-}}"
WORK_DIR=""
AUTH_HEADERS=()
FIRST_INSTALL=0
PANEL_USERNAME=""
PANEL_PASSWORD=""
PANEL_PORT=""
ACCESS_HOST=""
PANEL_BASE_PATH=""
PANEL_PROTOCOL="http"
FIREWALL_RESULT="未执行防火墙处理。"
RESTART_FAILED=0

info() { printf '\033[1;34m[信息]\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m[完成]\033[0m %s\n' "$*"; }
warning() { printf '\033[1;33m[提示]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }
cleanup() { [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]] && rm -rf -- "$WORK_DIR"; }
trap cleanup EXIT

[[ "${EUID}" -eq 0 ]] || fail "请使用 root 或 sudo 执行此安装脚本。"
if [[ -n "${XUI_VERSION:-}" && "$XUI_VERSION" != "$VERSION" ]]; then
    fail "安装器仅支持当前 1.0 正式版（${VERSION}）。"
fi
if [[ -n "$TOKEN" ]]; then AUTH_HEADERS=(-H "Authorization: Bearer ${TOKEN}"); fi

case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "暂不支持的服务器架构：$(uname -m)" ;;
esac

require_command() { command -v "$1" >/dev/null 2>&1 || fail "缺少命令：$1"; }
random_path_segment() {
    local chars='ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
    local value='' byte index
    while [[ ${#value} -lt 5 ]]; do
        byte="$(od -An -N1 -tu1 /dev/urandom | tr -d '[:space:]')"
        index=$((byte % 36))
        value+="${chars:index:1}"
    done
    printf '%s' "$value"
}

choose_panel_port() {
    local attempt=0 hex candidate
    if [[ -n "${XUI_PORT:-}" ]]; then
        [[ "$XUI_PORT" =~ ^[0-9]+$ && "$XUI_PORT" -ge 1024 && "$XUI_PORT" -le 65535 ]] || fail "XUI_PORT 必须是 1024–65535 的端口号。"
        printf '%s' "$XUI_PORT"; return
    fi
    # Keep the conventional x-ui port so cloud firewalls can be preconfigured.
    printf '54321'
}

resolve_access_host() {
    [[ -n "${XUI_ACCESS_HOST:-}" ]] && { printf '%s' "$XUI_ACCESS_HOST"; return; }
    curl --fail --silent --show-error --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}'
}

open_panel_firewall() {
    local port="$1" ufw_status port_rule nft_rules iptables_rules firewall_rules
    if [[ "${XUI_NO_FIREWALL:-0}" == "1" ]]; then
        FIREWALL_RESULT="已按 XUI_NO_FIREWALL=1 跳过本机防火墙处理。"
        return
    fi

    if command -v ufw >/dev/null 2>&1; then
        ufw_status="$(ufw status 2>&1 || true)"
        if [[ "${ufw_status,,}" == *"status: active"* ]]; then
            info "检测到 UFW，正在放行 TCP ${port}"
            if ufw allow "${port}/tcp" comment "x-ui panel" >/dev/null; then
                FIREWALL_RESULT="已通过 UFW 自动放行 TCP ${port}。"
            else
                FIREWALL_RESULT="UFW 自动放行 TCP ${port} 失败，请手动检查规则。"
                warning "$FIREWALL_RESULT"
            fi
            return
        fi
    fi

    if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
        port_rule="${port}/tcp"
        info "检测到 firewalld，正在放行 TCP ${port}"
        if firewall-cmd --add-port="$port_rule" >/dev/null && firewall-cmd --permanent --add-port="$port_rule" >/dev/null; then
            FIREWALL_RESULT="已通过 firewalld 自动放行 TCP ${port}（即时生效且重启后保留）。"
        else
            FIREWALL_RESULT="firewalld 自动放行 TCP ${port} 未完整完成，请手动检查运行态和持久规则。"
            warning "$FIREWALL_RESULT"
        fi
        return
    fi

    if command -v nft >/dev/null 2>&1; then
        nft_rules="$(nft list ruleset 2>/dev/null || true)"
        firewall_rules="${nft_rules,,}"
        if [[ "$firewall_rules" == *"policy drop"* || "$firewall_rules" == *"policy reject"* ]]; then
            FIREWALL_RESULT="检测到默认拒绝的 nftables 规则，为避免写入不可持久化规则，未自动修改；请确认 TCP ${port} 已放行。"
            warning "$FIREWALL_RESULT"
            return
        fi
    fi

    if command -v iptables >/dev/null 2>&1; then
        iptables_rules="$(iptables -S INPUT 2>/dev/null || true)"
        firewall_rules="${iptables_rules,,}"
        if [[ "$firewall_rules" == *"-p input drop"* || "$firewall_rules" == *"-p input reject"* ]]; then
            FIREWALL_RESULT="检测到默认拒绝的 iptables 规则，为避免写入不可持久化规则，未自动修改；请确认 TCP ${port} 已放行。"
            warning "$FIREWALL_RESULT"
            return
        fi
    fi

    FIREWALL_RESULT="未检测到需要放行的本机防火墙策略。"
}

read_panel_access() {
    local access_info key value
    access_info="$("$INSTALL_DIR/x-ui" setting -show-panel-access)" || return 1
    PANEL_PROTOCOL=""
    PANEL_PORT=""
    PANEL_BASE_PATH=""
    while IFS='=' read -r key value; do
        case "$key" in
            PANEL_PROTOCOL) PANEL_PROTOCOL="$value" ;;
            PANEL_PORT) PANEL_PORT="$value" ;;
            PANEL_BASE_PATH) PANEL_BASE_PATH="$value" ;;
        esac
    done <<< "$access_info"
    [[ "$PANEL_PROTOCOL" == "http" || "$PANEL_PROTOCOL" == "https" ]] || return 1
    [[ "$PANEL_PORT" =~ ^[0-9]+$ && "$PANEL_PORT" -ge 1 && "$PANEL_PORT" -le 65535 ]] || return 1
    [[ "$PANEL_BASE_PATH" == /* && "$PANEL_BASE_PATH" == */ ]] || return 1
}

show_install_access() {
    local install_title="X-UI 更新完成"
    if [[ "$FIRST_INSTALL" == "1" ]]; then
        install_title="X-UI 首次安装完成"
    fi
    printf '\n\033[1;32m========================================\n          %s\n========================================\033[0m\n' "$install_title"
    printf '访问地址：\033[1;36m%s://%s:%s%s\033[0m\n' "$PANEL_PROTOCOL" "$ACCESS_HOST" "$PANEL_PORT" "$PANEL_BASE_PATH"
    if [[ "$FIRST_INSTALL" == "1" ]]; then
        printf '用户名：  \033[1;36m%s\033[0m\n密码：    \033[1;36m%s\033[0m\n' "$PANEL_USERNAME" "$PANEL_PASSWORD"
        printf '\n防火墙处理：%s\n' "$FIREWALL_RESULT"
        printf '云厂商防火墙策略无法由脚本修改；如已启用，请同时放行 TCP %s。首次登录后请立即修改密码。\n' "$PANEL_PORT"
    fi
}

require_command curl
require_command tar
[[ -f "${DATA_DIR}/x-ui.db" ]] || FIRST_INSTALL=1
WORK_DIR="$(mktemp -d /tmp/x-ui-install.XXXXXX)"
ARCHIVE="$WORK_DIR/x-ui-linux-${ARCH}.tar.gz"

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
info "使用内置 Xray 核心 ${XRAY_CORE_VERSION}"
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
    PANEL_BASE_PATH="/${XUI_BASE_PATH:-$(random_path_segment)}/"
    info "初始化首次安装的访问凭据"
    "$INSTALL_DIR/x-ui" setting -username "$PANEL_USERNAME" -password "$PANEL_PASSWORD"
    "$INSTALL_DIR/x-ui" setting -port "$PANEL_PORT"
    "$INSTALL_DIR/x-ui" setting -base-path "$PANEL_BASE_PATH"
    open_panel_firewall "$PANEL_PORT"
fi

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
if [[ "${XUI_NO_RESTART:-0}" == "1" ]]; then
    success "文件已安装。请确认后手动执行：systemctl restart ${SERVICE_NAME}"
else
    info "重启面板服务（不会修改服务器网络、VPN 或面板数据库）"
    if systemctl restart "$SERVICE_NAME"; then
        systemctl --no-pager --full status "$SERVICE_NAME" || warning "无法读取服务状态，请执行 systemctl status ${SERVICE_NAME} 检查。"
        success "安装完成。数据目录保持为 ${DATA_DIR}。"
    else
        RESTART_FAILED=1
        warning "面板服务重启失败，访问地址仍已输出；请执行 systemctl status ${SERVICE_NAME} 排查。"
    fi
fi
ACCESS_HOST="$(resolve_access_host)"
[[ -n "$ACCESS_HOST" ]] || ACCESS_HOST="<服务器公网 IP>"
if ! read_panel_access; then
    fail "无法读取当前面板访问配置；请检查 ${DATA_DIR}/x-ui.db。"
fi
show_install_access
[[ "$RESTART_FAILED" == "0" ]] || exit 1
