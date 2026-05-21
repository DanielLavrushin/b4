#!/bin/sh
# Action: Install b4

action_install() {
    version="$1"
    force_arch="$2"

    check_root

    # --- Wizard ---
    if [ "$QUIET_MODE" -eq 1 ]; then
        WIZARD_MODE="auto"
        _user_bin_dir="$B4_BIN_DIR"
        _user_data_dir="$B4_DATA_DIR"
        if [ -n "$_user_bin_dir" ] && ! is_abs_path "$_user_bin_dir"; then
            log_err "B4_BIN_DIR must be an absolute path (got: $_user_bin_dir)"
            exit 1
        fi
        if [ -n "$_user_data_dir" ] && ! is_abs_path "$_user_data_dir"; then
            log_err "B4_DATA_DIR must be an absolute path (got: $_user_data_dir)"
            exit 1
        fi
        platform_auto_detect
        platform_call info
        [ -n "$_user_bin_dir" ] && B4_BIN_DIR="$_user_bin_dir"
        [ -n "$_user_data_dir" ] && B4_DATA_DIR="$_user_data_dir"
        [ -n "$_user_data_dir" ] && B4_CONFIG_FILE="${_user_data_dir}/b4.json"
        B4_ARCH="${force_arch:-$(detect_architecture)}"
        detect_pkg_manager
        # Enable all default features in quiet mode
        for f in $REGISTERED_FEATURES; do
            fdefault=$(feature_dispatch "$f" default_enabled)
            [ "$fdefault" = "yes" ] && ENABLED_FEATURES="${ENABLED_FEATURES} ${f}"
        done
    else
        wizard_start

        case "$WIZARD_MODE" in
        auto)
            wizard_auto_detect
            ;;
        manual)
            wizard_manual_configure
            ;;
        esac

        # Override arch if user forced it
        [ -n "$force_arch" ] && B4_ARCH="$force_arch"

        # Feature selection
        wizard_select_features
    fi

    echo ""
    log_header "Installing B4"

    # --- Check dependencies ---
    log_info "Checking dependencies..."
    platform_call check_deps

    # --- Resolve version ---
    if [ -z "$version" ]; then
        log_info "Fetching latest version..."
        version=$(get_latest_version)
    fi
    log_ok "Version: ${version}"
    log_ok "Architecture: ${B4_ARCH}"

    # --- Prepare directories ---
    ensure_dir "$B4_BIN_DIR" "Binary directory" || exit 1
    ensure_dir "$B4_DATA_DIR" "Data directory" || exit 1
    setup_temp

    # --- Download & install binary ---
    file_name="${BINARY_NAME}-linux-${B4_ARCH}.tar.gz"
    download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${file_name}"
    archive_path="${TEMP_DIR}/${file_name}"

    log_info "Downloading b4..."
    if ! fetch_file "$download_url" "$archive_path"; then
        log_err "Download failed for architecture: ${B4_ARCH}"
        exit 1
    fi

    # Verify checksum
    sha_url="${download_url}.sha256"
    _cs_ret=0
    verify_checksum "$archive_path" "$sha_url" || _cs_ret=$?
    # exit code 2 = actual SHA256 mismatch (corrupted/tampered download)
    if [ "$_cs_ret" -eq 2 ]; then
        log_warn "Checksum mismatch — download may be corrupted"
        if ! confirm "Continue anyway?"; then
            exit 1
        fi
    fi

    # Extract
    log_info "Extracting..."
    cd "$TEMP_DIR"
    tar -xzf "$archive_path" || { log_err "Failed to extract archive"; exit 1; }
    rm -f "$archive_path"

    if [ ! -f "${BINARY_NAME}" ]; then
        log_err "Binary not found in archive"
        exit 1
    fi

    # Stop running instance
    stop_b4

    # Remove stale stdout log files from older service scripts
    rm -f /var/log/b4.log /opt/var/log/b4.log /tmp/log/b4.log 2>/dev/null || true

    # Backup existing binary
    if [ -f "${B4_BIN_DIR}/${BINARY_NAME}" ]; then
        ts=$(date '+%Y%m%d_%H%M%S')
        mv "${B4_BIN_DIR}/${BINARY_NAME}" "${B4_BIN_DIR}/${BINARY_NAME}.backup.${ts}"
        log_info "Existing binary backed up"
    fi

    # Install
    mv "${BINARY_NAME}" "${B4_BIN_DIR}/" 2>/dev/null || cp "${BINARY_NAME}" "${B4_BIN_DIR}/" || {
        log_err "Failed to install binary to ${B4_BIN_DIR}"
        exit 1
    }
    chmod +x "${B4_BIN_DIR}/${BINARY_NAME}"

    # Verify — detect architecture mismatch (SIGILL on MIPS = wrong float ABI)
    _ver_exit=0
    sh -c "\"${B4_BIN_DIR}/${BINARY_NAME}\" --version" >/dev/null 2>&1 || _ver_exit=$?

    if [ "$_ver_exit" -eq 0 ]; then
        installed_ver=$("${B4_BIN_DIR}/${BINARY_NAME}" --version 2>&1 | head -1)
        log_ok "Binary installed: ${installed_ver}"
        rm -f "${B4_BIN_DIR}/${BINARY_NAME}".backup.* 2>/dev/null || true
    elif [ "$_ver_exit" -gt 128 ] && echo "$B4_ARCH" | grep -q "^mips" && ! echo "$B4_ARCH" | grep -q "softfloat"; then
        # Binary crashed (SIGILL/segfault) on MIPS hardfloat — retry with softfloat
        _sf_arch="${B4_ARCH}_softfloat"
        log_warn "Binary crashed (exit code $_ver_exit) — likely hardfloat/softfloat mismatch"
        log_info "Retrying with ${_sf_arch}..."

        _sf_file="${BINARY_NAME}-linux-${_sf_arch}.tar.gz"
        _sf_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${_sf_file}"
        _sf_archive="${TEMP_DIR}/${_sf_file}"

        if fetch_file "$_sf_url" "$_sf_archive"; then
            cd "$TEMP_DIR"
            rm -f "${BINARY_NAME}" 2>/dev/null
            tar -xzf "$_sf_archive" 2>/dev/null && rm -f "$_sf_archive"
            if [ -f "${BINARY_NAME}" ]; then
                mv "${BINARY_NAME}" "${B4_BIN_DIR}/" 2>/dev/null || cp "${BINARY_NAME}" "${B4_BIN_DIR}/"
                chmod +x "${B4_BIN_DIR}/${BINARY_NAME}"
                if "${B4_BIN_DIR}/${BINARY_NAME}" --version >/dev/null 2>&1; then
                    installed_ver=$("${B4_BIN_DIR}/${BINARY_NAME}" --version 2>&1 | head -1)
                    log_ok "Softfloat binary works: ${installed_ver}"
                    log_info "Tip: use --arch=${_sf_arch} for future installs"
                    B4_ARCH="$_sf_arch"
                    rm -f "${B4_BIN_DIR}/${BINARY_NAME}".backup.* 2>/dev/null || true
                else
                    log_err "Softfloat binary also failed — manual troubleshooting needed"
                    log_info "Run with --sysinfo for diagnostics, or try --arch=<arch> manually"
                fi
            else
                log_err "Failed to extract softfloat binary"
            fi
        else
            log_err "Could not download softfloat variant"
            log_info "Try reinstalling with: --arch=${_sf_arch}"
        fi
    else
        log_warn "Binary installed but version check failed (exit code: $_ver_exit)"
    fi

    # --- Install service ---
    log_info "Setting up service..."
    service_call install

    # --- Run enabled features ---
    if [ -n "$ENABLED_FEATURES" ]; then
        features_run
    fi

    # --- Summary ---
    _install_summary "$version"
}

