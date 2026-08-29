package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ipv6RouteFile = "/proc/net/ipv6_route"

var (
	hostIPv6RouteProbe = probeIPv6DefaultRoute

	hostIPv6RouteMu      sync.Mutex
	hostIPv6RouteKnown   bool
	hostIPv6RoutePresent bool
	hostIPv6RouteAt      time.Time
)

func HostHasIPv6DefaultRoute() bool {
	hostIPv6RouteMu.Lock()
	defer hostIPv6RouteMu.Unlock()

	now := ipv6Now()
	if hostIPv6RouteKnown && now.Sub(hostIPv6RouteAt) < hostIPv6ProbeTTL {
		return hostIPv6RoutePresent
	}
	hostIPv6RoutePresent = hostIPv6RouteProbe()
	hostIPv6RouteKnown = true
	hostIPv6RouteAt = now
	return hostIPv6RoutePresent
}

func HostCanReachIPv6() bool {
	return HostHasGlobalIPv6() && HostHasIPv6DefaultRoute()
}

func probeIPv6DefaultRoute() bool {
	data, err := os.ReadFile(ipv6RouteFile)
	if err != nil {
		return false
	}
	return parseIPv6DefaultRoute(string(data))
}

const (
	rtfUp      = 0x0001
	rtfReject  = 0x0200
	ipv6AnyHex = "00000000000000000000000000000000"
)

func parseIPv6DefaultRoute(data string) bool {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		if fields[0] != ipv6AnyHex || fields[1] != "00" {
			continue
		}
		if fields[9] == "lo" {
			continue
		}
		flags, err := strconv.ParseUint(fields[8], 16, 32)
		if err != nil {
			continue
		}
		if flags&rtfUp == 0 || flags&rtfReject != 0 {
			continue
		}
		return true
	}
	return false
}
