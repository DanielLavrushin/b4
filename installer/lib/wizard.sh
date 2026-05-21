#!/bin/sh
# Interactive wizard — handles both manual and automatic modes

WIZARD_MODE="" # "auto" or "manual"

# Show the welcome banner and mode selection
wizard_start() {
    echo ""
    printf "${BOLD}"
    echo "  ╔═══════════════════════════════════════╗"
    echo "  ║       B4 Universal Installer          ║"
    echo "  ╚═══════════════════════════════════════╝"
    printf "${NC}"
    echo ""

    while true; do
        log_sep
        echo ""
        printf "  ${BOLD}1${NC}) Automatic detection ${DIM}(recommended)${NC}\n"
        printf "  ${BOLD}2${NC}) Manual configuration\n"
        printf "  ${BOLD}3${NC}) System info\n"
        printf "  ${DIM}e) Exit${NC}\n"
        echo ""

        read_input "Select mode [1]: " "1"

        case "$_INPUT" in
        2) WIZARD_MODE="manual"; return 0 ;;
        3)
            action_sysinfo
            echo ""
            read_input "Press Enter to return to menu..." ""
            echo ""
            ;;
        *) WIZARD_MODE="auto"; return 0 ;;
        esac
    done
}

# Run automatic detection and show results for review
wizard_auto_detect() {
    log_header "Detecting system..."
    echo ""

    # 1. Detect platform
    platform_auto_detect
    if [ -z "$B4_PLATFORM" ]; then
        log_err "Could not detect platform"
        log_info "Try manual mode or set B4_PLATFORM environment variable"
        exit 1
    fi

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
    platform_call info
    [ -n "$_user_bin_dir" ] && B4_BIN_DIR="$_user_bin_dir"
    [ -n "$_user_data_dir" ] && B4_DATA_DIR="$_user_data_dir"
    [ -n "$_user_data_dir" ] && B4_CONFIG_FILE="${_user_data_dir}/b4.json"

    # 3. Detect architecture
    B4_ARCH=$(detect_architecture)

    # 4. Detect package manager
    detect_pkg_manager

    # 5. Show what was detected
    wizard_show_config

    echo ""
    if ! confirm "Proceed with these settings?"; then
        log_info "Switching to manual mode..."
        WIZARD_MODE="manual"
        wizard_manual_configure
    fi
}

