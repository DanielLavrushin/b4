#!/bin/sh
# B4 Universal Installer Script (POSIX Compliant)
# Automatically detects system architecture and installs the appropriate b4 binary
# Supports OpenWRT, MerlinWRT, and other Linux-based routers with only sh shell
#
# AUTO-GENERATED - Do not edit directly
# Edit files in installer/ and run installer/build.sh
#

set -e

export PATH="$HOME/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin:$PATH"

# --- END header.sh ---

# Configuration
REPO_OWNER="DanielLavrushin"
REPO_NAME="b4"
# These will be set dynamically by set_system_paths()
INSTALL_DIR=""
CONFIG_DIR=""
SERVICE_DIR=""
SERVICE_NAME=""
SYSTEM_TYPE=""
BINARY_NAME="b4"
CONFIG_FILE="" # Will be set after CONFIG_DIR is determined
TEMP_DIR="/tmp/b4_install_$$"
QUIET_MODE="0"
GEOSITE_SRC=""
GEOSITE_DST=""
# Proxy configuration for GitHub fallback
PROXY_BASE_URL="https://proxy.lavrush.in/github"

# geodat sources (pipe-delimited: number|name|url)
GEODAT_SOURCES="1|Loyalsoldier source|https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download
2|RUNET Freedom source [recommended]|https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release
3|Nidelon source|https://github.com/Nidelon/ru-block-v2ray-rules/releases/latest/download
4|DustinWin source|https://github.com/DustinWin/ruleset_geodata/releases/download/mihomo
5|Chocolate4U source|https://raw.githubusercontent.com/Chocolate4U/Iran-v2ray-rules/release"

# --- END config.sh ---

# Colors for output (if terminal supports it)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    CYAN='\033[0;36m'
    MAGENTA='\033[0;35m'
    BOLD='\033[1m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    CYAN=''
    MAGENTA=''
    BOLD=''
    NC=''
fi

# --- END colors.sh ---

# Helper functions
print_info() {
    if [ "$QUIET_MODE" -eq 0 ]; then
        printf "${BLUE}[INFO   ]${NC} %s\n" "$1" >&2
    fi
}

print_success() {
    if [ "$QUIET_MODE" -eq 0 ]; then
        printf "${GREEN}[SUCCESS]${NC} %s\n" "$1" >&2
    fi
}

print_error() {
    printf "${RED}[ERROR  ]${NC} %s\n" "$1" >&2
}

print_warning() {
    if [ "$QUIET_MODE" -eq 0 ]; then
        printf "${YELLOW}[WARNING]${NC} %s\n" "$1" >&2
    fi
}

print_header() {
    if [ "$QUIET_MODE" -eq 0 ]; then
        printf "\n${MAGENTA}%s${NC}\n" "$1" >&2
    fi
}

print_detail() {
    if [ "$QUIET_MODE" -eq 0 ]; then
        printf "  ${CYAN}%-22s${NC}: %b\n" "$1" "$2" >&2
    fi
}

# Check if command exists (works on routers without 'command' builtin)
command_exists() {
    # Try which first (common on routers)
    if which "$1" >/dev/null 2>&1; then
        return 0
    fi

    # Fallback: try to run with --help (most commands support this)
    if $1 --help >/dev/null 2>&1; then
        return 0
    fi

    # Command not found
    return 1
}

# Check if running as root (works on minimal routers)
check_root() {
    # Method 1: Check $USER variable
    if [ "$USER" = "root" ]; then
        return 0
    fi

    # Method 2: Try to write to /etc (only root can)
    if touch /etc/.root_test 2>/dev/null; then
        rm -f /etc/.root_test 2>/dev/null
        return 0
    fi

    # Method 3: Check whoami if available
    if which whoami >/dev/null 2>&1 && [ "$(whoami 2>/dev/null)" = "root" ]; then
        return 0
    fi

    # If we get here, probably not root
    print_error "This script must be run as root"
    print_info "Please switch to root user first"
    exit 1
}

# Create necessary directories
setup_directories() {
    print_info "Creating directories..."

    # Create install directory if it doesn't exist
    if [ ! -d "$INSTALL_DIR" ]; then
        mkdir -p "$INSTALL_DIR" || {
            print_error "Failed to create install directory: $INSTALL_DIR"
            exit 1
        }
    fi

    # Create config directory
    if [ ! -d "$CONFIG_DIR" ]; then
        if ! mkdir -p "$CONFIG_DIR" 2>/dev/null; then
            # Config dir creation failed - likely read-only filesystem
            # Try Entware fallback
            if [ -d "/opt/etc" ] && [ -w "/opt/etc" ]; then
                print_warning "Cannot write to $CONFIG_DIR (read-only?), falling back to /opt/etc/b4"
                CONFIG_DIR="/opt/etc/b4"
                CONFIG_FILE="${CONFIG_DIR}/b4.json"
                INSTALL_DIR="/opt/sbin"
                SERVICE_DIR="/opt/etc/init.d"
                SERVICE_NAME="S99b4"
                mkdir -p "$CONFIG_DIR" || {
                    print_error "Failed to create config directory: $CONFIG_DIR"
                    exit 1
                }
            else
                print_error "Failed to create config directory: $CONFIG_DIR"
                print_error "Filesystem may be read-only. Try installing Entware first."
                exit 1
            fi
        fi
    fi

    # Create temp directory
    rm -rf "$TEMP_DIR" 2>/dev/null || true
    mkdir -p "$TEMP_DIR" || {
        print_error "Failed to create temp directory"
        exit 1
    }
}

# Clean up temporary files
cleanup() {
    if [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

# Set up trap for cleanup
trap cleanup EXIT INT TERM

# Check if process is running (POSIX compliant, no pidof)
is_process_running() {
    process_name="$1"
    # Match exact binary name, not the installer script
    if ps 2>/dev/null | grep -v grep | grep -v "b4install" | grep -q "^.*${process_name}$\|^.*${process_name}[[:space:]]"; then
        return 0
    else
        return 1
    fi
}

# Stop process (POSIX compliant)
stop_process() {
    process_name="$1"
    if is_process_running "$process_name"; then
        print_info "Stopping existing $process_name process..."
        # Try pkill if available, otherwise use ps + kill
        if command_exists pkill; then
            pkill "^${process_name}$" 2>/dev/null || true
        else
            # BusyBox way: find and kill by name, exclude installer script
            ps | grep -v grep | grep -v "b4install" | grep "${process_name}$\|${process_name}[[:space:]]" | awk '{print $1}' | while read pid; do
                if [ -n "$pid" ]; then
                    kill "$pid" 2>/dev/null || true
                fi
            done
        fi
        sleep 2
    fi
}

# Convert GitHub URL to proxy URL
convert_to_proxy_url() {
    url="$1"
    # Check if URL is from allowed GitHub domains
    case "$url" in
    https://raw.githubusercontent.com/DanielLavrushin/* | \
        https://github.com/DanielLavrushin/* | \
        https://objects.githubusercontent.com/DanielLavrushin/* | \
        https://codeload.github.com/DanielLavrushin/* | \
        https://gist.githubusercontent.com/DanielLavrushin/* | \
        https://api.github.com/repos/DanielLavrushin/*)
        echo "${PROXY_BASE_URL}/${url}"
        ;;
    *)
        echo "$url"
        ;;
    esac
}

# Download file from URL with GitHub fallback
fetch_file() {
    url="$1"
    output="$2"

    # Try direct GitHub download first
    if command_exists wget; then
        if wget -q --timeout=10 -O "$output" "$url" 2>/dev/null; then
            return 0
        fi
    elif command_exists curl; then
        if curl -sfL --max-time 10 -o "$output" "$url" 2>/dev/null; then
            return 0
        fi
    else
        print_error "Neither wget nor curl found"
        return 1
    fi

    # If direct download failed, try proxy fallback
    proxy_url=$(convert_to_proxy_url "$url")
    if [ "$proxy_url" != "$url" ]; then
        print_warning "Direct download failed, trying proxy (proxy.lavrush.in)..."
        if command_exists wget; then
            wget -q --timeout=15 -O "$output" "$proxy_url" 2>/dev/null
            return $?
        elif command_exists curl; then
            curl -sfL --max-time 15 -o "$output" "$proxy_url" 2>/dev/null
            return $?
        fi
    fi

    print_error "Failed to download file from $url"
    return 1
}

# Fetch URL content to stdout with GitHub fallback
fetch_stdout() {
    url="$1"

    # Try direct GitHub download first
    result=""
    if command_exists wget; then
        result=$(wget -qO- --timeout=10 "$url" 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$result" ]; then
            echo "$result"
            return 0
        fi
    elif command_exists curl; then
        result=$(curl -sfL --max-time 10 "$url" 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$result" ]; then
            echo "$result"
            return 0
        fi
    else
        return 1
    fi

    # If direct download failed, try proxy fallback
    proxy_url=$(convert_to_proxy_url "$url")
    if [ "$proxy_url" != "$url" ]; then
        if command_exists wget; then
            wget -qO- --timeout=15 "$proxy_url" 2>/dev/null
        elif command_exists curl; then
            curl -sfL --max-time 15 "$proxy_url" 2>/dev/null
        fi
        return $?
    fi

    return 1
}

detect_pkg_manager() {
    if command_exists opkg; then
        PKG_MGR="opkg"
        PKG_UPDATE="opkg update"
        PKG_INSTALL="opkg install"
    elif command_exists apt-get; then
        PKG_MGR="apt"
        PKG_UPDATE="apt-get update"
        PKG_INSTALL="apt-get install -y"
    elif command_exists yum; then
        PKG_MGR="yum"
        PKG_UPDATE=""
        PKG_INSTALL="yum install -y"
    elif command_exists apk; then
        PKG_MGR="apk"
        PKG_UPDATE="apk update"
        PKG_INSTALL="apk add"
    fi
}

install_packages() {
    packages="$1"

    [ -z "$PKG_MGR" ] && detect_pkg_manager
    [ -z "$PKG_MGR" ] && {
        print_error "No package manager"
        return 1
    }

    [ -n "$PKG_UPDATE" ] && $PKG_UPDATE >/dev/null 2>&1 || true

    for pkg in $packages; do
        actual_pkg="$pkg"
        case "$PKG_MGR" in
        opkg) [ "$pkg" = "nohup" ] && actual_pkg="coreutils-nohup" ;;
        apt | apk) [ "$pkg" = "nohup" ] && actual_pkg="coreutils" ;;
        esac

        print_info "Installing $actual_pkg..."
        $PKG_INSTALL $actual_pkg >/dev/null 2>&1 && print_success "Installed $actual_pkg" || print_warning "Failed: $actual_pkg"
    done
}

check_dependencies() {
    required_deps="tar"

    missing_required=""

    if ! command_exists wget && ! command_exists curl; then
        missing_required="${missing_required} wget"
    fi

    for dep in $required_deps; do
        if ! command_exists "$dep"; then
            missing_required="${missing_required} $dep"
        fi
    done

    if [ -n "$missing_required" ]; then
        print_error "Missing required dependencies:$missing_required"

        if [ "$QUIET_MODE" = "1" ]; then
            install_packages "$missing_required" || exit 1
        else
            printf "${CYAN}Attempt to install? (Y/n): ${NC}"
            read answer </dev/tty || answer="y"

            case "$answer" in
            [nN] | [nN][oO]) exit 1 ;;
            *) install_packages "$missing_required" || exit 1 ;;
            esac
        fi
    fi

    check_recommended_packages
}

