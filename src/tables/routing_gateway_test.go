package tables

import (
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func captureRoutes(t *testing.T, ifaceRoutes string, refuseVia bool) *[]string {
	t.Helper()

	prevRun := run
	t.Cleanup(func() { run = prevRun })
	run = func(args ...string) (string, error) { return ifaceRoutes, nil }
	prevProto := routeIPSupportsProto
	t.Cleanup(func() { routeIPSupportsProto = prevProto })
	routeIPSupportsProto = func() bool { return false }
	prev := runLogged
	var cmds []string
	runLogged = func(op string, args ...string) bool {
		cmds = append(cmds, strings.Join(args, " "))
		return !refuseVia || !contains(args, "via")
	}
	t.Cleanup(func() { runLogged = prev })

	return &cmds
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func joined(cmds []string) string { return strings.Join(cmds, "\n") }

func last(cmds *[]string) string { return (*cmds)[len(*cmds)-1] }

type gwHost struct {
	ifaces map[string]routeHostIface
	reads  int
}

func gwHostIfaces(t *testing.T, spec map[string][]string, down ...string) map[string]routeHostIface {
	t.Helper()
	out := make(map[string]routeHostIface, len(spec))
	for name, addrs := range spec {
		ifc := routeHostIface{up: !slices.Contains(down, name)}
		for _, a := range addrs {
			ip, prefix, err := net.ParseCIDR(a)
			if err != nil {
				t.Fatalf("bad address %q: %v", a, err)
			}
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			ifc.addrs = append(ifc.addrs, net.IPNet{IP: ip, Mask: prefix.Mask})
		}
		out[name] = ifc
	}
	return out
}

func gwStubHost(t *testing.T, spec map[string][]string, down ...string) *gwHost {
	t.Helper()
	h := &gwHost{ifaces: gwHostIfaces(t, spec, down...)}
	prev := routeHostIfaces
	t.Cleanup(func() { routeHostIfaces = prev })
	routeHostIfaces = func() (map[string]routeHostIface, error) {
		h.reads++
		return h.ifaces, nil
	}
	return h
}

type gwCmds struct {
	mainV4, mainV6 string
	lines          []string
}

func (c *gwCmds) count(line string) int {
	n := 0
	for _, l := range c.lines {
		if l == line {
			n++
		}
	}
	return n
}

func (c *gwCmds) with(needle string) []string {
	var out []string
	for _, l := range c.lines {
		if strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return out
}

func gwStubCommands(t *testing.T) *gwCmds {
	t.Helper()
	c := &gwCmds{}
	prevRun, prevLogged, prevDel, prevProto := run, runLogged, routeDelRuleLoop, routeIPSupportsProto
	t.Cleanup(func() {
		run, runLogged, routeDelRuleLoop, routeIPSupportsProto = prevRun, prevLogged, prevDel, prevProto
	})
	run = func(args ...string) (string, error) {
		line := strings.Join(args, " ")
		c.lines = append(c.lines, line)
		switch line {
		case "ip route show table main":
			return c.mainV4, nil
		case "ip -6 route show table main":
			return c.mainV6, nil
		}
		return "", nil
	}
	runLogged = func(op string, args ...string) bool {
		c.lines = append(c.lines, strings.Join(args, " "))
		return true
	}
	routeDelRuleLoop = func(bool, string, string) {}
	routeIPSupportsProto = func() bool { return false }
	return c
}

func gwForgetWarnings(t *testing.T) {
	t.Helper()
	forget := func() {
		routeGatewayWarned.Range(func(k, _ any) bool {
			routeGatewayWarned.Delete(k)
			return true
		})
	}
	forget()
	t.Cleanup(forget)
}

type gwLinks struct {
	mu    sync.Mutex
	types map[string]int
	reads atomic.Int32
}

func (l *gwLinks) set(iface string, typ int) {
	l.mu.Lock()
	l.types[iface] = typ
	l.mu.Unlock()
}

func gwStubLinks(t *testing.T, types map[string]int) *gwLinks {
	t.Helper()
	l := &gwLinks{types: types}
	prev := routeLinkType
	t.Cleanup(func() { routeLinkType = prev })
	routeLinkType = func(iface string) (int, bool) {
		l.reads.Add(1)
		l.mu.Lock()
		defer l.mu.Unlock()
		typ, ok := l.types[iface]
		return typ, ok
	}
	return l
}

func gwIsolate(t *testing.T) {
	t.Helper()
	loopTestSysfs(t)
	localGuardReset(t)
	rpFilterHarness(t, map[string]string{"all": "0", "lo": "0", "eth1": "1"})
	prevSeen := routeIfaceSeen
	routeIfaceSeen = make(map[string]bool)
	t.Cleanup(func() { routeIfaceSeen = prevSeen })
	gwForgetWarnings(t)
	gwStubLinks(t, map[string]int{"eth1": routeArphrdEther, "eth2": routeArphrdEther, "lo": routeArphrdEther})
}

func gwTestSet(id, iface, gw string) *config.SetConfig {
	s := familyTestSet()
	s.Id = id
	s.Name = id
	s.Routing.EgressInterface = iface
	s.Routing.EgressGateway = gw
	return s
}

func gwResolvedState(cfg *config.Config, set *config.SetConfig) routeState {
	st := buildRouteState(cfg, set)
	st.set = set
	routeResolveGateways(cfg, set, &st, newRouteGatewayProbe())
	return st
}

func gwCachedState(t *testing.T, setID string) routeState {
	t.Helper()
	routeMu.Lock()
	defer routeMu.Unlock()
	st, ok := routeRuleCache[setID]
	if !ok {
		t.Fatalf("set %q is not installed", setID)
	}
	return st
}

func gwLastOp(ops []string, prefix string) string {
	for i := len(ops) - 1; i >= 0; i-- {
		if strings.HasPrefix(ops[i], prefix) {
			return ops[i]
		}
	}
	return ""
}

func TestASetGatewayBecomesTheNextHopOfItsDefaultRoute(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeReplaceDefaultRoute("mihomo", "10.99.0.2", "10.99.0.1", "132", false)
	got := joined(*cmds)
	if strings.Contains(got, "scope link") {
		t.Errorf("the gateway needs no on-link route of its own, got:\n%s", got)
	}
	if !strings.Contains(got, "route replace default via 10.99.0.1 dev mihomo") || !strings.Contains(got, "src 10.99.0.2") {
		t.Errorf("the set's default route must go through the gateway it asked for, got:\n%s", got)
	}
	if got := last(cmds); !strings.Contains(got, "via 10.99.0.1") {
		t.Errorf("the gateway route must be the last one written, so a refusal falls through to the fallback: %q", got)
	}
}

func TestOwnRoutesTakeBackTheDefaultAndTheKillSwitch(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeDeleteOwnRoutes("mihomo", "132")
	got := joined(*cmds)
	if !strings.Contains(got, "ip route del default dev mihomo table 132") {
		t.Errorf("the IPv4 default route must be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "blackhole default metric 4096 table 132") {
		t.Errorf("the kill switch must be removed with the default route, got:\n%s", got)
	}
	if strings.Contains(got, "scope link") || strings.Contains(got, "10.99.0.1") {
		t.Errorf("no next-hop route exists anymore, got:\n%s", got)
	}
}

func TestASetGatewayWinsOverTheOneFoundOnTheInterface(t *testing.T) {
	cmds := captureRoutes(t, "default via 192.0.2.1 dev eth1 proto static\n", false)
	routeReplaceDefaultRoute("eth1", "", "192.0.2.254", "140", false)
	got := joined(*cmds)

	if strings.Contains(got, "via 192.0.2.1") {
		t.Errorf("the gateway read off the interface must not be used once the set names one, got:\n%s", got)
	}
	if !strings.Contains(got, "default via 192.0.2.254 dev eth1") {
		t.Errorf("the set's own gateway must win, got:\n%s", got)
	}
}

func TestAnIPv6GatewayReachesOnlyTheIPv6Route(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeReplaceDefaultRoute("mihomo", "2001:db8::2", "2001:db8::1", "132", true)
	got := joined(*cmds)
	if strings.Contains(got, "scope link") {
		t.Errorf("the gateway needs no on-link route of its own, got:\n%s", got)
	}
	if !strings.Contains(got, "ip -6 route replace default via 2001:db8::1 dev mihomo") || !strings.Contains(got, "src 2001:db8::2") {
		t.Errorf("the set's IPv6 default route must go through the gateway it asked for, got:\n%s", got)
	}
	if strings.Contains(got, "ip route") {
		t.Errorf("an IPv6 gateway must not reach the IPv4 route table, got:\n%s", got)
	}
}

func TestAGatewayTheKernelRefusesFallsBackToTheInterfaceRoute(t *testing.T) {
	cmds := captureRoutes(t, "", true)
	routeReplaceDefaultRoute("mihomo", "10.99.0.2", "10.99.0.9", "132", false)
	got := joined(*cmds)
	fallback := last(cmds)
	if !strings.Contains(fallback, "route replace default dev mihomo") || contains(strings.Fields(fallback), "via") {
		t.Errorf("a gateway the kernel refuses must fall back to the interface route, got %q", fallback)
	}
	if strings.Contains(got, "scope link") {
		t.Errorf("the refused gateway leaves no on-link route behind, got:\n%s", got)
	}
}

func TestAnInterfaceWithoutAGatewayKeepsTheRouteItHadBefore(t *testing.T) {
	cmds := captureRoutes(t, "", false)
	routeReplaceDefaultRoute("mihomo", "10.99.0.2", "", "132", false)
	if got := joined(*cmds); got != "ip route replace default dev mihomo src 10.99.0.2 table 132" {
		t.Errorf("with no gateway b4 must write exactly the route it wrote before, got:\n%s", got)
	}

	cmds = captureRoutes(t, "default via 192.0.2.1 dev eth1 proto static\n", false)
	routeReplaceDefaultRoute("eth1", "192.0.2.10", "", "140", false)
	if got := joined(*cmds); got != "ip route replace default via 192.0.2.1 dev eth1 src 192.0.2.10 table 140" {
		t.Errorf("with no gateway the interface's own next hop must be used exactly as before, got:\n%s", got)
	}
}

func TestAGatewayOfOneFamilyOnlyReachesThatFamily(t *testing.T) {
	if got := routeAddrForFamily("192.0.2.254", true); got != "" {
		t.Errorf("an IPv4 gateway must not be offered to the IPv6 route, got %q", got)
	}
	if got := routeAddrForFamily("2001:DB8::254", false); got != "" {
		t.Errorf("an IPv6 gateway must not be offered to the IPv4 route, got %q", got)
	}
	if got := routeAddrForFamily("2001:DB8::254", true); got != "2001:db8::254" {
		t.Errorf("the IPv6 gateway must be canonicalized for the IPv6 route, got %q", got)
	}
}

func TestSetsWithDifferentGatewaysStopSharingATable(t *testing.T) {
	base := routeIfaceAutoKey("eth1", "192.0.2.10", "", false)
	one := routeIfaceAutoKey("eth1", "192.0.2.10", "192.0.2.1", false)
	two := routeIfaceAutoKey("eth1", "192.0.2.10", "192.0.2.2", false)

	if one == two {
		t.Fatalf("two gateways share the key %q, so the second route replace would overwrite the first", one)
	}
	if one == base {
		t.Errorf("a set with a gateway must not join the group of sets without one: %q", one)
	}
}

func TestASetWithoutAGatewayKeepsTheMarkAndTableItAlreadyHad(t *testing.T) {
	if got := routeIfaceAutoKey("eth1", "192.0.2.10", "", false); got != "eth1|192.0.2.10" {
		t.Errorf("the key of a set without a gateway changed: got %q", got)
	}
	if got := routeIfaceAutoKey("eth1", "192.0.2.10", "", true); got != "eth1|192.0.2.10|ks" {
		t.Errorf("the kill switch must still land last: got %q", got)
	}
	if got := routeIfaceAutoKey("eth1", "192.0.2.10", "192.0.2.1", true); got != "eth1|192.0.2.10|gw=192.0.2.1|ks" {
		t.Errorf("the gateway must sit before the kill switch: got %q", got)
	}
}

func TestGatewayUsability(t *testing.T) {
	cmds := gwStubCommands(t)
	cmds.mainV4 = strings.Join([]string{
		"default via 192.0.2.254 dev eth1 proto dhcp src 192.0.2.1 metric 100",
		"default dev ppp1 scope link metric 200",
		"10.0.0.0/24 dev br0 proto kernel scope link src 10.0.0.1",
		"10.32.0.2 dev ppp0 proto kernel scope link src 10.32.0.1",
		"10.33.0.9 dev wg0",
		"169.254.0.0/16 dev eth1 scope link metric 1000",
		"192.0.2.0/24 dev eth1 proto kernel scope link src 192.0.2.1",
		"198.51.100.0/24 via 192.0.2.254 dev eth1",
		"unreachable 203.0.113.0/24 metric 1",
	}, "\n")
	cmds.mainV6 = strings.Join([]string{
		"2001:db8:1::/64 dev eth1 proto kernel metric 256 pref medium",
		"2001:db8:98::/64 via fe80::1 dev eth1 metric 1024 pref medium",
		"2001:db8:99::/64 dev pl0 metric 1024 pref medium",
		"fe80::/64 dev eth1 proto kernel metric 256 pref medium",
		"default via fe80::1 dev eth1 proto ra metric 1024 expires 1799sec hoplimit 64 pref medium",
	}, "\n")
	gwStubHost(t, map[string][]string{
		"eth1": {"192.0.2.1/24", "fe80::2/64", "2001:db8:1::2/64"},
		"br0":  {"10.0.0.1/24", "fe80::1/64", "169.254.7.7/16"},
		"p31a": {"10.31.0.0/31"},
		"p31b": {"10.31.1.1/31"},
		"ppp0": {"10.32.0.1/32"},
		"ppp1": {"10.40.0.1/32"},
		"wg0":  {"10.33.0.1/32"},
		"pl0":  nil,
		"p127": {"2001:db8:127::1/127"},
		"eth2": {"198.18.0.1/30"},
		"tun9": {"fe80::9/64"},
	})
	for _, c := range []struct {
		iface, gw string
		usable    bool
		reason    string
	}{
		{"eth1", "192.0.2.5", true, ""},
		{"eth1", "192.0.2.1", false, "is one of this router's own addresses"},
		{"eth1", "10.0.0.1", false, "is one of this router's own addresses"},
		{"eth1", "fe80::2", false, "is one of this router's own addresses"},
		{"eth1", "fe80::1", true, ""},
		{"eth1", "169.254.7.7", true, ""},
		{"eth1", "192.0.2.0", false, "is the network address of 192.0.2.0/24"},
		{"eth1", "192.0.2.255", false, "is the broadcast address of 192.0.2.0/24"},
		{"eth2", "198.18.0.0", false, "is the network address of 198.18.0.0/30"},
		{"eth2", "198.18.0.3", false, "is the broadcast address of 198.18.0.0/30"},
		{"eth2", "198.18.0.2", true, ""},
		{"p31a", "10.31.0.1", true, ""},
		{"p31b", "10.31.1.0", true, ""},
		{"ppp0", "10.32.0.2", true, ""},
		{"ppp1", "10.40.0.2", true, ""},
		{"wg0", "10.33.0.9", true, ""},
		{"eth1", "2001:db8:1::1", true, ""},
		{"eth1", "2001:db8:1::", false, "is the network address of 2001:db8:1::/64"},
		{"pl0", "2001:db8:99::1", true, ""},
		{"pl0", "2001:db8:99::", false, "is the network address of 2001:db8:99::/64"},
		{"p127", "2001:db8:127::", true, ""},
		{"tun9", "fe80:1::1", true, ""},
		{"eth1", "198.51.100.5", false, "is not on a network eth1 reaches directly"},
		{"eth1", "203.0.113.1", false, "is not on a network eth1 reaches directly"},
		{"eth1", "2001:db8:98::1", false, "is not on a network eth1 reaches directly"},
		{"br0", "192.0.2.5", false, "is not on a network br0 reaches directly"},
		{"pl0", "fe80::5", false, "is not on a network pl0 reaches directly"},
		{"eth1", "not-an-ip", false, "is not an IP address"},
	} {
		usable, reason := routeGatewayUsable(c.iface, c.gw)
		if usable != c.usable || reason != c.reason {
			t.Errorf("routeGatewayUsable(%q, %q) = %v %q, want %v %q", c.iface, c.gw, usable, reason, c.usable, c.reason)
		}
	}
}

func TestGatewayProbeReadsTheHostOncePerSync(t *testing.T) {
	cmds := gwStubCommands(t)
	cmds.mainV6 = "2001:db8:99::/64 dev pl0 metric 1024 pref medium"
	host := gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24"}, "pl0": nil})
	probe := newRouteGatewayProbe()
	for _, c := range [][2]string{
		{"eth1", "192.0.2.1"}, {"eth1", "192.0.2.2"}, {"eth1", "198.51.100.1"}, {"eth1", "198.51.100.2"},
		{"pl0", "2001:db8:99::1"}, {"pl0", "2001:db8:99::2"}, {"eth1", "192.0.2.1"},
	} {
		probe.usable(c[0], c[1])
	}
	if host.reads != 1 {
		t.Errorf("the router's addresses were dumped %d times for one sync, want once", host.reads)
	}
	if n := cmds.count("ip route show table main"); n != 1 {
		t.Errorf("the IPv4 main table was read %d times for one sync, want once", n)
	}
	if n := cmds.count("ip -6 route show table main"); n != 1 {
		t.Errorf("the IPv6 main table was read %d times for one sync, want once", n)
	}
}

func TestASyncReadsTheHostOnceForAllItsGatewaySets(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	cmds := gwStubCommands(t)
	stubBinaries(t, "ip")
	host := gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24"}})
	routeEngine = &mockRouteBackend{}
	a := gwTestSet("syncone-a", "eth1", "192.0.2.1")
	b := gwTestSet("syncone-b", "eth1", "198.51.100.1")
	c := gwTestSet("syncone-c", "eth1", "198.51.100.2")
	b.Routing.FWMark, b.Routing.Table = 0x7e20, 233
	c.Routing.FWMark, c.Routing.Table = 0x7e30, 234
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{a, b, c}
	RoutingSyncConfig(cfg)
	if host.reads != 1 {
		t.Errorf("a sync of three gateway sets dumped the router's addresses %d times, want once", host.reads)
	}
	if n := cmds.count("ip route show table main"); n != 1 {
		t.Errorf("a sync of three gateway sets read the IPv4 main table %d times, want once", n)
	}
}

func TestAGatewayLessSetEmitsTheGuardItHadBefore(t *testing.T) {
	for _, engine := range []string{"mock", backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			gwIsolate(t)
			cmds := gwStubCommands(t)
			stubBinaries(t, backendIPTables, backendIP6Tables, "ipset")
			host := gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24"}})
			cfg := familyTestConfig(true, true)
			set := gwTestSet("nogw", "eth1", "")
			cfg.Sets = []*config.SetConfig{set}
			st := gwResolvedState(cfg, set)
			mock := &mockRouteBackend{}
			var be routeBackend = mock
			switch engine {
			case backendNFTables:
				be = &routeNftBackend{}
			case backendIPTables:
				be = &routeIptBackend{}
			}
			if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureRule: %v", err)
			}
			if host.reads != 0 || len(cmds.with("route show table main")) != 0 {
				t.Errorf("a set without a gateway must not probe the router for one (%d address dumps, main reads %v)", host.reads, cmds.with("route show table main"))
			}
			pre := chainRuleLines(cmds.lines, st.chainPre)
			out := chainRuleLines(cmds.lines, st.chainOut)
			switch engine {
			case "mock":
				if indexOfOp(mock.chainOps[st.chainPre], "loop-guard eth1") < 0 {
					t.Errorf("a set without a gateway keeps the full guard: %v", mock.chainOps[st.chainPre])
				}
				if indexOfPrefix(mock.chainOps[st.chainPre], "gateway-guard") >= 0 || indexOfPrefix(mock.chainOps[st.chainOut], "redirect-drop") >= 0 {
					t.Errorf("a set without a gateway takes none of the gateway rules: %v %v", mock.chainOps[st.chainPre], mock.chainOps[st.chainOut])
				}
			case backendNFTables:
				guards := []string{`nft add rule inet b4_route ` + st.chainPre + ` iifname "eth1" return`}
				if got := linesWith(pre, `iifname "eth1"`); !slices.Equal(got, guards) {
					t.Errorf("the guard must be the single bare rule it was before, got %v", got)
				}
			case backendIPTables:
				guards := []string{
					"iptables -w -t mangle -A " + st.chainPre + " -i eth1 -j RETURN",
					"ip6tables -w -t mangle -A " + st.chainPre + " -i eth1 -j RETURN",
				}
				if got := linesWith(pre, "-i eth1"); !slices.Equal(got, guards) {
					t.Errorf("the guard must be the bare per-family rules it was before, got %v", got)
				}
			}
			for _, needle := range []string{"saddr", " -s ", "fib daddr", "addrtype"} {
				if got := linesWith(pre, needle); len(got) != 0 {
					t.Errorf("a set without a gateway takes no gateway or local-destination rule, got %v", got)
				}
			}
			if got := linesWith(out, "redirect"); len(got) != 0 {
				t.Errorf("a set without a gateway drops no redirects, got %v", got)
			}
		})
	}
}

func linesWith(lines []string, needle string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return out
}

func TestAGatewayNarrowsTheIPv4GuardToTheGateways(t *testing.T) {
	gwIsolate(t)
	gwStubCommands(t)
	gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24"}})
	cfg := familyTestConfig(true, false)
	set := gwTestSet("narrow", "eth1", "192.0.2.1")
	cfg.Sets = []*config.SetConfig{set}
	st := gwResolvedState(cfg, set)
	if st.gwV4 != "192.0.2.1" || st.gwV6 != "" || !slices.Equal(st.gwPeersV4, []string{"192.0.2.1"}) {
		t.Fatalf("resolved gateways = %q %q %v, want the IPv4 gateway alone", st.gwV4, st.gwV6, st.gwPeersV4)
	}
	be := &mockRouteBackend{}
	if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}
	ops := be.chainOps[st.chainPre]
	if indexOfPrefix(ops, "loop-guard") >= 0 {
		t.Errorf("a usable IPv4 gateway replaces the full guard: %v", ops)
	}
	if indexOfOp(ops, "gateway-guard eth1 gw4=192.0.2.1 v6=false") < 0 {
		t.Errorf("the IPv4 gateway must be exempted by its address: %v", ops)
	}
	if indexOfOp(be.chainOps[st.chainOut], "redirect-drop eth1") < 0 {
		t.Errorf("a narrowed IPv4 guard drops the redirects leaving through the egress: %v", be.chainOps[st.chainOut])
	}
}

