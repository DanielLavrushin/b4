#!/bin/sh
# Action: Update b4 to latest version

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

    # Get target version
    if [ -n "$target_ver" ]; then
        latest_ver="$target_ver"
        log_info "Target: ${latest_ver}"
    else
        log_info "Checking for updates..."
        latest_ver=$(get_latest_version)
        log_info "Latest: ${latest_ver}"
    fi

    if [ "$current_ver" = "$latest_ver" ] || echo "$current_ver" | grep -Fq "$latest_ver"; then
        log_ok "Already up to date"
        return 0
    fi

    if [ "$QUIET_MODE" -eq 0 ]; then
        if ! confirm "Update to ${latest_ver}?"; then
            log_info "Update cancelled"
            return 0
        fi
    fi

    # Download and install
    setup_temp

    file_name="${BINARY_NAME}-linux-${B4_ARCH}.tar.gz"
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
    if [ "$_cs_ret" -eq 2 ]; then
        log_warn "Checksum mismatch — download may be corrupted"
        if ! confirm "Continue anyway?"; then
            exit 1
        fi
    fi

    # Extract
    cd "$TEMP_DIR"
    tar -xzf "$archive_path" || {
        log_err "Extraction failed"
        exit 1
    }

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
    cp "$existing_bin" "${existing_bin}.backup.${ts}"

    # Remove old binary first to avoid ETXTBSY if process is still running
    rm -f "$existing_bin"
    mv "${TEMP_DIR}/${BINARY_NAME}" "$existing_bin" 2>/dev/null ||
        cp "${TEMP_DIR}/${BINARY_NAME}" "$existing_bin" ||
        {
            log_err "Failed to replace binary"
            exit 1
        }
    chmod +x "$existing_bin"

    # Verify
    if "$existing_bin" --version >/dev/null 2>&1; then
        new_ver=$("$existing_bin" --version 2>&1 | head -1)
        log_ok "Updated to: ${new_ver}"
        rm -f "${existing_bin}".backup.* 2>/dev/null || true
    else
        log_warn "Updated binary failed version check"
    fi

    # Restart service if it was running
    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        log_info "Restarting service (${B4_SERVICE_TYPE})..."
        service_call start 2>/dev/null || true
        sleep 1
    fi

    if is_b4_running; then
        log_ok "b4 is running"
    elif [ -n "$saved_cmdline" ]; then
        log_info "Service manager did not restart b4 — relaunching directly"
        if relaunch_b4 "$saved_cmdline"; then
            log_ok "b4 relaunched with the new binary"
        else
            log_warn "Failed to relaunch b4 — start it manually:"
            log_warn "  ${saved_cmdline}"
        fi
    else
        log_warn "b4 is not running after update — start it manually:"
        log_warn "  ${existing_bin} --config ${B4_CONFIG_FILE}"
    fi

    echo ""
    log_ok "Update complete"
    echo ""
}
