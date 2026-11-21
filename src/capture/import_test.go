package capture

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	return testManagerIn(t, t.TempDir())
}

func testManagerIn(t *testing.T, dir string) *Manager {
	t.Helper()
	return newManager(dir)
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

func TestAnImportNeverTakesTheFileOfAnotherPayload(t *testing.T) {
	m := testManager(t)
	data := bytes.Repeat([]byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01}, 10)
	if err := m.SaveUploadedCapture("tls", "ab.import.example", data, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveImportedPayload("tls", "a_b.import.example", data); err != nil {
		t.Fatal(err)
	}
	uploaded, _ := m.GetCapture("tls", "ab.import.example")
	imported, ok := m.GetCapture("tls", "a_b.import.example")
	if !ok {
		t.Fatal("the import is not listed under its own name")
	}
	if imported.Filepath == uploaded.Filepath {
		t.Errorf("the import a_b.import.example took %s, the file of the upload ab.import.example", uploaded.Filepath)
	}
	if err := m.DeleteCapture("tls", "a_b.import.example"); err != nil {
		t.Fatal(err)
	}
	if got, err := m.LoadCaptureData(uploaded); err != nil || !bytes.Equal(got, data) {
		t.Errorf("deleting the import a_b.import.example left the upload ab.import.example without its bytes: %d bytes, %v", len(got), err)
	}
}

func TestAnImportFindsItsOwnEntryUnderATaggedFile(t *testing.T) {
	m := testManager(t)
	first := bytes.Repeat([]byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0xaa}, 10)
	second := bytes.Repeat([]byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0xbb}, 10)
	if err := m.SaveUploadedCapture("tls", "пример.рф", first, false); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveUploadedCapture("tls", "яндекс.рф", second, false); err != nil {
		t.Fatal(err)
	}
	tagged, _ := m.GetCapture("tls", "яндекс.рф")
	rel, err := m.SaveImportedPayload("tls", "яндекс.рф", second)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, c := range m.ListCaptures() {
		listed = append(listed, c.Domain+" "+c.Filepath)
	}
	if rel != tagged.Filepath || len(listed) != 2 {
		t.Errorf("importing the bytes of яндекс.рф under its own name returned %s, want its file %s; listed: %q", rel, tagged.Filepath, listed)
	}
}

func TestAnImportLeavesAnUnlistedFileOfOtherBytesAlone(t *testing.T) {
	m := testManager(t)
	unlisted := filepath.Join(m.outputPath, "tls_unlisted_import_example.bin")
	if err := os.WriteFile(unlisted, []byte("bytes a set may still name"), 0644); err != nil {
		t.Fatal(err)
	}
	rel, err := m.SaveImportedPayload("tls", "unlisted.import.example", []byte("imported bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(unlisted); string(data) != "bytes a set may still name" {
		t.Errorf("an import overwrote %s, an unlisted file, with %q", filepath.Base(unlisted), data)
	}
	c, ok := m.GetCapture("tls", "unlisted.import.example")
	if !ok || c.Filepath != rel || rel == filepath.Join("captures", filepath.Base(unlisted)) {
		t.Errorf("the import must be listed under its own name with a file of its own, got %v listed %t, returned %s", c, ok, rel)
	}
	if got, err := m.LoadCaptureData(c); err != nil || string(got) != "imported bytes" {
		t.Errorf("the imported entry holds %q (%v)", got, err)
	}
}
