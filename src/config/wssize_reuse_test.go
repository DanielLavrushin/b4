package config

import (
	"encoding/json"
	"os"
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

func TestWSReuseFollowsTheRunningTUNEngine(t *testing.T) {
	for _, saved := range []string{"", "nfqueue", "tun"} {
		t.Run("saved "+saved, func(t *testing.T) {
			SetRunningEngineTUN(true)
			t.Cleanup(func() { SetRunningEngineTUN(false) })
			cfg := reuseConfig(t)
			cfg.Queue.Mode = saved
			cfg.Sets[0].Name = "Video edges"
			warnings := captureEgressGatewayWarnings(t)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("a reuse set was refused (%v)", err)
			}
			if got := cfg.WSReuseInactiveReason(); got != "tun" {
				t.Errorf("queue.mode saved as %q while this process runs the TUN engine: inactive reason %q, want \"tun\"; the saved engine applies only after a restart", saved, got)
			}
			if cfg.WSReuseActive(cfg.Sets[0]) {
				t.Errorf("queue.mode saved as %q: reuse counts as active under the running TUN engine", saved)
			}
			if view := cfg.RoutingSets()[0]; view.Routing.Enabled || view.Routing.Mode == RoutingModeWSReuse {
				t.Errorf("queue.mode saved as %q: the routing view diverts the set to the reuse relay under the running TUN engine (enabled=%v mode=%q), so its TCP/443 loses the TUN engine's strategy until the restart", saved, view.Routing.Enabled, view.Routing.Mode)
			}
			if ids := wsReuseRoutingShape(cfg); len(ids) != 0 {
				t.Errorf("queue.mode saved as %q: the firewall shape lists reuse sets %v under the running TUN engine", saved, ids)
			}
			warned := false
			for _, line := range *warnings {
				if strings.Contains(line, "'Video edges'") && strings.Contains(line, "the engine is TUN") {
					warned = true
				}
			}
			if !warned {
				t.Errorf("queue.mode saved as %q: no warning says reuse does nothing while the engine is TUN: %q", saved, *warnings)
			}
		})
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

var wsReuseRefusalCodes = []string{
	"wssize_reuse_window",
	"wssize_reuse_targets",
	"wssize_reuse_domains",
	"wssize_reuse_tls",
	"wssize_reuse_port",
	"wssize_reuse_conflict",
	"wssize_reuse_age_range",
}

func uiErrorStrings(t *testing.T, locale string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../http/ui/src/i18n/" + locale + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	errs, _ := doc["errors"].(map[string]any)
	return errs
}

func TestWSReuseRefusalNamesTheSetAndTheCondition(t *testing.T) {
	escalating := func(c *Config) {
		other := NewSetConfig()
		other.Id, other.Name, other.Enabled = "other", "Other", true
		other.Targets.SNIDomains = []string{"example.net"}
		other.Targets.DomainsToMatch = []string{"example.net"}
		c.Sets = append(c.Sets, &other)
		c.Sets[0].Escalate.To = "other"
	}
	for _, c := range []struct {
		name  string
		edit  func(*Config)
		codes map[string]string
	}{
		{"a Window of 0", func(c *Config) { c.Sets[0].TCP.WSSize.Window = 0 }, map[string]string{"wssize_reuse_window": "sets[0].tcp.wssize.window"}},
		{"Real TCP window off", func(c *Config) { c.Sets[0].TCP.WSSize.Enabled = false }, map[string]string{"wssize_reuse_window": "sets[0].tcp.wssize.window"}},
		{"no IP targets", func(c *Config) { c.Sets[0].Targets.IPs = nil; c.Sets[0].Targets.IpsToMatch = nil }, map[string]string{"wssize_reuse_targets": "sets[0].targets.ip"}},
		{"domain-only matching", func(c *Config) { c.Sets[0].Targets.DomainOnly = true }, map[string]string{"wssize_reuse_domains": "sets[0].targets.sni_domains"}},
		{"a bypass domain", func(c *Config) { c.Sets[0].Targets.SNIDomains = []string{"example.org"} }, map[string]string{"wssize_reuse_domains": "sets[0].targets.sni_domains"}},
		{"a TLS version filter", func(c *Config) { c.Sets[0].Targets.TLSVersion = "1.3" }, map[string]string{"wssize_reuse_tls": "sets[0].targets.tls"}},
		{"a port filter without 443", func(c *Config) { c.Sets[0].TCP.DPortFilter = "8443" }, map[string]string{"wssize_reuse_port": "sets[0].tcp.dport_filter"}},
		{"escalation to another set", escalating, map[string]string{"wssize_reuse_conflict": "sets[0].tcp.wssize_reuse"}},
		{"a warm validity of -1", func(c *Config) { c.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds = -1 }, map[string]string{"wssize_reuse_age_range": "sets[0].tcp.wssize_reuse.max_age_seconds"}},
		{"a TLS version filter and a port filter together", func(c *Config) {
			c.Sets[0].Targets.TLSVersion = "1.3"
			c.Sets[0].TCP.DPortFilter = "8443"
		}, map[string]string{"wssize_reuse_tls": "sets[0].targets.tls", "wssize_reuse_port": "sets[0].tcp.dport_filter"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := reuseConfig(t)
			cfg.Sets[0].Name = "Video edges"
			c.edit(cfg)
			ve := mustValidationErr(t, cfg.Validate())
			for code, path := range c.codes {
				f := findField(ve, path, code)
				if f == nil {
					t.Errorf("%s: no %s at %s, got %+v", c.name, code, path, ve.Fields)
					continue
				}
				if f.Params["set"] != "Video edges" || !strings.HasPrefix(f.Message, `set "Video edges": `) {
					t.Errorf("%s: %s does not name the set: params %v, message %q", c.name, code, f.Params, f.Message)
				}
			}
			for _, f := range ve.Fields {
				if f.Code == "wssize_reuse_unsupported" {
					t.Errorf("%s: still refused with the catch-all code wssize_reuse_unsupported: %q", c.name, f.Message)
				}
			}
		})
	}
	refused := reuseConfig(t)
	refused.Queue.Mode = "tun"
	refused.Sets[0].TCP.WSSizeReuse.MaxAgeSeconds = -1
	warnings := captureEgressGatewayWarnings(t)
	if findField(mustValidationErr(t, refused.Validate()), "sets[0].tcp.wssize_reuse.max_age_seconds", "wssize_reuse_age_range") == nil {
		t.Fatal("a warm validity of -1 under the TUN engine was not refused")
	}
	for _, line := range *warnings {
		if strings.Contains(line, "Reuse WSSIZE connection") {
			t.Errorf("a refused reuse set also got the engine warning: %q", line)
		}
	}
	labels := map[string]map[string]string{
		"en": {"wssize_reuse_tls": "TLS Version Filter", "wssize_reuse_targets": "Use collected IPs", "wssize_reuse_window": "Real TCP window (wssize)"},
		"ru": {"wssize_reuse_tls": "«Фильтр версии TLS»", "wssize_reuse_targets": "«Использовать собранные IP»", "wssize_reuse_window": "«Окно реальных TCP-пакетов (wssize)»"},
	}
	for _, locale := range []string{"en", "ru"} {
		errs := uiErrorStrings(t, locale)
		if _, ok := errs["wssize_reuse_unsupported"]; ok {
			t.Errorf("%s.json still has errors.wssize_reuse_unsupported, which nothing sends any more", locale)
		}
		for _, code := range wsReuseRefusalCodes {
			s, _ := errs[code].(string)
			if !strings.Contains(s, "{{set}}") {
				t.Errorf("%s.json errors.%s does not name the set: %q", locale, code, s)
			}
			if label := labels[locale][code]; label != "" && !strings.Contains(s, label) {
				t.Errorf("%s.json errors.%s does not name %s: %q", locale, code, label, s)
			}
		}
		if s, _ := errs["wssize_reuse_age_range"].(string); !strings.Contains(s, "{{min}}") || !strings.Contains(s, "{{max}}") {
			t.Errorf("%s.json errors.wssize_reuse_age_range does not give the range: %q", locale, s)
		}
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
			field := findField(mustValidationErr(t, err), "sets[0].tcp.wssize_reuse.max_age_seconds", "wssize_reuse_age_range")
			if field == nil || field.Params["max"] != 300 || field.Params["set"] != c.Sets[0].Name {
				t.Fatalf("got %v, want wssize_reuse_age_range at sets[0].tcp.wssize_reuse.max_age_seconds with max 300 and the set's name", err)
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
