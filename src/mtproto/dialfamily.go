package mtproto

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

var dialIPv6Probe = config.HostCanReachIPv6

func dialIPv6Usable() bool {
	return dialIPv6Probe()
}

func dialIPv6Allowed() bool {
	if p := currentTelegramPath(); p.kind == telegramPathInterface && !p.ipv6 {
		return false
	}
	return dialIPv6Usable()
}

func dialNetwork() string {
	if dialIPv6Allowed() {
		return "tcp"
	}
	return "tcp4"
}

func dialFamilyAllows(ip net.IP) bool {
	if dialIPv6Allowed() {
		return true
	}
	return ip.To4() != nil
}
