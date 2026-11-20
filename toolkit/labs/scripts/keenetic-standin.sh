#!/bin/sh
set -eu

STATE="${KN_STATE:-/var/tmp/keenetic-standin}"

LAN_NET=192.168.10.0/24
LAN_ADDR=192.168.10.1
DEFAULT_DEV_ADDR=192.168.10.20
DEFAULT_DEV_MAC=02:00:00:00:10:20
POLICY_DEV_ADDR=192.168.10.30
POLICY_DEV_MAC=02:00:00:00:10:30
WAN1_ADDR=203.0.113.10
WAN1_GW=203.0.113.1
WAN1_LEN=23
WAN1_NET=203.0.112.0/23
WAN2_ADDR=198.51.100.10
WAN2_GW=198.51.100.1
WAN2_LEN=22
WAN2_NET=198.51.100.0/22
NWG_ADDR=10.250.20.2
NWG_PEER=10.250.20.1
SITE_IN_SET=192.0.2.80
SITE_OTHER=192.0.2.81
POLICIES=5
DEVICE_POLICY=0
POLICY_FIRST_MARK=0x0ffffaaa
HOTSPOT=_NDM_HOTSPOT_PREROUTING_MANG
RCI_PORT=79

die() {
	echo "keenetic-standin: $*" >&2
	exit 1
}

usage() {
	cat <<EOF
usage: keenetic-standin.sh <command> [arguments]

  --config=<file> [<b4> [args]]  as the binary of scripts/b4-netns.sh: build the router in
                                 that namespace, then run b4 there with the config and args
  check                          fetch $SITE_IN_SET and $SITE_OTHER from each device and the
                                 router, on ports 80 and 443, and print which uplink answered
  rewrite                        replace the mangle tables with the NDMS chains alone, once
  rewrite <seconds>              do that every so many seconds in the background
  rewrite stop                   stop the background rewrites
  show                           ip rules, their tables, mangle PREROUTING, the hotspot
                                 chain, nat POSTROUTING counters, rp_filter, rewrites done
  rci                            the requests the fake RCI has answered
  in <where> <command...>        run a command in router, default, policy, isp1, isp2 or vpn
  stop                           stop b4 and the stand-in

State and logs: $STATE (KN_STATE moves it).
EOF
}

policy_mark() {
	printf '0x%08x' $((POLICY_FIRST_MARK + $1))
}

in_ns() {
	ns_name="$1"
	shift
	ip netns exec "$ns_name" "$@"
}

standin_pid() {
	[ -f "$STATE/pid" ] || return 1
	sp="$(cat "$STATE/pid")"
	[ -n "$sp" ] || return 1
	kill -0 "$sp" 2>/dev/null || return 1
	grep -q keenetic-standin "/proc/$sp/cmdline" 2>/dev/null || return 1
	echo "$sp"
}

router_pid() {
	standin_pid || die "no stand-in runs: start it as the binary of scripts/b4-netns.sh (toolkit/labs/README.md)"
}

run_in() {
	where="$1"
	shift
	case "$where" in
	router) nsenter -t "$rp" -n -m "$@" ;;
	default | policy | isp1 | isp2 | vpn) nsenter -t "$rp" -n -m ip netns exec "kn-$where" "$@" ;;
	*) die "no namespace named '$where': router, default, policy, isp1, isp2 or vpn" ;;
	esac
}

wait_for() {
	w_what="$1"
	shift
	w_tries=0
	until "$@" >/dev/null 2>&1; do
		w_tries=$((w_tries + 1))
		[ "$w_tries" -lt 15 ] || die "$w_what did not come up; logs in $STATE"
		sleep 1
	done
}

