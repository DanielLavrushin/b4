#!/bin/sh
# Service registration and dispatch system
#
# Each service file must define these functions (prefixed with service_<type>_):
#   install   — Write the service/init script to disk
#   remove    — Stop and delete the service/init script
#   start     — Start the b4 service
#   stop      — Stop the b4 service
#
# Then register with: register_service "<type>"
#
# Required globals when service functions are called:
#   B4_SERVICE_TYPE, B4_SERVICE_DIR, B4_SERVICE_NAME
#   B4_BIN_DIR, B4_DATA_DIR, B4_CONFIG_FILE, BINARY_NAME

REGISTERED_SERVICES=""

register_service() {
    id="$1"
    REGISTERED_SERVICES="${REGISTERED_SERVICES} ${id}"
}

# Dispatch to the active service type
# Usage: service_call <function> [args...]
service_call() {
    func="$1"
    shift
    service_dispatch "$B4_SERVICE_TYPE" "$func" "$@"
}

# Dispatch to a specific service type
# Usage: service_dispatch <type> <function> [args...]
service_dispatch() {
    sid="$1"
    func="$2"
    shift 2
    fn="service_${sid}_${func}"
    if type "$fn" >/dev/null 2>&1; then
        "$fn" "$@"
    else
        log_warn "Service type '${sid}' does not implement '${func}'"
        return 1
    fi
}

service_show_crash_log() {
    _logdir=""
    if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
        if [ "$(jq -r '(.system.logging // {}) | has("directory")' "$B4_CONFIG_FILE" 2>/dev/null)" = "true" ]; then
            # New config: an explicit empty directory means file logging is off
            _logdir=$(jq -r '.system.logging.directory // ""' "$B4_CONFIG_FILE" 2>/dev/null)
            [ -z "$_logdir" ] && return 0
        else
            # Older config without 'directory': fall back to the legacy error_file
            _ef=$(jq -r '.system.logging.error_file // empty' "$B4_CONFIG_FILE" 2>/dev/null)
            [ -n "$_ef" ] && _logdir=$(dirname "$_ef")
        fi
    fi
    [ -z "$_logdir" ] && _logdir="/var/log/b4"
    _errlog="${_logdir}/errors.log"
    if [ -s "$_errlog" ]; then
        log_info "Last log entries from $_errlog:"
        tail -5 "$_errlog" 2>/dev/null | while IFS= read -r _line; do
            log_info "  $_line"
        done
    fi
}
