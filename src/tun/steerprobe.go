package tun

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const steerProbeDst = "198.51.100.1"

func (r *routeManager) pickCapturePriority() int {
	out, err := run("ip", "rule", "show")
	if err != nil {
		return defaultCapturePrio
	}
	lowest := 0
	for _, line := range strings.Split(out, "\n") {
		prio, ok := rulePriority(line)
		if !ok || prio <= capturePrioFloor || prio >= defaultCapturePrio {
			continue
		}
		if lowest == 0 || prio < lowest {
			lowest = prio
		}
	}
	if lowest == 0 {
		return defaultCapturePrio
	}
	if lowest-1 < capturePrioFloor {
		return capturePrioFloor
	}
	return lowest - 1
}

func rulePriority(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0, false
	}
	prio, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
	if err != nil {
		return 0, false
	}
	return prio, true
}

func steerProbeMark() string {
	return fmt.Sprintf("0x%x", defaultSteerMark)
}

type steerConflict struct {
	Iface  string
	Source string
	Went   string
}

func (c steerConflict) String() string {
	return fmt.Sprintf("%s (a packet from %s resolves to %s)", c.Iface, c.Source, c.Went)
}

func probeSourceFor(cidr string) string {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ""
	}
	v4 := ipnet.IP.To4()
	if v4 == nil {
		return ""
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones > 30 {
		return ip.String()
	}
	probe := make(net.IP, len(v4))
	copy(probe, v4)
	probe[3] |= 0x02
	if probe.Equal(ip.To4()) {
		probe[3] ^= 0x01
	}
	if !ipnet.Contains(probe) {
		return ip.String()
	}
	return probe.String()
}

func parseSteerProbe(out, tunName string) (dev string, reached bool) {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	if line == "" {
		return "", false
	}
	dev = extractField(line, "dev")
	if table := extractField(line, "table"); table != "" && dev != "" {
		dev += " table " + table
	}
	return dev, extractField(line, "dev") == tunName
}

func (r *routeManager) lanProbeTargets() map[string]string {
	out, err := run("ip", "-4", "-o", "addr", "show", "scope", "global")
	if err != nil {
		return nil
	}
	targets := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		iface, cidr := "", ""
		for i, f := range fields {
			switch f {
			case "inet":
				if i+1 < len(fields) {
					cidr = fields[i+1]
				}
			case "dev":
				if i+1 < len(fields) {
					iface = fields[i+1]
				}
			}
		}
		if iface == "" && len(fields) > 1 {
			iface = fields[1]
		}
		if cidr == "" || iface == "" || iface == r.tunName || iface == r.outIface {
			continue
		}
		if src := probeSourceFor(cidr); src != "" {
			targets[iface] = src
		}
	}
	return targets
}

func (r *routeManager) steerConflicts() ([]steerConflict, bool) {
	targets := r.lanProbeTargets()
	if len(targets) == 0 {
		return nil, false
	}
	ifaces := make([]string, 0, len(targets))
	for iface := range targets {
		ifaces = append(ifaces, iface)
	}
	sort.Strings(ifaces)

	var conflicts []steerConflict
	for _, iface := range ifaces {
		src := targets[iface]
		out, err := run("ip", "route", "get", steerProbeDst, "mark", steerProbeMark(), "from", src, "iif", iface)
		if err != nil {
			log.Tracef("TUN: could not test whether %s reaches %s (%v)", iface, r.tunName, err)
			return nil, false
		}
		dev, reached := parseSteerProbe(out, r.tunName)
		if reached || dev == "" {
			continue
		}
		conflicts = append(conflicts, steerConflict{Iface: iface, Source: src, Went: dev})
	}
	return conflicts, true
}

func conflictsEqual(a, b []steerConflict) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *routeManager) refreshSteerConflicts() {
	found, ok := r.steerConflicts()
	if !ok || conflictsEqual(found, r.conflicts) {
		return
	}
	if len(found) == 0 {
		log.Infof("TUN: every local network now reaches %s; whatever was routing ahead of b4 is gone", r.tunName)
		r.conflicts = nil
		return
	}
	r.conflicts = found
	logSteerConflicts(r.tunName, r.capturePrio, found)
}

func (r *routeManager) warnOnSteerConflicts() []steerConflict {
	conflicts, ok := r.steerConflicts()
	if !ok || len(conflicts) == 0 {
		return nil
	}
	logSteerConflicts(r.tunName, r.capturePrio, conflicts)
	return conflicts
}

func logSteerConflicts(tunName string, prio int, conflicts []steerConflict) {
	names := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		names = append(names, c.String())
	}
	log.Warnf("TUN: another program on this router routes traffic before b4 can capture it, so nothing from %s reaches %s and the bypass does nothing for those clients: %s. Look for an 'ip rule' with a priority below %d - a transparent proxy such as XRAYUI, a multi-WAN manager or a VPN policy script - and either remove it or give b4 the NFQUEUE engine instead.",
		strings.Join(ifaceNames(conflicts), ", "), tunName, strings.Join(names, "; "), prio)
}

func ifaceNames(conflicts []steerConflict) []string {
	out := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, c.Iface)
	}
	return out
}
