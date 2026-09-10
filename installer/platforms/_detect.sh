#!/bin/sh
# Auto-detection: iterate registered platforms and find the best match
#
# Override with: B4_PLATFORM=<id> environment variable

platform_auto_detect() {
    # User override — most reliable
    if [ -n "$B4_PLATFORM" ]; then
        # Verify the platform exists
        for p in $REGISTERED_PLATFORMS; do
            if [ "$p" = "$B4_PLATFORM" ]; then
                log_ok "Using user-specified platform: $B4_PLATFORM"
                return 0
            fi
        done
        log_err "Unknown platform: $B4_PLATFORM"
        log_info "Available: $REGISTERED_PLATFORMS"
        exit 1
    fi

    # Try each registered platform's match function
    # generic_linux is tried last since it's the catch-all
    _fallback=""
    for p in $REGISTERED_PLATFORMS; do
        [ "$p" = "generic_linux" ] && _fallback="generic_linux" && continue
        if platform_dispatch "$p" match 2>/dev/null; then
            B4_PLATFORM="$p"
            pname=$(platform_dispatch "$p" name)
            log_ok "Detected platform: ${pname}"
            return 0
        fi
    done

    # Try generic_linux last (its match() excludes known router firmwares)
    if [ -n "$_fallback" ] && platform_dispatch "generic_linux" match 2>/dev/null; then
        B4_PLATFORM="generic_linux"
        log_ok "Detected platform: Generic Linux"
        return 0
    fi

    # Nothing matched specifically — still use generic_linux as safe default
    if [ -n "$_fallback" ]; then
        B4_PLATFORM="generic_linux"
        log_warn "Could not detect specific platform, defaulting to Generic Linux"
        return 0
    fi

    return 1
}

platform_init() {
    _pi_bin_dir="$B4_BIN_DIR"
    _pi_data_dir="$B4_DATA_DIR"
    platform_auto_detect || return 1
    platform_call info
    [ -n "$_pi_bin_dir" ] && B4_BIN_DIR="$_pi_bin_dir"
    [ -n "$_pi_data_dir" ] && B4_DATA_DIR="$_pi_data_dir"
    [ -n "$_pi_data_dir" ] && B4_CONFIG_FILE="${_pi_data_dir}/b4.json"
    return 0
}
