#!/bin/sh
# Create systemd service file (for systems with systemd)
create_systemd_service() {
    # Only create if systemd is actually available and functioning
    if ! [ -d "/etc/systemd/system" ] || ! command_exists systemctl; then
        return
    fi

    # Check if systemd is actually running (not just installed)
    if ! systemctl list-units >/dev/null 2>&1; then
        return
    fi

    cat >"/etc/systemd/system/b4.service" <<EOF
[Unit]
Description=B4 DPI Bypass Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=${INSTALL_DIR}/${BINARY_NAME} --config ${CONFIG_FILE}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    print_success "Systemd service created. You can manage it with:"
    print_info "  systemctl start b4"
    print_info "  systemctl stop b4"
    print_info "  systemctl enable b4  # To start on boot"

    SYSTEMCTL_CREATED="1"
}

# Common init script body (shared between OpenWRT and standard)
get_init_script_body() {
    cat <<'BODY'
PROG=PROG_PLACEHOLDER
CONFIG_FILE=CONFIG_PLACEHOLDER
PIDFILE=/var/run/b4.pid

kernel_mod_load() {
    KERNEL=$(uname -r)
    connbytes_mod_path=$(find /lib/modules/$KERNEL -name "xt_connbytes.ko*" 2>/dev/null | head -1)
    [ -n "$connbytes_mod_path" ] && insmod "$connbytes_mod_path" >/dev/null 2>&1
    nfqueue_mod_path=$(find /lib/modules/$KERNEL -name "xt_NFQUEUE.ko*" 2>/dev/null | head -1)
    [ -n "$nfqueue_mod_path" ] && insmod "$nfqueue_mod_path" >/dev/null 2>&1
    multiport_mod_path=$(find /lib/modules/$KERNEL -name "xt_multiport.ko*" 2>/dev/null | head -1)
    [ -n "$multiport_mod_path" ] && insmod "$multiport_mod_path" >/dev/null 2>&1
    modprobe xt_connbytes >/dev/null 2>&1 || true
    modprobe xt_NFQUEUE >/dev/null 2>&1 || true
    modprobe xt_multiport >/dev/null 2>&1 || true
}

start() {
    echo "Starting b4..."
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "b4 is already running"
        return 1
    fi
    kernel_mod_load
    nohup $PROG --config $CONFIG_FILE > /var/log/b4.log 2>&1 &
    echo $! > "$PIDFILE"
    sleep 1
    if kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "b4 started (PID: $(cat "$PIDFILE"))"
    else
        echo "Warning: b4 may have failed to start, check /var/log/b4.log"
        rm -f "$PIDFILE"
        return 1
    fi
}

stop() {
    echo "Stopping b4..."
    if [ -f "$PIDFILE" ]; then
        kill $(cat "$PIDFILE") 2>/dev/null
        rm -f "$PIDFILE"
        echo "b4 stopped"
    else
        killall b4 2>/dev/null || true
        echo "b4 stopped"
    fi
}

status() {
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "b4 is running (PID: $(cat "$PIDFILE"))"
        return 0
    elif pgrep -x b4 >/dev/null 2>&1; then
        echo "b4 is running (no pidfile)"
        return 0
    else
        echo "b4 is not running"
        return 1
    fi
}
BODY
}

