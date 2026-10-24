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
		{"dscp stamp", func(c *Config) { c.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: 7} }},
		{"mss clamp", func(c *Config) { c.Queue.MSSClamp.Enabled = true; c.Queue.MSSClamp.Size = 1200 }},
		{"a set's tcp incoming mode", func(c *Config) { c.Sets[0].TCP.Incoming.Mode = "fake" }},
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
		{"device filtering with no device selected", func(c *Config) { c.Queue.Devices.Enabled = !c.Queue.Devices.Enabled }},
		{"devices selected while filtering is off", func(c *Config) {
			c.Queue.Devices.Devices = []Device{{MAC: "AA:BB:CC:DD:EE:01", Selected: true}}
			c.Queue.Devices.WhiteIsBlack = true
		}},
		{"set domains", func(c *Config) { c.Sets[0].Targets.SNIDomains = []string{"a.example"} }},
		{"monitor interval", func(c *Config) { c.System.Tables.MonitorInterval += 5 }},
		{"value of a dscp stamp that is off", func(c *Config) { c.System.Tables.DSCP.Value = 31 }},
		{"ports of a disabled set", func(c *Config) {
			extra := NewSetConfigWithDefaults()
			extra.Id = "two"
			extra.Enabled = false
			extra.TCP.DPortFilter = "8080"
			c.Sets = append(c.Sets, &extra)
		}},
		{"tcp incoming mode of a disabled set", func(c *Config) {
			extra := NewSetConfigWithDefaults()
			extra.Id = "two"
			extra.Enabled = false
			extra.TCP.Incoming.Mode = "reset"
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

func TestFirewallRefreshNeededFollowsTheDeviceFilter(t *testing.T) {
	filtered := func() *Config {
		c := refreshTestConfig()
		c.Queue.Devices.Enabled = true
		c.Queue.Devices.Devices = []Device{
			{MAC: "AA:BB:CC:DD:EE:01", Selected: true},
			{MAC: "AA:BB:CC:DD:EE:02"},
			{MAC: "02:B4:C0:A8:33:02", IP: "192.168.51.2", Selected: true, IsManual: true},
			{MAC: "02:B4:C0:A8:33:03", IP: "192.168.51.3", IsManual: true},
		}
		return c
	}
	for _, tc := range []struct {
		name    string
		change  func(c *Config)
		refresh bool
	}{
		{"turning the filter off", func(c *Config) { c.Queue.Devices.Enabled = false }, true},
		{"selecting another device", func(c *Config) { c.Queue.Devices.Devices[1].Selected = true }, true},
		{"the address of a selected device added by hand", func(c *Config) { c.Queue.Devices.Devices[2].IP = "192.168.51.9" }, true},
		{"excluding the selected devices", func(c *Config) { c.Queue.Devices.WhiteIsBlack = true }, true},
		{"the address of a device added by hand that is not selected", func(c *Config) { c.Queue.Devices.Devices[3].IP = "192.168.51.9" }, false},
		{"a device name", func(c *Config) { c.Queue.Devices.Devices[0].Name = "phone" }, false},
		{"vendor lookup", func(c *Config) { c.Queue.Devices.VendorLookup = !c.Queue.Devices.VendorLookup }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := filtered(), filtered()
			tc.change(after)
			if got := FirewallRefreshNeeded(before, after); got != tc.refresh {
				t.Errorf("a change to %s was read as needing a refresh: %v, want %v; the capture hooks follow the selected devices and nothing else", tc.name, got, tc.refresh)
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

func TestFirewallRefreshNeededFollowsDuplicateIPs(t *testing.T) {
	withIPs := func(dup bool, ips ...string) *Config {
		c := refreshTestConfig()
		c.Sets[0].TCP.Duplicate.Enabled = dup
		c.Sets[0].Targets.IPs = append([]string(nil), ips...)
		c.Sets[0].Targets.IpsToMatch = append([]string(nil), ips...)
		return c
	}

	if !FirewallRefreshNeeded(withIPs(false, "203.0.113.0/24"), withIPs(true, "203.0.113.0/24")) {
		t.Fatal("enabling TCP duplicate on a set with IPs must rebuild the duplicate sets")
	}
	if !FirewallRefreshNeeded(withIPs(true, "203.0.113.0/24"), withIPs(false, "203.0.113.0/24")) {
		t.Fatal("disabling TCP duplicate must rebuild the duplicate sets")
	}
	if !FirewallRefreshNeeded(withIPs(true, "203.0.113.0/24"), withIPs(true, "203.0.113.0/24", "2001:db8::/32")) {
		t.Fatal("a new IP on a duplicate set must rebuild the duplicate sets")
	}
	if !FirewallRefreshNeeded(withIPs(true, "203.0.113.0/24"), withIPs(true, "198.51.100.0/24")) {
		t.Fatal("a replaced IP on a duplicate set must rebuild the duplicate sets")
	}
	if FirewallRefreshNeeded(withIPs(true, "203.0.113.0/24", "198.51.100.0/24"), withIPs(true, "198.51.100.0/24", "203.0.113.0/24")) {
		t.Fatal("reordering the same IPs changes nothing in the firewall")
	}
	if FirewallRefreshNeeded(withIPs(false, "203.0.113.0/24"), withIPs(false, "198.51.100.0/24")) {
		t.Fatal("IPs of a set without duplication do not feed the duplicate sets")
	}

	disabled := withIPs(true, "203.0.113.0/24")
	disabled.Sets[0].Enabled = false
	if !FirewallRefreshNeeded(withIPs(true, "203.0.113.0/24"), disabled) {
		t.Fatal("disabling a duplicate set must rebuild the duplicate sets")
	}
}

func TestFirewallRefreshNeededWhenAnEmptyIPTargetComesOrGoes(t *testing.T) {
	deviceClampSet := func(declare func(*SetConfig)) *Config {
		c := refreshTestConfig()
		s := c.Sets[0]
		s.MSSClamp.Enabled = true
		s.MSSClamp.Size = 88
		s.Targets.SNIDomains = []string{"example.com"}
		s.Targets.DomainsToMatch = []string{"example.com"}
		s.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		if declare != nil {
			declare(s)
		}
		return c
	}
	cases := []struct {
		name    string
		declare func(*SetConfig)
	}{
		{"a geoip category that resolved to nothing", func(s *SetConfig) { s.Targets.GeoIpCategories = []string{"gone"} }},
		{"an unresolved ASN", func(s *SetConfig) { s.Targets.ASNs = []string{"64500"} }},
		{"manual IPs filtered out entirely", func(s *SetConfig) { s.Targets.IPs = []string{"2001:db8::1"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			macOnly := deviceClampSet(nil)
			declared := deviceClampSet(tc.declare)
			if macOnly.MSSClampFingerprint() == declared.MSSClampFingerprint() {
				t.Fatalf("the MAC-only clamp and the skipped clamp share a fingerprint: %q", macOnly.MSSClampFingerprint())
			}
			if !FirewallRefreshNeeded(declared, macOnly) {
				t.Fatal("removing the empty IP target must install the MAC-only clamp")
			}
			if !FirewallRefreshNeeded(macOnly, declared) {
				t.Fatal("adding an empty IP target must remove the MAC-only clamp")
			}
			if FirewallRefreshNeeded(declared, deviceClampSet(tc.declare)) {
				t.Fatal("two identical configs were read as needing a refresh")
			}
		})
	}
}
