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

service_stop_b4() {
    if [ -n "$B4_SERVICE_TYPE" ] && [ "$B4_SERVICE_TYPE" != "none" ]; then
        service_call stop 2>/dev/null || true
        wait_for_b4_exit 20 && return 0
        log_info "b4 is still running after the service stop, stopping it directly"
    fi
    stop_b4
}

service_verify_started() {
    _svs_old="$1"
    if _svs_new=$(wait_for_new_b4 "$_svs_old" "${2:-15}"); then
        log_ok "Service started (PID: ${_svs_new})"
        return 0
    fi
    log_err "Service failed to start"
    service_show_crash_log
    return 1
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
        return 0
    fi

    log_info "No entries in ${_errlog}."
    if [ "$B4_SERVICE_TYPE" = "systemd" ]; then
        log_info "Check: journalctl -u ${B4_SERVICE_NAME:-b4} --no-pager -n 30"
    fi
    log_info "Or run it in the foreground: ${B4_BIN_DIR}/${BINARY_NAME} --config ${B4_CONFIG_FILE}"
}

_service_write_standalone_init() {
    _swsi_path="$1"
    _swsi_pidfile="$2"
    cat >"$_swsi_path" <<EOF || return 1
#!/bin/sh
B4_INIT_GEN=2
PROG="${B4_BIN_DIR}/${BINARY_NAME}"
CONFIG="${B4_CONFIG_FILE}"
PIDFILE="${_swsi_pidfile}"
export PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
mkdir -p "\$(dirname "\$PIDFILE")" 2>/dev/null || PIDFILE="/var/run/b4.pid"

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in $B4_KERNEL_MODULES; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

b4_is_b4() {
    case "\$1" in
    '' | *[!0-9]*) return 1 ;;
    esac
    kill -0 "\$1" 2>/dev/null || return 1
    [ -d /proc/self ] || return 0
    grep -q '^State:[[:space:]]*Z' "/proc/\$1/status" 2>/dev/null && return 1
    case "\$(tr '\0' '\n' <"/proc/\$1/cmdline" 2>/dev/null | head -1)" in
    ${BINARY_NAME} | */${BINARY_NAME}) return 0 ;;
    esac
    return 1
}

b4_pids() {
    _out=""
    for _q in \$(cat "\$PIDFILE" 2>/dev/null) \$(pidof ${BINARY_NAME} 2>/dev/null) \$(pgrep -x ${BINARY_NAME} 2>/dev/null); do
        case " \$_out " in
        *" \$_q "*) continue ;;
        esac
        b4_is_b4 "\$_q" && _out="\$_out \$_q"
    done
    [ -n "\$_out" ] || return 1
    echo \$_out
}

b4_running() {
    b4_pids >/dev/null 2>&1
}

start() {
    echo "Starting b4..."
    if b4_running; then
        echo "Already running (PID: \$(b4_pids))"
        return 1
    fi
    kernel_mod_load
    _started=""
    if which nohup >/dev/null 2>&1; then
        nohup \$PROG --config \$CONFIG >/dev/null 2>&1 &
        _started=\$!
    elif which setsid >/dev/null 2>&1; then
        setsid \$PROG --config \$CONFIG >/dev/null 2>&1 &
        _started=\$!
    else
        (\$PROG --config \$CONFIG >/dev/null 2>&1 &)
    fi
    [ -n "\$_started" ] && echo "\$_started" >"\$PIDFILE"
    sleep 2
    if b4_running; then
        echo "b4 started (PID: \$(b4_pids))"
        return 0
    fi
    rm -f "\$PIDFILE"
    echo "b4 failed to start, check /var/log/b4/errors.log"
    return 1
}

stop() {
    echo "Stopping b4..."
    _pids=\$(b4_pids) || {
        rm -f "\$PIDFILE"
        echo "b4 is not running"
        return 0
    }
    for _q in \$_pids; do
        kill "\$_q" 2>/dev/null
    done
    _i=0
    while [ "\$_i" -lt 20 ]; do
        sleep 1
        b4_running || {
            rm -f "\$PIDFILE"
            echo "b4 stopped"
            return 0
        }
        _i=\$((_i + 1))
    done
    _pids=\$(b4_pids) || {
        rm -f "\$PIDFILE"
        echo "b4 stopped"
        return 0
    }
    echo "b4 (PID: \$_pids) did not exit, sending SIGKILL"
    for _q in \$_pids; do
        kill -9 "\$_q" 2>/dev/null
    done
    sleep 1
    if b4_running; then
        echo "b4 is still running"
        return 1
    fi
    rm -f "\$PIDFILE"
    echo "b4 stopped"
}

case "\$1" in
    start) start ;;
    stop) stop ;;
    restart) stop && start ;;
    status)
        if b4_running; then
            echo "b4 is running (PID: \$(b4_pids))"
        else
            echo "b4 is not running"
            exit 3
        fi
        ;;
    *)
        echo "Usage: \$0 {start|stop|restart|status}"
        exit 1
        ;;
esac
EOF
}
