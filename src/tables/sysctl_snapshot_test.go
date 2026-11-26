package tables

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const testSnapName = "b4_sysctl_snapshot.4026531992.json"

const (
	testChecksum = "net.netfilter.nf_conntrack_checksum"
	testLiberal  = "net.netfilter.nf_conntrack_tcp_be_liberal"
)

func useSnapshotDirs(t *testing.T, runDirs []string, private, legacy string) {
	t.Helper()
	prevPath, prevRun, prevPrivate, prevLegacy := sysctlSnapPath, sysctlSnapRunDirs, sysctlSnapPrivateDir, sysctlSnapLegacyDir
	prevOwner, prevLink, prevInit := sysctlSnapOwnerOK, readNetnsLink, readInitNetnsLink
	sysctlSnapPath, sysctlSnapRunDirs, sysctlSnapPrivateDir, sysctlSnapLegacyDir = "", runDirs, private, legacy
	readNetnsLink = func() (string, error) { return "net:[4026531992]", nil }
	readInitNetnsLink = func() (string, error) { return "net:[4026531992]", nil }
	forgetSysctlSnapshotState()
	t.Cleanup(func() {
		sysctlSnapPath, sysctlSnapRunDirs, sysctlSnapPrivateDir, sysctlSnapLegacyDir = prevPath, prevRun, prevPrivate, prevLegacy
		sysctlSnapOwnerOK, readNetnsLink, readInitNetnsLink = prevOwner, prevLink, prevInit
		forgetSysctlSnapshotState()
	})
}

func sharedTmp(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tmp")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o1777); err != nil {
		t.Fatal(err)
	}
	return dir
}

func privateRunDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeSnapshotFile(t *testing.T, path string, m map[string]string, mode os.FileMode) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readSnapshotJSON(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func requireGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s is still there (err %v)", path, err)
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func releaseFIFO(path string) func() {
	return func() {
		if f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	}
}

var finishesInTimeLimit = 10 * time.Second

func finishesInTime(t *testing.T, what string, release func(), fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return
	case <-time.After(finishesInTimeLimit):
	}
	giveUp := time.After(2 * time.Second)
	for {
		release()
		select {
		case <-done:
			t.Fatalf("%s was still blocked after %v", what, finishesInTimeLimit)
		case <-giveUp:
			panic(fmt.Sprintf("%s was still blocked after %v, and releasing it did not help", what, finishesInTimeLimit))
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestFinishesInTimeGivesUpOnAGoroutineItCannotRelease(t *testing.T) {
	if os.Getenv("B4_TEST_STUCK_HELPER") == "1" {
		finishesInTimeLimit = 100 * time.Millisecond
		finishesInTime(t, "a goroutine stuck on a channel", func() {}, func() { select {} })
		return
	}
	var out bytes.Buffer
	cmd := exec.Command(os.Args[0], "-test.run=^TestFinishesInTimeGivesUpOnAGoroutineItCannotRelease$", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), "B4_TEST_STUCK_HELPER=1", "TMPDIR="+t.TempDir())
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("the helper passed with a goroutine stuck for good:\n%s", out.String())
		}
		if !strings.Contains(out.String(), "still blocked") {
			t.Fatalf("the helper failed without saying what was stuck:\n%s", out.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("the helper was still waiting 30s after its deadline, so a stuck test hangs until go test's own timeout")
	}
}

type sysctlStub struct {
	cur   map[string]string
	wrote []string
}

func stubConntrackSysctls(t *testing.T, cur map[string]string) *sysctlStub {
	t.Helper()
	forgetConntrackStrictWarning()
	s := &sysctlStub{cur: cur}
	prevGet, prevSet, prevRun := getSysctl, setSysctl, run
	getSysctl = func(name string) string { return s.cur[name] }
	setSysctl = func(name, val string) {
		s.cur[name] = val
		s.wrote = append(s.wrote, name+"="+val)
	}
	run = func(args ...string) (string, error) {
		return "", errors.New("no commands in this test")
	}
	t.Cleanup(func() {
		getSysctl, setSysctl, run = prevGet, prevSet, prevRun
		forgetConntrackStrictWarning()
	})
	return s
}

func loadSysctlSnapshot() map[string]string {
	m, _ := readSysctlSnapshot()
	return m
}

func TestSysctlSnapshotLeavesTheSysctlsAloneWhenItCannotReadTheSnapshot(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0", "net.netfilter.nf_conntrack_acct": "0"})
	applyAll()
	collectedAccounting.Apply()
	path := filepath.Join(run, testSnapName)
	saved := readSnapshotJSON(t, path)
	prev := sysctlSnapOpenat
	t.Cleanup(func() { sysctlSnapOpenat = prev })
	failing := true
	sysctlSnapOpenat = func(dirfd int, name string, flags int, mode uint32) (int, error) {
		if failing && name == testSnapName {
			return -1, syscall.EMFILE
		}
		return prev(dirfd, name, flags, mode)
	}

	s.wrote = nil
	applyAll()
	collectedAccounting.Apply()
	revertAll()
	revertCollectedAccounting()
	if len(s.wrote) != 0 {
		t.Fatalf("with the saved settings unreadable for a moment, b4 wrote %v", s.wrote)
	}
	if got := readSnapshotJSON(t, path); len(got) != len(saved) || got[testChecksum] != "1" || got[testLiberal] != "0" || got["net.netfilter.nf_conntrack_acct"] != "0" {
		t.Fatalf("the saved settings went from %v to %v while they could not be read", saved, got)
	}

	failing = false
	revertAll()
	revertCollectedAccounting()
	want := testChecksum + "=1," + testLiberal + "=0,net.netfilter.nf_conntrack_acct=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("once readable again, RevertBack wrote %q, want the originals %q", got, want)
	}
}

func dirInode(t *testing.T, dir string) uint64 {
	t.Helper()
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Sys().(*syscall.Stat_t).Ino
}

func fdIsDir(fd int, ino uint64) bool {
	var st syscall.Stat_t
	return syscall.Fstat(fd, &st) == nil && st.Ino == ino
}

func TestSysctlSnapshotLeavesTheSysctlsAloneWhileAnEarlierFileCannotBeRead(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	writeSnapshotFile(t, filepath.Join(tmp, testSnapName), map[string]string{testChecksum: "0", testLiberal: "1"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})
	tmpIno := dirInode(t, tmp)
	prev := sysctlSnapOpenat
	t.Cleanup(func() { sysctlSnapOpenat = prev })
	failing := true
	sysctlSnapOpenat = func(dirfd int, name string, flags int, mode uint32) (int, error) {
		if failing && name == "b4_sysctl_snapshot.json" && fdIsDir(dirfd, tmpIno) {
			return -1, syscall.EMFILE
		}
		return prev(dirfd, name, flags, mode)
	}

	revertAll()
	applyAll()
	if len(s.wrote) != 0 {
		t.Fatalf("while the 1.92 file could not be read, b4 wrote %v from what it could read", s.wrote)
	}
	failing = false
	applyAll()
	s.wrote = nil
	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("stop wrote %q, want the originals %q from the 1.92 file", got, want)
	}
}

