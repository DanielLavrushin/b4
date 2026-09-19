package config

import "testing"

func refreshTestConfig() *Config {
	cfg := NewConfig()
	set := NewSetConfigWithDefaults()
	set.Id = "one"
	set.Enabled = true
	set.TCP.DPortFilter = "443"
	set.UDP.DPortFilter = "443"
	cfg.Sets = []*SetConfig{&set}
	return &cfg
}

func TestFirewallRefreshNeededFlipsOnEveryRefreshedSetting(t *testing.T) {
	cases := []struct {
		name   string
		change func(c *Config)
	}{
		{"skip setup", func(c *Config) { c.System.Tables.SkipSetup = true }},
		{"tcp ports", func(c *Config) { c.Sets[0].TCP.DPortFilter = "443,8443" }},
		{"tcp ports via http method eol", func(c *Config) { c.Sets[0].TCP.HTTPMethodEOL = true }},
		{"udp ports", func(c *Config) { c.Sets[0].UDP.DPortFilter = "443,3478" }},
		{"tcp conn bytes", func(c *Config) { c.Queue.TCPConnBytesLimit += 3 }},
		{"udp conn bytes", func(c *Config) { c.Queue.UDPConnBytesLimit += 3 }},
		{"dns tcp disabled", func(c *Config) { c.System.DNS.TCPDisabled = !c.System.DNS.TCPDisabled }},
		{"dns tcp intercept", func(c *Config) { c.Sets[0].DNS.Enabled = true; c.Sets[0].DNS.DoHURL = "https://dns.example/dns-query" }},
		{"mark", func(c *Config) { c.Queue.Mark = c.Queue.Mark << 1 }},
		{"ipv4", func(c *Config) { c.Queue.IPv4Enabled = !c.Queue.IPv4Enabled }},
		{"ipv6", func(c *Config) { c.Queue.IPv6Enabled = !c.Queue.IPv6Enabled }},
		{"masquerade", func(c *Config) { c.System.Tables.Masquerade.Enabled = !c.System.Tables.Masquerade.Enabled }},
		{"mss clamp", func(c *Config) { c.Queue.MSSClamp.Enabled = true; c.Queue.MSSClamp.Size = 1200 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := refreshTestConfig()
			after := refreshTestConfig()
			if FirewallRefreshNeeded(before, after) {
				t.Fatalf("two identical configs were read as needing a refresh")
			}
			tc.change(after)
			if !FirewallRefreshNeeded(before, after) {
				t.Fatalf("a change to %s was not read as needing a refresh", tc.name)
			}
		})
	}
}

func TestFirewallRefreshNeededIgnoresSettingsThatDoNotRebuildTheFirewall(t *testing.T) {
	cases := []struct {
		name   string
		change func(c *Config)
	}{
		{"device filtering", func(c *Config) { c.Queue.Devices.Enabled = !c.Queue.Devices.Enabled }},
		{"set domains", func(c *Config) { c.Sets[0].Targets.SNIDomains = []string{"a.example"} }},
		{"monitor interval", func(c *Config) { c.System.Tables.MonitorInterval += 5 }},
		{"ports of a disabled set", func(c *Config) {
			extra := NewSetConfigWithDefaults()
			extra.Id = "two"
			extra.Enabled = false
			extra.TCP.DPortFilter = "8080"
			c.Sets = append(c.Sets, &extra)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := refreshTestConfig()
			after := refreshTestConfig()
			tc.change(after)
			if FirewallRefreshNeeded(before, after) {
				t.Fatalf("a change to %s was read as needing a refresh although nothing rebuilds the firewall for it", tc.name)
			}
		})
	}
}

func TestFirewallRefreshNeededSkipsPortsWhenTablesAreSkipped(t *testing.T) {
	before := refreshTestConfig()
	before.System.Tables.SkipSetup = true
	after := refreshTestConfig()
	after.System.Tables.SkipSetup = true
	after.Sets[0].TCP.DPortFilter = "443,8443"
	if FirewallRefreshNeeded(before, after) {
		t.Fatalf("a port change was read as needing a refresh while table setup is skipped")
	}
}
