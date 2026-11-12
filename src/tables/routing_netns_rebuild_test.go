package tables

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type rebuildSample struct {
	jump    bool
	rules   bool
	ipRule  bool
	route   bool
	members int
}

func rebuildConfig(engine string) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.Threads = 1
	cfg.Queue.Mark = 0x8000
	cfg.System.Tables.Engine = engine
	cfg.System.Tables.SkipSetup = false

	quiet := config.NewSetConfig()
	quiet.Id = "netns-rebuild-quiet"
	quiet.Name = "quiet"
	quiet.Enabled = true
	quiet.Routing.Enabled = true
	quiet.Routing.Mode = config.RoutingModeInterface
	quiet.Routing.EgressInterface = netnsSecondary
	quiet.Targets.IPs = []string{netnsTarget}
	quiet.Targets.IpsToMatch = []string{netnsTarget}

	moving := config.NewSetConfig()
	moving.Id = "netns-rebuild-moving"
	moving.Name = "moving"
	moving.Enabled = true
	moving.Routing.Enabled = true
	moving.Routing.Mode = config.RoutingModeInterface
	moving.Routing.EgressInterface = netnsSecondary
	moving.Routing.KillSwitch = true
	moving.Targets.IPs = []string{"198.51.100.8"}
	moving.Targets.IpsToMatch = []string{"198.51.100.8"}

	cfg.Sets = []*config.SetConfig{&quiet, &moving}
	return &cfg
}

func rebuildProbe(engine, chain, setName string) rebuildSample {
	s := rebuildSample{}
	if engine == backendNFTables {
		if out, err := run("nft", "list", "chain", "inet", routeNftTable, routeNftPrerouting); err == nil {
			s.jump = strings.Contains(out, "jump "+chain)
		}
		if out, err := run("nft", "list", "chain", "inet", routeNftTable, chain); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "table ") || strings.HasPrefix(line, "chain ") || line == "}" {
					continue
				}
				s.rules = true
			}
		}
	} else {
		if out, err := run("iptables", "-t", "mangle", "-L", "PREROUTING", "-n"); err == nil {
			s.jump = strings.Contains(out, chain)
		}
		if out, err := run("iptables", "-t", "mangle", "-L", chain, "-n"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "Chain ") || strings.HasPrefix(line, "target ") {
					continue
				}
				s.rules = true
			}
		}
		if out, err := run("ipset", "list", setName); err == nil {
			body := false
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Members") {
					body = true
					continue
				}
				if body && line != "" {
					s.members++
				}
			}
		} else {
			s.members = -1
		}
	}

	out, err := run("ip", "rule", "show")
	if err != nil {
		return s
	}
	var tables []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasSuffix(routeRuleField(line, "fwmark"), "/0x27fff") {
			continue
		}
		if lookup := routeRuleField(line, "lookup"); lookup != "" {
			tables = append(tables, lookup)
		}
	}
	if len(tables) == 0 {
		return s
	}
	s.ipRule = true
	s.route = true
	for _, table := range tables {
		routes, routeErr := run("ip", "route", "show", "table", table)
		if routeErr != nil {
			s.route = false
			continue
		}
		reachable := false
		for _, line := range strings.Split(routes, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "default") {
				reachable = true
			}
		}
		if !reachable {
			s.route = false
		}
	}
	return s
}

