package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func reuseConfig(t *testing.T) *Config {
	t.Helper()
	c := NewConfig()
	s := NewSetConfig()
	s.Id, s.Name, s.Enabled = "reuse", "reuse", true
	s.Targets.IPs = []string{"203.0.113.2/32"}
	s.Targets.IpsToMatch = []string{"203.0.113.2/32"}
	s.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	s.TCP.WSSizeReuse.Enabled = true
	c.Sets = []*SetConfig{&s}
	return &c
}

func TestWSReuseSparseAndOldConfig(t *testing.T) {
	c := reuseConfig(t)
	raw, err := MarshalSparse(c)
	if err != nil {
		t.Fatal(err)
	}
	next := NewConfig()
	if err := json.Unmarshal(raw, &next); err != nil {
		t.Fatal(err)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if !next.Sets[0].ReusesWSSize() || next.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds != DefaultWSReuseMaxAgeSeconds {
		t.Fatalf("round trip: %+v", next.Sets[0].TCP)
	}
	legacy := NewConfig()
	if err := json.Unmarshal([]byte(`{"sets":[{"id":"legacy","tcp":{"win":{"mode":"zero"},"wssize":{"enabled":true,"window":0,"scale":0}}}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Validate(); err != nil {
		t.Fatal(err)
	}
	if legacy.Sets[0].ReusesWSSize() || legacy.Sets[0].TCP.Win.Mode != "zero" || legacy.Sets[0].TCP.WSSize.Window != 0 {
		t.Fatal("old configuration changed")
	}
}

func TestWSReuseStaysOffWithoutNFQUEUEFirewall(t *testing.T) {
	for _, c := range []struct {
		name   string
		edit   func(*Config)
		reason string
		warns  bool
	}{
		{"the TUN engine", func(c *Config) { c.Queue.Mode = "tun" }, "tun", true},
		{"firewall setup skipped", func(c *Config) { c.System.Tables.SkipSetup = true }, "skip_setup", true},
		{"a queue mark with the relay bit 0x40000", func(c *Config) { c.Queue.Mark = 0x48000 }, "queue_mark", true},
		{"a queue mark with the relay bit 0x200000", func(c *Config) { c.Queue.Mark = 0x208000 }, "queue_mark", true},
		{"a Discovery run", func(c *Config) { c.Queue.IsDiscovery = true }, "discovery", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := reuseConfig(t)
			cfg.Sets[0].Name = "Video edges"
			c.edit(cfg)
			warnings := captureEgressGatewayWarnings(t)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("%s: a reuse set was refused (%v); the set is valid, only the engine keeps reuse from working, so it must load and get its usual strategy", c.name, err)
			}
			if got := cfg.WSReuseInactiveReason(); got != c.reason {
				t.Errorf("%s: inactive reason %q, want %q", c.name, got, c.reason)
			}
			if cfg.WSReuseActive(cfg.Sets[0]) {
				t.Errorf("%s: reuse counts as active", c.name)
			}
			if view := cfg.RoutingSets()[0]; view.Routing.Enabled || view.Routing.Mode == RoutingModeWSReuse {
				t.Errorf("%s: the routing view still diverts the set to the reuse relay (enabled=%v mode=%q)", c.name, view.Routing.Enabled, view.Routing.Mode)
			}
			if ids := wsReuseRoutingShape(cfg); len(ids) != 0 {
				t.Errorf("%s: the firewall shape still lists reuse sets %v", c.name, ids)
			}
			warned := 0
			for _, line := range *warnings {
				if strings.Contains(line, "Reuse WSSIZE connection") {
					warned++
					if !strings.Contains(line, "'Video edges'") || !strings.Contains(line, "does nothing while") || !strings.Contains(line, "the set's connections get its usual strategy") {
						t.Errorf("%s: the warning does not name the set, the cause and what its connections get: %q", c.name, line)
					}
				}
			}
			if want := map[bool]int{true: 1, false: 0}[c.warns]; warned != want {
				t.Errorf("%s: %d Reuse WSSIZE connection warnings, want %d: %q", c.name, warned, want, *warnings)
			}
		})
	}
	active := reuseConfig(t)
	warnings := captureEgressGatewayWarnings(t)
	if err := active.Validate(); err != nil {
		t.Fatal(err)
	}
	if active.WSReuseInactiveReason() != "" || !active.WSReuseActive(active.Sets[0]) || active.RoutingSets()[0].Routing.Mode != RoutingModeWSReuse || len(wsReuseRoutingShape(active)) != 1 || len(*warnings) != 0 {
		t.Fatalf("an NFQUEUE engine with firewall setup does not keep reuse active: reason=%q warnings=%q", active.WSReuseInactiveReason(), *warnings)
	}
	active.Sets[0].TCP.WSSizeReuse.Enabled = false
	if active.WSReuseActive(active.Sets[0]) {
		t.Fatal("a set with reuse off counts as reusing")
	}
}

func TestWSReuseRejectsUnsupportedPaths(t *testing.T) {
	cases := map[string]func(*Config){
		"no window":       func(c *Config) { c.Sets[0].TCP.WSSize.Window = 0 },
		"off":             func(c *Config) { c.Sets[0].TCP.WSSize.Enabled = false },
		"domain only":     func(c *Config) { c.Sets[0].Targets.DomainOnly = true },
		"domain":          func(c *Config) { c.Sets[0].Targets.SNIDomains = []string{"example.org"} },
		"tls":             func(c *Config) { c.Sets[0].Targets.TLSVersion = "1.3" },
		"no address":      func(c *Config) { c.Sets[0].Targets.IPs = nil; c.Sets[0].Targets.IpsToMatch = nil },
		"fresh set ports": func(c *Config) { c.Sets[0].TCP.DPortFilter = "8443" },
		"negative age":    func(c *Config) { c.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds = -1 },
		"large age":       func(c *Config) { c.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds = MaxWSReuseMaxAgeSeconds + 1 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c := reuseConfig(t)
			change(c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted unsupported configuration")
			}
		})
	}
}

func TestWSReuseWarmAgeDefaultAndCap(t *testing.T) {
	if got := NewSetConfig().TCP.WSSizeReuse.MaxAgeSeconds; got != 120 {
		t.Fatalf("a new set's warm validity is %d s, want 120", got)
	}
	unset := reuseConfig(t)
	unset.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds = 0
	if err := unset.Validate(); err != nil || unset.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds != 120 {
		t.Fatalf("an unset warm validity became %d s (%v), want 120", unset.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds, err)
	}
	top := reuseConfig(t)
	top.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds = 300
	if err := top.Validate(); err != nil {
		t.Fatalf("a warm validity of 300 s was refused: %v", err)
	}
	for name, change := range map[string]func(*SetConfig){
		"reusing set at 301":     func(s *SetConfig) { s.TCP.WSSizeReuse.MaxAgeSeconds = 301 },
		"reuse off at 301":       func(s *SetConfig) { s.TCP.WSSizeReuse.Enabled = false; s.TCP.WSSizeReuse.MaxAgeSeconds = 301 },
		"disabled set at 100000": func(s *SetConfig) { s.Enabled = false; s.TCP.WSSizeReuse.MaxAgeSeconds = 100000 },
	} {
		t.Run(name, func(t *testing.T) {
			c := reuseConfig(t)
			change(c.Sets[0])
			err := c.Validate()
			if err == nil {
				t.Fatal("a warm validity above 300 s was accepted")
			}
			field := findField(mustValidationErr(t, err), "sets[0].tcp.wssize_reuse.max_age_seconds", "out_of_range")
			if field == nil || field.Params["max"] != 300 {
				t.Fatalf("got %v, want out_of_range at sets[0].tcp.wssize_reuse.max_age_seconds with max 300", err)
			}
		})
	}
}

func TestWSReuseRoutingViewAndApply(t *testing.T) {
	c := reuseConfig(t)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	set := c.Sets[0]
	view := c.RoutingSets()[0]
	if view == set || !view.Routing.Enabled || !RoutingUsesTProxy(view.Routing.Mode) || set.Routing.Enabled {
		t.Fatal("internal route mutated user set")
	}
	if len(view.Targets.IpsToMatch) != 1 || view.Id != set.Id {
		t.Fatal("lost native address selector")
	}
	off := c.Clone()
	off.Sets[0].TCP.WSSizeReuse.Enabled = false
	if !FirewallRefreshNeeded(off, c) {
		t.Fatal("enabling reuse did not request firewall refresh")
	}
	age := c.Clone()
	age.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds = 200
	if FirewallRefreshNeeded(c, age) {
		t.Fatal("age change rebuilt firewall")
	}
	from := NewSetConfig()
	from.TCP.WSSizeReuse.Enabled = true
	from.TCP.WSSizeReuse.MaxAgeSeconds = 45
	off.Sets[0].AdoptStrategy(&from)
	if off.Sets[0].ReusesWSSize() {
		t.Fatal("discovered strategy enabled interception")
	}
	set.AdoptStrategy(&from)
	if !set.ReusesWSSize() || set.TCP.WSSizeReuse.MaxAgeSeconds != DefaultWSReuseMaxAgeSeconds || set.TCP.WSSize.Window != 1 {
		t.Fatal("adopting strategy replaced local reuse policy")
	}
}

func TestCollectedOnlyReuseConsumerStartsEmpty(t *testing.T) {
	c := reuseConfig(t)
	c.Sets[0].Targets.IPs = nil
	c.Sets[0].Targets.IpsToMatch = nil
	c.Sets[0].Targets.UseCollectedIPs = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.RoutingSets()[0].HasIPOrDomainTargets() {
		t.Fatal("empty pool armed a routing catch-all")
	}
	view := c.WithCollectedIPs([]string{"1.1.1.1"}).RoutingSets()[0]
	if !view.RoutingDivertsPackets() || len(view.TargetIPs()) != 1 {
		t.Fatal("runtime consumer did not become routable")
	}
}
