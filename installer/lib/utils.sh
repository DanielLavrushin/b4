#!/bin/sh
# Core utility functions

# --- Configuration ---
REPO_OWNER="DanielLavrushin"
REPO_NAME="b4"
BINARY_NAME="b4"
TEMP_DIR="/tmp/b4_install_$$"
WGET_INSECURE=""
PROXY_BASE_URL="https://proxy.lavrush.in/github"

# --- Runtime state (set by platform/wizard) ---
B4_BIN_DIR=""
B4_DATA_DIR=""
B4_CONFIG_FILE=""
B4_SERVICE_TYPE=""
B4_SERVICE_DIR=""
B4_SERVICE_NAME=""
B4_PKG_MANAGER=""
B4_PLATFORM=""

# --- Command existence check (works on BusyBox/minimal shells) ---
command_exists() {
    command -v "$1" >/dev/null 2>&1 || which "$1" >/dev/null 2>&1
}

_byte_to_dec() {
    _btd_oct=$(od -b | head -1 | awk '{print $2}')
    [ -z "$_btd_oct" ] && return 1
    printf '%d\n' "0$_btd_oct"
}

# --- Root check ---
check_root() {
    if [ "$(id -u 2>/dev/null)" = "0" ]; then
        return 0
    fi
    if [ "$USER" = "root" ]; then
        return 0
    fi
    # Fallback: try writing to /etc
    if touch /etc/.b4_root_test 2>/dev/null; then
        rm -f /etc/.b4_root_test
        return 0
    fi
    log_err "This script must be run as root"
    exit 1
}

# --- Filesystem helpers ---
get_avail_kb() {
    _path="$1"
    # Return available space in KB
    df -Pk "$_path" 2>/dev/null | awk 'NR==2 {print $4}'
}

# --- Temp directory management ---
# Required free space: ~20MB (archive + extracted binary simultaneously)
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

# --- Package manager detection ---
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

# --- Architecture detection ---
detect_architecture() {
    arch=$(uname -m)

    case "$arch" in
    x86_64 | amd64) echo "amd64" ;;
    i386 | i486 | i586 | i686) echo "386" ;;
    aarch64 | arm64) echo "arm64" ;;
    armv7 | armv7l)
        # Check for full ARMv7 VFP support, otherwise use armv5 for safety
        if [ -f /proc/cpuinfo ] &&
            grep -qE "(vfpv[3-9])" /proc/cpuinfo 2>/dev/null &&
            grep -qE "CPU architecture:[[:space:]]*7" /proc/cpuinfo 2>/dev/null; then
            echo "armv7"
        else
            echo "armv5"
        fi
        ;;
    armv6*) echo "armv6" ;;
    armv5*) echo "armv5" ;;
    arm*)
        if [ -f /proc/cpuinfo ]; then
            if grep -qE "CPU architecture:[[:space:]]*7" /proc/cpuinfo 2>/dev/null; then
                echo "armv7"
            elif grep -qE "CPU architecture:[[:space:]]*6" /proc/cpuinfo 2>/dev/null; then
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
    # ELF header byte 6 (index 5): 1=little-endian, 2=big-endian
    [ "$(dd if=/bin/sh bs=1 skip=5 count=1 2>/dev/null | _byte_to_dec)" = "1" ] && return 0
    return 1
}

