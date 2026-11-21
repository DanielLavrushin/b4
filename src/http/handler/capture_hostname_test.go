package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/sni"
)

func generateRequest(t *testing.T, mux *http.ServeMux, domain string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"domain": domain})
	rec, panicked := serveRecovered(mux, httptest.NewRequest(http.MethodPost, "/api/capture/generate", bytes.NewReader(body)))
	if panicked != "" {
		t.Fatalf("generate %q panicked: %s", domain, panicked)
	}
	return rec
}

func TestGenerateTakesOnlyAHostName(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())

	for _, input := range []string{"https://", "   ", "http:///path", "not a host!", "[::1]:443", "bad\xffbyte.example"} {
		if rec := generateRequest(t, mux, input); rec.Code != http.StatusBadRequest {
			t.Errorf("generate %q answered %d %q, want 400", input, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}

	for _, c := range []struct{ input, host string }{
		{"https://gen-port.hostname.example:443/path?q=1", "gen-port.hostname.example"},
		{"gen-bare-port.hostname.example:8443", "gen-bare-port.hostname.example"},
		{"  Gen-Case.HostName.Example.  ", "gen-case.hostname.example"},
		{"пример.рф", "xn--e1afmkfd.xn--p1ai"},
		{"http://www.пример.рф/путь", "www.xn--e1afmkfd.xn--p1ai"},
	} {
		manager.DeleteCapture("tls", c.host)
		t.Cleanup(func() { manager.DeleteCapture("tls", c.host) })
		rec := generateRequest(t, mux, c.input)
		if rec.Code != http.StatusOK {
			t.Errorf("generate %q answered %d %q, want 200", c.input, rec.Code, strings.TrimSpace(rec.Body.String()))
			continue
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("generate %q answered %q: %v", c.input, rec.Body.String(), err)
		}
		if out["domain"] != c.host {
			t.Errorf("generate %q answered domain %v, want %q", c.input, out["domain"], c.host)
		}
		saved, ok := manager.GetCapture("tls", c.host)
		if !ok {
			var listed []string
			for _, l := range manager.ListCaptures() {
				listed = append(listed, l.Domain)
			}
			t.Errorf("generate %q saved no payload named %q; listed: %q", c.input, c.host, listed)
			continue
		}
		data, err := manager.LoadCaptureData(saved)
		if err != nil {
			t.Fatal(err)
		}
		if host, _, _ := sni.ParseTLSClientHelloSNI(data); host != c.host || bytes.Contains(data, []byte(":443")) {
			t.Errorf("generate %q wrote a ClientHello whose server_name reads %q, want %q", c.input, host, c.host)
		}
	}
}
