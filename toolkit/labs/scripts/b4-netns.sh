ROOT=""
UPLINK=0
while :; do
	case "${1:-}" in
	--root)
		ROOT="${2:-}"
		[ -n "$ROOT" ] && [ -d "$ROOT/proc" ] || {
			echo "b4-netns.sh: '$ROOT' is not a root filesystem to run b4 in" >&2
			exit 1
		}
		shift 2
		;;
	--uplink)
		UPLINK=1
		shift
		;;
	*) break ;;
	esac
done
BIN="${1:-}"
CONFIG="${2:-}"
[ -n "$BIN" ] && [ -x "$ROOT$BIN" ] && [ -n "$CONFIG" ] && [ -f "$ROOT$CONFIG" ] || {
	echo "usage: b4-netns.sh [--root <root filesystem>] [--uplink] <b4 binary on the guest> <config on the guest> [b4 arguments]" >&2
	echo "       with --root, b4 runs inside that root filesystem and both paths are inside it" >&2
	echo "       with --uplink, the namespace gets a dummy uplink wan0, 198.51.100.1/24, and a default route through it" >&2
	exit 1
}
shift 2
exec unshare -n -m sh -ec '
	mount --make-rprivate /
	mount -t tmpfs -o mode=0755 tmpfs /run
	mount -t tmpfs tmpfs /tmp
	mount -t sysfs sysfs /sys
	ip link set lo up
	root="$0"
	uplink="$1"
	bin="$2"
	config="$3"
	shift 3
	if [ "$uplink" = 1 ]; then
		ip link add wan0 type dummy
		ip addr add 198.51.100.1/24 dev wan0
		ip link set wan0 up
		ip route add default via 198.51.100.254 dev wan0 onlink
	fi
	if [ -z "$root" ]; then
		[ -x "$bin" ] && [ -f "$config" ] || {
			echo "b4-netns.sh: $bin or $config is out of reach inside the run, which has a /tmp of its own: keep both outside /tmp" >&2
			exit 1
		}
		exec "$bin" --config="$config" "$@"
	fi
	for d in proc sys dev; do
		mount --rbind "/$d" "$root/$d"
	done
	mount -t tmpfs tmpfs "$root/tmp"
	if [ -L "$root/run" ]; then
		mkdir -p "$root/tmp/run"
	else
		mount -t tmpfs -o mode=0755 tmpfs "$root/run"
	fi
	[ -x "$root$bin" ] && [ -f "$root$config" ] || {
		echo "b4-netns.sh: $bin or $config is not inside $root, or is under its /tmp" >&2
		exit 1
	}
	chroot_bin="$(command -v chroot)"
	exec env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/root "$chroot_bin" "$root" "$bin" --config="$config" "$@"
' "$ROOT" "$UPLINK" "$BIN" "$CONFIG" "$@"
