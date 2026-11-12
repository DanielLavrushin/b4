set -e
export DEBIAN_FRONTEND=noninteractive

codename="$(sed -n 's/^VERSION=.*(\(.*\)).*/\1/p' /etc/os-release)"
[ -n "$codename" ] || {
	echo "cannot read the Debian release name from /etc/os-release" >&2
	exit 1
}

systemctl stop apt-daily.timer apt-daily-upgrade.timer apt-daily.service apt-daily-upgrade.service 2>/dev/null || true

case "$codename" in
jessie)
	apt_source="deb [trusted=yes]"
	apt_force="--force-yes"
	;;
*)
	apt_source="deb"
	apt_force=""
	;;
esac

printf '%s\n' \
	"$apt_source http://archive.debian.org/debian $codename main" \
	"$apt_source http://archive.debian.org/debian-security $codename/updates main" \
	> /etc/apt/sources.list
rm -f /etc/apt/sources.list.d/*
printf 'Acquire::Check-Valid-Until "false";\nAcquire::ForceIPv4 "true";\n' > /etc/apt/apt.conf.d/99b4lab

tries=0
until apt-get update -q >/dev/null 2>&1; do
	tries=$((tries + 1))
	[ "$tries" -lt 10 ] || {
		echo "apt-get update keeps failing" >&2
		exit 1
	}
	sleep 3
done
apt-get install -y -q $apt_force --no-install-recommends ipset jq curl python3 conntrack tcpdump socat >/dev/null

for bin in iptables ip6tables; do
	[ -e "/usr/sbin/$bin-legacy" ] && continue
	printf '%s\nexec /sbin/%s "$@"\n' '#!/bin/sh' "$bin" > "/usr/sbin/$bin-legacy"
	chmod +x "/usr/sbin/$bin-legacy"
done

for mod in nfnetlink_queue xt_NFQUEUE xt_TPROXY xt_socket xt_set xt_connbytes xt_hashlimit xt_mark xt_connmark xt_conntrack xt_addrtype; do
	modprobe "$mod" 2>/dev/null || echo "module $mod did not load" >&2
done

echo "provisioned: kernel $(uname -r), $(iptables --version), $(ip -V 2>&1), ipset $(ipset version | head -1)"