func netnsRebuildIsNeverDisarmed(t *testing.T, engine string) {
	t.Helper()
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := rebuildConfig(engine)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	routeMu.Lock()
	st, cached := routeRuleCache["netns-rebuild-moving"]
	routeMu.Unlock()
	if !cached {
		t.Fatal("the set built no routing state")
	}

	learned := []string{"203.0.113.9", "203.0.113.10"}
	getRouteBackend(cfg).addElements(st.setV4, learned, 3600)

	setName := st.setV4
	if engine == backendNFTables {
		setName = ""
	}
	if base := rebuildProbe(engine, st.chainPre, setName); !base.jump || !base.rules || !base.ipRule || !base.route {
		t.Fatalf("the set is not fully installed before the test even starts: %+v", base)
	}

	var mu sync.Mutex
	var disarmed []rebuildSample
	minMembers := 1 << 30
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s := rebuildProbe(engine, st.chainPre, setName)
			mu.Lock()
			if !s.jump || !s.rules || !s.ipRule || !s.route {
				disarmed = append(disarmed, s)
			}
			if setName != "" && s.members < minMembers {
				minMembers = s.members
			}
			mu.Unlock()
		}
	}()

	for cycle := 0; cycle < 2; cycle++ {
		cfg.Sets[1].Routing.EgressInterface = netnsPrimary
		RoutingSyncConfig(cfg)
		time.Sleep(200 * time.Millisecond)
		cfg.Sets[1].Routing.EgressInterface = netnsSecondary
		RoutingSyncConfig(cfg)
		time.Sleep(200 * time.Millisecond)
	}
	RoutingForceResync(cfg)
	time.Sleep(200 * time.Millisecond)
	cfg.Sets[0].Routing.EgressInterface = netnsPrimary
	RoutingSyncConfig(cfg)
	time.Sleep(200 * time.Millisecond)
	cfg.Sets[0].Routing.EgressInterface = netnsSecondary
	RoutingSyncConfig(cfg)
	time.Sleep(200 * time.Millisecond)

	close(stop)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(disarmed) > 0 {
		t.Fatalf("reconfiguring a set left it matching nothing in %d samples, so every connection opened in that window leaves by the ordinary uplink and conntrack keeps it there for its whole life; first: %+v",
			len(disarmed), disarmed[0])
	}
	if setName != "" && minMembers < len(learned)+1 {
		t.Fatalf("the set was down to %d addresses during the rebuild, so the addresses it learned from DNS were thrown away and traffic to them stopped being routed until they were learned again",
			minMembers)
	}
}

func TestNetnsAReconfiguredSetIsNeverDisarmed(t *testing.T) {
	netnsRebuildIsNeverDisarmed(t, backendIPTables)
}

func TestNetnsAReconfiguredSetIsNeverDisarmedNft(t *testing.T) {
	netnsRebuildIsNeverDisarmed(t, backendNFTables)
}

func rebuildChainRuleCount(chain string) int {
	out, err := run("iptables", "-t", "mangle", "-S", chain)
	if err != nil {
		return -1
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "-A ") {
			n++
		}
	}
	return n
}

func rebuildPolicyRulePresent(mark uint32) bool {
	out, err := run("ip", "rule", "show")
	if err != nil {
		return false
	}
	want := routeSetMarkRule(mark)
	for _, line := range strings.Split(out, "\n") {
		if routeRuleField(line, "fwmark") == want {
			return true
		}
	}
	return false
}

