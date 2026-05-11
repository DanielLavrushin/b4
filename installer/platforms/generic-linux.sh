#!/bin/sh
# Platform: Generic Linux (Ubuntu, Debian, Fedora, Arch, Alpine, etc.)
# Covers any systemd-based or sysv-init desktop/server Linux

platform_generic_linux_name() {
    echo "Generic Linux (Ubuntu/Debian/Fedora/Arch/Alpine)"
}

platform_generic_linux_match() {
    # Match any Linux with systemd or standard init.d
    # This is the lowest-priority fallback — other platforms should match first
    [ "$(uname -s)" = "Linux" ] || return 1

    # Don't match if this looks like a router firmware
    [ -f /etc/openwrt_release ] && return 1
    [ -f /etc/merlinwrt_release ] && return 1
    [ -d /jffs ] && [ -d /opt/etc/init.d ] && return 1 # Merlin with Entware
    [ -d /etc/storage ] && [ -d /etc_ro ] && return 1  # Padavan
    [ -d /var/run/ndm ] && return 1                    # Keenetic NDMS
    command_exists ndmc && return 1                    # Keenetic NDMS
    command_exists nvram && nvram get firmver 2>/dev/null | grep -qi "merlin" && return 1
    [ -f /proc/device-tree/model ] && grep -qi "keenetic" /proc/device-tree/model 2>/dev/null && return 1

    # Match systemd or standard init
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
    # Warn about LXC container requirements
    _generic_linux_check_lxc

    missing=""

    # Check basic tools
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

    # Check kernel modules
    _generic_linux_check_kmods

    # Recommended packages
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
        # Prefer nftables on Alpine (apk) — iptables often doesn't work in LXC
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
    # Standard Linux — no special storage detection needed
    return 0
}

register_platform "generic_linux"