write_helper() {
	cat > "$STATE/standin.py" <<'PY'
import ctypes
import fcntl
import json
import os
import select
import socketserver
import struct
import sys
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

TUNSETIFF = 0x400454CA
IFF_TUN = 0x0001
IFF_NO_PI = 0x1000
CLONE_NEWNET = 0x40000000


class Server(socketserver.ThreadingMixIn, HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def say(line):
    sys.stdout.write(line + "\n")
    sys.stdout.flush()


class Quiet(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, code, body, kind="application/json"):
        raw = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)


def system_name(item):
    try:
        return item["show"]["interface"]["system-name"]["name"]
    except (KeyError, TypeError):
        return ""


def rci(port, path):
    with open(path) as f:
        data = json.load(f)

    class Handler(Quiet):
        def do_GET(self):
            say("GET " + self.path)
            if self.path == "/rci/show/interface":
                self.reply(200, data["interface"])
            elif self.path == "/rci/show/ip/hotspot":
                self.reply(200, data["hotspot"])
            else:
                self.reply(404, {"status": [{"status": "error", "message": "not in the stand-in"}]})

        def do_POST(self):
            raw = self.rfile.read(int(self.headers.get("Content-Length") or 0))
            say("POST " + self.path + " " + raw.decode("utf-8", "replace"))
            if self.path != "/rci/":
                self.reply(404, {"status": [{"status": "error", "message": "not in the stand-in"}]})
                return
            try:
                batch = json.loads(raw.decode("utf-8"))
            except ValueError:
                self.reply(400, {"status": [{"status": "error", "message": "not JSON"}]})
                return
            replies = []
            for item in batch:
                value = data["system"].get(system_name(item), {"status": [{"status": "error"}]})
                replies.append({"show": {"interface": {"system-name": value}}})
            self.reply(200, replies)

    Server(("127.0.0.1", port), Handler).serve_forever()


def site(name, ports):
    class Handler(Quiet):
        def do_GET(self):
            peer = self.client_address[0]
            say(name + " port " + str(self.server.server_address[1]) + " from " + peer)
            self.reply(200, (name + " " + peer + "\n").encode(), "text/plain")

    servers = [Server(("0.0.0.0", int(p)), Handler) for p in ports]
    for s in servers[1:]:
        threading.Thread(target=s.serve_forever, daemon=True).start()
    servers[0].serve_forever()


def attach(name):
    fd = os.open("/dev/net/tun", os.O_RDWR)
    fcntl.ioctl(fd, TUNSETIFF, struct.pack("16sH", name.encode(), IFF_TUN | IFF_NO_PI))
    return fd


def tunpair(near, netns, far):
    a = attach(near)
    ns = os.open(netns, os.O_RDONLY)
    if ctypes.CDLL(None, use_errno=True).setns(ns, CLONE_NEWNET) != 0:
        raise OSError(ctypes.get_errno(), "setns " + netns)
    b = attach(far)
    other = {a: b, b: a}
    say("carrying packets between " + near + " and " + far)
    while True:
        ready, _, _ = select.select([a, b], [], [])
        for fd in ready:
            try:
                os.write(other[fd], os.read(fd, 65536))
            except OSError:
                pass


def main():
    role = sys.argv[1]
    if role == "rci":
        rci(int(sys.argv[2]), sys.argv[3])
    elif role == "site":
        site(sys.argv[2], sys.argv[3:])
    elif role == "tunpair":
        tunpair(sys.argv[2], sys.argv[3], sys.argv[4])
    else:
        sys.exit("unknown role " + role)


main()
PY
}

write_mangle() {
	wm_mark="$(policy_mark "$DEVICE_POLICY")"
	{
		echo "*mangle"
		for wm_chain in PREROUTING INPUT FORWARD OUTPUT POSTROUTING; do
			echo ":$wm_chain ACCEPT [0:0]"
		done
		echo ":$HOTSPOT - [0:0]"
		echo "-A PREROUTING -j $HOTSPOT"
		echo "-A $HOTSPOT -m mac --mac-source $POLICY_DEV_MAC -j MARK --set-xmark $wm_mark/0xffffffff"
		echo "-A $HOTSPOT -m mac --mac-source $POLICY_DEV_MAC -j CONNMARK --save-mark --nfmask 0xffffffff --ctmask 0xffffffff"
		echo "-A $HOTSPOT -m mac --mac-source $POLICY_DEV_MAC -j RETURN"
		echo "COMMIT"
	} > "$STATE/mangle.rules"
}

