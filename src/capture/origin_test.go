package capture

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func managerWithLegacyEntries(t *testing.T, files map[string][]byte) *Manager {
	t.Helper()
	dir := t.TempDir()
	var entries []string
	for name, data := range files {
		file := "tls_" + sanitizeDomain(name) + ".bin"
		if err := os.WriteFile(filepath.Join(dir, file), data, 0644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, fmt.Sprintf(`%q:{"tls":{"timestamp":"2026-03-02T00:00:00Z","size":%d,"filepath":%q}}`, name, len(data), file))
	}
	if err := os.WriteFile(filepath.Join(dir, "payloads.json"), []byte("{"+strings.Join(entries, ",")+"}"), 0644); err != nil {
		t.Fatal(err)
	}
	m := testManagerIn(t, dir)
	m.loadMetadata()
	return m
}

func originsOf(m *Manager) map[string]string {
	out := map[string]string{}
	for _, c := range m.ListCaptures() {
		out[c.Domain] = c.Origin
	}
	return out
}

func TestTheOldGeneratorsPayloadsAreRecognisedAtLoad(t *testing.T) {
	files := map[string][]byte{
		"www.google.com":          legacyGeneratedHello(t, "www.google.com"),
		"max.ru":                  legacyGeneratedHello(t, "max.ru"),
		"a label of my own":       legacyGeneratedHello(t, "www.example.com"),
		"fixed-generator.example": generatedHello(t, "fixed-generator.example"),
		"google-preset.example":   config.FakeSNI1,
		"vk-preset.example":       config.FakeSNI2,
		"stun.example":            config.FakeSTUN,
		"not-a-hello.example":     notAnyKnownPayload,
	}
	m := managerWithLegacyEntries(t, files)
	want := map[string]string{
		"www.google.com":          OriginGenerated,
		"max.ru":                  OriginGenerated,
		"a label of my own":       OriginGenerated,
		"fixed-generator.example": OriginGenerated,
		"google-preset.example":   "",
		"vk-preset.example":       "",
		"stun.example":            "",
		"not-a-hello.example":     "",
	}
	got := originsOf(m)
	for name, origin := range want {
		if got[name] != origin {
			t.Errorf("the legacy entry %q is listed with origin %q, want %q", name, got[name], origin)
		}
	}
	onDisk := payloadsJSONOnDisk(t, m)
	if onDisk["max.ru"]["tls"]["origin"] != OriginGenerated {
		t.Errorf("the recognised origin was not saved back to payloads.json: %v", onDisk["max.ru"]["tls"])
	}
	if _, saved := onDisk["google-preset.example"]["tls"]["origin"]; saved {
		t.Errorf("an entry not recognised as generator output must keep no origin: %v", onDisk["google-preset.example"]["tls"])
	}
}

func TestSavedPayloadsRecordWhereTheyCameFrom(t *testing.T) {
	m := testManager(t)
	if err := m.GenerateCapture("generated.origin.example", "tls"); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveUploadedCapture("tls", "uploaded.origin.example", generatedHello(t, "uploaded.origin.example")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveImportedPayload("tls", "imported.origin.example", generatedHello(t, "imported.origin.example")); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"generated.origin.example": OriginGenerated,
		"uploaded.origin.example":  OriginUploaded,
		"imported.origin.example":  OriginImported,
	}
	for _, mm := range []*Manager{m, managerReloadedFrom(t, m)} {
		got := originsOf(mm)
		for name, origin := range want {
			if got[name] != origin {
				t.Errorf("%s is listed with origin %q, want %q", name, got[name], origin)
			}
		}
	}
}

func managerReloadedFrom(t *testing.T, m *Manager) *Manager {
	t.Helper()
	reloaded := testManagerIn(t, m.outputPath)
	reloaded.loadMetadata()
	return reloaded
}

func TestRegenerateRebuildsAGeneratedPayloadInPlace(t *testing.T) {
	old := legacyGeneratedHello(t, "www.google.com")
	m := managerWithLegacyEntries(t, map[string][]byte{
		"www.google.com":        old,
		"a label of my own":     legacyGeneratedHello(t, "max.ru"),
		"google-preset.example": config.FakeSNI1,
	})
	before, _ := m.GetCapture("tls", "www.google.com")

	regenerated, err := m.RegenerateCapture("www.google.com")
	if err != nil {
		t.Fatal(err)
	}
	data, err := m.LoadCaptureData(regenerated)
	if err != nil {
		t.Fatal(err)
	}
	if regenerated.Filepath != before.Filepath || len(data) != len(old) || bytes.Equal(data, old) || regenerated.Size != len(old) {
		t.Errorf("regenerate wrote %d bytes to %s (equal to the old bytes: %t), want new bytes of the old length %d in %s",
			len(data), regenerated.Filepath, bytes.Equal(data, old), len(old), before.Filepath)
	}
	if f := regenerated.Facts; f == nil || f.SNI != "www.google.com" || len(f.Faults) != 0 || regenerated.Origin != OriginGenerated {
		t.Errorf("the regenerated payload has facts %s and origin %q, want a well-formed hello for www.google.com, origin generated", factsSummary(f), regenerated.Origin)
	}
	if f := FactsOf("tls", data); len(f.Faults) != 0 {
		t.Errorf("the regenerated file still has malformed fields %q", f.Faults)
	}

	labelled, err := m.RegenerateCapture("a label of my own")
	if err != nil {
		t.Fatal(err)
	}
	if labelled.Size != len(legacyGeneratedHello(t, "max.ru")) || labelled.Facts == nil || labelled.Facts.SNI != "max.ru" {
		t.Errorf("a generated hello for max.ru kept under another label must be rebuilt for max.ru at its length, got %d bytes with facts %s", labelled.Size, factsSummary(labelled.Facts))
	}

	if _, err := m.RegenerateCapture("google-preset.example"); !errors.Is(err, ErrNotGenerated) {
		t.Errorf("regenerating a payload that is not generator output answered %v, want ErrNotGenerated", err)
	}
	if _, err := m.RegenerateCapture("never.saved.example"); !errors.Is(err, ErrPayloadNotFound) {
		t.Errorf("regenerating an unknown name answered %v, want ErrPayloadNotFound", err)
	}
}
