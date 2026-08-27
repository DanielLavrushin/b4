package mtproto

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

var dialIPv6Probe = config.HostHasGlobalIPv6

func dialIPv6Usable() bool {
	return dialIPv6Probe()
}

func dialNetwork() string {
	if dialIPv6Usable() {
		return "tcp"
	}
	return "tcp4"
}

func dialFamilyAllows(ip net.IP) bool {
	if dialIPv6Usable() {
		return true
	}
	return ip.To4() != nil
}