check_recommended_packages() {
    case "$SYSTEM_TYPE" in
    openwrt)
        recommended="kmod-nft-queue kmod-nf-conntrack-netlink iptables-mod-nfqueue jq wget-ssl coreutils-nohup"
        pkg_cmd="opkg"
        ;;
    entware | merlin)
        recommended="jq coreutils-nohup iptables"
        pkg_cmd="opkg"
        ;;
    systemd-linux | sysv-linux | generic-linux)
        missing=""
        for cmd in jq nohup iptables; do
            command_exists "$cmd" || missing="${missing} $cmd"
        done
        [ -z "$missing" ] && return 0
        print_warning "Recommended but missing:$missing"
        [ "$QUIET_MODE" = "1" ] && return 0
        detect_pkg_manager
        [ -z "$PKG_MGR" ] && {
            print_warning "No package manager found"
            return 0
        }
        printf "${CYAN}Install recommended packages? (Y/n): ${NC}"
        read answer </dev/tty || answer="y"

        case "$answer" in
        [nN] | [nN][oO]) return 0 ;;
        *) install_packages "$missing" ;;
        esac
        return 0
        ;;
    *)
        return 0
        ;;
    esac

    missing=""
    for pkg in $recommended; do
        $pkg_cmd list-installed 2>/dev/null | grep -q "^${pkg} " || missing="${missing} $pkg"
    done

    [ -z "$missing" ] && return 0

    print_warning "Missing packages:$missing"

    if [ "$QUIET_MODE" = "1" ]; then
        $pkg_cmd update >/dev/null 2>&1
        for pkg in $missing; do
            $pkg_cmd install "$pkg" >/dev/null 2>&1 || true
        done
        return 0
    fi

    printf "${CYAN}Install missing packages? (Y/n): ${NC}"
    read answer </dev/tty || answer="y"

    case "$answer" in
    [nN] | [nN][oO]) print_warning "B4 may not function correctly" ;;
    *)
        $pkg_cmd update >/dev/null 2>&1 || true
        for pkg in $missing; do
            print_info "Installing $pkg..."
            $pkg_cmd install "$pkg" >/dev/null 2>&1 && print_success "Installed $pkg" || print_warning "Failed: $pkg"
        done
        ;;
    esac
}

# --- END utils.sh ---

# Detect system type and set appropriate paths
detect_system_type() {
    # Check for Entware
    # Some systems like Keenetic don't have entware_release file
    if [ -d "/opt/etc/init.d" ]; then
        # Has Entware init structure
        if [ -f "/opt/etc/entware_release" ] || [ -f "/opt/bin/opkg" ] || [ -d "/opt/lib/opkg" ]; then
            echo "entware"
            return
        fi
    fi

    # Check for Keenetic specifically
    if [ -f "/proc/device-tree/model" ] && grep -qi "keenetic" /proc/device-tree/model 2>/dev/null; then
        echo "entware"
        return
    fi

    # Fallback: if /opt/sbin exists and is writable but /etc is read-only, assume Entware-like
    if [ -d "/opt/sbin" ] && [ -w "/opt/sbin" ] && ! [ -w "/etc" ]; then
        echo "entware"
        return
    fi

    # Check for OpenWRT
    if [ -f "/etc/openwrt_release" ]; then
        echo "openwrt"
        return
    fi

    # Check for MerlinWRT
    if [ -f "/etc/merlinwrt_release" ] || [ -d "/jffs" ]; then
        echo "merlin"
        return
    fi

    # Check for standard systemd-based Linux
    if [ -d "/etc/systemd/system" ] && command_exists systemctl; then
        echo "systemd-linux"
        return
    fi

    # Check for standard init.d Linux
    if [ -d "/etc/init.d" ] && [ ! -f "/etc/openwrt_release" ]; then
        echo "sysv-linux"
        return
    fi

    # Default to generic Linux
    echo "generic-linux"
}

# Set paths based on system type
set_system_paths() {
    SYSTEM_TYPE=$(detect_system_type)

    case "$SYSTEM_TYPE" in
    entware | merlin)
        INSTALL_DIR="/opt/sbin"
        CONFIG_DIR="/opt/etc/b4"
        SERVICE_DIR="/opt/etc/init.d"
        SERVICE_NAME="S99b4"
        ;;
    openwrt)
        # OpenWRT typically uses /usr/sbin or /usr/bin
        if [ -d "/usr/sbin" ]; then
            INSTALL_DIR="/usr/sbin"
        else
            INSTALL_DIR="/usr/bin"
        fi
        CONFIG_DIR="/etc/b4"
        SERVICE_DIR="/etc/init.d"
        SERVICE_NAME="b4"
        ;;
    systemd-linux)
        INSTALL_DIR="/usr/local/bin"
        CONFIG_DIR="/etc/b4"
        SERVICE_DIR="/etc/systemd/system"
        SERVICE_NAME="b4.service"
        ;;
    sysv-linux | generic-linux)
        INSTALL_DIR="/usr/local/bin"
        CONFIG_DIR="/etc/b4"
        SERVICE_DIR="/etc/init.d"
        SERVICE_NAME="b4"
        ;;
    *)
        # Fallback
        INSTALL_DIR="/usr/local/bin"
        CONFIG_DIR="/etc/b4"
        ;;
    esac

    CONFIG_FILE="${CONFIG_DIR}/b4.json"

    print_info "Detected system: $SYSTEM_TYPE"
    print_info "Using install directory: $INSTALL_DIR"
    print_info "Using config directory: $CONFIG_DIR"
}

# Detect system architecture and return appropriate binary variant
detect_architecture() {
    arch=$(uname -m)
    arch_variant=""

    case "$arch" in
    x86_64 | amd64)
        arch_variant="amd64"
        ;;
    i386 | i486 | i586 | i686)
        arch_variant="386"
        ;;
    aarch64 | arm64)
        arch_variant="arm64"
        ;;
    armv7)
        arch_variant="armv7"
        ;;
    armv7* | armv7l | armv7-*)
        # Default to armv5 for compatibility, only use armv7 if certain
        arch_variant="armv5"

        # Only use armv7 if we have clear evidence of full support
        if [ -f /proc/cpuinfo ]; then
            # Need BOTH vfpv3+ AND proper architecture confirmation
            if grep -qE "(vfpv[3-9]|vfpv[0-9][0-9])" /proc/cpuinfo 2>/dev/null &&
                grep -qE "CPU architecture:\s*7" /proc/cpuinfo 2>/dev/null; then
                arch_variant="armv7"
                print_info "Full ARMv7 support detected, using armv7 binary"
            else
                print_warning "armv7l detected but using armv5 for compatibility (safer for routers)"
            fi
        fi
        ;;
    armv6*)
        arch_variant="armv6"
        ;;
    armv5*)
        arch_variant="armv5"
        ;;
    arm*)
        # Generic ARM - try to detect version from CPU info
        if [ -f /proc/cpuinfo ]; then
            # Look for CPU architecture line first (most reliable)
            if grep -qE "CPU architecture:\s*7" /proc/cpuinfo; then
                arch_variant="armv7"
            elif grep -qE "CPU architecture:\s*6" /proc/cpuinfo; then
                arch_variant="armv6"
            elif grep -qE "CPU architecture:\s*5" /proc/cpuinfo; then
                arch_variant="armv5"
            # Fallback to searching for ARM version strings
            elif grep -qi "ARMv7" /proc/cpuinfo; then
                arch_variant="armv7"
            elif grep -qi "ARMv6" /proc/cpuinfo; then
                arch_variant="armv6"
            elif grep -qi "ARMv5" /proc/cpuinfo; then
                arch_variant="armv5"
            else
                # Default to armv5 for maximum compatibility
                arch_variant="armv5"
            fi
        else
            # No cpuinfo available, default to safest option
            arch_variant="armv5"
        fi
        ;;
    mips64)
        # Check MIPS endianness
        if grep -qi "mips.*el\|el.*mips" /proc/cpuinfo 2>/dev/null || uname -m | grep -qi "el"; then
            arch_variant="mips64le"
        else
            arch_variant="mips64"
        fi
        # Check for softfloat (no FPU)
        if [ -f /proc/cpuinfo ]; then
            if ! grep -qi "fpu" /proc/cpuinfo 2>/dev/null || grep -qi "nofpu\|no fpu" /proc/cpuinfo 2>/dev/null; then
                arch_variant="${arch_variant}_softfloat"
                print_warning "No FPU detected, using softfloat binary"
            fi
        fi
        ;;
    mips*)
        # 32-bit MIPS - determine endianness
        if grep -qi "mips.*el\|el.*mips" /proc/cpuinfo 2>/dev/null || uname -m | grep -qi "el"; then
            arch_variant="mipsle"
        else
            arch_variant="mips"
        fi
        # Check for softfloat (no FPU)
        if [ -f /proc/cpuinfo ]; then
            if ! grep -qi "fpu" /proc/cpuinfo 2>/dev/null || grep -qi "nofpu\|no fpu" /proc/cpuinfo 2>/dev/null; then
                arch_variant="${arch_variant}_softfloat"
                print_warning "No FPU detected, using softfloat binary"
            fi
        fi
        ;;
    ppc64le)
        arch_variant="ppc64le"
        ;;
    ppc64)
        arch_variant="ppc64"
        ;;
    riscv64)
        arch_variant="riscv64"
        ;;
    s390x)
        arch_variant="s390x"
        ;;
    loongarch64)
        arch_variant="loong64"
        ;;
    *)
        print_error "Unsupported architecture: $arch"
        exit 1
        ;;
    esac

    # ONLY output the result to stdout
    echo "$arch_variant"
}

