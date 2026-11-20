BIN="${1:-}"
PATTERN="${2:-TestNetns}"
COUNT="${3:-1}"
ROOT="${4:-}"
[ -x "$BIN" ] || {
	echo "usage: netns-test.sh <test binary on the guest> [pattern] [count] [root]" >&2
	exit 1
}
[ -z "$ROOT" ] || [ -d "$ROOT/proc" ] || {
	echo "netns-test.sh: $ROOT is not a root filesystem to run the tests in" >&2
	exit 1
}
exec unshare -n -m sh -ec '
	mount --make-rprivate /
	mount -t tmpfs tmpfs /run
	mount -t tmpfs tmpfs /tmp
	mount -t sysfs sysfs /sys
	ip link set lo up
	[ -x "$0" ] || {
		echo "netns-test.sh: $0 is out of reach inside the run, which has a /tmp of its own: keep the binary outside /tmp" >&2
		exit 1
	}
	if [ -z "$3" ]; then
		B4_NETNS_TEST=1 exec "$0" -test.run "$1" -test.count "$2" -test.v
	fi
	for d in proc sys dev; do
		mount --rbind "/$d" "$3/$d"
	done
	mount -t tmpfs tmpfs "$3/tmp"
	if [ -L "$3/run" ]; then
		mkdir -p "$3/tmp/run"
	else
		mount -t tmpfs tmpfs "$3/run"
	fi
	mkdir -p "$3/lab"
	mount --bind "$(dirname "$0")" "$3/lab"
	chroot_bin="$(command -v chroot)"
	exec env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/root B4_NETNS_TEST=1 \
		"$chroot_bin" "$3" "/lab/$(basename "$0")" -test.run "$1" -test.count "$2" -test.v
' "$BIN" "$PATTERN" "$COUNT" "$ROOT"
