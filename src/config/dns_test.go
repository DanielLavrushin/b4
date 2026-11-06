package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDNSDefaultsWhenSectionAbsent(t *testing.T) {
	cfg := NewConfig()
	if err := json.Unmarshal([]byte(`{"system":{"logging":{"level":1}}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.System.DNS.TCPDisabled {
		t.Error("interception should default to on when the section is absent")
	}
	if got := cfg.DNSTCPListenPort(); got != DefaultDNSTCPPort {
		t.Errorf("port: got %d, want %d", got, DefaultDNSTCPPort)
	}
	if got := cfg.DNSQueryTimeout(); got != DefaultDNSQueryTimeoutSec*time.Second {
		t.Errorf("query timeout: got %v", got)
	}
}

func TestDNSExplicitDisableIsHonoured(t *testing.T) {
	cfg := NewConfig()
	if err := json.Unmarshal([]byte(`{"system":{"dns":{"tcp_disabled":true}}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.System.DNS.TCPDisabled {
		t.Error("explicit tcp_disabled=true must be honoured")
	}
}

func TestDNSZeroValueConfigKeepsInterceptionOn(t *testing.T) {
	var cfg Config
	if cfg.System.DNS.TCPDisabled {
		t.Error("a zero-value config must leave DNS over TCP enabled")
	}
}

func TestDNSZeroValuesFallBackToDefaults(t *testing.T) {
	cfg := NewConfig()
	cfg.System.DNS = DNSSystemConfig{}

	cases := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"query", cfg.DNSQueryTimeout(), DefaultDNSQueryTimeoutSec * time.Second},
		{"idle", cfg.DNSTCPIdleTimeout(), DefaultDNSTCPIdleSec * time.Second},
		{"io", cfg.DNSTCPIOTimeout(), DefaultDNSTCPIOSec * time.Second},
		{"dial", cfg.DNSTCPDialTimeout(), DefaultDNSTCPDialSec * time.Second},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if got := cfg.DNSTCPListenPort(); got != DefaultDNSTCPPort {
		t.Errorf("port: got %d, want %d", got, DefaultDNSTCPPort)
	}
}

func TestDNSTCPListenPortRejectsOutOfRange(t *testing.T) {
	cfg := NewConfig()
	for _, p := range []int{-1, 0, 65536, 99999} {
		cfg.System.DNS.TCPPort = p
		if got := cfg.DNSTCPListenPort(); got != DefaultDNSTCPPort {
			t.Errorf("port %d: got %d, want fallback %d", p, got, DefaultDNSTCPPort)
		}
	}
	cfg.System.DNS.TCPPort = 5300
	if got := cfg.DNSTCPListenPort(); got != 5300 {
		t.Errorf("valid port: got %d, want 5300", got)
	}
}

func TestDNSTCPInterceptEnabledNeedsBothFlagAndSet(t *testing.T) {
	cfg := NewConfig()
	if cfg.DNSTCPInterceptEnabled() {
		t.Error("no set with DNS redirect: should be disabled")
	}

	set := &SetConfig{Id: "s", Name: "s", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.DoHURL = "https://example.invalid/dns-query"
	cfg.Sets = []*SetConfig{set}

	if !cfg.DNSTCPInterceptEnabled() {
		t.Error("set with DoH and tcp_enabled=true: should be enabled")
	}

	cfg.System.DNS.TCPDisabled = true
	if cfg.DNSTCPInterceptEnabled() {
		t.Error("tcp_disabled=true must disable interception even with a DoH set")
	}
}

func TestDNSSectionOmittedFromSavedConfigAtDefaults(t *testing.T) {
	cfg := NewConfig()
	defaults := NewConfig()

	current, err := toMap(&cfg)
	if err != nil {
		t.Fatalf("toMap current: %v", err)
	}
	def, err := toMap(&defaults)
	if err != nil {
		t.Fatalf("toMap defaults: %v", err)
	}

	out, err := json.Marshal(sparsifyMap(current, def))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "tcp_disabled") {
		t.Errorf("default dns settings should be omitted from a saved config, got: %s", out)
	}
}

func TestDNSPortCollidesWithOtherServices(t *testing.T) {
	newCfgWithDoH := func() Config {
		c := NewConfig()
		set := &SetConfig{Id: "s", Name: "s", Enabled: true}
		set.DNS.Enabled = true
		set.DNS.DoHURL = "https://example.invalid/dns-query"
		c.Sets = []*SetConfig{set}
		return c
	}

	t.Run("collides with web server", func(t *testing.T) {
		c := newCfgWithDoH()
		c.System.DNS.TCPPort = c.System.WebServer.Port
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "system.dns.tcp_port") {
			t.Errorf("expected a dns port collision error, got: %v", err)
		}
	})

	t.Run("collides with socks5", func(t *testing.T) {
		c := newCfgWithDoH()
		c.System.Socks5.Enabled = true
		c.System.Socks5.Port = 5453
		c.System.DNS.TCPPort = 5453
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "5453") {
			t.Errorf("expected a dns/socks5 port collision, got: %v", err)
		}
	})

	t.Run("no collision on distinct ports", func(t *testing.T) {
		c := newCfgWithDoH()
		c.System.DNS.TCPPort = 5453
		if err := c.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("disabled dns tcp does not reserve the port", func(t *testing.T) {
		c := newCfgWithDoH()
		c.System.DNS.TCPDisabled = true
		c.System.DNS.TCPPort = c.System.WebServer.Port
		if err := c.Validate(); err != nil {
			t.Errorf("disabled dns tcp should not collide, got: %v", err)
		}
	})

	t.Run("out of range port is rejected", func(t *testing.T) {
		c := newCfgWithDoH()
		c.System.DNS.TCPPort = 70000
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "system.dns.tcp_port") {
			t.Errorf("expected out_of_range error, got: %v", err)
		}
	})
}

func TestDNSTCPInterceptEnabledForAnECHStripSet(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "s", Name: "s", Enabled: true}
	set.DNS.StripECH = true
	cfg.Sets = []*SetConfig{set}

	if !cfg.DNSTCPInterceptEnabled() {
		t.Error("a set that strips ECH keys needs the answers that come over TCP too")
	}
	set.Enabled = false
	if cfg.DNSTCPInterceptEnabled() {
		t.Error("a disabled set must not turn interception on")
	}
	set.Enabled = true
	cfg.System.DNS.TCPDisabled = true
	if cfg.DNSTCPInterceptEnabled() {
		t.Error("tcp_disabled=true must win over a set that strips ECH")
	}
}

