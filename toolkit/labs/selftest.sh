#!/bin/sh
set -u

unset B4_LAB_REPO LAB_BASE LAB_PKG
LAB_DIR="$(cd "$(dirname "$0")" && pwd)"
T="$(mktemp -d)"
: > "$T/pids"
cleanup() {
	while read -r pid; do
		kill "$pid" 2>/dev/null
	done < "$T/pids"
	rm -rf "$T"
}
trap cleanup EXIT
mkdir -p "$T/bin" "$T/remote"

cat > "$T/idle" <<'EOF'
#!/bin/sh
while :; do
	sleep 1
done
EOF
cat > "$T/bin/ssh" <<EOF
#!/bin/sh
for arg in "\$@"; do command="\$arg"; done
case "\$command" in
*change.test*) side=change ;;
*base.test*) side=base ;;
"sh -s -- "*) exec sh -c "\$command" ;;
"sh -s")
	echo attempt >> "$T/provision.log"
	[ -e "$T/provision-fails" ] && exit 1
	exit 0
	;;
*) exit 0 ;;
esac
[ -e "$T/slow" ] && sleep 1
cat "$T/remote/\$side.out"
exit "\$(cat "$T/remote/\$side.status")"
EOF
cat > "$T/bin/scp" <<EOF
#!/bin/sh
for arg in "\$@"; do
	printf '%s\n' "\$arg"
done > "$T/scp.args"
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
name=""
pidfile=""
port=""
seed=""
while [ "\$#" -gt 0 ]; do
	case "\$1" in
	-name) name="\$2" ;;
	-pidfile) pidfile="\$2" ;;
	*hostfwd=*)
		port="\${1##*127.0.0.1:}"
		port="\${port%%-*}"
		;;
	file=*seed.iso*)
		seed="\${1#file=}"
		seed="\${seed%%,*}"
		;;
	esac
	shift
done
if [ -e "$T/qemu-broken" ]; then
	echo "qemu-system-x86_64: no such machine" >&2
	exit 1
fi
if [ ! -e "\$seed" ]; then
	echo "qemu-system-x86_64: Could not open '\$seed': No such file or directory" >&2
	exit 1
fi
if [ ! -e "$T/port-taken" ]; then
	echo "\$port" > "$T/port-taken"
	echo "qemu-system-x86_64: Could not set up host forwarding rule 'tcp:127.0.0.1:\$port-:22'" >&2
	exit 1
fi
"$T/idle" -name "\$name" -end > /dev/null 2>&1 &
echo \$! > "\$pidfile"
echo \$! >> "$T/pids"
EOF
cat > "$T/bin/curl" <<'EOF'
#!/bin/sh
while [ "$#" -gt 0 ]; do
	[ "$1" = -o ] && out="$2"
	shift
