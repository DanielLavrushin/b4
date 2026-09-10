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
	t.Cleanup(CloseErrorFile)
	InitWarnf("could not update pidfile %s", "/var/run/b4.pid")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"=== b4 error log opened", "[INIT] single-instance guard DISABLED", "[INIT] could not update pidfile"} {
		if !strings.Contains(got, want) {
			t.Fatalf("errors.log is missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "=== b4 error log opened") != 1 {
		t.Fatalf("the session header must be written once:\n%s", got)
	}
}
