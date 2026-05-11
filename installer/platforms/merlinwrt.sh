#!/bin/sh
# Platform: Asus Merlin (Asuswrt-Merlin firmware with Entware)
#
# Key characteristics:
#   - Root filesystem is read-only (squashfs)
#   - /jffs is a persistent writable JFFS2 partition
#   - Entware provides /opt (usually on USB or /jffs)
#   - Uses Entware's rc.func init system (not systemd, not procd)
#   - opkg is the package manager (via Entware)
#   - Kernel modules are usually built into firmware

platform_merlinwrt_name() {
    echo "Asus Merlin (Asuswrt-Merlin)"
}

platform_merlinwrt_match() {
    # Check for Merlin-specific indicators

    # nvram firmware version contains "merlin"
    if command_exists nvram; then
        fw=$(nvram get firmver 2>/dev/null)
        bw=$(nvram get buildno 2>/dev/null)
        if echo "$fw $bw" | grep -qi "merlin"; then
            return 0
        fi
    fi

    # /jffs exists and is writable (Merlin signature)
    # plus Entware init structure
    if [ -d "/jffs" ] && [ -w "/jffs" ] && [ -d "/opt/etc/init.d" ]; then
        # Additional check: rc.func exists (Entware on Merlin)
        [ -f "/opt/etc/init.d/rc.func" ] && return 0
    fi

    # /etc/merlinwrt_release (some builds)
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

    # Check if Entware is actually installed
    if [ ! -d "/opt/etc/init.d" ]; then
        log_warn "Entware not detected!"
        log_info "Entware is required for MerlinWRT. Install it first:"
        log_info "  1. Plug in a USB drive and format it via the router admin panel"
        log_info "  2. Open SSH and run: amtm"
        log_info "  3. Select option 'ep' to install Entware"
        log_info "  More info: https://diversion.ch/amtm.html"

        # Fallback to /jffs if available
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

    # Kernel modules — on Merlin, most are built into firmware
    # Just try to load them, don't panic if they fail
    _merlinwrt_load_kmods

    # Recommended packages via Entware opkg
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

    # Check SSL support packages
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
    # Merlin storage priority:
    # 1. /opt (Entware on USB) — preferred, most space
    # 2. /jffs — persistent but limited (~60MB typically)
    # 3. /tmp — volatile, last resort

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
