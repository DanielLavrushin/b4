package capture

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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

func withFileSizeLimit(t *testing.T, limit uint64, f func()) {
	t.Helper()
	signal.Ignore(syscall.SIGXFSZ)
	defer signal.Reset(syscall.SIGXFSZ)
	var prev syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &prev); err != nil {
		t.Fatal(err)
	}
	limited := prev
	limited.Cur = limit
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &prev); err != nil {
			t.Fatal(err)
		}
	}()
	f()
}

func binFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin") {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestAFailedPayloadWriteKeepsTheFileItWouldReplace(t *testing.T) {
	m := testManager(t)
	const domain = "replace.atomic.example"
	old := bytes.Repeat([]byte{0x16}, 100)
	if err := m.SaveUploadedCapture("tls", domain, old, true); err != nil {
		t.Fatal(err)
	}
	c, ok := m.GetCapture("tls", domain)
	if !ok {
		t.Fatal("the first upload is not listed")
	}

	var replaceErr, generateErr, importErr error
	withFileSizeLimit(t, 50, func() {
		replaceErr = m.SaveUploadedCapture("tls", domain, bytes.Repeat([]byte{0x17}, 200), true)
		generateErr = m.GenerateCapture("generate.atomic.example", "tls")
		_, importErr = m.SaveImportedPayload("tls", "import.atomic.example", bytes.Repeat([]byte{0x18}, 200))
	})
	if replaceErr == nil || generateErr == nil || importErr == nil {
		t.Fatalf("fixture: every write past the file size limit must fail, got replace %v, generate %v, import %v", replaceErr, generateErr, importErr)
	}

	got, err := m.LoadCaptureData(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, old) {
		now, _ := m.GetCapture("tls", domain)
		t.Errorf("a failed replace (%v) destroyed the previous payload: the file now holds %d bytes (% x...) while the list reports size %d",
			replaceErr, len(got), got[:min(4, len(got))], now.Size)
	}
	if files := binFiles(t, m.outputPath); len(files) != 1 || files[0] != filepath.Base(c.Filepath) {
		t.Errorf("failed writes must leave no partial payload files, the captures folder holds %q", files)
	}
}

func TestAFailedMetadataSaveLeavesNothingListed(t *testing.T) {
	m := testManager(t)
	const kept, added, generated = "kept.rollback.example", "added.rollback.example", "generated.rollback.example"
	old := bytes.Repeat([]byte{0x16}, 40)
	if err := m.SaveUploadedCapture("tls", kept, old, true); err != nil {
		t.Fatal(err)
	}
	keptCapture, _ := m.GetCapture("tls", kept)

	if err := os.Remove(m.metadataFile); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(m.metadataFile, "blocker"), 0755); err != nil {
		t.Fatal(err)
	}
	var problems []string
	if err := m.SaveUploadedCapture("tls", added, []byte{0x16, 0x03}, true); err == nil {
		problems = append(problems, "fixture: an upload whose metadata save fails must return the error")
	}
	if _, listed := m.GetCapture("tls", added); listed {
		problems = append(problems, "an upload whose metadata save failed is listed until the next restart drops it")
	}
	if err := m.GenerateCapture(generated, "tls"); err == nil {
		problems = append(problems, "a generate whose metadata save failed returned no error")
	}
	if _, listed := m.GetCapture("tls", generated); listed {
		problems = append(problems, "a generate whose metadata save failed is listed")
	}
	if err := m.SaveUploadedCapture("tls", kept, bytes.Repeat([]byte{0x17}, 80), true); err == nil {
		problems = append(problems, "a re-upload whose metadata save fails must return the error")
	}
	if c, listed := m.GetCapture("tls", kept); !listed || c.Size != len(old) {
		problems = append(problems, fmt.Sprintf("after a failed re-upload the earlier payload must stay listed with its size %d, got listed %t", len(old), listed))
	}
	if got, err := m.LoadCaptureData(keptCapture); err != nil || !bytes.Equal(got, old) {
		problems = append(problems, fmt.Sprintf("after a failed re-upload the file must hold the earlier %d bytes, got %d (%v)", len(old), len(got), err))
	}
	if files := binFiles(t, m.outputPath); len(files) != 1 || files[0] != filepath.Base(keptCapture.Filepath) {
		problems = append(problems, fmt.Sprintf("payloads whose metadata was not saved left their files: %q", files))
	}

	if err := os.RemoveAll(m.metadataFile); err != nil {
		t.Fatal(err)
	}
	if err := m.GenerateCapture(generated, "tls"); err != nil {
		problems = append(problems, fmt.Sprintf("a generate retried after the failed save must succeed, got %v", err))
	}
	if len(problems) > 0 {
		t.Fatal(strings.Join(problems, "; "))
	}
}

