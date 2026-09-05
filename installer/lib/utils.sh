#!/bin/sh

B4_KERNEL_MODULES="nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket nf_tproxy_ipv4 nf_tproxy_ipv6 xt_TPROXY xt_socket xt_hashlimit nft_limit"
# Core utility functions

# --- Configuration ---
REPO_OWNER="DanielLavrushin"
REPO_NAME="b4"
BINARY_NAME="b4"
TEMP_DIR="/tmp/b4_install_$$"
WGET_INSECURE=""
B4_MIRRORS="${B4_MIRRORS:-https://proxy.b4core.app https://proxy2.b4core.app}"
B4_SF_BASE="${B4_SF_BASE:-https://downloads.sourceforge.net/project/b4core}"
B4_CONNECT_TIMEOUT="${B4_CONNECT_TIMEOUT:-8}"
B4_STALL_TIMEOUT="${B4_STALL_TIMEOUT:-30}"
B4_MAX_TIME="${B4_MAX_TIME:-600}"
B4_PROBE_TIMEOUT="${B4_PROBE_TIMEOUT:-6}"

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

get_fs_device() {
    df -Pk "$1" 2>/dev/null | awk 'NR==2 {print $1}'
}

same_filesystem() {
    _sfs_a=$(get_fs_device "$1")
    _sfs_b=$(get_fs_device "$2")
    [ -n "$_sfs_a" ] && [ "$_sfs_a" = "$_sfs_b" ]
}

BIN_SLACK_KB=512

ensure_bin_space() {
    _ebs_dir="$1"
    _ebs_src="$2"

    [ -d "$_ebs_dir" ] || return 0
    [ -f "$_ebs_src" ] || return 0
    same_filesystem "$(dirname "$_ebs_src")" "$_ebs_dir" && return 0

    _ebs_need=$(du -k "$_ebs_src" 2>/dev/null | awk '{print $1}')
    [ -n "$_ebs_need" ] || return 0
    _ebs_need=$((_ebs_need + BIN_SLACK_KB))

    _ebs_avail=$(get_avail_kb "$_ebs_dir")
    [ -n "$_ebs_avail" ] || return 0

    if [ "$_ebs_avail" -lt "$_ebs_need" ] 2>/dev/null; then
        log_err "Not enough disk space in ${_ebs_dir}: ${_ebs_avail}KB free, need ${_ebs_need}KB"
        log_info "Free space there, or re-run with --bin-dir on external storage."
        return 1
    fi
    return 0
}

stash_binary() {
    _sb_bin="$1"
    _sb_backup="$2"

    rm -f "${_sb_bin}".backup.* 2>/dev/null || true
    [ -f "$_sb_bin" ] || return 0
    mv "$_sb_bin" "$_sb_backup" 2>/dev/null || return 1
    return 0
}

restore_binary() {
    _rb_bin="$1"
    _rb_backup="$2"

    [ -n "$_rb_backup" ] && [ -f "$_rb_backup" ] || return 1
    rm -f "$_rb_bin" 2>/dev/null || true
    mv "$_rb_backup" "$_rb_bin" 2>/dev/null || return 1
    chmod +x "$_rb_bin" 2>/dev/null || true
    return 0
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
MIRROR_OWNERS="DanielLavrushin Loyalsoldier runetfreedom XTLS Flowseal"

mirror_url() {
    _mu_base="$1"
    _mu_url="$2"

    for _mu_owner in $MIRROR_OWNERS; do
        case "$_mu_url" in
        https://raw.githubusercontent.com/${_mu_owner}/* | \
            https://github.com/${_mu_owner}/* | \
            https://api.github.com/repos/${_mu_owner}/*)
            echo "${_mu_base}/github/${_mu_url}"
            return 0
            ;;
        esac
    done

    echo ""
}

sf_url() {
    _su_prefix="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/"
    case "$1" in
    "${_su_prefix}"*)
        echo "${B4_SF_BASE}/${1#"$_su_prefix"}"
        ;;
    *) echo "" ;;
    esac
}

_wget_supports() {
    wget --help 2>&1 | grep -qF -- "$1"
}

