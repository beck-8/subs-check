#!/bin/sh
# subs-check 一键安装脚本
# 兼容 bash / sh / dash
# 用法: curl -fsSL https://raw.githubusercontent.com/beck-8/subs-check/master/install.sh | bash
#   或: wget -qO- https://raw.githubusercontent.com/beck-8/subs-check/master/install.sh | bash
# 加速: bash <(curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/beck-8/subs-check/master/install.sh) https://ghfast.top/

set -e

# ============ 配置 ============
REPO="beck-8/subs-check"
INSTALL_DIR="/opt/subs-check"
BINARY_NAME="subs-check"
SERVICE_NAME="subs-check"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_PROXY="${1:-}"

# ============ 运行状态 ============
HAS_SYSTEMD=1
IS_UPGRADE=0

# ============ 颜色输出 ============
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info()  { printf "${BLUE}[INFO]${NC} %s\n" "$1"; }
ok()    { printf "${GREEN}[OK]${NC} %s\n" "$1"; }
warn()  { printf "${YELLOW}[WARN]${NC} %s\n" "$1"; }
error() { printf "${RED}[ERROR]${NC} %s\n" "$1"; exit 1; }

# ============ 前置检查 ============
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "请使用 root 用户或 sudo 运行此脚本"
    fi
}

check_os() {
    if [ "$(uname -s)" != "Linux" ]; then
        error "此脚本仅支持 Linux 系统"
    fi
}

check_systemd() {
    if ! command -v systemctl >/dev/null 2>&1; then
        HAS_SYSTEMD=0
        warn "未检测到 systemd，将跳过服务配置，安装完成后需手动运行"
    fi
}

check_existing() {
    if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        IS_UPGRADE=1
        info "检测到已有安装，将执行升级操作"
    fi
}

check_download_tool() {
    if command -v curl >/dev/null 2>&1; then
        DOWNLOADER="curl"
    elif command -v wget >/dev/null 2>&1; then
        DOWNLOADER="wget"
    else
        error "需要 curl 或 wget，请先安装其中之一"
    fi
}

# ============ 下载封装 ============
download() {
    url="$1"
    output="$2"
    if [ "$DOWNLOADER" = "curl" ]; then
        curl -fsSL -o "$output" "$url"
    else
        wget -qO "$output" "$url"
    fi
}

fetch_url() {
    url="$1"
    if [ "$DOWNLOADER" = "curl" ]; then
        curl -fsSL "$url"
    else
        wget -qO- "$url"
    fi
}

# ============ 架构检测 ============
detect_arch() {
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)
            ARCH="x86_64"
            ;;
        aarch64|arm64)
            ARCH="aarch64"
            ;;
        armv7*|armhf)
            ARCH="armv7"
            ;;
        i386|i686)
            ARCH="i386"
            ;;
        *)
            error "不支持的架构: $arch"
            ;;
    esac
    ok "检测到系统架构: $ARCH"
}

# ============ 获取最新版本 ============
get_latest_version() {
    info "正在获取最新版本..."
    LATEST_VERSION=$(fetch_url "$GITHUB_API" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
    if [ -z "$LATEST_VERSION" ]; then
        error "无法获取最新版本号，请检查网络连接"
    fi
    ok "最新版本: $LATEST_VERSION"
}

# ============ 下载并安装 ============
install_binary() {
    FILE_NAME="${BINARY_NAME}_Linux_${ARCH}.tar.gz"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_VERSION}/${FILE_NAME}"

    if [ -n "$GITHUB_PROXY" ]; then
        DOWNLOAD_URL="${GITHUB_PROXY}${DOWNLOAD_URL}"
    fi

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    info "正在下载 ${FILE_NAME}..."
    download "$DOWNLOAD_URL" "${TMP_DIR}/${FILE_NAME}"
    ok "下载完成"

    if [ "$IS_UPGRADE" -eq 1 ]; then
        info "正在升级 ${INSTALL_DIR} 中的程序..."
    else
        info "正在安装到 ${INSTALL_DIR}..."
    fi

    mkdir -p "$INSTALL_DIR"
    tar -xzf "${TMP_DIR}/${FILE_NAME}" -C "$TMP_DIR"

    # 升级时停止正在运行的服务
    if [ "$IS_UPGRADE" -eq 1 ] && [ "$HAS_SYSTEMD" -eq 1 ]; then
        if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
            warn "检测到服务正在运行，正在停止..."
            systemctl stop "$SERVICE_NAME"
        fi
    fi

    cp "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

    if [ "$IS_UPGRADE" -eq 1 ]; then
        ok "升级完成: ${INSTALL_DIR}/${BINARY_NAME}"
    else
        ok "安装完成: ${INSTALL_DIR}/${BINARY_NAME}"
    fi
}

