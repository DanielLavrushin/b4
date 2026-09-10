#!/bin/sh
# Service type: sysv
# Manages b4 using a traditional SysV init.d script

service_sysv_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    _service_write_standalone_init "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" /var/run/b4.pid || {
        log_err "Cannot write init script: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        return 1
    }
    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" || return 1

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