func TestNetnsAFailedEditLeavesThePreviousStateWorking(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := rebuildConfig(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	routeMu.Lock()
	before, cached := routeRuleCache["netns-rebuild-moving"]
	routeMu.Unlock()
	if !cached {
		t.Fatal("the set built no routing state")
	}

	learned := []string{"203.0.113.9", "203.0.113.10"}
	getRouteBackend(cfg).addElements(before.setV4, learned, 3600)

	rulesBefore := rebuildChainRuleCount(before.chainPre)
	if rulesBefore <= 0 || !rebuildPolicyRulePresent(before.mark) {
		t.Fatalf("the set is not fully installed before the test even starts: %d chain rules", rulesBefore)
	}
	membersBefore, _ := run("ipset", "list", before.setV4)

	cfg.Sets[1].Routing.EgressInterface = "this-name-is-far-too-long-for-an-interface"
	RoutingSyncConfig(cfg)

	routeMu.Lock()
	after, stillCached := routeRuleCache["netns-rebuild-moving"]
	routeMu.Unlock()

	if !stillCached {
		t.Fatalf("an edit b4 could not install dropped the set from its cache, so nothing re-asserts the rules still standing in the kernel")
	}
	if after.mark != before.mark || after.table != before.table {
		t.Fatalf("after a failed rebuild the cache holds mark 0x%x table %d, but mark 0x%x table %d is what is still installed",
			after.mark, after.table, before.mark, before.table)
	}
	if !rebuildPolicyRulePresent(before.mark) {
		t.Errorf("an edit b4 could not install took the policy rule away and left the marking chain behind, so the set marks traffic toward a table nothing points at")
	}
	if got := rebuildChainRuleCount(before.chainPre); got < rulesBefore {
		t.Errorf("an edit b4 could not install cut the rules that were working: %d -> %d", rulesBefore, got)
	}
	membersAfter, _ := run("ipset", "list", before.setV4)
	if a, b := strings.Count(membersAfter, "203.0.113."), strings.Count(membersBefore, "203.0.113."); a < b {
		t.Errorf("an edit b4 could not install threw away addresses the set had learned: %d -> %d", b, a)
	}
}

func netnsARebuiltInterfaceSetDoesNotStackItsRule(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(engine)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	for i := 0; i < 3; i++ {
		RoutingForceResync(cfg)
		cfg.Sets[0].Routing.KillSwitch = !cfg.Sets[0].Routing.KillSwitch
		RoutingSyncConfig(cfg)
		RoutingReinstallForInterface(cfg, netnsSecondary)
		RoutingReconcilePolicyRules(cfg)
	}

	routeMu.Lock()
	_, cached := routeRuleCache[cfg.Sets[0].Id]
	routeMu.Unlock()
	if !cached {
		t.Fatal("the set built no routing state")
	}

	var found []string
	for _, line := range strings.Split(netnsRun(t, "ip", "rule", "show"), "\n") {
		if strings.HasSuffix(routeRuleField(line, "fwmark"), fmt.Sprintf("/0x%x", routeSetMarkMask)) {
			found = append(found, strings.TrimSpace(line))
		}
	}
	if len(found) != 1 {
		t.Fatalf("six rebuilds, three interface reinstalls and three monitor passes left %d rules sending the set's mark to its table, want one. An old kernel takes an identical rule a second time instead of refusing it (3.16 does, 4.9 refuses it), so a rule added without looking first is stacked once more on every rebuild; on a router whose firmware rewrites the firewall every few minutes that is hundreds of rules, each walked for every packet the router routes: %v", len(found), found)
	}
}

func TestNetnsARebuiltInterfaceSetDoesNotStackItsRule(t *testing.T) {
	netnsARebuiltInterfaceSetDoesNotStackItsRule(t, backendIPTables)
}

func TestNetnsARebuiltInterfaceSetDoesNotStackItsRuleNft(t *testing.T) {
	netnsARebuiltInterfaceSetDoesNotStackItsRule(t, backendNFTables)
}

const (
	netnsIfaceDialGap     = 10 * time.Millisecond
	netnsIfaceDialTimeout = 40 * time.Millisecond
)

func netnsCountersIn(t *testing.T, ipt, table, base, chain string, rules ...[]string) func() []int {
	t.Helper()
	at := []string{ipt, "-w", "-t", table}
	remove := func() {
		for {
			if _, err := run(append(at, "-D", base, "-j", chain)...); err != nil {
				break
			}
		}
		_, _ = run(append(at, "-F", chain)...)
		_, _ = run(append(at, "-X", chain)...)
	}
	remove()
	netnsRun(t, append(at, "-N", chain)...)
	t.Cleanup(remove)
	for _, rule := range rules {
		netnsRun(t, append(append(at, "-A", chain), rule...)...)
	}
	netnsRun(t, append(at, "-I", base, "1", "-j", chain)...)
	return func() []int {
		out := netnsRun(t, append(at, "-L", chain, "-v", "-x", "-n")...)
		var counts []int
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			if n, err := strconv.Atoi(f[0]); err == nil {
				counts = append(counts, n)
			}
		}
		if len(counts) != len(rules) {
			t.Fatalf("%s %s lists %d counters, want %d:\n%s", table, chain, len(counts), len(rules), out)
		}
		return counts
	}
}

