#!/bin/sh
# Action: Remove b4

action_remove() {
    check_root

    log_header "Removing B4"

    platform_init || true

    # Find config file, check all known locations
    _remove_find_config

    # Stop running process
    service_stop_b4 || {
        log_err "b4 is still running and could not be stopped"
        log_info "Stop it by hand and re-run, or its firewall rules will be left behind."
        exit 1
    }

    _removed_any=0
    _remove_netfilter_state

    # Remove service
    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        if [ -n "$B4_SERVICE_DIR" ] && [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
            _removed_any=1
        fi
        log_info "Removing service..."
        service_call remove 2>/dev/null || true
    else
        # Manual cleanup of known service locations
        for svc in \
            /etc/systemd/system/b4.service \
            /etc/init.d/b4 \
            /opt/etc/init.d/S99b4; do
            if [ -f "$svc" ]; then
                rm -f "$svc"
                log_info "Removed: $svc"
                _removed_any=1
            fi
        done
        command_exists systemctl && systemctl daemon-reload 2>/dev/null || true
    fi

    # Remove features (geodat etc., reads paths from config)
    features_remove

    # Remove binary from known locations
    for dir in "$B4_BIN_DIR" /usr/local/bin /usr/bin /usr/sbin /opt/bin /opt/sbin /jffs/b4 /ssd/b4 /tmp/b4; do
        [ -z "$dir" ] && continue
        if [ -f "${dir}/${BINARY_NAME}" ]; then
            rm -f "${dir}/${BINARY_NAME}"
            rm -f "${dir}/${BINARY_NAME}".backup.* 2>/dev/null || true
            rm -f "${dir}/${BINARY_NAME}".new.* 2>/dev/null || true
            log_info "Removed binary from: ${dir}"
            _removed_any=1
        fi
    done
    _stray=$(command -v "$BINARY_NAME" 2>/dev/null || true)
    if [ -n "$_stray" ] && [ -f "$_stray" ]; then
        log_warn "A b4 binary is still on PATH at ${_stray} - remove it by hand"
    fi

    # Ask about config directories
    _remove_config_dirs

    # Cleanup
    rm -f /var/run/b4.pid /run/b4.pid /opt/var/run/b4.pid /tmp/b4.pid 2>/dev/null || true
    rm -f /var/run/b4-tun.state /run/b4-tun.state /tmp/b4_sysctl_snapshot.json 2>/dev/null || true
    rm -f /var/log/b4.log /opt/var/log/b4.log /tmp/log/b4.log 2>/dev/null || true
    rm -rf /var/log/b4 2>/dev/null || true

    echo ""
    if [ "$_removed_any" -eq 1 ]; then
        log_ok "B4 has been removed"
    else
        log_warn "Nothing to remove: no b4 binary or service was found"
    fi
    echo ""
}

_remove_netfilter_state() {
    _rns_bin=""
    for _rns_dir in "$B4_BIN_DIR" /usr/local/bin /usr/bin /usr/sbin /opt/bin /opt/sbin /jffs/b4 /ssd/b4 /tmp/b4; do
        [ -n "$_rns_dir" ] && [ -x "${_rns_dir}/${BINARY_NAME}" ] || continue
        _rns_bin="${_rns_dir}/${BINARY_NAME}"
        break
    done
    if [ -n "$_rns_bin" ]; then
        log_info "Clearing firewall and routing state..."
        if [ -n "$B4_CONFIG_FILE" ] && [ -f "$B4_CONFIG_FILE" ]; then
            _rns_out=$("$_rns_bin" --clear-tables --config "$B4_CONFIG_FILE" 2>&1) && return 0
        else
            _rns_out=$("$_rns_bin" --clear-tables 2>&1) && return 0
        fi
        log_warn "${_rns_bin} --clear-tables failed"
        if [ -n "$_rns_out" ]; then
            echo "$_rns_out" | tail -n 5 | while read -r _rns_line; do
                printf "    %s\n" "$_rns_line" >&2
            done
        fi
        log_warn "Removing the known nftables tables directly"
    fi
    command_exists nft || return 0
    for _rns_t in "inet b4_mangle" "inet b4_route" "ip b4_nat" "ip b4_dnsnat" "ip6 b4_dnsnat6"; do
        nft delete table ${_rns_t} 2>/dev/null || true
    done
}

# Find the active config file so features can read paths from it
_remove_find_config() {
    # Already set by platform detection or user override
    if [ -n "$B4_CONFIG_FILE" ] && [ -f "$B4_CONFIG_FILE" ]; then
        log_info "Using config: $B4_CONFIG_FILE"
        return 0
    fi

    # Search known locations
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

# Remove config directories, but list what's inside first
_remove_config_dirs() {
    # Collect unique config dirs to check
    checked=""
    for cfg_dir in "$B4_DATA_DIR" /etc/b4 /opt/etc/b4 /etc/storage/b4; do
        [ -z "$cfg_dir" ] && continue
        [ -d "$cfg_dir" ] || continue
        # Skip if already checked (exact match to avoid substring false positives)
        case " $checked " in
        *" $cfg_dir "*) continue ;;
        esac
        checked="${checked} ${cfg_dir}"

        # Show remaining contents
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
