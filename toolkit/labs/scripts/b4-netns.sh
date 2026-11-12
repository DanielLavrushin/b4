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
	mount -t sysfs sysfs /sys
	ip link set lo up
	bin="$0"
	config="$1"
	shift
	exec "$bin" --config="$config" "$@"
' "$BIN" "$CONFIG" "$@"
