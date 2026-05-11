#!/bin/sh
# B4 Installer — Universal Linux installer with wizard interface
# Supports desktop Linux, OpenWRT, MerlinWRT, Keenetic, Mikrotik, Docker, and more
#
# AUTO-GENERATED — Do not edit directly
# Edit files in installer2/ and run: make build-installer
#

set -e

# Ensure sane PATH (Entware paths first for wget-ssl/curl from /opt/bin)
export PATH="/opt/bin:/opt/sbin:$HOME/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin:$PATH"
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    CYAN='\033[0;36m'
    MAGENTA='\033[0;35m'
    BOLD='\033[1m'
    DIM='\033[2m'
    NC='\033[0m'
else
    RED='' GREEN='' YELLOW='' BLUE='' CYAN='' MAGENTA='' BOLD='' DIM='' NC=''
fi
QUIET_MODE=0

log_info() {
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${BLUE}[INFO]${NC} %s\n" "$1" >&2
}

log_ok() {
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${GREEN}[ OK ]${NC} %s\n" "$1" >&2
}

log_warn() {
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${YELLOW}[WARN]${NC} %s\n" "$1" >&2
}

log_err() {
    printf "${RED}[ERR ]${NC} %s\n" "$1" >&2
}

log_header() {
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "\n${MAGENTA}${BOLD}%s${NC}\n" "$1" >&2
}

log_detail() {
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "  ${CYAN}%-22s${NC}: %b\n" "$1" "$2" >&2
}

log_sep() {
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${DIM}%s${NC}\n" "─────────────────────────────────────────" >&2
}
REPO_OWNER="DanielLavrushin"
REPO_NAME="b4"
BINARY_NAME="b4"
TEMP_DIR="/tmp/b4_install_$$"
WGET_INSECURE=""
PROXY_BASE_URL="https://proxy.lavrush.in/github"

B4_BIN_DIR=""
B4_DATA_DIR=""
B4_CONFIG_FILE=""
B4_SERVICE_TYPE=""
B4_SERVICE_DIR=""
B4_SERVICE_NAME=""
B4_PKG_MANAGER=""
B4_PLATFORM=""

command_exists() {
    command -v "$1" >/dev/null 2>&1 || which "$1" >/dev/null 2>&1
}

_byte_to_dec() {
    _btd_oct=$(od -b | head -1 | awk '{print $2}')
    [ -z "$_btd_oct" ] && return 1
    printf '%d\n' "0$_btd_oct"
}

check_root() {
    if [ "$(id -u 2>/dev/null)" = "0" ]; then
        return 0
    fi
    if [ "$USER" = "root" ]; then
        return 0
    fi
    if touch /etc/.b4_root_test 2>/dev/null; then
        rm -f /etc/.b4_root_test
        return 0
    fi
    log_err "This script must be run as root"
    exit 1
}

get_avail_kb() {
    _path="$1"
    df -Pk "$_path" 2>/dev/null | awk 'NR==2 {print $4}'
}

TEMP_MIN_KB=20000

setup_temp() {
    _tmp_avail=$(get_avail_kb /tmp)
    if [ -n "$_tmp_avail" ] && [ "$_tmp_avail" -gt "$TEMP_MIN_KB" ] 2>/dev/null; then
        TEMP_DIR="/tmp/b4_install_$$"
    else
        _fallback=""
        if [ -n "$B4_BIN_DIR" ] && [ -d "$B4_BIN_DIR" ] && [ -w "$B4_BIN_DIR" ]; then
            _fb_avail=$(get_avail_kb "$B4_BIN_DIR")
            if [ -n "$_fb_avail" ] && [ "$_fb_avail" -gt "$TEMP_MIN_KB" ] 2>/dev/null; then
                _fallback="$B4_BIN_DIR"
            fi
        fi
        for _fb_dir in /opt /var/tmp /root "$HOME"; do
            [ -z "$_fallback" ] || break
            [ -d "$_fb_dir" ] && [ -w "$_fb_dir" ] || continue
            _fb_avail=$(get_avail_kb "$_fb_dir")
            if [ -n "$_fb_avail" ] && [ "$_fb_avail" -gt "$TEMP_MIN_KB" ] 2>/dev/null; then
                _fallback="$_fb_dir"
            fi
        done
        if [ -z "$_fallback" ]; then
            log_err "Not enough disk space — /tmp has ${_tmp_avail:-?}KB free (need ${TEMP_MIN_KB}KB)"
            log_err "No writable fallback directory found."
            log_info "Free space or re-run with --bin-dir on external storage."
            exit 1
        else
            TEMP_DIR="${_fallback}/.b4_install_$$"
            log_info "Using ${_fallback} for temp files (/tmp too small)"
        fi
    fi

    rm -rf "$TEMP_DIR" 2>/dev/null || true
    mkdir -p "$TEMP_DIR" || {
        log_err "Cannot create temp dir: $TEMP_DIR"
        exit 1
    }
}

cleanup_temp() {
    rm -rf "$TEMP_DIR" 2>/dev/null || true
}

trap cleanup_temp EXIT INT TERM

detect_pkg_manager() {
    if [ -n "$B4_PKG_MANAGER" ]; then
        return 0
    fi
    if command_exists apt-get; then
        B4_PKG_MANAGER="apt"
    elif command_exists dnf; then
        B4_PKG_MANAGER="dnf"
    elif command_exists yum; then
        B4_PKG_MANAGER="yum"
    elif command_exists pacman; then
        B4_PKG_MANAGER="pacman"
    elif command_exists apk; then
        B4_PKG_MANAGER="apk"
    elif command_exists opkg; then
        B4_PKG_MANAGER="opkg"
    fi
}

pkg_install() {
    detect_pkg_manager
    case "$B4_PKG_MANAGER" in
    apt)
        apt-get update -qq >/dev/null 2>&1
        apt-get install -y -qq "$@" >/dev/null 2>&1
        ;;
    dnf) dnf install -y -q "$@" >/dev/null 2>&1 ;;
    yum) yum install -y -q "$@" >/dev/null 2>&1 ;;
    pacman) pacman -S --noconfirm --needed "$@" >/dev/null 2>&1 ;;
    apk) apk add --quiet "$@" >/dev/null 2>&1 ;;
    opkg)
        opkg update >/dev/null 2>&1
        opkg install "$@" >/dev/null 2>&1
        ;;
    *)
        log_warn "No package manager detected"
        return 1
        ;;
    esac
}

detect_architecture() {
    arch=$(uname -m)

    case "$arch" in
    x86_64 | amd64) echo "amd64" ;;
    i386 | i486 | i586 | i686) echo "386" ;;
    aarch64 | arm64) echo "arm64" ;;
    armv7 | armv7l)
        if [ -f /proc/cpuinfo ] &&
            grep -qE "(vfpv[3-9])" /proc/cpuinfo 2>/dev/null &&
            grep -qE "CPU architecture:\s*7" /proc/cpuinfo 2>/dev/null; then
            echo "armv7"
        else
            echo "armv5"
        fi
        ;;
    armv6*) echo "armv6" ;;
    armv5*) echo "armv5" ;;
    arm*)
        if [ -f /proc/cpuinfo ]; then
            if grep -qE "CPU architecture:\s*7" /proc/cpuinfo 2>/dev/null; then
                echo "armv7"
            elif grep -qE "CPU architecture:\s*6" /proc/cpuinfo 2>/dev/null; then
                echo "armv6"
            else
                echo "armv5"
            fi
        else
            echo "armv5"
        fi
        ;;
    mips64*)
        variant="mips64"
        if is_little_endian; then variant="mips64le"; fi
        if is_softfloat; then variant="${variant}_softfloat"; fi
        echo "$variant"
        ;;
    mips*)
        variant="mips"
        if is_little_endian; then variant="mipsle"; fi
        if is_softfloat; then variant="${variant}_softfloat"; fi
        echo "$variant"
        ;;
    ppc64le) echo "ppc64le" ;;
    ppc64) echo "ppc64" ;;
    riscv64) echo "riscv64" ;;
    s390x) echo "s390x" ;;
    loongarch64) echo "loong64" ;;
    *)
        log_err "Unsupported architecture: $arch"
        exit 1
        ;;
    esac
}

is_little_endian() {
    uname -m | grep -qi "el" && return 0
    [ -f /sys/kernel/cpu_byteorder ] && grep -qi "little" /sys/kernel/cpu_byteorder 2>/dev/null && return 0
    [ -f /proc/cpuinfo ] && grep -qi "little.endian\|byteorder.*little" /proc/cpuinfo 2>/dev/null && return 0
    command_exists opkg && opkg print-architecture 2>/dev/null | grep -qi "mipsel\|mips64el" && return 0
    [ "$(dd if=/bin/sh bs=1 skip=5 count=1 2>/dev/null | _byte_to_dec)" = "1" ] && return 0
    return 1
}

is_softfloat() {
    if [ -f /etc/openwrt_release ]; then
        _sf_owrt_arch=$(sed -n "s/^DISTRIB_ARCH=['\"\`]*\([^'\"\`]*\).*/\1/p" /etc/openwrt_release 2>/dev/null)
        if [ -n "$_sf_owrt_arch" ]; then
            case "$_sf_owrt_arch" in
            *_softfloat* | *_nofpu* | *soft*) return 0 ;;
            esac
            if echo "$_sf_owrt_arch" | grep -qE '_[a-z]*[0-9]+k?f$'; then
                return 1
            fi
            case "$_sf_owrt_arch" in
            mips_* | mipsel_* | mips64_* | mips64el_*) return 0 ;;
            esac
        fi
    fi
    if command_exists opkg; then
        _sf_opkg_arch="$(opkg print-architecture 2>/dev/null)"
        echo "$_sf_opkg_arch" | grep -qi "softfloat\|_nofpu\|soft_float" && return 0
        if echo "$_sf_opkg_arch" | grep -qiE "mips(el|64|64el)?_[a-z]*[0-9]+k?f( |$)"; then
            return 1
        fi
        echo "$_sf_opkg_arch" | grep -qi "mips" && return 0
    fi
    if [ -f /proc/cpuinfo ]; then
        grep -qi "nofpu\|no fpu\|soft.float" /proc/cpuinfo 2>/dev/null && return 0
    fi
    _sf_elf_bin=""
    for _sf_b in /bin/sh /bin/busybox /bin/ls; do
        [ -f "$_sf_b" ] && _sf_elf_bin="$_sf_b" && break
    done
    if [ -n "$_sf_elf_bin" ]; then
        _sf_ei_class=$(dd if="$_sf_elf_bin" bs=1 skip=4 count=1 2>/dev/null | _byte_to_dec)
        _sf_ei_data=$(dd if="$_sf_elf_bin" bs=1 skip=5 count=1 2>/dev/null | _byte_to_dec)
        _sf_flags_off=""
        [ "$_sf_ei_class" = "1" ] && _sf_flags_off=36
        [ "$_sf_ei_class" = "2" ] && _sf_flags_off=48
        if [ -n "$_sf_flags_off" ]; then
            if [ "$_sf_ei_data" = "1" ]; then
                _sf_check_off=$((_sf_flags_off + 1))
            else
                _sf_check_off=$((_sf_flags_off + 2))
            fi
            _sf_flag_byte=$(dd if="$_sf_elf_bin" bs=1 skip="$_sf_check_off" count=1 2>/dev/null | _byte_to_dec)
            if [ -n "$_sf_flag_byte" ]; then
                [ $((_sf_flag_byte & 8)) -ne 0 ] && return 0
                return 1
            fi
        fi
    fi
    if command_exists file; then
        _sf_file_out="$(file /bin/sh 2>/dev/null)"
        echo "$_sf_file_out" | grep -qi "soft.float" && return 0
        echo "$_sf_file_out" | grep -qi "MIPS\|ELF" && return 1
    fi
    if command_exists readelf; then
        readelf -A /bin/sh 2>/dev/null | grep -qi "soft.float\|softfloat" && return 0
    fi

    return 1
}

check_https_support() {
    if command_exists curl && curl -sI --max-time 5 "https://github.com" >/dev/null 2>&1; then
        return 0
    fi
    if command_exists wget && wget --spider -q --timeout=5 "https://github.com" 2>/dev/null; then
        return 0
    fi
    if command_exists wget && wget --spider -q --timeout=5 --no-check-certificate "https://github.com" 2>/dev/null; then
        WGET_INSECURE="--no-check-certificate"
        log_warn "HTTPS works only with --no-check-certificate (CA certs missing)"
        return 0
    fi
    return 1
}

ensure_https_support() {
    if check_https_support; then
        return 0
    fi
    log_warn "HTTPS not available — trying to install SSL support"
    if command_exists opkg; then
        opkg update >/dev/null 2>&1 || true
        opkg install ca-certificates >/dev/null 2>&1 || true
        opkg install wget-ssl >/dev/null 2>&1 || true
        hash -r 2>/dev/null || true
        if check_https_support; then return 0; fi
    fi
    log_err "HTTPS not available. Cannot download from GitHub."
    log_info "On Entware/OpenWrt: opkg install wget-ssl ca-certificates"
    return 1
}

convert_to_proxy_url() {
    url="$1"
    case "$url" in
    https://raw.githubusercontent.com/${REPO_OWNER}/* | \
        https://github.com/${REPO_OWNER}/* | \
        https://api.github.com/repos/${REPO_OWNER}/*)
        echo "${PROXY_BASE_URL}/${url}"
        ;;
    *) echo "$url" ;;
    esac
}

_wget_supports() {
    wget --help 2>&1 | grep -q "$1"
}

_do_fetch() {
    _fetch_url="$1"
    _fetch_out="$2"
    if [ -t 2 ] && [ "$QUIET_MODE" -ne 1 ]; then
        if command_exists curl && curl -fL --progress-bar --max-time 120 -o "$_fetch_out" "$_fetch_url" 2>&1; then return 0; fi
        if command_exists wget; then
            _wget_args="$WGET_INSECURE"
            _wget_supports "--show-progress" && _wget_args="$_wget_args --show-progress -q"
            _wget_supports "--timeout" && _wget_args="$_wget_args --timeout=120"
            wget $_wget_args -O "$_fetch_out" "$_fetch_url" 2>&1 && return 0
        fi
    else
        if command_exists curl && curl -sfL --max-time 120 -o "$_fetch_out" "$_fetch_url" 2>/dev/null; then return 0; fi
        if command_exists wget; then
            _wget_args="-q $WGET_INSECURE"
            _wget_supports "--timeout" && _wget_args="$_wget_args --timeout=120"
            wget $_wget_args -O "$_fetch_out" "$_fetch_url" 2>/dev/null && return 0
        fi
    fi
    return 1
}