# --- END system.sh ---

# This is the core installation part script for b4 Universal.

# Get latest release version from GitHub - ONLY returns version string
get_latest_version() {
    api_url="https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest"

    version=$(fetch_stdout "$api_url" | grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4)

    if [ -z "$version" ]; then
        print_error "Failed to fetch latest version"
        exit 1
    fi

    echo "$version"
}

# Verify checksum
verify_checksum() {
    file="$1"
    checksum_url="$2"
    checksum_file="${file}.sha256"

    print_info "Downloading SHA256 checksum..."

    if ! fetch_file "$checksum_url" "$checksum_file"; then
        rm -f "$checksum_file"
        return 1
    fi

    if [ ! -s "$checksum_file" ]; then
        rm -f "$checksum_file"
        return 1
    fi

    expected_checksum=$(awk '{print $1}' "$checksum_file")

    if [ -z "$expected_checksum" ]; then
        print_warning "Could not parse checksum from file"
        rm -f "$checksum_file"
        return 1
    fi

    if ! command_exists sha256sum; then
        print_warning "sha256sum not found, skipping verification"
        rm -f "$checksum_file"
        return 1
    fi

    actual_checksum=$(sha256sum "$file" | awk '{print $1}')

    rm -f "$checksum_file"

    if [ "$expected_checksum" = "$actual_checksum" ]; then
        print_success "SHA256 checksum verified: $actual_checksum"
        return 0
    else
        print_error "SHA256 checksum mismatch!"
        print_error "Expected: $expected_checksum"
        print_error "Got:      $actual_checksum"
        return 2
    fi
}

# Download file and verify checksums
download_file() {
    url="$1"
    output="$2"
    version="$3"
    arch="$4"

    print_info "Downloading from: $url"

    if ! fetch_file "$url" "$output"; then
        print_error "Download failed"
        return 1
    fi

    # Construct checksum URL
    file_name="${BINARY_NAME}-linux-${arch}.tar.gz"
    sha256_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${file_name}.sha256"

    # Try to verify SHA256 checksum
    if verify_checksum "$output" "$sha256_url"; then
        return 0
    elif [ $? -eq 2 ]; then
        print_error "Download verification failed!"
        return 0
    else
        print_warning "No checksum file found - unable to verify download integrity"

        if command_exists sha256sum; then
            local_hash=$(sha256sum "$output" | awk '{print $1}')
            print_info "Local SHA256: $local_hash"
        fi
    fi

    return 0
}

# --- END download.sh ---

# Create systemd service file (for systems with systemd)
create_systemd_service() {
    # Only create if systemd is actually available and functioning
    if ! [ -d "/etc/systemd/system" ] || ! command_exists systemctl; then
        return
    fi

    # Check if systemd is actually running (not just installed)
    if ! systemctl list-units >/dev/null 2>&1; then
        return
    fi

    cat >"/etc/systemd/system/b4.service" <<EOF
[Unit]
Description=B4 DPI Bypass Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=${INSTALL_DIR}/${BINARY_NAME} --config ${CONFIG_FILE}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    print_success "Systemd service created. You can manage it with:"
    print_info "  systemctl start b4"
    print_info "  systemctl stop b4"
    print_info "  systemctl enable b4  # To start on boot"

    SYSTEMCTL_CREATED="1"
}

# Common init script body (shared between OpenWRT and standard)
get_init_script_body() {
    cat <<'BODY'
PROG=PROG_PLACEHOLDER
CONFIG_FILE=CONFIG_PLACEHOLDER
PIDFILE=/var/run/b4.pid

kernel_mod_load() {
    KERNEL=$(uname -r)
    connbytes_mod_path=$(find /lib/modules/$KERNEL -name "xt_connbytes.ko*" 2>/dev/null | head -1)
    [ -n "$connbytes_mod_path" ] && insmod "$connbytes_mod_path" >/dev/null 2>&1
    nfqueue_mod_path=$(find /lib/modules/$KERNEL -name "xt_NFQUEUE.ko*" 2>/dev/null | head -1)
    [ -n "$nfqueue_mod_path" ] && insmod "$nfqueue_mod_path" >/dev/null 2>&1
    multiport_mod_path=$(find /lib/modules/$KERNEL -name "xt_multiport.ko*" 2>/dev/null | head -1)
    [ -n "$multiport_mod_path" ] && insmod "$multiport_mod_path" >/dev/null 2>&1
    modprobe xt_connbytes >/dev/null 2>&1 || true
    modprobe xt_NFQUEUE >/dev/null 2>&1 || true
    modprobe xt_multiport >/dev/null 2>&1 || true
}

start() {
    echo "Starting b4..."
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "b4 is already running"
        return 1
    fi
    kernel_mod_load
    nohup $PROG --config $CONFIG_FILE > /var/log/b4.log 2>&1 &
    echo $! > "$PIDFILE"
    sleep 1
    if kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "b4 started (PID: $(cat "$PIDFILE"))"
    else
        echo "Warning: b4 may have failed to start, check /var/log/b4.log"
        rm -f "$PIDFILE"
        return 1
    fi
}

stop() {
    echo "Stopping b4..."
    if [ -f "$PIDFILE" ]; then
        kill $(cat "$PIDFILE") 2>/dev/null
        rm -f "$PIDFILE"
        echo "b4 stopped"
    else
        killall b4 2>/dev/null || true
        echo "b4 stopped"
    fi
}

status() {
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "b4 is running (PID: $(cat "$PIDFILE"))"
        return 0
    elif pgrep -x b4 >/dev/null 2>&1; then
        echo "b4 is running (no pidfile)"
        return 0
    else
        echo "b4 is not running"
        return 1
    fi
}
BODY
}

# Create OpenWRT/Entware init script
create_sysv_service() {
    INIT_DIR=""

    if [ -d "/opt/etc/init.d" ] && [ -w "/opt/etc/init.d" ]; then
        INIT_DIR="/opt/etc/init.d"
        print_info "Detected Entware/MerlinWRT system"
    elif [ -d "/etc/init.d" ] && [ -w "/etc/init.d" ]; then
        INIT_DIR="/etc/init.d"
    elif [ -d "/opt/etc" ]; then
        mkdir -p /opt/etc/init.d 2>/dev/null && INIT_DIR="/opt/etc/init.d"
    fi

    if [ -z "$INIT_DIR" ]; then
        print_warning "Could not create init script - no writable init directory found"
        return
    fi

    print_info "Creating init script in $INIT_DIR..."

    if [ "$INIT_DIR" = "/etc/init.d" ]; then
        INIT_SCRIPT_NAME="b4"
    else
        INIT_SCRIPT_NAME="S99b4"
    fi

    INIT_FULL_PATH="${INIT_DIR}/${INIT_SCRIPT_NAME}"
    rm -f "${INIT_DIR}/S99b4" 2>/dev/null || true

    if [ -f "${INIT_DIR}/rc.func" ]; then
        # Merlin/Entware rc.func - completely different mechanism
        print_info "rc.func found in $INIT_DIR, using it for init script"
        cat >"${INIT_FULL_PATH}" <<'EOF'
#!/bin/sh
# B4 DPI Bypass Service Init Script

ENABLED=yes
PROCS=b4
ARGS="--config=CONFIG_PLACEHOLDER"
PREARGS="nohup"
DESC="$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

kernel_mod_load() {
    KERNEL=$(uname -r)
    connbytes_mod_path=$(find /lib/modules/$KERNEL -name "xt_connbytes.ko*" 2>/dev/null | head -1)
    [ -n "$connbytes_mod_path" ] && insmod "$connbytes_mod_path" >/dev/null 2>&1
    nfqueue_mod_path=$(find /lib/modules/$KERNEL -name "xt_NFQUEUE.ko*" 2>/dev/null | head -1)
    [ -n "$nfqueue_mod_path" ] && insmod "$nfqueue_mod_path" >/dev/null 2>&1
    multiport_mod_path=$(find /lib/modules/$KERNEL -name "xt_multiport.ko*" 2>/dev/null | head -1)
    [ -n "$multiport_mod_path" ] && insmod "$multiport_mod_path" >/dev/null 2>&1
    modprobe xt_connbytes >/dev/null 2>&1 || true
    modprobe xt_NFQUEUE >/dev/null 2>&1 || true
    modprobe xt_multiport >/dev/null 2>&1 || true
}

[ "$1" = "start" ] || [ "$1" = "restart" ] && kernel_mod_load

. /opt/etc/init.d/rc.func
EOF

    elif [ -f "/etc/openwrt_release" ] && [ -f "/etc/rc.common" ]; then
        # OpenWRT - rc.common handles command dispatch
        print_info "Creating OpenWRT init script"
        {
            echo '#!/bin/sh /etc/rc.common'
            echo '# B4 DPI Bypass Service - OpenWRT'
            echo ''
            echo 'START=99'
            echo 'STOP=10'
            echo ''
            get_init_script_body
        } >"${INIT_FULL_PATH}"

    else
        # Standard SysV init - needs case block
        print_info "Creating standard init.d script"
        {
            echo '#!/bin/sh'
            echo '# B4 DPI Bypass Service Init Script'
            echo ''
            get_init_script_body
            cat <<'EOF'

case "$1" in
    start)   start ;;
    stop)    stop ;;
    restart) stop; sleep 1; start ;;
    status)  status ;;
    *)       echo "Usage: $0 {start|stop|restart|status}"; exit 1 ;;
