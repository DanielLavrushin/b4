#!/bin/sh

STATE="/opt/share/b4diag/.deadman"
LOG="/opt/share/b4diag/deadman.log"
SERVICE="/opt/etc/init.d/S99b4"

usage() {
    echo "usage: $0 {arm [seconds]|cancel|status|now}"
    echo
    echo "  arm 180   in 180 seconds, stop b4 and clear its rules unless cancelled"
    echo "  cancel    call this once the router is still answering and you are happy"
    echo "  status    show whether a countdown is running and how long is left"
    echo "  now       run the recovery immediately"
    echo
    echo "arm it BEFORE you enable the set. If the box goes silent, do nothing:"
    echo "the countdown removes b4 and the network comes back without a power cycle."
    echo "the log of what it did is $LOG, which survives a reboot."
}

say() {
    echo "[deadman] $*"
    echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >>"$LOG"
}

recover() {
    mkdir -p /opt/share/b4diag 2>/dev/null
    say "recovery starting"

    if [ -x "$SERVICE" ]; then
        "$SERVICE" stop >>"$LOG" 2>&1
        say "ran $SERVICE stop"
    fi
    sleep 3
    if pidof b4 >/dev/null 2>&1; then
        killall -9 b4 2>/dev/null
        say "b4 did not exit, killed it"
        sleep 1
    fi

    for t in mangle nat filter raw; do
        for parent in PREROUTING INPUT FORWARD OUTPUT POSTROUTING; do
            while iptables -t "$t" -D "$parent" -j B4 2>/dev/null; do :; done
            while iptables -t "$t" -D "$parent" -j B4_PREROUTING 2>/dev/null; do :; done
            iptables -t "$t" -L "$parent" -n --line-numbers 2>/dev/null \
                | awk '/b4r_/ {print $1}' | sort -rn | while read -r ln; do
                    iptables -t "$t" -D "$parent" "$ln" 2>/dev/null
                done
        done
        iptables -t "$t" -L -n 2>/dev/null | awk '/^Chain (B4|b4r_)/ {print $2}' | while read -r c; do
            iptables -t "$t" -F "$c" 2>/dev/null
            iptables -t "$t" -X "$c" 2>/dev/null
        done
    done
    say "removed b4 firewall chains and jumps"

    ip rule show 2>/dev/null | awk -F: '$1 >= 10000 && $1 < 11000 {print $1}' | while read -r pr; do
        while ip rule del priority "$pr" 2>/dev/null; do :; done
        say "removed ip rule priority $pr"
    done

    ip route show table all 2>/dev/null | awk '/proto 155/ {for (i=1;i<NF;i++) if ($i=="table") print $(i+1)}' \
        | sort -u | while read -r tbl; do
            [ "$tbl" = "main" ] && continue
            [ "$tbl" = "local" ] && continue
            ip route flush table "$tbl" 2>/dev/null
            say "flushed routing table $tbl"
        done

    ipset list -n 2>/dev/null | grep '^b4r_' | while read -r s; do
        ipset flush "$s" 2>/dev/null
        ipset destroy "$s" 2>/dev/null
    done
    say "destroyed b4 ipsets"

    say "recovery done, b4 is stopped and its rules are gone"
    sync
}

case "$1" in
    arm)
        secs="${2:-180}"
        mkdir -p /opt/share/b4diag 2>/dev/null
        if [ -f "$STATE" ]; then
            echo "already armed, cancel it first"
            exit 1
        fi
        date +%s >"$STATE"
        echo "$secs" >>"$STATE"
        nohup "$0" _wait "$secs" >/dev/null 2>&1 &
        say "armed for ${secs}s, cancel with: $0 cancel"
        ;;
    _wait)
        secs="${2:-180}"
        i=0
        while [ "$i" -lt "$secs" ]; do
            [ -f "$STATE" ] || exit 0
            sleep 1
            i=$((i + 1))
        done
        [ -f "$STATE" ] || exit 0
        rm -f "$STATE"
        recover
        ;;
    cancel)
        if [ -f "$STATE" ]; then
            rm -f "$STATE"
            say "cancelled"
        else
            echo "nothing armed"
        fi
        ;;
    status)
        if [ -f "$STATE" ]; then
            start=$(sed -n 1p "$STATE")
            secs=$(sed -n 2p "$STATE")
            now=$(date +%s)
            echo "armed, $((start + secs - now))s left"
        else
            echo "not armed"
        fi
        ;;
    now)
        rm -f "$STATE"
        recover
        ;;
    *)
        usage
        exit 1
        ;;
esac