fetch_file() {
    url="$1"
    output="$2"

    if ! command_exists curl && ! command_exists wget; then
        log_err "Neither curl nor wget found"
        return 1
    fi

    if _do_fetch "$url" "$output"; then return 0; fi

    proxy_url=$(convert_to_proxy_url "$url")
    if [ "$proxy_url" != "$url" ]; then
        log_warn "Direct download failed, trying proxy..."
        if _do_fetch "$proxy_url" "$output"; then return 0; fi
    fi

    log_err "Failed to download: $url"
    return 1
}

fetch_stdout() {
    url="$1"

    if command_exists curl; then
        result=$(curl -sfL --max-time 15 "$url" 2>/dev/null) && [ -n "$result" ] && echo "$result" && return 0
    fi
    if command_exists wget; then
        result=$(wget -qO- $WGET_INSECURE --timeout=15 "$url" 2>/dev/null) && [ -n "$result" ] && echo "$result" && return 0
    fi

    proxy_url=$(convert_to_proxy_url "$url")
    if [ "$proxy_url" != "$url" ]; then
        if command_exists curl; then
            result=$(curl -sfL --max-time 15 "$proxy_url" 2>/dev/null) && [ -n "$result" ] && echo "$result" && return 0
        fi
        if command_exists wget; then
            result=$(wget -qO- $WGET_INSECURE --timeout=15 "$proxy_url" 2>/dev/null) && [ -n "$result" ] && echo "$result" && return 0
        fi
    fi

    return 1
}

get_latest_version() {
    api_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
    version=$(fetch_stdout "$api_url" | grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4)
    if [ -z "$version" ]; then
        log_err "Failed to fetch latest version"
        exit 1
    fi
    echo "$version"
}

verify_checksum() {
    file="$1"
    checksum_url="$2"
    checksum_file="${file}.sha256"

    if ! fetch_file "$checksum_url" "$checksum_file"; then
        rm -f "$checksum_file"
        return 1
    fi

    expected=$(awk '{print $1}' "$checksum_file")
    rm -f "$checksum_file"
    [ -z "$expected" ] && return 1

    if ! command_exists sha256sum; then
        log_warn "sha256sum not found, skipping verification"
        return 1
    fi

    actual=$(sha256sum "$file" | awk '{print $1}')
    if [ "$expected" = "$actual" ]; then
        log_ok "SHA256 verified: $actual"
        return 0
    else
        log_err "SHA256 mismatch! Expected: $expected Got: $actual"
        return 2
    fi
}

is_lxc_container() {
    if [ -f /proc/1/environ ]; then
        tr '\0' '\n' </proc/1/environ 2>/dev/null | grep -q '^container=lxc' && return 0
    fi
    [ -f /run/systemd/container ] && grep -q "lxc" /run/systemd/container 2>/dev/null && return 0
    return 1
}

_kmod_builtin() {
    _mod="$1"
    _kver=$(uname -r)
    for _f in "/lib/modules/${_kver}/modules.builtin" "/lib/modules/${_kver}/modules.builtin.modinfo"; do
        [ -f "$_f" ] && grep -q "${_mod}" "$_f" 2>/dev/null && return 0
    done
    [ -d "/sys/module/${_mod}" ] && return 0
    return 1
}

_kmod_available() {
    lsmod 2>/dev/null | grep -q "^$1" && return 0
    _kmod_builtin "$1" && return 0
    return 1
}

_nft_functional() {
    command_exists nft || return 1
    nft list ruleset >/dev/null 2>&1
}

is_b4_running() {
    for pf in /var/run/b4.pid /opt/var/run/b4.pid; do
        if [ -f "$pf" ]; then
            _pid=$(cat "$pf" 2>/dev/null)
            [ -n "$_pid" ] && kill -0 "$_pid" 2>/dev/null && return 0
        fi
    done
    if command_exists pgrep; then
        pgrep -x "$BINARY_NAME" >/dev/null 2>&1 && return 0
    fi
    _mypid=$$
    _ps_out=$(ps w 2>/dev/null || ps 2>/dev/null) || true
    if [ -n "$_ps_out" ]; then
        echo "$_ps_out" | grep -v grep | grep -v "$_mypid" | grep -q "[/ ]${BINARY_NAME}$" && return 0
        echo "$_ps_out" | grep -v grep | grep -v "$_mypid" | grep -q "[/ ]${BINARY_NAME} " && return 0
    fi
    return 1
}

stop_b4() {
    if ! is_b4_running; then return 0; fi
    log_info "Stopping running b4 process..."
    for pf in /var/run/b4.pid /opt/var/run/b4.pid; do
        if [ -f "$pf" ]; then
            _pid=$(cat "$pf" 2>/dev/null)
            [ -n "$_pid" ] && kill "$_pid" 2>/dev/null || true
        fi
    done
    if command_exists pkill; then
        pkill -x "$BINARY_NAME" 2>/dev/null || true
    fi
    sleep 2
}

is_writable_dir() {
    dir="$1"
    [ -d "$dir" ] && [ -w "$dir" ] && return 0
    mkdir -p "$dir" 2>/dev/null && [ -w "$dir" ] && return 0
    return 1
}

ensure_dir() {
    dir="$1"
    label="$2"
    if ! mkdir -p "$dir" 2>/dev/null; then
        log_err "Cannot create ${label}: ${dir}"
        return 1
    fi
    if [ ! -w "$dir" ]; then
        log_err "${label} not writable: ${dir}"
        return 1
    fi
    return 0
}

check_exit() {
    case "$1" in
    [eEqQ] | exit | EXIT | quit | QUIT)
        echo ""
        log_info "Aborted by user."
        exit 0
        ;;
    esac
}

_INPUT=""
read_input() {
    prompt="$1"
    default="$2"
    if [ "$QUIET_MODE" -eq 1 ] 2>/dev/null; then
        _INPUT="$default"
        return 0
    fi
    printf "${CYAN}%b${NC}" "$prompt" >&2
    read _INPUT || _INPUT="$default"
    _INPUT=$(printf '%s' "$_INPUT" | tr -d '\r')
    check_exit "$_INPUT"
    [ -z "$_INPUT" ] && _INPUT="$default"
    return 0
}

confirm() {
    prompt="$1"
    default="${2:-y}" # default yes

    if [ "$default" = "y" ]; then
        hint="Y/n/e"
    else
        hint="y/N/e"
    fi

    read_input "${prompt} (${hint}): " "$default"

    case "$_INPUT" in
    [yY] | [yY][eE][sS]) return 0 ;;
    [nN] | [nN][oO]) return 1 ;;
    *) [ "$default" = "y" ] && return 0 || return 1 ;;
    esac
}
WIZARD_MODE="" # "auto" or "manual"

wizard_start() {
    echo ""
    printf "${BOLD}"
    echo "  ╔═══════════════════════════════════════╗"
    echo "  ║       B4 Universal Installer          ║"
    echo "  ╚═══════════════════════════════════════╝"
    printf "${NC}"
    echo ""

    while true; do
        log_sep
        echo ""
        printf "  ${BOLD}1${NC}) Automatic detection ${DIM}(recommended)${NC}\n"
        printf "  ${BOLD}2${NC}) Manual configuration\n"
        printf "  ${BOLD}3${NC}) System info\n"
        printf "  ${DIM}e) Exit${NC}\n"
        echo ""

        read_input "Select mode [1]: " "1"

        case "$_INPUT" in
        2) WIZARD_MODE="manual"; return 0 ;;
        3)
            action_sysinfo
            echo ""
            read_input "Press Enter to return to menu..." ""
            echo ""
            ;;
        *) WIZARD_MODE="auto"; return 0 ;;
        esac
    done
}

wizard_auto_detect() {
    log_header "Detecting system..."
    echo ""

    platform_auto_detect
    if [ -z "$B4_PLATFORM" ]; then
        log_err "Could not detect platform"
        log_info "Try manual mode or set B4_PLATFORM environment variable"
        exit 1
    fi

    _user_bin_dir="$B4_BIN_DIR"
    _user_data_dir="$B4_DATA_DIR"
    platform_call info
    [ -n "$_user_bin_dir" ] && B4_BIN_DIR="$_user_bin_dir"
    [ -n "$_user_data_dir" ] && B4_DATA_DIR="$_user_data_dir"
    [ -n "$_user_data_dir" ] && B4_CONFIG_FILE="${_user_data_dir}/b4.json"

    B4_ARCH=$(detect_architecture)

    detect_pkg_manager

    wizard_show_config

    echo ""
    if ! confirm "Proceed with these settings?"; then
        log_info "Switching to manual mode..."
        WIZARD_MODE="manual"
        wizard_manual_configure
    fi
}

wizard_manual_configure() {
    log_header "Manual configuration"
    echo ""

    while true; do
        echo "  Available platforms:"
        idx=1
        for p in $REGISTERED_PLATFORMS; do
            pname=$(platform_dispatch "$p" name)
            printf "    ${BOLD}%d${NC}) %s\n" "$idx" "$pname"
            idx=$((idx + 1))
        done
        echo ""

        read_input "Select platform [1]: " "1"
        idx=1
        for p in $REGISTERED_PLATFORMS; do
            if [ "$idx" = "$_INPUT" ]; then
                B4_PLATFORM="$p"
                break
            fi
            idx=$((idx + 1))
        done

        if [ -n "$B4_PLATFORM" ]; then
            break
        fi
        log_warn "Invalid selection, please try again"
        echo ""
    done

    platform_call info

    read_input "Binary directory [${B4_BIN_DIR}]: " "$B4_BIN_DIR"
    B4_BIN_DIR="$_INPUT"

    read_input "Data directory [${B4_DATA_DIR}]: " "$B4_DATA_DIR"
    B4_DATA_DIR="$_INPUT"
    B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"

    echo ""
    echo "  Service types: systemd, openrc, procd, sysv, entware, none"
    read_input "Service type [${B4_SERVICE_TYPE}]: " "$B4_SERVICE_TYPE"
    B4_SERVICE_TYPE="$_INPUT"

    auto_arch=$(detect_architecture)
    B4_SUPPORTED_ARCHS="amd64 arm64 armv7 armv6 armv5 386 mips mipsle mips_softfloat mipsle_softfloat mips64 mips64le loong64 ppc64 ppc64le riscv64 s390x"

    _arch_default=1
    _arch_idx=1
    for a in $B4_SUPPORTED_ARCHS; do
        if [ "$a" = "$auto_arch" ]; then
            _arch_default=$_arch_idx
            break
        fi
        _arch_idx=$((_arch_idx + 1))
    done

    while true; do
        echo "  Available architectures:"
        _arch_idx=1
        for a in $B4_SUPPORTED_ARCHS; do
            if [ "$a" = "$auto_arch" ]; then
                printf "    ${BOLD}%2d${NC}) %s ${DIM}(detected)${NC}\n" "$_arch_idx" "$a"
            else
                printf "    ${BOLD}%2d${NC}) %s\n" "$_arch_idx" "$a"
            fi
            _arch_idx=$((_arch_idx + 1))
        done
        echo ""

        read_input "Select architecture [${_arch_default}]: " "$_arch_default"
        _arch_idx=1
        B4_ARCH=""
        for a in $B4_SUPPORTED_ARCHS; do
            if [ "$_arch_idx" = "$_INPUT" ]; then
                B4_ARCH="$a"
                break
            fi
            _arch_idx=$((_arch_idx + 1))
        done

        if [ -n "$B4_ARCH" ]; then
            break
        fi
        log_warn "Invalid selection, please try again"
        echo ""
    done

    detect_pkg_manager
    read_input "Package manager [${B4_PKG_MANAGER:-none}]: " "$B4_PKG_MANAGER"
    B4_PKG_MANAGER="$_INPUT"

    echo ""
    wizard_show_config
    echo ""
    if ! confirm "Proceed with these settings?"; then
        log_info "Aborted."
        exit 0
    fi
}

wizard_show_config() {
    log_sep
    pname=""
    if [ -n "$B4_PLATFORM" ]; then
        pname=$(platform_dispatch "$B4_PLATFORM" name)
    fi
    log_detail "Platform" "${BOLD}${pname}${NC} (${B4_PLATFORM})"
    log_detail "Architecture" "${B4_ARCH}"
    log_detail "Binary directory" "${B4_BIN_DIR}"
    log_detail "Data directory" "${B4_DATA_DIR}"
    log_detail "Config file" "${B4_CONFIG_FILE}"
    log_detail "Service type" "${B4_SERVICE_TYPE}"
    log_detail "Package manager" "${B4_PKG_MANAGER:-none}"

    if [ -n "$REGISTERED_FEATURES" ]; then
        echo ""
        log_detail "Features" ""
        for f in $REGISTERED_FEATURES; do
            fname=$(feature_dispatch "$f" name)
            fdesc=$(feature_dispatch "$f" description)
            printf "    ${GREEN}+${NC} %s ${DIM}— %s${NC}\n" "$fname" "$fdesc" >&2
        done
    fi
    log_sep
}

wizard_select_features() {
    if [ -z "$REGISTERED_FEATURES" ]; then
        return 0
    fi

    log_header "Optional features"
    echo ""

    for f in $REGISTERED_FEATURES; do
        fname=$(feature_dispatch "$f" name)
        fdesc=$(feature_dispatch "$f" description)
        fdefault=$(feature_dispatch "$f" default_enabled)

        if [ "$fdefault" = "yes" ]; then
            def="y"
        else
            def="n"
        fi

        if confirm "  Enable ${BOLD}${fname}${NC}? ${DIM}(${fdesc})${NC}" "$def"; then
            ENABLED_FEATURES="${ENABLED_FEATURES} ${f}"
        fi
    done
}
REGISTERED_PLATFORMS=""

register_platform() {
    id="$1"
    REGISTERED_PLATFORMS="${REGISTERED_PLATFORMS} ${id}"
}

platform_call() {
    func="$1"
    shift
    platform_dispatch "$B4_PLATFORM" "$func" "$@"
}

platform_dispatch() {
    pid="$1"
    func="$2"
    shift 2
    fn="platform_${pid}_${func}"
    if type "$fn" >/dev/null 2>&1; then
        "$fn" "$@"
    else
        log_warn "Platform '${pid}' does not implement '${func}'"
        return 1
    fi
}
platform_auto_detect() {
    if [ -n "$B4_PLATFORM" ]; then
        for p in $REGISTERED_PLATFORMS; do
            if [ "$p" = "$B4_PLATFORM" ]; then
                log_ok "Using user-specified platform: $B4_PLATFORM"
                return 0
            fi
        done
        log_err "Unknown platform: $B4_PLATFORM"
        log_info "Available: $REGISTERED_PLATFORMS"
        exit 1
    fi

    _fallback=""
    for p in $REGISTERED_PLATFORMS; do
        [ "$p" = "generic_linux" ] && _fallback="generic_linux" && continue
        if platform_dispatch "$p" match 2>/dev/null; then
            B4_PLATFORM="$p"
            pname=$(platform_dispatch "$p" name)
            log_ok "Detected platform: ${pname}"
            return 0
        fi
    done

    if [ -n "$_fallback" ] && platform_dispatch "generic_linux" match 2>/dev/null; then
        B4_PLATFORM="generic_linux"
        log_ok "Detected platform: Generic Linux"
        return 0
    fi

    if [ -n "$_fallback" ]; then
        B4_PLATFORM="generic_linux"
        log_warn "Could not detect specific platform, defaulting to Generic Linux"
        return 0
    fi

    return 1
}
platform_generic_linux_name() {
    echo "Generic Linux (Ubuntu/Debian/Fedora/Arch/Alpine)"
}