esac
EOF
        } >"${INIT_FULL_PATH}"
    fi

    sed "s|PROG_PLACEHOLDER|${INSTALL_DIR}/${BINARY_NAME}|g; s|CONFIG_PLACEHOLDER|${CONFIG_FILE}|g" \
        "${INIT_FULL_PATH}" >"${INIT_FULL_PATH}.tmp"
    mv "${INIT_FULL_PATH}.tmp" "${INIT_FULL_PATH}"
    chmod +x "${INIT_FULL_PATH}"

    if [ -f "/etc/openwrt_release" ] && [ -f "/etc/rc.common" ]; then
        "${INIT_FULL_PATH}" enable 2>/dev/null && print_success "Service enabled for auto-start on boot"
    fi

    print_success "Init script created at ${INIT_FULL_PATH}"
    print_info "  ${INIT_FULL_PATH} {start|stop|restart|status}"

    if [ -f "/etc/openwrt_release" ]; then
        print_info "  ${INIT_FULL_PATH} enable   # Start on boot"
        print_info "  ${INIT_FULL_PATH} disable  # Don't start on boot"
    fi
}

# --- END service.sh ---

# Get geosite path from config using jq if available
get_geodat_from_config() {
    if [ -f "$CONFIG_FILE" ] && command_exists jq; then
        sitedat_path=$(jq -r '.system.geo.sitedat_path // empty' "$CONFIG_FILE" 2>/dev/null)
        if [ -n "$sitedat_path" ] && [ "$sitedat_path" != "null" ]; then
            # Extract directory from path
            echo "$(dirname "$sitedat_path")"
            return 0
        fi
    fi
    return 1
}

# Display geosite source menu and get user choice
select_geo_source() {
    echo "" >&2
    echo "=======================================" >&2
    echo "  Select Geosite Data Source" >&2
    echo "=======================================" >&2
    echo "" >&2

    # Display sources (using POSIX-compliant iteration)
    echo "$GEODAT_SOURCES" | while IFS='|' read -r num name url; do
        if [ -n "$num" ]; then
            printf "  ${GREEN}%s${NC}) %s\n" "$num" "$name" >&2
        fi
    done

    echo "" >&2
    printf "${CYAN}Select source (1-5) or 'q' to skip: ${NC}" >&2
    read choice </dev/tty || choice="q"

    case "$choice" in
    [qQ] | [qQ][uU][iI][tT])
        return 1
        ;;
    [1-5])
        # Extract URL for selected choice (POSIX-compliant)
        selected_url=$(echo "$GEODAT_SOURCES" | grep "^${choice}|" | cut -d'|' -f3)
        if [ -n "$selected_url" ]; then
            echo "$selected_url"
            return 0
        else
            print_error "Invalid selection"
            return 1
        fi
        ;;
    *)
        print_error "Invalid selection"
        return 1
        ;;
    esac
}

download_geodat() {
    base_url="$1"
    save_dir="$2"

    sitedat_url="${base_url}/geosite.dat"
    ipdat_url="${base_url}/geoip.dat"
    sitedat_path="${save_dir}/geosite.dat"
    ipdat_path="${save_dir}/geoip.dat"

    # Verify save_dir is writable
    if [ ! -w "$(dirname "$save_dir")" ] && [ ! -d "$save_dir" ]; then
        if [ -d "/opt/etc" ] && [ -w "/opt/etc" ]; then
            save_dir="/opt/etc/b4"
            sitedat_path="${save_dir}/geosite.dat"
            ipdat_path="${save_dir}/geoip.dat"
            print_warning "Original path not writable, using: $save_dir"
        fi
    fi

    # Create directory
    if [ ! -d "$save_dir" ]; then
        mkdir -p "$save_dir" || {
            print_error "Failed to create directory: $save_dir"
            return 1
        }
    fi

    # Download geosite.dat
    print_info "Downloading geosite.dat from: $sitedat_url"
    if ! fetch_file "$sitedat_url" "$sitedat_path"; then
        print_error "Failed to download geosite.dat"
        return 1
    fi

    if [ ! -s "$sitedat_path" ]; then
        print_error "Downloaded geosite.dat is empty"
        rm -f "$sitedat_path"
        return 1
    fi

    # Download geoip.dat
    print_info "Downloading geoip.dat from: $ipdat_url"
    if ! fetch_file "$ipdat_url" "$ipdat_path"; then
        print_error "Failed to download geoip.dat"
        return 1
    fi

    if [ ! -s "$ipdat_path" ]; then
        print_error "Downloaded geoip.dat is empty"
        rm -f "$ipdat_path"
        return 1
    fi

    print_success "Geosite: $sitedat_path"
    print_success "GeoIP: $ipdat_path"
    return 0
}

# Update config file with geodat paths
update_config_geodat_path() {
    sitedat_path="$1"
    ipdat_path="$2"
    sitedat_url="$3/geosite.dat"
    ipdat_url="$3/geoip.dat"

    # Try to update with jq if available
    if command_exists jq; then
        print_info "Updating config file..."

        if [ ! -f "$CONFIG_FILE" ]; then
            jq -n \
                --arg sitedat_path "$sitedat_path" \
                --arg sitedat_url "$sitedat_url" \
                --arg ipdat_path "$ipdat_path" \
                --arg ipdat_url "$ipdat_url" \
                '{
                    system: {
                        geo: {
                            sitedat_path: $sitedat_path,
                            sitedat_url: $sitedat_url,
                            ipdat_path: $ipdat_path,
                            ipdat_url: $ipdat_url
                        }
                    }
                }' >"$CONFIG_FILE"
            print_success "Created new config file with geodat settings"
            return 0
        fi

        # Create temporary file
        temp_file="${CONFIG_FILE}.tmp"

        # Merge into existing geo object instead of replacing
        if jq \
            --arg sitedat_path "$sitedat_path" \
            --arg sitedat_url "$sitedat_url" \
            --arg ipdat_path "$ipdat_path" \
            --arg ipdat_url "$ipdat_url" \
            '.system.geo = (.system.geo // {}) + {
                 sitedat_path: $sitedat_path,
                 sitedat_url: $sitedat_url,
                 ipdat_path: $ipdat_path,
                 ipdat_url: $ipdat_url
             }' \
            "$CONFIG_FILE" >"$temp_file" 2>/dev/null; then

            mv "$temp_file" "$CONFIG_FILE" || {
                print_error "Failed to update config file"
                rm -f "$temp_file"
                return 1
            }
            print_success "Config updated:"
            print_success "  Geosite: $sitedat_path"
            print_success "  URL: $sitedat_url"
            print_success "  GeoIP:   $ipdat_path"
            print_success "  URL: $ipdat_url"

            # Show what was actually written
            print_info "Verifying config..."
            if command_exists jq; then
                jq '.system.geo' "$CONFIG_FILE" 2>/dev/null || true
            fi
            return 0
        else
            print_error "Failed to parse config with jq"
            rm -f "$temp_file"
            return 1
        fi
    else
        print_warning "jq not found - cannot automatically update config"
        print_info "Please manually add to your config file:"
        print_info '  "system": {'
        print_info '    "geo": {'
        print_info "      \"sitedat_path\": \"$sitedat_path\","
        print_info "      \"sitedat_url\": \"$sitedat_url\","
        print_info "      \"ipdat_path\": \"$ipdat_path\","
        print_info "      \"ipdat_url\": \"$ipdat_url\""
        print_info '    }'
        print_info '  }'
        echo ""
        print_info "Or update paths in B4 Web UI: Settings -> Geodat Settings"
        return 0
    fi
}

