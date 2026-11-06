package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
)

func TestCaptureListNamesTheSetsUsingEachCapture(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = hubTestConfigPath(t)
	manager := capture.GetManager(&cfg)

	const tlsDomain, quicDomain, freeDomain = "used-over-tcp.example", "used-over-udp.example", "used-by-nobody.example"
	saved := func(protocol, domain string) string {
		t.Helper()
		file, err := manager.SaveImportedPayload(protocol, domain, []byte(protocol+" payload of "+domain))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { manager.DeleteCapture(protocol, domain) })
		return file
	}
	tlsFile, quicFile := saved("tls", tlsDomain), saved("quic", quicDomain)
	saved("tls", freeDomain)

	tcpFake := namedSet("a", "tcp fake", true, "a.example")
	tcpFake.Faking.SNIType = config.FakePayloadCapture
	tcpFake.Faking.PayloadFile = tlsFile
	switchedOff := namedSet("b", "disabled set", false, "b.example")
	switchedOff.Faking.SNIType = config.FakePayloadCapture
	switchedOff.Faking.PayloadFile = "./" + tlsFile
	udpFake := namedSet("c", "udp fake", true, "c.example")
	udpFake.UDP.FakePayloadFile = quicFile
	stale := namedSet("d", "stale name", true, "d.example")
	stale.Faking.SNIType = config.FakePayloadDefault1
	stale.Faking.PayloadFile = tlsFile
	cfg.Sets = []*config.SetConfig{tcpFake, switchedOff, udpFake, stale}

	api := &API{cfgPtr: testCfgPtr(&cfg)}
	rec := httptest.NewRecorder()
	api.handleListCaptures(rec, httptest.NewRequest(http.MethodGet, "/api/capture/list", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d (%s)", rec.Code, rec.Body.String())
	}
	var listed []capture.Capture
	decodeInto(t, rec, &listed)

	usedBy := map[string][]string{}
	for _, c := range listed {
		usedBy[c.Domain] = c.UsedBy
	}
	if want := []string{"tcp fake", "disabled set"}; !reflect.DeepEqual(usedBy[tlsDomain], want) {
		t.Errorf("TLS capture used_by = %v, want %v", usedBy[tlsDomain], want)
	}
	if want := []string{"udp fake"}; !reflect.DeepEqual(usedBy[quicDomain], want) {
		t.Errorf("QUIC capture used_by = %v, want %v", usedBy[quicDomain], want)
	}
	if got, ok := usedBy[freeDomain]; !ok || got != nil {
		t.Errorf("a capture no set names has no used_by, got %v (listed %t)", got, ok)
	}
}