platform_generic_linux_match() {
    [ "$(uname -s)" = "Linux" ] || return 1

    [ -f /etc/openwrt_release ] && return 1
    [ -f /etc/merlinwrt_release ] && return 1
    [ -d /jffs ] && [ -d /opt/etc/init.d ] && return 1 # Merlin with Entware
    [ -d /etc/storage ] && [ -d /etc_ro ] && return 1  # Padavan
    [ -d /var/run/ndm ] && return 1                    # Keenetic NDMS
    command_exists ndmc && return 1                    # Keenetic NDMS
    command_exists nvram && nvram get firmver 2>/dev/null | grep -qi "merlin" && return 1
    [ -f /proc/device-tree/model ] && grep -qi "keenetic" /proc/device-tree/model 2>/dev/null && return 1

    command_exists systemctl && return 0
    [ -d /etc/init.d ] && return 0

    return 0
}

platform_generic_linux_info() {
    B4_BIN_DIR="/usr/local/bin"
    B4_DATA_DIR="/etc/b4"
    B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"

    if command_exists systemctl && systemctl list-units >/dev/null 2>&1; then
        B4_SERVICE_TYPE="systemd"
        B4_SERVICE_DIR="/etc/systemd/system"
        B4_SERVICE_NAME="b4.service"
    elif [ -f /sbin/openrc-run ] || command_exists openrc-run; then
        B4_SERVICE_TYPE="openrc"
        B4_SERVICE_DIR="/etc/init.d"
        B4_SERVICE_NAME="b4"
    elif [ -d /etc/init.d ]; then
        B4_SERVICE_TYPE="sysv"
        B4_SERVICE_DIR="/etc/init.d"
        B4_SERVICE_NAME="b4"
    else
        B4_SERVICE_TYPE="none"
    fi

    detect_pkg_manager
}

platform_generic_linux_check_deps() {
    _generic_linux_check_lxc

    missing=""

    if ! command_exists curl && ! command_exists wget; then
        missing="${missing} wget"
    fi
    command_exists tar || missing="${missing} tar"

    if [ -n "$missing" ]; then
        log_warn "Missing required:${missing}"
        if confirm "Install missing packages?"; then
            pkg_install $missing || log_warn "Some packages failed to install"
        else
            log_err "Cannot continue without:${missing}"
            exit 1
        fi
    fi

    ensure_https_support || exit 1

    _generic_linux_check_kmods

    _generic_linux_check_recommended
}

_generic_linux_check_lxc() {
    is_lxc_container || return 0

    echo ""
    log_warn "Running inside an LXC container"
    log_info "B4 requires netfilter/NFQUEUE support from the host kernel."
    log_info "The LXC container config (on the host) must include:"
    echo "" >&2
    printf "  ${BOLD}lxc.cgroup2.devices.allow: c 10:200 rwm${NC}\n" >&2
    printf "  ${BOLD}lxc.mount.entry: /dev/net/tun dev/net/tun none bind,create=file${NC}\n" >&2
    printf "  ${BOLD}lxc.prlimit.nofile: 1048576${NC}\n" >&2
    printf "  ${BOLD}features: nesting=1,keyctl=1${NC}\n" >&2
    echo "" >&2
    log_info "On Proxmox: edit /etc/pve/lxc/<CTID>.conf and restart the container."
    echo ""

    if ! confirm "Continue installation?"; then
        log_info "Aborted. Apply the LXC config changes first, then re-run the installer."
        exit 0
    fi
}

_generic_linux_check_kmods() {
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        _kmod_available "$mod" && continue
        modprobe "$mod" 2>/dev/null || true
    done

    if ! _kmod_available "xt_NFQUEUE" && ! _kmod_available "nfnetlink_queue" && ! _kmod_available "nft_queue"; then
        log_warn "No netfilter queue module available"
        case "$B4_PKG_MANAGER" in
        apt) log_info "Try: apt install xtables-addons-common" ;;
        dnf | yum) log_info "Try: dnf install xtables-addons" ;;
        pacman) log_info "Try: pacman -S xtables-addons" ;;
        apk) log_info "Try: apk add iptables-nft" ;;
        *) ;;
        esac
    fi
}

_generic_linux_check_recommended() {
    rec_missing=""
    command_exists jq || rec_missing="${rec_missing} jq"
    if ! command_exists iptables && ! command_exists nft; then
        if [ "$B4_PKG_MANAGER" = "apk" ]; then
            rec_missing="${rec_missing} nftables"
        else
            rec_missing="${rec_missing} iptables"
        fi
    fi

    if command_exists iptables && ! _nft_functional && ! command_exists ipset; then
        rec_missing="${rec_missing} ipset"
    fi

    if [ -n "$rec_missing" ]; then
        log_warn "Recommended but missing:${rec_missing}"
        if confirm "Install recommended packages?"; then
            pkg_install $rec_missing || true
        fi
    fi
}

platform_generic_linux_find_storage() {
    return 0
}

register_platform "generic_linux"
platform_keenetic_name() {
    echo "Keenetic (NDMS)"
}

platform_keenetic_match() {
    if [ -f /proc/device-tree/model ] && grep -qi "keenetic" /proc/device-tree/model 2>/dev/null; then
        return 0
    fi

    if [ -d /var/run/ndm ] || command_exists ndmc; then
        return 0
    fi

    if [ -d "/opt/sbin" ] && [ -w "/opt/sbin" ] && [ ! -w "/etc" ] &&
        [ ! -d "/jffs" ] && [ ! -f /etc/openwrt_release ]; then
        [ -d /tmp/ndm ] && return 0
    fi

    return 1
}

platform_keenetic_info() {
    B4_BIN_DIR="/opt/sbin"
    B4_DATA_DIR="/opt/etc/b4"
    B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
    B4_SERVICE_TYPE="entware"
    B4_SERVICE_DIR="/opt/etc/init.d"
    B4_SERVICE_NAME="S99b4"
    B4_PKG_MANAGER="opkg"

    if [ ! -d "/opt/etc/init.d" ] && [ ! -f "/opt/bin/opkg" ]; then
        log_warn "Entware not detected!"
        log_info "Entware is required on Keenetic. To install:"
        log_info "  1. Go to router admin panel > System Settings"
        log_info "  2. Enable OPKG package manager component"
        log_info "  3. For older models: plug in a USB drive and install Entware"
        log_info "  More info: https://help.keenetic.com/hc/en-us/articles/360021214160"

        if [ -d "/tmp" ] && [ -w "/tmp" ]; then
            log_warn "Falling back to /tmp (non-persistent, will not survive reboot)"
            B4_BIN_DIR="/tmp/b4"
            B4_DATA_DIR="/tmp/b4"
            B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
            B4_SERVICE_TYPE="none"
        fi
    fi
}

platform_keenetic_check_deps() {
    if ! command_exists curl && ! command_exists wget; then
        log_warn "Neither curl nor wget found"
        if command_exists opkg; then
            log_info "Installing wget-ssl..."
            pkg_install wget-ssl || true
        fi
    fi

    command_exists tar || {
        log_warn "tar not found"
        command_exists opkg && pkg_install tar || true
    }

    ensure_https_support || exit 1

    _keenetic_load_kmods

    _keenetic_check_recommended
}

_keenetic_load_kmods() {
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        _kmod_available "$mod" && continue
        modprobe "$mod" 2>/dev/null && continue
        kver=$(uname -r)
        mod_path=$(find /lib/modules/"$kver" -name "${mod}.ko*" 2>/dev/null | head -1)
        [ -n "$mod_path" ] && insmod "$mod_path" 2>/dev/null || true
    done

    if ! _kmod_available "xt_NFQUEUE" && ! _kmod_available "nfnetlink_queue" && ! _kmod_available "nft_queue"; then
        log_warn "No netfilter queue module available — b4 may not work"
        log_info "Check that your Keenetic firmware supports Netfilter Queue"
        log_info "You may need to enable 'Kernel modules for Netfilter' in the package manager"
    fi

    if ! _kmod_available "xt_connbytes" && ! _nft_functional; then
        log_warn "xt_connbytes kernel module not available — b4 will fail to start on iptables"
        log_info "Enable it via router web UI: System settings > Component options"
        log_info "  look for 'Netfilter kernel modules' / 'xtables-addons' / 'Connection tracking extensions'"
        log_info "Or try (Entware, kernel must match):"
        log_info "  opkg update && opkg install kmod-ipt-conntrack-extra"
    fi
}

_keenetic_check_recommended() {
    if ! command_exists opkg; then
        log_warn "opkg not available — cannot install recommended packages"
        return 0
    fi

    rec_missing=""
    command_exists jq || rec_missing="${rec_missing} jq"
    command_exists iptables || rec_missing="${rec_missing} iptables"
    command_exists ipset || rec_missing="${rec_missing} ipset"
    command_exists nohup || rec_missing="${rec_missing} coreutils-nohup"

    if ! opkg list-installed 2>/dev/null | grep -q "^ca-certificates "; then
        rec_missing="${rec_missing} ca-certificates"
    fi
    if ! opkg list-installed 2>/dev/null | grep -q "^wget-ssl "; then
        if ! command_exists curl || ! curl -sI --max-time 3 "https://github.com" >/dev/null 2>&1; then
            rec_missing="${rec_missing} wget-ssl"
        fi
    fi

    if [ -n "$rec_missing" ]; then
        log_warn "Recommended but missing:${rec_missing}"
        if confirm "Install recommended packages?"; then
            opkg update >/dev/null 2>&1 || true
            for pkg in $rec_missing; do
                log_info "Installing ${pkg}..."
                opkg install "$pkg" >/dev/null 2>&1 && log_ok "Installed ${pkg}" || log_warn "Failed: ${pkg}"
            done
        fi
    fi
}

platform_keenetic_find_storage() {

    if [ -d "/opt" ] && [ -w "/opt" ]; then
        return 0
    fi

    log_err "No writable persistent storage found (/opt not available)"
    log_info "Ensure Entware is installed:"
    log_info "  - Newer models: Enable OPKG in system settings"
    log_info "  - Older models: Plug in a USB drive and install Entware"
    return 1
}

register_platform "keenetic"
platform_merlinwrt_name() {
    echo "Asus Merlin (Asuswrt-Merlin)"
}

platform_merlinwrt_match() {

    if command_exists nvram; then
        fw=$(nvram get firmver 2>/dev/null)
        bw=$(nvram get buildno 2>/dev/null)
        if echo "$fw $bw" | grep -qi "merlin"; then
            return 0
        fi
    fi

    if [ -d "/jffs" ] && [ -w "/jffs" ] && [ -d "/opt/etc/init.d" ]; then
        [ -f "/opt/etc/init.d/rc.func" ] && return 0
    fi

    [ -f "/etc/merlinwrt_release" ] && return 0

    return 1
}

platform_merlinwrt_info() {
    B4_BIN_DIR="/opt/sbin"
    B4_DATA_DIR="/opt/etc/b4"
    B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
    B4_SERVICE_TYPE="entware"
    B4_SERVICE_DIR="/opt/etc/init.d"
    B4_SERVICE_NAME="S99b4"
    B4_PKG_MANAGER="opkg"

    if [ ! -d "/opt/etc/init.d" ]; then
        log_warn "Entware not detected!"
        log_info "Entware is required for MerlinWRT. Install it first:"
        log_info "  1. Plug in a USB drive and format it via the router admin panel"
        log_info "  2. Open SSH and run: amtm"
        log_info "  3. Select option 'ep' to install Entware"
        log_info "  More info: https://diversion.ch/amtm.html"

        if [ -d "/jffs" ] && [ -w "/jffs" ]; then
            log_warn "Falling back to /jffs (limited space, Entware recommended)"
            B4_BIN_DIR="/jffs/b4"
            B4_DATA_DIR="/jffs/b4"
            B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
            B4_SERVICE_TYPE="none"
        fi
    fi
}

platform_merlinwrt_check_deps() {
    if ! command_exists curl && ! command_exists wget; then
        log_warn "Neither curl nor wget found"
        if command_exists opkg; then
            log_info "Installing wget-ssl..."
            pkg_install wget-ssl || true
        fi
    fi

    command_exists tar || {
        log_warn "tar not found"
        command_exists opkg && pkg_install tar || true
    }

    ensure_https_support || exit 1

    _merlinwrt_load_kmods

    _merlinwrt_check_recommended
}

_merlinwrt_load_kmods() {
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        _kmod_available "$mod" && continue
        modprobe "$mod" 2>/dev/null && continue
        kver=$(uname -r)
        mod_path=$(find /lib/modules/"$kver" -name "${mod}.ko*" 2>/dev/null | head -1)
        [ -n "$mod_path" ] && insmod "$mod_path" 2>/dev/null || true
    done

    if ! _kmod_available "xt_NFQUEUE" && ! _kmod_available "nfnetlink_queue" && ! _kmod_available "nft_queue"; then
        log_warn "No netfilter queue module available — b4 may not work"
        log_info "Check your firmware version supports NFQUEUE"
    fi
}

_merlinwrt_check_recommended() {
    if ! command_exists opkg; then
        log_warn "opkg not available — cannot install recommended packages"
        return 0
    fi

    rec_missing=""
    command_exists jq || rec_missing="${rec_missing} jq"
    command_exists iptables || rec_missing="${rec_missing} iptables"
    command_exists ipset || rec_missing="${rec_missing} ipset"
    command_exists nohup || rec_missing="${rec_missing} coreutils-nohup"

    if ! opkg list-installed 2>/dev/null | grep -q "^ca-certificates "; then
        rec_missing="${rec_missing} ca-certificates"
    fi

    if [ -n "$rec_missing" ]; then
        log_warn "Recommended but missing:${rec_missing}"
        if confirm "Install recommended packages?"; then
            opkg update >/dev/null 2>&1 || true
            for pkg in $rec_missing; do
                log_info "Installing ${pkg}..."
                opkg install "$pkg" >/dev/null 2>&1 && log_ok "Installed ${pkg}" || log_warn "Failed: ${pkg}"
            done
        fi
    fi
}