func TestSysctlSnapshotLeavesTheSysctlsAloneWhileItsDirectoryCannotBeOpened(t *testing.T) {
	for _, private := range []bool{false, true} {
		name := "run directory"
		if private {
			name = "directory in tmp"
		}
		t.Run(name, func(t *testing.T) {
			run := privateRunDir(t)
			tmp := sharedTmp(t)
			privateDir := filepath.Join(tmp, "b4-run")
			dir := run
			runDirs := []string{run}
			if private {
				if err := os.Mkdir(privateDir, 0o700); err != nil {
					t.Fatal(err)
				}
				dir = privateDir
				runDirs = []string{filepath.Join(t.TempDir(), "missing")}
			}
			useSnapshotDirs(t, runDirs, privateDir, tmp)
			writeSnapshotFile(t, filepath.Join(dir, testSnapName), map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
			s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})
			prev := sysctlSnapOpen
			t.Cleanup(func() { sysctlSnapOpen = prev })
			failures := 1
			sysctlSnapOpen = func(path string, flags int, mode uint32) (int, error) {
				if path == dir && failures > 0 {
					failures--
					return -1, syscall.EMFILE
				}
				return prev(path, flags, mode)
			}

			applyAll()
			s.wrote = nil
			revertAll()
			want := testChecksum + "=1," + testLiberal + "=0"
			if got := strings.Join(s.wrote, ","); got != want {
				t.Fatalf("after %s could not be opened once, stop wrote %q, want the originals %q", dir, got, want)
			}
		})
	}
}

func TestSysctlSnapshotLeavesTheSysctlsAloneWhenItsDirectoryCannotBeReachedLater(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})
	revertAll()
	applyAll()
	prev := sysctlSnapDup
	t.Cleanup(func() { sysctlSnapDup = prev })
	failures := 1
	sysctlSnapDup = func(fd int) (int, error) {
		if failures > 0 {
			failures--
			return -1, syscall.EMFILE
		}
		return prev(fd)
	}

	applyAll()
	s.wrote = nil
	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("after the snapshot's directory could not be reached once during a rules pass, stop wrote %q, want the originals %q", got, want)
	}
}

func TestSysctlSnapshotFinishesTheTakeoverPastAnEntryItCannotRemove(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	older := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	writeSnapshotFile(t, older, map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	planted := filepath.Join(tmp, testSnapName)
	if err := os.Mkdir(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("start wrote %q, want the originals %q", got, want)
	}
	requireGone(t, older)
	applyAll()
	revertAll()
	if names := dirEntries(t, run); len(names) != 0 {
		t.Fatalf("after a clean stop %s holds %v; an entry b4 could not remove kept the takeover open", run, names)
	}
	s.cur[testChecksum], s.cur[testLiberal] = "0", "1"
	forgetSysctlSnapshotState()
	s.wrote = nil
	revertAll()
	if len(s.wrote) != 0 {
		t.Fatalf("the next start wrote %v over values set while b4 was stopped", s.wrote)
	}
}

func TestSysctlSnapshotRefusesAFileOfAnotherUserBeforeOpeningIt(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	planted := filepath.Join(tmp, testSnapName)
	writeSnapshotFile(t, planted, map[string]string{testChecksum: "7"}, 0o664)
	f, err := os.OpenFile(planted, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := unix.FcntlInt(f.Fd(), unix.F_SETLEASE, unix.F_WRLCK); err != nil {
		t.Skipf("cannot take a write lease here: %v", err)
	}
	t.Cleanup(func() { _, _ = unix.FcntlInt(f.Fd(), unix.F_SETLEASE, unix.F_UNLCK) })
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("with a leased file of another user at the 1.93 name, start wrote %q, want the originals %q", got, want)
	}
}

func TestSysctlSnapshotTellsTheInitialNamespaceApartOrTakesTheUnkeyedFile(t *testing.T) {
	cases := []struct {
		name string
		errs []error
	}{
		{name: "a passing error, then an answer", errs: []error{syscall.ENOMEM}},
		{name: "no way to tell", errs: []error{syscall.EACCES, syscall.EACCES, syscall.EACCES, syscall.EACCES, syscall.EACCES, syscall.EACCES}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := privateRunDir(t)
			tmp := sharedTmp(t)
			useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
			errs := c.errs
			readInitNetnsLink = func() (string, error) {
				if len(errs) > 0 {
					err := errs[0]
					errs = errs[1:]
					return "", &os.PathError{Op: "readlink", Path: "/proc/1/ns/net", Err: err}
				}
				return "net:[4026531992]", nil
			}
			writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
			writeSnapshotFile(t, filepath.Join(tmp, testSnapName), map[string]string{testChecksum: "0", testLiberal: "1"}, 0o600)
			s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

			revertAll()
			applyAll()
			s.wrote = nil
			revertAll()
			want := testChecksum + "=1," + testLiberal + "=0"
			if got := strings.Join(s.wrote, ","); got != want {
				t.Fatalf("stop wrote %q, want the originals %q from the 1.92 file", got, want)
			}
		})
	}
}

