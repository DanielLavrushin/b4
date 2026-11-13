package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
)

func convertApplyAPI(t *testing.T) (*http.ServeMux, *config.Config) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = hubTestConfigPath(t)
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterConvertApi()
	t.Cleanup(func() { capture.GetManager(&cfg).DeleteCapture("tls", "byedpi-fake") })
	return mux, &cfg
}

const binaryFakeLine = `-f-1 -l :\\x16\\x03\\x01\\xfc\\x00\\xa5`

var binaryFake = []byte{0x16, 0x03, 0x01, 0xfc, 0x00, 0xa5}

func TestConvertApplyKeepsEveryByteOfAnInlineFakeAcrossASave(t *testing.T) {
	mux, cfg := convertApplyAPI(t)

	rec := postConvert(t, mux, "/api/convert/apply", `{"text":"`+binaryFakeLine+`","domains":["example.com"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	var applied ConvertApplyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if len(applied.Sets) != 1 || !strings.HasPrefix(applied.Sets[0].Faking.PayloadFile, "captures/") {
		t.Fatalf("the applied set must name a file under captures/, got %+v", applied.Sets)
	}

	reloaded := config.NewConfig()
	reloaded.ConfigPath = cfg.ConfigPath
	if err := reloaded.LoadFromFile(cfg.ConfigPath); err != nil {
		t.Fatalf("reload the saved config: %v", err)
	}
	if len(reloaded.Sets) != 1 {
		t.Fatalf("saved config holds %d sets, want 1", len(reloaded.Sets))
	}
	f := reloaded.Sets[0].Faking
	if f.SNIType != config.FakePayloadCapture || f.CustomPayload != "" {
		t.Fatalf("sni_type %d, custom_payload %q: the fake must be a payload file, a JSON string loses bytes that are not text",
			f.SNIType, f.CustomPayload)
	}
	got, err := reloaded.ReadCapturePayload(f.PayloadFile)
	if err != nil {
		t.Fatalf("read %s: %v", f.PayloadFile, err)
	}
	if !bytes.Equal(got, binaryFake) {
		t.Fatalf("payload after a save and reload = % x, want % x", got, binaryFake)
	}
}

func TestConvertApplyKeepsEveryByteOfAHexQUICFakeAcrossASave(t *testing.T) {
	mux, cfg := convertApplyAPI(t)
	t.Cleanup(func() { capture.GetManager(cfg).DeleteCapture("quic", "zapret-fake") })

	body := `{"text":"--filter-udp=443 --dpi-desync=fake --dpi-desync-fake-quic=0x0d0a0bfc","domains":["example.com"]}`
	rec := postConvert(t, mux, "/api/convert/apply", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}

	reloaded := config.NewConfig()
	reloaded.ConfigPath = cfg.ConfigPath
	if err := reloaded.LoadFromFile(cfg.ConfigPath); err != nil {
		t.Fatalf("reload the saved config: %v", err)
	}
	if len(reloaded.Sets) != 1 {
		t.Fatalf("saved config holds %d sets, want 1", len(reloaded.Sets))
	}
	file := reloaded.Sets[0].UDP.FakePayloadFile
	if !strings.HasPrefix(file, "captures/quic_") {
		t.Fatalf("udp.fake_payload_file = %q, want a QUIC payload under captures/", file)
	}
	got, err := reloaded.ReadCapturePayload(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	if want := []byte{0x0d, 0x0a, 0x0b, 0xfc}; !bytes.Equal(got, want) {
		t.Fatalf("payload after a save and reload = % x, want % x", got, want)
	}
}

func TestConvertAnalyzeSavesNoPayload(t *testing.T) {
	mux, cfg := convertApplyAPI(t)
	manager := capture.GetManager(cfg)
	before := len(manager.ListCaptures())

	rec := postConvert(t, mux, "/api/convert/analyze", `{"text":"`+binaryFakeLine+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("analyze: %d %s", rec.Code, rec.Body.String())
	}
	if after := len(manager.ListCaptures()); after != before {
		t.Fatalf("analyze is a preview and must not write payloads: %d captures before, %d after", before, after)
	}
}