mirror_alive() {
    _ma_base="$1"

    if command_exists curl; then
        _ma_insecure=""
        [ -n "$WGET_INSECURE" ] && _ma_insecure="-k"
        curl -sf $_ma_insecure --connect-timeout "$B4_CONNECT_TIMEOUT" \
            --max-time "$B4_PROBE_TIMEOUT" -o /dev/null \
            "${_ma_base}/b4/health" 2>/dev/null && return 0
        return 1
    fi

    if command_exists wget; then
        _ma_args="-q $WGET_INSECURE -O /dev/null"
        _wget_supports "--timeout" && _ma_args="$_ma_args --timeout=$B4_PROBE_TIMEOUT"
        wget $_ma_args "${_ma_base}/b4/health" 2>/dev/null && return 0
    fi

    return 1
}

_wget_guarded() {
    _wg_out="$1"
    _wg_quiet="$2"
    shift 2

    if [ "$_wg_quiet" = "1" ]; then
        wget "$@" 2>/dev/null &
    else
        wget "$@" &
    fi
    _wg_pid=$!

    _wg_prev=0
    _wg_stall=0
    _wg_elapsed=0
    _wg_tick=5
    _wg_floor=$((1024 * 5))

    while kill -0 "$_wg_pid" 2>/dev/null; do
        sleep "$_wg_tick"
        _wg_elapsed=$((_wg_elapsed + _wg_tick))

        _wg_now=$(wc -c <"$_wg_out" 2>/dev/null | awk '{print $1}')
        if [ -z "$_wg_now" ]; then
            _wg_now=0
        fi

        if [ $((_wg_now - _wg_prev)) -lt "$_wg_floor" ]; then
            _wg_stall=$((_wg_stall + _wg_tick))
        else
            _wg_stall=0
        fi
        _wg_prev=$_wg_now

        if [ "$_wg_stall" -ge "$B4_STALL_TIMEOUT" ] || [ "$_wg_elapsed" -ge "$B4_MAX_TIME" ]; then
            kill "$_wg_pid" 2>/dev/null || true
            wait "$_wg_pid" 2>/dev/null || true
            return 1
        fi
    done

    wait "$_wg_pid"
}

_do_fetch() {
    _fetch_url="$1"
    _fetch_out="$2"
    if [ -t 2 ] && [ "$QUIET_MODE" -ne 1 ]; then
        if command_exists curl && curl -fL --progress-bar \
            --connect-timeout "$B4_CONNECT_TIMEOUT" \
            --speed-limit 1024 --speed-time "$B4_STALL_TIMEOUT" \
            --max-time "$B4_MAX_TIME" -o "$_fetch_out" "$_fetch_url" 2>&1; then return 0; fi
        if command_exists wget; then
            _wget_args="$WGET_INSECURE"
            _wget_supports "--show-progress" && _wget_args="$_wget_args --show-progress -q"
            _wget_supports "--connect-timeout" && _wget_args="$_wget_args --connect-timeout=$B4_CONNECT_TIMEOUT"
            _wget_supports "--timeout" && _wget_args="$_wget_args --timeout=$B4_STALL_TIMEOUT"
            _wget_guarded "$_fetch_out" 0 $_wget_args -O "$_fetch_out" "$_fetch_url" && return 0
        fi
    else
        if command_exists curl && curl -sfL \
            --connect-timeout "$B4_CONNECT_TIMEOUT" \
            --speed-limit 1024 --speed-time "$B4_STALL_TIMEOUT" \
            --max-time "$B4_MAX_TIME" -o "$_fetch_out" "$_fetch_url" 2>/dev/null; then return 0; fi
        if command_exists wget; then
            _wget_args="-q $WGET_INSECURE"
            _wget_supports "--connect-timeout" && _wget_args="$_wget_args --connect-timeout=$B4_CONNECT_TIMEOUT"
            _wget_supports "--timeout" && _wget_args="$_wget_args --timeout=$B4_STALL_TIMEOUT"
            _wget_guarded "$_fetch_out" 1 $_wget_args -O "$_fetch_out" "$_fetch_url" && return 0
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

    _ff_announced=0
    for _ff_base in $B4_MIRRORS; do
        _ff_url=$(mirror_url "$_ff_base" "$url")
        [ -z "$_ff_url" ] && continue
        mirror_alive "$_ff_base" || continue
        if [ "$_ff_announced" -eq 0 ]; then
            log_warn "Direct download failed, trying mirrors..."
            _ff_announced=1
        fi
        log_info "Mirror: ${_ff_base}"
        if _do_fetch "$_ff_url" "$output"; then return 0; fi
    done

    _ff_sf=$(sf_url "$url")
    if [ -n "$_ff_sf" ]; then
        if [ "$_ff_announced" -eq 0 ]; then
            log_warn "Direct download failed, trying mirrors..."
            _ff_announced=1
        fi
        log_info "Mirror: SourceForge"
        if _do_fetch "$_ff_sf" "$output"; then return 0; fi
    fi

    log_err "Failed to download: $url"
    return 1
}