write_rci() {
	cat > "$STATE/rci.json" <<EOF
{
  "interface": {
    "Bridge0": {"id": "Bridge0", "interface-name": "Home", "description": "Home network"},
    "GigabitEthernet1": {"id": "GigabitEthernet1", "interface-name": "ISP", "description": "Provider"},
    "GigabitEthernet0/Vlan5": {"id": "GigabitEthernet0/Vlan5", "interface-name": "GigabitEthernet0/Vlan5", "description": "LTE router"},
    "Wireguard1": {"id": "Wireguard1", "interface-name": "Wireguard1", "description": "VPN"}
  },
  "system": {
    "Bridge0": "br0",
    "GigabitEthernet1": "eth3",
    "GigabitEthernet0/Vlan5": "eth2.5",
    "Wireguard1": "nwg1"
  },
  "hotspot": {"host": [
    {"mac": "$DEFAULT_DEV_MAC", "ip": "$DEFAULT_DEV_ADDR", "hostname": "laptop", "name": "Default policy laptop"},
    {"mac": "$POLICY_DEV_MAC", "ip": "$POLICY_DEV_ADDR", "hostname": "tv", "name": "Policy TV"}
  ]}
}
EOF
}

start_helper() {
	sh_name="$1"
	sh_ns="$2"
	shift 2
	if [ -n "$sh_ns" ]; then
		ip netns exec "$sh_ns" python3 "$STATE/standin.py" "$@" </dev/null >"$STATE/$sh_name.log" 2>&1 &
	else
		python3 "$STATE/standin.py" "$@" </dev/null >"$STATE/$sh_name.log" 2>&1 &
	fi
	echo "$!" >> "$STATE/helpers"
}

add_sites() {
	in_ns "$1" sysctl -q -w net.ipv4.conf.all.arp_ignore=1
	in_ns "$1" ip addr add "$SITE_IN_SET/32" dev lo
	in_ns "$1" ip addr add "$SITE_OTHER/32" dev lo
	start_helper "site-$2" "$1" site "$2" 80 443
	wait_for "the listener of $2" in_ns "$1" curl -s -m 1 "http://$SITE_IN_SET:80/"
}

add_device() {
	ad_ns="$1"
	ad_port="$2"
	ad_mac="$3"
	ad_addr="$4"
	ip netns add "$ad_ns"
	ip link add "$ad_port" type veth peer name kn-peer
	ip link set kn-peer netns "$ad_ns"
	ip link set "$ad_port" master br0
	ip link set "$ad_port" up
	in_ns "$ad_ns" ip link set lo up
	in_ns "$ad_ns" ip link set kn-peer name eth0
	in_ns "$ad_ns" ip link set eth0 address "$ad_mac"
	in_ns "$ad_ns" ip addr add "$ad_addr/24" dev eth0
	in_ns "$ad_ns" ip link set eth0 up
	in_ns "$ad_ns" ip route add default via "$LAN_ADDR"
}

add_uplink() {
	au_ns="$1"
	au_dev="$2"
	au_addr="$3"
	au_gw="$4"
	au_len="$5"
	ip netns add "$au_ns"
	ip link add "$au_dev" type veth peer name kn-peer
	ip link set kn-peer netns "$au_ns"
	ip addr add "$au_addr/$au_len" dev "$au_dev"
	ip link set "$au_dev" up
	in_ns "$au_ns" ip link set lo up
	in_ns "$au_ns" ip link set kn-peer name eth0
	in_ns "$au_ns" ip addr add "$au_gw/$au_len" dev eth0
	in_ns "$au_ns" ip link set eth0 up
}

build_lan() {
	echo 1 > /proc/sys/net/ipv4/ip_forward
	ip link add br0 type bridge
	ip addr add "$LAN_ADDR/24" dev br0
	ip link set br0 up
	add_device kn-default lan-default "$DEFAULT_DEV_MAC" "$DEFAULT_DEV_ADDR"
	add_device kn-policy lan-policy "$POLICY_DEV_MAC" "$POLICY_DEV_ADDR"
}

