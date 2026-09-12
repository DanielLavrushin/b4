package capture

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
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
