#!/bin/sh
set -u

unset B4_LAB_REPO LAB_BASE LAB_PKG
LAB_DIR="$(cd "$(dirname "$0")" && pwd)"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/home/instances/fake" "$T/bin" "$T/remote"
echo $$ > "$T/home/instances/fake/pid"
echo 2222 > "$T/home/instances/fake/port"
echo root > "$T/home/instances/fake/user"
echo debian9 > "$T/home/instances/fake/guest"

cat > "$T/bin/ssh" <<EOF
#!/bin/sh
for arg in "\$@"; do command="\$arg"; done
case "\$command" in
*change.test*) side=change ;;
*base.test*) side=base ;;
*) exit 0 ;;
esac
cat "$T/remote/\$side.out"
exit "\$(cat "$T/remote/\$side.status")"
EOF
cat > "$T/bin/go" <<'EOF'
#!/bin/sh
while [ "$#" -gt 0 ]; do
	[ "$1" = -o ] && : > "$2"
	shift
done
exit 0
EOF
cat > "$T/bin/unshare" <<'EOF'
#!/bin/sh
while [ "$#" -gt 0 ]; do
	case "$1" in
	-*) shift ;;
	*) break ;;
	esac
done
exec "$@"
EOF
cat > "$T/bin/mount" <<EOF
#!/bin/sh
echo "mount \$*" >> "$T/guest.log"
[ "\$1" = --make-rprivate ] && [ -e "$T/mounts-stay-shared" ] && exit 32
exit 0
EOF
cat > "$T/guest.bin" <<EOF
#!/bin/sh
echo "ran \$*" >> "$T/guest.log"
echo PASS
EOF
cat > "$T/bin/qemu-system-x86_64" <<EOF
#!/bin/sh
pidfile=""
port=""
while [ "\$#" -gt 0 ]; do
	case "\$1" in
	-pidfile) pidfile="\$2" ;;
	*hostfwd=*)
		port="\${1##*127.0.0.1:}"
		port="\${port%%-*}"
		;;
	esac
	shift
done
if [ -e "$T/qemu-broken" ]; then
	echo "qemu-system-x86_64: no such machine" >&2
	exit 1
fi
if [ ! -e "$T/port-taken" ]; then
	echo "\$port" > "$T/port-taken"
	echo "qemu-system-x86_64: Could not set up host forwarding rule 'tcp:127.0.0.1:\$port-:22'" >&2
	exit 1
fi
echo $$ > "\$pidfile"
EOF
for stub in scp git ip qemu-img sha256sum; do
	printf '#!/bin/sh\nexit 0\n' > "$T/bin/$stub"
