package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetThreadDumpState() {
	threadDumpDone.Store(false)
}

func TestOSThreadsMatchesRuntime(t *testing.T) {
	if n := osThreads(); n < 1 {
		t.Fatalf("osThreads() = %d, want at least 1", n)
	}
}

func TestCheckThreadPressureBelowTriggerWritesNothing(t *testing.T) {
	resetThreadDumpState()
	dir := t.TempDir()
	SetThreadDumpDir(dir)

	checkThreadPressure(threadDumpTrigger - 1)

	if _, err := os.Stat(filepath.Join(dir, "goroutines.txt")); !os.IsNotExist(err) {
		t.Fatalf("a dump was written below the trigger (err=%v)", err)
	}
}

func TestCheckThreadPressureWritesDumpOnce(t *testing.T) {
	resetThreadDumpState()
	dir := t.TempDir()
	SetThreadDumpDir(dir)

	path := filepath.Join(dir, "goroutines.txt")
	checkThreadPressure(threadDumpTrigger)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no dump written at the trigger: %v", err)
	}
	if !strings.Contains(string(data), "goroutine ") || !strings.Contains(string(data), "runtime/pprof") {
		t.Fatalf("dump does not look like a goroutine stack dump: %.80q", data)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	checkThreadPressure(threadDumpTrigger * 2)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a second dump was written (err=%v)", err)
	}
}
