#!/bin/sh
# Start xray by hand with only the TUN device it needs, and none of XRAYUI's
# policy routing, so a b4 test can tell "b4 routed this" from "xray routed this".
#
#   xray-manual.sh start          xray0 up, xray running, no ip rules added
#   xray-manual.sh start --route  the same plus the LAN source rule XRAYUI would add
#   xray-manual.sh stop           xray down, xray0 gone, every rule this script added removed
#   xray-manual.sh status         what is running and what is routed where
#   xray-manual.sh purge          remove XRAYUI's leftover rules and table too
#
# It never touches b4.

set -e

XRAY_BIN="${XRAY_BIN:-/opt/sbin/xray}"
XRAY_CONF="${XRAY_CONF:-/opt/etc/xray/tun.json}"
TUN_DEV="${TUN_DEV:-xray0}"
TUN_ADDR="${TUN_ADDR:-192.168.10.1/24}"
LAN_CIDR="${LAN_CIDR:-192.168.1.0/24}"
ROUTE_TABLE="${ROUTE_TABLE:-250}"
RULE_PRIO="${RULE_PRIO:-51}"
LOCAL_PRIO="${LOCAL_PRIO:-49}"
LAN_PROBE="${LAN_PROBE:-192.168.1.100}"
LAN_IFACE="${LAN_IFACE:-br0}"
PIDFILE="${PIDFILE:-/tmp/xray-manual.pid}"
LOGFILE="${LOGFILE:-/tmp/xray-manual.log}"

say() { echo "[xray-manual] $*"; }

running() {
    [ -f "$PIDFILE" ] || return 1
    kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}

make_tun() {
    if ip link show "$TUN_DEV" >/dev/null 2>&1; then
        say "$TUN_DEV already exists, leaving it alone"
    elif ip tuntap add dev "$TUN_DEV" mode tun 2>/dev/null; then
        say "created $TUN_DEV"
    else
        say "could not create $TUN_DEV here; assuming xray creates it itself"
        return 0
    fi
    ip addr add "$TUN_ADDR" dev "$TUN_DEV" 2>/dev/null || true
    ip link set "$TUN_DEV" up 2>/dev/null || true
    ip link set "$TUN_DEV" mtu 1500 2>/dev/null || true
}

wait_for_tun() {
    i=0
    while [ "$i" -lt 10 ]; do
        ip link show "$TUN_DEV" >/dev/null 2>&1 && return 0
        i=$((i + 1))
        sleep 1
    done
    return 1
}

drop_tun() {
    if ip link show "$TUN_DEV" >/dev/null 2>&1; then
        ip link del "$TUN_DEV" 2>/dev/null || true
        say "removed $TUN_DEV"
    fi
}

add_routing() {
    ip route replace default dev "$TUN_DEV" table "$ROUTE_TABLE"
    ip rule del from "$LAN_CIDR" to "$LAN_CIDR" lookup main priority "$LOCAL_PRIO" 2>/dev/null || true
    ip rule add from "$LAN_CIDR" to "$LAN_CIDR" lookup main priority "$LOCAL_PRIO"
    ip rule del from "$LAN_CIDR" lookup "$ROUTE_TABLE" priority "$RULE_PRIO" 2>/dev/null || true
    ip rule add from "$LAN_CIDR" lookup "$ROUTE_TABLE" priority "$RULE_PRIO"
    say "LAN $LAN_CIDR source-routed into $TUN_DEV (table $ROUTE_TABLE, ip rule priority $RULE_PRIO)"
}

drop_routing() {
    while ip rule del from "$LAN_CIDR" lookup "$ROUTE_TABLE" priority "$RULE_PRIO" 2>/dev/null; do :; done
    while ip rule del from "$LAN_CIDR" to "$LAN_CIDR" lookup main priority "$LOCAL_PRIO" 2>/dev/null; do :; done
    ip route flush table "$ROUTE_TABLE" 2>/dev/null || true
    say "removed the LAN source routing this script added"
}

foreign_routes() {
    tbl=$(ip route show table all 2>/dev/null | awk -v d="$TUN_DEV" '$0 ~ ("dev " d) && $1 == "default" {
        for (i = 1; i < NF; i++) if ($i == "table") print $(i + 1)
    }' | sort -u)
    [ -n "$tbl" ] || return 1
    hit=""
    for t in $tbl; do
        r=$(ip rule show | grep "lookup $t\\b" || true)
        [ -n "$r" ] && hit="$hit$r
"
    done
    [ -n "$hit" ] || return 1
    say "WARNING: something already routes into $TUN_DEV, so this is not a clean test:"
    echo "$hit" | sed 's/^/    /'
    say "run '$0 purge' first if you want xray to receive only what b4 sends it"
    return 0
}

RPSAVE="${RPSAVE:-/tmp/xray-manual.rp}"

