#!/bin/sh
# Service type: openrc
# Manages b4 using OpenRC (Alpine Linux and other OpenRC-based distros)

service_openrc_init_gen() {
    echo 3
}

service_openrc_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF || return 1
#!/sbin/openrc-run

name="b4"
description="B4 DPI Bypass Service"
B4_INIT_GEN=$(service_openrc_init_gen)

command="${B4_BIN_DIR}/${BINARY_NAME}"
command_args="--config ${B4_CONFIG_FILE}"
command_background=true
pidfile="/run/b4.pid"
retry="TERM/20/KILL/5"

output_log="/dev/null"
error_log="/dev/null"

export PATH="\${PATH}:/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

depend() {
    need net
}

start_pre() {
    # Load kernel modules
    for mod in $B4_KERNEL_MODULES; do
        modprobe "\$mod" >/dev/null 2>&1 || true
    done
}
EOF

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" || return 1
    rc-update add "${B4_SERVICE_NAME}" default 2>/dev/null || true
    log_ok "OpenRC service created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    log_info "  rc-service ${B4_SERVICE_NAME} start"
    log_info "  rc-service ${B4_SERVICE_NAME} stop"
}

service_openrc_remove() {
    rc-update del "${B4_SERVICE_NAME}" default 2>/dev/null || true
    rc-service "${B4_SERVICE_NAME}" stop 2>/dev/null || true
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed OpenRC service: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_openrc_start() {
    _old=$(b4_pid) || _old=""
    rc-service "${B4_SERVICE_NAME}" restart 2>/dev/null || {
        log_warn "Could not start service"
        return 1
    }
    service_verify_started "$_old"
}

service_openrc_stop() {
    rc-service "${B4_SERVICE_NAME}" stop 2>/dev/null || true
    is_b4_running || return 0
    log_info "OpenRC does not own the running b4, stopping it directly"
    stop_b4
}

register_service "openrc"