func TestDNSTCPInterceptEnabledForAFakeOnlySet(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "s", Name: "s", Enabled: true}
	set.DNS.FakeIP.Enabled = true
	set.DNS.FakeIP.CIDR = "198.18.0.0/15"
	cfg.Sets = []*SetConfig{set}

	if set.DNS.Enabled {
		t.Fatal("the fixture is wrong: dns.enabled must stay off")
	}
	if !cfg.HasDNSRedirect() {
		t.Error("a set that answers fake addresses must count as a DNS redirect")
	}
	if !cfg.DNSTCPInterceptEnabled() {
		t.Error("with DNS over TCP allowed, a fake-only set needs the listener and its redirect")
	}

	set.DNS.Enabled = true
	if !cfg.DNSTCPInterceptEnabled() {
		t.Error("fake answers with the redirect on: interception must stay on")
	}

	set.Enabled = false
	if cfg.HasDNSRedirect() || cfg.DNSTCPInterceptEnabled() {
		t.Error("a disabled set must not turn interception on")
	}
}

func TestUpstreamListKeepsTheURLOverThePlainTarget(t *testing.T) {
	cfg := &DNSConfig{
		Enabled:   true,
		DoHURL:    "https://doh.example/dns-query",
		TargetDNS: "198.51.100.1",
		Upstreams: []string{"198.51.100.2"},
	}
	list := cfg.UpstreamList()
	if len(list) != 2 || list[0] != "https://doh.example/dns-query" || list[1] != "198.51.100.2" {
		t.Fatalf("UpstreamList = %v, want the URL and the list only", list)
	}

	cfg.DoHURL = ""
	list = cfg.UpstreamList()
	if len(list) != 2 || list[0] != "198.51.100.1" || list[1] != "198.51.100.2" {
		t.Fatalf("UpstreamList without a URL = %v, want the plain target and the list", list)
	}
}
