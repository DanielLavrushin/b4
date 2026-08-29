package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBinaryReplacedDetectsAReplacedFile(t *testing.T) {
	prev := executableFingerprintAtStart
	t.Cleanup(func() { executableFingerprintAtStart = prev })

	if executableFingerprintAtStart == "" {
		t.Skip("cannot read this test binary's own path")
	}

	if binaryReplaced() {
		t.Fatal("an untouched binary must not read as replaced")
	}

	// What a manual swap or a container image update looks like: same path, different file.
	executableFingerprintAtStart = "1:1"
	if !binaryReplaced() {
		t.Fatal("a binary that differs from the one this process started from must be reported")
	}
}

func TestBinaryReplacedStaysQuietWithoutAReadablePath(t *testing.T) {
	prev := executableFingerprintAtStart
	t.Cleanup(func() { executableFingerprintAtStart = prev })

	// A container without /proc: os.Executable() fails, so there is nothing to compare.
	executableFingerprintAtStart = ""
	if binaryReplaced() {
		t.Fatal("with no readable path the answer must be no, not a guess")
	}
}

func TestExecutableFingerprintChangesWithTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4")

	if err := os.WriteFile(path, []byte("one"), 0755); err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte("a different build"), 0755); err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if fi1.Size() == fi2.Size() && fi1.ModTime().Equal(fi2.ModTime()) {
		t.Fatal("size and mtime together should distinguish two different builds")
	}
}
