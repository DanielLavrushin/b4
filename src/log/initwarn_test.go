package log

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInitWarningsReachTheErrorFileOnceItOpens(t *testing.T) {
	initPending = nil
	InitWarnf("single-instance guard DISABLED, flock(%s): %v", "/var/run/b4.pid", "permission denied")

	path := filepath.Join(t.TempDir(), "errors.log")
	if err := InitErrorFile(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetErrorFile("") })
	InitWarnf("could not update pidfile %s", "/var/run/b4.pid")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"=== b4 error log opened", "[INIT] single-instance guard DISABLED", "[INIT] could not update pidfile"} {
		if n := strings.Count(got, want); n != 1 {
			t.Fatalf("errors.log must carry %q exactly once, found %d:\n%s", want, n, got)
		}
	}
}

func TestErrorfKeepsWrappedErrorsReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "errors.log")
	if err := InitErrorFile(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetErrorFile("") })

	cause := errors.New("bad port")
	err := Errorf("invalid configuration: %w", cause)
	if !errors.Is(err, cause) {
		t.Fatalf("the returned error must still wrap its cause, got %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(data); !strings.Contains(got, "[ERROR] invalid configuration: bad port") || strings.Contains(got, "%!w") {
		t.Fatalf("a %%w argument must be written as its text, got:\n%s", got)
	}

	flags, fcntlErr := unix.FcntlInt(uintptr(origStderr), unix.F_GETFD, 0)
	if fcntlErr != nil {
		t.Fatal(fcntlErr)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("the saved stderr must close on exec, or every child process and restart inherits it")
	}
}