# Setup geosite data
setup_geodat() {
    echo ""
    echo "======================================="
    echo "  GEO Data Setup"
    echo "======================================="
    echo ""

    if [ -z "$GEOSITE_SRC" ] && [ -z "$GEOSITE_DST" ]; then
        # Skip prompts in quiet mode
        if [ "$QUIET_MODE" = "1" ]; then
            print_info "Geosite setup skipped (quiet mode)"
            return 0
        fi

        printf "${CYAN}Do you want to download geosite.dat & geoip.dat files? (y/N): ${NC}"
        read answer </dev/tty || answer="n"
    else
        answer="y"
    fi

    case "$answer" in
    [yY] | [yY][eE][sS])
        # Select source
        if [ -z "$GEOSITE_SRC" ]; then
            sitedat_url=$(select_geo_source)
            if [ $? -ne 0 ] || [ -z "$sitedat_url" ]; then
                print_info "Geosite setup skipped"
                return 0
            fi
        else
            sitedat_url="$GEOSITE_SRC"
            print_info "Using geosite source: $sitedat_url"
        fi

        # Set default directory BEFORE using it
        default_dir="$CONFIG_DIR"

        # Try to get existing path from config
        existing_dir=$(get_geodat_from_config || true)
        if [ -n "$existing_dir" ]; then
            default_dir="$existing_dir"
            print_info "Found existing geosite path in config: $default_dir"
        fi

        if [ -z "$GEOSITE_DST" ]; then
            # Skip in quiet mode - use default
            if [ "$QUIET_MODE" = "1" ]; then
                geosite_dst_dir="$default_dir"
            else
                echo ""
                printf "${CYAN}Save directory [${default_dir}]: ${NC}"
                read geosite_dst_dir </dev/tty || geosite_dst_dir="$default_dir"

                if [ -z "$geosite_dst_dir" ]; then
                    geosite_dst_dir="$default_dir"
                fi
            fi
        else
            geosite_dst_dir="$GEOSITE_DST"
            print_info "Using geodat destination: $geosite_dst_dir"
        fi

        # Download geosite file
        download_geodat "$sitedat_url" "$geosite_dst_dir"
        sitedat_path="${geosite_dst_dir}/geosite.dat"
        ipdat_path="${geosite_dst_dir}/geoip.dat"

        # Update config
        update_config_geodat_path "$sitedat_path" "$ipdat_path" "$sitedat_url"

        print_success "Geosite setup completed!"
        return 0

        ;;
    *)
        print_info "Geosite setup skipped"
        ;;
    esac

    echo ""
    return 0
}

# --- END geodat.sh ---

# This is the core installation part script for b4 Universal.
# Install b4 binary
install_b4() {
    arch="$1"
    version="$2"

    # Construct download URL
    file_name="${BINARY_NAME}-linux-${arch}.tar.gz"
    download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${file_name}"
    archive_path="${TEMP_DIR}/${file_name}"

    # Download the archive with checksum verification
    if ! download_file "$download_url" "$archive_path" "$version" "$arch"; then
        print_error "Failed to download b4 for architecture: $arch"
        exit 1
    fi

    rm -f "/opt/etc/init.d/S99b4" 2>/dev/null || true # remove legacy script
    rm -f "/etc/init.d/b4" 2>/dev/null || true        # remove legacy script
    rm -f "/var/log/b4.log" 2>/dev/null || true       # remove legacy log

    # Extract the binary
    print_info "Extracting archive..."
    cd "$TEMP_DIR"
    tar -xzf "$archive_path" || {
        print_error "Failed to extract archive"
        exit 1
    }

    # Check if binary exists
    if [ ! -f "${BINARY_NAME}" ]; then
        print_error "Binary not found in archive"
        exit 1
    fi

    # Stop existing b4 if running
    stop_process "$BINARY_NAME"

    # Create timestamp in POSIX way
    timestamp=$(date '+%Y%m%d_%H%M%S')
    BACKUP_FILE="${INSTALL_DIR}/${BINARY_NAME}.backup.${timestamp}"

    # Backup existing binary if it exists
    if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        print_info "Backing up existing binary..."
        mv "${INSTALL_DIR}/${BINARY_NAME}" "$BACKUP_FILE"
    fi

    # Install the new binary
    print_info "Installing b4 to ${INSTALL_DIR}..."
    cp "${BINARY_NAME}" "${INSTALL_DIR}/" || {
        print_error "Failed to copy binary to install directory"
        exit 1
    }

    # Set executable permissions
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}" || {
        print_error "Failed to set executable permissions"
        exit 1
    }

    # Verify installation
    if "${INSTALL_DIR}/${BINARY_NAME}" --version >/dev/null 2>&1; then
        [ -n "$BACKUP_FILE" ] && rm -f "$BACKUP_FILE" 2>/dev/null || true
        # Clean old backups
        rm -f "${INSTALL_DIR}/${BINARY_NAME}".backup.* 2>/dev/null || true
        print_success "b4 installed successfully!"
    else
        print_warning "Binary installed but version check failed"
    fi
}

# Print web interface access information
print_web_interface_info() {

    local web_port
    if [ -f "$CONFIG_FILE" ] && command_exists jq; then
        web_port=$(jq -r '.web_server.port // 7000' "$CONFIG_FILE" 2>/dev/null)
    fi

    echo ""
    echo "======================================="
    echo "  Web Interface Access"
    echo "======================================="
    echo ""

    # Get LAN IP (br0 interface on routers)
    lan_ip=""

    # Try to get IP from br0 specifically (most common on routers)
    if command_exists ip; then
        lan_ip=$(ip -4 addr show br0 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)
    fi

    # Fallback to ifconfig
    if [ -z "$lan_ip" ] && command_exists ifconfig; then
        lan_ip=$(ifconfig br0 2>/dev/null | grep 'inet addr:' | awk '{print $2}' | cut -d':' -f2)
    fi

    # If br0 didn't work, look for any 192.168.x.x address
    if [ -z "$lan_ip" ]; then
        if command_exists ip; then
            lan_ip=$(ip -4 addr show 2>/dev/null | grep 'inet 192.168' | head -n1 | awk '{print $2}' | cut -d'/' -f1)
        elif command_exists ifconfig; then
            lan_ip=$(ifconfig 2>/dev/null | grep 'inet addr:192.168' | head -n1 | awk '{print $2}' | cut -d':' -f2)
        fi
    fi

    # Print local/LAN access
    if [ -n "$lan_ip" ]; then
        print_info "Local network access (LAN):"
        printf "        ${GREEN}http://%s:%s${NC}\n" "$lan_ip" "$web_port"
        printf "        (remember to start the service first)\n"
    fi

    # # Get external IP
    # print_info "Checking external IP address..."
    # external_ip=""

    # if command_exists curl; then
    #     external_ip=$(curl -s --max-time 3 ifconfig.me 2>/dev/null || true)
    # elif command_exists wget; then
    #     external_ip=$(wget -qO- --timeout=3 ifconfig.me 2>/dev/null || true)
    # fi

    # # Print external access if different from LAN
    # if [ -n "$external_ip" ] && [ "$external_ip" != "$lan_ip" ]; then
    #     print_info "External access (WAN, if port ${web_port} is forwarded):"
    #     printf "        ${GREEN}http://%s:%s${NC}\n" "$external_ip" "$web_port"
    #     print_warning "Note: Ensure port ${web_port} is open in your firewall"
    # fi

    echo ""
}

# Main installation process
main_install() {

    #  get args
    VERSION=""
    for arg in "$@"; do
        case "$arg" in
        v* | V*)
            VERSION="$arg"
            print_info "Using specified version: $VERSION"
            ;;
        --quiet | -q)
            QUIET_MODE=1
            ;;
        --geosite-src=*)
            GEOSITE_SRC="${arg#*=}"
            ;;
        --geosite-dst=*)
            GEOSITE_DST="${arg#*=}"
            ;;
        esac
    done

    # Detect system and set paths
    set_system_paths

    if [ "$QUIET_MODE" = "0" ]; then
        echo ""
        echo "======================================="
        echo "     B4 Universal Installer"
        echo "======================================="
        echo ""
    fi

    # Check if running as root
    check_root

    # Check dependencies
    check_dependencies

    # Detect architecture
    print_info "Detecting system architecture..."
    ARCH=$(detect_architecture)
    print_info "Raw architecture: $(uname -m)"
    print_success "Architecture detected: $ARCH"

    if [ -z "$VERSION" ]; then
        print_info "Fetching latest release information..."
        VERSION=$(get_latest_version)
        print_success "Latest version: $VERSION"
    fi

    # Setup directories
    setup_directories

    # Install b4
    install_b4 "$ARCH" "$VERSION"

    # Create service files
    create_systemd_service
    if [ "$SYSTEMCTL_CREATED" != "1" ]; then
        create_sysv_service
    fi

    if [ "$QUIET_MODE" = "0" ]; then
        setup_geodat

        # Print installation summary
        echo ""
        print_info "Binary installed to: ${INSTALL_DIR}/${BINARY_NAME}"
        print_info "Configuration at: ${CONFIG_FILE}"
        echo ""
        print_info "To see all B4 options:"
        print_info "  ${INSTALL_DIR}/${BINARY_NAME} --help"

        # Check PATH
        if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
            print_warning "Note: $INSTALL_DIR is not in your PATH"
            print_info "You may want to add it to your PATH or create a symlink:"
            print_info "  ln -s ${INSTALL_DIR}/${BINARY_NAME} /usr/bin/${BINARY_NAME}"
        fi

        print_web_interface_info

        echo ""
        print_success "Installation finished successfully!"
        echo ""
        printf "${CYAN}Start B4 service now? (Y/n): ${NC}"
        read answer </dev/tty || answer="y"

        if [ -z "$answer" ]; then
            answer="y"
        fi

        case "$answer" in
        [nN] | [nN][oO])
            print_info "Service not started. Start manually when ready."
            ;;
        *)
            if [ -f "/etc/systemd/system/b4.service" ] && command_exists systemctl; then
                systemctl restart b4 2>/dev/null && print_success "Service started"
            elif [ -f "/opt/etc/init.d/S99b4" ]; then
                /opt/etc/init.d/S99b4 restart 2>/dev/null && print_success "Service started"
            elif [ -f "/etc/init.d/b4" ]; then
                /etc/init.d/b4 restart 2>/dev/null && print_success "Service started"
            fi
            ;;
        esac
        echo ""

        echo "======================================="
        echo "       Installation Complete!"
        echo "======================================="
    fi

}

# --- END core.sh ---

