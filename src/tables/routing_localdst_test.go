package tables

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func localGuardReset(t *testing.T) {
	t.Helper()
	routeLocalGuardForget()
	proxyOutMarkForget()
	t.Cleanup(func() {
		routeLocalGuardForget()
		proxyOutMarkForget()
	})
}

func captureProxyRuleArgv(t *testing.T) *[]string {
	t.Helper()
	emitted := stubProxyRuleSideEffects(t)
	run = func(args ...string) (string, error) {
		*emitted = append(*emitted, strings.Join(args, " "))
		return "", nil
	}
	return emitted
}

func chainRuleLines(emitted []string, chain string) []string {
	var out []string
	for _, line := range emitted {
		if strings.Contains(line, " -A "+chain+" ") || strings.Contains(line, "add rule inet "+routeNftTable+" "+chain+" ") {
			out = append(out, line)
		}
	}
	return out
}

func firstLineWith(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func countLinesWith(lines []string, needle string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, needle) {
			n++
		}
	}
	return n
}

func v4OnlyConfig() *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	return &cfg
}

func TestProxyPreChainLocalGuardSitsBetweenTheClaimedGuardAndTheDivert(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables)

	cfg := v4OnlyConfig()
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)

	if err := routeEnsureProxyRule(&routeIptBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}

	pre := chainRuleLines(*emitted, st.chainPre)
	guard := firstLineWith(pre, "-m addrtype --dst-type LOCAL,BROADCAST,MULTICAST -j RETURN")
	claimed := firstLineWith(pre, "! --mark 0x0/0x27fff")
	divert := firstLineWith(pre, "-m socket --transparent")
	tproxy := firstLineWith(pre, "-j TPROXY")
	if guard < 0 || claimed < 0 || divert < 0 || tproxy < 0 {
		t.Fatalf("expected the claimed guard, the local guard, the divert and the tproxy rule in %s, got:\n%s", st.chainPre, strings.Join(pre, "\n"))
	}
	if !(claimed < guard && guard < divert && divert < tproxy) {
		t.Errorf("the local destination guard must sit below the claimed-mark guard and above the divert and tproxy rules (claimed=%d guard=%d divert=%d tproxy=%d):\n%s",
			claimed, guard, divert, tproxy, strings.Join(pre, "\n"))
	}
	if n := countLinesWith(pre, "addrtype"); n != 1 {
		t.Errorf("expected exactly one address-type rule in %s, got %d", st.chainPre, n)
	}
	if n := countLinesWith(chainRuleLines(*emitted, st.chainOut), "addrtype"); n != 0 {
		t.Errorf("the out chain carries the router's own connections over the local route, so a local-destination guard there would return them: %v", chainRuleLines(*emitted, st.chainOut))
	}
}

func TestProxyPreChainLocalGuardIPv6UsesSeparateRulesAndNeverBroadcast(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables, backendIP6Tables)

	cfg := v4OnlyConfig()
	cfg.Queue.IPv6Enabled = true
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)
	st.ipv6 = true

	if err := routeEnsureProxyRule(&routeIptBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}

	var v6 []string
	for _, line := range chainRuleLines(*emitted, st.chainPre) {
		if strings.HasPrefix(line, backendIP6Tables+" ") {
			v6 = append(v6, line)
		}
	}
	for _, want := range []string{
		"-m addrtype --dst-type LOCAL -j RETURN",
		"-d ff00::/8 -j RETURN",
		"-d fe80::/10 -j RETURN",
	} {
		idx := firstLineWith(v6, want)
		if idx < 0 {
			t.Errorf("ip6tables pre chain lacks %q:\n%s", want, strings.Join(v6, "\n"))
			continue
		}
		if divert := firstLineWith(v6, "-m socket --transparent"); divert >= 0 && idx > divert {
			t.Errorf("%q sits below the ip6tables divert rule", want)
		}
	}
	for _, line := range v6 {
		if strings.Contains(line, "BROADCAST") || strings.Contains(line, "LOCAL,") {
			t.Errorf("ip6tables rejects BROADCAST and ANDs a type list, so this rule either fails to load or matches nothing: %q", line)
		}
	}
	if firstLineWith(chainRuleLines(*emitted, st.chainPre), "iptables -w -t mangle -A "+st.chainPre+" -m addrtype --dst-type LOCAL,BROADCAST,MULTICAST -j RETURN") < 0 {
		t.Errorf("the iptables rule must keep the combined type list")
	}
}