loosen_rp_filter() {
    saved=""
    for i in all "$TUN_DEV"; do
        f="/proc/sys/net/ipv4/conf/$i/rp_filter"
        [ -f "$f" ] || continue
        cur=$(cat "$f" 2>/dev/null) || continue
        saved="$saved$i=$cur
"
        [ "$cur" = "2" ] || echo 2 > "$f" 2>/dev/null || true
    done
    printf '%s' "$saved" > "$RPSAVE" 2>/dev/null || true
    say "rp_filter set to 2 (loose) on all and $TUN_DEV; a reply arriving on the tunnel is dropped otherwise"
}

restore_rp_filter() {
    [ -f "$RPSAVE" ] || return 0
    while IFS='=' read -r i v; do
        [ -n "$i" ] || continue
        f="/proc/sys/net/ipv4/conf/$i/rp_filter"
        [ -f "$f" ] && echo "$v" > "$f" 2>/dev/null || true
    done < "$RPSAVE"
    rm -f "$RPSAVE"
    say "restored rp_filter"
}

forwarding_on() {
    echo 1 > /proc/sys/net/ipv4/ip_forward
    iptables -C FORWARD -i "$TUN_DEV" -j ACCEPT 2>/dev/null || iptables -I FORWARD -i "$TUN_DEV" -j ACCEPT
    iptables -C FORWARD -o "$TUN_DEV" -j ACCEPT 2>/dev/null || iptables -I FORWARD -o "$TUN_DEV" -j ACCEPT
}

forwarding_off() {
    while iptables -D FORWARD -i "$TUN_DEV" -j ACCEPT 2>/dev/null; do :; done
    while iptables -D FORWARD -o "$TUN_DEV" -j ACCEPT 2>/dev/null; do :; done
}

start() {
    [ -x "$XRAY_BIN" ] || { say "no xray binary at $XRAY_BIN"; exit 1; }
    [ -f "$XRAY_CONF" ] || { say "no config at $XRAY_CONF"; exit 1; }
    if running; then say "already running as pid $(cat "$PIDFILE")"; exit 0; fi

    make_tun
    forwarding_on
    loosen_rp_filter
    foreign_routes || true
    if [ "$1" = "--route" ]; then
        add_routing
    fi

    nohup "$XRAY_BIN" -c "$XRAY_CONF" >"$LOGFILE" 2>&1 &
    echo $! > "$PIDFILE"
    sleep 2

    if running; then
        say "xray running as pid $(cat "$PIDFILE"), log $LOGFILE"
        if wait_for_tun; then
            ip addr add "$TUN_ADDR" dev "$TUN_DEV" 2>/dev/null || true
            ip link set "$TUN_DEV" up 2>/dev/null || true
        else
            say "warning: $TUN_DEV never appeared, check $LOGFILE"
        fi
        if [ "$1" != "--route" ]; then
            say "no policy routing added: nothing reaches $TUN_DEV until something routes it there"
        fi
    else
        say "xray exited immediately, last lines:"
        tail -20 "$LOGFILE"
        drop_tun
        exit 1
    fi
}

stop() {
    if running; then
        kill "$(cat "$PIDFILE")" 2>/dev/null || true
        sleep 1
        kill -9 "$(cat "$PIDFILE")" 2>/dev/null || true
        say "stopped xray"
    else
        say "xray was not running under this script"
    fi
    rm -f "$PIDFILE"
    drop_routing
    forwarding_off
    restore_rp_filter
    drop_tun
}

status() {
    if running; then
        say "xray running as pid $(cat "$PIDFILE")"
    else
        say "xray not running under this script"
    fi
    echo
    echo "--- $TUN_DEV ---"
    ip -4 addr show "$TUN_DEV" 2>/dev/null || echo "(absent)"
    echo
    echo "--- ip rule ---"
    ip rule show
    echo
    echo "--- table $ROUTE_TABLE ---"
    ip route show table "$ROUTE_TABLE" 2>/dev/null || echo "(empty)"
    echo
    echo "--- rp_filter (2 = loose, 1 = strict drops the reply) ---"
    for i in all "$TUN_DEV"; do
        printf "    %-8s " "$i"
        cat "/proc/sys/net/ipv4/conf/$i/rp_filter" 2>/dev/null || echo "(absent)"
    done
    echo
    echo "--- anything else routing into $TUN_DEV ---"
    foreign_routes || echo "(nothing)"
    echo
    echo "--- where a LAN client's packet goes ---"
    ip route get 198.51.100.1 from "$LAN_PROBE" iif "$LAN_IFACE" 2>/dev/null || true
    echo
    echo "--- where the router's own packet goes ---"
    ip route get 198.51.100.1 2>/dev/null || true
}

purge() {
    stop
    say "removing routing left behind by XRAYUI"
    for prio in 19 20 49 51; do
        while ip rule del priority "$prio" 2>/dev/null; do :; done
    done
    for tbl in 77 250 8437; do
        ip route flush table "$tbl" 2>/dev/null || true
    done
    say "done, current rules:"
    ip rule show
}

case "$1" in
    start)  start "$2" ;;
    stop)   stop ;;
    status) status ;;
    purge)  purge ;;
    *)      echo "usage: $0 {start [--route]|stop|status|purge}"; exit 1 ;;
esac