# B4 DPI Bypass Uninstaller Script
remove_b4() {
    echo ""
    echo "======================================="
    echo "     B4 Uninstaller"
    echo "======================================="
    echo ""

    # Detect system to get proper paths
    set_system_paths

    # Stop the service first
    print_info "Stopping b4 service if running..."

    # Check systemd service FIRST
    if [ -f "/etc/systemd/system/b4.service" ] && command_exists systemctl; then
        systemctl stop b4 2>/dev/null || true
        systemctl disable b4 2>/dev/null || true
        print_info "Stopped systemd service"
    fi

    # Check Entware init script
    if [ -f "/opt/etc/init.d/S99b4" ]; then
        /opt/etc/init.d/S99b4 stop 2>/dev/null || true
        print_info "Stopped Entware service"
    fi

    # Check standard init script
    if [ -f "/etc/init.d/b4" ]; then
        /etc/init.d/b4 stop 2>/dev/null || true
        print_info "Stopped init service"
    fi

    # Kill any remaining b4 processes
    kill_b4_processes

    # Remove binary from all possible locations
    POSSIBLE_DIRS="/opt/sbin /usr/local/bin /usr/bin /usr/sbin"
    for dir in $POSSIBLE_DIRS; do
        if [ -f "$dir/$BINARY_NAME" ]; then
            print_info "Removing binary: $dir/$BINARY_NAME"
            rm -f "$dir/$BINARY_NAME"
            print_success "Binary removed from $dir"

            # Remove any backup files
            rm -f "$dir/"${BINARY_NAME}.backup.* 2>/dev/null || true

        fi
    done

    # Remove service files
    if [ -f "/etc/systemd/system/b4.service" ]; then
        print_info "Removing systemd service..."
        rm -f "/etc/systemd/system/b4.service"
        if command_exists systemctl; then
            systemctl daemon-reload 2>/dev/null || true
        fi
        print_success "Systemd service removed"
    fi

    if [ -f "/opt/etc/init.d/S99b4" ]; then
        print_info "Removing Entware init script..."
        rm -f "/opt/etc/init.d/S99b4"
        print_success "Entware init script removed"
    fi

    if [ -f "/etc/init.d/b4" ]; then
        print_info "Removing init script..."
        rm -f "/etc/init.d/b4"
        print_success "Init script removed"
    fi

    # Remove symlinks
    if [ -L "/usr/bin/${BINARY_NAME}" ]; then
        print_info "Removing symlink: /usr/bin/${BINARY_NAME}"
        rm -f "/usr/bin/${BINARY_NAME}"
    fi

    # Ask about configuration ONCE
    printf "${CYAN}Remove configuration files as well? (y/N): ${NC}"
    read answer </dev/tty || answer="n"

    case "$answer" in
    [yY] | [yY][eE][sS])
        print_info "Removing configuration directory: $CONFIG_DIR"
        rm -rf "$CONFIG_DIR"
        print_success "Configuration removed"
        ;;
    *)
        print_info "Configuration preserved at: $CONFIG_DIR"
        ;;
    esac

    # Remove log files
    rm -f /var/log/b4.log 2>/dev/null || true
    rm -f /var/run/b4.pid 2>/dev/null || true

    echo ""
    print_success "B4 has been uninstalled successfully!"
    echo ""

    exit 0
}

# Kill any remaining b4 processes
kill_b4_processes() {
    # Collect PIDs first, avoid subshell issues
    pids=$(ps 2>/dev/null | grep -v grep | grep -v "b4install" | grep "b4$\|b4[[:space:]]" | awk '{print $1}' | tr '\n' ' ')

    if [ -n "$pids" ]; then
        print_info "Killing remaining b4 processes: $pids"

        # SIGTERM first
        for pid in $pids; do
            kill "$pid" 2>/dev/null || true
        done

        sleep 2

        # SIGKILL stubborn processes
        for pid in $pids; do
            if kill -0 "$pid" 2>/dev/null; then
                print_warning "Force killing PID $pid"
                kill -9 "$pid" 2>/dev/null || true
            fi
        done

        sleep 1
    fi
}

# --- END remove.sh ---

# Perform update - stops service, updates, and restarts
perform_update() {
    QUIET_MODE=1 # Force quiet mode during updates

    echo ""
    echo "======================================="
    echo "     B4 Update Process"
    echo "======================================="
    echo ""

    # Find existing installation
    FOUND_BINARY=""
    for dir in /opt/sbin /usr/local/bin /usr/bin /usr/sbin; do
        if [ -f "$dir/$BINARY_NAME" ]; then
            FOUND_BINARY="$dir/$BINARY_NAME"
            INSTALL_DIR="$dir"
            break
        fi
    done

    if [ -z "$FOUND_BINARY" ]; then
        print_error "B4 binary not found. Please install first."
        exit 1
    fi

    # Find existing config
    for dir in /opt/etc/b4 /etc/b4; do
        if [ -f "$dir/b4.json" ]; then
            CONFIG_DIR="$dir"
            CONFIG_FILE="$dir/b4.json"
            break
        fi
    done

    print_info "Found existing installation at: $INSTALL_DIR"
    print_info "Using config at: $CONFIG_DIR"

    # Detect service manager
    SERVICE_MANAGER=""
    RESTART_CMD=""

    if [ -f "/etc/systemd/system/b4.service" ] && command_exists systemctl; then
        SERVICE_MANAGER="systemd"
        RESTART_CMD="systemctl restart b4"
    elif [ -f "/opt/etc/init.d/S99b4" ]; then
        SERVICE_MANAGER="entware"
        RESTART_CMD="/opt/etc/init.d/S99b4 restart"
    elif [ -f "/etc/init.d/b4" ]; then
        SERVICE_MANAGER="init"
        RESTART_CMD="/etc/init.d/b4 restart"
    else
        print_error "No service manager detected. Cannot perform automatic update."
        exit 1
    fi

    print_info "Detected service manager: $SERVICE_MANAGER"

    # Extract geosite settings from config if available
    GEOSITE_SRC=""
    GEOSITE_DST=""
    if [ -f "$CONFIG_FILE" ] && command_exists jq; then
        GEOSITE_SRC=$(jq -r '.system.geo.sitedat_url // empty' "$CONFIG_FILE" 2>/dev/null)
        sitedat_path=$(jq -r '.system.geo.sitedat_path // empty' "$CONFIG_FILE" 2>/dev/null)
        if [ -n "$sitedat_path" ] && [ "$sitedat_path" != "null" ]; then
            GEOSITE_DST=$(dirname "$sitedat_path")
        fi
    fi

    # Stop the service
    print_info "Stopping b4 service..."
    case "$SERVICE_MANAGER" in
    systemd)
        systemctl stop b4 2>/dev/null || true
        ;;
    entware)
        /opt/etc/init.d/S99b4 stop 2>/dev/null || true
        ;;
    init)
        /etc/init.d/b4 stop 2>/dev/null || true
        ;;
    esac

    sleep 2
    print_success "Service stopped"

    # Perform the update (call main_install with quiet mode)
    print_info "Installing latest version..."
    main_install "$@"

    # Start the service
    print_info "Starting b4 service..."
    case "$SERVICE_MANAGER" in
    systemd)
        systemctl start b4
        ;;
    entware)
        /opt/etc/init.d/S99b4 start
        ;;
    init)
        /etc/init.d/b4 start
        ;;
    esac

    sleep 2

    # Verify service is running
    service_running=0
    case "$SERVICE_MANAGER" in
    systemd) systemctl is-active --quiet b4 && service_running=1 ;;
    entware | init) ps | grep -v grep | grep -q "b4$\|b4[[:space:]]" && service_running=1 ;;
    esac

    if [ "$service_running" = "1" ]; then
        print_success "Service started successfully"
        echo ""
        echo "======================================="
        echo "     Update Complete!"
        echo "======================================="
        echo ""
    else
        print_error "Service may not have started correctly"
        print_info "Check logs: journalctl -u b4 -f (systemd) or /var/log/b4.log"
        exit 1
    fi
}

# --- END update.sh ---

# Check kernel module status
check_kernel_module() {
    module_name="$1"

    # Check if module is loaded
    if lsmod 2>/dev/null | grep -q "^$module_name"; then
        echo "loaded"
        return 0
    fi

    # Skip filesystem check on routers - often hangs
    echo "unknown"
    return 0
}

# Get service status
get_service_status() {
    # Check if b4 process is running
    if ps 2>/dev/null | grep -v grep | grep -v "b4install" | grep -q "b4$\|b4[[:space:]]"; then
        echo "running"
        return 0
    fi

    # Check systemd service
    if [ -f "/etc/systemd/system/b4.service" ] && command_exists systemctl; then
        if systemctl is-active --quiet b4 2>/dev/null; then
            echo "running (systemd)"
            return 0
        else
            echo "stopped (systemd)"
            return 0
        fi
    fi

    # Check Entware init
    if [ -f "/opt/etc/init.d/S99b4" ]; then
        echo "configured (entware)"
        return 0
    fi

    # Check standard init
    if [ -f "/etc/init.d/b4" ]; then
        echo "configured (init.d)"
        return 0
    fi

    echo "not installed"
    return 0
}

# Get network interfaces info
get_network_info() {
    primary_ip=""
    if command_exists ip; then
        primary_ip=$(ip -4 route get 1 2>/dev/null | awk '/src/{print $7}' | head -1 || true)
    elif command_exists ifconfig; then
        primary_ip=$(ifconfig 2>/dev/null | grep 'inet addr:' | grep -v '127.0.0.1' | head -1 | awk '{print $2}' | cut -d':' -f2 || true)
    fi

    echo "$primary_ip"
}