func TestProxyPreChainLocalGuardNftIsOneFibRuleWithoutMark(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)

	cfg := v4OnlyConfig()
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)

	if err := routeEnsureProxyRule(&routeNftBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}

	pre := chainRuleLines(*emitted, st.chainPre)
	fib := firstLineWith(pre, "fib daddr type { local , broadcast , multicast } return")
	divert := firstLineWith(pre, "socket transparent 1")
	tproxy := firstLineWith(pre, "tproxy ip to")
	if fib < 0 || divert < 0 || tproxy < 0 {
		t.Fatalf("expected the fib guard, the divert and the tproxy rule in %s, got:\n%s", st.chainPre, strings.Join(pre, "\n"))
	}
	if !(fib < divert && divert < tproxy) {
		t.Errorf("the fib guard must precede the divert and tproxy rules (fib=%d divert=%d tproxy=%d)", fib, divert, tproxy)
	}
	if n := countLinesWith(pre, "fib daddr"); n != 1 {
		t.Errorf("expected one fib rule, got %d", n)
	}
	if strings.Contains(pre[fib], "mark") || strings.Contains(pre[fib], "iif") {
		t.Errorf("a mark-aware or interface-aware lookup classifies the router's own loopback trip as local: %q", pre[fib])
	}
	if firstLineWith(pre, "fe80::/10") >= 0 {
		t.Errorf("the IPv6 link-local rule was emitted with IPv6 disabled")
	}
	if n := countLinesWith(chainRuleLines(*emitted, st.chainOut), "fib daddr"); n != 0 {
		t.Errorf("the out chain must not carry the guard")
	}
}

func testCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	n.IP = ip
	return n
}

func TestProxyLocalGuardFallsBackToAnAddressListWhenTheMatchIsRejected(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables)

	origAddrs := routeHostAddrs
	t.Cleanup(func() { routeHostAddrs = origAddrs })
	routeHostAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			testCIDR(t, "127.0.0.1/8"),
			testCIDR(t, "192.168.1.1/24"),
			testCIDR(t, "10.8.0.2/32"),
			testCIDR(t, "203.0.113.5/30"),
			testCIDR(t, "fe80::1/64"),
			testCIDR(t, "fd00::1/64"),
		}, nil
	}
	rejections := 0
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		*emitted = append(*emitted, joined)
		if strings.Contains(joined, "addrtype") {
			rejections++
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		return "", nil
	}

	cfg := v4OnlyConfig()
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)
	if err := routeEnsureProxyRule(&routeIptBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule must not fail when the guard cannot load: %v", err)
	}

	pre := chainRuleLines(*emitted, st.chainPre)
	divert := firstLineWith(pre, "-m socket --transparent")
	if firstLineWith(pre, "-j TPROXY") < 0 {
		t.Fatalf("the tproxy rules were not emitted:\n%s", strings.Join(pre, "\n"))
	}
	for _, prefix := range []string{
		"127.0.0.0/8", "224.0.0.0/4", "255.255.255.255/32",
		"192.168.1.1/32", "192.168.1.255/32", "10.8.0.2/32", "203.0.113.5/32", "203.0.113.7/32",
	} {
		idx := firstLineWith(pre, "-d "+prefix+" -j RETURN")
		if idx < 0 {
			t.Errorf("fallback list lacks %s:\n%s", prefix, strings.Join(pre, "\n"))
			continue
		}
		if idx > divert {
			t.Errorf("fallback rule for %s sits below the divert", prefix)
		}
	}
	for _, unwanted := range []string{"fd00::1", "fe80::1", "127.0.0.1/32"} {
		if firstLineWith(pre, unwanted) >= 0 {
			t.Errorf("fallback list carries %s, which does not belong in the IPv4 chain", unwanted)
		}
	}

	kmodStateMu.Lock()
	reason := kmodRejected["xt_addrtype"]
	kmodStateMu.Unlock()
	if reason == "" {
		t.Errorf("the rejection was not recorded for diagnostics")
	}

	second := orderTestSet("guard2", config.RoutingModeProxy, nil)
	st2 := proxyGuardState(0x239ca)
	st2.setID = "guard2"
	st2.chainPre, st2.chainOut = "b4r_guard2_pre", "b4r_guard2_out"
	if err := routeEnsureProxyRule(&routeIptBackend{}, cfg, second, st2, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	if rejections != 1 {
		t.Errorf("the rejected match was tried %d times; a rejection is remembered for the rest of the run", rejections)
	}
	if firstLineWith(chainRuleLines(*emitted, st2.chainPre), "-d 192.168.1.1/32 -j RETURN") < 0 {
		t.Errorf("the second set did not get the fallback list")
	}
}