# Manual configuration — ask for every setting
wizard_manual_configure() {
    log_header "Manual configuration"
    echo ""

    # 1. Platform selection
    while true; do
        echo "  Available platforms:"
        idx=1
        for p in $REGISTERED_PLATFORMS; do
            pname=$(platform_dispatch "$p" name)
            printf "    ${BOLD}%d${NC}) %s\n" "$idx" "$pname"
            idx=$((idx + 1))
        done
        echo ""

        read_input "Select platform [1]: " "1"
        idx=1
        for p in $REGISTERED_PLATFORMS; do
            if [ "$idx" = "$_INPUT" ]; then
                B4_PLATFORM="$p"
                break
            fi
            idx=$((idx + 1))
        done

        if [ -n "$B4_PLATFORM" ]; then
            break
        fi
        log_warn "Invalid selection, please try again"
        echo ""
    done

    # Load platform defaults first
    platform_call info

    while true; do
        read_input "Binary directory [${B4_BIN_DIR}]: " "$B4_BIN_DIR"
        if is_abs_path "$_INPUT"; then
            B4_BIN_DIR="$_INPUT"
            break
        fi
        log_warn "Binary directory must be an absolute path (got: ${_INPUT:-empty})"
    done

    while true; do
        read_input "Data directory [${B4_DATA_DIR}]: " "$B4_DATA_DIR"
        if is_abs_path "$_INPUT"; then
            B4_DATA_DIR="$_INPUT"
            B4_CONFIG_FILE="${B4_DATA_DIR}/b4.json"
            break
        fi
        log_warn "Data directory must be an absolute path (got: ${_INPUT:-empty})"
    done

    # 4. Service type
    echo ""
    echo "  Service types: systemd, openrc, procd, sysv, entware, none"
    read_input "Service type [${B4_SERVICE_TYPE}]: " "$B4_SERVICE_TYPE"
    B4_SERVICE_TYPE="$_INPUT"

    # 5. Architecture
    auto_arch=$(detect_architecture)
    B4_SUPPORTED_ARCHS="amd64 arm64 armv7 armv6 armv5 386 mips mipsle mips_softfloat mipsle_softfloat mips64 mips64le loong64 ppc64 ppc64le riscv64 s390x"

    # Find the index of the detected architecture for default
    _arch_default=1
    _arch_idx=1
    for a in $B4_SUPPORTED_ARCHS; do
        if [ "$a" = "$auto_arch" ]; then
            _arch_default=$_arch_idx
            break
        fi
        _arch_idx=$((_arch_idx + 1))
    done

    while true; do
        echo "  Available architectures:"
        _arch_idx=1
        for a in $B4_SUPPORTED_ARCHS; do
            if [ "$a" = "$auto_arch" ]; then
                printf "    ${BOLD}%2d${NC}) %s ${DIM}(detected)${NC}\n" "$_arch_idx" "$a"
            else
                printf "    ${BOLD}%2d${NC}) %s\n" "$_arch_idx" "$a"
            fi
            _arch_idx=$((_arch_idx + 1))
        done
        echo ""

        read_input "Select architecture [${_arch_default}]: " "$_arch_default"
        _arch_idx=1
        B4_ARCH=""
        for a in $B4_SUPPORTED_ARCHS; do
            if [ "$_arch_idx" = "$_INPUT" ]; then
                B4_ARCH="$a"
                break
            fi
            _arch_idx=$((_arch_idx + 1))
        done

        if [ -n "$B4_ARCH" ]; then
            break
        fi
        log_warn "Invalid selection, please try again"
        echo ""
    done

    # 6. Package manager
    detect_pkg_manager
    read_input "Package manager [${B4_PKG_MANAGER:-none}]: " "$B4_PKG_MANAGER"
    B4_PKG_MANAGER="$_INPUT"

    echo ""
    wizard_show_config
    echo ""
    if ! confirm "Proceed with these settings?"; then
        log_info "Aborted."
        exit 0
    fi
}

# Display current configuration
wizard_show_config() {
    log_sep
    pname=""
    if [ -n "$B4_PLATFORM" ]; then
        pname=$(platform_dispatch "$B4_PLATFORM" name)
    fi
    log_detail "Platform" "${BOLD}${pname}${NC} (${B4_PLATFORM})"
    log_detail "Architecture" "${B4_ARCH}"
    log_detail "Binary directory" "${B4_BIN_DIR}"
    log_detail "Data directory" "${B4_DATA_DIR}"
    log_detail "Config file" "${B4_CONFIG_FILE}"
    log_detail "Service type" "${B4_SERVICE_TYPE}"
    log_detail "Package manager" "${B4_PKG_MANAGER:-none}"

    # Show enabled features
    if [ -n "$REGISTERED_FEATURES" ]; then
        echo ""
        log_detail "Features" ""
        for f in $REGISTERED_FEATURES; do
            fname=$(feature_dispatch "$f" name)
            fdesc=$(feature_dispatch "$f" description)
            printf "    ${GREEN}+${NC} %s ${DIM}— %s${NC}\n" "$fname" "$fdesc" >&2
        done
    fi
    log_sep
}

# Feature selection wizard (called during install)
wizard_select_features() {
    if [ -z "$REGISTERED_FEATURES" ]; then
        return 0
    fi

    log_header "Optional features"
    echo ""

    for f in $REGISTERED_FEATURES; do
        fname=$(feature_dispatch "$f" name)
        fdesc=$(feature_dispatch "$f" description)
        fdefault=$(feature_dispatch "$f" default_enabled)

        if [ "$fdefault" = "yes" ]; then
            def="y"
        else
            def="n"
        fi

        if confirm "  Enable ${BOLD}${fname}${NC}? ${DIM}(${fdesc})${NC}" "$def"; then
            ENABLED_FEATURES="${ENABLED_FEATURES} ${f}"
        fi
    done
}