func TestARefusedGatewayKeepsTheFullGuardAndTheInterfaceRoute(t *testing.T) {
	gwIsolate(t)
	cmds := gwStubCommands(t)
	stubBinaries(t, "ip")
	logs := captureTablesLog(t)
	gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	cfg := familyTestConfig(true, false)
	set := gwTestSet("refused", "lo", "198.51.100.250")
	cfg.Sets = []*config.SetConfig{set}
	st := gwResolvedState(cfg, set)
	if st.gwV4 != "" || st.gwV6 != "" || st.gwPeersV4 != nil {
		t.Fatalf("a gateway lo cannot reach must count as no gateway, got %q %q %v", st.gwV4, st.gwV6, st.gwPeersV4)
	}
	if st.egressGW != "198.51.100.250" {
		t.Fatalf("the configured gateway must stay in the state so the table key does not move, got %q", st.egressGW)
	}
	be := &mockRouteBackend{}
	if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}
	if indexOfOp(be.chainOps[st.chainPre], "loop-guard lo") < 0 || indexOfPrefix(be.chainOps[st.chainPre], "gateway-guard") >= 0 {
		t.Errorf("a refused gateway must leave the full guard in place: %v", be.chainOps[st.chainPre])
	}
	if indexOfPrefix(be.chainOps[st.chainOut], "redirect-drop") >= 0 {
		t.Errorf("a refused gateway drops no redirects: %v", be.chainOps[st.chainOut])
	}
	routes := cmds.with("route replace default")
	if len(routes) != 1 || strings.Contains(routes[0], "via") || !strings.Contains(routes[0], "ip route replace default dev lo") {
		t.Errorf("a refused gateway must leave the interface's own route, got %v", routes)
	}
	gwResolvedState(cfg, set)
	warned := 0
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "198.51.100.250") {
			warned++
			if !strings.Contains(line, "'refused'") || !strings.Contains(line, " on lo") || !strings.Contains(line, "is not on a network lo reaches directly") {
				t.Errorf("the warning must name the set, the gateway, the egress and the reason, got %q", line)
			}
		}
	}
	if warned != 1 {
		t.Errorf("the refused gateway must be reported once, got %d lines:\n%s", warned, logs.String())
	}
}

