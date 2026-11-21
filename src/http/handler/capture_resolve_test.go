package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
)

func resolvePayloadFile(t *testing.T, mux *http.ServeMux, file string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/capture/resolve?" + url.Values{"file": {file}}.Encode()
	rec, panicked := serveRecovered(mux, httptest.NewRequest(http.MethodGet, target, nil))
	if panicked != "" {
		t.Fatalf("GET %s panicked: %s", target, panicked)
	}
	return rec
}

func requireResolved(t *testing.T, mux *http.ServeMux, file string) map[string]any {
	t.Helper()
	rec := resolvePayloadFile(t, mux, file)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/capture/resolve?file=%s answered %d %q, want 200", file, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	answer := answerObject(t, "resolve "+file, rec)
	requireKeys(t, "the resolve answer for "+file, answer, "file", "listed", "exists", "size", "too_large")
	return answer
}

func unlistedCaptureFile(t *testing.T, manager *capture.Manager, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(manager.GetOutputPath(), name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	return "captures/" + name
}

func requireNoFacts(t *testing.T, what string, answer map[string]any) {
	t.Helper()
	if facts, carried := answer["facts"]; carried {
		t.Errorf("%s carries facts %v, want the key omitted", what, facts)
	}
}

func TestResolveFindsTheListedEntryUnderEverySpellingTheLoaderAccepts(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "alias.resolve.example"
	listedFile, err := manager.SaveImportedPayload("tls", name, config.FakeSNI2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	base := filepath.Base(listedFile)

	for _, spelling := range []string{listedFile, "./" + listedFile, "./captures//" + base, "captures/./" + base, "captures/elsewhere/../" + base} {
		answer := requireResolved(t, mux, spelling)
		if answer["file"] != listedFile || answer["listed"] != true || answer["exists"] != true || answer["size"] != float64(len(config.FakeSNI2)) {
			t.Errorf("%s resolves to file %v, listed %v, exists %v, size %v; want %s, true, true, %d", spelling, answer["file"], answer["listed"], answer["exists"], answer["size"], listedFile, len(config.FakeSNI2))
		}
		facts := objectAt(t, "the resolve answer for "+spelling, answer, "facts")
		if facts["kind"] != "tls_client_hello" || facts["sni"] != "vk.com" {
			t.Errorf("%s resolves with facts %v, want the vk.com ClientHello", spelling, facts)
		}
	}
}

func TestResolveDescribesAFileThePayloadListDoesNotName(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const base = "tls_unlisted_resolve_example.bin"
	file := unlistedCaptureFile(t, manager, base, config.FakeSNI2)

	answer := requireResolved(t, mux, "./captures//"+base)
	if answer["file"] != file || answer["listed"] != false || answer["exists"] != true || answer["size"] != float64(len(config.FakeSNI2)) {
		t.Errorf("an unlisted readable file resolves to file %v, listed %v, exists %v, size %v; want %s, false, true, %d", answer["file"], answer["listed"], answer["exists"], answer["size"], file, len(config.FakeSNI2))
	}
	facts := objectAt(t, "the resolve answer for an unlisted file", answer, "facts")
	if facts["kind"] != "tls_client_hello" || facts["sni"] != "vk.com" {
		t.Errorf("an unlisted readable file resolves with facts %v, want the vk.com ClientHello", facts)
	}
}

func TestResolveSaysWhenThereIsNothingToSend(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	empty := unlistedCaptureFile(t, manager, "tls_empty_resolve_example.bin", nil)
	large := unlistedCaptureFile(t, manager, "tls_large_resolve_example.bin", bytes.Repeat([]byte{0x16}, config.MaxCapturePayloadBytes+1))

	answer := requireResolved(t, mux, "captures/never_saved_resolve_example.bin")
	if answer["file"] != "captures/never_saved_resolve_example.bin" || answer["listed"] != false || answer["exists"] != false || answer["size"] != float64(0) {
		t.Errorf("a missing file resolves to file %v, listed %v, exists %v, size %v; want exists false and size 0", answer["file"], answer["listed"], answer["exists"], answer["size"])
	}
	requireNoFacts(t, "the resolve answer for a missing file", answer)

	answer = requireResolved(t, mux, empty)
	if answer["exists"] != true || answer["size"] != float64(0) {
		t.Errorf("an empty file resolves with exists %v, size %v; want true, 0", answer["exists"], answer["size"])
	}
	requireNoFacts(t, "the resolve answer for an empty file", answer)

	answer = requireResolved(t, mux, large)
	if answer["exists"] != true || answer["size"] != float64(config.MaxCapturePayloadBytes+1) {
		t.Errorf("a file over the payload limit resolves with exists %v, size %v; want true, %d", answer["exists"], answer["size"], config.MaxCapturePayloadBytes+1)
	}
	requireNoFacts(t, "the resolve answer for a file the loader refuses as too large", answer)
}

func TestResolveSaysWhenAFileIsLargerThanAPayloadMayBe(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	large := unlistedCaptureFile(t, manager, "quic_big_unlisted_resolve_example.bin", bytes.Repeat([]byte{0x5c}, config.MaxCapturePayloadBytes+1))
	normal := unlistedCaptureFile(t, manager, "tls_normal_unlisted_resolve_example.bin", config.FakeSNI2)

	answer := requireResolved(t, mux, large)
	if answer["listed"] != false || answer["exists"] != true || answer["size"] != float64(config.MaxCapturePayloadBytes+1) || answer["too_large"] != true {
		t.Errorf("an unlisted file one byte over the payload limit resolves with listed %v, exists %v, size %v, too_large %v; want false, true, %d, true", answer["listed"], answer["exists"], answer["size"], answer["too_large"], config.MaxCapturePayloadBytes+1)
	}
	requireNoFacts(t, "the resolve answer for a file over the payload limit", answer)

	answer = requireResolved(t, mux, normal)
	if answer["exists"] != true || answer["too_large"] != false {
		t.Errorf("an unlisted file within the payload limit resolves with exists %v, too_large %v; want true, false", answer["exists"], answer["too_large"])
	}
	objectAt(t, "the resolve answer for a file within the payload limit", answer, "facts")

	answer = requireResolved(t, mux, "captures/never_saved_too_large_example.bin")
	if answer["exists"] != false || answer["too_large"] != false {
		t.Errorf("a missing file resolves with exists %v, too_large %v; want false, false", answer["exists"], answer["too_large"])
	}
}

func TestResolveRefusesPathsOutsideCaptures(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	dir := filepath.Dir(manager.GetOutputPath())
	outside := filepath.Join(dir, "outside_resolve_example.bin")
	if err := os.WriteFile(outside, config.FakeSNI2, 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	for _, file := range []string{"", "../b4.json", "../outside_resolve_example.bin", "outside_resolve_example.bin", "captures/../outside_resolve_example.bin", outside, "/etc/x.bin", "captures/notes_resolve_example.txt", "captures"} {
		rec := resolvePayloadFile(t, mux, file)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET /api/capture/resolve?file=%s answered %d %q, want 400", file, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}

	link := filepath.Join(manager.GetOutputPath(), "tls_link_resolve_example.bin")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
	answer := requireResolved(t, mux, "captures/tls_link_resolve_example.bin")
	if answer["exists"] != false || answer["size"] != float64(0) {
		t.Errorf("a link from captures to a file outside it resolves with exists %v, size %v; want it left unread: false, 0", answer["exists"], answer["size"])
	}
	requireNoFacts(t, "the resolve answer for a link out of captures", answer)
}