# Create OpenWRT/Entware init script
create_sysv_service() {
    INIT_DIR=""

    if [ -d "/opt/etc/init.d" ] && [ -w "/opt/etc/init.d" ]; then
        INIT_DIR="/opt/etc/init.d"
        print_info "Detected Entware/MerlinWRT system"
    elif [ -d "/etc/init.d" ] && [ -w "/etc/init.d" ]; then
        INIT_DIR="/etc/init.d"
    elif [ -d "/opt/etc" ]; then
        mkdir -p /opt/etc/init.d 2>/dev/null && INIT_DIR="/opt/etc/init.d"
    fi

    if [ -z "$INIT_DIR" ]; then
        print_warning "Could not create init script - no writable init directory found"
        return
    fi

    print_info "Creating init script in $INIT_DIR..."

    if [ "$INIT_DIR" = "/etc/init.d" ]; then
        INIT_SCRIPT_NAME="b4"
    else
        INIT_SCRIPT_NAME="S99b4"
    fi

    INIT_FULL_PATH="${INIT_DIR}/${INIT_SCRIPT_NAME}"
    rm -f "${INIT_DIR}/S99b4" 2>/dev/null || true

    if [ -f "${INIT_DIR}/rc.func" ]; then
        # Merlin/Entware rc.func - completely different mechanism
        print_info "rc.func found in $INIT_DIR, using it for init script"
        cat >"${INIT_FULL_PATH}" <<'EOF'
#!/bin/sh
# B4 DPI Bypass Service Init Script

ENABLED=yes
PROCS=b4
ARGS="--config=CONFIG_PLACEHOLDER"
PREARGS="nohup"
DESC="$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

kernel_mod_load() {
    KERNEL=$(uname -r)
    connbytes_mod_path=$(find /lib/modules/$KERNEL -name "xt_connbytes.ko*" 2>/dev/null | head -1)
    [ -n "$connbytes_mod_path" ] && insmod "$connbytes_mod_path" >/dev/null 2>&1
    nfqueue_mod_path=$(find /lib/modules/$KERNEL -name "xt_NFQUEUE.ko*" 2>/dev/null | head -1)
    [ -n "$nfqueue_mod_path" ] && insmod "$nfqueue_mod_path" >/dev/null 2>&1
    multiport_mod_path=$(find /lib/modules/$KERNEL -name "xt_multiport.ko*" 2>/dev/null | head -1)
    [ -n "$multiport_mod_path" ] && insmod "$multiport_mod_path" >/dev/null 2>&1
    modprobe xt_connbytes >/dev/null 2>&1 || true
    modprobe xt_NFQUEUE >/dev/null 2>&1 || true
    modprobe xt_multiport >/dev/null 2>&1 || true
}

[ "$1" = "start" ] || [ "$1" = "restart" ] && kernel_mod_load

. /opt/etc/init.d/rc.func
EOF

    elif [ -f "/etc/openwrt_release" ] && [ -f "/etc/rc.common" ]; then
        # OpenWRT - rc.common handles command dispatch
        print_info "Creating OpenWRT init script"
        {
            echo '#!/bin/sh /etc/rc.common'
            echo '# B4 DPI Bypass Service - OpenWRT'
            echo ''
            echo 'START=99'
            echo 'STOP=10'
            echo ''
            get_init_script_body
        } >"${INIT_FULL_PATH}"

    else
        # Standard SysV init - needs case block
        print_info "Creating standard init.d script"
        {
            echo '#!/bin/sh'
            echo '# B4 DPI Bypass Service Init Script'
            echo ''
            get_init_script_body
            cat <<'EOF'

case "$1" in
    start)   start ;;
    stop)    stop ;;
    restart) stop; sleep 1; start ;;
    status)  status ;;
    *)       echo "Usage: $0 {start|stop|restart|status}"; exit 1 ;;
esac
EOF
        } >"${INIT_FULL_PATH}"
    fi

    sed "s|PROG_PLACEHOLDER|${INSTALL_DIR}/${BINARY_NAME}|g; s|CONFIG_PLACEHOLDER|${CONFIG_FILE}|g" \
        "${INIT_FULL_PATH}" >"${INIT_FULL_PATH}.tmp"
    mv "${INIT_FULL_PATH}.tmp" "${INIT_FULL_PATH}"
    chmod +x "${INIT_FULL_PATH}"

    if [ -f "/etc/openwrt_release" ] && [ -f "/etc/rc.common" ]; then
        "${INIT_FULL_PATH}" enable 2>/dev/null && print_success "Service enabled for auto-start on boot"
    fi

    print_success "Init script created at ${INIT_FULL_PATH}"
    print_info "  ${INIT_FULL_PATH} {start|stop|restart|status}"

    if [ -f "/etc/openwrt_release" ]; then
        print_info "  ${INIT_FULL_PATH} enable   # Start on boot"
        print_info "  ${INIT_FULL_PATH} disable  # Don't start on boot"
    fi
}