func applyAll() {
	for _, setting := range b4SysctlSettings() {
		setting.Apply()
	}
}

func revertAll() {
	for _, setting := range b4SysctlSettings() {
		setting.RevertBack()
	}
}

func TestSysctlSnapshotGoesToTheFirstRunDirectoryOnlyB4CanWrite(t *testing.T) {
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	group := t.TempDir()
	if err := os.Chmod(group, 0o775); err != nil {
		t.Fatal(err)
	}
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	run := privateRunDir(t)
	later := privateRunDir(t)
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	candidates := []string{missing, shared, group, run, later}
	if os.Geteuid() != 0 {
		candidates = []string{missing, shared, group, readOnly, run, later}
	}
	useSnapshotDirs(t, candidates, private, tmp)

	if err := saveSysctlSnapshot(map[string]string{testChecksum: "1"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := filepath.Join(run, testSnapName)
	if got := readSnapshotJSON(t, path); got[testChecksum] != "1" {
		t.Fatalf("snapshot in %s = %v, want checksum=1", run, got)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode %04o, want 0600", fi.Mode().Perm())
	}
	for _, dir := range []string{shared, group, readOnly, later} {
		if names := dirEntries(t, dir); len(names) != 0 {
			t.Fatalf("%s got %v; the snapshot belongs in the first run directory only b4 can write", dir, names)
		}
	}
	requireGone(t, private)
	if names := dirEntries(t, run); !slices.Equal(names, []string{testSnapName}) {
		t.Fatalf("run directory holds %v, want only %s: a temporary file was left behind", names, testSnapName)
	}
	if got := loadSysctlSnapshot()[testChecksum]; got != "1" {
		t.Fatalf("load gave checksum %q, want 1", got)
	}
}

func TestSysctlSnapshotFallsBackToADirectoryOfItsOwnInTmp(t *testing.T) {
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	useSnapshotDirs(t, []string{filepath.Join(t.TempDir(), "no-var-run"), filepath.Join(t.TempDir(), "no-run")}, private, tmp)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "1"}, 0o600)

	m := loadSysctlSnapshot()
	m[testLiberal] = "0"
	if err := saveSysctlSnapshot(m); err != nil {
		t.Fatalf("save: %v", err)
	}
	fi, err := os.Lstat(private)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("%s: mode %v, want a directory with mode 0700", private, fi.Mode())
	}
	if got := readSnapshotJSON(t, filepath.Join(private, testSnapName)); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("snapshot = %v, want checksum=1 from the older file and liberal=0", got)
	}
	if names := dirEntries(t, tmp); !slices.Equal(names, []string{"b4-run"}) {
		t.Fatalf("shared tmp holds %v, want only b4-run", names)
	}
}

func TestSysctlSnapshotRefusesAPrivateDirectorySomeoneElsePrepared(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, private string) func()
		want    string
	}{
		{
			name: "a symbolic link to a directory b4 would trust",
			prepare: func(t *testing.T, private string) func() {
				if err := os.Symlink(privateRunDir(t), private); err != nil {
					t.Fatal(err)
				}
				return func() {}
			},
			want: "symbolic link",
		},
		{
			name: "a directory other users can write",
			prepare: func(t *testing.T, private string) func() {
				if err := os.Mkdir(private, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(private, 0o777); err != nil {
					t.Fatal(err)
				}
				return func() {}
			},
			want: "can be written by other users",
		},
		{
			name: "a directory of another user",
			prepare: func(t *testing.T, private string) func() {
				if err := os.Mkdir(private, 0o700); err != nil {
					t.Fatal(err)
				}
				sysctlSnapOwnerOK = func(uint32) bool { return false }
				return func() {}
			},
			want: "belongs to uid",
		},
		{
			name: "a plain file",
			prepare: func(t *testing.T, private string) func() {
				writeSnapshotFile(t, private, map[string]string{}, 0o600)
				return func() {}
			},
			want: "not a directory",
		},
		{
			name: "a FIFO",
			prepare: func(t *testing.T, private string) func() {
				if err := syscall.Mkfifo(private, 0o600); err != nil {
					t.Skipf("no FIFO here: %v", err)
				}
				return releaseFIFO(private)
			},
			want: "not a directory",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmp := sharedTmp(t)
			private := filepath.Join(tmp, "b4-run")
			useSnapshotDirs(t, []string{filepath.Join(t.TempDir(), "missing")}, private, tmp)
			release := c.prepare(t, private)
			s := stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})

			var err error
			finishesInTime(t, "saving next to "+c.name, release, func() {
				err = saveSysctlSnapshot(map[string]string{testChecksum: "1"})
				applyAll()
			})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("save into %s: err %v, want one that says %q", c.name, err, c.want)
			}
			if len(s.wrote) != 0 {
				t.Fatalf("Apply wrote %v with nowhere safe to save the old values", s.wrote)
			}
			if target, err := os.Readlink(private); err == nil {
				if names := dirEntries(t, target); len(names) != 0 {
					t.Fatalf("b4 followed the link into %s and wrote %v", target, names)
				}
			}
		})
	}
}

func TestSysctlSnapshotIgnoresAFileOtherUsersCanWrite(t *testing.T) {
	buf := captureTablesLog(t)
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	path := filepath.Join(run, testSnapName)
	writeSnapshotFile(t, path, map[string]string{testChecksum: "7", testLiberal: "7"}, 0o666)

	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})
	revertAll()
	if len(s.wrote) != 0 {
		t.Fatalf("RevertBack wrote %v from a file other users can write", s.wrote)
	}
	if got := loadSysctlSnapshot(); len(got) != 0 {
		t.Fatalf("load gave %v from a file other users can write", got)
	}
	if n := logLinesWith(buf.String(), "ignoring the saved conntrack settings", "can be written by other users"); n != 1 {
		t.Fatalf("%d warnings about the file, want 1:\n%s", n, buf.String())
	}
}