done
echo part1 > "$out"
sleep 1
echo part2 >> "$out"
EOF
. "$LAB_DIR/guests/debian9.env"
cat > "$T/bin/sha256sum" <<EOF
#!/bin/sh
if [ "\$(cat "\$1")" = "part1
part2" ]; then
	echo "$IMAGE_SHA256  \$1"
else
	echo "0000  \$1"
fi
EOF
cat > "$T/bin/xorriso" <<'EOF'
#!/bin/sh
while [ "$#" -gt 0 ]; do
	[ "$1" = -output ] && : > "$2"
	shift
done
exit 0
EOF
for stub in git ip qemu-img; do
	printf '#!/bin/sh\nexit 0\n' > "$T/bin/$stub"
done
chmod +x "$T"/bin/* "$T/guest.bin" "$T/idle"
: > "$T/b4.json"

instance() {
	mkdir -p "$T/home/instances/$1"
	echo 2222 > "$T/home/instances/$1/port"
	echo root > "$T/home/instances/$1/user"
	echo debian9 > "$T/home/instances/$1/guest"
}

instance fake
"$T/idle" -name b4-lab-fake -end &
echo $! > "$T/home/instances/fake/pid"
echo $! >> "$T/pids"

mkdir -p "$T/home/keys" "$T/home/images"
: > "$T/home/keys/id_ed25519"
echo "ssh-ed25519 selftest" > "$T/home/keys/id_ed25519.pub"
: > "$T/home/images/$(basename "$IMAGE_URL")"
for name in up1 up2 up3 dl1 dl2 noseed; do
	mkdir -p "$T/home/instances/$name"
	: > "$T/home/instances/$name/disk.qcow2"
done
for name in up1 up2 dl1 dl2 noseed; do
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

fail() {
	failed=$((failed + 1))
	echo "FAIL  $*"
}

check() {
	label="$1"
	want="$2"
	text="$3"
	shift 3
	"$@" > "$T/out" 2>&1
	got=$?
	if [ "$got" -eq "$want" ] && { [ -z "$text" ] || grep -qF -- "$text" "$T/out"; }; then
		echo "ok    $label"
		return 0
	fi
	fail "$label: exit status $got, want $want and the output to have \"$text\""
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

: > "$T/guest.log"
check "netns-test.sh: starts the test binary after its setup" 0 "PASS" \
	sh "$LAB_DIR/scripts/netns-test.sh" "$T/guest.bin" TestNetns 1
grep -q "tmpfs /tmp" "$T/guest.log" || fail "netns-test.sh: the run shares the guest's /tmp, where b4 keeps the sysctl values it restores"
stops "netns-test.sh: stops when the mounts cannot be made private" \
	"$LAB_DIR/scripts/netns-test.sh" "$T/guest.bin" TestNetns 1
: > "$T/guest.log"
check "b4-netns.sh: starts the binary after its setup" 0 "PASS" \
	sh "$LAB_DIR/scripts/b4-netns.sh" "$T/guest.bin" "$T/b4.json"
grep -q "tmpfs /tmp" "$T/guest.log" || fail "b4-netns.sh: the run shares the guest's /tmp, where b4 keeps its pid file and the sysctl values it restores"
stops "b4-netns.sh: stops when the mounts cannot be made private" \
	"$LAB_DIR/scripts/b4-netns.sh" "$T/guest.bin" "$T/b4.json"

check "up: moves to the next port when another instance took the one it chose" 0 "up1 is up" \
	lab up debian9 up1
if [ "$(cat "$T/home/instances/up1/port" 2>/dev/null)" = "$(cat "$T/port-taken" 2>/dev/null)" ]; then
	failed=$((failed + 1))
	echo "FAIL  up: the instance's port file names the port that was already taken"
fi

: > "$T/provision-fails"
check "up: a failed provisioning fails the start" 1 "provisioning up3" \
	lab up debian9 up3
rm -f "$T/provision-fails"
check "up: provisioning is retried for an instance left running without it" 0 "up3 is up" \
	lab up debian9 up3
if [ ! -e "$T/home/instances/up3/provisioned" ] || [ "$(grep -c . "$T/provision.log")" -ne 2 ]; then
	fail "up: the instance was provisioned $(grep -c . "$T/provision.log") time(s) and is $([ -e "$T/home/instances/up3/provisioned" ] || echo not) marked provisioned, want 2 and marked"
fi

instance stale
"$T/idle" &
stale=$!
echo "$stale" >> "$T/pids"
echo "$stale" > "$T/home/instances/stale/pid"
check "status: a pid that is not this guest's qemu is not a running instance" 0 "stale: stopped" \
	lab status stale
check "down: leaves alone a process that only has the pid of a guest that is gone" 0 "stale is not running" \
	lab down stale
kill -0 "$stale" 2>/dev/null || fail "down: the process that had the stale pid was killed"

printf '%s\n' 'for a in "$@"; do' '	printf "[%s]\n" "$a"' 'done' > "$T/args.sh"
check "run: an argument with spaces and shell characters arrives whole" 0 '[two words; $HOME *]' \
	lab run fake "$T/args.sh" 'two words; $HOME *' plain

: > "$T/binary with space"
check "push: a source path with a space stays one path" 0 "" \
	lab push fake "$T/binary with space" /root/lab
grep -qxF "$T/binary with space" "$T/scp.args" 2>/dev/null ||
	fail "push: scp did not get the source path as one argument"

instance old8
echo debian8 > "$T/home/instances/old8/guest"
: > "$T/home/instances/old8/disk.qcow2"
: > "$T/home/instances/old8/provisioned"
check "up: an instance made from one guest is not started as another" 1 "old8 was made from the guest debian8" \
	lab up debian9 old8
[ "$(cat "$T/home/instances/old8/guest")" = debian8 ] ||
	fail "up: the instance made from debian8 is now labelled $(cat "$T/home/instances/old8/guest")"

check "ssh: an instance that is not running is not contacted" 1 "old8 is not running" \
	lab ssh old8 true

check "up: a start that never got its seed image makes it" 0 "noseed is up" \
	lab up debian9 noseed

remote change 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'PASS'
: > "$T/slow"
both=0
neither=0
for round in 1 2 3; do
	lab test fake > "$T/one.out" 2>&1 &
	one=$!
	lab test fake > "$T/two.out" 2>&1
	two=$?
	wait "$one"
	one=$?
	[ "$one" -eq 0 ] && [ "$two" -eq 0 ] && both=$((both + 1))
	[ "$one" -ne 0 ] && [ "$two" -ne 0 ] && neither=$((neither + 1))
done
rm -f "$T/slow"
if [ "$both" -eq 0 ] && [ "$neither" -eq 0 ] && grep -qF "another lab test or ab is running on fake" "$T/one.out" "$T/two.out"; then
	echo "ok    test: of two runs started together on one instance, one runs and one is refused"
else
	fail "test: two runs started together on one instance: both ran $both time(s) and neither ran $neither time(s) in 3 tries, want 0 and 0"
	sed 's/^/      /' "$T/one.out" "$T/two.out"
fi
check "test: an instance is free again once its run has ended" 0 "1 pass, 0 fail, 0 skip" \
	lab test fake

mkdir -p "$T/home/victim"
: > "$T/home/victim/keep"
check "destroy: a name that leads out of the instance folder is refused" 1 "is not a name" \
	lab destroy ../victim
[ -e "$T/home/victim/keep" ] || fail "destroy: a folder outside the instances was deleted"
check "up: a guest name that leads out of the guest folder is refused" 1 "is not a name" \
	lab up ../guests/debian9 elsewhere

rm -f "$T/home/images/$(basename "$IMAGE_URL")"
lab up debian9 dl1 > "$T/dl1.out" 2>&1 &
first=$!
lab up debian9 dl2 > "$T/dl2.out" 2>&1
second=$?
wait "$first"
first=$?
if [ "$first" -eq 0 ] && [ "$second" -eq 0 ]; then
	echo "ok    up: two first starts at once each get a whole image"
else
	fail "up: two first starts at once ended with exit status $first and $second, want 0 and 0"
	sed 's/^/      /' "$T/dl1.out" "$T/dl2.out"
fi

: > "$T/qemu-broken"
check "up: stops at once when qemu fails for another reason" 1 "qemu did not start up2" \
	lab up debian9 up2

[ "$failed" -eq 0 ] || {
	echo "lab selftest: $failed failed"
	exit 1
}
echo "lab selftest: all passed"
