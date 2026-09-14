package capture

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	return testManagerIn(t, t.TempDir())
}

func testManagerIn(t *testing.T, dir string) *Manager {
	t.Helper()
	return &Manager{
		metadata:        make(map[string]map[string]*PayloadMetadata),
		outputPath:      dir,
		metadataFile:    filepath.Join(dir, "payloads.json"),
		activeProbes:    make(map[string]time.Time),
		pendingCaptures: make(map[string]*PendingCapture),
		connToDomain:    make(map[string]string),
	}
}

func TestSaveImportedPayloadReusesIdenticalFileAndRenamesOnConflict(t *testing.T) {
	m := testManager(t)
	first, err := m.SaveImportedPayload("tls", "www.example.com", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	if first != filepath.Join("captures", "tls_www_example_com.bin") {
		t.Errorf("unexpected path %q", first)
	}
	again, err := m.SaveImportedPayload("tls", "www.example.com", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Errorf("identical bytes must reuse the existing file, got %q", again)
	}
	other, err := m.SaveImportedPayload("tls", "www.example.com", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Errorf("different bytes must not overwrite the existing file")
	}
	data, err := os.ReadFile(filepath.Join(m.outputPath, "tls_www_example_com.bin"))
	if err != nil || string(data) != "one" {
		t.Errorf("original file was touched: %q %v", data, err)
	}
	if len(m.ListCaptures()) != 2 {
		t.Errorf("expected two captures listed, got %d", len(m.ListCaptures()))
	}
}

func TestSaveImportedPayloadRegistersAnExistingUnlistedFile(t *testing.T) {
	m := testManager(t)
	metadataFile := m.metadataFile
	m.metadataFile = filepath.Join(m.outputPath, "missing", "payloads.json")
	if _, err := m.SaveImportedPayload("tls", "www.example.com", []byte("one")); err == nil {
		t.Fatal("a failed metadata save must be reported")
	}
	if _, err := os.Stat(filepath.Join(m.outputPath, "tls_www_example_com.bin")); err != nil {
		t.Fatalf("the payload file must be on disk after the failed save: %v", err)
	}
	if len(m.ListCaptures()) != 0 {
		t.Errorf("a payload whose metadata was not saved must not be listed")
	}

	m.metadataFile = metadataFile
	rel, err := m.SaveImportedPayload("tls", "www.example.com", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	if rel != filepath.Join("captures", "tls_www_example_com.bin") {
		t.Errorf("unexpected path %q", rel)
	}
	if len(m.ListCaptures()) != 1 {
		t.Fatalf("the retry must register the payload, got %d listed", len(m.ListCaptures()))
	}
	reloaded := testManagerIn(t, m.outputPath)
	reloaded.loadMetadata()
	if len(reloaded.ListCaptures()) != 1 {
		t.Errorf("the registration must be persisted, got %d listed after reload", len(reloaded.ListCaptures()))
	}

	if err := os.Remove(metadataFile); err != nil {
		t.Fatal(err)
	}
	orphaned := testManagerIn(t, m.outputPath)
	orphaned.loadMetadata()
	if len(orphaned.ListCaptures()) != 0 {
		t.Fatalf("test setup: the registry must start empty")
	}
	if _, err := orphaned.SaveImportedPayload("tls", "www.example.com", []byte("one")); err != nil {
		t.Fatal(err)
	}
	captures := orphaned.ListCaptures()
	if len(captures) != 1 {
		t.Fatalf("an identical unlisted file must be registered on import, got %d", len(captures))
	}
	info, err := os.Stat(filepath.Join(m.outputPath, "tls_www_example_com.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !captures[0].Timestamp.Equal(info.ModTime()) {
		t.Errorf("a repaired entry must carry the file's own time, got %v want %v", captures[0].Timestamp, info.ModTime())
	}
	if _, err := os.Stat(metadataFile); err != nil {
		t.Errorf("the repaired registry must be written to disk: %v", err)
	}
}
