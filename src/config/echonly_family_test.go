package config

import "testing"

func echStripFamilies(ipv4, ipv6, strip bool) *Config {
	cfg := NewConfig()
	cfg.Queue.IPv4Enabled = ipv4
	cfg.Queue.IPv6Enabled = ipv6
	set := NewSetConfig()
	set.Id, set.Name, set.Enabled = "ech", "ech", true
	set.DNS.StripECH = strip
	cfg.Sets = []*SetConfig{&set}
	return &cfg
}

func TestECHOnlyFamilyIsTheFamilyB4OtherwiseLeavesAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    func() *Config
		v4, v6 bool
	}{
		{"IPv6 support off and a set removes ECH keys", func() *Config { return echStripFamilies(true, false, true) }, false, true},
		{"IPv6 support off and no set removes them", func() *Config { return echStripFamilies(true, false, false) }, false, false},
		{"both families on", func() *Config { return echStripFamilies(true, true, true) }, false, false},
		{"IPv4 support off", func() *Config { return echStripFamilies(false, true, true) }, true, false},
		{"both families off", func() *Config { return echStripFamilies(false, false, true) }, false, false},
		{"the set is disabled", func() *Config {
			c := echStripFamilies(true, false, true)
			c.Sets[0].Enabled = false
			return c
		}, false, false},
		{"the TUN engine", func() *Config {
			c := echStripFamilies(true, false, true)
			c.Queue.Mode = "tun"
			return c
		}, false, false},
		{"Discovery", func() *Config {
			c := echStripFamilies(true, false, true)
			c.Queue.IsDiscovery = true
			return c
		}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			if got := cfg.ECHOnlyFamily(false); got != tc.v4 {
				t.Errorf("ECHOnlyFamily(IPv4) = %v, want %v", got, tc.v4)
			}
			if got := cfg.ECHOnlyFamily(true); got != tc.v6 {
				t.Errorf("ECHOnlyFamily(IPv6) = %v, want %v", got, tc.v6)
			}
			for _, v6 := range []bool{false, true} {
				if got, want := cfg.QueuesIPFamily(v6), cfg.IPFamilyEnabled(v6) || cfg.ECHOnlyFamily(v6); got != want {
					t.Errorf("QueuesIPFamily(v6=%v) = %v, want %v", v6, got, want)
				}
			}
		})
	}
}

func TestSwitchingECHRemovalWithIPv6OffRebuildsTheFirewall(t *testing.T) {
	off, on := echStripFamilies(true, false, false), echStripFamilies(true, false, true)
	off.System.DNS.TCPDisabled, on.System.DNS.TCPDisabled = true, true
	if !FirewallRefreshNeeded(off, on) || !FirewallRefreshNeeded(on, off) {
		t.Fatal("switching ECH key removal on or off with IPv6 support off must rebuild the firewall: only the rules decide whether IPv6 DNS answers reach b4")
	}

	off6, on6 := echStripFamilies(true, true, false), echStripFamilies(true, true, true)
	off6.System.DNS.TCPDisabled, on6.System.DNS.TCPDisabled = true, true
	if FirewallRefreshNeeded(off6, on6) {
		t.Fatal("with IPv6 support on, b4 already queues IPv6 DNS answers, so switching ECH key removal needs no rebuild")
	}
}
