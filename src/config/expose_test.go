package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func exposeTestConfig() *Config {
	cfg := NewConfig()
	return &cfg
}

func TestExposurePlanOfTheDefaultConfigOpensNothing(t *testing.T) {
	cfg := exposeTestConfig()
	ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
	if len(ports) != 0 || len(blocked) != 0 {
		t.Fatalf("defaults must expose nothing, got ports=%+v blocked=%+v", ports, blocked)
	}
}

func TestExposurePlanWildcardBindOpensBothFamilies(t *testing.T) {
	for _, bind := range []string{"", "0.0.0.0", "::", "[::]", " 0.0.0.0 "} {
		cfg := exposeTestConfig()
		cfg.System.MTProto.Enabled = true
		cfg.System.MTProto.Expose = true
		cfg.System.MTProto.BindAddress = bind
		ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
		want := []ExposedPort{{Service: ExposeMTProto, Port: cfg.System.MTProto.Port, V4: true, V6: true}}
		if !reflect.DeepEqual(ports, want) || len(blocked) != 0 {
			t.Errorf("bind %q: Go listens dual-stack on a wildcard, so both families must open; got ports=%+v blocked=%+v", bind, ports, blocked)
		}
	}
}

func TestExposurePlanSpecificBindOpensOnlyThatAddress(t *testing.T) {
	cases := []struct {
		bind string
		want ExposedPort
	}{
		{"192.168.1.1", ExposedPort{Service: ExposeSocks5, Port: 1080, Address: "192.168.1.1", V4: true}},
		{"::ffff:192.168.1.1", ExposedPort{Service: ExposeSocks5, Port: 1080, Address: "192.168.1.1", V4: true}},
		{"2001:db8::1", ExposedPort{Service: ExposeSocks5, Port: 1080, Address: "2001:db8::1", V6: true}},
		{"[2001:DB8::1]", ExposedPort{Service: ExposeSocks5, Port: 1080, Address: "2001:db8::1", V6: true}},
	}
	for _, tc := range cases {
		cfg := exposeTestConfig()
		cfg.System.WebServer.Username = "admin"
		cfg.System.WebServer.Password = "hash"
		cfg.System.Socks5.Enabled = true
		cfg.System.Socks5.Expose = true
		cfg.System.Socks5.Username = "u"
		cfg.System.Socks5.Password = "p"
		cfg.System.Socks5.Port = 1080
		cfg.System.Socks5.BindAddress = tc.bind
		ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
		if !reflect.DeepEqual(ports, []ExposedPort{tc.want}) || len(blocked) != 0 {
			t.Errorf("bind %q: got ports=%+v blocked=%+v, want %+v", tc.bind, ports, blocked, tc.want)
		}
	}
}

func TestExposurePlanRefusesBindsNobodyOutsideCanReach(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1":   ExposeBlockedLoopback,
		"127.0.0.53":  ExposeBlockedLoopback,
		"::1":         ExposeBlockedLoopback,
		"router.lan":  ExposeBlockedInvalidBind,
		"fe80::1%br0": ExposeBlockedInvalidBind,
	}
	for bind, reason := range cases {
		cfg := exposeTestConfig()
		cfg.System.MTProto.Enabled = true
		cfg.System.MTProto.Expose = true
		cfg.System.MTProto.BindAddress = bind
		ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
		want := []ExposeBlock{{Service: ExposeMTProto, Reason: reason}}
		if len(ports) != 0 || !reflect.DeepEqual(blocked, want) {
			t.Errorf("bind %q: got ports=%+v blocked=%+v, want blocked %+v", bind, ports, blocked, want)
		}
	}
}

func TestExposurePlanWebInterfaceNeedsBothCredentials(t *testing.T) {
	for _, creds := range [][2]string{{"", ""}, {"admin", ""}, {"", "hash"}} {
		cfg := exposeTestConfig()
		cfg.System.WebServer.Expose = true
		cfg.System.WebServer.Username = creds[0]
		cfg.System.WebServer.Password = creds[1]
		ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
		want := []ExposeBlock{{Service: ExposeWebServer, Reason: ExposeBlockedNoAuth}}
		if len(ports) != 0 || !reflect.DeepEqual(blocked, want) {
			t.Errorf("credentials %q: an unauthenticated admin interface must never be opened, got ports=%+v blocked=%+v", creds, ports, blocked)
		}
	}
}

