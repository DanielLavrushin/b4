BIN="${1:-}"
PATTERN="${2:-TestNetns}"
COUNT="${3:-1}"
[ -x "$BIN" ] || {
	echo "usage: netns-test.sh <test binary on the guest> [pattern] [count]" >&2
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
	B4_NETNS_TEST=1 exec "$0" -test.run "$1" -test.count "$2" -test.v
' "$BIN" "$PATTERN" "$COUNT"
