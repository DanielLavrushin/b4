#!/bin/sh
# Action: Update b4 to latest version

LEGACY_SERVICE_LOGS="/var/log/b4.log /opt/var/log/b4.log /tmp/log/b4.log"

installed_service_type() {
    _svc="$1"
    if grep -qF "[Service]" "$_svc" 2>/dev/null || grep -qF "ExecStart=" "$_svc" 2>/dev/null; then
        echo "systemd"
    elif grep -q "USE_PROCD\|procd_open_instance" "$_svc" 2>/dev/null; then
        echo "procd"
    elif grep -q "openrc-run" "$_svc" 2>/dev/null; then
        echo "openrc"
    elif grep -q "rc\.func\|/opt/var/run/b4\.pid" "$_svc" 2>/dev/null; then
        echo "entware"
    else
        echo "sysv"
    fi
}

refresh_legacy_service_script() {
    [ -z "$B4_SERVICE_DIR" ] && return 0
    [ -z "$B4_SERVICE_NAME" ] && return 0

    _svc="${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    [ -f "$_svc" ] || return 0
    grep -q "b4\.log" "$_svc" 2>/dev/null || return 0

    _svc_type=$(installed_service_type "$_svc")
    if [ "$_svc_type" = "systemd" ]; then
        log_warn "Systemd unit ${_svc} sends b4 output to a legacy log file"
        log_info "Leaving the unit as it is, b4 did not write it"
        return 0
    fi

    log_warn "Init script logs b4 output to a legacy file that is never rotated"
    log_info "Refreshing ${_svc_type} service script: ${_svc}"

    if service_dispatch "$_svc_type" install >/dev/null 2>&1; then
        log_ok "Service script refreshed"
    else
        log_warn "Could not regenerate the service script, patching the redirect in place"
        for _legacy in $LEGACY_SERVICE_LOGS; do
            _esc=$(echo "$_legacy" | sed 's#\.#\\.#g')
            sed -i "s#>${_esc} 2>&1#>/dev/null 2>\&1#g" "$_svc" 2>/dev/null || true
            sed -i "s#\"${_esc}\"#\"/dev/null\"#g" "$_svc" 2>/dev/null || true
            sed -i "s#${_esc}#/var/log/b4/errors.log#g" "$_svc" 2>/dev/null || true
        done
    fi

    for _legacy in $LEGACY_SERVICE_LOGS; do
        if [ -f "$_legacy" ]; then
            _sz=$(du -sk "$_legacy" 2>/dev/null | awk '{print $1}')
            [ -n "$_sz" ] && log_info "Removing stale log ${_legacy} (${_sz} KB)"
            rm -f "$_legacy" 2>/dev/null || true
        fi
    done
}