func TestSysctlSnapshotKeepsItsOwnFileWhenSomeoneLinksIt(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})

	applyAll()
	if err := os.Link(filepath.Join(run, testSnapName), filepath.Join(t.TempDir(), "elsewhere")); err != nil {
		t.Skipf("cannot hard link here: %v", err)
	}
	applyAll()
	s.wrote = nil
	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("RevertBack wrote %q after a second link to b4's own file appeared, want the originals %q", got, want)
	}
}

func TestSysctlSnapshotTakesOverWhatAnOlderB4LeftInTmp(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	unkeyed := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	keyed := filepath.Join(tmp, testSnapName)
	writeSnapshotFile(t, unkeyed, map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	writeSnapshotFile(t, keyed, map[string]string{testChecksum: "0", testLiberal: "1"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("RevertBack wrote %q, want the originals the crashed 1.92 saved: %q", got, want)
	}
	requireGone(t, unkeyed)
	requireGone(t, keyed)
	requireGone(t, filepath.Join(run, testSnapName))

	applyAll()
	if got := readSnapshotJSON(t, filepath.Join(run, testSnapName)); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("snapshot after Apply = %v, want the originals checksum=1 liberal=0", got)
	}
}

func TestSysctlSnapshotTakesOverTheOldFileOnAKernelWithoutNetworkNamespaces(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	readNetnsLink = func() (string, error) { return "", syscall.ENOENT }
	older := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	writeSnapshotFile(t, older, map[string]string{testLiberal: "0"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "1"})

	revertAll()
	if got := strings.Join(s.wrote, ","); got != testLiberal+"=0" {
		t.Fatalf("RevertBack wrote %q, want the original %s=0 from %s", got, testLiberal, older)
	}
	requireGone(t, older)
	applyAll()
	path := filepath.Join(run, "b4_sysctl_snapshot.json")
	if got := readSnapshotJSON(t, path); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("%s = %v, want checksum=1 liberal=0", path, got)
	}
	if names := dirEntries(t, tmp); len(names) != 0 {
		t.Fatalf("shared tmp holds %v", names)
	}
}

func TestSysctlSnapshotLeavesTheUnkeyedFileToTheInitialNamespace(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	readInitNetnsLink = func() (string, error) { return "net:[4026531840]", nil }
	hosts := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	writeSnapshotFile(t, hosts, map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	own := filepath.Join(tmp, testSnapName)
	writeSnapshotFile(t, own, map[string]string{testLiberal: "0"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

	revertAll()
	if got := strings.Join(s.wrote, ","); got != testLiberal+"=0" {
		t.Fatalf("a b4 outside the initial network namespace wrote %q, want only its own %s=0", got, testLiberal)
	}
	applyAll()
	revertAll()
	if got := readSnapshotJSON(t, hosts); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("%s = %v: another namespace took the file of the initial one", hosts, got)
	}
	requireGone(t, own)
}

func TestSysctlSnapshotKeepsItsOwnValuesOverOlderOnes(t *testing.T) {
	buf := captureTablesLog(t)
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(run, testSnapName), map[string]string{testChecksum: "1"}, 0o600)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "0", testLiberal: "0", "net.netfilter.nf_conntrack_acct": "x"}, 0o600)

	got := loadSysctlSnapshot()
	if got[testChecksum] != "1" || got[testLiberal] != "0" || len(got) != 2 {
		t.Fatalf("snapshot = %v, want checksum=1 kept and liberal=0 taken over, and nothing that is not a number", got)
	}
	if err := saveSysctlSnapshot(got); err != nil {
		t.Fatal(err)
	}
	if n := logLinesWith(buf.String(), "moved the conntrack settings", testLiberal+"=0"); n != 1 {
		t.Fatalf("%d lines about the takeover, want 1:\n%s", n, buf.String())
	}

	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{"net.netfilter.nf_conntrack_acct": "0"}, 0o600)
	if got := loadSysctlSnapshot(); len(got) != 2 {
		t.Fatalf("a second look took over again: %v", got)
	}
}

func TestSysctlSnapshotTakesNothingFromAFileItCannotTrustInTmp(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, path string) func()
		want    string
	}{
		{
			name: "writable by other users",
			prepare: func(t *testing.T, path string) func() {
				writeSnapshotFile(t, path, map[string]string{testChecksum: "7", testLiberal: "7"}, 0o666)
				return func() {}
			},
			want: "can be written by other users",
		},
		{
			name: "hard linked",
			prepare: func(t *testing.T, path string) func() {
				writeSnapshotFile(t, path, map[string]string{testChecksum: "7", testLiberal: "7"}, 0o600)
				if err := os.Link(path, filepath.Join(t.TempDir(), "elsewhere")); err != nil {
					t.Skipf("cannot hard link here: %v", err)
				}
				return func() {}
			},
			want: "hard links",
		},
		{
			name: "a symbolic link",
			prepare: func(t *testing.T, path string) func() {
				target := filepath.Join(t.TempDir(), "target.json")
				writeSnapshotFile(t, target, map[string]string{testChecksum: "7", testLiberal: "7"}, 0o600)
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := os.Stat(target); err != nil {
						t.Errorf("the link's target %s is gone: %v", target, err)
					}
				})
				return func() {}
			},
			want: "not a regular file",
		},
		{
			name: "a FIFO",
			prepare: func(t *testing.T, path string) func() {
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Skipf("no FIFO here: %v", err)
				}
				return releaseFIFO(path)
			},
			want: "not a regular file",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := captureTablesLog(t)
			run := privateRunDir(t)
			tmp := sharedTmp(t)
			useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
			path := filepath.Join(tmp, "b4_sysctl_snapshot.json")
			release := c.prepare(t, path)
			s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

			finishesInTime(t, "RevertBack next to "+c.name, release, revertAll)
			if len(s.wrote) != 0 {
				t.Fatalf("RevertBack wrote %v from a file that is %s", s.wrote, c.name)
			}
			if n := logLinesWith(buf.String(), "ignoring the saved conntrack settings in "+path, c.want); n != 1 {
				t.Fatalf("%d warnings naming %s, want 1:\n%s", n, path, buf.String())
			}
			finishesInTime(t, "Apply next to "+c.name, release, applyAll)
			requireGone(t, path)
		})
	}
}