func netnsARebuiltInterfaceSetKeepsTheRoutersOwnTraffic(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)

	forwarding, err := os.ReadFile(netnsForwardSysctl)
	if err != nil {
		t.Skipf("%s is not readable in this namespace: %v", netnsForwardSysctl, err)
	}
	if err := os.WriteFile(netnsForwardSysctl, []byte("0"), 0644); err != nil {
		t.Skipf("cannot turn off forwarding in this namespace: %v", err)
	}
	t.Cleanup(func() { _ = os.WriteFile(netnsForwardSysctl, forwarding, 0644) })

	peer := netnsSecondary + "p"
	netnsRun(t, "ip", "neigh", "replace", netnsTarget, "lladdr", netnsLinkMAC(t, peer), "dev", netnsSecondary, "nud", "permanent")
	t.Cleanup(func() { _, _ = run("ip", "neigh", "del", netnsTarget, "dev", netnsSecondary) })

	routeEngine = nil
	cfg := netnsConfig(engine)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	RoutingSyncConfig(cfg)
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(cfg)
		routeEngine = nil
	})

	ipt := netnsCounterBinary(engine)
	sent := netnsCountersIn(t, ipt, "mangle", "POSTROUTING", "b4t_cnt_out",
		[]string{"-o", netnsPrimary, "-d", netnsTarget, "-p", "tcp"},
		[]string{"-o", netnsSecondary, "-d", netnsTarget, "-p", "tcp"})
	seen := netnsCountersIn(t, ipt, "raw", "PREROUTING", "b4t_cnt_in",
		[]string{"-i", peer, "-s", netnsPrimaryIP, "-d", netnsTarget, "-p", "tcp"},
		[]string{"-i", peer, "-s", netnsSecondIP, "-d", netnsTarget, "-p", "tcp"})

	addr := net.JoinHostPort(netnsTarget, "443")
	var dials sync.WaitGroup
	dialFor := func(span time.Duration) int {
		started := 0
		for end := time.Now().Add(span); time.Now().Before(end); {
			started++
			dials.Add(1)
			go func() {
				defer dials.Done()
				if conn, err := net.DialTimeout("tcp", addr, netnsIfaceDialTimeout); err == nil {
					conn.Close()
				}
			}()
			time.Sleep(netnsIfaceDialGap)
		}
		dials.Wait()
		return started
	}

	calm := dialFor(time.Second)
	out, in := sent(), seen()
	t.Logf("%s: with no rebuild running, %d connections: %d packet(s) left by the uplink and %d by the set's interface, where %d kept the uplink's address and %d carried the interface's",
		engine, calm, out[0], out[1], in[0], in[1])
	if out[1] == 0 {
		netnsLogState(t, engine)
		t.Fatal("the set does not take the router's own connections before any rebuild")
	}
	if out[0] != 0 || in[0] != 0 || in[1] == 0 {
		t.Skipf("this namespace cannot show what a rebuild does to the router's own connections: with nothing being rebuilt %d packet(s) left by the uplink, and of those that left by the set's interface %d kept the uplink's address and %d carried the interface's", out[0], in[0], in[1])
	}

	stop := make(chan struct{})
	rebuilt := make(chan int, 1)
	go func() {
		n := 0
		for {
			select {
			case <-stop:
				rebuilt <- n
				return
			default:
			}
			RoutingForceResync(cfg)
			cfg.Sets[0].Routing.KillSwitch = !cfg.Sets[0].Routing.KillSwitch
			RoutingSyncConfig(cfg)
			n += 2
		}
	}()
	started := dialFor(3 * time.Second)
	close(stop)
	rebuilds := <-rebuilt

	after, afterIn := sent(), seen()
	leaked, routed := after[0], after[1]-out[1]
	unrewritten, rewritten := afterIn[0], afterIn[1]-in[1]
	t.Logf("%s: %d rebuilds under %d connections: %d packet(s) left by the uplink and %d by the set's interface, where %d kept the uplink's address and %d carried the interface's",
		engine, rebuilds, started, leaked, routed, unrewritten, rewritten)
	if rebuilds < 2 || routed == 0 || rewritten == 0 {
		t.Fatalf("the test measured nothing: %d rebuilds, %d packet(s) by the set's interface, %d of them seen at its far end", rebuilds, routed, rewritten)
	}
	if leaked != 0 || unrewritten != 0 {
		netnsLogState(t, engine)
		t.Fatalf("while the set was rebuilt %d time(s) under %d of the router's own connections, %d packet(s) for the address it routes left by the ordinary uplink, and %d left by the set's interface still carrying the uplink's address. A rebuild must never take a set's jump out before its replacement is in: without the jump that marks the router's own traffic a connection opened in that moment leaves unrouted, and without the jump that rewrites the source it leaves by the right interface with an address the far end cannot answer; conntrack then keeps either mistake for the life of the connection",
			rebuilds, started, leaked, unrewritten)
	}
}

func TestNetnsARebuiltInterfaceSetKeepsTheRoutersOwnTraffic(t *testing.T) {
	netnsARebuiltInterfaceSetKeepsTheRoutersOwnTraffic(t, backendIPTables)
}

func TestNetnsARebuiltInterfaceSetKeepsTheRoutersOwnTrafficNft(t *testing.T) {
	netnsARebuiltInterfaceSetKeepsTheRoutersOwnTraffic(t, backendNFTables)
}

func TestNetnsARebuiltInterfaceSetKeepsTheRoutersOwnTrafficLegacy(t *testing.T) {
	netnsARebuiltInterfaceSetKeepsTheRoutersOwnTraffic(t, backendIPTablesLegacy)
}
