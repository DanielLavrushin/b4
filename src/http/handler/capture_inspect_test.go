package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
)

func inspectCapturePayload(t *testing.T, mux *http.ServeMux, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "inspect.bin")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/capture/inspect", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec, panicked := serveRecovered(mux, req)
	if panicked != "" {
		t.Fatalf("inspect of %d bytes panicked: %s", len(data), panicked)
	}
	return rec
}

func captureFolderEntries(t *testing.T, manager *capture.Manager) []string {
	t.Helper()
	entries, err := os.ReadDir(manager.GetOutputPath())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestInspectDescribesAFileWithoutSavingIt(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	before := captureFolderEntries(t, manager)
	listedBefore := len(listedPayloads(t, mux))

	cases := []struct {
		what      string
		data      []byte
		kind      string
		protocol  string
		name      string
		fitsTLS   bool
		fitsQUIC  bool
		factsFits bool
	}{
		{"Preset: Google (classic)", config.FakeSNI1, "tls_client_hello", "tls", "www.google.com", true, true, true},
		{"QUIC preset 1", config.FakeQUIC1, "quic_initial", "quic", "www.google.com", true, true, true},
		{"Preset: STUN", config.FakeSTUN, "stun", "quic", "", true, true, true},
		{"bytes of no known kind", bytes.Repeat([]byte{0x5c, 0x2e, 0x91, 0x07}, 40), "other", "", "", true, true, true},
		{"a hello too large for one packet", payloadTestHello(0x61, config.MaxUDPFakeLen+1), "tls_client_hello", "tls", "", false, false, false},
		{"a QUIC Initial too large for a TCP packet", append(append([]byte{}, config.FakeQUIC2...), make([]byte, config.MaxTCPFakeLenInOnePacket-len(config.FakeQUIC2)+1)...), "quic_initial", "quic", "dbankcloud.ru", false, true, true},
	}
	for _, c := range cases {
		rec := inspectCapturePayload(t, mux, c.data)
		if rec.Code != http.StatusOK {
			t.Errorf("inspect %s answered %d %q, want 200", c.what, rec.Code, strings.TrimSpace(rec.Body.String()))
			continue
		}
		answer := answerObject(t, "inspect", rec)
		requireKeys(t, "the inspect answer", answer, "size", "facts", "protocol", "name", "fits", "limits")
		facts := objectAt(t, "the inspect answer", answer, "facts")
		fits := objectAt(t, "the inspect answer", answer, "fits")
		limits := objectAt(t, "the inspect answer", answer, "limits")
		if answer["size"] != float64(len(c.data)) || facts["kind"] != c.kind || answer["protocol"] != c.protocol || answer["name"] != c.name {
			t.Errorf("inspect %s answered size %v, kind %v, protocol %v, name %v; want %d, %s, %q, %q", c.what, answer["size"], facts["kind"], answer["protocol"], answer["name"], len(c.data), c.kind, c.protocol, c.name)
		}
		if fits["tls"] != c.fitsTLS || fits["quic"] != c.fitsQUIC || facts["fits_one_packet"] != c.factsFits {
			t.Errorf("inspect %s answered fits %v and facts.fits_one_packet %v, want tls %t, quic %t, facts %t", c.what, fits, facts["fits_one_packet"], c.fitsTLS, c.fitsQUIC, c.factsFits)
		}
		want := map[string]any{"tls": float64(config.MaxTCPFakeLenInOnePacket), "quic": float64(config.MaxUDPFakeLen), "file": float64(config.MaxCapturePayloadBytes)}
		if !reflect.DeepEqual(limits, want) {
			t.Errorf("inspect %s answered limits %v, want %v", c.what, limits, want)
		}
	}

	rec := inspectCapturePayload(t, mux, nil)
	if answer := answerObject(t, "inspect of an empty file", rec); rec.Code != http.StatusOK || answer["size"] != float64(0) || answer["protocol"] != "" {
		t.Errorf("inspect of an empty file answered %d %v, want 200 with size 0 and no protocol", rec.Code, answer)
	}
	rec = inspectCapturePayload(t, mux, make([]byte, config.MaxCapturePayloadBytes+1))
	if text := rec.Body.String(); rec.Code != http.StatusBadRequest || !strings.Contains(text, strconv.Itoa(config.MaxCapturePayloadBytes)) {
		t.Errorf("inspect of a file over the limit answered %d %q, want 400 naming the limit", rec.Code, strings.TrimSpace(text))
	}

	if after := captureFolderEntries(t, manager); !reflect.DeepEqual(after, before) || len(listedPayloads(t, mux)) != listedBefore {
		t.Errorf("inspect saved something: the captures folder went from %q to %q", before, after)
	}
}

func TestAnUploadOverAnExistingNameAsksFirst(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const used, unused = "used.replace.example", "unused.replace.example"
	first, second := payloadTestHello(0x71, 300), payloadTestHello(0x72, 320)
	for _, name := range []string{used, unused} {
		t.Cleanup(func() {
			manager.DeleteCapture("tls", name)
			manager.DeleteCapture("quic", name)
		})
		if rec := uploadCapturePayload(t, mux, name, "tls", first); rec.Code != http.StatusOK {
			t.Fatalf("first upload of %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	saved, _ := manager.GetCapture("tls", used)
	set := namedSet("replace", "replace set", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = saved.Filepath
	useSets(api, set)

	for name, usedBy := range map[string][]any{used: {"replace set"}, unused: {}} {
		rec := uploadCapturePayload(t, mux, name, "tls", second)
		if rec.Code != http.StatusConflict {
			t.Errorf("an upload over the existing %s without replace answered %d %q, want 409", name, rec.Code, strings.TrimSpace(rec.Body.String()))
			continue
		}
		answer := answerObject(t, "the 409 of upload", rec)
		requireKeys(t, "the 409 of upload", answer, "code", "used_by")
		if answer["code"] != "exists" || !reflect.DeepEqual(answer["used_by"], usedBy) {
			t.Errorf("the 409 for %s answered %v, want code exists and used_by %v", name, answer, usedBy)
		}
	}
	if got := runningSet(api).Faking.PayloadData; !bytes.Equal(got, first) {
		t.Errorf("a refused upload changed what the set sends (%d bytes)", len(got))
	}
	if c, _ := manager.GetCapture("tls", used); c.Size != len(first) {
		t.Errorf("a refused upload changed the listed size to %d", c.Size)
	}
	if rec := uploadCapturePayload(t, mux, used, "quic", second); rec.Code != http.StatusOK {
		t.Errorf("the same name as a UDP payload is a payload of its own, got %d %s", rec.Code, rec.Body.String())
	}

	rec := replaceCapturePayload(t, mux, used, "tls", second)
	if rec.Code != http.StatusOK {
		t.Fatalf("an upload with replace=1 answered %d %s", rec.Code, rec.Body.String())
	}
	if got := reloadedBy(t, rec); !reflect.DeepEqual(got, []any{"replace set"}) {
		t.Errorf("the confirmed replace answered reloaded=%v", got)
	}
	if got := runningSet(api).Faking.PayloadData; !bytes.Equal(got, second) {
		t.Errorf("after the confirmed replace the set sends %d bytes, not the new %d", len(got), len(second))
	}
}
