#!/bin/sh
# Service type: procd
# Manages b4 using OpenWrt's procd init system

service_procd_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/bin/sh /etc/rc.common
# B4 DPI Bypass Service (procd)

START=99
STOP=10
USE_PROCD=1

PROG="${B4_BIN_DIR}/${BINARY_NAME}"
CONFIG="${B4_CONFIG_FILE}"
export PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

kernel_mod_load() {
    KERNEL=\$(uname -r)
    for mod in $B4_KERNEL_MODULES; do
        modprobe "\$mod" >/dev/null 2>&1 && continue
        mod_path=\$(find /lib/modules/\$KERNEL -name "\${mod}.ko*" 2>/dev/null | head -1)
        [ -n "\$mod_path" ] && insmod "\$mod_path" >/dev/null 2>&1 || true
    done
}

start_service() {
    kernel_mod_load

    procd_open_instance
    procd_set_param command \$PROG --config \$CONFIG
    procd_set_param env PATH="\$PATH"
    procd_set_param respawn \${respawn_threshold:-3600} \${respawn_timeout:-5} \${respawn_retry:-5}
    procd_set_param stdout 0
    procd_set_param stderr 1
    # Must exceed b4's own shutdown budget or procd SIGKILLs it mid-teardown,
    # leaving its ip rules and routing tables behind. No pidfile param here:
    # b4 writes and flocks /var/run/b4.pid itself for its single-instance guard.
    procd_set_param term_timeout 20
    procd_close_instance
}

service_triggers() {
    procd_add_reload_trigger "b4"
}
EOF

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    log_ok "Procd init script created: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"

    # Enable the service to start on boot
    "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" enable 2>/dev/null || true
    log_info "Service enabled for boot"
}

service_procd_remove() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" disable 2>/dev/null || true
        rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
        log_info "Removed procd service: ${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    fi
}

service_procd_start() {
    _init="${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    if [ ! -f "$_init" ]; then
        log_warn "Could not start service"
        return 1
    fi
    # procd serialises restart: it forks the new instance only after the old
    # one has actually exited, so `restart` is safe here. What is not safe is
    # confirming the start by process name.
    _old=$(b4_pid) || _old=""
    "$_init" restart 2>/dev/null || {
        log_warn "Could not start service"
        return 1
    }
    service_verify_started "$_old"
}

service_procd_stop() {
    if [ -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" ]; then
        "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" stop 2>/dev/null || true
    fi
}

register_service "procd"
