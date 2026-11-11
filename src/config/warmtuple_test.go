package config

import (
	"encoding/json"
	"testing"
)

func warmTupleConfig() Config {
	c := NewConfig()
	s := NewSetConfig()
	s.Id = "warm"
	s.Targets.SNIDomains = []string{"googlevideo.com"}
	s.Targets.DomainsToMatch = []string{"googlevideo.com"}
	s.TCP.WarmTuple.Enabled = true
	s.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1, Cutoff: "client_hello"}
	c.Sets = []*SetConfig{&s}
	return c
}

func TestWarmTupleValidation(t *testing.T) {
	c := warmTupleConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"tun":       func(c *Config) { c.Queue.Mode = "tun" },
		"ipv6":      func(c *Config) { c.Queue.IPv4Enabled = false },
		"interface": func(c *Config) { c.Queue.Interfaces = []string{"br-lan"} },
		"routing":   func(c *Config) { c.Sets[0].Routing.Enabled = true },
		"cutoff":    func(c *Config) { c.Sets[0].TCP.WSSize.Cutoff = "" },
		"tls":       func(c *Config) { c.Sets[0].Targets.TLSVersion = "1.3" },
		"ports": func(c *Config) {
			c.System.Socks5.WarmTuple.FirstPort = 65535
			c.System.Socks5.WarmTuple.LastPort = 65536
		},
		"timeout": func(c *Config) { c.System.Socks5.WarmTuple.WarmTimeoutMs = -1 },
		"delay":   func(c *Config) { c.System.Socks5.WarmTuple.ReconnectDelayMs = 10001 },
		"target":  func(c *Config) { c.System.Socks5.WarmTuple.Targets = map[string]string{"a.googlevideo.com": "::1"} },
	} {
		t.Run(name, func(t *testing.T) {
			c := warmTupleConfig()
			change(&c)
			if c.Validate() == nil {
				t.Fatal("accepted unsupported configuration")
			}
		})
	}
}

func TestWarmTupleSparseRoundTrip(t *testing.T) {
	c := warmTupleConfig()
	c.System.Socks5.WarmTuple.CompactHello = true
	c.System.Socks5.WarmTuple.Targets = map[string]string{"r.googlevideo.com": "203.0.113.2"}
	b, err := MarshalSparse(&c)
	if err != nil {
		t.Fatal(err)
	}
	d := NewConfig()
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if !d.Sets[0].TCP.WarmTuple.Enabled || !d.System.Socks5.WarmTuple.CompactHello || d.System.Socks5.WarmTuple.ReconnectDelayMs != 0 {
		t.Fatal("settings changed during round trip")
	}
	if NewSetConfig().TCP.WarmTuple.Enabled {
		t.Fatal("enabled by default")
	}
	if got := d.System.Socks5.WarmTuple.Effective(); got.WarmTimeoutMs != 15000 || got.ReuseTimeoutMs != 10000 || got.FirstPort != 61000 || got.LastPort != 61999 {
		t.Fatalf("bad effective defaults: %+v", got)
	}
}

func TestWarmTupleAddressOnlyValidation(t *testing.T) {
	for _, targets := range []TargetsConfig{
		{GeoIpCategories: []string{"BLOCKED_TLS_RU"}, IPPriority: true},
		{IPs: []string{"203.0.113.2/32"}, IPPriority: true},
	} {
		c := warmTupleConfig()
		c.System.Geo.GeoIpPath = "/tmp/googlevideo.dat"
		c.Sets[0].Targets = targets
		if err := c.Validate(); err != nil {
			t.Fatal("rejected native address selector", err)
		}
		c.Sets[0].Targets.DomainOnly = true
		if c.Validate() == nil {
			t.Fatal("accepted address-only selector with domain-only mode")
		}
	}
	c := warmTupleConfig()
	c.Sets[0].Targets = TargetsConfig{}
	if c.Validate() == nil {
		t.Fatal("accepted unbounded warm selection")
	}
}
