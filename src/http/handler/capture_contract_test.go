package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tlsgen"
)

func legacyGeneratedFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "capture", "testdata", "legacy-generated-"+name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func answerObject(t *testing.T, what string, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s answered %d %q: %v", what, rec.Code, strings.TrimSpace(rec.Body.String()), err)
	}
	return out
}

func requireKeys(t *testing.T, what string, obj map[string]any, keys ...string) {
	t.Helper()
	var missing []string
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		var have []string
		for k := range obj {
			have = append(have, k)
		}
		sort.Strings(have)
		t.Errorf("%s lacks %q of the API contract; it has %q", what, missing, have)
	}
}

func objectAt(t *testing.T, what string, obj map[string]any, key string) map[string]any {
	t.Helper()
	inner, ok := obj[key].(map[string]any)
	if !ok {
		t.Errorf("%s.%s is %v, want an object", what, key, obj[key])
		return map[string]any{}
	}
	return inner
}

func TestThePayloadListEntryKeepsTheContractsNames(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const tlsName, quicName = "contract.list.example", "contract-quic.list.example"
	tlsFile, err := manager.SaveImportedPayload("tls", tlsName, legacyGeneratedFixture(t, "www.google.com"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SaveImportedPayload("quic", quicName, config.FakeQUIC1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		manager.DeleteCapture("tls", tlsName)
		manager.DeleteCapture("quic", quicName)
	})
	set := namedSet("contract", "contract set", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = tlsFile
	useSets(api, set)

	entry := listedPayloadObject(t, mux, "tls", tlsName)
	requireKeys(t, "a list entry", entry, "protocol", "domain", "timestamp", "size", "filepath", "origin", "facts", "warnings", "missing", "used_by")
	if entry["origin"] != "imported" {
		t.Errorf("a payload saved through SaveImportedPayload is listed with origin %v, want imported", entry["origin"])
	}
	facts := objectAt(t, "a list entry", entry, "facts")
	requireKeys(t, "the facts of a TLS ClientHello", facts, "kind", "sni", "extensions", "ech", "fits_one_packet", "faults")
	if facts["kind"] != "tls_client_hello" || facts["sni"] != "www.google.com" || facts["fits_one_packet"] != true {
		t.Errorf("the facts of a generated hello read %v", facts)
	}
	if got := entry["warnings"]; !reflect.DeepEqual(got, []any{"malformed"}) {
		t.Errorf("the old generator's hello is listed with warnings %v, want [malformed]", got)
	}

	quic := listedPayloadObject(t, mux, "quic", quicName)
	quicFacts := objectAt(t, "a QUIC list entry", quic, "facts")
	requireKeys(t, "the facts of a QUIC Initial", quicFacts, "kind", "quic_version", "sni", "fits_one_packet")
	if quic["protocol"] != "quic" || quicFacts["kind"] != "quic_initial" || quicFacts["quic_version"] != float64(1) {
		t.Errorf("QUIC preset 1 uploaded as a UDP payload is listed as %v with facts %v, want protocol quic, kind quic_initial, version 1", quic["protocol"], quicFacts)
	}
	for _, key := range []string{"extensions", "ech", "faults"} {
		if _, carried := quicFacts[key]; carried {
			t.Errorf("the facts of a QUIC Initial carry %q, which belongs to a TLS ClientHello only: %v", key, quicFacts)
		}
	}
	if warnings, ok := quic["warnings"].([]any); !ok || len(warnings) != 0 {
		t.Errorf("a payload with nothing to warn about must carry warnings [], got %v", quic["warnings"])
	}
}

func TestPayloadAnswersKeepTheContractsNames(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const generated, uploaded = "contract.generate.example", "contract.upload.example"
	t.Cleanup(func() {
		manager.DeleteCapture("tls", generated)
		manager.DeleteCapture("tls", uploaded)
	})

	rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodPost, "/api/capture/generate", strings.NewReader(`{"domain":"`+generated+`"}`)))
	answer := answerObject(t, "generate", rec)
	requireKeys(t, "the generate answer", answer, "success", "message", "size", "domain", "facts", "reloaded")
	if facts := objectAt(t, "the generate answer", answer, "facts"); facts["sni"] != generated || facts["kind"] != "tls_client_hello" {
		t.Errorf("the generate answer carries facts %v", facts)
	}

	rec = uploadCapturePayload(t, mux, uploaded, "tls", config.FakeSNI2)
	answer = answerObject(t, "upload", rec)
	requireKeys(t, "the upload answer", answer, "success", "message", "size", "domain", "protocol", "facts", "reloaded")
	if facts := objectAt(t, "the upload answer", answer, "facts"); facts["sni"] != "vk.com" {
		t.Errorf("the upload answer carries facts %v", facts)
	}

	rec, _ = serveRecovered(mux, httptest.NewRequest(http.MethodDelete, "/api/capture/delete?protocol=tls&domain="+uploaded, nil))
	requireKeys(t, "the delete answer", answerObject(t, "delete", rec), "success", "message", "reloaded")

	rec, _ = serveRecovered(mux, httptest.NewRequest(http.MethodPost, "/api/capture/clear", nil))
	requireKeys(t, "the clear answer", answerObject(t, "clear", rec), "success", "message", "reloaded")
}

func TestThePayloadListWarnsAboutWhatEachFileIs(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	fine, err := tlsgen.GenerateTLSClientHello("fine.warn.example")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		protocol, name string
		data           []byte
		want           []any
	}{
		{"tls", "fine.warn.example", fine, []any{}},
		{"tls", "large.warn.example", payloadTestHello(0x61, config.MaxTCPFakeLenInOnePacket+52), []any{"too_large", "malformed"}},
		{"tls", "quic-file.warn.example", config.FakeQUIC1, []any{"kind_mismatch"}},
		{"quic", "hello-over-udp.warn.example", config.FakeSNI1, []any{"kind_mismatch"}},
		{"quic", "random.warn.example", bytes.Repeat([]byte{0x5c, 0x2e, 0x91, 0x07}, 40), []any{"unrecognized"}},
		{"tls", "legacy.warn.example", legacyGeneratedFixture(t, "max.ru"), []any{"malformed"}},
		{"tls", "gone.warn.example", config.FakeSNI2, []any{"missing"}},
		{"tls", "empty.warn.example", config.FakeSNI2[:100], []any{"empty"}},
	}
	for _, c := range cases {
		file, err := manager.SaveImportedPayload(c.protocol, c.name, c.data)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(manager.GetOutputPath(), filepath.Base(file))
		t.Cleanup(func() {
			manager.DeleteCapture(c.protocol, c.name)
			os.Remove(path)
		})
		switch c.name {
		case "gone.warn.example":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "empty.warn.example":
			if err := os.Truncate(path, 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range cases {
		entry := listedPayloadObject(t, mux, c.protocol, c.name)
		if got := entry["warnings"]; !reflect.DeepEqual(got, c.want) {
			t.Errorf("the %s payload %s is listed with warnings %v, want %v (facts %v)", c.protocol, c.name, got, c.want, entry["facts"])
		}
	}
}