build_uplinks() {
	add_uplink kn-isp1 eth3 "$WAN1_ADDR" "$WAN1_GW" "$WAN1_LEN"
	add_sites kn-isp1 isp1
	add_uplink kn-isp2 eth2.5 "$WAN2_ADDR" "$WAN2_GW" "$WAN2_LEN"
	add_sites kn-isp2 isp2
}

build_tunnel() {
	ip netns add kn-vpn
	ip tuntap add dev nwg1 mode tun
	ip addr add "$NWG_ADDR/32" dev nwg1
	ip link set nwg1 up
	in_ns kn-vpn ip link set lo up
	in_ns kn-vpn ip tuntap add dev nwgpeer mode tun
	in_ns kn-vpn ip addr add "$NWG_PEER/32" dev nwgpeer
	in_ns kn-vpn ip link set nwgpeer up
	in_ns kn-vpn ip route add "$NWG_ADDR/32" dev nwgpeer
	add_sites kn-vpn vpn
	start_helper tunnel "" tunpair nwg1 /var/run/netns/kn-vpn nwgpeer
	wait_for "the carrier of nwg1" grep -qx 1 /sys/class/net/nwg1/carrier
}

build_routing() {
	ip route add default via "$WAN1_GW" dev eth3
	ip rule add pref 10 fwmark 0x0ffffa00 lookup main
	br_n=0
	while [ "$br_n" -lt "$POLICIES" ]; do
		br_mark="$(policy_mark "$br_n")"
		br_table=$((4096 + 2 * br_n))
		br_pref=$((100 + 2 * br_n))
		ip route add "$LAN_NET" dev br0 table "$br_table"
		if [ "$br_n" -eq "$DEVICE_POLICY" ]; then
			ip route add default via "$WAN2_GW" dev eth2.5 table "$br_table"
		else
			ip route add default via "$WAN1_GW" dev eth3 table "$br_table"
		fi
		ip rule add pref "$br_pref" fwmark "$br_mark" lookup "$br_table"
		ip rule add pref $((br_pref + 1)) fwmark "$br_mark" blackhole
		br_n=$((br_n + 1))
	done
	ip route add "$WAN2_NET" dev eth2.5 table 16386
	ip route add default via "$WAN2_GW" dev eth2.5 table 16386
	ip rule add pref 1154 from "$WAN2_ADDR" lookup 16386
	ip route add "$WAN1_NET" dev eth3 table 16387
	ip route add default via "$WAN1_GW" dev eth3 table 16387
	ip rule add pref 1156 from "$WAN1_ADDR" lookup 16387
	ip route add default dev nwg1 table 16395
	ip rule add pref 1172 from "$NWG_ADDR" lookup 16395
}

build_firewall() {
	iptables-restore < "$STATE/mangle.rules"
	ip6tables-restore < "$STATE/mangle.rules"
	for bf_dev in eth3 eth2.5 nwg1; do
		iptables -w -t nat -A POSTROUTING -o "$bf_dev"
	done
	iptables -w -t nat -A POSTROUTING -o eth3 -j MASQUERADE
	iptables -w -t nat -A POSTROUTING -o eth2.5 -j MASQUERADE
}

summary() {
	cat <<EOF
keenetic-standin: router up, pid $$, state in $STATE
  br0 $LAN_ADDR/24: kn-default $DEFAULT_DEV_ADDR ($DEFAULT_DEV_MAC, default policy),
    kn-policy $POLICY_DEV_ADDR ($POLICY_DEV_MAC, policy $DEVICE_POLICY, mark $(policy_mark "$DEVICE_POLICY"))
  eth3 $WAN1_ADDR/$WAN1_LEN to kn-isp1 (main table), eth2.5 $WAN2_ADDR/$WAN2_LEN to kn-isp2 (table 4096 of policy $DEVICE_POLICY)
  nwg1 $NWG_ADDR/32, a TUN device carried to nwgpeer $NWG_PEER in kn-vpn
  $SITE_IN_SET and $SITE_OTHER answer on ports 80 and 443 in kn-isp1, kn-isp2 and kn-vpn with that uplink's name
  fake RCI on 127.0.0.1:$RCI_PORT, /var/run/ndm present, mangle holds $HOTSPOT
EOF
}