func TestSysctlSnapshotKeepsTheOriginalsUntilItCanSaveThem(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	blocker := filepath.Join(run, testSnapName)
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	writeSnapshotFile(t, older, map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("start with a snapshot b4 cannot write: RevertBack wrote %q, want the originals %q", got, want)
	}
	applyAll()
	if got := readSnapshotJSON(t, older); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("%s = %v after saves that failed; the only copy of the originals is gone", older, got)
	}

	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	applyAll()
	if got := readSnapshotJSON(t, blocker); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("once saving worked again, the snapshot holds %v, want the originals moved in at once", got)
	}
	requireGone(t, older)

	forgetSysctlSnapshotState()
	s.wrote = nil
	revertAll()
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("a restart after a crash wrote %q, want the originals %q", got, want)
	}
}

func TestSysctlSnapshotNamesOnlyWhatTheFileHolds(t *testing.T) {
	buf := captureTablesLog(t)
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)

	loadSysctlSnapshot()
	path := filepath.Join(run, testSnapName)
	if n := logLinesWith(buf.String(), "moved the conntrack settings", path, testChecksum+"=1", testLiberal+"=0"); n != 1 {
		t.Fatalf("%d lines naming what moved into %s, want 1:\n%s", n, path, buf.String())
	}
	if got := readSnapshotJSON(t, path); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("%s holds %v right after the line that names checksum=1 and liberal=0", path, got)
	}
}

func TestSysctlSnapshotStaysInTheDirectoryItChoseFirst(t *testing.T) {
	later := filepath.Join(t.TempDir(), "var-run")
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	useSnapshotDirs(t, []string{later}, private, tmp)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})

	revertAll()
	applyAll()
	if err := os.Mkdir(later, 0o755); err != nil {
		t.Fatal(err)
	}
	applyAll()
	if names := dirEntries(t, later); len(names) != 0 {
		t.Fatalf("%s got %v in the middle of a run; the saved values stay where b4 put them first", later, names)
	}
	s.wrote = nil
	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("stop wrote %q after a better directory appeared, want the originals %q", got, want)
	}

	if err := os.Remove(private); err != nil {
		t.Fatal(err)
	}
	applyAll()
	if got := readSnapshotJSON(t, filepath.Join(later, testSnapName)); got[testChecksum] != "1" {
		t.Fatalf("after its directory was removed, b4 saved %v, want checksum=1 in %s", got, later)
	}
}

func TestSysctlSnapshotDoesNotMoveOnAfterAPassingError(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	useSnapshotDirs(t, []string{run}, private, tmp)
	prev := sysctlSnapOpen
	t.Cleanup(func() { sysctlSnapOpen = prev })
	sysctlSnapOpen = func(path string, flags int, mode uint32) (int, error) {
		if path == run {
			return -1, syscall.EMFILE
		}
		return prev(path, flags, mode)
	}

	if err := saveSysctlSnapshot(map[string]string{testChecksum: "1"}); err == nil {
		t.Fatal("a save went through while the run directory could not be opened")
	}
	requireGone(t, private)
	sysctlSnapOpen = prev
	if err := saveSysctlSnapshot(map[string]string{testChecksum: "1"}); err != nil {
		t.Fatal(err)
	}
	if got := readSnapshotJSON(t, filepath.Join(run, testSnapName)); got[testChecksum] != "1" {
		t.Fatalf("snapshot = %v", got)
	}
	requireGone(t, private)
}

func TestSysctlSnapshotRemovesOnlyTheOldFilesItRead(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(run, testSnapName), map[string]string{testLiberal: "0"}, 0o600)
	older := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	writeSnapshotFile(t, older, map[string]string{testChecksum: "1"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})
	prev := sysctlSnapOpen
	t.Cleanup(func() { sysctlSnapOpen = prev })
	failures := 2
	sysctlSnapOpen = func(path string, flags int, mode uint32) (int, error) {
		if path == tmp && failures > 0 {
			failures--
			return -1, syscall.EMFILE
		}
		return prev(path, flags, mode)
	}

	revertAll()
	if got := readSnapshotJSON(t, older); got[testChecksum] != "1" {
		t.Fatalf("%s = %v after b4 could not open its directory", older, got)
	}
	applyAll()
	s.wrote = nil
	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("stop wrote %q, want the originals %q", got, want)
	}
}

func TestSysctlSnapshotKeepsTheOlderFilesWhenOneCannotBeRemoved(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	writeSnapshotFile(t, filepath.Join(tmp, testSnapName), map[string]string{testChecksum: "0", testLiberal: "1"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})
	var tmpIno uint64
	if fi, err := os.Stat(tmp); err == nil {
		tmpIno = fi.Sys().(*syscall.Stat_t).Ino
	}
	prev := sysctlSnapUnlink
	t.Cleanup(func() { sysctlSnapUnlink = prev })
	sysctlSnapUnlink = func(dirfd int, name string) error {
		var st syscall.Stat_t
		if name == testSnapName && syscall.Fstat(dirfd, &st) == nil && st.Ino == tmpIno {
			return syscall.EPERM
		}
		return prev(dirfd, name)
	}

	revertAll()
	applyAll()
	revertAll()
	forgetSysctlSnapshotState()
	s.wrote = nil
	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("the next start wrote %q while the 1.93 file could not be removed, want the originals %q", got, want)
	}
}

