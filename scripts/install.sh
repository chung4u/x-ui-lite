#!/usr/bin/env bash
# X-UI RoyLive public edition installer.
# Downloads a chosen revision from the public repository, builds it on the server,
# and installs it without touching /etc/x-ui/x-ui.db.

set -Eeuo pipefail

REPOSITORY="${XUI_REPOSITORY:-chung4u/x-ui-roylive}"
REF="${XUI_REF:-main}"
INSTALL_DIR="${XUI_INSTALL_DIR:-/usr/local/x-ui}"
DATA_DIR="${XUI_DATA_DIR:-/etc/x-ui}"
SERVICE_NAME="${XUI_SERVICE_NAME:-x-ui}"
TOKEN="${XUI_GITHUB_TOKEN:-${GH_TOKEN:-}}"
GO_VERSION="${XUI_GO_VERSION:-1.22.12}"
WORK_DIR=""
GO_CMD=""
AUTH_HEADERS=()

info() { printf '\033[1;34m[信息]\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m[完成]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }

cleanup() {
    [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]] && rm -rf -- "$WORK_DIR"
}
trap cleanup EXIT

[[ "${EUID}" -eq 0 ]] || fail "请使用 root 或 sudo 执行此安装脚本。"
if [[ -n "$TOKEN" ]]; then
    AUTH_HEADERS=(-H "Authorization: Bearer ${TOKEN}")
fi

case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "暂不支持的服务器架构：$(uname -m)" ;;
esac

install_build_dependencies() {
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y --no-install-recommends ca-certificates curl tar build-essential
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y ca-certificates curl tar gcc gcc-c++ make
    elif command -v yum >/dev/null 2>&1; then
        yum install -y ca-certificates curl tar gcc gcc-c++ make
    else
        fail "未识别的软件包管理器。请先安装 curl、tar、gcc 和 make。"
    fi
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "缺少命令：$1"
}

ensure_go() {
    local current_version=""
    local current_minor=""
    local go_arch=""
    local go_root=""

    if command -v go >/dev/null 2>&1; then
        current_version="$(go version | awk '{print $3}' | sed 's/^go//')"
        current_minor="$(printf '%s' "$current_version" | awk -F. '{print $2}')"
        if [[ "$current_version" == 1.* && "$current_minor" =~ ^[0-9]+$ && "$current_minor" -ge 16 ]]; then
            GO_CMD="$(command -v go)"
            return
        fi
    fi

    case "$ARCH" in
        amd64) go_arch="amd64" ;;
        arm64) go_arch="arm64" ;;
    esac
    go_root="/opt/x-ui-go/${GO_VERSION}"
    GO_CMD="${go_root}/go/bin/go"

    if [[ ! -x "$GO_CMD" ]]; then
        info "下载 Go ${GO_VERSION} 编译器"
        mkdir -p "$go_root"
        curl --fail --silent --show-error --location --retry 3 \
            "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz" \
            -o "$WORK_DIR/go.tar.gz"
        tar -xzf "$WORK_DIR/go.tar.gz" -C "$go_root"
    fi

    [[ -x "$GO_CMD" ]] || fail "Go 编译器准备失败。"
}

info "准备安装 ${REPOSITORY}@${REF}（${ARCH}）"
install_build_dependencies
require_command curl
require_command tar

WORK_DIR="$(mktemp -d /tmp/x-ui-install.XXXXXX)"
ARCHIVE="$WORK_DIR/source.tar.gz"
ensure_go

info "下载项目源码"
curl --fail --silent --show-error --location --retry 3 \
    "${AUTH_HEADERS[@]}" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${REPOSITORY}/tarball/${REF}" \
    -o "$ARCHIVE"

tar -xzf "$ARCHIVE" -C "$WORK_DIR"
SOURCE_DIR="$(find "$WORK_DIR" -mindepth 1 -maxdepth 1 -type d -name '*x-ui*' | head -n 1)"
[[ -n "$SOURCE_DIR" && -f "$SOURCE_DIR/go.mod" ]] || fail "下载内容不是有效的 x-ui 源码。"
[[ -f "$SOURCE_DIR/bin/xray-linux-${ARCH}" ]] || fail "源码中缺少 bin/xray-linux-${ARCH}。"

info "构建 Linux ${ARCH} 面板二进制"
(
    cd "$SOURCE_DIR"
    CGO_ENABLED=1 GOOS=linux GOARCH="$ARCH" "$GO_CMD" build -trimpath -o "$WORK_DIR/x-ui" .
)

[[ -x "$WORK_DIR/x-ui" ]] || fail "面板二进制构建失败。"

BACKUP_DIR="${INSTALL_DIR}/backups/$(date +%Y%m%d-%H%M%S)"
if [[ -d "$INSTALL_DIR" ]]; then
    info "备份当前运行文件到 ${BACKUP_DIR}（不会备份或修改面板数据库）"
    mkdir -p "$BACKUP_DIR"
    for item in x-ui bin x-ui.sh; do
        [[ -e "$INSTALL_DIR/$item" ]] && cp -a "$INSTALL_DIR/$item" "$BACKUP_DIR/"
    done
fi

info "安装程序文件；面板数据保留在 ${DATA_DIR}"
mkdir -p "$INSTALL_DIR/bin" "$DATA_DIR"
install -m 0755 "$WORK_DIR/x-ui" "$INSTALL_DIR/x-ui"
install -m 0755 "$SOURCE_DIR/bin/xray-linux-${ARCH}" "$INSTALL_DIR/bin/xray-linux-${ARCH}"
install -m 0644 "$SOURCE_DIR/bin/geoip.dat" "$INSTALL_DIR/bin/geoip.dat"
install -m 0644 "$SOURCE_DIR/bin/geosite.dat" "$INSTALL_DIR/bin/geosite.dat"
install -m 0755 "$SOURCE_DIR/x-ui.sh" "$INSTALL_DIR/x-ui.sh"
install -m 0755 "$SOURCE_DIR/scripts/install.sh" "$INSTALL_DIR/install.sh"
install -m 0644 "$SOURCE_DIR/x-ui.service" "/etc/systemd/system/${SERVICE_NAME}.service"
ln -sfn "$INSTALL_DIR/x-ui.sh" /usr/local/bin/x-ui

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

if [[ "${XUI_NO_RESTART:-0}" == "1" ]]; then
    success "文件已安装。XUI_NO_RESTART=1 已启用，请确认后手动执行：systemctl restart ${SERVICE_NAME}"
else
    info "重启面板服务（不会修改服务器网络、VPN 或面板数据库）"
    systemctl restart "$SERVICE_NAME"
    systemctl --no-pager --full status "$SERVICE_NAME"
    success "安装完成。数据目录保持为 ${DATA_DIR}。"
fi