platform_merlinwrt_find_storage() {

    if [ -d "/opt" ] && [ -w "/opt" ]; then
        return 0
    fi

    if [ -d "/jffs" ] && [ -w "/jffs" ]; then
        log_warn "Entware /opt not available, using /jffs (limited space)"
        B4_BIN_DIR="/jffs/b4"
        B4_DATA_DIR="/jffs/b4"
        B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
        return 0
    fi

    log_err "No writable persistent storage found"
    log_info "Please install Entware via amtm (run 'amtm' in SSH, select 'ep')"
    return 1
}

register_platform "merlinwrt"
platform_openwrt_name() {
    echo "OpenWrt"
}

platform_openwrt_match() {
    [ -f /etc/openwrt_release ] && return 0

    if [ -f /etc/os-release ]; then
        grep -qi "openwrt" /etc/os-release 2>/dev/null && return 0
    fi

    [ -f /etc/board.json ] && return 0

    return 1
}

platform_openwrt_info() {
    B4_BIN_DIR="/usr/bin"
    B4_DATA_DIR="/etc/b4"
    B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
    if command_exists apk; then
        B4_PKG_MANAGER="apk"
    else
        B4_PKG_MANAGER="opkg"
    fi

    if [ -f /sbin/procd ] || command_exists procd; then
        B4_SERVICE_TYPE="procd"
        B4_SERVICE_DIR="/etc/init.d"
        B4_SERVICE_NAME="b4"
    elif [ -d /etc/init.d ]; then
        B4_SERVICE_TYPE="sysv"
        B4_SERVICE_DIR="/etc/init.d"
        B4_SERVICE_NAME="b4"
    else
        B4_SERVICE_TYPE="none"
    fi

    if [ -d "/opt" ] && [ -w "/opt" ]; then
        _opt_avail=$(df /opt 2>/dev/null | tail -1 | awk '{print $4}')
        if [ -n "$_opt_avail" ] && [ "$_opt_avail" -gt 10000 ] 2>/dev/null; then
            B4_BIN_DIR="/opt/bin"
            B4_DATA_DIR="/opt/etc/b4"
            B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
        fi
    fi

    if [ "$B4_BIN_DIR" = "/usr/bin" ]; then
        for mnt in /mnt/sda1 /mnt/sda2 /mnt/mmcblk* /mnt/usb*; do
            if [ -d "$mnt" ] && [ -w "$mnt" ]; then
                _mnt_avail=$(df "$mnt" 2>/dev/null | tail -1 | awk '{print $4}')
                if [ -n "$_mnt_avail" ] && [ "$_mnt_avail" -gt 10000 ] 2>/dev/null; then
                    log_info "External storage found: $mnt"
                    B4_BIN_DIR="${mnt}/b4"
                    B4_DATA_DIR="${mnt}/b4"
                    B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
                    break
                fi
            fi
        done
    fi
}

platform_openwrt_check_deps() {
    if ! command_exists curl && ! command_exists wget; then
        log_warn "Neither curl nor wget found"
        log_info "Installing wget-ssl..."
        pkg_install wget-ssl ca-certificates || true
    fi

    command_exists tar || {
        log_warn "tar not found"
        pkg_install tar || true
    }

    ensure_https_support || exit 1

    _openwrt_load_kmods

    _openwrt_check_recommended
}

_openwrt_load_kmods() {
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        _kmod_available "$mod" && continue
        modprobe "$mod" 2>/dev/null && continue
        kver=$(uname -r)
        mod_path=$(find /lib/modules/"$kver" -name "${mod}.ko*" 2>/dev/null | head -1)
        [ -n "$mod_path" ] && insmod "$mod_path" 2>/dev/null || true
    done

    if ! _kmod_available "xt_NFQUEUE" && ! _kmod_available "nfnetlink_queue" && ! _kmod_available "nft_queue"; then
        log_warn "No netfilter queue module available — b4 may not work"
        if [ "$B4_PKG_MANAGER" = "apk" ]; then
            log_info "Try: apk add kmod-nft-queue kmod-nft-nat kmod-nft-compat"
        else
            log_info "Try: opkg install kmod-nfnetlink-queue kmod-ipt-nfqueue iptables-mod-nfqueue kmod-ipt-conntrack-extra iptables-mod-conntrack-extra"
        fi
    fi
}

_openwrt_check_recommended() {
    rec_missing=""
    command_exists jq || rec_missing="${rec_missing} jq"
    if ! command_exists iptables && ! command_exists nft; then
        if [ "$B4_PKG_MANAGER" = "apk" ]; then
            rec_missing="${rec_missing} nftables"
        else
            rec_missing="${rec_missing} iptables"
        fi
    fi

    if [ "$B4_PKG_MANAGER" = "apk" ]; then
        if ! _kmod_available "nft_queue"; then
            rec_missing="${rec_missing} kmod-nft-queue"
        fi
        if ! _kmod_available "nf_nat"; then
            rec_missing="${rec_missing} kmod-nft-nat"
        fi
    fi

    if ! _kmod_available "nft_tproxy"; then
        rec_missing="${rec_missing} kmod-nft-tproxy"
    fi
    if ! _kmod_available "nft_socket"; then
        rec_missing="${rec_missing} kmod-nft-socket"
    fi

    if ! _nft_functional; then
        if ! command_exists ipset; then
            rec_missing="${rec_missing} ipset"
            if [ "$B4_PKG_MANAGER" = "opkg" ]; then
                _kmod_available "ip_set" || rec_missing="${rec_missing} kmod-ipt-ipset"
            fi
        fi
        if ! _kmod_available "xt_connbytes" && [ "$B4_PKG_MANAGER" = "opkg" ]; then
            rec_missing="${rec_missing} kmod-ipt-conntrack-extra iptables-mod-conntrack-extra"
        fi
    fi

    if ! command_exists curl || ! curl -sI --max-time 3 "https://github.com" >/dev/null 2>&1; then
        if [ "$B4_PKG_MANAGER" = "apk" ]; then
            command_exists wget || rec_missing="${rec_missing} wget"
            [ -d /etc/ssl/certs ] && [ -n "$(ls /etc/ssl/certs/ 2>/dev/null)" ] || rec_missing="${rec_missing} ca-certificates"
        else
            if ! opkg list-installed 2>/dev/null | grep -q "^ca-certificates "; then
                rec_missing="${rec_missing} ca-certificates"
            fi
            if ! opkg list-installed 2>/dev/null | grep -q "^wget-ssl "; then
                rec_missing="${rec_missing} wget-ssl"
            fi
        fi
    fi

    if [ -n "$rec_missing" ]; then
        log_warn "Recommended but missing:${rec_missing}"
        if confirm "Install recommended packages?"; then
            if [ "$B4_PKG_MANAGER" = "apk" ]; then
                for pkg in $rec_missing; do
                    log_info "Installing ${pkg}..."
                    apk add "$pkg" >/dev/null 2>&1 && log_ok "Installed ${pkg}" || log_warn "Failed: ${pkg}"
                done
            else
                opkg update >/dev/null 2>&1 || true
                for pkg in $rec_missing; do
                    log_info "Installing ${pkg}..."
                    opkg install "$pkg" >/dev/null 2>&1 && log_ok "Installed ${pkg}" || log_warn "Failed: ${pkg}"
                done
            fi
        fi
    fi
}

platform_openwrt_find_storage() {

    if [ -d "/opt" ] && [ -w "/opt" ]; then
        _opt_avail=$(df /opt 2>/dev/null | tail -1 | awk '{print $4}')
        if [ -n "$_opt_avail" ] && [ "$_opt_avail" -gt 10000 ] 2>/dev/null; then
            return 0
        fi
    fi

    for mnt in /mnt/sda1 /mnt/sda2 /mnt/mmcblk* /mnt/usb*; do
        if [ -d "$mnt" ] && [ -w "$mnt" ]; then
            return 0
        fi
    done

    _root_avail=$(df / 2>/dev/null | tail -1 | awk '{print $4}')
    if [ -n "$_root_avail" ] && [ "$_root_avail" -lt 2000 ] 2>/dev/null; then
        log_warn "Root filesystem has very little space ($(df -h / 2>/dev/null | tail -1 | awk '{print $4}') available)"
        log_info "Consider using extroot or USB storage"
        log_info "See: https://openwrt.org/docs/guide-user/additional-software/extroot_configuration"
    fi

    return 0
}

register_platform "openwrt"
REGISTERED_FEATURES=""
ENABLED_FEATURES=""

register_feature() {
    id="$1"
    REGISTERED_FEATURES="${REGISTERED_FEATURES} ${id}"
}

feature_dispatch() {
    fid="$1"
    func="$2"
    shift 2
    fn="feature_${fid}_${func}"
    if type "$fn" >/dev/null 2>&1; then
        "$fn" "$@"
    else
        log_warn "Feature '${fid}' does not implement '${func}'"
        return 1
    fi
}

features_run() {
    for f in $ENABLED_FEATURES; do
        fname=$(feature_dispatch "$f" name)
        log_header "Feature: ${fname}"
        feature_dispatch "$f" run || log_warn "Feature '${fname}' had issues"
    done
}

features_remove() {
    _geo_files_to_remove=""
    _geo_files_display=""
    for f in $REGISTERED_FEATURES; do
        case "$f" in
        geoip|geosite)
            _gpath=$(_geo_find_file_path "$f")
            if [ -n "$_gpath" ]; then
                _geo_files_to_remove="${_geo_files_to_remove} ${_gpath}"
                _geo_files_display="${_geo_files_display}\n    ${_gpath}"
            fi
            ;;
        *)
            feature_dispatch "$f" remove || true
            ;;
        esac
    done

    if [ -n "$_geo_files_to_remove" ]; then
        log_info "Found geodata files:${_geo_files_display}"
        if [ "$QUIET_MODE" -eq 1 ] || confirm "Remove geodata files?" "y"; then
            for _gf in $_geo_files_to_remove; do
                rm -f "$_gf" && log_info "Removed: $_gf"
            done
        else
            log_info "Keeping geodata files"
        fi
    fi
}
feature_auth_name() {
    echo "Web UI authentication"
}

feature_auth_description() {
    echo "Protect the web interface with a login/password"
}

feature_auth_default_enabled() {
    echo "no"
}

feature_auth_run() {
    log_info "Set up credentials for the B4 web interface"
    echo ""

    read_input "  Username: " ""
    _auth_user="$_INPUT"

    if [ -z "$_auth_user" ]; then
        log_info "No username provided, skipping authentication setup"
        return 0
    fi

    while true; do
        printf "  Password: " >&2
        stty -echo 2>/dev/null || true
        read -r _auth_pass
        stty echo 2>/dev/null || true
        echo "" >&2

        if [ -z "$_auth_pass" ]; then
            log_warn "Password cannot be empty"
            continue
        fi

        printf "  Confirm password: " >&2
        stty -echo 2>/dev/null || true
        read -r _auth_pass2
        stty echo 2>/dev/null || true
        echo "" >&2

        if [ "$_auth_pass" != "$_auth_pass2" ]; then
            log_warn "Passwords do not match, try again"
            continue
        fi

        break
    done

    if ! command_exists jq; then
        log_warn "jq not found — please update config manually:"
        log_info "  Set system.web_server.username = $_auth_user"
        log_info "  Set system.web_server.password = <your password>"
        return 0
    fi

    if [ ! -f "$B4_CONFIG_FILE" ]; then
        ensure_dir "$(dirname "$B4_CONFIG_FILE")" "Config directory" || return 1
        jq -n \
            --arg user "$_auth_user" \
            --arg pass "$_auth_pass" \
            '{ system: { web_server: { username: $user, password: $pass } } }' \
            >"$B4_CONFIG_FILE"
    else
        tmp="${B4_CONFIG_FILE}.tmp"
        if jq --arg user "$_auth_user" --arg pass "$_auth_pass" \
            '.system.web_server.username = $user | .system.web_server.password = $pass' \
            "$B4_CONFIG_FILE" >"$tmp" 2>/dev/null; then
            mv "$tmp" "$B4_CONFIG_FILE"
        else
            rm -f "$tmp"
            log_warn "Failed to update config"
            return 1
        fi
    fi

    log_ok "Web UI authentication configured for user '${_auth_user}'"
}

feature_auth_remove() {
    return 0
}

register_feature "auth"
GEOIP_SOURCES="1|Loyalsoldier|https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download
2|RUNET Freedom|https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release
3|B4 GeoIP (recommended)|https://github.com/DanielLavrushin/b4geoip/releases/latest/download"

feature_geoip_name() {
    echo "GeoIP data"
}

feature_geoip_description() {
    echo "Download geoip.dat for IP-based filtering"
}

feature_geoip_default_enabled() {
    echo "yes"
}

feature_geoip_run() {
    base_url=$(echo "$GEOIP_SOURCES" | grep "^3|" | cut -d'|' -f3)
    save_dir="$B4_DATA_DIR"

    if [ "$QUIET_MODE" -ne 1 ]; then
        log_sep
        echo ""

        echo "  Available geoip sources:"
        echo "$GEOIP_SOURCES" | while IFS='|' read -r num name _url; do
            [ -n "$num" ] && printf "    ${BOLD}%s${NC}) %s\n" "$num" "$name"
        done
        echo ""

        read_input "Select source [3]: " "3"

        _sel_url=$(echo "$GEOIP_SOURCES" | grep "^${_INPUT}|" | cut -d'|' -f3) || true
        [ -n "$_sel_url" ] && base_url="$_sel_url" || log_warn "Invalid selection, using default"

        if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
            existing=$(jq -r '.system.geo.ipdat_path // empty' "$B4_CONFIG_FILE" 2>/dev/null) || true
            if [ -n "$existing" ] && [ "$existing" != "null" ]; then
                save_dir=$(dirname "$existing")
                log_info "Found existing geoip path: $save_dir"
            fi
        fi

        read_input "Save directory [${save_dir}]: " "$save_dir"
        save_dir="$_INPUT"
    fi

    ensure_dir "$save_dir" "GeoIP directory" || return 1

    log_info "Downloading geoip.dat..."
    if ! fetch_file "${base_url}/geoip.dat" "${save_dir}/geoip.dat"; then
        log_err "Failed to download geoip.dat"
        return 1
    fi
    [ ! -s "${save_dir}/geoip.dat" ] && log_err "geoip.dat is empty" && return 1

    log_ok "geoip.dat downloaded to ${save_dir}"

    _geo_update_config "ipdat_path" "${save_dir}/geoip.dat" "ipdat_url" "${base_url}/geoip.dat"
}

feature_geoip_remove() {
    _geo_remove_file "ipdat_path" "geoip.dat"
}

register_feature "geoip"
GEOSITE_SOURCES="1|Loyalsoldier|https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download
2|RUNET Freedom (recommended)|https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release"

feature_geosite_name() {
    echo "GeoSite data"
}

feature_geosite_description() {
    echo "Download geosite.dat for domain categorization"
}