_do_fetch_stdout() {
    _dfs_url="$1"

    if command_exists curl; then
        curl -sfL --connect-timeout "$B4_CONNECT_TIMEOUT" --max-time 25 "$_dfs_url" 2>/dev/null && return 0
    fi
    if command_exists wget; then
        _dfs_args="-qO- $WGET_INSECURE"
        _wget_supports "--connect-timeout" && _dfs_args="$_dfs_args --connect-timeout=$B4_CONNECT_TIMEOUT"
        _wget_supports "--timeout" && _dfs_args="$_dfs_args --timeout=25"
        wget $_dfs_args "$_dfs_url" 2>/dev/null && return 0
    fi
    return 1
}

fetch_stdout() {
    url="$1"

    result=$(_do_fetch_stdout "$url") && [ -n "$result" ] && echo "$result" && return 0

    for _fs_base in $B4_MIRRORS; do
        _fs_url=$(mirror_url "$_fs_base" "$url")
        [ -z "$_fs_url" ] && continue
        mirror_alive "$_fs_base" || continue
        result=$(_do_fetch_stdout "$_fs_url") && [ -n "$result" ] && echo "$result" && return 0
    done

    return 1
}

# --- GitHub release helpers ---
_extract_tag_name() {
    grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4
}

get_latest_version() {
    api_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"
    version=$(fetch_stdout "$api_url" | _extract_tag_name)

    if [ -z "$version" ]; then
        for _glv_base in $B4_MIRRORS; do
            mirror_alive "$_glv_base" || continue
            version=$(_do_fetch_stdout "${_glv_base}/b4/api/releases/latest" | _extract_tag_name)
            [ -n "$version" ] && break
        done
    fi

    if [ -z "$version" ]; then
        log_err "Failed to fetch latest version"
        exit 1
    fi
    echo "$version"
}

sha256_of() {
    _sh_file="$1"

    _sh_out=$(sha256sum "$_sh_file" 2>/dev/null | awk '{print $1}')
    if [ -n "$_sh_out" ]; then
        echo "$_sh_out"
        return 0
    fi

    _sh_out=$(busybox sha256sum "$_sh_file" 2>/dev/null | awk '{print $1}')
    if [ -n "$_sh_out" ]; then
        echo "$_sh_out"
        return 0
    fi

    _sh_out=$(openssl dgst -sha256 "$_sh_file" 2>/dev/null | awk '{print $NF}')
    if [ -n "$_sh_out" ]; then
        echo "$_sh_out"
        return 0
    fi

    return 1
}

verify_checksum() {
    file="$1"
    checksum_url="$2"
    checksum_file="${file}.sha256"

    if ! fetch_file "$checksum_url" "$checksum_file"; then
        rm -f "$checksum_file"
        log_warn "Could not fetch the published SHA256 for this archive"
        return 1
    fi

    expected=$(awk '{print $1}' "$checksum_file")
    rm -f "$checksum_file"
    if [ -z "$expected" ]; then
        log_warn "The published SHA256 for this archive is empty"
        return 1
    fi

    actual=$(sha256_of "$file") || {
        log_warn "No working sha256 tool found (tried sha256sum, busybox sha256sum, openssl)"
        return 3
    }

    if [ "$expected" = "$actual" ]; then
        log_ok "SHA256 verified: $actual"
        return 0
    fi

    log_err "SHA256 mismatch! Expected: $expected Got: $actual"
    return 2
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
    _f="/lib/modules/${_kver}/modules.builtin"
    [ -f "$_f" ] && grep -q "/${_mod}\.ko" "$_f" 2>/dev/null && return 0
    _f="/lib/modules/${_kver}/modules.builtin.modinfo"
    [ -f "$_f" ] && grep -q "${_mod}\.[a-z]" "$_f" 2>/dev/null && return 0
    [ -d "/sys/module/${_mod}" ] && return 0
    return 1
}