func TestNamesThatSanitizeAlikeKeepTheirOwnFiles(t *testing.T) {
	m := testManager(t)
	var problems []string
	saved := func(name string) (*Capture, []byte) {
		t.Helper()
		c, ok := m.GetCapture("tls", name)
		if !ok {
			t.Fatalf("%q is not listed", name)
		}
		data, err := m.LoadCaptureData(c)
		if err != nil {
			t.Fatal(err)
		}
		return c, data
	}

	first, second := []byte("payload of пример.рф"), []byte("payload of яндекс.рф")
	if err := m.SaveUploadedCapture("tls", "пример.рф", first, true); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveUploadedCapture("tls", "яндекс.рф", second, true); err != nil {
		t.Fatal(err)
	}
	a, aData := saved("пример.рф")
	b, bData := saved("яндекс.рф")
	if a.Filepath == b.Filepath || !bytes.Equal(aData, first) || !bytes.Equal(bData, second) {
		problems = append(problems, fmt.Sprintf("uploads пример.рф and яндекс.рф: files %s and %s, пример.рф (size %d) now holds %q", a.Filepath, b.Filepath, a.Size, aData))
	}

	again := []byte("payload of пример.рф, uploaded again")
	if err := m.SaveUploadedCapture("tls", "пример.рф", again, true); err != nil {
		t.Fatal(err)
	}
	if c, data := saved("пример.рф"); c.Filepath != a.Filepath || !bytes.Equal(data, again) {
		problems = append(problems, fmt.Sprintf("a re-upload of пример.рф must replace its own file %s, got %s holding %q", a.Filepath, c.Filepath, data))
	}
	if _, data := saved("яндекс.рф"); !bytes.Equal(data, second) {
		problems = append(problems, fmt.Sprintf("a re-upload of пример.рф changed яндекс.рф to %q", data))
	}

	for _, name := range []string{"ab.sanitize.example", "a_b.sanitize.example"} {
		if err := m.GenerateCapture(name, "tls"); err != nil {
			t.Fatal(err)
		}
	}
	plain, plainData := saved("ab.sanitize.example")
	underscored, underscoredData := saved("a_b.sanitize.example")
	if plain.Filepath == underscored.Filepath || !bytes.Contains(plainData, []byte("ab.sanitize.example")) || !bytes.Contains(underscoredData, []byte("a_b.sanitize.example")) {
		problems = append(problems, fmt.Sprintf("generated ab.sanitize.example and a_b.sanitize.example share %s", plain.Filepath))
	}

	orphan := filepath.Join(m.outputPath, "tls_orphan_sanitize_example.bin")
	if err := os.WriteFile(orphan, []byte("a file no entry lists"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveUploadedCapture("tls", "orphan.sanitize.example", []byte("new upload"), true); err != nil {
		t.Fatal(err)
	}
	if c, data := saved("orphan.sanitize.example"); c.Filepath != filepath.Join("captures", filepath.Base(orphan)) || string(data) != "new upload" {
		problems = append(problems, fmt.Sprintf("an upload must take its plain file %s when no entry lists it, as a set may name it; got %s holding %q", filepath.Base(orphan), c.Filepath, data))
	}

	blocker := filepath.Join(m.outputPath, "tls_dir_sanitize_example.bin")
	if err := os.Mkdir(blocker, 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveUploadedCapture("tls", "dir.sanitize.example", []byte("next to a directory"), true); err != nil {
		t.Fatal(err)
	}
	if c, data := saved("dir.sanitize.example"); c.Filepath == filepath.Join("captures", filepath.Base(blocker)) || string(data) != "next to a directory" {
		problems = append(problems, fmt.Sprintf("an upload whose plain name is a directory must take a tagged file, got %s holding %q", c.Filepath, data))
	}
	if info, err := os.Stat(blocker); err != nil || !info.IsDir() {
		problems = append(problems, fmt.Sprintf("the upload replaced the directory %s: %v", filepath.Base(blocker), err))
	}

	if len(problems) > 0 {
		t.Fatal(strings.Join(problems, "; "))
	}
}

func TestAFailedSaveOverAnUnlistedFileGivesItsBytesBack(t *testing.T) {
	m := testManager(t)
	unlisted := filepath.Join(m.outputPath, "tls_unlisted_rollback_example.bin")
	if err := os.WriteFile(unlisted, []byte("bytes a set may still name"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(m.metadataFile, "blocker"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveUploadedCapture("tls", "unlisted.rollback.example", []byte("new upload"), true); err == nil {
		t.Fatal("fixture: an upload whose metadata save fails must return the error")
	}
	if data, err := os.ReadFile(unlisted); err != nil || string(data) != "bytes a set may still name" {
		t.Errorf("an upload whose metadata save failed left the unlisted file it took holding %q (%v)", data, err)
	}
	if _, listed := m.GetCapture("tls", "unlisted.rollback.example"); listed {
		t.Errorf("an upload whose metadata save failed is listed")
	}
}

func TestReloadTakesTheListOnDiskAsItIs(t *testing.T) {
	m := testManager(t)
	if err := m.SaveUploadedCapture("tls", "before.reload.example", []byte{0x16}, true); err != nil {
		t.Fatal(err)
	}
	restored := `{"restored.reload.example":{"quic":{"timestamp":"2026-11-01T00:00:00Z","size":2,"filepath":"quic_restored_reload_example.bin"}}}`
	if err := os.WriteFile(m.metadataFile, []byte(restored), 0644); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	var names []string
	for _, c := range m.ListCaptures() {
		names = append(names, c.Protocol+":"+c.Domain)
	}
	if len(names) != 1 || names[0] != "quic:restored.reload.example" {
		t.Errorf("after a reload the list must be the one on disk, got %q", names)
	}
	if err := os.Remove(m.metadataFile); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if listed := m.ListCaptures(); len(listed) != 0 {
		t.Errorf("after a reload with no payloads.json the list must be empty, got %d entries", len(listed))
	}
}

func TestNullEntriesInPayloadsJSONDoNotPanic(t *testing.T) {
	calls := []struct {
		name string
		call func(m *Manager)
	}{
		{"ListCaptures", func(m *Manager) { m.ListCaptures() }},
		{"ClearAll", func(m *Manager) { m.ClearAll() }},
		{"SaveUploadedCapture", func(m *Manager) { m.SaveUploadedCapture("tls", "upload.null.example", []byte{0x16, 0x03, 0x01}, true) }},
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
			if err := m.SaveUploadedCapture("tls", "after.null.example", []byte{0x16}, true); err != nil {
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
