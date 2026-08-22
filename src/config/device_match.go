package config

import (
	"net"
	"sort"
	"strings"
)

type DeviceMatch struct {
	MAC string
	IP  string
	V6  bool
}

func (m DeviceMatch) IsIP() bool { return m.IP != "" }

func (m DeviceMatch) Key() string {
	if m.IsIP() {
		if m.V6 {
			return "ip6=" + m.IP
		}
		return "ip4=" + m.IP
	}
	return "mac=" + m.MAC
}

func (d *Device) Match() (DeviceMatch, bool) {
	if d.IsManual {
		ip := net.ParseIP(strings.TrimSpace(d.IP))
		if ip == nil {
			return DeviceMatch{}, false
		}
		return DeviceMatch{IP: ip.String(), V6: ip.To4() == nil}, true
	}
	mac := strings.ToUpper(strings.TrimSpace(d.MAC))
	if mac == "" {
		return DeviceMatch{}, false
	}
	return DeviceMatch{MAC: mac}, true
}

func (dc *DevicesConfig) MatchForMAC(mac string) (DeviceMatch, bool) {
	mac = strings.ToUpper(strings.TrimSpace(mac))
	if mac == "" {
		return DeviceMatch{}, false
	}
	if d := dc.FindByMAC(mac); d != nil {
		return d.Match()
	}
	return DeviceMatch{MAC: mac}, true
}

func AppendDeviceMatch(out []DeviceMatch, seen map[string]struct{}, m DeviceMatch) []DeviceMatch {
	k := m.Key()
	if _, dup := seen[k]; dup {
		return out
	}
	seen[k] = struct{}{}
	return append(out, m)
}

func deviceMatchKeys(matches []DeviceMatch) string {
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		keys = append(keys, m.Key())
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
