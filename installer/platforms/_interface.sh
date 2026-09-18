#!/bin/sh
# Platform registration and dispatch system
#
# Each platform file must define these functions (prefixed with platform_<id>_):
#   name             — Human-readable name
#   match            — Return 0 if this platform is detected, 1 otherwise
#   info             — Set B4_BIN_DIR, B4_DATA_DIR, B4_SERVICE_TYPE, etc.
#   check_deps       — Verify/install kernel modules and dependencies
#   find_storage     — Find writable storage (for routers with limited rootfs)
#
# Then register with: register_platform "<id>"

REGISTERED_PLATFORMS=""

register_platform() {
    id="$1"
    REGISTERED_PLATFORMS="${REGISTERED_PLATFORMS} ${id}"
}

# Dispatch a call to the active platform
# Usage: platform_call <function> [args...]
platform_call() {
    func="$1"
    shift
    platform_dispatch "$B4_PLATFORM" "$func" "$@"
}

platform_call_optional() {
    func="$1"
    shift
    fn="platform_${B4_PLATFORM}_${func}"
    type "$fn" >/dev/null 2>&1 || return 0
    "$fn" "$@"
}

# Dispatch to a specific platform
# Usage: platform_dispatch <platform_id> <function> [args...]
platform_dispatch() {
    pid="$1"
    func="$2"
    shift 2
    # Build function name: platform_<id>_<func>
    fn="platform_${pid}_${func}"
    if type "$fn" >/dev/null 2>&1; then
        "$fn" "$@"
    else
        log_warn "Platform '${pid}' does not implement '${func}'"
        return 1
    fi
}