feature_geosite_default_enabled() {
    echo "yes"
}

feature_geosite_run() {
    base_url=$(echo "$GEOSITE_SOURCES" | grep "^2|" | cut -d'|' -f3)
    save_dir="$B4_DATA_DIR"

    if [ "$QUIET_MODE" -ne 1 ]; then
        log_sep
        echo ""

        echo "  Available geosite sources:"
        echo "$GEOSITE_SOURCES" | while IFS='|' read -r num name _url; do
            [ -n "$num" ] && printf "    ${BOLD}%s${NC}) %s\n" "$num" "$name"
        done
        echo ""

        read_input "Select source [2]: " "2"

        _sel_url=$(echo "$GEOSITE_SOURCES" | grep "^${_INPUT}|" | cut -d'|' -f3) || true
        [ -n "$_sel_url" ] && base_url="$_sel_url" || log_warn "Invalid selection, using default"

        if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
            existing=$(jq -r '.system.geo.sitedat_path // empty' "$B4_CONFIG_FILE" 2>/dev/null) || true
            if [ -n "$existing" ] && [ "$existing" != "null" ]; then
                save_dir=$(dirname "$existing")
                log_info "Found existing geosite path: $save_dir"
            fi
        fi

        read_input "Save directory [${save_dir}]: " "$save_dir"
        save_dir="$_INPUT"
    fi

    ensure_dir "$save_dir" "GeoSite directory" || return 1

    log_info "Downloading geosite.dat..."
    if ! fetch_file "${base_url}/geosite.dat" "${save_dir}/geosite.dat"; then
        log_err "Failed to download geosite.dat"
        return 1
    fi
    [ ! -s "${save_dir}/geosite.dat" ] && log_err "geosite.dat is empty" && return 1

    log_ok "geosite.dat downloaded to ${save_dir}"

    _geo_update_config "sitedat_path" "${save_dir}/geosite.dat" "sitedat_url" "${base_url}/geosite.dat"
}

feature_geosite_remove() {
    _geo_remove_file "sitedat_path" "geosite.dat"
}

register_feature "geosite"
_geo_update_config() {
    path_key="$1"
    path_val="$2"
    url_key="$3"
    url_val="$4"

    if ! command_exists jq; then
        log_warn "jq not found — please update config manually:"
        log_info "  Set system.geo.${path_key} = ${path_val}"
        return 0
    fi

    if [ ! -f "$B4_CONFIG_FILE" ]; then
        jq -n \
            --arg pv "$path_val" \
            --arg uv "$url_val" \
            "{ system: { geo: { ${path_key}: \$pv, ${url_key}: \$uv } } }" \
            >"$B4_CONFIG_FILE"
        log_ok "Created config with ${path_key}"
        return 0
    fi

    tmp="${B4_CONFIG_FILE}.tmp"
    if jq \
        --arg pv "$path_val" \
        --arg uv "$url_val" \
        ".system.geo = (.system.geo // {}) + { \"${path_key}\": \$pv, \"${url_key}\": \$uv }" \
        "$B4_CONFIG_FILE" >"$tmp" 2>/dev/null; then
        mv "$tmp" "$B4_CONFIG_FILE"
        log_ok "Config updated: ${path_key}"
    else
        rm -f "$tmp"
        log_warn "Failed to update config, please set ${path_key} manually"
    fi
}

_geo_find_file_path() {
    _feat="$1"
    case "$_feat" in
    geoip)   _cfg_key="ipdat_path";   _fname="geoip.dat" ;;
    geosite) _cfg_key="sitedat_path"; _fname="geosite.dat" ;;
    *) return 1 ;;
    esac

    for cfg in "$B4_CONFIG_FILE" /etc/b4/b4.json /opt/etc/b4/b4.json; do
        [ -f "$cfg" ] || continue
        if command_exists jq; then
            fpath=$(jq -r ".system.geo.${_cfg_key} // empty" "$cfg" 2>/dev/null) || true
            if [ -n "$fpath" ] && [ -f "$fpath" ]; then
                echo "$fpath"
                return 0
            fi
        fi
    done

    for dir in /etc/b4 /opt/etc/b4 "$B4_DATA_DIR"; do
        [ -z "$dir" ] && continue
        if [ -f "${dir}/${_fname}" ]; then
            echo "${dir}/${_fname}"
            return 0
        fi
    done
}

_geo_remove_file() {
    config_key="$1"
    filename="$2"

    for cfg in "$B4_CONFIG_FILE" /etc/b4/b4.json /opt/etc/b4/b4.json; do
        [ -f "$cfg" ] || continue
        if command_exists jq; then
            fpath=$(jq -r ".system.geo.${config_key} // empty" "$cfg" 2>/dev/null) || true
            if [ -n "$fpath" ] && [ -f "$fpath" ]; then
                log_info "Found ${filename}: ${fpath}"
                if [ "$QUIET_MODE" -eq 1 ] || confirm "Remove ${filename}?" "y"; then
                    rm -f "$fpath" && log_info "Removed: $fpath"
                else
                    log_info "Keeping ${filename}"
                fi
                return 0
            fi
        fi
    done

    for dir in /etc/b4 /opt/etc/b4; do
        if [ -f "${dir}/${filename}" ]; then
            log_info "Found ${filename}: ${dir}/${filename}"
            if [ "$QUIET_MODE" -eq 1 ] || confirm "Remove ${filename}?" "y"; then
                rm -f "${dir}/${filename}" && log_info "Removed: ${dir}/${filename}"
            else
                log_info "Keeping ${filename}"
            fi
            return 0
        fi
    done
}
feature_https_name() {
    echo "HTTPS web interface"
}

feature_https_description() {
    echo "Enable HTTPS for B4 web UI using detected TLS certificates"
}

feature_https_default_enabled() {
    _https_detect_certs >/dev/null 2>&1 && echo "yes" || echo "no"
}

feature_https_run() {
    cert_info=$(_https_detect_certs) || true
    if [ -z "$cert_info" ]; then
        log_info "No compatible TLS certificates found on this system"
        log_info "You can configure HTTPS later in B4 Web UI > Settings > Web Server"
        _https_remove_config
        return 0
    fi

    cert_path=$(echo "$cert_info" | cut -d'|' -f1)
    key_path=$(echo "$cert_info" | cut -d'|' -f2)
    cert_source=$(echo "$cert_info" | cut -d'|' -f3)

    log_info "Found TLS certificate: ${cert_source}"
    log_detail "Certificate" "$cert_path"
    log_detail "Key" "$key_path"

    if ! confirm "Enable HTTPS with this certificate?"; then
        _https_remove_config
        return 0
    fi

    if ! command_exists jq; then
        log_warn "jq not found — please update config manually:"
        log_info "  Set system.web_server.tls_cert = $cert_path"
        log_info "  Set system.web_server.tls_key = $key_path"
        return 0
    fi

    if [ ! -f "$B4_CONFIG_FILE" ]; then
        ensure_dir "$(dirname "$B4_CONFIG_FILE")" "Config directory" || return 1
        jq -n \
            --arg cert "$cert_path" \
            --arg key "$key_path" \
            '{ system: { web_server: { tls_cert: $cert, tls_key: $key } } }' \
            >"$B4_CONFIG_FILE"
    else
        tmp="${B4_CONFIG_FILE}.tmp"
        if jq --arg cert "$cert_path" --arg key "$key_path" \
            '.system.web_server.tls_cert = $cert | .system.web_server.tls_key = $key' \
            "$B4_CONFIG_FILE" >"$tmp" 2>/dev/null; then
            mv "$tmp" "$B4_CONFIG_FILE"
        else
            rm -f "$tmp"
            log_warn "Failed to update config"
            return 1
        fi
    fi

    log_ok "HTTPS enabled"
}

_https_detect_certs() {
    _https_check_pair "/etc/uhttpd.crt" "/etc/uhttpd.key" "OpenWrt uhttpd" && return 0
    _https_check_pair "/etc/cert.pem" "/etc/key.pem" "System default" && return 0
    _https_check_pair "/etc/ssl/certs/server.crt" "/etc/ssl/private/server.key" "System SSL" && return 0
    return 1
}

_https_check_pair() {
    cert="$1" key="$2" label="$3"
    [ -f "$cert" ] && [ -f "$key" ] || return 1
    grep -q "BEGIN" "$cert" 2>/dev/null && grep -q "BEGIN" "$key" 2>/dev/null || {
        log_warn "Skipping ${label} certificate — not in PEM format (possibly DER-encoded)"
        return 1
    }
    echo "${cert}|${key}|${label}"
    return 0
}

_https_remove_config() {
    if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
        tls=$(jq -r '.system.web_server.tls_cert // ""' "$B4_CONFIG_FILE" 2>/dev/null) || true
        if [ -n "$tls" ]; then
            tmp="${B4_CONFIG_FILE}.tmp"
            if jq 'del(.system.web_server.tls_cert, .system.web_server.tls_key)' \
                "$B4_CONFIG_FILE" >"$tmp" 2>/dev/null; then
                mv "$tmp" "$B4_CONFIG_FILE"
                log_info "Removed previous HTTPS configuration"
            else
                rm -f "$tmp"
            fi
        fi
    fi
}

feature_https_remove() {
    return 0
}

register_feature "https"
REGISTERED_SERVICES=""

register_service() {
    id="$1"
    REGISTERED_SERVICES="${REGISTERED_SERVICES} ${id}"
}

service_call() {
    func="$1"
    shift
    service_dispatch "$B4_SERVICE_TYPE" "$func" "$@"
}

service_dispatch() {
    sid="$1"
    func="$2"
    shift 2
    fn="service_${sid}_${func}"
    if type "$fn" >/dev/null 2>&1; then
        "$fn" "$@"
    else
        log_warn "Service type '${sid}' does not implement '${func}'"
        return 1
    fi
}

service_show_crash_log() {
    _errlog=""
    if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
        _errlog=$(jq -r '.system.logging.error_file // empty' "$B4_CONFIG_FILE" 2>/dev/null)
    fi
    [ -z "$_errlog" ] && _errlog="/var/log/b4/errors.log"
    if [ -s "$_errlog" ]; then
        log_info "Last log entries from $_errlog:"
        tail -5 "$_errlog" 2>/dev/null | while IFS= read -r _line; do
            log_info "  $_line"
        done
    fi
}
service_entware_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" 2>/dev/null || true

    if [ -f "${B4_SERVICE_DIR}/rc.func" ]; then
        _service_entware_install_rcfunc
    else
        _service_entware_install_standalone
    fi

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    log_ok "Init script created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    log_info "  ${B4_SERVICE_DIR}/${B4_SERVICE_NAME} start"
    log_info "  ${B4_SERVICE_DIR}/${B4_SERVICE_NAME} stop"
}

_service_entware_install_rcfunc() {
    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/bin/sh
# B4 DPI Bypass Service — Entware

ENABLED=yes
PROCS=b4
ARGS="--config=${B4_CONFIG_FILE}"
PREARGS=""
which nohup >/dev/null 2>&1 && PREARGS="nohup"
DESC="\$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

[ "\$1" = "start" ] || [ "\$1" = "restart" ] && kernel_mod_load

. /opt/etc/init.d/rc.func
EOF
}

_service_entware_install_standalone() {
    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/bin/sh
# B4 DPI Bypass Service — Entware standalone
PROG="${B4_BIN_DIR}/${BINARY_NAME}"
CONFIG="${B4_CONFIG_FILE}"
PIDFILE="/opt/var/run/b4.pid"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

start() {
    echo "Starting b4..."
    [ -f "\$PIDFILE" ] && kill -0 \$(cat "\$PIDFILE") 2>/dev/null && echo "Already running" && return 1
    kernel_mod_load
    if which nohup >/dev/null 2>&1; then
        nohup \$PROG --config \$CONFIG >/dev/null 2>&1 &
    elif which setsid >/dev/null 2>&1; then
        setsid \$PROG --config \$CONFIG >/dev/null 2>&1 &
    else
        (\$PROG --config \$CONFIG >/dev/null 2>&1 &)
    fi
    echo \$! >"\$PIDFILE"
    sleep 1
    if kill -0 \$(cat "\$PIDFILE") 2>/dev/null; then
        echo "b4 started (PID: \$(cat \$PIDFILE))"
    else
        echo "b4 failed to start, check /var/log/b4/errors.log"
        rm -f "\$PIDFILE"
        return 1
    fi
}

stop() {
    echo "Stopping b4..."
    [ -f "\$PIDFILE" ] && kill \$(cat "\$PIDFILE") 2>/dev/null
    rm -f "\$PIDFILE"
    killall b4 2>/dev/null || true
    echo "b4 stopped"
}

case "\$1" in
    start)   start ;;
    stop)    stop ;;
    restart) stop; sleep 1; start ;;
    *)       echo "Usage: \$0 {start|stop|restart}"; exit 1 ;;
esac
EOF
}

service_entware_remove() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed service: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_entware_start() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" start 2>/dev/null || {
            log_warn "Could not start service"
            return 1
        }
        sleep 2
        if pidof b4 >/dev/null 2>&1 || pgrep -x b4 >/dev/null 2>&1; then
            log_ok "Service started"
            return 0
        fi
        log_err "Service crashed immediately after start"
        service_show_crash_log
        return 1
    fi
    log_warn "Could not start service"
    return 1
}

service_entware_stop() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
    fi
}

register_service "entware"
service_none_install() {
    log_warn "No init system configured — b4 will not start automatically"
    log_info "Start manually: ${B4_BIN_DIR}/${BINARY_NAME} --config ${B4_CONFIG_FILE}"
}

service_none_remove() {
    return 0
}

service_none_start() {
    log_warn "No service configured — start b4 manually"
    return 1
}

service_none_stop() {
    return 0
}

register_service "none"
service_openrc_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/sbin/openrc-run

name="b4"
description="B4 DPI Bypass Service"

command="${B4_BIN_DIR}/${BINARY_NAME}"
command_args="--config ${B4_CONFIG_FILE}"
command_background=true
pidfile="/run/b4.pid"

output_log="/dev/null"
error_log="/dev/null"

depend() {
    need net
}

start_pre() {
    # Load kernel modules
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        modprobe "\$mod" >/dev/null 2>&1 || true
    done
}
EOF

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    rc-update add "${B4_SERVICE_NAME}" default 2>/dev/null || true
    log_ok "OpenRC service created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    log_info "  rc-service ${B4_SERVICE_NAME} start"
    log_info "  rc-service ${B4_SERVICE_NAME} stop"
}