_install_summary() {
    version="$1"

    echo ""
    log_header "Installation Complete"
    log_sep
    log_detail "Version" "$version"
    log_detail "Binary" "${B4_BIN_DIR}/${BINARY_NAME}"
    log_detail "Config" "${B4_CONFIG_FILE}"
    log_detail "Service" "${B4_SERVICE_TYPE}"
    log_sep

    # Check if binary is in PATH
    if ! echo "$PATH" | grep -q "$B4_BIN_DIR"; then
        log_warn "$B4_BIN_DIR is not in PATH"
        log_info "Consider: ln -s ${B4_BIN_DIR}/${BINARY_NAME} /usr/bin/${BINARY_NAME}"
    fi

    # Show web interface info
    _show_web_info

    echo ""
    log_info "To see all options: ${B4_BIN_DIR}/${BINARY_NAME} --help"
    echo ""

    # Offer to start/restart service
    if [ "$QUIET_MODE" -eq 0 ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        if is_b4_running; then
            if confirm "B4 is already running. Restart now?"; then
                service_call stop || true
                sleep 1
                service_call start || true
            fi
        else
            if confirm "Start B4 service now?"; then
                service_call start || true
            fi
        fi
    fi

    echo ""
    printf "${GREEN}${BOLD}  B4 installation finished!${NC}\n"
    echo ""
}

_show_web_info() {
    web_port="7000"
    protocol="http"

    if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
        web_port=$(jq -r '.system.web_server.port // 7000' "$B4_CONFIG_FILE" 2>/dev/null) || true
        tls=$(jq -r '.system.web_server.tls_cert // ""' "$B4_CONFIG_FILE" 2>/dev/null) || true
        [ -n "$tls" ] && protocol="https"
    fi

    # Try to get LAN IP
    lan_ip=""
    if command_exists ip; then
        lan_ip=$(ip -4 addr show br0 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)
        [ -z "$lan_ip" ] && lan_ip=$(ip -4 addr 2>/dev/null | grep 'inet 192.168' | head -1 | awk '{print $2}' | cut -d'/' -f1)
    fi

    if [ -n "$lan_ip" ]; then
        echo ""
        log_info "Web interface: ${protocol}://${lan_ip}:${web_port}"
    fi
}
