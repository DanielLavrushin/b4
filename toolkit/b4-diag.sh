#!/bin/sh

OUTROOT="${OUTROOT:-/opt/share/b4diag}"
INTERVAL="${INTERVAL:-2}"
MAXSAMPLES="${MAXSAMPLES:-900}"
KEEPSAMPLES="${KEEPSAMPLES:-120}"
RUNFILE="$OUTROOT/.watch.pid"
BASEDIR="$OUTROOT/baseline"

usage() {
    echo "usage: $0 {baseline|snapshot|watch|stopwatch|diff|status}"
    echo
    echo "  baseline   record the firewall as it stands BEFORE b4 touches it"
    echo "  snapshot   collect everything once and exit"
    echo "  watch      collect a summary every ${INTERVAL}s until stopped or the box dies"
    echo "  stopwatch  stop a running watch"
    echo "  diff       show which firewall rules disappeared since the baseline"
    echo "  status     show whether a watch is running and where its output is"
    echo
    echo "output goes under $OUTROOT/<timestamp>/ which survives a reboot"
    echo "everything here only reads: no rule, route, sysctl or interface is changed"
}

have() { command -v "$1" >/dev/null 2>&1; }

b4pid() {
    if [ -f /opt/var/run/b4.pid ]; then
        p=$(cat /opt/var/run/b4.pid 2>/dev/null)
        [ -n "$p" ] && [ -d "/proc/$p" ] && echo "$p" && return 0
    fi
    for p in /proc/[0-9]*; do
        [ -r "$p/comm" ] || continue
        if [ "$(cat "$p/comm" 2>/dev/null)" = "b4" ]; then
            basename "$p"
            return 0
        fi
    done
    return 1
}

section() {
    printf '\n===== %s =====\n' "$1" >>"$2"
}