action_update() {
    target_ver="$1"
    force_arch="$2"

    check_root

    log_header "Updating B4"

    # Detect platform
    if [ -z "$B4_PLATFORM" ]; then
        platform_auto_detect || true
        if [ -n "$B4_PLATFORM" ]; then
            platform_call info
        fi
    fi

    # Find existing binary
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

    # Get current version
    _ver_full=$("$existing_bin" --version 2>&1) || _ver_full=""
    current_ver=$(echo "$_ver_full" | grep -i "version" | head -1)
    [ -z "$current_ver" ] && current_ver="unknown"
    log_info "Current: ${current_ver}"

    # Detect arch from existing binary or system
    if [ -n "$force_arch" ]; then
        B4_ARCH="$force_arch"
    else
        B4_ARCH=$(detect_architecture)
    fi

    if [ -n "$B4_LOCAL_ARCHIVE" ]; then
        if [ -n "$target_ver" ]; then
            log_warn "Ignoring a supplied archive: ${target_ver} was asked for by name"
            B4_LOCAL_ARCHIVE=""
        elif [ ! -f "$B4_LOCAL_ARCHIVE" ]; then
            log_warn "Ignoring a stale archive path left by an earlier run: ${B4_LOCAL_ARCHIVE}"
            B4_LOCAL_ARCHIVE=""
        fi
    fi

    # Get target version
    if [ -n "$B4_LOCAL_ARCHIVE" ]; then
        latest_ver=""
        log_info "Source: ${B4_LOCAL_ARCHIVE}"
    elif [ -n "$target_ver" ]; then
        latest_ver="$target_ver"
        log_info "Target: ${latest_ver}"
    else
        log_info "Checking for updates..."
        latest_ver=$(get_latest_version)
        log_info "Latest: ${latest_ver}"
    fi

    if [ -z "$B4_LOCAL_ARCHIVE" ]; then
        if [ "$current_ver" = "$latest_ver" ] || echo "$current_ver" | grep -Fq "$latest_ver"; then
            log_ok "Already up to date"
            return 0
        fi
    fi

    if [ "$QUIET_MODE" -eq 0 ]; then
        if [ -n "$B4_LOCAL_ARCHIVE" ]; then
            _confirm_msg="Install the supplied archive?"
        else
            _confirm_msg="Update to ${latest_ver}?"
        fi
        if ! confirm "$_confirm_msg"; then
            log_info "Update cancelled"
            return 0
        fi
    fi

    # Download and install
    setup_temp

    file_name="${BINARY_NAME}-linux-${B4_ARCH}.tar.gz"

    if [ -n "$B4_LOCAL_ARCHIVE" ]; then
        case "$B4_LOCAL_ARCHIVE" in
        /*) ;;
        *)
            log_err "Archive path must be absolute: ${B4_LOCAL_ARCHIVE}"
            exit 1
            ;;
        esac
        if [ ! -f "$B4_LOCAL_ARCHIVE" ]; then
            log_err "Archive not found: ${B4_LOCAL_ARCHIVE}"
            exit 1
        fi
        archive_path="$B4_LOCAL_ARCHIVE"
        log_info "Installing from a supplied archive, no download"
    else
        download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${latest_ver}/${file_name}"
        archive_path="${TEMP_DIR}/${file_name}"

        log_info "Downloading ${latest_ver}..."
        fetch_file "$download_url" "$archive_path" || {
            log_err "Download failed"
            exit 1
        }

        # Verify
        sha_url="${download_url}.sha256"
        _cs_ret=0
        verify_checksum "$archive_path" "$sha_url" || _cs_ret=$?
        if [ "$_cs_ret" -ne 0 ]; then
            if [ "$_cs_ret" -eq 2 ]; then
                log_err "SHA256 mismatch: the archive is not the published release"
            else
                log_err "The archive could not be checked against its published SHA256"
            fi
            if [ "$QUIET_MODE" -eq 1 ]; then
                log_err "Refusing to install an unverified binary unattended"
                exit 1
            fi
            if ! confirm "Install it anyway?" "n"; then
                exit 1
            fi
        fi
    fi

    # Extract
    cd "$TEMP_DIR"
    tar -xzf "$archive_path" || {
        log_err "Extraction failed"
        exit 1
    }

    if [ ! -f "${TEMP_DIR}/${BINARY_NAME}" ]; then
        log_err "Binary not found in archive"
        exit 1
    fi

    if [ -n "$B4_LOCAL_ARCHIVE" ] && [ "$B4_LOCAL_ARCHIVE_OWNED" = "1" ]; then
        rm -f "$B4_LOCAL_ARCHIVE" 2>/dev/null || true
    fi

    bin_dir=$(dirname "$existing_bin")
    ensure_bin_space "$bin_dir" "${TEMP_DIR}/${BINARY_NAME}" || exit 1

    saved_cmdline=$(b4_running_cmdline 2>/dev/null || true)
    [ -n "$saved_cmdline" ] && log_info "Running command line: ${saved_cmdline}"

    # Stop service properly (prevents systemd/procd auto-restart race condition)
    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        log_info "Stopping service (${B4_SERVICE_TYPE})..."
        service_call stop 2>/dev/null || true
        sleep 1
    fi

    if is_b4_running; then
        log_info "Process still running after service stop — forcing stop"
        stop_b4
    fi
    if is_b4_running; then
        log_warn "Could not stop the running b4 process; replacing binary anyway"
    fi

    ts=$(date '+%Y%m%d_%H%M%S')
    backup_bin="${existing_bin}.backup.${ts}"

    stash_binary "$existing_bin" "$backup_bin" || {
        log_err "Could not move the current binary aside"
        exit 1
    }

    update_failed=0
    if mv "${TEMP_DIR}/${BINARY_NAME}" "$existing_bin" 2>/dev/null ||
        cp "${TEMP_DIR}/${BINARY_NAME}" "$existing_bin"; then
        chmod +x "$existing_bin"
    else
        log_err "Failed to replace binary"
        update_failed=1
    fi

    # Verify
    if [ "$update_failed" -eq 0 ] && "$existing_bin" --version >/dev/null 2>&1; then
        new_ver=$("$existing_bin" --version 2>&1 | head -1)
        log_ok "Updated to: ${new_ver}"
        rm -f "$backup_bin" 2>/dev/null || true
    else
        if [ "$update_failed" -eq 0 ]; then
            log_warn "Updated binary failed version check"
        fi
        update_failed=1
        if restore_binary "$existing_bin" "$backup_bin"; then
            log_ok "Rolled back to the previous version"
        elif [ -f "$existing_bin" ]; then
            log_warn "No backup to roll back to, keeping the new binary"
        else
            log_err "No working binary in ${bin_dir}, reinstall b4 manually"
        fi
    fi

    refresh_legacy_service_script

    # Restart service if it was running
    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        log_info "Restarting service (${B4_SERVICE_TYPE})..."
        service_call start 2>/dev/null || true
        _wait=0
        while [ "$_wait" -lt 10 ]; do
            is_b4_running && break
            sleep 1
            _wait=$((_wait + 1))
        done
    fi

    if is_b4_running; then
        log_ok "b4 is running"
    elif [ "$B4_SERVICE_TYPE" = "systemd" ] || [ "$B4_SERVICE_TYPE" = "procd" ]; then
        log_err "b4 did not come back up under ${B4_SERVICE_TYPE}"
        log_info "Not starting it by hand: ${B4_SERVICE_TYPE} keeps its own restart schedule, and a second"
        log_info "b4 outside the service manager would lose the single-instance lock race with it"
        if [ "$B4_SERVICE_TYPE" = "systemd" ]; then
            log_info "  check: journalctl -u ${B4_SERVICE_NAME:-b4} --no-pager -n 20"
        fi
    elif [ -n "$saved_cmdline" ]; then
        log_info "Service manager did not restart b4, relaunching directly"
        if relaunch_b4 "$saved_cmdline"; then
            log_ok "b4 relaunched"
        else
            log_warn "Failed to relaunch b4, start it manually:"
            log_warn "  ${saved_cmdline}"
        fi
    else
        log_warn "b4 is not running after update, start it manually:"
        log_warn "  ${existing_bin} --config ${B4_CONFIG_FILE}"
    fi

    echo ""
    if [ "$update_failed" -eq 1 ]; then
        log_warn "Update did not complete, previous version is still in place"
        echo ""
        return 1
    fi
    log_ok "Update complete"
    echo ""
}
