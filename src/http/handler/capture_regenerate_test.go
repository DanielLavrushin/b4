package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

func plantLegacyPayload(t *testing.T, manager *capture.Manager, name, file string, data []byte) string {
	t.Helper()
	dir := manager.GetOutputPath()
	if err := os.WriteFile(filepath.Join(dir, file), data, 0644); err != nil {
		t.Fatal(err)
	}
	listPath := filepath.Join(dir, "payloads.json")
	entries := map[string]map[string]map[string]any{}
	if raw, err := os.ReadFile(listPath); err == nil {
		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Fatal(err)
		}
	}
	entries[name] = map[string]map[string]any{"tls": {"timestamp": time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), "size": len(data), "filepath": file}}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(listPath, raw, 0644); err != nil {
		t.Fatal(err)
	}
	capture.Reload()
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	return filepath.Join("captures", file)
}

func regenerate(t *testing.T, mux *http.ServeMux, name string) *httptest.ResponseRecorder {
	t.Helper()
	rec, panicked := serveRecovered(mux, httptest.NewRequest(http.MethodPost, "/api/capture/regenerate", strings.NewReader(`{"domain":"`+name+`"}`)))
	if panicked != "" {
		t.Fatalf("regenerate %s panicked: %s", name, panicked)
	}
	return rec
}

func TestRegenerateRepairsAnOldGeneratedPayloadForTheSetsThatUseIt(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "old-generated.regenerate.example"
	old := legacyGeneratedFixture(t, "www.google.com")
	file := plantLegacyPayload(t, manager, name, "tls_old-generated_regenerate_example.bin", old)
	set := namedSet("regen", "regenerated", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = file
	useSets(api, set)
	if !bytes.Equal(sock.GetPayload(&runningSet(api).Faking), old) {
		t.Fatal("setup: the set does not send the old generated hello")
	}
	if entry := listedPayloadObject(t, mux, "tls", name); entry["origin"] != "generated" {
		t.Errorf("the old generator's hello is listed with origin %v, want generated", entry["origin"])
	}

	rec := regenerate(t, mux, name)
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate %s answered %d %q, want 200", name, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	answer := answerObject(t, "regenerate", rec)
	requireKeys(t, "the regenerate answer", answer, "success", "message", "size", "domain", "facts", "reloaded")
	if got := answer["reloaded"]; !reflect.DeepEqual(got, []any{"regenerated"}) {
		t.Errorf("regenerate answered reloaded=%v, want the set that names the file", got)
	}
	facts := objectAt(t, "the regenerate answer", answer, "facts")
	if _, faulty := facts["faults"]; faulty || answer["size"] != float64(len(old)) {
		t.Errorf("regenerate answered size %v and facts %v, want %d bytes with no malformed field", answer["size"], facts, len(old))
	}
	sent := sock.GetPayload(&runningSet(api).Faking)
	onDisk, err := os.ReadFile(filepath.Join(manager.GetOutputPath(), filepath.Base(file)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sent, old) || !bytes.Equal(sent, onDisk) || len(onDisk) != len(old) {
		t.Errorf("after regenerate the set sends %d bytes (the old hello: %t, the new file: %t), want the %d-byte file just written", len(sent), bytes.Equal(sent, old), bytes.Equal(sent, onDisk), len(old))
	}
	if entry := listedPayloadObject(t, mux, "tls", name); !reflect.DeepEqual(entry["warnings"], []any{}) || entry["filepath"] != file {
		t.Errorf("after regenerate the entry is listed at %v with warnings %v, want %s and none", entry["filepath"], entry["warnings"], file)
	}
}

func TestRegenerateTakesOnlyAGeneratedTLSPayload(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const uploaded = "uploaded.regenerate.example"
	if rec := uploadCapturePayload(t, mux, uploaded, "tls", config.FakeSNI2); rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	t.Cleanup(func() { manager.DeleteCapture("tls", uploaded) })

	if rec := regenerate(t, mux, uploaded); rec.Code != http.StatusConflict {
		t.Errorf("regenerating an uploaded payload answered %d %q, want 409", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if rec := regenerate(t, mux, "never-saved.regenerate.example"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "never-saved.regenerate.example") {
		t.Errorf("regenerating an unknown name answered %d %q, want 404 naming it", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodGet, "/api/capture/regenerate", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/capture/regenerate answered %d, want 405", rec.Code)
	}
}
