#!/bin/sh

OUT="/opt/share/b4diag/stoptest-$(date +%Y%m%d-%H%M%S)"
B4BIN="${B4BIN:-/opt/sbin/b4}"
HOLD="${HOLD:-30}"
PROBES="https://example.com https://api.ipify.org"

mkdir -p "$OUT" || exit 1
exec >"$OUT/run.log" 2>&1

snap() {
    d="$OUT/$1"
    mkdir -p "$d"
    for t in mangle nat filter raw; do
        iptables -t "$t" -L -n -v -x >"$d/ipt-$t.txt" 2>&1
    done
    ip rule show >"$d/iprule.txt" 2>&1
    ip route show table all >"$d/iproute.txt" 2>&1
    ipset list -t >"$d/ipset.txt" 2>&1
    for s in $(ipset list -n 2>/dev/null | grep '^b4r_'); do
        ipset list "$s" 2>/dev/null | sed -n '/Members/,$p' >>"$d/ipset.txt"
    done
    for k in /proc/sys/net/ipv4/conf/all/rp_filter /proc/sys/net/ipv4/ip_forward \
             /proc/sys/net/netfilter/nf_conntrack_tcp_be_liberal \
             /proc/sys/net/netfilter/nf_conntrack_checksum; do
        echo "$k = $(cat $k 2>/dev/null)" >>"$d/sysctl.txt"
    done
    pidof b4 >"$d/b4pid.txt" 2>&1
    pidof xray >"$d/xraypid.txt" 2>&1
    sync
}

probe() {
    d="$OUT/$1-probe.txt"
    : >"$d"
    echo "--- dns ---" >>"$d"
    for h in example.com api.ipify.org hjem.dk; do
        echo "$h -> $(timeout 5 nslookup $h 2>/dev/null | sed -n 's/^Address [0-9]*: //p' | tr '\n' ' ')" >>"$d"
    done
    echo "--- curl from the ROUTER ---" >>"$d"
    for u in $PROBES; do
        echo "$u : $(curl -s -o /dev/null -m 5 -w '%{http_code} %{time_total}s' "$u" 2>&1)" >>"$d"
    done
    sync
}

echo "=== 1. with b4 RUNNING ==="
snap running
probe running

echo "=== 2. stopping b4 at $(date +%H:%M:%S) ==="
BPID=$(pidof b4)
echo "stopping pid $BPID"
kill -TERM "$BPID" 2>/dev/null
i=0
while [ "$i" -lt 20 ]; do
    pidof b4 >/dev/null 2>&1 || break
    sleep 1
    i=$((i + 1))
done
echo "b4 gone after ${i}s (pidof: $(pidof b4 2>/dev/null))"
sleep 2

echo "=== 3. with b4 STOPPED at $(date +%H:%M:%S), holding ${HOLD}s ==="
snap stopped
probe stopped
sleep "$HOLD"
echo "=== hold over at $(date +%H:%M:%S) ==="

echo "=== 4. starting b4 again ==="
nohup "$B4BIN" </dev/null >>/opt/share/b4diag/b4-restart.log 2>&1 &
sleep 8
if ! pidof b4 >/dev/null 2>&1; then
    echo "b4 did NOT come back, trying once more"
    nohup "$B4BIN" </dev/null >>/opt/share/b4diag/b4-restart.log 2>&1 &
    sleep 8
fi
if ! pidof b4 >/dev/null 2>&1; then
    echo "b4 STILL not running: clearing its leftovers so the network works without it"
    ip rule show 2>/dev/null | awk -F: "\$1 >= 10000 && \$1 < 11000 {print \$1}" | while read -r pr; do
        while ip rule del priority "$pr" 2>/dev/null; do :; done
    done
    ip route show table all 2>/dev/null | awk "/proto 155/ {for (i=1;i<NF;i++) if (\$i==\"table\") print \$(i+1)}" | sort -u | while read -r tbl; do
        [ "$tbl" = "main" ] || [ "$tbl" = "local" ] || ip route flush table "$tbl" 2>/dev/null
    done
    for t in mangle nat filter raw; do
        for parent in PREROUTING INPUT FORWARD OUTPUT POSTROUTING; do
            iptables -t "$t" -L "$parent" -n --line-numbers 2>/dev/null | awk "/b4r_|B4/ {print \$1}" | sort -rn | while read -r ln; do
                iptables -t "$t" -D "$parent" "$ln" 2>/dev/null
            done
        done
    done
fi
echo "final b4 pid: $(pidof b4 2>/dev/null)"

echo "=== 5. recovered ==="
snap recovered
probe recovered

echo "=== done, results in $OUT ==="
echo "$OUT" > /opt/share/b4diag/.last-stoptest
sync
