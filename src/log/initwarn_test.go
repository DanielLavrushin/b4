package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