stop_rewrites() {
	[ -f "$STATE/rewrite.pid" ] || return 0
	sr_pid="$(cat "$STATE/rewrite.pid")"
	if [ -n "$sr_pid" ] && grep -q mangle.rules "/proc/$sr_pid/cmdline" 2>/dev/null; then
		kill "$sr_pid" 2>/dev/null || true
	fi
	rm -f "$STATE/rewrite.pid"
}

finish() {
	fin_status=$?
	trap - EXIT HUP INT TERM
	set +e
	if [ -n "${b4_pid:-}" ] && kill -0 "$b4_pid" 2>/dev/null; then
		echo "keenetic-standin: stopping b4, pid $b4_pid" >&2
		kill "$b4_pid"
		(
			sleep 30
			kill -9 "$b4_pid" 2>/dev/null
		) </dev/null >/dev/null 2>&1 &
		fin_killer=$!
		wait "$b4_pid"
		fin_b4=$?
		kill "$fin_killer" 2>/dev/null
		[ "$fin_status" -ne 0 ] || fin_status=$fin_b4
	fi
	stop_rewrites
	if [ -f "$STATE/helpers" ]; then
		while read -r fin_helper; do
			kill "$fin_helper" 2>/dev/null
		done < "$STATE/helpers"
	fi
	rm -f "$STATE/pid" "$STATE/b4.pid" "$STATE/helpers"
	echo "keenetic-standin: stopped" >&2
	exit "$fin_status"
}

launch() {
	config_arg="$1"
	shift
	b4_bin=""
	if [ "$#" -gt 0 ]; then
		b4_bin="$1"
		shift
	fi
	[ -z "$b4_bin" ] || [ -x "$b4_bin" ] || die "$b4_bin is not an executable file"
	[ "$(readlink /proc/self/ns/net)" != "$(readlink /proc/1/ns/net)" ] ||
		die "this is the guest's own network namespace: start the stand-in as the binary of scripts/b4-netns.sh"
	for tool in python3 iptables-restore ip6tables-restore curl nsenter; do
		command -v "$tool" >/dev/null 2>&1 || die "$tool is missing in the guest"
	done
	[ -c /dev/net/tun ] || die "/dev/net/tun is missing in the guest"
	if old="$(standin_pid)"; then
		die "a stand-in already runs as pid $old: keenetic-standin.sh stop"
	fi
	mkdir -p "$STATE"
	rm -f "$STATE"/*.log "$STATE/helpers" "$STATE/b4.pid"
	stop_rewrites
	echo "$$" > "$STATE/pid"
	b4_pid=""
	trap finish EXIT
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 0' TERM
	write_helper
	write_mangle
	write_rci
	build_lan
	build_uplinks
	build_tunnel
	build_routing
	build_firewall
	start_helper rci "" rci "$RCI_PORT" "$STATE/rci.json"
	wait_for "the fake RCI" curl -s -m 1 "http://127.0.0.1:$RCI_PORT/rci/show/interface"
	mkdir -p /var/run/ndm
	summary
	if [ -z "$b4_bin" ]; then
		echo "keenetic-standin: no b4 binary given, the router runs without b4 until: keenetic-standin.sh stop"
		while :; do
			sleep 1
		done
	fi
	"$b4_bin" "$config_arg" "$@" </dev/null &
	b4_pid=$!
	echo "$b4_pid" > "$STATE/b4.pid"
	b4_status=0
	wait "$b4_pid" || b4_status=$?
	b4_pid=""
	exit "$b4_status"
}

check() {
	rp="$(router_pid)"
	if [ -f "$STATE/b4.pid" ]; then
		echo "b4 runs as pid $(cat "$STATE/b4.pid")"
	else
		echo "b4 does not run"
	fi
	printf '%-8s %-16s %s\n' from to "answer: the uplink, the source address it saw"
	for ck_from in default policy router; do
		for ck_addr in "$SITE_IN_SET" "$SITE_OTHER"; do
			for ck_port in 80 443; do
				ck_answer="$(run_in "$ck_from" curl -s -m 3 "http://$ck_addr:$ck_port/" </dev/null || echo "no answer")"
				printf '%-8s %-16s %s\n' "$ck_from" "$ck_addr:$ck_port" "$ck_answer"
			done
		done
	done
}

show() {
	rp="$(router_pid)"
	echo "== ip rule"
	run_in router ip rule
	for sw_table in $(run_in router ip rule | sed -n 's/.*lookup \([0-9][0-9]*\).*/\1/p' | sort -n -u); do
		echo "== table $sw_table"
		run_in router ip route show table "$sw_table"
	done
	echo "== mangle PREROUTING"
	run_in router iptables -w -t mangle -L PREROUTING -n -v --line-numbers
	echo "== mangle $HOTSPOT"
	run_in router iptables -w -t mangle -L "$HOTSPOT" -n -v 2>&1 || true
	echo "== nat POSTROUTING"
	run_in router iptables -w -t nat -L POSTROUTING -n -v --line-numbers
	echo "== rp_filter"
	for sw_conf in all default br0 eth3 eth2.5 nwg1; do
		echo "$sw_conf $(run_in router cat "/proc/sys/net/ipv4/conf/$sw_conf/rp_filter")"
	done
	echo "== NDMS rewrites"
	cat "$STATE/rewrite.log" 2>/dev/null || echo "none"
}