# Check if a kernel module is available (loaded OR built-in)
_kmod_available() {
    lsmod 2>/dev/null | grep -q "^${1}[[:space:]]" && return 0
    _kmod_builtin "$1" && return 0
    return 1
}

_nft_functional() {
    command_exists nft || return 1
    if [ -z "$B4_NFT_FUNCTIONAL" ]; then
        if nft add table inet _b4_test >/dev/null 2>&1; then
            nft delete table inet _b4_test >/dev/null 2>&1
            B4_NFT_FUNCTIONAL="yes"
        else
            B4_NFT_FUNCTIONAL="no"
        fi
    fi
    [ "$B4_NFT_FUNCTIONAL" = "yes" ]
}

_firewall_backend() {
    if _nft_functional; then
        echo "nftables"
    elif command_exists iptables; then
        if iptables --version 2>/dev/null | grep -q "nf_tables" && command_exists iptables-legacy; then
            echo "iptables-legacy"
        else
            echo "iptables"
        fi
    elif command_exists iptables-legacy; then
        echo "iptables-legacy"
    else
        echo "none"
    fi
}

_nft_probe_rule() {
    _pt="_b4_probe_$$"
    nft delete table inet "$_pt" >/dev/null 2>&1
    nft add table inet "$_pt" >/dev/null 2>&1 || return 1
    _prc=1
    if nft add chain inet "$_pt" test >/dev/null 2>&1; then
        nft add rule inet "$_pt" test $1 >/dev/null 2>&1 && _prc=0
    fi
    nft delete table inet "$_pt" >/dev/null 2>&1
    return $_prc
}

_nft_queue_works() {
    _nft_functional || return 1
    _nft_probe_rule "counter queue num 0 bypass"
}

_nft_ct_counters_work() {
    _nft_functional || return 1
    _nft_probe_rule "ct original packets < 20 counter accept"
}

_ipt_probe_rule() {
    _ib="$1"
    _itable="$2"
    shift 2
    command_exists "$_ib" || return 1
    _ipc="B4_PROBE_$$"
    $_ib -t "$_itable" -F "$_ipc" >/dev/null 2>&1
    $_ib -t "$_itable" -X "$_ipc" >/dev/null 2>&1
    $_ib -t "$_itable" -N "$_ipc" >/dev/null 2>&1 || return 1
    _prc=1
    $_ib -t "$_itable" -A "$_ipc" "$@" >/dev/null 2>&1 && _prc=0
    $_ib -t "$_itable" -F "$_ipc" >/dev/null 2>&1
    $_ib -t "$_itable" -X "$_ipc" >/dev/null 2>&1
    return $_prc
}

_ipt_nfqueue_works() {
    _ipt_probe_rule "$1" mangle -j NFQUEUE --queue-num 0 --queue-bypass
}

_ipt_connbytes_works() {
    _ipt_probe_rule "$1" filter -p tcp -m connbytes --connbytes-dir original \
        --connbytes-mode packets --connbytes 0:10 -j ACCEPT
}

_nft_flow_guard() {
    awk '
        /flow add @|flow offload @/ {
            g = 0
            for (i = 1; i <= NF; i++) {
                if ($i == "ct" && $(i + 1) == "original" && $(i + 2) == "packets") {
                    op = $(i + 3)
                    val = $(i + 4) + 0
                    if (op == ">=" || op == "ge") g = val
                    else if (op == ">" || op == "gt") g = val + 1
                }
            }
            if (!seen || g < min) min = g
            seen = 1
        }
        END { print (seen ? min : 0) }
    '
}

