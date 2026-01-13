#!/bin/sh
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
