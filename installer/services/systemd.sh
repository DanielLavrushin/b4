#!/bin/sh
# Service type: systemd
# Manages b4 as a systemd unit on standard Linux systems

service_systemd_install() {
    ensure_dir "$B4_SERVICE_DIR" "Service directory" || return 1

    cat >"${B4_SERVICE_DIR}/${B4_SERVICE_NAME}" <<EOF
[Unit]
Description=B4 DPI Bypass Service
After=network.target

[Service]
Type=simple
User=root
Environment=PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ExecStart=${B4_BIN_DIR}/${BINARY_NAME} --config ${B4_CONFIG_FILE}
Restart=on-failure
RestartSec=5
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "${B4_SERVICE_NAME}" 2>/dev/null || true
    log_ok "Systemd service created and enabled: ${B4_SERVICE_NAME}"
}

service_systemd_remove() {
    systemctl stop "${B4_SERVICE_NAME}" 2>/dev/null || true
    systemctl disable "${B4_SERVICE_NAME}" 2>/dev/null || true
    rm -f "${B4_SERVICE_DIR}/${B4_SERVICE_NAME}"
    systemctl daemon-reload
    log_info "Removed systemd service: ${B4_SERVICE_NAME}"
}

service_systemd_start() {
    systemctl reset-failed "${B4_SERVICE_NAME}" 2>/dev/null || true
    if systemctl restart "${B4_SERVICE_NAME}" 2>/dev/null; then
        # Poll for up to 10s — RestartSec=5 means a single check after 2s can false-negative
        _elapsed=0
        while [ "$_elapsed" -lt 10 ]; do
            sleep 1
            _elapsed=$((_elapsed + 1))
            if systemctl is-active --quiet "${B4_SERVICE_NAME}" 2>/dev/null; then
                log_ok "Service started"
                return 0
            fi
            if systemctl is-failed --quiet "${B4_SERVICE_NAME}" 2>/dev/null; then
                break
            fi
        done
        log_err "Service failed to start"
        log_info "Check logs with: journalctl -u ${B4_SERVICE_NAME} --no-pager -n 10"
        journalctl -u "${B4_SERVICE_NAME}" --no-pager -n 5 2>/dev/null | while IFS= read -r _line; do
            log_info "  $_line"
        done
        return 1
    fi
    log_warn "Could not start service"
    return 1
}

service_systemd_stop() {
    systemctl stop "${B4_SERVICE_NAME}" 2>/dev/null || true
}

register_service "systemd"