func TestAnUnusableGatewayIsReportedOnlyWhileTheEgressIsUp(t *testing.T) {
	gwIsolate(t)
	gwStubCommands(t)
	logs := captureTablesLog(t)
	host := gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24"}}, "eth1")
	cfg := familyTestConfig(true, false)
	set := gwTestSet("downgw", "eth1", "198.51.100.1")
	cfg.Sets = []*config.SetConfig{set}
	reported := func() int {
		return len(linesWith(strings.Split(logs.String(), "\n"), "set 'downgw' names 198.51.100.1 as its gateway"))
	}
	gwResolvedState(cfg, set)
	if n := reported(); n != 0 {
		t.Fatalf("a gateway must not be reported while its egress is down, the link-up reinstall decides again:\n%s", logs.String())
	}
	host.ifaces = gwHostIfaces(t, map[string][]string{"eth1": {"192.0.2.10/24"}})
	gwResolvedState(cfg, set)
	gwResolvedState(cfg, set)
	if n := reported(); n != 1 {
		t.Errorf("an unusable gateway on an egress that is up must be reported once, got %d:\n%s", n, logs.String())
	}
	host.ifaces = gwHostIfaces(t, map[string][]string{"eth1": {"198.51.100.10/24"}})
	gwResolvedState(cfg, set)
	host.ifaces = gwHostIfaces(t, map[string][]string{"eth1": {"192.0.2.10/24"}})
	gwResolvedState(cfg, set)
	if n := reported(); n != 2 {
		t.Errorf("a gateway that worked in between must be reported again when it stops working, got %d:\n%s", n, logs.String())
	}
}