done
chmod +x "$T"/bin/* "$T/guest.bin"
: > "$T/b4.json"

. "$LAB_DIR/guests/debian9.env"
for name in up1 up2; do
	mkdir -p "$T/home/keys" "$T/home/images" "$T/home/instances/$name"
	: > "$T/home/keys/id_ed25519"
	: > "$T/home/images/$(basename "$IMAGE_URL")"
	: > "$T/home/instances/$name/disk.qcow2"
	: > "$T/home/instances/$name/provisioned"
done

PATH="$T/bin:$PATH"
B4_LAB_HOME="$T/home"
export PATH B4_LAB_HOME

failed=0

lab() {
	sh "$LAB_DIR/lab" "$@"
}

remote() {
	side="$1"
	echo "$2" > "$T/remote/$side.status"
	shift 2
	printf '%s\n' "$@" > "$T/remote/$side.out"
}

check() {
	label="$1"
	want="$2"
	text="$3"
	shift 3
	"$@" > "$T/out" 2>&1
	got=$?
	if [ "$got" -eq "$want" ] && grep -qF -- "$text" "$T/out"; then
		echo "ok    $label"
		return 0
	fi
	failed=$((failed + 1))
	echo "FAIL  $label: exit status $got, want $want and the output to have \"$text\""
	sed 's/^/      /' "$T/out"
}

stops() {
	label="$1"
	shift
	: > "$T/guest.log"
	: > "$T/mounts-stay-shared"
	sh "$@" > "$T/out" 2>&1
	got=$?
	rm -f "$T/mounts-stay-shared"
	if [ "$got" -ne 0 ] && ! grep -q -e tmpfs -e '^ran' "$T/guest.log"; then
		echo "ok    $label"
		return 0
	fi
	failed=$((failed + 1))
	echo "FAIL  $label: exit status $got, want a failure with nothing mounted over /run and the binary not started; the guest saw"
	sed 's/^/      /' "$T/guest.log"
}

remote change 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'PASS'
check "test: a clean run passes" 0 "1 pass, 0 fail, 0 skip" lab test fake

remote change 1 \
	'=== RUN   TestNetnsParent' \
	'=== RUN   TestNetnsParent/old' \
	'=== RUN   TestNetnsParent/new' \
	'--- FAIL: TestNetnsParent (0.00s)' \
	'    --- PASS: TestNetnsParent/old (0.00s)' \
	'    --- FAIL: TestNetnsParent/new (0.00s)' \
	'FAIL'
check "test: a failing subtest is listed by its path" 1 "failed: TestNetnsParent/new" lab test fake

remote base 1 \
	'=== RUN   TestNetnsParent' \
	'=== RUN   TestNetnsParent/old' \
	'=== RUN   TestNetnsParent/new' \
	'--- FAIL: TestNetnsParent (0.00s)' \
	'    --- FAIL: TestNetnsParent/old (0.00s)' \
	'    --- PASS: TestNetnsParent/new (0.00s)' \
	'FAIL'
remote change 1 \
	'=== RUN   TestNetnsParent' \
	'=== RUN   TestNetnsParent/old' \
	'=== RUN   TestNetnsParent/new' \
	'--- FAIL: TestNetnsParent (0.00s)' \
	'    --- FAIL: TestNetnsParent/old (0.00s)' \
	'    --- FAIL: TestNetnsParent/new (0.00s)' \
	'FAIL'
check "ab: a new failing subtest under a parent that fails on the base too" 1 "    TestNetnsParent/new" lab ab fake

cp "$T/remote/change.out" "$T/remote/base.out"
check "ab: the same failures on both sides" 0 "failing on both: 3" lab ab fake

remote change 255 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'=== RUN   TestNetnsB'
check "test: a run cut off after a passing test" 2 "the run did not finish (exit status 255)" lab test fake

remote change 2 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'=== RUN   TestNetnsB' \
	'panic: boom'
check "test: a run that ends in a panic" 2 "the run did not finish (exit status 2)" lab test fake

remote change 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)'
check "test: exit status 0 without the closing PASS line" 2 "the run did not finish (exit status 0)" lab test fake

remote base 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'PASS'
remote change 255 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'=== RUN   TestNetnsB'
check "ab: the checkout's run cut off" 2 "the run of this checkout did not finish" lab ab fake

remote base 255 \
	'=== RUN   TestNetnsA'
remote change 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'PASS'
check "ab: the base's run cut off" 2 "the run of origin/main did not finish" lab ab fake

check "netns-test.sh: starts the test binary after its setup" 0 "PASS" \
	sh "$LAB_DIR/scripts/netns-test.sh" "$T/guest.bin" TestNetns 1
stops "netns-test.sh: stops when the mounts cannot be made private" \
	"$LAB_DIR/scripts/netns-test.sh" "$T/guest.bin" TestNetns 1
check "b4-netns.sh: starts the binary after its setup" 0 "PASS" \
	sh "$LAB_DIR/scripts/b4-netns.sh" "$T/guest.bin" "$T/b4.json"
stops "b4-netns.sh: stops when the mounts cannot be made private" \
	"$LAB_DIR/scripts/b4-netns.sh" "$T/guest.bin" "$T/b4.json"

check "up: moves to the next port when another instance took the one it chose" 0 "up1 is up" \
	lab up debian9 up1
if [ "$(cat "$T/home/instances/up1/port" 2>/dev/null)" = "$(cat "$T/port-taken" 2>/dev/null)" ]; then
	failed=$((failed + 1))
	echo "FAIL  up: the instance's port file names the port that was already taken"
fi

: > "$T/qemu-broken"
check "up: stops at once when qemu fails for another reason" 1 "qemu did not start up2" \
	lab up debian9 up2

[ "$failed" -eq 0 ] || {
	echo "lab selftest: $failed failed"
	exit 1
}
echo "lab selftest: all passed"
