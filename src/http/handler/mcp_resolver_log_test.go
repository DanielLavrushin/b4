package handler

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestMCPLogMasksAResolverURLInTheAddressField(t *testing.T) {
	for target, want := range map[string]string{
		"https://canaryuser:canarypass@dns.example/canarypath?id=canaryquery": "https://dns.example",
		"https://canaryuser:canary^pass@dns.example/":                         "https://[redacted]",
		"dns.example/canarypath?id=canaryquery":                               "[redacted]",
		"quic://canaryuser:canarypass@dns.example":                            "[redacted]",
		"192.0.2.1:53": "192.0.2.1:53",
		"192.0.2.1":    "192.0.2.1",
		"":             "",
	} {
		if got := mcpLogValue("sets[video].dns.target_dns", target); got != want {
			t.Errorf("target_dns %q is logged as %q, want %q", target, got, want)
		}
	}
}

func TestAResolverWrittenOverMCPIsLoggedByItsHost(t *testing.T) {
	cfg := writableCfg(t)
	cfg.Sets[0].DNS = config.DNSConfig{Enabled: true, TargetDNS: "9.9.9.9"}
	srv, _ := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	logs := captureLog(t)

	first := "https://dns.example/canaryfirst"
	second := "https://dns.example:8443/canarysecond?token=canaryquery"
	login := "https://canaryuser,canary:canarypass@dns.example/canarypath"
	for _, w := range []struct{ path, value string }{
		{"sets[video].dns.doh_url", first},
		{"sets[video].dns.doh_url", second},
		{"sets[video].dns.doh_url", login},
		{"sets[video].dns.target_dns", first},
		{" sets[video].dns.target_dns ", "9.9.9.9"},
		{"sets[video].dns.upstreams", "1.1.1.1," + first},
		{"sets[video].dns.upstreams", "1.1.1.1," + first + "," + first},
		{"sets[video].dns.upstreams", first + ",1.1.1.1"},
		{"sets[video].tcp.seg2delay", "7"},
	} {
		if res := callSetValue(t, session, ctx, w.path, w.value); res.IsError {
			t.Fatalf("%s=%s: %s", w.path, w.value, mcpErrorText(res))
		}
	}
	if got := decodeSetValue(t, callSetValue(t, session, ctx, "sets[video].dns.doh_url", second)); got.Current != second || got.Previous != login {
		t.Errorf("the tool still answers with the stored addresses, got %+v", got)
	}
	for range 6 {
		decodeRevert(t, session, ctx)
	}

	text := logs.String()
	for _, want := range []string{
		"mcp: sets[video].dns.doh_url changed from  to https://dns.example\n",
		"mcp: sets[video].dns.doh_url changed from https://dns.example to https://dns.example:8443\n",
		"mcp: sets[video].dns.doh_url changed from https://dns.example:8443 to https://dns.example\n",
		"mcp: sets[video].dns.target_dns changed from 9.9.9.9 to https://dns.example\n",
		"changed from https://dns.example to 9.9.9.9\n",
		"mcp: sets[video].dns.upstreams changed from  to 1.1.1.1,https://dns.example\n",
		"mcp: sets[video].dns.upstreams rejected on save, still 1.1.1.1,https://dns.example\n",
		"mcp: sets[video].tcp.seg2delay changed from 0 to 7\n",
		"mcp: reverted sets[video].dns.upstreams back to 1.1.1.1,https://dns.example\n",
		"mcp: reverted sets[video].dns.doh_url back to https://dns.example\n",
		"mcp: reverted sets[video].tcp.seg2delay back to 0\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the log must say %q", want)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "canary") {
			t.Errorf("the log holds a private part of a resolver address: %s", line)
		}
	}
}
