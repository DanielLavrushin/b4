package tables

import "github.com/daniellavrushin/b4/config"

func iptSourceMatchArgs(m config.DeviceMatch, v6 bool) ([]string, bool) {
	if m.IsIP() {
		if m.V6 != v6 {
			return nil, false
		}
		return []string{"-s", m.IP}, true
	}
	return []string{"-m", "mac", "--mac-source", m.MAC}, true
}

func iptReplyMatchArgs(m config.DeviceMatch, v6 bool) ([]string, bool) {
	if !m.IsIP() {
		return nil, false
	}
	if m.V6 != v6 {
		return nil, false
	}
	return []string{"-d", m.IP}, true
}

func nftSourceMatchArgs(m config.DeviceMatch) []string {
	if m.IsIP() {
		if m.V6 {
			return []string{"ip6", "saddr", m.IP}
		}
		return []string{"ip", "saddr", m.IP}
	}
	return []string{"ether", "saddr", m.MAC}
}

func nftReplyMatchArgs(m config.DeviceMatch) []string {
	if m.IsIP() {
		if m.V6 {
			return []string{"ip6", "daddr", m.IP}
		}
		return []string{"ip", "daddr", m.IP}
	}
	return []string{"ether", "daddr", m.MAC}
}

func deviceMatchFamilyEnabled(m config.DeviceMatch, ipv4, ipv6 bool) bool {
	if !m.IsIP() {
		return true
	}
	if m.V6 {
		return ipv6
	}
	return ipv4
}

func setHasSourceForFamily(sources []config.DeviceMatch, v6 bool) bool {
	if len(sources) == 0 {
		return true
	}
	for _, m := range sources {
		if !m.IsIP() || m.V6 == v6 {
			return true
		}
	}
	return false
}

func setDeclaresIPTargets(set *config.SetConfig) bool {
	if set == nil {
		return false
	}
	t := &set.Targets
	return len(t.IpsToMatch) > 0 || len(t.IPs) > 0 || len(t.GeoIpCategories) > 0 || len(t.ASNs) > 0
}

func mssClampEntrySet(cfg *config.Config, e config.SetMSSClampEntry) *config.SetConfig {
	if cfg == nil || e.SetIdx < 0 || e.SetIdx >= len(cfg.Sets) {
		return nil
	}
	set := cfg.Sets[e.SetIdx]
	if set == nil || set.Id != e.SetID {
		return nil
	}
	return set
}

func mssClampUnresolved(cfg *config.Config, e config.SetMSSClampEntry) bool {
	if len(e.IPv4) > 0 || len(e.IPv6) > 0 {
		return false
	}
	set := mssClampEntrySet(cfg, e)
	return set == nil || setDeclaresIPTargets(set)
}

func mssClampMACOnly(cfg *config.Config, e config.SetMSSClampEntry) bool {
	if len(e.IPv4) > 0 || len(e.IPv6) > 0 || len(e.Sources) == 0 {
		return false
	}
	return !mssClampUnresolved(cfg, e)
}