func TestProxyLocalGuardNftFallsBackToPerPrefixRules(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)

	origAddrs := routeHostAddrs
	t.Cleanup(func() { routeHostAddrs = origAddrs })
	routeHostAddrs = func() ([]net.Addr, error) {
		return []net.Addr{testCIDR(t, "192.168.1.1/24")}, nil
	}
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		*emitted = append(*emitted, joined)
		if strings.Contains(joined, "fib daddr") {
			return "Error: Could not process rule: No such file or directory", errors.New("exit status 1")
		}
		return "", nil
	}

	cfg := v4OnlyConfig()
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)
	if err := routeEnsureProxyRule(&routeNftBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	pre := chainRuleLines(*emitted, st.chainPre)
	divert := firstLineWith(pre, "socket transparent 1")
	for _, prefix := range []string{"127.0.0.0/8", "224.0.0.0/4", "255.255.255.255/32", "192.168.1.1/32", "192.168.1.255/32"} {
		idx := firstLineWith(pre, "ip daddr "+prefix+" return")
		if idx < 0 || idx > divert {
			t.Errorf("fallback rule for %s missing or below the divert (idx=%d divert=%d):\n%s", prefix, idx, divert, strings.Join(pre, "\n"))
		}
	}
}

func TestProxyLocalGuardIsEmittedForMTProtoWSSets(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables)

	cfg := v4OnlyConfig()
	set := orderTestSet("guard", config.RoutingModeMTProtoWS, nil)
	st := proxyGuardState(0x239c9)
	st.mode = config.RoutingModeMTProtoWS
	if err := routeEnsureProxyRule(&routeIptBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	if firstLineWith(chainRuleLines(*emitted, st.chainPre), "-m addrtype --dst-type LOCAL,BROADCAST,MULTICAST -j RETURN") < 0 {
		t.Errorf("the mtproto-ws chain shares the proxy emitter and must carry the guard")
	}
}

func TestInterfaceAndBlockChainsCarryNoLocalGuard(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables)

	cfg := v4OnlyConfig()
	iface := orderTestSet("iface", config.RoutingModeInterface, nil)
	ifaceSt := routeState{
		setID: "iface", mode: config.RoutingModeInterface, mark: 0x1b1d, table: 100, iface: "wg0", ipv4: true,
		setV4: "b4r_iface_v4", setV6: "b4r_iface_v6", chainPre: "b4r_iface_pre", chainOut: "b4r_iface_out", chainSNAT: "b4r_iface_snat",
	}
	if err := routeEnsureRule(&routeIptBackend{}, cfg, iface, ifaceSt, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}
	block := orderTestSet("blk", config.RoutingModeBlock, nil)
	blockSt := routeState{
		setID: "blk", mode: config.RoutingModeBlock, blockAction: "drop", ipv4: true,
		setV4: "b4r_blk_v4", setV6: "b4r_blk_v6", chainPre: "b4r_blk_pre",
	}
	if err := routeEnsureBlockRule(&routeIptBackend{}, cfg, block, blockSt, nil); err != nil {
		t.Fatalf("routeEnsureBlockRule: %v", err)
	}
	for _, line := range *emitted {
		if strings.Contains(line, "addrtype") || strings.Contains(line, "fib daddr") {
			t.Errorf("only the tproxy chains divert into a listener, so no other mode needs the guard: %q", line)
		}
	}
}

