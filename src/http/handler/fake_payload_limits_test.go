package handler

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/convert"
)

func TestRESTSetCreateRefusesANegativeUDPFakeLen(t *testing.T) {
	api, mux := concurrentAdoptAPI(t)
	body := `{"name":"neg","targets":{"sni_domains":["example.org"]},"udp":{"mode":"fake","fake_seq_length":1,"fake_len":-1}}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sets", bytes.NewBufferString(body)))
	for _, s := range api.getCfg().Sets {
		if s.Name == "neg" && s.UDP.FakeLen < 0 {
			t.Fatalf("POST /api/sets answered %d and stored udp.fake_len=%d; the UDP fake builder panics on it", rec.Code, s.UDP.FakeLen)
		}
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("POST /api/sets with udp.fake_len=-1 answered %d, want a 4xx refusal: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "udp.fake_len") {
		t.Errorf("the refusal must name udp.fake_len: %s", rec.Body.String())
	}
}

func TestMCPWritesTheVKPresetByName(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[0].Faking.SNIType = config.FakePayloadDefault1
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callSetValue(t, session, ctx, "sets[video].faking.sni_type", "vk")
	if res.IsError {
		t.Fatalf("sni_type 3 is the vk.com hello and must be writable as \"vk\": %s", mcpErrorText(res))
	}
	if out := decodeSetValue(t, res); out.Current != "vk" {
		t.Errorf("the write must read back as \"vk\", got %q", out.Current)
	}
	if got := api.getCfg().Sets[0].Faking.SNIType; got != config.FakePayloadDefault2 {
		t.Errorf("stored sni_type = %d, want %d", got, config.FakePayloadDefault2)
	}
}

func TestConvertInstallRefusesATLSPayloadAboveTheImportLimit(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = hubTestConfigPath(t)
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	const name = "limit-check-fake"
	t.Cleanup(func() { capture.GetManager(&cfg).DeleteCapture("tls", name) })

	for _, c := range []struct {
		size      int
		installed bool
	}{
		{16385, false},
		{65484, false},
		{16384, true},
	} {
		set := config.NewSetConfig()
		set.Name = "imported"
		set.Faking.SNIType = config.FakePayloadCapture
		set.Faking.PayloadFile = convert.PendingPayloadPrefix + name
		set.Faking.PayloadData = bytes.Repeat([]byte{0x16}, c.size)

		err := api.installConvertPayloads([]*config.SetConfig{&set})
		if c.installed {
			if err != nil || strings.HasPrefix(set.Faking.PayloadFile, convert.PendingPayloadPrefix) {
				t.Fatalf("a %d-byte payload is within the import limit and must be saved: %v, file %q", c.size, err, set.Faking.PayloadFile)
			}
			continue
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "payload_install_failed" {
			t.Fatalf("a %d-byte TLS payload is above the import limit and must be refused, got %v (file %q)", c.size, err, set.Faking.PayloadFile)
		}
		if !strings.HasPrefix(set.Faking.PayloadFile, convert.PendingPayloadPrefix) {
			t.Errorf("a refused %d-byte payload must not be saved, yet the set names %q", c.size, set.Faking.PayloadFile)
		}
		if apiErr.Status != http.StatusBadRequest {
			t.Errorf("a payload over the limit is the request's fault, got status %d", apiErr.Status)
		}
	}
}