# Detect firewall backend
detect_firewall_backend() {
    if command_exists nft; then
        out=$(nft list tables 2>/dev/null || true)
        if [ -n "$out" ]; then
            echo "nftables"
            return 0
        fi
    fi

    # Check for iptables
    if command_exists iptables; then
        out=$(iptables --version 2>/dev/null || true)
        if echo "$out" | grep -q "nf_tables"; then
            echo "iptables-nft"
        else
            echo "iptables-legacy"
        fi
        return 0
    fi

    echo "none"
    return 0
}

# Check if iptables multiport module works
check_multiport_support() {
    if ! command_exists iptables; then
        echo "no_iptables"
        return 0
    fi

    # Try to add a test rule using multiport
    # Try with -w first (lock wait), fall back without it for older iptables
    if iptables -w -t filter -A INPUT -p tcp -m multiport --dports 80,443 -j ACCEPT 2>/dev/null || \
       iptables -t filter -A INPUT -p tcp -m multiport --dports 80,443 -j ACCEPT 2>/dev/null; then
        # Success - remove it immediately (try both variants)
        iptables -w -t filter -D INPUT -p tcp -m multiport --dports 80,443 -j ACCEPT 2>/dev/null || \
        iptables -t filter -D INPUT -p tcp -m multiport --dports 80,443 -j ACCEPT 2>/dev/null
        # Check if loaded as module or built-in
        if lsmod 2>/dev/null | grep -q "^xt_multiport"; then
            echo "works_module"
        else
            echo "works_builtin"
        fi
        return 0
    fi

    echo "not_available"
    return 0
}

# System information display function
show_system_info() {

    set_system_paths

    echo ""
    echo "======================================="
    echo "       B4 System Information"
    echo "======================================="

    print_header "System Information"

    # OS Detection
    os_type="Unknown"
    if [ -f /etc/openwrt_release ]; then
        os_type="OpenWRT"
        os_version=$(grep 'DISTRIB_RELEASE' /etc/openwrt_release | cut -d'=' -f2 | tr -d "'\"" || true)
    elif [ -f /etc/merlinwrt_release ]; then
        os_type="MerlinWRT"
        os_version=$(cat /etc/merlinwrt_release 2>/dev/null || true)
    elif [ -f /etc/entware_release ]; then
        os_type="Entware"
        os_version=$(cat /etc/entware_release 2>/dev/null || true)
    elif [ -f /etc/os-release ]; then
        os_type=$(grep '^NAME=' /etc/os-release | cut -d'=' -f2 | tr -d '"' || echo "Linux")
        os_version=$(grep '^VERSION=' /etc/os-release | cut -d'=' -f2 | tr -d '"' || true)
    else
        os_type="Linux"
    fi

    print_detail "Operating System" "$os_type ${os_version}"
    print_detail "Kernel Version" "$(uname -r)"
    print_detail "Architecture (raw)" "$(uname -m)"
    print_detail "Architecture (b4)" "$(detect_architecture)"
    print_detail "Hostname" "$(hostname 2>/dev/null || echo 'unknown')"

    # CPU Info
    if [ -f /proc/cpuinfo ]; then
        cpu_model=$(grep 'model name' /proc/cpuinfo 2>/dev/null | head -1 | cut -d':' -f2 | sed 's/^ *//' || true)
        if [ -z "$cpu_model" ]; then
            cpu_model=$(grep 'Processor' /proc/cpuinfo 2>/dev/null | head -1 | cut -d':' -f2 | sed 's/^ *//' || true)
        fi
        if [ -n "$cpu_model" ]; then
            print_detail "CPU Model" "$cpu_model"
        fi

        cpu_cores=$(grep -c '^processor' /proc/cpuinfo 2>/dev/null || echo "1")
        print_detail "CPU Cores" "$cpu_cores"
    fi

    # Memory Info
    if [ -f /proc/meminfo ]; then
        mem_total=$(grep '^MemTotal:' /proc/meminfo | awk '{printf "%.0f MB", $2/1024}')
        mem_available=$(grep '^MemAvailable:' /proc/meminfo | awk '{printf "%.0f MB", $2/1024}')
        if [ -n "$mem_available" ]; then
            print_detail "Memory" "$mem_total (Available: $mem_available)"
        else
            # Fallback for older kernels without MemAvailable
            mem_free=$(grep '^MemFree:' /proc/meminfo | awk '{printf "%.0f MB", $2/1024}')
            print_detail "Memory" "$mem_total (Free: $mem_free)"
        fi
    fi

    # B4 Installation Status
    print_header "B4 Status"

    # Check if b4 is installed
    if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        print_detail "Binary Location" "${INSTALL_DIR}/${BINARY_NAME}"

        # Get version if possible
        if "${INSTALL_DIR}/${BINARY_NAME}" --version >/dev/null 2>&1; then
            b4_version=$("${INSTALL_DIR}/${BINARY_NAME}" --version 2>&1 | head -1)
            print_detail "Installed Version" "$b4_version"
        else
            print_detail "Installed Version" "Unknown (binary present)"
        fi

        # Check service status
        service_status=$(get_service_status)
        if echo "$service_status" | grep -q "running"; then
            print_detail "Service Status" "${GREEN}$service_status${NC}"
        else
            print_detail "Service Status" "${YELLOW}$service_status${NC}"
        fi
    else
        print_detail "Installation Status" "${RED}Not installed${NC}"
    fi

    # Check for config file
    if [ -f "$CONFIG_FILE" ]; then
        print_detail "Config File" "$CONFIG_FILE"

        # Check config content if jq is available
        if command_exists jq; then
            queue_num=$(jq -r '.queue_start_num // 537' "$CONFIG_FILE" 2>/dev/null || echo "537")
            threads=$(jq -r '.threads // 4' "$CONFIG_FILE" 2>/dev/null || echo "4")
            web_port=$(jq -r '.web_server.port // 0' "$CONFIG_FILE" 2>/dev/null || echo "0")
            print_detail "Queue Number" "$queue_num"
            print_detail "Worker Threads" "$threads"
            if [ "$web_port" != "0" ]; then
                print_detail "Web UI Port" "$web_port"
            fi
        fi
    else
        print_detail "Config File" "${YELLOW}Not found${NC}"
    fi

    # Check for geosite data
    if [ -f "$CONFIG_FILE" ] && command_exists jq; then
        sitedat_path=$(jq -r '.system.geo.sitedat_path // empty' "$CONFIG_FILE" 2>/dev/null)
        if [ -n "$sitedat_path" ] && [ "$sitedat_path" != "null" ] && [ -f "$sitedat_path" ]; then
            geosite_size=$(du -h "$sitedat_path" 2>/dev/null | cut -f1)
            print_detail "geosite.dat" "$sitedat_path ($geosite_size)"
        fi

        ipdat_path=$(jq -r '.system.geo.ipdat_path // empty' "$CONFIG_FILE" 2>/dev/null)
        if [ -n "$ipdat_path" ] && [ "$ipdat_path" != "null" ] && [ -f "$ipdat_path" ]; then
            ipdat_size=$(du -h "$ipdat_path" 2>/dev/null | cut -f1)
            print_detail "geoip.dat" "$ipdat_path ($ipdat_size)"
        fi
    fi

    # Show process details if running
    if ps 2>/dev/null | grep -v grep | grep -v "b4install" | grep -q "b4$\|b4[[:space:]]"; then
        b4_pid=$(ps 2>/dev/null | grep -v grep | grep -v "b4install" | grep "b4$\|b4[[:space:]]" | awk '{print $1}' | head -1)
        if [ -n "$b4_pid" ] && [ -d "/proc/$b4_pid" ]; then
            # Memory usage
            if [ -f "/proc/$b4_pid/status" ]; then
                rss=$(grep '^VmRSS:' /proc/$b4_pid/status 2>/dev/null | awk '{printf "%.1f MB", $2/1024}')
                [ -n "$rss" ] && print_detail "Memory Usage" "$rss (PID: $b4_pid)"
            fi
            # Uptime
            if [ -f "/proc/$b4_pid/stat" ] && [ -f "/proc/uptime" ]; then
                start_ticks=$(awk '{print $22}' /proc/$b4_pid/stat 2>/dev/null)
                uptime_sec=$(awk '{print int($1)}' /proc/uptime 2>/dev/null)
                hz=$(getconf CLK_TCK 2>/dev/null || echo 100)
                if [ -n "$start_ticks" ] && [ -n "$uptime_sec" ]; then
                    proc_uptime=$((uptime_sec - start_ticks / hz))
                    days=$((proc_uptime / 86400))
                    hours=$(((proc_uptime % 86400) / 3600))
                    mins=$(((proc_uptime % 3600) / 60))
                    if [ $days -gt 0 ]; then
                        print_detail "Uptime" "${days}d ${hours}h ${mins}m"
                    elif [ $hours -gt 0 ]; then
                        print_detail "Uptime" "${hours}h ${mins}m"
                    else
                        print_detail "Uptime" "${mins}m"
                    fi
                fi
            fi
        fi
    fi

    # Service Manager Detection
    print_header "Service Management"

    if [ -f "/etc/systemd/system/b4.service" ] && command_exists systemctl; then
        print_detail "Service Manager" "systemd"
        print_detail "Service File" "/etc/systemd/system/b4.service"
    elif [ -f "/opt/etc/init.d/S99b4" ]; then
        print_detail "Service Manager" "Entware init"
        print_detail "Service File" "/opt/etc/init.d/S99b4"
    elif [ -f "/etc/init.d/b4" ]; then
        print_detail "Service Manager" "SysV init"
        print_detail "Service File" "/etc/init.d/b4"
    else
        print_detail "Service Manager" "${YELLOW}None configured${NC}"
    fi

    # Firewall/Netfilter Status
    print_header "Firewall & Netfilter"

    firewall_backend=$(detect_firewall_backend)
    print_detail "Firewall Backend" "$firewall_backend"

    # Check for iptables
    if command_exists iptables; then
        iptables_version=$(iptables --version 2>&1 | head -1 | awk '{print $2}' | tr -d 'v')
        print_detail "iptables" "${GREEN}Available${NC} (v$iptables_version)"

        # Check for b4 rules in iptables
        if iptables -t mangle -L B4 -n 2>/dev/null | grep -q NFQUEUE; then
            print_detail "iptables Rules" "${GREEN}Active${NC}"
        fi
    else
        print_detail "iptables" "${YELLOW}Not found${NC}"
    fi

    # Check for nftables
    if command_exists nft; then
        nft_version=$(nft --version 2>&1 | awk '{print $2}' | tr -d 'v')
        print_detail "nftables" "${GREEN}Available${NC} (v$nft_version)"

        # Check for b4 rules in nftables
        if nft list table inet b4_mangle 2>/dev/null | grep -q queue; then
            print_detail "nftables Rules" "${GREEN}Active${NC}"
        fi
    else
        print_detail "nftables" "${YELLOW}Not found${NC}"
    fi

    # Check for ip6tables
    if command_exists ip6tables; then
        print_detail "ip6tables" "${GREEN}Available${NC}"
    else
        print_detail "ip6tables" "${YELLOW}Not found${NC}"
    fi

    # Check netfilter queue status
    if [ -f /proc/net/netfilter/nfnetlink_queue ]; then
        nfqueue_info=$(cat /proc/net/netfilter/nfnetlink_queue 2>/dev/null | grep -v "^#" | head -1 || true)
        if [ -n "$nfqueue_info" ]; then
            print_detail "NFQueue Status" "${GREEN}Available${NC}"
        else
            print_detail "NFQueue Status" "${YELLOW}Available (no queues)${NC}"
        fi
    else
        print_detail "NFQueue Status" "${RED}Not available${NC}"
    fi

    # Show queue stats if b4 is using queues
    if [ -f /proc/net/netfilter/nfnetlink_queue ]; then
        queue_stats=$(cat /proc/net/netfilter/nfnetlink_queue 2>/dev/null | grep -v "^#" | head -1)
        if [ -n "$queue_stats" ]; then
            queue_id=$(echo "$queue_stats" | awk '{print $1}')
            queue_dropped=$(echo "$queue_stats" | awk '{print $5}')
            print_detail "Queue ID" "$queue_id"
            if [ "$queue_dropped" != "0" ]; then
                print_detail "Queue Drops" "${YELLOW}$queue_dropped${NC}"
            fi
        fi
    fi

    print_header "Network Interfaces"

    # WAN interface (common names)
    # for iface in eth0 wan0 ppp0 vlan2; do
    #     if [ -d "/sys/class/net/$iface" ]; then
    #         ip_addr=$(ip -4 addr show "$iface" 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1 | head -1)
    #         [ -n "$ip_addr" ] && print_detail "WAN ($iface)" "$ip_addr"
    #         break
    #     fi
    # done

    # LAN interface
    for iface in br0 br-lan eth1 lan0; do
        if [ -d "/sys/class/net/$iface" ]; then
            ip_addr=$(ip -4 addr show "$iface" 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1 | head -1)
            [ -n "$ip_addr" ] && print_detail "LAN ($iface)" "$ip_addr"
            break
        fi
    done

    # Kernel Modules
    print_header "Kernel Modules"

    # Netfilter modules
    modules="nf_conntrack xt_connbytes xt_NFQUEUE xt_multiport nf_tables nft_queue nft_ct"
    for mod in $modules; do
        status=$(check_kernel_module "$mod" || true)
        case "$status" in
        loaded)
            print_detail "$mod" "${GREEN}Loaded${NC}"
            ;;
        available)
            print_detail "$mod" "${CYAN}Available${NC}"
            ;;
        unknown)
            print_detail "$mod" "${YELLOW}Not found${NC}"
            ;;
        esac
    done

    # Check conntrack settings
    if [ -f /proc/sys/net/netfilter/nf_conntrack_checksum ]; then
        checksum=$(cat /proc/sys/net/netfilter/nf_conntrack_checksum 2>/dev/null || echo "1")
        if [ "$checksum" = "0" ]; then
            print_detail "conntrack_checksum" "${GREEN}Disabled (good)${NC}"
        else
            print_detail "conntrack_checksum" "${YELLOW}Enabled${NC}"
        fi
    fi

    if [ -f /proc/sys/net/netfilter/nf_conntrack_tcp_be_liberal ]; then
        liberal=$(cat /proc/sys/net/netfilter/nf_conntrack_tcp_be_liberal 2>/dev/null || echo "0")
        if [ "$liberal" = "1" ]; then
            print_detail "tcp_be_liberal" "${GREEN}Enabled (good)${NC}"
        else
            print_detail "tcp_be_liberal" "${YELLOW}Disabled${NC}"
        fi
    fi

    # Check multiport support (iptables only)
    if [ "$firewall_backend" != "nftables" ] && command_exists iptables; then
        multiport_status=$(check_multiport_support)
        case "$multiport_status" in
        works_module)
            print_detail "iptables multiport" "${GREEN}Available (module)${NC}"
            ;;
        works_builtin)
            print_detail "iptables multiport" "${GREEN}Available (built-in)${NC}"
            ;;
        not_available)
            print_detail "iptables multiport" "${YELLOW}Not available (fallback mode)${NC}"
            ;;
        esac
    fi

    # Dependencies Check
    print_header "Dependencies"

    deps="wget curl tar jq sha256sum nohup"
    for dep in $deps; do
        if command_exists "$dep"; then
            print_detail "$dep" "${GREEN}Available${NC}"
        else
            print_detail "$dep" "${YELLOW}Not found${NC}"
        fi
    done

    # Package Manager Detection
    print_header "Package Management"

    if command_exists opkg; then
        print_detail "Package Manager" "opkg (OpenWRT/Entware)"
    elif command_exists apt-get; then
        print_detail "Package Manager" "apt (Debian/Ubuntu)"
    elif command_exists yum; then
        print_detail "Package Manager" "yum (RedHat/CentOS)"
    elif command_exists apk; then
        print_detail "Package Manager" "apk (Alpine)"
    else
        print_detail "Package Manager" "${YELLOW}None detected${NC}"
    fi

    # Recommendations
    print_header "Recommendations"

    recommendations=0

    # Check if running as root
    if [ "$USER" != "root" ] && ! (touch /etc/.root_test 2>/dev/null && rm -f /etc/.root_test 2>/dev/null); then
        printf "  ${YELLOW}⚠${NC}  Run this script as root for installation"
        recommendations=$((recommendations + 1))
    fi

    # Check for missing critical dependencies
    if ! command_exists wget && ! command_exists curl; then
        printf "  ${RED}✗${NC}  Install wget or curl for downloading"
        recommendations=$((recommendations + 1))
    fi

    if ! command_exists tar; then
        printf "  ${RED}✗${NC}  Install tar for extracting archives"
        recommendations=$((recommendations + 1))
    fi

    # Check for missing kernel modules
    if [ "$(check_kernel_module nf_conntrack)" = "missing" ]; then
        printf "  ${YELLOW}⚠${NC}  nf_conntrack module not found - may need kernel rebuild"
        recommendations=$((recommendations + 1))
    fi

    # Check firewall
    if [ "$firewall_backend" = "none" ]; then
        printf "  ${RED}✗${NC}  No firewall (iptables/nftables) detected"
        recommendations=$((recommendations + 1))
    fi

    # Check if b4 is installed but not running
    if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        service_status=$(get_service_status)
        if ! echo "$service_status" | grep -q "running"; then
            printf "  ${YELLOW}⚠${NC}  B4 is installed but not running"
            if [ -f "/etc/systemd/system/b4.service" ]; then
                printf "      Try: systemctl start b4"
            elif [ -f "/opt/etc/init.d/S99b4" ]; then
                printf "      Try: /opt/etc/init.d/S99b4 start"
            elif [ -f "/etc/init.d/b4" ]; then
                printf "      Try: /etc/init.d/b4 start"
            fi
            recommendations=$((recommendations + 1))
        fi
    fi

    if [ $recommendations -eq 0 ]; then
        printf "  ${GREEN}✓${NC}  System appears ready for B4"
    fi

    echo ""

}