func TestKmodPackageTableNamesTheAddrtypeAndFibModules(t *testing.T) {
	newKmodFixture(t)
	got := strings.Join(kmodPkgsFor([]string{"xt_addrtype"}), " ")
	for _, want := range []string{"kmod-ipt-extra", "iptables-mod-extra"} {
		if !strings.Contains(got, want) {
			t.Errorf("packages for xt_addrtype lack %s: %q", want, got)
		}
	}
	if got := strings.Join(kmodPkgsFor([]string{"nft_fib_inet"}), " "); !strings.Contains(got, "kmod-nft-fib") {
		t.Errorf("packages for nft_fib_inet lack kmod-nft-fib: %q", got)
	}
}

func TestRoutingRulesPresentIgnoresTheLocalGuard(t *testing.T) {
	dump := `Chain b4r_x_pre (1 references)
    pkts      bytes target     prot opt in     out     source               destination
       0        0 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            mark match 0x8000/0x8000
       0        0 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            mark match 0x40000/0x40000
       0        0 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            ADDRTYPE match dst-type LOCAL,BROADCAST,MULTICAST
       0        0 TPROXY     tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            match-set b4r_x_v4 dst TPROXY redirect 0.0.0.0:13001 mark 0x239c9/0x239c9
`
	chains := parseIptDump(dump)
	for _, m := range []uint32{0x8000, 0x40000} {
		if !iptDumpReturnsOn(chains, "b4r_x_pre", m) {
			t.Errorf("the bypass on 0x%x is no longer seen once the address-type rule is in the chain", m)
		}
	}

	nft := `table inet b4_route {
	chain b4r_x_pre {
		meta mark & 0x00008000 == 0x00008000 return
		meta mark & 0x00040000 == 0x00040000 return
		fib daddr type { local, broadcast, multicast } return
		ip protocol tcp ip daddr @b4r_x_v4 meta mark set 0x00020001 tproxy ip to :13001 accept
	}
}`
	present, bypass := parseNftRouteChains(nft)
	if !present["b4r_x_pre"] {
		t.Fatalf("chain not seen: %v", present)
	}
	for _, m := range []uint32{0x8000, 0x40000} {
		if !bypass["b4r_x_pre"][m] {
			t.Errorf("the nft bypass on 0x%x is no longer seen once the fib rule is in the chain: %v", m, bypass)
		}
	}
}

func TestProxyOutputMarkRuleClaimsOnlyTheOriginalDirection(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables)

	cfg := v4OnlyConfig()
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)
	if err := routeEnsureProxyRule(&routeIptBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	out := chainRuleLines(*emitted, st.chainOut)
	want := "-A b4r_guard_out -p tcp -m conntrack --ctdir ORIGINAL -m set --match-set b4r_guard_v4 dst -j MARK --set-mark 0x239c9/0x239c9"
	mark := firstLineWith(out, want)
	if mark < 0 {
		t.Fatalf("expected %q in the out chain:\n%s", want, strings.Join(out, "\n"))
	}
	for _, line := range out {
		if strings.Contains(line, "-j MARK") && !strings.Contains(line, "--ctdir ORIGINAL") {
			t.Errorf("an unqualified mark rule marks the replies of the router's own services to addresses in the set: %q", line)
		}
	}
	for _, guard := range []string{"--mark 0x8000/0x8000 -j RETURN", "--mark 0x40000/0x40000 -j RETURN", "! --mark 0x0/0x27fff"} {
		if idx := firstLineWith(out, guard); idx < 0 || idx > mark {
			t.Errorf("%q must precede the mark rule (idx=%d mark=%d)", guard, idx, mark)
		}
	}
}

