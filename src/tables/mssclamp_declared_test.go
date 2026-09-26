package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const declaredMAC = "AA:BB:CC:DD:EE:FF"

func mssDeclaredCfg(set *config.SetConfig) *config.Config {
	cfg := mssCfgWithDevices(config.Device{MAC: declaredMAC})
	cfg.Queue.IPv6Enabled = true
	cfg.Sets = []*config.SetConfig{set}
	return cfg
}

func mssDeclaredCases() []struct {
	name    string
	declare func(*config.SetConfig)
} {
	return []struct {
		name    string
		declare func(*config.SetConfig)
	}{
		{"an unresolved ASN", func(s *config.SetConfig) { s.Targets.ASNs = []string{"15169"} }},
		{"a geoip category missing from the file", func(s *config.SetConfig) { s.Targets.GeoIpCategories = []string{"gone"} }},
		{"manual IPs filtered out entirely", func(s *config.SetConfig) { s.Targets.IPs = []string{"2001:db8::1"} }},
	}
}

func TestMSSClampDeclaredButUnresolvedIPTargetsClampNothing(t *testing.T) {
	for _, tc := range mssDeclaredCases() {
		t.Run(tc.name, func(t *testing.T) {
			set := mssClampSet("s1", 88, nil, []string{declaredMAC})
			tc.declare(set)
			cfg := mssDeclaredCfg(set)

			for _, ipt := range []string{"iptables", "ip6tables"} {
				if specs := mssSpecs(t, cfg, ipt); len(specs) != 0 {
					t.Errorf("%s: a set whose IP targets resolve to nothing must not clamp every packet of its devices, got %v", ipt, specs)
				}
			}
			if rules := nftSpecs(t, cfg); len(rules) != 0 {
				t.Errorf("nft: a set whose IP targets resolve to nothing must not clamp every packet of its devices, got %v", rules)
			}
		})
	}
}

func TestMSSClampDeviceOnlySetsKeepTheirMACScope(t *testing.T) {
	t.Run("no destination targets at all", func(t *testing.T) {
		cfg := mssDeclaredCfg(mssClampSet("s1", 88, nil, []string{declaredMAC}))

		specs := mssSpecs(t, cfg, "iptables")
		if len(specs) != 1 || !strings.Contains(specs[0], "--mac-source "+declaredMAC) || strings.Contains(specs[0], "--match-set") {
			t.Errorf("expected the MAC-only clamp, got %v", specs)
		}
		all := strings.Join(nftSpecs(t, cfg), "\n")
		if !strings.Contains(all, "ether saddr "+declaredMAC+" tcp dport 443") {
			t.Errorf("expected the nft MAC-only clamp, got %q", all)
		}
	})

	t.Run("domain targets cannot scope a SYN, so the device scope stays", func(t *testing.T) {
		set := mssClampSet("s1", 88, nil, []string{declaredMAC})
		set.Targets.SNIDomains = []string{"example.com"}
		set.Targets.DomainsToMatch = []string{"example.com"}
		set.Targets.GeoSiteCategories = []string{"youtube"}
		cfg := mssDeclaredCfg(set)

		specs := mssSpecs(t, cfg, "iptables")
		if len(specs) != 1 || !strings.Contains(specs[0], "--mac-source "+declaredMAC) {
			t.Errorf("a domain-only set scoped to a device must keep clamping that device, got %v", specs)
		}
		all := strings.Join(nftSpecs(t, cfg), "\n")
		if !strings.Contains(all, "ether saddr "+declaredMAC+" tcp dport 443") {
			t.Errorf("a domain-only set scoped to a device must keep clamping that device in nft, got %q", all)
		}
	})
}

func TestMSSClampResolvedASNPrefixesStayNarrowed(t *testing.T) {
	set := mssClampSet("s1", 88, nil, []string{declaredMAC})
	set.Targets.ASNs = []string{"15169"}
	set.Targets.IpsToMatch = []string{"8.8.8.0/24"}
	cfg := mssDeclaredCfg(set)

	specs := mssSpecs(t, cfg, "iptables")
	if len(specs) != 1 || !strings.Contains(specs[0], "--match-set b4_mss_0_v4 dst") {
		t.Errorf("expected a clamp narrowed to the resolved prefixes, got %v", specs)
	}
	if v6 := mssSpecs(t, cfg, "ip6tables"); len(v6) != 0 {
		t.Errorf("no ipv6 prefix resolved, so nothing may be clamped over ipv6, got %v", v6)
	}
	for _, rule := range nftSpecs(t, cfg) {
		if !strings.Contains(rule, "@b4_mss_0_v4") {
			t.Errorf("every nft clamp must stay narrowed to the resolved prefixes, got %q", rule)
		}
	}
}

func TestNftMSSClampDisabledFamilyDoesNotWidenToTheDevice(t *testing.T) {
	cfg := mssCfgWithDevices(config.Device{MAC: declaredMAC})
	cfg.Queue.IPv4Enabled = false
	cfg.Queue.IPv6Enabled = true
	cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, []string{"203.0.113.7"}, []string{declaredMAC})}

	if rules := nftSpecs(t, cfg); len(rules) != 0 {
		t.Errorf("a set scoped to ipv4 addresses must not clamp all ipv6 traffic of its devices when ipv4 is off, got %v", rules)
	}
}

func TestMSSClampEntrySetLookup(t *testing.T) {
	set := mssClampSet("s1", 88, nil, []string{declaredMAC})
	cfg := mssDeclaredCfg(set)

	if got := mssClampEntrySet(cfg, config.SetMSSClampEntry{SetID: "s1", SetIdx: 0}); got != set {
		t.Errorf("expected the entry's own set, got %v", got)
	}
	for _, e := range []config.SetMSSClampEntry{
		{SetID: "s1", SetIdx: 1},
		{SetID: "s1", SetIdx: -1},
		{SetID: "other", SetIdx: 0},
	} {
		if got := mssClampEntrySet(cfg, e); got != nil {
			t.Errorf("entry %+v must not resolve to a set, got %v", e, got)
		}
		if mssClampMACOnly(cfg, config.SetMSSClampEntry{SetID: e.SetID, SetIdx: e.SetIdx, Sources: []config.DeviceMatch{{MAC: declaredMAC}}}) {
			t.Errorf("an entry whose set cannot be found must not widen to a MAC-only clamp: %+v", e)
		}
	}
	if mssClampEntrySet(nil, config.SetMSSClampEntry{}) != nil {
		t.Error("a nil config has no sets")
	}
}