# ============ 配置 systemd ============
setup_systemd() {
    info "正在配置 systemd 服务..."
    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Subs Check - 订阅检测转换工具
After=network-online.target
Wants=network-online.target
StartLimitBurst=5
StartLimitIntervalSec=60

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
Restart=always
RestartSec=10
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    ok "systemd 服务配置完成"
}

# ============ 交互式选择 ============
ask_enable() {
    printf "${YELLOW}是否设置开机自启动？[Y/n]: ${NC}"
    read -r answer < /dev/tty
    case "$answer" in
        [nN]|[nN][oO])
            info "跳过开机自启动"
            ;;
        *)
            systemctl enable "$SERVICE_NAME"
            ok "已设置开机自启动"
            ;;
    esac
}

ask_start() {
    printf "${YELLOW}是否立即启动服务？[Y/n]: ${NC}"
    read -r answer < /dev/tty
    case "$answer" in
        [nN]|[nN][oO])
            info "跳过启动服务"
            ;;
        *)
            systemctl start "$SERVICE_NAME"
            ok "服务已启动"
            ;;
    esac
}

# ============ 打印信息 ============
print_info() {
    printf "\n"
    printf "${GREEN}========================================${NC}\n"
    if [ "$IS_UPGRADE" -eq 1 ]; then
        printf "${GREEN} subs-check 升级成功！${NC}\n"
    else
        printf "${GREEN} subs-check 安装成功！${NC}\n"
    fi
    printf "${GREEN}========================================${NC}\n"
    printf "\n"
    printf "  版本:       %s\n" "$LATEST_VERSION"
    printf "  安装目录:   %s\n" "$INSTALL_DIR"
    printf "  配置文件:   %s/config/config.yaml\n" "$INSTALL_DIR"

    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        printf "  服务管理:\n"
        printf "    启动:     systemctl start %s\n" "$SERVICE_NAME"
        printf "    停止:     systemctl stop %s\n" "$SERVICE_NAME"
        printf "    重启:     systemctl restart %s\n" "$SERVICE_NAME"
        printf "    状态:     systemctl status %s\n" "$SERVICE_NAME"
        printf "    日志:     journalctl -u %s -f\n" "$SERVICE_NAME"
        printf "\n"
        printf "  卸载方法:\n"
        printf "    systemctl stop %s\n" "$SERVICE_NAME"
        printf "    systemctl disable %s\n" "$SERVICE_NAME"
        printf "    rm -rf %s %s\n" "$INSTALL_DIR" "$SERVICE_FILE"
        printf "    systemctl daemon-reload\n"
    else
        printf "\n"
        printf "${YELLOW}  未检测到 systemd，请手动运行：${NC}\n"
        printf "    cd %s && ./%s\n" "$INSTALL_DIR" "$BINARY_NAME"
        printf "\n"
        printf "  后台运行:\n"
        printf "    cd %s && nohup ./%s > subs-check.log 2>&1 &\n" "$INSTALL_DIR" "$BINARY_NAME"
        printf "\n"
        printf "  查看日志:\n"
        printf "    tail -f %s/subs-check.log\n" "$INSTALL_DIR"
        printf "\n"
        printf "  卸载方法:\n"
        printf "    rm -rf %s\n" "$INSTALL_DIR"
    fi
    printf "\n"
    printf "${YELLOW}  如需修改参数，请编辑配置文件：${NC}\n"
    printf "    %s/config/config.yaml\n" "$INSTALL_DIR"
    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        printf "  修改后重启服务：systemctl restart %s\n" "$SERVICE_NAME"
    else
        printf "  修改后重新运行程序即可生效\n"
    fi
    printf "\n"
}

# ============ 主流程 ============
main() {
    printf "\n"
    printf "${GREEN}========================================${NC}\n"
    printf "${GREEN} subs-check 一键安装脚本${NC}\n"
    printf "${GREEN}========================================${NC}\n"
    printf "\n"

    check_root
    check_os
    check_systemd
    check_existing
    check_download_tool
    detect_arch
    get_latest_version
    install_binary

    if [ "$HAS_SYSTEMD" -eq 1 ]; then
        setup_systemd
        ask_enable
        # 升级模式：询问是否重新启动；新安装：询问是否立即启动
        if [ "$IS_UPGRADE" -eq 1 ]; then
            printf "${YELLOW}是否重新启动服务？[Y/n]: ${NC}"
            read -r answer < /dev/tty
            case "$answer" in
                [nN]|[nN][oO])
                    info "跳过启动服务"
                    ;;
                *)
                    systemctl restart "$SERVICE_NAME"
                    ok "服务已重新启动"
                    ;;
            esac
        else
            ask_start
        fi
    fi

    print_info
}

main
