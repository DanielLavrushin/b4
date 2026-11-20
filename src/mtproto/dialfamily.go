package mtproto

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

var dialIPv6Probe = config.HostCanReachIPv6

func dialIPv6Usable() bool {
	return dialIPv6Probe()
}

func routeFamilies(mark uint) (v4, v6 bool) {
	if p := currentTelegramPath(); p.kind == telegramPathInterface && mark&^uint(config.SelfDialRelayMark) != 0 {
		switch {
		case !p.ipv6:
			return true, false
		case !p.ipv4:
			return false, true
		}
	}
	return true, true
}

func dialFamilies(mark uint) (v4, v6 bool) {
	v4, v6 = routeFamilies(mark)
	if v4 && v6 {
		v6 = dialIPv6Usable()
	}
	return v4, v6
}

func familyNetwork(v4, v6 bool) string {
	switch {
	case !v4:
		return "tcp6"
	case v6:
		return "tcp"
	}
	return "tcp4"
}

func dialNetwork(mark uint) string {
	return familyNetwork(dialFamilies(mark))
}

func routeNetwork(mark uint) string {
	return familyNetwork(routeFamilies(mark))
}

func dialFamilyAllows(ip net.IP, mark uint) bool {
	v4, v6 := dialFamilies(mark)
	if ip.To4() != nil {
		return v4
	}
	return v6
}
