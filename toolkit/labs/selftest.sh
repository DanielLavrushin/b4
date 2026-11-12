#!/bin/sh
set -u

unset B4_LAB_REPO LAB_BASE LAB_PKG
LAB="$(cd "$(dirname "$0")" && pwd)/lab"
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
printf '#!/bin/sh\nexit 0\n' > "$T/bin/scp"
printf '#!/bin/sh\nexit 0\n' > "$T/bin/git"
chmod +x "$T/bin/ssh" "$T/bin/go" "$T/bin/scp" "$T/bin/git"

failed=0

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
	PATH="$T/bin:$PATH" B4_LAB_HOME="$T/home" sh "$LAB" "$@" > "$T/out" 2>&1
	got=$?
	if [ "$got" -eq "$want" ] && grep -qF -- "$text" "$T/out"; then
		echo "ok    $label"
		return 0
	fi
	failed=$((failed + 1))
	echo "FAIL  $label: exit status $got, want $want and the output to have \"$text\""
	sed 's/^/      /' "$T/out"
}

remote change 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'PASS'
check "test: a clean run passes" 0 "1 pass, 0 fail, 0 skip" test fake

remote change 1 \
	'=== RUN   TestNetnsParent' \
	'=== RUN   TestNetnsParent/old' \
	'=== RUN   TestNetnsParent/new' \
	'--- FAIL: TestNetnsParent (0.00s)' \
	'    --- PASS: TestNetnsParent/old (0.00s)' \
	'    --- FAIL: TestNetnsParent/new (0.00s)' \
	'FAIL'
check "test: a failing subtest is listed by its path" 1 "failed: TestNetnsParent/new" test fake

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
check "ab: a new failing subtest under a parent that fails on the base too" 1 "    TestNetnsParent/new" ab fake

cp "$T/remote/change.out" "$T/remote/base.out"
check "ab: the same failures on both sides" 0 "failing on both: 3" ab fake

remote change 255 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'=== RUN   TestNetnsB'
check "test: a run cut off after a passing test" 2 "the run did not finish (exit status 255)" test fake

remote change 2 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'=== RUN   TestNetnsB' \
	'panic: boom'
check "test: a run that ends in a panic" 2 "the run did not finish (exit status 2)" test fake

remote change 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)'
check "test: exit status 0 without the closing PASS line" 2 "the run did not finish (exit status 0)" test fake

remote base 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'PASS'
remote change 255 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'=== RUN   TestNetnsB'
check "ab: the checkout's run cut off" 2 "the run of this checkout did not finish" ab fake

remote base 255 \
	'=== RUN   TestNetnsA'
remote change 0 \
	'=== RUN   TestNetnsA' \
	'--- PASS: TestNetnsA (0.10s)' \
	'PASS'
check "ab: the base's run cut off" 2 "the run of origin/main did not finish" ab fake

[ "$failed" -eq 0 ] || {
	echo "lab selftest: $failed failed"
	exit 1
}
echo "lab selftest: all passed"
