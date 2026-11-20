set -eu
[ -z "${LAB_PROVISION_ARGS:-}" ] || set -- $LAB_PROVISION_ARGS "$@"

ROOT=/xiaomi
HIDDEN=/root/hidden-modules
FIRMWARE=/root/lab-files/miwifi_rc06_firmware_93757_1.1.16.bin
FIRMWARE_SHA256=0f7aac416bba6b0b31e6511937083169859ae353848bd486bb5aaa049d9abfc4
UBI_OFFSET=764
UBI_SIZE=38273024
UBI_SHA256=9c8734d888442af948fa9272caaee6620f419d7f0d4423a4d99e94058a53aaad
ROOTFS_FIRST_PEB=33
ROOTFS_LAST_PEB=291
ROOTFS_SHA256=1078842a7e3019eaa835dd1944dae888619aec7291fcbe47bea664d2a4b74d93
FIRMWARE_KERNEL=5.4.164
FIRMWARE_BUILTIN="nfnetlink nfnetlink_queue nfnetlink_log nf_conntrack nf_conntrack_netlink nf_defrag_ipv4 nf_defrag_ipv6 br_netfilter nf_flow_table"
KERNEL="$(uname -r)"
WORK=/root/be7000-work
export DEBIAN_FRONTEND=noninteractive

checksum() {
	[ "$(sha256sum "$1" | cut -d' ' -f1)" = "$2" ] || {
		echo "be7000.provision.sh: $1 does not have the checksum $2" >&2
		exit 1
	}
}

cloud-init status --wait > /dev/null 2>&1 || true
systemctl disable --now unattended-upgrades.service apt-daily.timer apt-daily-upgrade.timer > /dev/null 2>&1 || true
systemctl stop apt-daily.service apt-daily-upgrade.service > /dev/null 2>&1 || true
dpkg-query -W -f '${db:Status-Abbrev} ${Package}\n' 'linux-*' 2> /dev/null | awk '$1 == "ii" { print $2 }' | xargs -r apt-mark hold > /dev/null
apt-get -q -o DPkg::Lock::Timeout=900 update > /dev/null
apt-get -y -q -o DPkg::Lock::Timeout=900 install squashfs-tools > /dev/null

checksum "$FIRMWARE" "$FIRMWARE_SHA256"
rm -rf "$WORK"
mkdir -p "$WORK"
tail -c +$((UBI_OFFSET + 1)) "$FIRMWARE" | head -c "$UBI_SIZE" > "$WORK/root.ubi"
checksum "$WORK/root.ubi" "$UBI_SHA256"
i="$ROOTFS_FIRST_PEB"
while [ "$i" -le "$ROOTFS_LAST_PEB" ]; do
	dd if="$WORK/root.ubi" bs=4096 skip=$((i * 32 + 1)) count=31 2> /dev/null
	i=$((i + 1))
done > "$WORK/rootfs.sqfs"
checksum "$WORK/rootfs.sqfs" "$ROOTFS_SHA256"
rm -rf "$ROOT"
unsquashfs -q -d "$ROOT" "$WORK/rootfs.sqfs" > /dev/null
rm -rf "$WORK"
for d in proc sys dev tmp lab; do
	mkdir -p "$ROOT/$d"
done

firmware_has() {
	[ -f "$ROOT/lib/modules/$FIRMWARE_KERNEL/$1.ko" ] && return 0
	for b in $FIRMWARE_BUILTIN; do
		[ "$b" = "$1" ] && return 0
	done
	return 1
}

mkdir -p "$HIDDEN"
: > /etc/modules-load.d/be7000.conf
for f in $(find "/lib/modules/$KERNEL/kernel/net/netfilter" "/lib/modules/$KERNEL/kernel/net/ipv4/netfilter" \
	"/lib/modules/$KERNEL/kernel/net/ipv6/netfilter" "/lib/modules/$KERNEL/kernel/net/bridge/netfilter" -name '*.ko'); do
	m="$(basename "$f" .ko)"
	hide=1
	firmware_has "$m" && hide=0
	for extra in "$@"; do
		[ "$extra" = "$m" ] && hide=1
	done
	if [ "$hide" = 1 ]; then
		modprobe -r "$m" 2> /dev/null || true
		mv "$f" "$HIDDEN/"
	else
		echo "$m" >> /etc/modules-load.d/be7000.conf
	fi
done
for extra in "$@"; do
	f="$(find "/lib/modules/$KERNEL" -name "$extra.ko" | head -1)"
	[ -z "$f" ] || mv "$f" "$HIDDEN/"
done
depmod -a
for m in xt_NFQUEUE xt_hashlimit xt_NFLOG ip6table_raw "$@"; do
	! modprobe -n "$m" 2> /dev/null || {
		echo "be7000.provision.sh: the module $m is still loadable, but the firmware has no such module" >&2
		exit 1
	}
done
sort -u -o /etc/modules-load.d/be7000.conf /etc/modules-load.d/be7000.conf

apt-get -y -q -o DPkg::Lock::Timeout=900 install make gcc "linux-headers-$KERNEL" > /dev/null
mkdir -p "$WORK/xt_mark"
cp /root/lab-files/be7000-xt_mark.c "$WORK/xt_mark/xt_mark.c"
echo 'obj-m := xt_mark.o' > "$WORK/xt_mark/Kbuild"
make -s -C "/lib/modules/$KERNEL/build" M="$WORK/xt_mark" modules > /dev/null
find "/lib/modules/$KERNEL/kernel" -name 'xt_mark.ko' -exec mv {} "$HIDDEN/xt_mark.ko.upstream" \;
install -D -m 644 "$WORK/xt_mark/xt_mark.ko" "/lib/modules/$KERNEL/updates/xt_mark.ko"
rm -rf "$WORK"
echo xt_mark >> /etc/modules-load.d/be7000.conf
sort -u -o /etc/modules-load.d/be7000.conf /etc/modules-load.d/be7000.conf
depmod -a
modprobe -r xt_mark 2> /dev/null || true
xargs -r -n 1 modprobe < /etc/modules-load.d/be7000.conf 2> /dev/null || true
mkdir -p "$ROOT/tmp/run"
chroot "$ROOT" /usr/sbin/iptables -t mangle -N B4_LAB_CHECK
chroot "$ROOT" /usr/sbin/iptables -t mangle -A B4_LAB_CHECK -m mark --mark 0x1/0x1 -j MARK --set-xmark 0x2/0x2 || {
	echo "be7000.provision.sh: the firmware's iptables cannot add a MARK rule; see dmesg. An xt_mark loaded before this run stays in use until the guest restarts (lab down, lab up)" >&2
	exit 1
}
chroot "$ROOT" /usr/sbin/iptables -t mangle -F B4_LAB_CHECK
chroot "$ROOT" /usr/sbin/iptables -t mangle -X B4_LAB_CHECK

chroot "$ROOT" /usr/sbin/iptables -V
chroot "$ROOT" /usr/sbin/ipset -v | head -1
chroot "$ROOT" /usr/sbin/ip -V
chroot "$ROOT" /bin/busybox 2>&1 | head -1
echo "be7000.provision.sh: $(wc -l < /etc/modules-load.d/be7000.conf) netfilter modules load at boot, $(ls "$HIDDEN" | wc -l) are hidden"