collect_static() {
    d="$1"
    f="$d/static.txt"
    : >"$f"

    section "date uname" "$f"; date >>"$f" 2>&1; uname -a >>"$f" 2>&1
    section "b4 process" "$f"
    p=$(b4pid) && {
        echo "pid $p" >>"$f"
        cat "/proc/$p/status" >>"$f" 2>&1
        echo "fds: $(ls "/proc/$p/fd" 2>/dev/null | wc -l)" >>"$f"
    } || echo "b4 is not running" >>"$f"

    section "interfaces" "$f"; ip -o link >>"$f" 2>&1; ip -o -4 addr >>"$f" 2>&1; ip -o -6 addr >>"$f" 2>&1
    section "ip rule v4" "$f"; ip rule show >>"$f" 2>&1
    section "ip rule v6" "$f"; ip -6 rule show >>"$f" 2>&1
    section "routes all tables v4" "$f"; ip route show table all >>"$f" 2>&1
    section "routes all tables v6" "$f"; ip -6 route show table all >>"$f" 2>&1
    section "rt_tables" "$f"; cat /etc/iproute2/rt_tables >>"$f" 2>&1

    section "iptables-save mangle" "$f"; iptables-save -t mangle >>"$f" 2>&1
    section "iptables-save nat" "$f"; iptables-save -t nat >>"$f" 2>&1
    section "iptables-save filter" "$f"; iptables-save -t filter >>"$f" 2>&1
    section "iptables-save raw" "$f"; iptables-save -t raw >>"$f" 2>&1
    have ip6tables-save && { section "ip6tables-save mangle" "$f"; ip6tables-save -t mangle >>"$f" 2>&1; }
    have nft && { section "nft ruleset" "$f"; nft list ruleset >>"$f" 2>&1; }

    section "ipset -t" "$f"; ipset list -t >>"$f" 2>&1
    section "ipset b4r members" "$f"
    for s in $(ipset list -n 2>/dev/null | grep '^b4r_'); do
        echo "--- $s ---" >>"$f"
        ipset list "$s" 2>/dev/null | sed -n '/Members/,$p' | head -200 >>"$f"
    done

    section "rp_filter" "$f"
    for k in /proc/sys/net/ipv4/conf/*/rp_filter; do
        echo "$k = $(cat "$k" 2>/dev/null)" >>"$f"
    done
    section "forwarding and conntrack sysctls" "$f"
    for k in /proc/sys/net/ipv4/ip_forward \
             /proc/sys/net/netfilter/nf_conntrack_max \
             /proc/sys/net/netfilter/nf_conntrack_count \
             /proc/sys/net/netfilter/nf_conntrack_tcp_be_liberal \
             /proc/sys/net/netfilter/nf_conntrack_checksum; do
        echo "$k = $(cat "$k" 2>/dev/null)" >>"$f"
    done

    section "route get probes" "$f"
    ip route get 1.1.1.1 >>"$f" 2>&1
    ip route get 1.1.1.1 from 192.168.1.100 iif br0 >>"$f" 2>&1
    section "xray-manual status" "$f"
    [ -x /opt/share/xrayui/xray-manual.sh ] && /opt/share/xrayui/xray-manual.sh status >>"$f" 2>&1

    section "b4 log tail" "$f"; tail -300 /var/log/b4/b4.log >>"$f" 2>&1
    section "b4 error log tail" "$f"; tail -200 /var/log/b4/errors.log >>"$f" 2>&1
    section "xray log tail" "$f"; tail -200 /tmp/xray-manual.log >>"$f" 2>&1
    section "dmesg tail" "$f"; dmesg 2>/dev/null | tail -200 >>"$f" 2>&1
    sync
}

sample() {
    d="$1"
    n="$2"
    f="$d/s$n.txt"
    : >"$f"

    echo "time $(date '+%Y-%m-%d %H:%M:%S')" >>"$f"
    echo "load $(cat /proc/loadavg 2>/dev/null)" >>"$f"
    ct=$(cat /proc/sys/net/netfilter/nf_conntrack_count 2>/dev/null)
    ctmax=$(cat /proc/sys/net/netfilter/nf_conntrack_max 2>/dev/null)
    echo "conntrack ${ct:-?}/${ctmax:-?}" >>"$f"
    mt=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}')
    ma=$(grep MemAvailable /proc/meminfo 2>/dev/null | awk '{print $2}')
    [ -z "$ma" ] && ma=$(grep MemFree /proc/meminfo 2>/dev/null | awk '{print $2}')
    echo "mem_avail_kb ${ma:-?} of ${mt:-?}" >>"$f"

    p=$(b4pid)
    if [ -n "$p" ]; then
        thr=$(grep Threads "/proc/$p/status" 2>/dev/null | awk '{print $2}')
        rss=$(grep VmRSS "/proc/$p/status" 2>/dev/null | awk '{print $2}')
        fds=$(ls "/proc/$p/fd" 2>/dev/null | wc -l)
        echo "b4 pid=$p threads=${thr:-?} rss_kb=${rss:-?} fds=${fds:-?}" >>"$f"
    else
        echo "b4 not running" >>"$f"
    fi

    section "ip -s link" "$f"; ip -s link >>"$f" 2>&1
    section "mangle counters" "$f"; iptables -t mangle -L -n -v -x >>"$f" 2>&1
    section "nat counters" "$f"; iptables -t nat -L -n -v -x >>"$f" 2>&1
    section "ip rule" "$f"; ip rule show >>"$f" 2>&1
    section "conntrack top destinations" "$f"
    awk '{for(i=1;i<=NF;i++) if($i ~ /^dst=/) {print $i; break}}' /proc/net/nf_conntrack 2>/dev/null \
        | sort | uniq -c | sort -rn | head -15 >>"$f" 2>&1
    section "top" "$f"; top -b -n 1 2>/dev/null | head -20 >>"$f" 2>&1

    counts=""
    for t in mangle nat filter; do
        c=$(iptables-save -t "$t" 2>/dev/null | grep -c '^-A')
        counts="$counts $t=$c"
        prevc=$(cat "$d/.count-$t" 2>/dev/null)
        echo "$c" >"$d/.count-$t"
        if [ -n "$prevc" ] && [ "$prevc" != "$c" ]; then
            echo "$(date '+%H:%M:%S') $t rule count changed $prevc -> $c" >>"$d/ruleschanged.txt"
            iptables-save -t "$t" 2>/dev/null >"$d/$t-at-$n.txt"
        fi
    done
    echo "rules$counts" >>"$f"

    line="$(date '+%H:%M:%S') ct=${ct:-?}/${ctmax:-?} memfree=${ma:-?}kb load=$(cut -d' ' -f1 /proc/loadavg 2>/dev/null) rules$counts"
    if [ -n "$p" ]; then
        line="$line b4=thr:${thr:-?},rss:${rss:-?}kb,fd:${fds:-?}"
    else
        line="$line b4=down"
    fi
    for i in br0 xray0; do
        st=$(ip -s link show "$i" 2>/dev/null | tail -3 | tr -s ' ' | tr '\n' ' ')
        [ -n "$st" ] && line="$line | $i $st"
    done
    echo "$line" >>"$d/timeline.txt"
    sync
}

prune() {
    d="$1"
    cnt=$(ls "$d"/s*.txt 2>/dev/null | wc -l)
    [ "$cnt" -le "$KEEPSAMPLES" ] && return 0
    drop=$((cnt - KEEPSAMPLES))
    ls "$d"/s*.txt 2>/dev/null | head -"$drop" | while read -r old; do rm -f "$old"; done
}

save_tables() {
    d="$1"
    mkdir -p "$d" || return 1
    for t in mangle nat filter raw; do
        iptables-save -t "$t" 2>/dev/null | grep -v '^#' >"$d/ipt-$t.txt"
        have ip6tables-save && ip6tables-save -t "$t" 2>/dev/null | grep -v '^#' >"$d/ip6t-$t.txt"
    done
    ip rule show >"$d/iprule.txt" 2>&1
    ip route show table all >"$d/iproute.txt" 2>&1
    sync
}

do_baseline() {
    rm -rf "$BASEDIR"
    save_tables "$BASEDIR" || { echo "cannot write $BASEDIR"; exit 1; }
    date >"$BASEDIR/taken-at.txt"
    echo "baseline of the firewall saved to $BASEDIR"
    echo "now enable the set, then run: $0 diff"
}

do_diff() {
    if [ ! -d "$BASEDIR" ]; then
        echo "no baseline: run '$0 baseline' first, with the set disabled"
        exit 1
    fi
    ts=$(date +%Y%m%d-%H%M%S)
    d="$OUTROOT/diff-$ts"
    save_tables "$d" || exit 1
    echo "baseline taken $(cat "$BASEDIR/taken-at.txt" 2>/dev/null)"
    found=0
    for f in "$BASEDIR"/ipt-*.txt "$BASEDIR"/ip6t-*.txt "$BASEDIR"/iprule.txt; do
        [ -f "$f" ] || continue
        b=$(basename "$f")
        [ -f "$d/$b" ] || continue
        gone=$(grep -v -x -F -f "$d/$b" "$f" 2>/dev/null | grep -v 'b4r_\|B4\|^:')
        if [ -n "$gone" ]; then
            found=1
            echo
            echo "=== rules that were in $b before and are GONE now ==="
            echo "$gone" | sed 's/^/    /'
        fi
    done
    if [ "$found" = 0 ]; then
        echo "nothing that was there before has disappeared"
    else
        echo
        echo "the lines above are not b4's own. Something removed the firmware's rules."
    fi
    echo
    echo "full copies: $d"
}

do_snapshot() {
    ts=$(date +%Y%m%d-%H%M%S)
    d="$OUTROOT/$ts"
    mkdir -p "$d" || { echo "cannot create $d"; exit 1; }
    echo "collecting into $d"
    collect_static "$d"
    sample "$d" 0000
    echo "done: $d/static.txt and $d/s0000.txt"
}

do_watch() {
    ts=$(date +%Y%m%d-%H%M%S)
    d="$OUTROOT/$ts"
    mkdir -p "$d" || { echo "cannot create $d"; exit 1; }
    echo "$$" >"$RUNFILE"
    echo "$d" >"$OUTROOT/.watch.dir"
    collect_static "$d"
    i=0
    while [ "$i" -lt "$MAXSAMPLES" ]; do
        [ -f "$RUNFILE" ] || break
        n=$(printf '%04d' "$i")
        sample "$d" "$n"
        prune "$d"
        i=$((i + 1))
        sleep "$INTERVAL"
    done
    rm -f "$RUNFILE"
}

case "$1" in
    baseline)
        mkdir -p "$OUTROOT"
        do_baseline
        ;;
    diff)
        do_diff
        ;;
    snapshot)
        do_snapshot
        ;;
    watch)
        mkdir -p "$OUTROOT"
        if [ -f "$RUNFILE" ] && kill -0 "$(cat "$RUNFILE" 2>/dev/null)" 2>/dev/null; then
            echo "a watch is already running as pid $(cat "$RUNFILE")"
            exit 1
        fi
        nohup "$0" _watchloop >/dev/null 2>&1 &
        sleep 2
        echo "watch started, output under $(cat "$OUTROOT/.watch.dir" 2>/dev/null)"
        echo "stop it with: $0 stopwatch"
        ;;
    _watchloop)
        do_watch
        ;;
    stopwatch)
        rm -f "$RUNFILE"
        echo "watch will stop within ${INTERVAL}s"
        ;;
    status)
        if [ -f "$RUNFILE" ] && kill -0 "$(cat "$RUNFILE" 2>/dev/null)" 2>/dev/null; then
            echo "watch running as pid $(cat "$RUNFILE"), output $(cat "$OUTROOT/.watch.dir" 2>/dev/null)"
        else
            echo "no watch running"
        fi
        echo "collected runs:"
        ls -1 "$OUTROOT" 2>/dev/null | grep -v '^\.' | sed 's/^/  /'
        ;;
    *)
        usage
        exit 1
        ;;
esac
