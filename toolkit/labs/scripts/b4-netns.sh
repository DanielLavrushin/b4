BIN="${1:-}"
CONFIG="${2:-}"
[ -x "$BIN" ] && [ -f "$CONFIG" ] || {
	echo "usage: b4-netns.sh <b4 binary on the guest> <config on the guest> [b4 arguments]" >&2
	exit 1
}
shift 2
exec unshare -n -m sh -ec '
	mount --make-rprivate /
	mount -t tmpfs tmpfs /run
	mount -t tmpfs tmpfs /tmp
	mount -t sysfs sysfs /sys
	ip link set lo up
	bin="$0"
	config="$1"
	shift
	[ -x "$bin" ] && [ -f "$config" ] || {
		echo "b4-netns.sh: $bin or $config is out of reach inside the run, which has a /tmp of its own: keep both outside /tmp" >&2
		exit 1
	}
	exec "$bin" --config="$config" "$@"
' "$BIN" "$CONFIG" "$@"
