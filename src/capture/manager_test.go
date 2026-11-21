package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func managerWithPayloadsJSON(t *testing.T, payloadsJSON string) *Manager {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "payloads.json"), []byte(payloadsJSON), 0644); err != nil {
		t.Fatal(err)
	}
	m := testManagerIn(t, dir)
	m.loadMetadata()
	return m
}

func panicOf(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return ""
}

func TestPayloadsJSONCannotReachOutsideCaptures(t *testing.T) {
	base := t.TempDir()
	captures := filepath.Join(base, "captures")
	if err := os.MkdirAll(captures, 0755); err != nil {
		t.Fatal(err)
	}
	outsideJSON := filepath.Join(base, "b4.json")
	outsideBin := filepath.Join(base, "outside.bin")
	for _, f := range []string{outsideJSON, outsideBin} {
		if err := os.WriteFile(f, []byte(`{"system":{"web_server":{"password":"secret"}}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	entries := map[string]string{
		"json.escape.example":     "../b4.json",
		"bin.escape.example":      "../outside.bin",
		"absolute.escape.example": outsideBin,
		"nested.escape.example":   "sub/../../outside.bin",
	}
	var restored []string
	for name, file := range entries {
		restored = append(restored, fmt.Sprintf(`%q:{"tls":{"timestamp":"2026-01-01T00:00:00Z","size":1,"filepath":%q}}`, name, file))
	}
	if err := os.WriteFile(filepath.Join(captures, "payloads.json"), []byte("{"+strings.Join(restored, ",")+"}"), 0644); err != nil {
		t.Fatal(err)
	}

	var problems []string
	survives := func(when string) {
		for _, f := range []string{outsideJSON, outsideBin} {
			if _, err := os.Stat(f); err != nil {
				problems = append(problems, fmt.Sprintf("%s removed %s, a file outside the captures folder", when, f))
				os.WriteFile(f, []byte("{}"), 0600)
			}
		}
	}

	loaded := testManagerIn(t, captures)
	loaded.loadMetadata()
	for _, c := range loaded.ListCaptures() {
		problems = append(problems, fmt.Sprintf("payloads.json entry %q (file %q) is listed", c.Domain, entries[c.Domain]))
	}
	for name := range entries {
		loaded.DeleteCapture("tls", name)
	}
	survives("Delete of a loaded entry")
	loaded.ClearAll()
	survives("Clear All after loading")

	held := testManagerIn(t, captures)
	for name, file := range entries {
		held.metadata[name] = map[string]*PayloadMetadata{"tls": {Size: 1, Filepath: file}}
	}
	for name := range entries {
		if c, ok := held.GetCapture("tls", name); ok {
			if data, err := held.LoadCaptureData(c); err == nil {
				problems = append(problems, fmt.Sprintf("LoadCaptureData of %q read %d bytes through %q", name, len(data), entries[name]))
			}
		}
		held.DeleteCapture("tls", name)
	}
	survives("Delete of an entry held in memory")
	for name, file := range entries {
		held.metadata[name] = map[string]*PayloadMetadata{"tls": {Size: 1, Filepath: file}}
	}
	held.ClearAll()
	survives("Clear All of entries held in memory")

	if len(problems) > 0 {
		t.Fatal(strings.Join(problems, "; "))
	}
}

func TestNullEntriesInPayloadsJSONDoNotPanic(t *testing.T) {
	calls := []struct {
		name string
		call func(m *Manager)
	}{
		{"ListCaptures", func(m *Manager) { m.ListCaptures() }},
		{"ClearAll", func(m *Manager) { m.ClearAll() }},
		{"SaveUploadedCapture", func(m *Manager) { m.SaveUploadedCapture("tls", "upload.null.example", []byte{0x16, 0x03, 0x01}) }},
		{"GenerateCapture", func(m *Manager) { m.GenerateCapture("generate.null.example", "tls") }},
		{"SaveImportedPayload", func(m *Manager) { m.SaveImportedPayload("tls", "import.null.example", []byte{0x16, 0x03, 0x02}) }},
	}
	var problems []string
	for _, payloadsJSON := range []string{`null`, `{"entry.null.example":{"tls":null}}`, `{"protocols.null.example":null}`} {
		for _, c := range calls {
			m := managerWithPayloadsJSON(t, payloadsJSON)
			if msg := panicOf(func() { c.call(m) }); msg != "" {
				problems = append(problems, fmt.Sprintf("%s with a payloads.json of %s panicked: %s", c.name, payloadsJSON, msg))
			}
		}
		m := managerWithPayloadsJSON(t, payloadsJSON)
		if msg := panicOf(func() {
			if err := m.SaveUploadedCapture("tls", "after.null.example", []byte{0x16}); err != nil {
				t.Errorf("an upload after loading %s failed: %v", payloadsJSON, err)
			}
			if listed := m.ListCaptures(); len(listed) != 1 || listed[0].Domain != "after.null.example" {
				t.Errorf("after loading %s and one upload, the list must hold that upload only, got %d entries", payloadsJSON, len(listed))
			}
		}); msg != "" {
			problems = append(problems, fmt.Sprintf("an upload and a list after loading %s panicked: %s", payloadsJSON, msg))
		}
	}
	if len(problems) > 0 {
		t.Fatal(strings.Join(problems, "; "))
	}
}
