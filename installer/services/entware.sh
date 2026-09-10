#!/bin/sh
# Service type: entware
# Manages b4 using Entware's init.d system (rc.func or standalone)
# Used by Keenetic (NDMS) and Asus Merlin (Asuswrt-Merlin)

service_entware_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    # Remove stale service file
    rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" 2>/dev/null || true

    if [ -f "${B4_SERVICE_DIR}/rc.func" ]; then
        _service_entware_install_rcfunc || return 1
    else
        _service_write_standalone_init "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" /opt/var/run/b4.pid || return 1
    fi

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" || return 1
    log_ok "Init script created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    log_info "  ${B4_SERVICE_DIR}/${B4_SERVICE_NAME} start"
    log_info "  ${B4_SERVICE_DIR}/${B4_SERVICE_NAME} stop"
}

_service_entware_install_rcfunc() {
    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF || return 1
#!/bin/sh
# B4 DPI Bypass Service - Entware
B4_INIT_GEN=2

ENABLED=yes
PROCS=b4
ARGS="--config=${B4_CONFIG_FILE}"
PREARGS=""
which nohup >/dev/null 2>&1 && PREARGS="nohup"
DESC="\$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in $B4_KERNEL_MODULES; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

[ "\$1" = "start" ] || [ "\$1" = "restart" ] && kernel_mod_load

. /opt/etc/init.d/rc.func
EOF
}

service_entware_remove() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed service: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_entware_start() {
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

service_entware_stop() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
    fi
}

register_service "entware"
