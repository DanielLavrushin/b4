#!/bin/sh
# Action: Remove b4

action_remove() {
    check_root

    log_header "Removing B4"

    # Must run even when --platform= presets B4_PLATFORM: platform_<id>_info is
    # what sets B4_SERVICE_TYPE/DIR/NAME, and without it service_call remove is
    # skipped and the boot symlink is left dangling.
    platform_auto_detect || true
    if [ -n "$B4_PLATFORM" ]; then
        platform_call info
    fi

    # Find config file — check all known locations
    _remove_find_config

    # Stop running process. Deleting the binary and init script out from under a
    # live b4 would leave it running with its nft tables and ip rules installed
    # and nothing left on disk to stop it.
    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        service_call stop 2>/dev/null || true
    fi
    _b4_stopped=1
    stop_b4 || _b4_stopped=0
    if [ "$_b4_stopped" -eq 0 ]; then
        log_err "b4 is still running and could not be stopped"
        log_info "Stop it by hand and re-run, or its firewall rules will be left behind."
        exit 1
    fi

    # Remove service
    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
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
            fi
        done
        command_exists systemctl && systemctl daemon-reload 2>/dev/null || true
    fi

    # Remove features (geodat etc. — reads paths from config)
    features_remove

    # Remove binary from known locations
    _removed_any=0
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
    if [ "$_removed_any" -eq 0 ]; then
        _stray=$(command -v "$BINARY_NAME" 2>/dev/null || true)
        if [ -n "$_stray" ]; then
            log_warn "A b4 binary is still on PATH at ${_stray} — remove it by hand"
        fi
    fi

    # Ask about config directories
    _remove_config_dirs

    # Best-effort netfilter cleanup. b4 tears its own rules down on a clean
    # shutdown, but an instance that had to be SIGKILLed leaves them installed —
    # and past this point there is no b4 left on disk to do it.
    if command_exists nft; then
        for _t in b4_mangle b4_nat b4_route b4_dnsnat; do
            nft delete table inet "$_t" 2>/dev/null || true
        done
    fi
    if command_exists ip; then
        if ip rule show 2>/dev/null | grep -q "fwmark"; then
            log_warn "Policy routing rules with an fwmark are still present"
            log_info "b4's own are gone once it shut down cleanly; review with: ip rule show"
        fi
    fi

    # Cleanup
    rm -f /var/run/b4.pid /run/b4.pid /opt/var/run/b4.pid /tmp/b4.pid 2>/dev/null || true
    rm -f /var/log/b4.log /opt/var/log/b4.log /tmp/log/b4.log 2>/dev/null || true
    rm -rf /var/log/b4 2>/dev/null || true

    echo ""
    log_ok "B4 has been removed"
    echo ""
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
