#!/bin/sh
# Service type: sysv
# Manages b4 using a traditional SysV init.d script

service_sysv_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/bin/sh
# B4 DPI Bypass Service
PROG="${B4_BIN_DIR}/${BINARY_NAME}"
CONFIG="${B4_CONFIG_FILE}"
PIDFILE="/var/run/b4.pid"
export PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in $B4_KERNEL_MODULES; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

b4_pidof() {
    if [ -f "\$PIDFILE" ]; then
        _p=\$(cat "\$PIDFILE" 2>/dev/null)
        if [ -n "\$_p" ] && kill -0 "\$_p" 2>/dev/null; then
            echo "\$_p"
            return 0
        fi
    fi
    if command -v pidof >/dev/null 2>&1; then
        _p=\$(pidof ${BINARY_NAME} 2>/dev/null | tr ' ' '\n' | head -1)
        [ -n "\$_p" ] && echo "\$_p" && return 0
    fi
    if command -v pgrep >/dev/null 2>&1; then
        _p=\$(pgrep -x ${BINARY_NAME} 2>/dev/null | head -1)
        [ -n "\$_p" ] && echo "\$_p" && return 0
    fi
    return 1
}

b4_running() {
    b4_pidof >/dev/null 2>&1
}

start() {
    echo "Starting b4..."
    if b4_running; then
        echo "Already running (PID: \$(b4_pidof))"
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
        echo "b4 started (PID: \$(b4_pidof))"
    else
        echo "b4 failed to start, check /var/log/b4/errors.log"
        return 1
    fi
}

stop() {
    echo "Stopping b4..."
    _p=\$(b4_pidof) || { echo "b4 is not running"; return 0; }
    kill "\$_p" 2>/dev/null
    _i=0
    while [ "\$_i" -lt 20 ]; do
        sleep 1
        b4_running || { echo "b4 stopped"; return 0; }
        _i=\$((_i + 1))
    done
    _p=\$(b4_pidof) || { echo "b4 stopped"; return 0; }
    echo "b4 (PID: \$_p) did not exit, sending SIGKILL"
    kill -9 "\$_p" 2>/dev/null
    sleep 1
    if b4_running; then
        echo "b4 is still running"
        return 1
    fi
    echo "b4 stopped"
}

case "\$1" in
    start)   start ;;
    stop)    stop ;;
    restart) stop && start ;;
    status)  if b4_running; then echo "b4 is running (PID: \$(b4_pidof))"; else echo "b4 is not running"; exit 3; fi ;;
    *)       echo "Usage: \$0 {start|stop|restart|status}"; exit 1 ;;
esac
EOF

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"

    if command -v update-rc.d >/dev/null 2>&1; then
        update-rc.d "${B4_SERVICE_NAME}" defaults 2>/dev/null || true
    elif command -v chkconfig >/dev/null 2>&1; then
        chkconfig --add "${B4_SERVICE_NAME}" 2>/dev/null || true
        chkconfig "${B4_SERVICE_NAME}" on 2>/dev/null || true
    fi

    log_ok "Init script created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
}

service_sysv_remove() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
        if command -v update-rc.d >/dev/null 2>&1; then
            update-rc.d -f "${B4_SERVICE_NAME}" remove 2>/dev/null || true
        elif command -v chkconfig >/dev/null 2>&1; then
            chkconfig --del "${B4_SERVICE_NAME}" 2>/dev/null || true
        fi
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed init script: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_sysv_start() {
    _init="${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    if [ ! -f "$_init" ]; then
        log_warn "Could not start service"
        return 1
    fi
    _old=$(b4_pid) || _old=""
    "$_init" restart 2>/dev/null || {
        log_warn "Could not start service"
        return 1
    }
    service_verify_started "$_old"
}

service_sysv_stop() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
    fi
}

register_service "sysv"