func TestProxyOutputMarkRuleNftClaimsOnlyTheOriginalDirection(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)

	cfg := v4OnlyConfig()
	set := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)
	if err := routeEnsureProxyRule(&routeNftBackend{}, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	out := chainRuleLines(*emitted, st.chainOut)
	for _, sn := range []string{"b4r_guard_v4", "b4r_guard_v4_d"} {
		if firstLineWith(out, "ip protocol tcp ct direction original ip daddr @"+sn+" meta mark set 0x239c9") < 0 {
			t.Errorf("no direction-qualified mark rule for %s:\n%s", sn, strings.Join(out, "\n"))
		}
	}
	for _, line := range out {
		if strings.Contains(line, "meta mark set") && !strings.Contains(line, "ct direction original") {
			t.Errorf("unqualified mark rule: %q", line)
		}
	}
}

func TestProxyOutputMarkRuleFallsBackWhenConntrackIsRejected(t *testing.T) {
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables)

	tries := 0
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		*emitted = append(*emitted, joined)
		if strings.Contains(joined, "--ctdir") {
			tries++
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		return "", nil
	}

	cfg := v4OnlyConfig()
	for i, id := range []string{"guard", "guard2"} {
		set := orderTestSet(id, config.RoutingModeProxy, nil)
		st := proxyGuardState(0x239c9 + uint32(i))
		st.setID = id
		st.setV4, st.setV6 = "b4r_"+id+"_v4", "b4r_"+id+"_v6"
		st.chainPre, st.chainOut = "b4r_"+id+"_pre", "b4r_"+id+"_out"
		if err := routeEnsureProxyRule(&routeIptBackend{}, cfg, set, st, nil); err != nil {
			t.Fatalf("routeEnsureProxyRule: %v", err)
		}
		out := chainRuleLines(*emitted, st.chainOut)
		wantMarks := 1
		if i == 0 {
			wantMarks = 2
		}
		if n := countLinesWith(out, "-j MARK"); n != wantMarks {
			t.Errorf("%s: expected %d mark lines (the rejected attempt on the first set only, then the unqualified rule), got %d:\n%s", id, wantMarks, n, strings.Join(out, "\n"))
		}
		if firstLineWith(out, "-A "+st.chainOut+" -p tcp -m set --match-set "+st.setV4+" dst -j MARK --set-mark") < 0 {
			t.Errorf("%s: no unqualified fallback rule:\n%s", id, strings.Join(out, "\n"))
		}
	}
	if tries != 1 {
		t.Errorf("the rejected direction match was tried %d times; a rejection is remembered for the rest of the run", tries)
	}
}

func TestProxyOutputChainCapsRouterOriginatedNewConnections(t *testing.T) {
	localGuardReset(t)
	stubProxyRuleSideEffects(t)

	cfg := v4OnlyConfig()
	cfg.Queue.IPv6Enabled = true
	unscoped := orderTestSet("guard", config.RoutingModeProxy, nil)
	st := proxyGuardState(0x239c9)
	st.ipv6 = true

	be := &mockRouteBackend{}
	if err := routeEnsureProxyRule(be, cfg, unscoped, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	families := map[bool]bool{}
	for _, g := range be.guards {
		if g.chain != st.chainOut {
			t.Errorf("the rate guard belongs in the out chain, not %s", g.chain)
		}
		families[g.v6] = true
	}
	if !families[false] || !families[true] {
		t.Errorf("an unscoped proxy set carries the router's own connections, including those of an upstream running on the router, so its out chain needs the rate guard per family: %+v", be.guards)
	}
	ops := be.chainOps[st.chainOut]
	if guard, claimed := indexOfOp(ops, "router-traffic-guard"), indexOfOp(ops, "claimed-bypass 0x0"); guard < 0 || claimed < 0 || guard < claimed {
		t.Errorf("the guard must follow the bypass rules: %v", ops)
	}

	scoped := orderTestSet("scoped", config.RoutingModeProxy, []string{"AA:BB:CC:DD:EE:FF"})
	stScoped := proxyGuardState(0x239ca)
	stScoped.setID, stScoped.srcScoped = "scoped", true
	stScoped.chainPre, stScoped.chainOut = "b4r_scoped_pre", "b4r_scoped_out"
	be2 := &mockRouteBackend{}
	if err := routeEnsureProxyRule(be2, cfg, scoped, stScoped, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	if len(be2.guards) != 0 {
		t.Errorf("a source-scoped set leaves the router's own traffic alone, so it needs no guard: %+v", be2.guards)
	}
}
