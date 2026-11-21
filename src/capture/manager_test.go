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