func TestSysctlSnapshotLeavesAnOldFileThatChangedAfterItWasRead(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	unkeyed := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	writeSnapshotFile(t, unkeyed, map[string]string{testChecksum: "1"}, 0o600)
	writeSnapshotFile(t, filepath.Join(tmp, testSnapName), map[string]string{testLiberal: "0"}, 0o600)
	prev := sysctlSnapUnlink
	t.Cleanup(func() { sysctlSnapUnlink = prev })
	swapped := false
	sysctlSnapUnlink = func(dirfd int, name string) error {
		if !swapped && name == testSnapName {
			swapped = true
			if err := os.Remove(unkeyed); err != nil {
				t.Error(err)
			}
			writeSnapshotFile(t, unkeyed, map[string]string{testChecksum: "1", "net.netfilter.nf_conntrack_acct": "0"}, 0o600)
		}
		return prev(dirfd, name)
	}

	loadSysctlSnapshot()
	if got := readSnapshotJSON(t, unkeyed); got["net.netfilter.nf_conntrack_acct"] != "0" {
		t.Fatalf("%s = %v: b4 removed a file that appeared after it read the old one", unkeyed, got)
	}
	sysctlSnapUnlink = prev
	got := loadSysctlSnapshot()
	if got["net.netfilter.nf_conntrack_acct"] != "0" || got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("snapshot = %v, want the new file taken over on the next look", got)
	}
	requireGone(t, unkeyed)
}

func TestSysctlSnapshotPutsBackWhatAnOlderB4LeftWhenItHasNowhereToSave(t *testing.T) {
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	if err := os.Symlink(t.TempDir(), private); err != nil {
		t.Fatal(err)
	}
	useSnapshotDirs(t, []string{filepath.Join(t.TempDir(), "missing")}, private, tmp)
	older := filepath.Join(tmp, testSnapName)
	writeSnapshotFile(t, older, map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})

	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("with nowhere safe to save, RevertBack wrote %q, want the originals %q from %s", got, want, older)
	}
	if got := readSnapshotJSON(t, older); len(got) != 2 {
		t.Fatalf("%s = %v; with nowhere to move them b4 must leave the older values where they are", older, got)
	}
}

func TestSysctlSnapshotKeepsTheOriginalsWhenRemovingOldFilesStopsHalfway(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	writeSnapshotFile(t, filepath.Join(tmp, testSnapName), map[string]string{testChecksum: "0", testLiberal: "1"}, 0o600)
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})
	prev := sysctlSnapUnlink
	t.Cleanup(func() { sysctlSnapUnlink = prev })
	allowed := 1
	sysctlSnapUnlink = func(dirfd int, name string) error {
		if allowed == 0 {
			return syscall.EIO
		}
		allowed--
		return prev(dirfd, name)
	}

	b4SysctlSettings()[0].RevertBack()
	sysctlSnapUnlink = prev
	forgetSysctlSnapshotState()
	s.wrote = nil
	revertAll()
	want := testChecksum + "=1," + testLiberal + "=0"
	if got := strings.Join(s.wrote, ","); got != want {
		t.Fatalf("after b4 died between removing the two old files, a restart wrote %q, want the originals %q", got, want)
	}
}

func TestSysctlSnapshotTakesOverFromEveryOtherPlaceInOrder(t *testing.T) {
	run := privateRunDir(t)
	other := privateRunDir(t)
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	useSnapshotDirs(t, []string{run, other}, private, tmp)
	writeSnapshotFile(t, filepath.Join(run, testSnapName), map[string]string{"a": "1"}, 0o600)
	writeSnapshotFile(t, filepath.Join(other, testSnapName), map[string]string{"a": "2", "b": "2"}, 0o600)
	writeSnapshotFile(t, filepath.Join(private, testSnapName), map[string]string{"a": "3", "b": "3", "c": "3"}, 0o600)
	writeSnapshotFile(t, filepath.Join(tmp, "b4_sysctl_snapshot.json"), map[string]string{"a": "4", "b": "4", "c": "4", "d": "4"}, 0o600)
	writeSnapshotFile(t, filepath.Join(tmp, testSnapName), map[string]string{"a": "5", "b": "5", "c": "5", "d": "5", "e": "5"}, 0o600)

	got := loadSysctlSnapshot()
	want := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
	if len(got) != len(want) {
		t.Fatalf("snapshot = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("snapshot = %v, want %v: the order is b4's own file, the other run directories, its directory in tmp, then the names in tmp before and after 1.93", got, want)
		}
	}
	if err := saveSysctlSnapshot(got); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(other, testSnapName), filepath.Join(private, testSnapName), filepath.Join(tmp, "b4_sysctl_snapshot.json"), filepath.Join(tmp, testSnapName)} {
		requireGone(t, path)
	}
	if got := readSnapshotJSON(t, filepath.Join(run, testSnapName)); len(got) != len(want) {
		t.Fatalf("saved snapshot = %v, want %v", got, want)
	}
}

func TestSysctlSnapshotDoesNotFollowALinkAtItsDirectoryInTmp(t *testing.T) {
	run := privateRunDir(t)
	elsewhere := privateRunDir(t)
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	if err := os.Symlink(elsewhere, private); err != nil {
		t.Fatal(err)
	}
	useSnapshotDirs(t, []string{run}, private, tmp)
	target := filepath.Join(elsewhere, testSnapName)
	writeSnapshotFile(t, target, map[string]string{"a": "9"}, 0o600)

	got := loadSysctlSnapshot()
	if _, took := got["a"]; took {
		t.Fatalf("snapshot = %v: b4 followed the link at %s", got, private)
	}
	got["b"] = "1"
	if err := saveSysctlSnapshot(got); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("b4 removed %s behind the link: %v", target, err)
	}
}

func TestSysctlSnapshotIsNotHeldUpByAFIFOAtItsDirectoryInTmp(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	private := filepath.Join(tmp, "b4-run")
	if err := syscall.Mkfifo(private, 0o600); err != nil {
		t.Skipf("no FIFO here: %v", err)
	}
	useSnapshotDirs(t, []string{run}, private, tmp)

	finishesInTime(t, "a load and a save", releaseFIFO(private), func() {
		m := loadSysctlSnapshot()
		m[testChecksum] = "1"
		if err := saveSysctlSnapshot(m); err != nil {
			t.Errorf("save: %v", err)
		}
	})
	if got := readSnapshotJSON(t, filepath.Join(run, testSnapName)); got[testChecksum] != "1" {
		t.Fatalf("snapshot = %v", got)
	}
}

