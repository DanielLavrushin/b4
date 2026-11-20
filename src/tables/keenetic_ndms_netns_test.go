package tables

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

const (
	ndmsStandInHotspotChain = "_NDM_HOTSPOT_PREROUTING_MANG"
	ndmsPolicyMarkBase      = 0x0ffffaaa
	ndmsPolicyTableBase     = 4096
	ndmsPolicyRuleBase      = 100
	ndmsExactMask           = "0xffffffff"
)

type ndmsJumpPlace int

const (
	ndmsJumpBelowB4 ndmsJumpPlace = iota
	ndmsJumpAboveB4
)

type ndmsPolicy struct {
	mark     uint32
	table    int
	priority int
}

type ndmsStandIn struct{}

func ndmsPolicyAt(n int) ndmsPolicy {
	return ndmsPolicy{
		mark:     ndmsPolicyMarkBase + uint32(n),
		table:    ndmsPolicyTableBase + 2*n,
		priority: ndmsPolicyRuleBase + 2*n,
	}
}

func ndmsMangle(args ...string) []string {
	return append([]string{backendIPTables, "-w", "-t", "mangle"}, args...)
}

func ndmsDropChain() {
	_, _ = run(ndmsMangle("-D", "PREROUTING", "-j", ndmsStandInHotspotChain)...)
	_, _ = run(ndmsMangle("-F", ndmsStandInHotspotChain)...)
	_, _ = run(ndmsMangle("-X", ndmsStandInHotspotChain)...)
}

func ndmsStart(t *testing.T) *ndmsStandIn {
	t.Helper()
	ndmsDropChain()
	netnsRun(t, ndmsMangle("-N", ndmsStandInHotspotChain)...)
	t.Cleanup(ndmsDropChain)
	return &ndmsStandIn{}
}

func ndmsRouteTable(t *testing.T, table int, uplink, gateway string) {
	t.Helper()
	id := strconv.Itoa(table)
	args := []string{"ip", "route", "replace", "default"}
	if gateway != "" {
		args = append(args, "via", gateway)
	}
	netnsRun(t, append(args, "dev", uplink, "table", id)...)
	t.Cleanup(func() { _, _ = run("ip", "route", "del", "default", "table", id) })
}

func (s *ndmsStandIn) addPolicy(t *testing.T, p ndmsPolicy, uplink, gateway string) {
	t.Helper()
	mark := fmt.Sprintf("0x%x", p.mark)
	exact := mark + "/" + ndmsExactMask
	table := strconv.Itoa(p.table)
	lookup := strconv.Itoa(p.priority)
	hole := strconv.Itoa(p.priority + 1)
	netnsRun(t, "ip", "rule", "add", "priority", lookup, "fwmark", mark, "lookup", table)
	t.Cleanup(func() { _, _ = run("ip", "rule", "del", "priority", lookup, "fwmark", exact, "lookup", table) })
	netnsRun(t, "ip", "rule", "add", "priority", hole, "fwmark", mark, "blackhole")
	t.Cleanup(func() { _, _ = run("ip", "rule", "del", "priority", hole, "fwmark", exact, "blackhole") })
	if uplink != "" {
		ndmsRouteTable(t, p.table, uplink, gateway)
	}
}

func (s *ndmsStandIn) emptyTable(t *testing.T, p ndmsPolicy) {
	t.Helper()
	table := strconv.Itoa(p.table)
	netnsRun(t, "ip", "route", "del", "default", "table", table)
	if routes := strings.TrimSpace(netnsRun(t, "ip", "route", "show", "table", table)); routes != "" {
		t.Fatalf("table %s still holds routes after its default route was taken out:\n%s", table, routes)
	}
}

func (s *ndmsStandIn) addMember(t *testing.T, p ndmsPolicy, mac string) {
	t.Helper()
	match := []string{"-A", ndmsStandInHotspotChain, "-m", "mac", "--mac-source", mac}
	for _, target := range [][]string{
		{"-j", "MARK", "--set-xmark", fmt.Sprintf("0x%x/%s", p.mark, ndmsExactMask)},
		{"-j", "CONNMARK", "--save-mark", "--nfmask", ndmsExactMask, "--ctmask", ndmsExactMask},
		{"-j", "RETURN"},
	} {
		netnsRun(t, ndmsMangle(append(append([]string(nil), match...), target...)...)...)
	}
}

func (s *ndmsStandIn) addSourceRule(t *testing.T, addr string, table, priority int, uplink, gateway string) {
	t.Helper()
	id, prio := strconv.Itoa(table), strconv.Itoa(priority)
	netnsRun(t, "ip", "rule", "add", "priority", prio, "from", addr, "lookup", id)
	t.Cleanup(func() { _, _ = run("ip", "rule", "del", "priority", prio, "from", addr, "lookup", id) })
	ndmsRouteTable(t, table, uplink, gateway)
}

func (s *ndmsStandIn) placeJump(t *testing.T, at ndmsJumpPlace) {
	t.Helper()
	spec := []string{"-A", "PREROUTING"}
	if at == ndmsJumpAboveB4 {
		spec = []string{"-I", "PREROUTING", "1"}
	}
	netnsRun(t, ndmsMangle(append(spec, "-j", ndmsStandInHotspotChain)...)...)

	out := netnsRun(t, ndmsMangle("-L", "PREROUTING", "-n", "--line-numbers")...)
	ndms, firstB4, lastB4 := 0, 0, 0
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		switch {
		case f[1] == ndmsStandInHotspotChain:
			ndms = n
		case f[1] == captureChainPre || strings.HasPrefix(f[1], routeChainPrefix):
			if firstB4 == 0 {
				firstB4 = n
			}
			lastB4 = n
		}
	}
	if ndms == 0 || firstB4 == 0 {
		t.Fatalf("mangle PREROUTING lacks the stand-in's jump (line %d) or b4's jumps (line %d):\n%s", ndms, firstB4, out)
	}
	if at == ndmsJumpBelowB4 && ndms < lastB4 || at == ndmsJumpAboveB4 && ndms > firstB4 {
		t.Fatalf("the stand-in's jump sits at line %d, b4's jumps at lines %d-%d, which is not the placement this case is about:\n%s", ndms, firstB4, lastB4, out)
	}
}

func (s *ndmsStandIn) marked(t *testing.T, mac string) int {
	t.Helper()
	out := netnsRun(t, ndmsMangle("-L", ndmsStandInHotspotChain, "-v", "-x", "-n")...)
	total := 0
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[2] != "MARK" || !strings.Contains(strings.ToLower(line), strings.ToLower(mac)) {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			t.Fatalf("unparsable counter in %q", line)
		}
		total += n
	}
	return total
}