is_softfloat() {
    # On OpenWrt, DISTRIB_ARCH is the most reliable indicator
    # Convention: mips_24kc / mips_74kc = soft-float, mips_24kf = hard-float ('f' = FPU)
    if [ -f /etc/openwrt_release ]; then
        _sf_owrt_arch=$(sed -n "s/^DISTRIB_ARCH=['\"\`]*\([^'\"\`]*\).*/\1/p" /etc/openwrt_release 2>/dev/null)
        if [ -n "$_sf_owrt_arch" ]; then
            case "$_sf_owrt_arch" in
            *_softfloat* | *_nofpu* | *soft*) return 0 ;;
            esac
            # CPU model ending in 'f' (e.g. 24kf, 74kf) = hard-float
            if echo "$_sf_owrt_arch" | grep -qE '_[a-z]*[0-9]+k?f$'; then
                return 1
            fi
            # MIPS without 'f' suffix = soft-float on OpenWrt
            case "$_sf_owrt_arch" in
            mips_* | mipsel_* | mips64_* | mips64el_*) return 0 ;;
            esac
        fi
    fi
    # On OpenWrt/Entware, check opkg architecture
    if command_exists opkg; then
        _sf_opkg_arch="$(opkg print-architecture 2>/dev/null)"
        echo "$_sf_opkg_arch" | grep -qi "softfloat\|_nofpu\|soft_float" && return 0
        # Same convention: CPU model with 'f' suffix = hard-float
        if echo "$_sf_opkg_arch" | grep -qiE "mips(el|64|64el)?_[a-z]*[0-9]+k?f( |$)"; then
            return 1
        fi
        # MIPS in opkg without explicit hard-float = soft-float
        echo "$_sf_opkg_arch" | grep -qi "mips" && return 0
    fi
    # Check /proc/cpuinfo for soft-float indicators
    if [ -f /proc/cpuinfo ]; then
        grep -qi "nofpu\|no fpu\|soft.float" /proc/cpuinfo 2>/dev/null && return 0
    fi
    # Check ELF header for MIPS soft-float flag (EF_MIPS_SOFT_FLOAT = 0x800)
    _sf_elf_bin=""
    for _sf_b in /bin/sh /bin/busybox /bin/ls; do
        [ -f "$_sf_b" ] && _sf_elf_bin="$_sf_b" && break
    done
    if [ -n "$_sf_elf_bin" ]; then
        _sf_ei_class=$(dd if="$_sf_elf_bin" bs=1 skip=4 count=1 2>/dev/null | _byte_to_dec)
        _sf_ei_data=$(dd if="$_sf_elf_bin" bs=1 skip=5 count=1 2>/dev/null | _byte_to_dec)
        # e_flags offset: 36 for 32-bit ELF, 48 for 64-bit ELF
        _sf_flags_off=""
        [ "$_sf_ei_class" = "1" ] && _sf_flags_off=36
        [ "$_sf_ei_class" = "2" ] && _sf_flags_off=48
        if [ -n "$_sf_flags_off" ]; then
            # EF_MIPS_SOFT_FLOAT = 0x800 (bit 11)
            # In little-endian e_flags: bit 11 is in byte at offset+1, bit 3
            # In big-endian e_flags: bit 11 is in byte at offset+2, bit 3
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
    # Fallback: check via file or readelf if available
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