func TestExposurePlanWebInterfaceFollowsTheRunningListener(t *testing.T) {
	cfg := exposeTestConfig()
	cfg.System.WebServer.Expose = true
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "hash"
	cfg.System.WebServer.Port = 9000
	cfg.System.WebServer.BindAddress = "10.0.0.1"

	ports, _ := cfg.ExposurePlan(WebListener{Port: 7000, BindAddress: "0.0.0.0"})
	want := []ExposedPort{{Service: ExposeWebServer, Port: 7000, V4: true, V6: true}}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("the web server binds once at startup, so the rule must follow the port it runs on; got %+v", ports)
	}

	if ports, blocked := cfg.ExposurePlan(WebListener{Port: 0}); len(ports) != 0 || len(blocked) != 0 {
		t.Fatalf("a disabled web server (port 0) has nothing to expose, got ports=%+v blocked=%+v", ports, blocked)
	}
}

func TestExposurePlanRefusesAnOpenSOCKS5Relay(t *testing.T) {
	cases := []struct {
		name     string
		user     string
		sources  []string
		expose   bool
		blocked  bool
		expected bool
	}{
		{name: "no credentials, no sources", blocked: true},
		{name: "blank source entries", sources: []string{"  ", ""}, blocked: true},
		{name: "source list", sources: []string{"203.0.113.0/24"}, expected: true},
		{name: "credentials", user: "u", expected: true},
	}
	for _, tc := range cases {
		cfg := exposeTestConfig()
		cfg.System.WebServer.Username = "admin"
		cfg.System.WebServer.Password = "hash"
		cfg.System.Socks5.Enabled = true
		cfg.System.Socks5.Expose = true
		cfg.System.Socks5.Username = tc.user
		if tc.user != "" {
			cfg.System.Socks5.Password = "p"
		}
		cfg.System.Socks5.AllowedSources = tc.sources
		ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
		if tc.blocked && (len(ports) != 0 || !reflect.DeepEqual(blocked, []ExposeBlock{{Service: ExposeSocks5, Reason: ExposeBlockedOpenRelay}})) {
			t.Errorf("%s: an open relay must not be exposed, got ports=%+v blocked=%+v", tc.name, ports, blocked)
		}
		if tc.expected && (len(ports) != 1 || ports[0].Service != ExposeSocks5 || len(blocked) != 0) {
			t.Errorf("%s: expected SOCKS5 to be exposed, got ports=%+v blocked=%+v", tc.name, ports, blocked)
		}
	}
}

func TestExposurePlanWebRelayNeedsItsOwnPort(t *testing.T) {
	cfg := exposeTestConfig()
	cfg.System.MTProto.Enabled = true
	cfg.System.MTProto.BindAddress = "203.0.113.5"
	cfg.System.MTProto.WebProxy.Enabled = true
	cfg.System.MTProto.WebProxy.Expose = true

	ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
	if len(ports) != 0 || !reflect.DeepEqual(blocked, []ExposeBlock{{Service: ExposeMTProtoWebProxy, Reason: ExposeBlockedSharedPort}}) {
		t.Fatalf("with port 0 the relay rides the web interface port and must not open it on its own, got ports=%+v blocked=%+v", ports, blocked)
	}

	cfg.System.MTProto.WebProxy.Port = 443
	ports, blocked = cfg.ExposurePlan(cfg.ConfiguredWebListener())
	want := []ExposedPort{{Service: ExposeMTProtoWebProxy, Port: 443, Address: "203.0.113.5", V4: true}}
	if !reflect.DeepEqual(ports, want) || len(blocked) != 0 {
		t.Fatalf("the relay listens on the MTProto bind address, got ports=%+v blocked=%+v", ports, blocked)
	}
}