func TestEverySetOnAnEgressExemptsEveryGatewayOnIt(t *testing.T) {
	gwIsolate(t)
	gwStubCommands(t)
	gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24"}, "eth2": {"198.18.0.1/24"}})
	cfg := familyTestConfig(true, false)
	a := gwTestSet("peer-a", "eth1", "192.0.2.20")
	b := gwTestSet("peer-b", "eth1", "192.0.2.3")
	off := gwTestSet("peer-off", "eth1", "192.0.2.4")
	off.Enabled = false
	unrouted := gwTestSet("peer-unrouted", "eth1", "192.0.2.5")
	unrouted.Routing.Enabled = false
	proxy := gwTestSet("peer-proxy", "eth1", "192.0.2.6")
	proxy.Routing.Mode = config.RoutingModeProxy
	other := gwTestSet("peer-other", "eth2", "198.18.0.2")
	far := gwTestSet("peer-far", "eth1", "203.0.113.1")
	v6 := gwTestSet("peer-v6", "eth1", "2001:db8::1")
	plain := gwTestSet("peer-plain", "eth1", "")
	cfg.Sets = []*config.SetConfig{a, b, off, unrouted, proxy, other, far, v6, plain}
	want := []string{"192.0.2.3", "192.0.2.20"}
	for _, set := range []*config.SetConfig{a, b} {
		st := gwResolvedState(cfg, set)
		if !slices.Equal(st.gwPeersV4, want) {
			t.Errorf("set %s exempts %v, want every usable gateway of the enabled interface sets on eth1 in order: %v", set.Name, st.gwPeersV4, want)
		}
		be := &mockRouteBackend{}
		if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
			t.Fatalf("routeEnsureRule: %v", err)
		}
		if indexOfOp(be.chainOps[st.chainPre], "gateway-guard eth1 gw4=192.0.2.3,192.0.2.20 v6=false") < 0 {
			t.Errorf("set %s must return both gateways' traffic: %v", set.Name, be.chainOps[st.chainPre])
		}
	}
	for _, set := range []*config.SetConfig{far, v6, plain} {
		if st := gwResolvedState(cfg, set); st.gwPeersV4 != nil {
			t.Errorf("set %s has no usable IPv4 gateway of its own and keeps the full guard, yet lists %v", set.Name, st.gwPeersV4)
		}
	}
}