service_openrc_remove() {
    rc-update del "${B4_SERVICE_NAME}" default 2>/dev/null || true
    rc-service "${B4_SERVICE_NAME}" stop 2>/dev/null || true
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed OpenRC service: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_openrc_start() {
    rc-service "${B4_SERVICE_NAME}" start 2>/dev/null || { log_warn "Could not start service"; return 1; }
    sleep 2
    if pidof b4 >/dev/null 2>&1 || pgrep -x b4 >/dev/null 2>&1; then
        log_ok "Service started"
        return 0
    fi
    log_err "Service crashed immediately after start"
    service_show_crash_log
    return 1
}

service_openrc_stop() {
    rc-service "${B4_SERVICE_NAME}" stop 2>/dev/null || true
}

register_service "openrc"
service_procd_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/bin/sh /etc/rc.common
# B4 DPI Bypass Service (procd)

START=99
STOP=10
USE_PROCD=1

PROG="${B4_BIN_DIR}/${BINARY_NAME}"
CONFIG="${B4_CONFIG_FILE}"

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

start_service() {
    kernel_mod_load

    procd_open_instance
    procd_set_param command \$PROG --config \$CONFIG
    procd_set_param respawn \${respawn_threshold:-3600} \${respawn_timeout:-5} \${respawn_retry:-5}
    procd_set_param stdout 0
    procd_set_param stderr 0
    procd_set_param pidfile /var/run/b4.pid
    procd_close_instance
}

stop_service() {
    return 0
}

service_triggers() {
    procd_add_reload_trigger "b4"
}
EOF

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    log_ok "Procd init script created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"

    "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" enable 2>/dev/null || true
    log_info "Service enabled for boot"
}

service_procd_remove() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" disable 2>/dev/null || true
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed procd service: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_procd_start() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" restart 2>/dev/null || { log_warn "Could not start service"; return 1; }
        sleep 2
        if pidof b4 >/dev/null 2>&1 || pgrep -x b4 >/dev/null 2>&1; then
            log_ok "Service started"
            return 0
        fi
        log_err "Service crashed immediately after start"
        service_show_crash_log
        return 1
    fi
    log_warn "Could not start service"
    return 1
}

service_procd_stop() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
    fi
}

register_service "procd"
service_systemd_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
[Unit]
Description=B4 DPI Bypass Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=${B4_BIN_DIR}/${BINARY_NAME} --config ${B4_CONFIG_FILE}
Restart=on-failure
RestartSec=5
TimeoutStopSec=10

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "${B4_SERVICE_NAME}" 2>/dev/null || true
    log_ok "Systemd service created and enabled: ${B4_SERVICE_NAME}"
}

service_systemd_remove() {
    systemctl stop "${B4_SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${B4_SERVICE_NAME}" 2>/dev/null || true
    rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    systemctl daemon-reload
    log_info "Removed systemd service: ${B4_SERVICE_NAME}"
}

service_systemd_start() {
    if systemctl restart "${B4_SERVICE_NAME}" 2>/dev/null; then
        _elapsed=0
        while [ "$_elapsed" -lt 10 ]; do
            sleep 1
            _elapsed=$((_elapsed + 1))
            if systemctl is-active --quiet "${B4_SERVICE_NAME}" 2>/dev/null; then
                log_ok "Service started"
                return 0
            fi
            if systemctl is-failed --quiet "${B4_SERVICE_NAME}" 2>/dev/null; then
                break
            fi
        done
        log_err "Service failed to start"
        log_info "Check logs with: journalctl -u ${B4_SERVICE_NAME} --no-pager -n 10"
        journalctl -u "${B4_SERVICE_NAME}" --no-pager -n 5 2>/dev/null | while IFS= read -r _line; do
            log_info "  $_line"
        done
        return 1
    fi
    log_warn "Could not start service"
    return 1
}

service_systemd_stop() {
    systemctl stop "${B4_SERVICE_NAME}" 2>/dev/null || true
}

register_service "systemd"
service_sysv_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/bin/sh
# B4 DPI Bypass Service
PROG="${B4_BIN_DIR}/${BINARY_NAME}"
CONFIG="${B4_CONFIG_FILE}"
PIDFILE="/var/run/b4.pid"

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

start() {
    echo "Starting b4..."
    [ -f "\$PIDFILE" ] && kill -0 \$(cat "\$PIDFILE") 2>/dev/null && echo "Already running" && return 1
    kernel_mod_load
    if which nohup >/dev/null 2>&1; then
        nohup \$PROG --config \$CONFIG >/dev/null 2>&1 &
    elif which setsid >/dev/null 2>&1; then
        setsid \$PROG --config \$CONFIG >/dev/null 2>&1 &
    else
        (\$PROG --config \$CONFIG >/dev/null 2>&1 &)
    fi
    echo \$! >"\$PIDFILE"
    sleep 1
    if kill -0 \$(cat "\$PIDFILE") 2>/dev/null; then
        echo "b4 started (PID: \$(cat \$PIDFILE))"
    else
        echo "b4 failed to start, check /var/log/b4/errors.log"
        rm -f "\$PIDFILE"
        return 1
    fi
}

stop() {
    echo "Stopping b4..."
    [ -f "\$PIDFILE" ] && kill \$(cat "\$PIDFILE") 2>/dev/null
    rm -f "\$PIDFILE"
    echo "b4 stopped"
}

case "\$1" in
    start)   start ;;
    stop)    stop ;;
    restart) stop; sleep 1; start ;;
    *)       echo "Usage: \$0 {start|stop|restart}"; exit 1 ;;
esac
EOF

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"

    if command -v update-rc.d >/dev/null 2>&1; then
        update-rc.d "${B4_SERVICE_NAME}" defaults 2>/dev/null || true
    elif command -v chkconfig >/dev/null 2>&1; then
        chkconfig --add "${B4_SERVICE_NAME}" 2>/dev/null || true
        chkconfig "${B4_SERVICE_NAME}" on 2>/dev/null || true
    fi

    log_ok "Init script created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
}

service_sysv_remove() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
        if command -v update-rc.d >/dev/null 2>&1; then
            update-rc.d -f "${B4_SERVICE_NAME}" remove 2>/dev/null || true
        elif command -v chkconfig >/dev/null 2>&1; then
            chkconfig --del "${B4_SERVICE_NAME}" 2>/dev/null || true
        fi
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed init script: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_sysv_start() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" start 2>/dev/null || { log_warn "Could not start service"; return 1; }
        sleep 2
        if pidof b4 >/dev/null 2>&1 || pgrep -x b4 >/dev/null 2>&1; then
            log_ok "Service started"
            return 0
        fi
        log_err "Service crashed immediately after start"
        service_show_crash_log
        return 1
    fi
    log_warn "Could not start service"
    return 1
}

service_sysv_stop() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
    fi
}

register_service "sysv"
action_install() {
    version="$1"
    force_arch="$2"

    check_root

    if [ "$QUIET_MODE" -eq 1 ]; then
        WIZARD_MODE="auto"
        _user_bin_dir="$B4_BIN_DIR"
        _user_data_dir="$B4_DATA_DIR"
        platform_auto_detect
        platform_call info
        [ -n "$_user_bin_dir" ] && B4_BIN_DIR="$_user_bin_dir"
        [ -n "$_user_data_dir" ] && B4_DATA_DIR="$_user_data_dir"
        [ -n "$_user_data_dir" ] && B4_CONFIG_FILE="${_user_data_dir}/b4.json"
        B4_ARCH="${force_arch:-$(detect_architecture)}"
        detect_pkg_manager
        for f in $REGISTERED_FEATURES; do
            fdefault=$(feature_dispatch "$f" default_enabled)
            [ "$fdefault" = "yes" ] && ENABLED_FEATURES="${ENABLED_FEATURES} ${f}"
        done
    else
        wizard_start

        case "$WIZARD_MODE" in
        auto)
            wizard_auto_detect
            ;;
        manual)
            wizard_manual_configure
            ;;
        esac

        [ -n "$force_arch" ] && B4_ARCH="$force_arch"

        wizard_select_features
    fi

    echo ""
    log_header "Installing B4"

    log_info "Checking dependencies..."
    platform_call check_deps

    if [ -z "$version" ]; then
        log_info "Fetching latest version..."
        version=$(get_latest_version)
    fi
    log_ok "Version: ${version}"
    log_ok "Architecture: ${B4_ARCH}"

    ensure_dir "$B4_BIN_DIR" "Binary directory" || exit 1
    ensure_dir "$B4_DATA_DIR" "Data directory" || exit 1
    setup_temp

    file_name="${BINARY_NAME}-linux-${B4_ARCH}.tar.gz"
    download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${file_name}"
    archive_path="${TEMP_DIR}/${file_name}"

    log_info "Downloading b4..."
    if ! fetch_file "$download_url" "$archive_path"; then
        log_err "Download failed for architecture: ${B4_ARCH}"
        exit 1
    fi

    sha_url="${download_url}.sha256"
    _cs_ret=0
    verify_checksum "$archive_path" "$sha_url" || _cs_ret=$?
    if [ "$_cs_ret" -eq 2 ]; then
        log_warn "Checksum mismatch — download may be corrupted"
        if ! confirm "Continue anyway?"; then
            exit 1
        fi
    fi

    log_info "Extracting..."
    cd "$TEMP_DIR"
    tar -xzf "$archive_path" || { log_err "Failed to extract archive"; exit 1; }
    rm -f "$archive_path"

    if [ ! -f "${BINARY_NAME}" ]; then
        log_err "Binary not found in archive"
        exit 1
    fi

    stop_b4

    rm -f /var/log/b4.log /opt/var/log/b4.log /tmp/log/b4.log 2>/dev/null || true

    if [ -f "${B4_BIN_DIR}/${BINARY_NAME}" ]; then
        ts=$(date '+%Y%m%d_%H%M%S')
        mv "${B4_BIN_DIR}/${BINARY_NAME}" "${B4_BIN_DIR}/${BINARY_NAME}.backup.${ts}"
        log_info "Existing binary backed up"
    fi

    mv "${BINARY_NAME}" "${B4_BIN_DIR}/" 2>/dev/null || cp "${BINARY_NAME}" "${B4_BIN_DIR}/" || {
        log_err "Failed to install binary to ${B4_BIN_DIR}"
        exit 1
    }
    chmod +x "${B4_BIN_DIR}/${BINARY_NAME}"

    _ver_exit=0
    sh -c "\"${B4_BIN_DIR}/${BINARY_NAME}\" --version" >/dev/null 2>&1 || _ver_exit=$?

    if [ "$_ver_exit" -eq 0 ]; then
        installed_ver=$("${B4_BIN_DIR}/${BINARY_NAME}" --version 2>&1 | head -1)
        log_ok "Binary installed: ${installed_ver}"
        rm -f "${B4_BIN_DIR}/${BINARY_NAME}".backup.* 2>/dev/null || true
    elif [ "$_ver_exit" -gt 128 ] && echo "$B4_ARCH" | grep -q "^mips" && ! echo "$B4_ARCH" | grep -q "softfloat"; then
        _sf_arch="${B4_ARCH}_softfloat"
        log_warn "Binary crashed (exit code $_ver_exit) — likely hardfloat/softfloat mismatch"
        log_info "Retrying with ${_sf_arch}..."

        _sf_file="${BINARY_NAME}-linux-${_sf_arch}.tar.gz"
        _sf_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${_sf_file}"
        _sf_archive="${TEMP_DIR}/${_sf_file}"

        if fetch_file "$_sf_url" "$_sf_archive"; then
            cd "$TEMP_DIR"
            rm -f "${BINARY_NAME}" 2>/dev/null
            tar -xzf "$_sf_archive" 2>/dev/null && rm -f "$_sf_archive"
            if [ -f "${BINARY_NAME}" ]; then
                mv "${BINARY_NAME}" "${B4_BIN_DIR}/" 2>/dev/null || cp "${BINARY_NAME}" "${B4_BIN_DIR}/"
                chmod +x "${B4_BIN_DIR}/${BINARY_NAME}"
                if "${B4_BIN_DIR}/${BINARY_NAME}" --version >/dev/null 2>&1; then
                    installed_ver=$("${B4_BIN_DIR}/${BINARY_NAME}" --version 2>&1 | head -1)
                    log_ok "Softfloat binary works: ${installed_ver}"
                    log_info "Tip: use --arch=${_sf_arch} for future installs"
                    B4_ARCH="$_sf_arch"
                    rm -f "${B4_BIN_DIR}/${BINARY_NAME}".backup.* 2>/dev/null || true
                else
                    log_err "Softfloat binary also failed — manual troubleshooting needed"
                    log_info "Run with --sysinfo for diagnostics, or try --arch=<arch> manually"
                fi
            else
                log_err "Failed to extract softfloat binary"
            fi
        else
            log_err "Could not download softfloat variant"
            log_info "Try reinstalling with: --arch=${_sf_arch}"
        fi
    else
        log_warn "Binary installed but version check failed (exit code: $_ver_exit)"
    fi

    log_info "Setting up service..."
    service_call install

    if [ -n "$ENABLED_FEATURES" ]; then
        features_run
    fi

    _install_summary "$version"
}