# --- END sysinfo.sh ---

# Main function - parse arguments
main() {
    # Check for remove flag first
    for arg in "$@"; do
        case "$arg" in
        --remove | --uninstall | -r)
            check_root
            remove_b4
            exit 0
            ;;
        --update | -u)
            check_root
            perform_update "$@"
            exit 0
            ;;
        --info | -i | --sysinfo)
            show_system_info
            exit 0
            ;;
        --help | -h)
            echo "Usage: $0 [OPTIONS] [VERSION]"
            echo ""
            echo "Options:"
            echo "  --sysinfo, -i     Show system information and b4 status"
            echo "  --remove, -r      Uninstall b4 from the system"
            echo "  --update, -u      Update b4 to latest version"
            echo "  --help, -h        Show this help message"
            echo "  --quiet, -q       Suppress output except for errors"
            echo "  --geosite-src URL Specify geosite.dat source URL"
            echo "  --geosite-dst DIR Specify directory to save geosite.dat"
            echo "  VERSION           Install specific version (e.g., v1.4.0)"
            echo ""
            echo "Examples:"
            echo "  $0                Install latest version"
            echo "  $0 v1.4.0         Install version 1.4.0"
            echo "  $0 --sysinfo      Show system diagnostics"
            echo "  $0 --update       Update to latest version"
            echo "  $0 --remove       Uninstall b4"
            exit 0
            ;;
        esac
    done

    # No remove/update flag found, proceed with installation
    main_install "$@"
}

# Run main function
main "$@"

# --- END main.sh ---