func TestGatewayGuardEmission(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)
	for _, c := range []struct {
		name     string
		be       routeBackend
		gateways []string
		ipv6     bool
		want     []string
	}{
		{"nft one gateway with IPv6 off", &routeNftBackend{}, []string{"192.0.2.1"}, false, []string{
			`nft add rule inet b4_route pre iifname "eth1" ip saddr 192.0.2.1 return`,
			`nft add rule inet b4_route pre iifname "eth1" meta nfproto ipv6 return`,
		}},
		{"nft two gateways and IPv6", &routeNftBackend{}, []string{"192.0.2.1", "192.0.2.2"}, true, []string{
			`nft add rule inet b4_route pre iifname "eth1" ip saddr { 192.0.2.1, 192.0.2.2 } return`,
			`nft add rule inet b4_route pre iifname "eth1" meta nfproto ipv6 return`,
		}},
		{"iptables one gateway with IPv6 off", &routeIptBackend{}, []string{"192.0.2.1"}, false, []string{
			"iptables -w -t mangle -A pre -i eth1 -s 192.0.2.1 -j RETURN",
		}},
		{"iptables two gateways and IPv6", &routeIptBackend{}, []string{"192.0.2.1", "192.0.2.2"}, true, []string{
			"iptables -w -t mangle -A pre -i eth1 -s 192.0.2.1 -j RETURN",
			"iptables -w -t mangle -A pre -i eth1 -s 192.0.2.2 -j RETURN",
			"ip6tables -w -t mangle -A pre -i eth1 -j RETURN",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var cmds []string
			prev := runLogged
			t.Cleanup(func() { runLogged = prev })
			runLogged = func(op string, args ...string) bool {
				cmds = append(cmds, strings.Join(args, " "))
				return true
			}
			if !c.be.addGatewayEgressGuard("pre", "eth1", c.gateways, c.ipv6) {
				t.Fatal("every rule installed, so the guard must report success")
			}
			if !slices.Equal(cmds, c.want) {
				t.Errorf("emitted:\n%s\nwant:\n%s", strings.Join(cmds, "\n"), strings.Join(c.want, "\n"))
			}
			runLogged = func(op string, args ...string) bool { return !strings.Contains(strings.Join(args, " "), "192.0.2.1") }
			if c.be.addGatewayEgressGuard("pre", "eth1", c.gateways, c.ipv6) {
				t.Error("a gateway rule that did not install must fail the guard")
			}
		})
	}
}

