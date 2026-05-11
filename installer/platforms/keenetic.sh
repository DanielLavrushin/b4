#!/bin/sh
# Platform: Keenetic routers (NDMS OS with Entware)
#
# Key characteristics:
#   - NDMS is a proprietary Linux-based OS (not OpenWrt)
#   - Root filesystem is read-only
#   - Entware provides /opt (USB or internal storage on newer models)
#   - Uses Entware init system (rc.func or standalone scripts)
#   - opkg is the package manager (via Entware)
#   - Older models: MIPS (MT7621 — mipsle_softfloat)
#   - Newer models: aarch64
#   - Kernel modules usually built into firmware
#   - May not have /opt/etc/entware_release file

platform_keenetic_name() {
    echo "Keenetic (NDMS)"
}

platform_keenetic_match() {
    # /proc/device-tree/model contains "keenetic"
    if [ -f /proc/device-tree/model ] && grep -qi "keenetic" /proc/device-tree/model 2>/dev/null; then
        return 0
    fi

    # NDMS-specific: /var/run/ndm exists or ndmc command available
    if [ -d /var/run/ndm ] || command_exists ndmc; then
        return 0
    fi

    # Keenetic with Entware but no entware_release file
    # /opt writable + read-only /etc + no /jffs (not Merlin) + no openwrt_release
    if [ -d "/opt/sbin" ] && [ -w "/opt/sbin" ] && [ ! -w "/etc" ] &&
        [ ! -d "/jffs" ] && [ ! -f /etc/openwrt_release ]; then
        # Check if it looks like NDMS (has /tmp/ndm or similar)
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

    # Check if Entware is installed
    if [ ! -d "/opt/etc/init.d" ] && [ ! -f "/opt/bin/opkg" ]; then
        log_warn "Entware not detected!"
        log_info "Entware is required on Keenetic. To install:"
        log_info "  1. Go to router admin panel > System Settings"
        log_info "  2. Enable OPKG package manager component"
        log_info "  3. For older models: plug in a USB drive and install Entware"
        log_info "  More info: https://help.keenetic.com/hc/en-us/articles/360021214160"

        # Try /tmp as last resort (non-persistent)
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
    # Check basic download tools
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

    # Kernel modules — on Keenetic, usually built into NDMS firmware
    _keenetic_load_kmods

    # Recommended packages
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

    # SSL support
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
    # Keenetic storage priority:
    # 1. /opt (Entware — USB or internal on newer models)
    # 2. /tmp — volatile, absolute last resort

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