# --- HTTPS support ---
check_https_support() {
    if command_exists curl && curl -sI --max-time 5 "https://github.com" >/dev/null 2>&1; then
        return 0
    fi
    if command_exists wget && wget --spider -q --timeout=5 "https://github.com" 2>/dev/null; then
        return 0
    fi
    # Try with --no-check-certificate
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

# --- Download helpers ---
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

    # Try direct
    if _do_fetch "$url" "$output"; then return 0; fi

    # Try proxy fallback
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

    # Proxy fallback
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

# --- GitHub release helpers ---
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

# --- Container detection ---
is_lxc_container() {
    # Check /proc/1/environ for container=lxc
    if [ -f /proc/1/environ ]; then
        tr '\0' '\n' </proc/1/environ 2>/dev/null | grep -q '^container=lxc' && return 0
    fi
    # Fallback: check systemd container detection
    [ -f /run/systemd/container ] && grep -q "lxc" /run/systemd/container 2>/dev/null && return 0
    return 1
}

# --- Kernel module helpers ---
# Check if a kernel module is built-in (compiled into kernel, not loadable)
_kmod_builtin() {
    _mod="$1"
    _kver=$(uname -r)
    for _f in "/lib/modules/${_kver}/modules.builtin" "/lib/modules/${_kver}/modules.builtin.modinfo"; do
        [ -f "$_f" ] && grep -q "${_mod}" "$_f" 2>/dev/null && return 0
    done
    [ -d "/sys/module/${_mod}" ] && return 0
    return 1
}

# Check if a kernel module is available (loaded OR built-in)
_kmod_available() {
    lsmod 2>/dev/null | grep -q "^$1" && return 0
    _kmod_builtin "$1" && return 0
    return 1
}

_nft_functional() {
    command_exists nft || return 1
    nft list ruleset >/dev/null 2>&1
}

# --- Process management ---
is_b4_running() {
    # Check PID files first (most reliable)
    for pf in /var/run/b4.pid /opt/var/run/b4.pid; do
        if [ -f "$pf" ]; then
            _pid=$(cat "$pf" 2>/dev/null)
            [ -n "$_pid" ] && kill -0 "$_pid" 2>/dev/null && return 0
        fi
    done
    # Try pgrep -x (exact process name match — won't match scripts containing "b4")
    if command_exists pgrep; then
        pgrep -x "$BINARY_NAME" >/dev/null 2>&1 && return 0
    fi
    # Fallback: check ps for the actual b4 binary (not scripts mentioning b4)
    # Match paths like /usr/bin/b4 or standalone "b4" command, exclude our own PID
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
    # Try PID file first
    for pf in /var/run/b4.pid /opt/var/run/b4.pid; do
        if [ -f "$pf" ]; then
            _pid=$(cat "$pf" 2>/dev/null)
            [ -n "$_pid" ] && kill "$_pid" 2>/dev/null || true
        fi
    done
    # Then try pkill -x (exact name match)
    if command_exists pkill; then
        pkill -x "$BINARY_NAME" 2>/dev/null || true
    fi
    sleep 2
}

b4_running_cmdline() {
    _pid=""
    if command_exists pgrep; then
        _pid=$(pgrep -x "$BINARY_NAME" 2>/dev/null | head -1)
    fi
    [ -z "$_pid" ] && return 1
    if [ -r "/proc/${_pid}/cmdline" ]; then
        tr '\0' ' ' <"/proc/${_pid}/cmdline" 2>/dev/null | sed 's/ *$//'
        return 0
    fi
    return 1
}

relaunch_b4() {
    _cmd="$1"
    [ -z "$_cmd" ] && return 1
    # Re-exec the captured argv words directly — no shell re-parse of the
    # cmdline (avoids interpreting ; $ ` etc.). Disable globbing so a literal
    # * in an arg isn't expanded, then restore the caller's noglob state.
    case "$-" in
    *f*) _had_noglob=1 ;;
    *) _had_noglob=0 ;;
    esac
    set -f
    if command_exists setsid; then
        setsid $_cmd >/dev/null 2>&1 &
    else
        nohup $_cmd >/dev/null 2>&1 &
    fi
    if [ "$_had_noglob" = 0 ]; then set +f; fi
    sleep 2
    is_b4_running
}

# --- Directory helpers ---
is_writable_dir() {
    dir="$1"
    [ -d "$dir" ] && [ -w "$dir" ] && return 0
    # Try to create and test
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

is_abs_path() {
    case "$1" in
    /*) return 0 ;;
    *) return 1 ;;
    esac
}

require_abs_path() {
    if ! is_abs_path "$1"; then
        log_err "${2:-Path} must be an absolute path (got: ${1:-empty})"
        return 1
    fi
    return 0
}

# --- Check if user wants to exit ---
check_exit() {
    case "$1" in
    [eEqQ] | exit | EXIT | quit | QUIT)
        echo ""
        log_info "Aborted by user."
        exit 0
        ;;
    esac
}

# --- Read user input (works even when stdin is piped) ---
# Uses global _INPUT to avoid subshell issues with exit
_INPUT=""
read_input() {
    prompt="$1"
    default="$2"
    # In quiet mode, always use default without prompting
    if [ "$QUIET_MODE" -eq 1 ] 2>/dev/null; then
        _INPUT="$default"
        return 0
    fi
    printf "${CYAN}%b${NC}" "$prompt" >&2
    read _INPUT || _INPUT="$default"
    # Strip carriage returns (some terminals/SSH clients send \r)
    _INPUT=$(printf '%s' "$_INPUT" | tr -d '\r')
    check_exit "$_INPUT"
    [ -z "$_INPUT" ] && _INPUT="$default"
    return 0
}

# --- Yes/No prompt ---
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