func TestRedirectDropFollowsTheIPv4Gateway(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		for _, c := range []struct {
			name, gw string
			want     bool
		}{
			{"ipv4 gateway", "192.0.2.1", true},
			{"no gateway", "", false},
			{"ipv6 gateway", "2001:db8:1::1", false},
		} {
			t.Run(engine+"/"+c.name, func(t *testing.T) {
				gwIsolate(t)
				cmds := gwStubCommands(t)
				stubBinaries(t, backendIPTables, backendIP6Tables, "ipset")
				gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24", "2001:db8:1::10/64"}})
				cfg := familyTestConfig(true, true)
				set := gwTestSet("rdset", "eth1", c.gw)
				set.Routing.RouterTraffic = config.RouterTrafficExclude
				cfg.Sets = []*config.SetConfig{set}
				st := gwResolvedState(cfg, set)
				var be routeBackend = &routeNftBackend{}
				want := `nft add rule inet b4_route ` + st.chainOut + ` oifname "eth1" icmp type redirect drop`
				if engine == backendIPTables {
					be = &routeIptBackend{}
					want = "iptables -w -t mangle -A " + st.chainOut + " -o eth1 -p icmp --icmp-type redirect -j DROP"
				}
				if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
					t.Fatalf("routeEnsureRule: %v", err)
				}
				drops := linesWith(chainRuleLines(cmds.lines, st.chainOut), "redirect")
				if c.want && !slices.Equal(drops, []string{want}) {
					t.Errorf("an IPv4 gateway set must drop IPv4 redirects through the egress even with router traffic excluded, got %v", drops)
				}
				if !c.want && len(drops) != 0 {
					t.Errorf("no IPv4 gateway in use, so no redirect drop, got %v", drops)
				}
				if got := cmds.with("nd-redirect"); len(got) != 0 {
					t.Errorf("the IPv6 guard stays full, so no ND redirect drop is needed, got %v", got)
				}
			})
		}
	}
}