_install_summary() {
    version="$1"

    echo ""
    log_header "Installation Complete"
    log_sep
    log_detail "Version" "$version"
    log_detail "Binary" "${B4_BIN_DIR}/${BINARY_NAME}"
    log_detail "Config" "${B4_CONFIG_FILE}"
    log_detail "Service" "${B4_SERVICE_TYPE}"
    log_sep

    if ! echo "$PATH" | grep -q "$B4_BIN_DIR"; then
        log_warn "$B4_BIN_DIR is not in PATH"
        log_info "Consider: ln -s ${B4_BIN_DIR}/${BINARY_NAME} /usr/bin/${BINARY_NAME}"
    fi

    _show_web_info

    echo ""
    log_info "To see all options: ${B4_BIN_DIR}/${BINARY_NAME} --help"
    echo ""

    if [ "$QUIET_MODE" -eq 0 ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        if is_b4_running; then
            if confirm "B4 is already running. Restart now?"; then
                service_call stop || true
                sleep 1
                service_call start || true
            fi
        else
            if confirm "Start B4 service now?"; then
                service_call start || true
            fi
        fi
    fi

    echo ""
    printf "${GREEN}${BOLD}  B4 installation finished!${NC}\n"
    echo ""
}

_show_web_info() {
    web_port="7000"
    protocol="http"

    if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
        web_port=$(jq -r '.system.web_server.port // 7000' "$B4_CONFIG_FILE" 2>/dev/null) || true
        tls=$(jq -r '.system.web_server.tls_cert // ""' "$B4_CONFIG_FILE" 2>/dev/null) || true
        [ -n "$tls" ] && protocol="https"
    fi

    lan_ip=""
    if command_exists ip; then
        lan_ip=$(ip -4 addr show br0 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)
        [ -z "$lan_ip" ] && lan_ip=$(ip -4 addr 2>/dev/null | grep 'inet 192.168' | head -1 | awk '{print $2}' | cut -d'/' -f1)
    fi

    if [ -n "$lan_ip" ]; then
        echo ""
        log_info "Web interface: ${protocol}://${lan_ip}:${web_port}"
    fi
}
action_remove() {
    check_root

    log_header "Removing B4"

    if [ -z "$B4_PLATFORM" ]; then
        platform_auto_detect || true
        if [ -n "$B4_PLATFORM" ]; then
            platform_call info
        fi
    fi

    _remove_find_config

    stop_b4

    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        log_info "Removing service..."
        service_call remove 2>/dev/null || true
    else
        for svc in \
            /etc/systemd/system/b4.service \
            /etc/init.d/b4 \
            /opt/etc/init.d/S99b4; do
            if [ -f "$svc" ]; then
                rm -f "$svc"
                log_info "Removed: $svc"
            fi
        done
        command_exists systemctl && systemctl daemon-reload 2>/dev/null || true
    fi

    features_remove

    for dir in /usr/local/bin /usr/bin /usr/sbin /opt/bin /opt/sbin /tmp/b4; do
        if [ -f "${dir}/${BINARY_NAME}" ]; then
            rm -f "${dir}/${BINARY_NAME}"
            rm -f "${dir}/${BINARY_NAME}".backup.* 2>/dev/null || true
            log_info "Removed binary from: ${dir}"
        fi
    done

    _remove_config_dirs

    rm -f /var/run/b4.pid 2>/dev/null || true
    rm -f /var/log/b4.log /opt/var/log/b4.log /tmp/log/b4.log 2>/dev/null || true
    rm -rf /var/log/b4 2>/dev/null || true

    echo ""
    log_ok "B4 has been removed"
    echo ""
}

_remove_find_config() {
    if [ -n "$B4_CONFIG_FILE" ] && [ -f "$B4_CONFIG_FILE" ]; then
        log_info "Using config: $B4_CONFIG_FILE"
        return 0
    fi

    for cfg in /etc/b4/b4.json /opt/etc/b4/b4.json /etc/storage/b4/b4.json; do
        if [ -f "$cfg" ]; then
            B4_CONFIG_FILE="$cfg"
            B4_DATA_DIR=$(dirname "$cfg")
            log_info "Found config: $B4_CONFIG_FILE"
            return 0
        fi
    done

    log_warn "No config file found"
}

_remove_config_dirs() {
    checked=""
    for cfg_dir in "$B4_DATA_DIR" /etc/b4 /opt/etc/b4 /etc/storage/b4; do
        [ -z "$cfg_dir" ] && continue
        [ -d "$cfg_dir" ] || continue
        case " $checked " in
        *" $cfg_dir "*) continue ;;
        esac
        checked="${checked} ${cfg_dir}"

        remaining=$(ls -1 "$cfg_dir" 2>/dev/null)
        if [ -n "$remaining" ]; then
            log_info "Remaining files in ${cfg_dir}:"
            echo "$remaining" | while read -r f; do
                printf "    %s\n" "$f" >&2
            done
        fi

        if [ "$QUIET_MODE" -eq 1 ] || confirm "Remove config directory ${cfg_dir}?" "n"; then
            rm -rf "$cfg_dir"
            log_info "Removed: ${cfg_dir}"
        else
            log_info "Keeping: ${cfg_dir}"
        fi
    done
}
action_update() {
    target_ver="$1"
    force_arch="$2"

    check_root

    log_header "Updating B4"

    if [ -z "$B4_PLATFORM" ]; then
        platform_auto_detect || true
        if [ -n "$B4_PLATFORM" ]; then
            platform_call info
        fi
    fi

    existing_bin=""

    if [ -n "$B4_EXISTING_BIN" ] && [ -f "$B4_EXISTING_BIN" ]; then
        existing_bin="$B4_EXISTING_BIN"
        B4_BIN_DIR=$(dirname "$B4_EXISTING_BIN")
    fi
    if [ -z "$existing_bin" ]; then
        for dir in "$B4_BIN_DIR" /usr/local/bin /usr/bin /usr/sbin /opt/bin /opt/sbin /jffs/b4 /tmp/b4 /ssd/b4; do
            [ -z "$dir" ] && continue
            if [ -f "${dir}/${BINARY_NAME}" ]; then
                existing_bin="${dir}/${BINARY_NAME}"
                B4_BIN_DIR="$dir"
                break
            fi
        done
    fi

    if [ -z "$existing_bin" ]; then
        _path_bin=$(command -v "$BINARY_NAME" 2>/dev/null || true)
        if [ -n "$_path_bin" ] && [ -f "$_path_bin" ]; then
            existing_bin="$_path_bin"
            B4_BIN_DIR=$(dirname "$_path_bin")
        fi
    fi

    if [ -z "$existing_bin" ]; then
        log_err "B4 is not installed. Use install mode instead."
        exit 1
    fi

    _ver_full=$("$existing_bin" --version 2>&1) || _ver_full=""
    current_ver=$(echo "$_ver_full" | grep -i "version" | head -1)
    [ -z "$current_ver" ] && current_ver="unknown"
    log_info "Current: ${current_ver}"

    if [ -n "$force_arch" ]; then
        B4_ARCH="$force_arch"
    else
        B4_ARCH=$(detect_architecture)
    fi

    if [ -n "$target_ver" ]; then
        latest_ver="$target_ver"
        log_info "Target: ${latest_ver}"
    else
        log_info "Checking for updates..."
        latest_ver=$(get_latest_version)
        log_info "Latest: ${latest_ver}"
    fi

    if [ "$current_ver" = "$latest_ver" ] || echo "$current_ver" | grep -Fq "$latest_ver"; then
        log_ok "Already up to date"
        return 0
    fi

    if [ "$QUIET_MODE" -eq 0 ]; then
        if ! confirm "Update to ${latest_ver}?"; then
            log_info "Update cancelled"
            return 0
        fi
    fi

    setup_temp

    file_name="${BINARY_NAME}-linux-${B4_ARCH}.tar.gz"
    download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${latest_ver}/${file_name}"
    archive_path="${TEMP_DIR}/${file_name}"

    log_info "Downloading ${latest_ver}..."
    fetch_file "$download_url" "$archive_path" || {
        log_err "Download failed"
        exit 1
    }

    sha_url="${download_url}.sha256"
    _cs_ret=0
    verify_checksum "$archive_path" "$sha_url" || _cs_ret=$?
    if [ "$_cs_ret" -eq 2 ]; then
        log_warn "Checksum mismatch — download may be corrupted"
        if ! confirm "Continue anyway?"; then
            exit 1
        fi
    fi

    cd "$TEMP_DIR"
    tar -xzf "$archive_path" || {
        log_err "Extraction failed"
        exit 1
    }

    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        log_info "Stopping service..."
        service_call stop 2>/dev/null || true
        sleep 1
    else
        stop_b4
    fi

    ts=$(date '+%Y%m%d_%H%M%S')
    cp "$existing_bin" "${existing_bin}.backup.${ts}"

    rm -f "$existing_bin"
    mv "${TEMP_DIR}/${BINARY_NAME}" "$existing_bin" 2>/dev/null ||
        cp "${TEMP_DIR}/${BINARY_NAME}" "$existing_bin" ||
        {
            log_err "Failed to replace binary"
            exit 1
        }
    chmod +x "$existing_bin"

    if "$existing_bin" --version >/dev/null 2>&1; then
        new_ver=$("$existing_bin" --version 2>&1 | head -1)
        log_ok "Updated to: ${new_ver}"
        rm -f "${existing_bin}".backup.* 2>/dev/null || true
    else
        log_warn "Updated binary failed version check"
    fi

    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        log_info "Restarting service..."
        service_call start 2>/dev/null || true
    fi

    echo ""
    log_ok "Update complete"
    echo ""
}
action_sysinfo() {
    log_header "B4 System Diagnostics"

    log_sep
    log_detail "Hostname" "$(hostname 2>/dev/null || cat /proc/sys/kernel/hostname 2>/dev/null || echo 'unknown')"
    log_detail "Kernel" "$(uname -r)"
    log_detail "Architecture (raw)" "$(uname -m)"
    log_detail "Architecture (b4)" "$(detect_architecture 2>/dev/null || echo 'unknown')"
    [ -f /etc/os-release ] && log_detail "Distribution" "$(. /etc/os-release && echo "$PRETTY_NAME")"
    [ -f /etc/openwrt_release ] && log_detail "OpenWrt" "$(. /etc/openwrt_release && echo "$DISTRIB_DESCRIPTION")"
    is_lxc_container && log_detail "Container" "${YELLOW}LXC${NC}"

    cpu_cores=""
    if [ -f /proc/cpuinfo ]; then
        cpu_cores=$(grep -c "^processor" /proc/cpuinfo 2>/dev/null)
    fi
    [ -n "$cpu_cores" ] && log_detail "CPU cores" "$cpu_cores"

    _raw_arch=$(uname -m)
    case "$_raw_arch" in
    mips*)
        if [ -f /proc/cpuinfo ]; then
            _cpu_model=$(grep -i "cpu model" /proc/cpuinfo 2>/dev/null | head -1 | sed 's/.*: *//')
            [ -n "$_cpu_model" ] && log_detail "CPU model" "$_cpu_model"
            if grep -qi "nofpu\|no fpu" /proc/cpuinfo 2>/dev/null; then
                log_detail "FPU" "${YELLOW}not available (softfloat needed)${NC}"
            elif grep -qi "FPU" /proc/cpuinfo 2>/dev/null; then
                log_detail "FPU" "${GREEN}available${NC}"
            fi
        fi
        if [ -f /etc/openwrt_release ]; then
            _owrt_arch=$(sed -n "s/^DISTRIB_ARCH=['\"\`]*\([^'\"\`]*\).*/\1/p" /etc/openwrt_release 2>/dev/null)
            [ -n "$_owrt_arch" ] && log_detail "OpenWrt arch" "$_owrt_arch"
        fi
        if command_exists opkg; then
            _opkg_arch=$(opkg print-architecture 2>/dev/null | grep -i "mips" | head -1 | awk '{print $2}')
            [ -n "$_opkg_arch" ] && log_detail "opkg arch" "$_opkg_arch"
        fi
        _elf_bin=""
        for _eb in /bin/sh /bin/busybox /bin/ls; do
            [ -f "$_eb" ] && _elf_bin="$_eb" && break
        done
        if [ -n "$_elf_bin" ]; then
            _ei_data=$(dd if="$_elf_bin" bs=1 skip=5 count=1 2>/dev/null | _byte_to_dec)
            case "$_ei_data" in
                1) log_detail "ELF endian" "little-endian" ;;
                2) log_detail "ELF endian" "big-endian" ;;
                *) ;;
            esac
        fi
        if is_softfloat; then
            log_detail "Float ABI" "${YELLOW}soft-float${NC}"
        else
            log_detail "Float ABI" "hard-float"
        fi
        ;;
    *) ;;
    esac

    if [ -f /proc/meminfo ]; then
        mem_total=$(awk '/^MemTotal:/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null)
        mem_avail=$(awk '/^MemAvailable:/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null)
        [ -z "$mem_avail" ] && mem_avail=$(awk '/^MemFree:/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null)
        if [ -n "$mem_total" ]; then
            log_detail "Memory" "${mem_total} MB (Available: ${mem_avail:-?} MB)"
        fi
    fi

    _saved_platform="$B4_PLATFORM"
    _saved_bin_dir="$B4_BIN_DIR"
    _saved_data_dir="$B4_DATA_DIR"
    _saved_config_file="$B4_CONFIG_FILE"
    _saved_service_type="$B4_SERVICE_TYPE"
    _saved_service_dir="$B4_SERVICE_DIR"
    _saved_service_name="$B4_SERVICE_NAME"
    _saved_pkg_manager="$B4_PKG_MANAGER"
    platform_auto_detect 2>/dev/null || true
    if [ -n "$B4_PLATFORM" ]; then
        pname=$(platform_dispatch "$B4_PLATFORM" name 2>/dev/null)
        log_detail "Detected platform" "${pname} (${B4_PLATFORM})"
        platform_call info 2>/dev/null || true
        log_detail "Binary dir" "${B4_BIN_DIR}"
        log_detail "Data dir" "${B4_DATA_DIR}"
        log_detail "Service type" "${B4_SERVICE_TYPE}"
    fi

    log_sep

    found_bin=""
    _bin_crashed=""
    for dir in "$B4_BIN_DIR" /usr/local/bin /usr/bin /usr/sbin /opt/bin /opt/sbin /tmp/b4; do
        [ -z "$dir" ] && continue
        if [ -f "${dir}/${BINARY_NAME}" ] && [ -x "${dir}/${BINARY_NAME}" ]; then
            _ver_exit=0
            _ver_full=$(sh -c "\"${dir}/${BINARY_NAME}\" --version 2>/dev/null" 2>/dev/null) || _ver_exit=$?
            if [ "$_ver_exit" -gt 128 ] 2>/dev/null; then
                _bin_crashed="${dir}/${BINARY_NAME}"
                continue
            fi
            if echo "$_ver_full" | grep -qi "b4 version\|bypass\|dpi"; then
                found_bin="${dir}/${BINARY_NAME}"
                _ver_out=$(echo "$_ver_full" | grep -i "version" | head -1)
                break
            fi
        fi
    done

    if [ -n "$found_bin" ]; then
        log_detail "Binary" "$found_bin"
        log_detail "Version" "$_ver_out"
        if [ -n "$B4_BIN_DIR" ] && [ -n "$_bin_crashed" ]; then
            log_detail "WARNING" "${RED}${_bin_crashed} crashes (segfault) — wrong architecture?${NC}"
        fi
    elif [ -n "$_bin_crashed" ]; then
        log_detail "Binary" "${_bin_crashed}"
        _arch_hint=""
        if command_exists file; then
            _arch_hint=$(file "$_bin_crashed" 2>/dev/null | sed 's/.*: //')
        elif command_exists readelf; then
            _arch_hint=$(readelf -h "$_bin_crashed" 2>/dev/null | awk '/Machine:/ {$1=""; print substr($0,2)}')
        fi
        if [ -n "$_arch_hint" ]; then
            log_detail "Status" "${RED}crashes on startup (segfault)${NC}"
            log_detail "Binary type" "$_arch_hint"
            log_detail "System arch" "$(uname -m)"
        else
            log_detail "Status" "${RED}crashes on startup (segfault) — wrong architecture?${NC}"
        fi
    else
        log_detail "Binary" "${YELLOW}not found${NC}"
    fi

    cfg_file=""
    for cfg in "$B4_CONFIG_FILE" /etc/b4/b4.json /opt/etc/b4/b4.json; do
        [ -z "$cfg" ] && continue
        [ -f "$cfg" ] && cfg_file="$cfg" && break
    done
    [ -n "$cfg_file" ] && log_detail "Config" "$cfg_file"

    if is_b4_running; then
        log_detail "Service status" "${GREEN}running${NC}"

        b4_pid=""
        for pf in /var/run/b4.pid /opt/var/run/b4.pid; do
            if [ -f "$pf" ] && kill -0 "$(cat "$pf")" 2>/dev/null; then
                b4_pid=$(cat "$pf")
                break
            fi
        done
        [ -z "$b4_pid" ] && b4_pid=$(pgrep -x "$BINARY_NAME" 2>/dev/null | head -1)
        [ -z "$b4_pid" ] && b4_pid=$(pgrep -f "${BINARY_NAME}" 2>/dev/null | head -1)

        if [ -n "$b4_pid" ]; then
            if [ -f "/proc/${b4_pid}/status" ]; then
                mem_kb=$(awk '/^VmRSS:/ {print $2}' "/proc/${b4_pid}/status" 2>/dev/null)
                if [ -n "$mem_kb" ]; then
                    mem_mb=$(awk "BEGIN {printf \"%.1f\", $mem_kb/1024}")
                    log_detail "Memory usage" "${mem_mb} MB (PID: ${b4_pid})"
                fi
            fi

            if [ -f "/proc/${b4_pid}/stat" ]; then
                proc_start=$(awk '{print $22}' "/proc/${b4_pid}/stat" 2>/dev/null)
                clk_tck=$(getconf CLK_TCK 2>/dev/null || echo 100)
                sys_uptime=$(awk '{print int($1)}' /proc/uptime 2>/dev/null)
                if [ -n "$proc_start" ] && [ -n "$sys_uptime" ] && [ "$clk_tck" -gt 0 ] 2>/dev/null; then
                    proc_secs=$((proc_start / clk_tck))
                    running_secs=$((sys_uptime - proc_secs))
                    if [ "$running_secs" -ge 3600 ] 2>/dev/null; then
                        hours=$((running_secs / 3600))
                        mins=$(((running_secs % 3600) / 60))
                        log_detail "Uptime" "${hours}h ${mins}m"
                    elif [ "$running_secs" -ge 60 ] 2>/dev/null; then
                        mins=$((running_secs / 60))
                        log_detail "Uptime" "${mins}m"
                    elif [ "$running_secs" -ge 0 ] 2>/dev/null; then
                        log_detail "Uptime" "${running_secs}s"
                    fi
                fi
            fi
        fi
    else
        log_detail "Service status" "${YELLOW}not running${NC}"
    fi

    if [ -n "$cfg_file" ] && command_exists jq; then
        queue_num=$(jq -r '.system.queue_num // empty' "$cfg_file" 2>/dev/null) || true
        workers=$(jq -r '.system.workers // empty' "$cfg_file" 2>/dev/null) || true
        geosite=$(jq -r '.system.geo.sitedat_path // empty' "$cfg_file" 2>/dev/null) || true
        geoip=$(jq -r '.system.geo.ipdat_path // empty' "$cfg_file" 2>/dev/null) || true

        [ -n "$queue_num" ] && [ "$queue_num" != "null" ] && log_detail "Queue number" "$queue_num"
        [ -n "$workers" ] && [ "$workers" != "null" ] && log_detail "Worker threads" "$workers"

        if [ -n "$geosite" ] && [ "$geosite" != "null" ] && [ -f "$geosite" ]; then
            size=$(ls -lh "$geosite" 2>/dev/null | awk '{print $5}')
            log_detail "geosite.dat" "${geosite} (${size})"
        fi
        if [ -n "$geoip" ] && [ "$geoip" != "null" ] && [ -f "$geoip" ]; then
            size=$(ls -lh "$geoip" 2>/dev/null | awk '{print $5}')
            log_detail "geoip.dat" "${geoip} (${size})"
        fi
    fi

    log_sep

    echo ""
    log_info "Kernel modules:"
    for mod in xt_NFQUEUE nfnetlink_queue xt_connbytes xt_multiport nf_conntrack; do
        if lsmod 2>/dev/null | grep -q "^${mod}"; then
            printf "    ${GREEN}loaded${NC}   %s\n" "$mod" >&2
        elif _kmod_builtin "$mod"; then
            printf "    ${GREEN}built-in${NC} %s\n" "$mod" >&2
        else
            printf "    ${YELLOW}missing${NC}  %s ${DIM}(may be built-in)${NC}\n" "$mod" >&2
        fi
    done

    _nfq_ipt=""
    if command_exists iptables; then
        _nfq_ipt="iptables"
    elif command_exists iptables-legacy; then
        _nfq_ipt="iptables-legacy"
    fi
    if [ -n "$_nfq_ipt" ]; then
        if $_nfq_ipt -t mangle -C B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null; then
            $_nfq_ipt -t mangle -D B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null || true
        fi
        if $_nfq_ipt -t mangle -N B4_TEST 2>/dev/null; then
            if $_nfq_ipt -t mangle -A B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null; then
                printf "    ${GREEN}  OK${NC}    %s\n" "NFQUEUE works (functional test passed)" >&2
                $_nfq_ipt -t mangle -D B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null || true
            else
                printf "    ${RED}  FAIL${NC}  %s\n" "NFQUEUE not functional" >&2
            fi
            $_nfq_ipt -t mangle -X B4_TEST 2>/dev/null || true
        fi
    fi

    echo ""
    log_info "Required tools:"
    _fw_found=0
    if command_exists nft; then
        if nft add table inet _b4_test 2>/dev/null; then
            nft delete table inet _b4_test 2>/dev/null || true
            printf "    ${GREEN}found${NC}   nft ${DIM}(nftables — functional)${NC}\n" >&2
            _fw_found=1
        else
            printf "    ${YELLOW}found${NC}   nft ${DIM}(nftables — ${RED}not functional${NC}${DIM})${NC}\n" >&2
        fi
    fi
    if command_exists iptables; then
        _ipt_ver=$(iptables --version 2>/dev/null)
        if echo "$_ipt_ver" | grep -q "nf_tables"; then
            printf "    ${YELLOW}found${NC}   iptables ${DIM}(nft-variant)${NC}\n" >&2
        else
            printf "    ${GREEN}found${NC}   iptables\n" >&2
        fi
        _fw_found=1
    fi
    if command_exists iptables-legacy; then
        printf "    ${GREEN}found${NC}   iptables-legacy\n" >&2
        _fw_found=1
    fi
    if [ "$_fw_found" = "0" ]; then
        printf "    ${RED}missing${NC} iptables or nft ${DIM}(firewall required)${NC}\n" >&2
    fi
    for tool in tar; do
        if command_exists "$tool"; then
            printf "    ${GREEN}found${NC}   %s\n" "$tool" >&2
        else
            printf "    ${RED}missing${NC} %s ${DIM}(required for install)${NC}\n" >&2
        fi
    done
    if command_exists curl; then
        if curl -sI --max-time 5 "https://github.com" >/dev/null 2>&1; then
            printf "    ${GREEN}found${NC}   curl ${GREEN}(HTTPS OK)${NC}\n" >&2
        else
            printf "    ${YELLOW}found${NC}   curl ${RED}(HTTPS failed)${NC}\n" >&2
        fi
    elif command_exists wget; then
        if wget --spider -q --timeout=5 "https://github.com" 2>/dev/null; then
            printf "    ${GREEN}found${NC}   wget ${GREEN}(HTTPS OK)${NC}\n" >&2
        elif wget --spider -q --timeout=5 --no-check-certificate "https://github.com" 2>/dev/null; then
            printf "    ${YELLOW}found${NC}   wget ${YELLOW}(HTTPS only with --no-check-certificate)${NC}\n" >&2
        else
            printf "    ${YELLOW}found${NC}   wget ${RED}(HTTPS failed)${NC}\n" >&2
        fi
    else
        printf "    ${RED}missing${NC} curl or wget ${DIM}(required for download)${NC}\n" >&2
    fi

    echo ""
    log_info "Optional tools:"
    for tool in jq sha256sum nohup modprobe ipset; do
        if command_exists "$tool"; then
            printf "    ${GREEN}found${NC}   %s\n" "$tool" >&2
        else
            case "$tool" in
            jq)        printf "    ${YELLOW}missing${NC} %s ${DIM}(config editing won't work)${NC}\n" "$tool" >&2 ;;
            sha256sum) printf "    ${YELLOW}missing${NC} %s ${DIM}(checksum verify skipped)${NC}\n" "$tool" >&2 ;;
            nohup)     printf "    ${YELLOW}missing${NC} %s ${DIM}(service may stop on session close)${NC}\n" "$tool" >&2 ;;
            modprobe)  printf "    ${YELLOW}missing${NC} %s ${DIM}(kernel modules loaded via insmod)${NC}\n" "$tool" >&2 ;;
            ipset)
                if _nft_functional; then
                    printf "    ${YELLOW}missing${NC} %s ${DIM}(needed for routing on iptables systems)${NC}\n" "$tool" >&2
                elif command_exists iptables || command_exists iptables-legacy; then
                    printf "    ${RED}missing${NC} %s ${DIM}(required — iptables backend in use, install ipset)${NC}\n" "$tool" >&2
                else
                    printf "    ${YELLOW}missing${NC} %s ${DIM}(no firewall backend detected — backend availability unclear)${NC}\n" "$tool" >&2
                fi
                ;;
            *) ;;
            esac
        fi
    done
    if command_exists curl && command_exists wget; then
        printf "    ${GREEN}found${NC}   wget ${DIM}(fallback downloader)${NC}\n" >&2
    elif command_exists wget && ! command_exists curl; then
        printf "    ${YELLOW}missing${NC} curl ${DIM}(wget used as primary)${NC}\n" >&2
    elif command_exists curl && ! command_exists wget; then
        printf "    ${DIM}  ---${NC}   wget ${DIM}(not needed, curl available)${NC}\n" >&2
    fi

    echo ""
    detect_pkg_manager
    log_detail "Package manager" "${B4_PKG_MANAGER:-none}"

    echo ""
    log_info "Storage:"
    _sysinfo_shown_devs=""
    for dir in / /opt /tmp /jffs /mnt/sda1 /etc/storage; do
        if [ -d "$dir" ]; then
            _sysinfo_show_storage "$dir"
        fi
    done
    for dir in /mnt/*; do
        [ -d "$dir" ] || continue
        case "$dir" in /mnt/sda1) continue ;; *) ;; esac
        _sysinfo_show_storage "$dir"
    done

    echo ""
    log_sep

    B4_PLATFORM="$_saved_platform"
    B4_BIN_DIR="$_saved_bin_dir"
    B4_DATA_DIR="$_saved_data_dir"
    B4_CONFIG_FILE="$_saved_config_file"
    B4_SERVICE_TYPE="$_saved_service_type"
    B4_SERVICE_DIR="$_saved_service_dir"
    B4_SERVICE_NAME="$_saved_service_name"
    B4_PKG_MANAGER="$_saved_pkg_manager"
}

_sysinfo_show_storage() {
    _dir="$1"
    _dev=$(df "$_dir" 2>/dev/null | tail -1 | awk '{print $1}')
    case "$_sysinfo_shown_devs" in
    *"|${_dev}|"*) return 0 ;; # already shown
    esac
    _sysinfo_shown_devs="${_sysinfo_shown_devs}|${_dev}|"
    avail=$(df -h "$_dir" 2>/dev/null | tail -1 | awk '{print $4}')
    writable="rw"
    [ ! -w "$_dir" ] && writable="ro"
    printf "    %-20s %s available (%s)\n" "$_dir" "${avail:-?}" "$writable" >&2
}
main() {
    ACTION="install"
    VERSION=""
    FORCE_ARCH=""

    for arg in "$@"; do
        case "$arg" in
        --remove | --uninstall | -r)
            ACTION="remove"
            ;;
        --update | -u)
            ACTION="update"
            ;;
        --sysinfo | --info | -i)
            ACTION="sysinfo"
            ;;
        --quiet | -q)
            QUIET_MODE=1
            ;;
        --arch=*)
            FORCE_ARCH="${arg#*=}"
            ;;
        --platform=*)
            B4_PLATFORM="${arg#*=}"
            ;;
        --bin-dir=*)
            B4_BIN_DIR="${arg#*=}"
            ;;
        --data-dir=*)
            B4_DATA_DIR="${arg#*=}"
            ;;
        --help | -h)
            _show_help
            exit 0
            ;;
        v* | V*)
            VERSION="$arg"
            ;;
        *) ;;
        esac
    done

    if [ "$QUIET_MODE" -ne 1 ] 2>/dev/null && [ ! -t 0 ] && [ -e /dev/tty ]; then
        exec </dev/tty
    fi

    case "$ACTION" in
    install) action_install "$VERSION" "$FORCE_ARCH" ;;
    remove) action_remove ;;
    update) action_update "$VERSION" "$FORCE_ARCH" ;;
    sysinfo) action_sysinfo ;;
    *) ;;
    esac
}

_show_help() {
    echo "B4 Universal Installer"
    echo ""
    echo "Usage: $0 [OPTIONS] [VERSION]"
    echo ""
    echo "Actions:"
    echo "  (default)           Install b4 (interactive wizard)"
    echo "  --update, -u        Update b4 to latest version"
    echo "  --remove, -r        Uninstall b4"
    echo "  --sysinfo, -i       Show system diagnostics"
    echo ""
    echo "Options:"
    echo "  --arch=ARCH         Force architecture (skip detection)"
    echo "  --platform=ID       Force platform (skip detection)"
    echo "  --bin-dir=DIR       Override binary directory"
    echo "  --data-dir=DIR      Override data/config directory"
    echo "  --quiet, -q         Non-interactive mode with defaults"
    echo "  --help, -h          Show this help"
    echo ""
    echo "Environment overrides:"
    echo "  B4_PLATFORM         Platform ID (generic_linux, openwrt, merlinwrt, ...)"
    echo "  B4_BIN_DIR          Binary install directory"
    echo "  B4_DATA_DIR         Data/config directory"
    echo "  B4_PKG_MANAGER      Package manager (apt, dnf, pacman, opkg, ...)"
    echo ""
    echo "Architectures:"
    echo "  amd64, 386, arm64, armv5, armv6, armv7,"
    echo "  mips, mipsle, mips_softfloat, mipsle_softfloat,"
    echo "  mips64, mips64le, loong64, ppc64, ppc64le, riscv64, s390x"
    echo ""
    echo "Examples:"
    echo "  $0                            Interactive install"
    echo "  $0 v1.4.0                     Install specific version"
    echo "  $0 --arch=mipsle_softfloat    Force architecture"
    echo "  $0 --platform=openwrt         Force platform"
    echo "  $0 --quiet                    Non-interactive with defaults"
    echo "  $0 --update                   Update to latest"
    echo "  $0 --remove                   Uninstall"
    echo "  $0 --sysinfo                  Show diagnostics"
}

main "$@"
exit 0

