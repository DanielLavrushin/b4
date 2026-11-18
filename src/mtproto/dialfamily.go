package mtproto

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

var dialIPv6Probe = config.HostCanReachIPv6

func dialIPv6Usable() bool {
	return dialIPv6Probe()
}

func dialFamilies() (v4, v6 bool) {
	if p := currentTelegramPath(); p.kind == telegramPathInterface {
		switch {
		case !p.ipv6:
			return true, false
		case !p.ipv4:
			return false, true
		}
	}
	return true, dialIPv6Usable()
}

func dialNetwork() string {
	switch v4, v6 := dialFamilies(); {
	case !v4:
		return "tcp6"
	case v6:
		return "tcp"
	}
	return "tcp4"
}

func dialFamilyAllows(ip net.IP) bool {
	v4, v6 := dialFamilies()
	if ip.To4() != nil {
		return v4
	}
	return v6
}