func TestIPv6KeepsTheFullGuardWhateverTheGateway(t *testing.T) {
	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			gwIsolate(t)
			cmds := gwStubCommands(t)
			stubBinaries(t, backendIPTables, backendIP6Tables, "ipset")
			gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24", "2001:db8:1::10/64"}})
			cfg := familyTestConfig(true, true)
			v4 := gwTestSet("v4gw", "eth1", "192.0.2.1")
			v6 := gwTestSet("v6gw", "eth1", "2001:db8:1::1")
			v6.Routing.FWMark, v6.Routing.Table = 0x7e20, 233
			cfg.Sets = []*config.SetConfig{v4, v6}
			var be routeBackend = &routeNftBackend{}
			if engine == backendIPTables {
				be = &routeIptBackend{}
			}

			st4 := gwResolvedState(cfg, v4)
			if err := routeEnsureRule(be, cfg, v4, st4, nil); err != nil {
				t.Fatalf("routeEnsureRule: %v", err)
			}
			pre4 := chainRuleLines(cmds.lines, st4.chainPre)
			full6 := `iifname "eth1" meta nfproto ipv6 return`
			bare4 := `iifname "eth1" return`
			if engine == backendIPTables {
				full6 = "ip6tables -w -t mangle -A " + st4.chainPre + " -i eth1 -j RETURN"
				bare4 = "iptables -w -t mangle -A " + st4.chainPre + " -i eth1 -j RETURN"
			}
			if len(linesWith(pre4, full6)) != 1 {
				t.Errorf("IPv6 arriving on the egress keeps the full guard next to an IPv4 gateway:\n%s", strings.Join(pre4, "\n"))
			}
			if len(linesWith(pre4, bare4)) != 0 {
				t.Errorf("the IPv4 guard of a gateway set is narrowed, not full:\n%s", strings.Join(pre4, "\n"))
			}

			st6 := gwResolvedState(cfg, v6)
			if st6.gwV6 != "2001:db8:1::1" || st6.gwV4 != "" {
				t.Fatalf("resolved gateways = %q %q, want the IPv6 gateway alone", st6.gwV4, st6.gwV6)
			}
			if err := routeEnsureRule(be, cfg, v6, st6, nil); err != nil {
				t.Fatalf("routeEnsureRule: %v", err)
			}
			pre6 := chainRuleLines(cmds.lines, st6.chainPre)
			for _, needle := range []string{"saddr", " -s ", "fib daddr", "addrtype"} {
				if got := linesWith(pre6, needle); len(got) != 0 {
					t.Errorf("an IPv6 gateway changes only the IPv6 route and keeps the guard full, got %v", got)
				}
			}
			guards := linesWith(pre6, `"eth1"`)
			if engine == backendIPTables {
				guards = linesWith(pre6, "-i eth1")
			}
			want := 1
			if engine == backendIPTables {
				want = 2
			}
			if len(guards) != want {
				t.Errorf("an IPv6 gateway set keeps the bare guard of a set without a gateway, got %v", guards)
			}
		})
	}
}

func TestGatewayFieldsArePartOfTheState(t *testing.T) {
	base := routeState{iface: "eth1", egressGW: "192.0.2.1", gwV4: "192.0.2.1", gwPeersV4: []string{"192.0.2.1"}}
	for name, mutate := range map[string]func(*routeState){
		"egressGW":  func(st *routeState) { st.egressGW = "192.0.2.2" },
		"gwV4":      func(st *routeState) { st.gwV4 = "" },
		"gwV6":      func(st *routeState) { st.gwV6 = "2001:db8::1" },
		"gwPeersV4": func(st *routeState) { st.gwPeersV4 = []string{"192.0.2.1", "192.0.2.9"} },
	} {
		other := base
		other.gwPeersV4 = slices.Clone(base.gwPeersV4)
		mutate(&other)
		if routeStateEqual(base, other) {
			t.Errorf("routeStateEqual ignores %s, so the sync cache-hits and keeps the old route and guard", name)
		}
		if name != "egressGW" && !routeStateSameConfig(base, other) {
			t.Errorf("%s comes from the router, not the config, so the DNS path must not rebuild on it", name)
		}
	}
}

func TestAGatewayChangeOnAPinnedTableRebuildsTheSet(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	cmds := gwStubCommands(t)
	stubBinaries(t, "ip")
	gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	be := &mockRouteBackend{}
	routeEngine = be
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{gwTestSet("pinned", "lo", "192.0.2.1")}
	RoutingSyncConfig(cfg)
	first := gwCachedState(t, "pinned")

	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{gwTestSet("pinned", "lo", "192.0.2.2")}
	cmds.lines = nil
	RoutingSyncConfig(next)
	second := gwCachedState(t, "pinned")
	if first.mark != second.mark || first.table != second.table || second.table != 232 {
		t.Fatalf("the set must stay on its pinned mark and table, got 0x%x/%d then 0x%x/%d", first.mark, first.table, second.mark, second.table)
	}
	if routeStateEqual(first, second) || second.gwV4 != "192.0.2.2" {
		t.Fatalf("a new gateway on a pinned table must change the state, got %q", second.gwV4)
	}
	if got := gwLastOp(be.chainOps[second.chainPre], "gateway-guard"); got != "gateway-guard lo gw4=192.0.2.2 v6=false" {
		t.Errorf("the rebuilt chain must exempt the new gateway, got %q", got)
	}
	if got := cmds.with("route replace default via 192.0.2.2 dev lo"); len(got) != 1 || !strings.HasSuffix(got[0], "table 232") {
		t.Errorf("the pinned table must get the new gateway as its next hop, got %v", cmds.with("route replace default"))
	}
}

func TestReinstallReemitsTheGatewaySetsRoutes(t *testing.T) {
	familyResetGlobals(t)
	gwIsolate(t)
	cmds := gwStubCommands(t)
	stubBinaries(t, "ip")
	host := gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	be := &mockRouteBackend{}
	routeEngine = be
	cfg := familyTestConfig(true, false)
	set := gwTestSet("reinstall", "lo", "192.0.2.1")
	cfg.Sets = []*config.SetConfig{set}
	routeRuleCache[set.Id] = gwResolvedState(cfg, set)
	reads := host.reads
	RoutingReinstallForInterface(cfg, "lo")
	if got := cmds.with("route replace default via 192.0.2.1 dev lo"); len(got) != 1 {
		t.Errorf("the reinstall must re-emit the gateway default, ran:\n%s", strings.Join(cmds.lines, "\n"))
	}
	if host.reads != reads+1 {
		t.Errorf("the reinstall must check the gateway once against the router as it is now, read it %d times", host.reads-reads)
	}
	if len(be.chainOps) != 0 {
		t.Errorf("an unchanged gateway needs no rebuild, got %v", be.chainOps)
	}
}