func TestSysctlSnapshotRemovesATemporaryFileLeftByADeadB4(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	stale := filepath.Join(run, testSnapName+".k3j9")
	writeSnapshotFile(t, stale, map[string]string{testChecksum: "1"}, 0o600)
	foreign := filepath.Join(run, "b4_sysctl_snapshot.1.json.k3j9")
	writeSnapshotFile(t, foreign, map[string]string{testChecksum: "1"}, 0o600)

	loadSysctlSnapshot()
	requireGone(t, stale)
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("another namespace's file %s was removed: %v", foreign, err)
	}
}

func TestSysctlSnapshotSurvivesARunDirectoryThatIsALinkToAnother(t *testing.T) {
	run := privateRunDir(t)
	varRun := filepath.Join(t.TempDir(), "var-run")
	if err := os.Symlink(run, varRun); err != nil {
		t.Fatal(err)
	}
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{varRun, run}, filepath.Join(tmp, "b4-run"), tmp)
	writeSnapshotFile(t, filepath.Join(run, testSnapName), map[string]string{testChecksum: "1"}, 0o600)

	m := loadSysctlSnapshot()
	if m[testChecksum] != "1" {
		t.Fatalf("snapshot = %v, want checksum=1", m)
	}
	m[testLiberal] = "0"
	if err := saveSysctlSnapshot(m); err != nil {
		t.Fatal(err)
	}
	if got := readSnapshotJSON(t, filepath.Join(run, testSnapName)); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("snapshot behind the link = %v: the takeover removed b4's own file", got)
	}
}

func TestAFailedSysctlSaveIsLoggedOncePerRun(t *testing.T) {
	buf := captureTablesLog(t)
	gone := filepath.Join(t.TempDir(), "gone")
	useSnapshotDirs(t, []string{filepath.Join(gone, "var-run")}, filepath.Join(gone, "tmp", "b4-run"), filepath.Join(gone, "tmp"))
	s := stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})

	for range 3 {
		applyAll()
	}
	if len(s.wrote) != 0 {
		t.Fatalf("Apply wrote %v without saving the old values", s.wrote)
	}
	for _, name := range []string{testChecksum, testLiberal} {
		if n := logLinesWith(buf.String(), "[ERROR]", "leaving "+name); n != 1 {
			t.Fatalf("%d errors for %s over three rules passes, want 1:\n%s", n, name, buf.String())
		}
	}
}

func TestAFailedSysctlSaveIsLoggedOnceForEachOutage(t *testing.T) {
	buf := captureTablesLog(t)
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})
	blocker := filepath.Join(run, testSnapName)
	block := func() {
		if err := os.Mkdir(blocker, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(blocker, "x"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	block()
	applyAll()
	applyAll()
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}
	applyAll()
	revertAll()
	block()
	applyAll()
	applyAll()
	if n := logLinesWith(buf.String(), "[ERROR]", "leaving "+testChecksum); n != 2 {
		t.Fatalf("%d errors for %s over two outages with a successful save between them, want 2:\n%s", n, testChecksum, buf.String())
	}
}

func TestSysctlSnapshotIsNotHeldUpByAFIFOSwappedInAfterItLooked(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	older := filepath.Join(tmp, "b4_sysctl_snapshot.json")
	writeSnapshotFile(t, older, map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	stubConntrackSysctls(t, map[string]string{testChecksum: "0", testLiberal: "1"})
	prev := sysctlSnapOpenat
	t.Cleanup(func() { sysctlSnapOpenat = prev })
	swapped := false
	sysctlSnapOpenat = func(dirfd int, name string, flags int, mode uint32) (int, error) {
		if !swapped && name == "b4_sysctl_snapshot.json" {
			swapped = true
			if err := os.Remove(older); err != nil {
				t.Error(err)
			}
			if err := syscall.Mkfifo(older, 0o600); err != nil {
				t.Error(err)
			}
		}
		return prev(dirfd, name, flags, mode)
	}

	finishesInTime(t, "a load next to a FIFO swapped in after the look", releaseFIFO(older), func() {
		loadSysctlSnapshot()
	})
	if err := os.Remove(older); err != nil {
		t.Fatal(err)
	}
	writeSnapshotFile(t, older, map[string]string{testChecksum: "1", testLiberal: "0"}, 0o600)
	if got := loadSysctlSnapshot(); got[testChecksum] != "1" || got[testLiberal] != "0" {
		t.Fatalf("snapshot = %v, want the originals once the file is back", got)
	}
}

func TestAnUnreadableSnapshotIsLoggedOnceForEachOutage(t *testing.T) {
	buf := captureTablesLog(t)
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})
	applyAll()
	prev := sysctlSnapOpenat
	t.Cleanup(func() { sysctlSnapOpenat = prev })
	failing := false
	sysctlSnapOpenat = func(dirfd int, name string, flags int, mode uint32) (int, error) {
		if failing && name == testSnapName {
			return -1, syscall.EMFILE
		}
		return prev(dirfd, name, flags, mode)
	}

	failing = true
	applyAll()
	applyAll()
	failing = false
	applyAll()
	failing = true
	applyAll()
	applyAll()
	if n := logLinesWith(buf.String(), "[ERROR]", "leaving "+testChecksum+" as it is"); n != 2 {
		t.Fatalf("%d errors for %s over two read outages with a good read between them, want 2:\n%s", n, testChecksum, buf.String())
	}
}

func TestAnUnreadableSnapshotIsLoggedOnceForEachOutageWhenPuttingBack(t *testing.T) {
	buf := captureTablesLog(t)
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})
	applyAll()
	prev := sysctlSnapOpenat
	t.Cleanup(func() { sysctlSnapOpenat = prev })
	failing := false
	sysctlSnapOpenat = func(dirfd int, name string, flags int, mode uint32) (int, error) {
		if failing && name == testSnapName {
			return -1, syscall.EMFILE
		}
		return prev(dirfd, name, flags, mode)
	}

	failing = true
	for range 3 {
		revertAll()
	}
	failing = false
	revertAll()
	applyAll()
	failing = true
	revertAll()
	revertAll()
	if n := logLinesWith(buf.String(), "not putting "+testChecksum+" back now"); n != 2 {
		t.Fatalf("%d warnings for %s over two read outages of several refreshes each, want 2:\n%s", n, testChecksum, buf.String())
	}
}