_ipt_flow_guard() {
    awk '
        /FLOWOFFLOAD/ {
            g = 0
            if (index($0, "--connbytes-dir original") && index($0, "--connbytes-mode packets") && !index($0, "! --connbytes ")) {
                for (i = 1; i <= NF; i++) {
                    if ($i == "--connbytes") {
                        split($(i + 1), b, ":")
                        g = b[1] + 0
                    }
                }
            }
            if (!seen || g < min) min = g
            seen = 1
        }
        END { print (seen ? min : 0) }
    '
}

_b4_queue_window() {
    _qw_tcp=19
    _qw_udp=8
    if [ -n "$B4_CONFIG_FILE" ] && [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
        _qw_tcp=$(jq -r '.queue.tcp_conn_bytes_limit // 19' "$B4_CONFIG_FILE" 2>/dev/null || echo 19)
        _qw_udp=$(jq -r '.queue.udp_conn_bytes_limit // 8' "$B4_CONFIG_FILE" 2>/dev/null || echo 8)
    fi
    case "$_qw_tcp" in '' | *[!0-9]*) _qw_tcp=19 ;; esac
    case "$_qw_udp" in '' | *[!0-9]*) _qw_udp=8 ;; esac
    if [ "$_qw_udp" -gt "$_qw_tcp" ]; then
        echo "$_qw_udp"
    else
        echo "$_qw_tcp"
    fi
}

_b4_duplicate_sets() {
    if [ -z "$B4_CONFIG_FILE" ] || [ ! -f "$B4_CONFIG_FILE" ] || ! command_exists jq; then
        echo 0
        return 0
    fi
    _ds=$(jq -r '[.sets[]? | select(.enabled == true) | select(.tcp.duplicate.enabled == true)] | length' "$B4_CONFIG_FILE" 2>/dev/null || echo 0)
    case "$_ds" in '' | *[!0-9]*) _ds=0 ;; esac
    echo "$_ds"
}

_queue_functional() {
    case "$1" in
    nftables) _nft_queue_works ;;
    iptables | iptables-legacy) _ipt_nfqueue_works "$1" ;;
    *) return 1 ;;
    esac
}

_queue_modules_for_backend() {
    case "$1" in
    nftables) echo "nft_queue nfnetlink_queue nft_ct nf_conntrack" ;;
    *) echo "xt_NFQUEUE nfnetlink_queue xt_connbytes xt_multiport nf_conntrack" ;;
    esac
}

_queue_pkgs_for_backend() {
    if [ "$B4_PKG_MANAGER" = "apk" ]; then
        case "$1" in
        nftables) echo "kmod-nft-queue kmod-nft-nat kmod-nft-compat" ;;
        *) echo "kmod-nft-compat kmod-nft-queue" ;;
        esac
        return 0
    fi
    case "$1" in
    nftables) echo "kmod-nft-queue kmod-nfnetlink-queue kmod-nft-conntrack" ;;
    *) echo "kmod-nfnetlink-queue kmod-ipt-nfqueue iptables-mod-nfqueue kmod-ipt-conntrack-extra iptables-mod-conntrack-extra" ;;
    esac
}

_warn_if_queue_unavailable() {
    _wq_backend=$(_firewall_backend)
    if [ "$_wq_backend" = "none" ]; then
        log_warn "No working firewall backend found (neither nft nor iptables) - b4 cannot install its rules"
        return 1
    fi
    if _queue_functional "$_wq_backend"; then
        return 0
    fi

    log_warn "The kernel cannot queue packets to b4 on the ${_wq_backend} backend - b4 will not start in NFQUEUE mode"
    case "$B4_PKG_MANAGER" in
    apk) log_info "Try: apk add $(_queue_pkgs_for_backend "$_wq_backend")" ;;
    opkg) log_info "Try: opkg install $(_queue_pkgs_for_backend "$_wq_backend")" ;;
    *) log_info "Load the queue modules: $(_queue_modules_for_backend "$_wq_backend")" ;;
    esac
    log_info "Or switch b4 to TUN mode (queue.mode = \"tun\"), which does not need NFQUEUE"
    return 1
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