rewrite_once() {
	run_in router iptables-restore < "$STATE/mangle.rules"
	run_in router ip6tables-restore < "$STATE/mangle.rules"
	echo "$(date '+%F %T') mangle rewritten" >> "$STATE/rewrite.log"
	run_in router iptables -w -t mangle -S PREROUTING
}

rewrite() {
	rp="$(router_pid)"
	case "${1:-}" in
	"")
		rewrite_once
		;;
	stop)
		stop_rewrites
		echo "keenetic-standin: no background rewrites"
		;;
	0 | *[!0-9]*)
		die "usage: keenetic-standin.sh rewrite [<seconds> | stop]"
		;;
	*)
		stop_rewrites
		nsenter -t "$rp" -n -m setsid sh -c '
			echo "$$" > "$0/rewrite.pid"
			while :; do
				sleep "$1"
				iptables-restore < "$0/mangle.rules"
				ip6tables-restore < "$0/mangle.rules"
				echo "$(date "+%F %T") mangle rewritten"
			done
		' "$STATE" "$1" </dev/null >>"$STATE/rewrite.log" 2>&1 &
		wait_for "the rewrite loop" test -s "$STATE/rewrite.pid"
		echo "keenetic-standin: mangle rewritten every $1 s, pid $(cat "$STATE/rewrite.pid")"
		;;
	esac
}

stop() {
	if ! st_pid="$(standin_pid)"; then
		echo "keenetic-standin: none runs"
		return 0
	fi
	kill "$st_pid"
	st_tries=0
	while kill -0 "$st_pid" 2>/dev/null; do
		st_tries=$((st_tries + 1))
		[ "$st_tries" -lt 60 ] || die "pid $st_pid did not stop"
		sleep 1
	done
	echo "keenetic-standin: stopped"
}

main() {
	case "${1:-}" in
	--config=*) launch "$@" ;;
	check) check ;;
	show) show ;;
	rewrite)
		shift
		rewrite "$@"
		;;
	rci) cat "$STATE/rci.log" ;;
	in)
		shift
		[ "$#" -ge 2 ] || die "usage: keenetic-standin.sh in <where> <command...>"
		rp="$(router_pid)"
		run_in "$@"
		;;
	stop) stop ;;
	help | -h | --help) usage ;;
	*)
		usage >&2
		exit 1
		;;
	esac
}

main "$@"