func TestAnEmptySysctlSnapshotLeavesNoFile(t *testing.T) {
	run := privateRunDir(t)
	tmp := sharedTmp(t)
	useSnapshotDirs(t, []string{run}, filepath.Join(tmp, "b4-run"), tmp)
	stubConntrackSysctls(t, map[string]string{testChecksum: "1", testLiberal: "0"})

	applyAll()
	if names := dirEntries(t, run); !slices.Equal(names, []string{testSnapName}) {
		t.Fatalf("run directory after Apply = %v", names)
	}
	revertAll()
	if names := dirEntries(t, run); len(names) != 0 {
		t.Fatalf("run directory after RevertBack = %v, want empty", names)
	}
}

func TestSysctlSnapshotTrustsOnlyRootOrB4(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeSnapshotFile(t, path, map[string]string{}, 0o600)
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		t.Fatal(err)
	}
	if err := trustSnapshotFile(path, attrsOf(&st), true); err != nil {
		t.Fatalf("a file of b4's own user: %v", err)
	}
	prev := sysctlSnapOwnerOK
	t.Cleanup(func() { sysctlSnapOwnerOK = prev })
	sysctlSnapOwnerOK = func(uint32) bool { return false }
	if err := trustSnapshotFile(path, attrsOf(&st), true); err == nil || !strings.Contains(err.Error(), "belongs to uid") {
		t.Fatalf("a file of another user: err %v", err)
	}
	sysctlSnapOwnerOK = prev
	if !sysctlSnapOwnerOK(0) {
		t.Fatal("root's files are not trusted")
	}
	if os.Geteuid() != 0 && sysctlSnapOwnerOK(uint32(os.Geteuid()+1)) {
		t.Fatal("another user's files are trusted")
	}
}

func TestTheQueueEngineNetnsRunKeepsItsTempDirAndBinaryInReach(t *testing.T) {
	for _, tool := range []string{"make", "unshare", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s", tool)
		}
	}
	if err := exec.Command("unshare", "-Urnm", "true").Run(); err != nil {
		t.Skipf("no unprivileged user namespaces here: %v", err)
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(repo+"/", "/tmp/") {
		t.Skip("the checkout itself is under /tmp")
	}
	outside := filepath.Join(repo, "out")
	if _, err := os.Stat(outside); os.IsNotExist(err) {
		if err := os.Mkdir(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(outside) })
	}
	stubDir := func(t *testing.T, parent string) string {
		t.Helper()
		dir, err := os.MkdirTemp(parent, "netns-run-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		stub := "#!/bin/sh\necho \"tmpdir=$TMPDIR\"\n[ -d \"$TMPDIR\" ] && echo tmpdir-ok\n[ -x \"$B4_WSREUSE_BINARY\" ] && echo relay-ok\nexit 0\n"
		for _, name := range []string{"nfq.test", "b4-netns"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	runLine := func(t *testing.T, outDir string) (string, error) {
		t.Helper()
		recipe, err := exec.Command("make", "-s", "-n", "-C", repo, "test-netns", "OUT_DIR="+outDir).CombinedOutput()
		if err != nil {
			t.Fatalf("make -n test-netns: %v\n%s", err, recipe)
		}
		m := regexp.MustCompile(`unshare -Urnm sh -c '([^']*nfq\.test[^']*)'(?:\s+(/\S+))?`).FindStringSubmatch(string(recipe))
		if m == nil {
			t.Fatalf("no queue engine namespace line in make -n test-netns:\n%s", recipe)
		}
		args := []string{"-Urnm", "sh", "-c", m[1]}
		if m[2] != "" {
			args = append(args, m[2])
		}
		cmd := exec.Command("unshare", args...)
		cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("output outside /tmp, TMPDIR inside it", func(t *testing.T) {
		out, err := runLine(t, stubDir(t, outside))
		if err != nil || !strings.Contains(out, "tmpdir-ok") || !strings.Contains(out, "relay-ok") {
			t.Fatalf("the suite would start without its temporary directory or relay binary (err %v):\n%s", err, out)
		}
	})
	t.Run("output under /tmp", func(t *testing.T) {
		out, err := runLine(t, stubDir(t, "/tmp"))
		if err == nil || !strings.Contains(out, "under /tmp") {
			t.Fatalf("a build under /tmp ran in a namespace that hides it (err %v):\n%s", err, out)
		}
	})
}

func TestUninstallRemovesTheSysctlSnapshotWhereverB4KeepsIt(t *testing.T) {
	script, err := filepath.Abs("../../installer/actions/remove.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	stubs := `set -f; rm() { echo "rm $*"; }; rmdir() { echo "rmdir $*"; }; readlink() { echo 'net:[4026531992]'; }; `
	out, err := exec.Command("sh", "-c", `. "$1" && `+stubs+`_remove_sysctl_snapshots`, "sh", script).CombinedOutput()
	if err != nil {
		t.Fatalf("_remove_sysctl_snapshots: %v\n%s", err, out)
	}
	var want []string
	for _, dir := range append(slices.Clone(sysctlSnapRunDirs), sysctlSnapPrivateDir, sysctlSnapLegacyDir) {
		want = append(want, "rm -f "+dir+"/b4_sysctl_snapshot.json "+dir+"/b4_sysctl_snapshot.json.*", "rm -f "+dir+"/"+testSnapName+" "+dir+"/"+testSnapName+".*")
	}
	want = append(want, "rmdir "+sysctlSnapPrivateDir)
	if got := strings.Split(strings.TrimSpace(string(out)), "\n"); !slices.Equal(got, want) {
		t.Fatalf("uninstall ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
