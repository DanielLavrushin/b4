#!/bin/sh
# Service type: openrc
# Manages b4 using OpenRC (Alpine Linux and other OpenRC-based distros)

service_openrc_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
#!/sbin/openrc-run

name="b4"
description="B4 DPI Bypass Service"

command="${B4_BIN_DIR}/${BINARY_NAME}"
command_args="--config ${B4_CONFIG_FILE}"
command_background=true
pidfile="/run/b4.pid"

output_log="/dev/null"
error_log="/dev/null"

depend() {
    need net
}

start_pre() {
    # Load kernel modules
    for mod in nfnetlink nf_conntrack nf_conntrack_netlink xt_connbytes xt_NFQUEUE nfnetlink_queue xt_multiport nf_tables nft_queue nft_ct nf_nat nft_masq nft_tproxy nft_socket; do
        modprobe "\$mod" >/dev/null 2>&1 || true
    done
}
EOF

    chmod +x "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
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
    rc-service "${B4_SERVICE_NAME}" start 2>/dev/null || { log_warn "Could not start service"; return 1; }
    sleep 2
    if pidof b4 >/dev/null 2>&1 || pgrep -x b4 >/dev/null 2>&1; then
        log_ok "Service started"
        return 0
    fi
    log_err "Service crashed immediately after start"
    service_show_crash_log
    return 1
}

service_openrc_stop() {
    rc-service "${B4_SERVICE_NAME}" stop 2>/dev/null || true
}

register_service "openrc"