func TestReinstallRebuildsWhenTheGatewayBecomesUsable(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	cmds := gwStubCommands(t)
	stubBinaries(t, "ip")
	host := gwStubHost(t, map[string][]string{"lo": {"10.9.9.1/24"}})
	be := &mockRouteBackend{}
	routeEngine = be
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{gwTestSet("flip", "lo", "192.0.2.1")}
	RoutingSyncConfig(cfg)
	before := gwCachedState(t, "flip")
	if before.gwV4 != "" || gwLastOp(be.chainOps[before.chainPre], "loop-guard") != "loop-guard lo" {
		t.Fatalf("a gateway lo cannot reach yet must start on the full guard, got %q %v", before.gwV4, be.chainOps[before.chainPre])
	}

	host.ifaces = gwHostIfaces(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	cmds.lines = nil
	RoutingReinstallForInterface(cfg, "lo")
	after := gwCachedState(t, "flip")
	if after.gwV4 != "192.0.2.1" {
		t.Fatalf("the reinstall must rebuild the set once its gateway is usable, state has %q", after.gwV4)
	}
	if got := gwLastOp(be.chainOps[after.chainPre], "gateway-guard"); got != "gateway-guard lo gw4=192.0.2.1 v6=false" {
		t.Errorf("the rebuilt chain must narrow the guard to the gateway, got %q", got)
	}
	if got := cmds.with("route replace default via 192.0.2.1 dev lo"); len(got) == 0 {
		t.Errorf("the rebuilt set must route through its gateway, ran:\n%s", strings.Join(cmds.lines, "\n"))
	}
}

func TestTheDNSPathReusesTheGatewayVerdict(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	cmds := gwStubCommands(t)
	stubBinaries(t, "ip")
	host := gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	be := &mockRouteBackend{}
	routeEngine = be
	cfg := familyTestConfig(true, false)
	set := gwTestSet("dnsgw", "lo", "192.0.2.1")
	cfg.Sets = []*config.SetConfig{set}
	RoutingSyncConfig(cfg)
	reads := host.reads
	cmds.lines = nil
	be.setOps = nil

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.8")})
	if host.reads != reads || len(cmds.lines) != 0 {
		t.Errorf("an answer for an installed gateway set must run nothing but the set update (%d address dumps, ran %v)", host.reads-reads, cmds.lines)
	}
	if !slices.Contains(be.setOps, "add "+gwCachedState(t, set.Id).setV4) {
		t.Errorf("the answer must still reach the set, ops %v", be.setOps)
	}

	moved := gwTestSet("dnsgw", "lo", "192.0.2.2")
	movedCfg := familyTestConfig(true, false)
	movedCfg.Sets = []*config.SetConfig{moved}
	RoutingHandleDNS(movedCfg, moved, []net.IP{net.ParseIP("198.51.100.9")})
	if host.reads != reads+1 {
		t.Errorf("a set the DNS path rebuilds must check its gateway once, got %d dumps", host.reads-reads)
	}
	if st := gwCachedState(t, set.Id); st.gwV4 != "192.0.2.2" {
		t.Errorf("the DNS path rebuilt the set without its new gateway, got %q", st.gwV4)
	}
}

func TestTheEnabledSetLineNamesTheGateway(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	gwStubCommands(t)
	stubBinaries(t, "ip")
	gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	logs := captureTablesLog(t)
	routeEngine = &mockRouteBackend{}
	cfg := familyTestConfig(true, false)
	withGW := gwTestSet("loggw", "lo", "192.0.2.1")
	without := gwTestSet("lognogw", "lo", "")
	without.Routing.FWMark, without.Routing.Table = 0x7e20, 233
	cfg.Sets = []*config.SetConfig{withGW, without}
	RoutingHandleDNS(cfg, withGW, []net.IP{net.ParseIP("198.51.100.8")})
	RoutingHandleDNS(cfg, without, []net.IP{net.ParseIP("198.51.100.9")})
	var gwLine, plainLine string
	for _, line := range strings.Split(logs.String(), "\n") {
		switch {
		case strings.Contains(line, "enabled set 'loggw'"):
			gwLine = line
		case strings.Contains(line, "enabled set 'lognogw'"):
			plainLine = line
		}
	}
	if !strings.Contains(gwLine, "iface=lo gw=192.0.2.1 mark=") {
		t.Errorf("the enabled-set line must name the gateway, got %q", gwLine)
	}
	if plainLine == "" || strings.Contains(plainLine, "gw=") {
		t.Errorf("a set without a gateway keeps the line it had, got %q", plainLine)
	}
}

func TestHostIfacesMatchTheKernelsPerInterfaceAddresses(t *testing.T) {
	got, err := routeHostIfacesExec()
	if err != nil {
		t.Skipf("no route netlink socket here: %v", err)
	}
	links, err := net.Interfaces()
	if err != nil {
		t.Skipf("no interface list here: %v", err)
	}
	for _, link := range links {
		addrs, err := link.Addrs()
		if err != nil {
			continue
		}
		var want, have []string
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				want = append(want, n.String())
			}
		}
		for _, n := range got[link.Name].addrs {
			have = append(have, n.String())
		}
		slices.Sort(want)
		slices.Sort(have)
		if !slices.Equal(want, have) {
			t.Errorf("%s: one dump gave %v, the per-interface lookup %v", link.Name, have, want)
		}
		if up := link.Flags&net.FlagUp != 0; got[link.Name].up != up {
			t.Errorf("%s: one dump says up=%v, the interface flags %v", link.Name, got[link.Name].up, up)
		}
	}
}