func TestExposurePlanSkipsListenersThatAreOff(t *testing.T) {
	cfg := exposeTestConfig()
	cfg.System.MTProto.Expose = true
	cfg.System.MTProto.WebProxy.Enabled = true
	cfg.System.MTProto.WebProxy.Port = 443
	cfg.System.MTProto.WebProxy.Expose = true
	cfg.System.Socks5.Expose = true
	cfg.System.Socks5.Username = "u"
	cfg.System.Socks5.Password = "p"

	if ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener()); len(ports) != 0 || len(blocked) != 0 {
		t.Fatalf("a switch on a listener that is off opens nothing, got ports=%+v blocked=%+v", ports, blocked)
	}
}

func TestExposureAddedReportsOnlyNewOpenings(t *testing.T) {
	base := func() *Config {
		cfg := exposeTestConfig()
		cfg.System.MTProto.Enabled = true
		cfg.System.MTProto.Expose = true
		cfg.System.MTProto.BindAddress = "192.168.1.1"
		return cfg
	}

	same := base()
	if added := ExposureAdded(base(), same); len(added) != 0 {
		t.Errorf("an unchanged plan adds nothing, got %+v", added)
	}

	closed := base()
	closed.System.MTProto.Enabled = false
	if added := ExposureAdded(base(), closed); len(added) != 0 {
		t.Errorf("closing a port is not an addition, got %+v", added)
	}

	for name, mutate := range map[string]func(*Config){
		"enable":       func(c *Config) {},
		"new port":     func(c *Config) { c.System.MTProto.Port = 4443 },
		"wider bind":   func(c *Config) { c.System.MTProto.BindAddress = "0.0.0.0" },
		"other family": func(c *Config) { c.System.MTProto.BindAddress = "2001:db8::1" },
	} {
		old := base()
		if name == "enable" {
			old.System.MTProto.Enabled = false
		}
		next := base()
		mutate(next)
		if added := ExposureAdded(old, next); len(added) != 1 || added[0].Service != ExposeMTProto {
			t.Errorf("%s: expected the MTProto port to be reported as newly opened, got %+v", name, added)
		}
	}
}

func TestExposeFlagsSurviveASparseSaveAndStayOutWhenOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := exposeTestConfig()
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), `"expose"`) {
		t.Fatalf("expose is off by default and must be omitted from the sparse file:\n%s", raw)
	}

	cfg.System.WebServer.Expose = true
	cfg.System.MTProto.Expose = true
	cfg.System.MTProto.WebProxy.Expose = true
	cfg.System.Socks5.Expose = true
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loaded.System.WebServer.Expose || !loaded.System.MTProto.Expose || !loaded.System.MTProto.WebProxy.Expose || !loaded.System.Socks5.Expose {
		t.Fatalf("expose flags lost in the round trip: %+v %+v %+v %+v",
			loaded.System.WebServer.Expose, loaded.System.MTProto.Expose, loaded.System.MTProto.WebProxy.Expose, loaded.System.Socks5.Expose)
	}
}

func TestExposurePlanKeepsSOCKS5ClosedWhileTheWebInterfaceIsOpen(t *testing.T) {
	cfg := exposeTestConfig()
	cfg.System.Socks5.Enabled = true
	cfg.System.Socks5.Expose = true
	cfg.System.Socks5.Username = "u"
	cfg.System.Socks5.Password = "p"

	ports, blocked := cfg.ExposurePlan(cfg.ConfiguredWebListener())
	if len(ports) != 0 || !reflect.DeepEqual(blocked, []ExposeBlock{{Service: ExposeSocks5, Reason: ExposeBlockedWebNoAuth}}) {
		t.Fatalf("a SOCKS5 client can CONNECT to 127.0.0.1:7000, so an exposed proxy in front of an unauthenticated web interface hands out the admin API; got ports=%+v blocked=%+v", ports, blocked)
	}

	cfg.System.WebServer.Port = 0
	if ports, _ := cfg.ExposurePlan(WebListener{}); len(ports) != 1 {
		t.Fatalf("with the web interface off there is nothing to reach, got %+v", ports)
	}
	if ports, blocked := cfg.ExposurePlan(WebListener{Port: 7000}); len(ports) != 0 || len(blocked) != 1 || blocked[0].Reason != ExposeBlockedWebNoAuth {
		t.Fatalf("port 0 applies only after a restart; while the server still runs without login SOCKS5 must stay closed, got ports=%+v blocked=%+v", ports, blocked)
	}
}
