package tables

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

var netnsFirmwareLocal443Drop = []string{"INPUT", "-p", "tcp", "--dport", "443", "-m", "length", "--length", "100:65535", "-j", "DROP"}

func netnsFirmwareInputRig(t *testing.T, engine string, firmware ...[]string) (*gwNode, routeState, *config.Config) {
	t.Helper()
	client, _, _ := quicNsRig(t)
	quicNsHostFirewall(t, engine, quicNsLAN)
	for _, rule := range firmware {
		rule := rule
		netnsRun(t, append([]string{engine, "-w", "-t", "mangle", "-A"}, rule...)...)
		t.Cleanup(func() { _, _ = run(append([]string{engine, "-w", "-t", "mangle", "-D"}, rule...)...) })
	}

	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })

	cfg := quicNsConfig(engine)
	quicNsBlock(cfg.Sets[0], false)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	RoutingSyncConfig(cfg)
	t.Cleanup(RoutingClearAll)

	st, ok := routeRuleCache[quicNsSetID]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	return client, st, cfg
}

func netnsMangleInput(t *testing.T, ipt string) string {
	t.Helper()
	return netnsRun(t, ipt, "-w", "-t", "mangle", "-S", "INPUT")
}

func netnsFirmwareDropsLocal443Payload(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)

	client, st, _ := netnsFirmwareInputRig(t, engine, netnsFirmwareLocal443Drop)
	port, _ := portFromState(st)
	s := &netnsSockets{tcp: map[string][]string{}, udp: map[string][]string{}}
	netnsServeTCP(t, netnsMarkedTransparentListener(t, port, 0), "tproxy", "tproxy:", s)

	header := strings.Repeat("o", 170)
	want := "reply=tproxy:" + header
	t.Logf("mangle INPUT with the PR:\n%s", netnsMangleInput(t, engine))

	if got := quicNsTCP(client, quicNsMatched, 443, header); got != want {
		t.Errorf("with the mangle accept, a client's 443 payload through the proxy set: %.40s", got)
	}
	if got := quicNsTCP(nil, quicNsMatched, 443, header); got != want {
		t.Errorf("with the mangle accept, the router's own 443 payload through the proxy set: %.40s", got)
	}

	markHex := fmt.Sprintf("0x%x/0x%x", st.mark, st.mark)
	netnsRun(t, engine, "-w", "-t", "mangle", "-D", "INPUT", "-m", "mark", "--mark", markHex, "-j", "ACCEPT")
	t.Logf("mangle INPUT as before the PR:\n%s", netnsMangleInput(t, engine))

	if got := quicNsTCP(client, quicNsMatched, 443, header); got == want {
		t.Errorf("without the mangle accept the client's 443 payload still got through, so the stand-in proves nothing")
	} else {
		t.Logf("without the mangle accept, client 443: %s", got)
	}
	if got := quicNsTCP(client, quicNsMatched, 80, header); got != want {
		t.Errorf("without the mangle accept, client 80 through the proxy set: %.40s; the stand-in must only hit 443", got)
	}
	if got := quicNsTCP(nil, quicNsMatched, 443, header); got == want {
		t.Errorf("without the mangle accept the router's own 443 payload still got through")
	} else {
		t.Logf("without the mangle accept, router-own 443: %s", got)
	}
}

func TestNetnsAFirmwareDropInMangleInputSparesProxiedConnections(t *testing.T) {
	netnsFirmwareDropsLocal443Payload(t, backendIPTables)
}

func TestNetnsAFirmwareDropInMangleInputSparesProxiedConnectionsLegacy(t *testing.T) {
	netnsFirmwareDropsLocal443Payload(t, backendIPTablesLegacy)
}

func netnsInputAccepts(t *testing.T, mark uint32) map[string]int {
	t.Helper()
	want := fmt.Sprintf("--mark 0x%x/0x%x -j ACCEPT", mark, mark)
	got := map[string]int{}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		for _, table := range []string{"filter", "mangle"} {
			out, err := run(bin, "-w", "-t", table, "-S", "INPUT")
			if err != nil {
				t.Fatalf("%s -t %s -S INPUT: %v (%s)", bin, table, err, strings.TrimSpace(out))
			}
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "-A INPUT") && strings.HasSuffix(strings.TrimSpace(line), want) {
					got[bin+" "+table]++
				}
			}
		}
	}
	return got
}

func netnsWantInputAccepts(t *testing.T, step string, mark uint32, n int) {
	t.Helper()
	got := netnsInputAccepts(t, mark)
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		for _, table := range []string{"filter", "mangle"} {
			if got[bin+" "+table] != n {
				t.Errorf("%s: %s %s INPUT holds %d accepts for mark 0x%x, want %d", step, bin, table, got[bin+" "+table], mark, n)
			}
		}
	}
}

func TestNetnsProxyInputAcceptLeavesEveryTableWithItsSet(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	engine := backendIPTables
	netnsRequireEngine(t, engine)
	if _, err := run(backendIP6Tables, "-w", "-t", "mangle", "-S", "INPUT"); err != nil {
		t.Skip("ip6tables cannot read mangle here")
	}

	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	cfg := quicNsConfig(engine)
	set := cfg.Sets[0]
	quicNsBlock(set, false)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	t.Cleanup(RoutingClearAll)

	RoutingSyncConfig(cfg)
	first := routeRuleCache[quicNsSetID].mark
	netnsWantInputAccepts(t, "enabled", first, 1)

	RoutingSyncConfig(cfg)
	netnsWantInputAccepts(t, "unchanged resync", first, 1)

	set.Routing.FWMark = 0x21234
	RoutingSyncConfig(cfg)
	second := routeRuleCache[quicNsSetID].mark
	if second == first {
		t.Fatalf("the pinned fwmark did not change the set's mark (0x%x)", first)
	}
	netnsWantInputAccepts(t, "mark changed, old mark", first, 0)
	netnsWantInputAccepts(t, "mark changed, new mark", second, 1)

	cfg.Queue.IPv6Enabled = true
	RoutingSyncConfig(cfg)
	netnsWantInputAccepts(t, "families changed", second, 1)

	set.Enabled = false
	RoutingSyncConfig(cfg)
	netnsWantInputAccepts(t, "disabled", second, 0)

	set.Enabled = true
	RoutingSyncConfig(cfg)
	netnsWantInputAccepts(t, "enabled again", second, 1)

	cfg.Sets = nil
	RoutingSyncConfig(cfg)
	netnsWantInputAccepts(t, "deleted", second, 0)

	cfg.Sets = []*config.SetConfig{set}
	RoutingSyncConfig(cfg)
	netnsWantInputAccepts(t, "added again", second, 1)
	RoutingClearAll()
	netnsWantInputAccepts(t, "shutdown", second, 0)

	RoutingSyncConfig(cfg)
	netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-I", "INPUT", "1", "-m", "mark", "--mark", "0x2000a/0x2000a", "-j", "ACCEPT")
	routeEngine = nil
	routeRuleCache = make(map[string]routeState)
	RoutingClearAll()
	netnsWantInputAccepts(t, "startup sweep", second, 0)
	if got := netnsInputAccepts(t, 0x2000a)[backendIPTables+" mangle"]; got != 0 {
		t.Errorf("startup sweep left %d mangle accepts for a crashed run's mark 0x2000a", got)
	}
}

func netnsInputCopies(copies []netnsDNSCopy) int {
	n := 0
	for _, c := range copies {
		if c.hook == netnsHookInput && c.dport == 53 {
			n++
		}
	}
	return n
}

func netnsMangleInputOrder(t *testing.T, ipt string, mark uint32) (acceptAt, copyAt int) {
	t.Helper()
	markHex := fmt.Sprintf("0x%x/0x%x", mark, mark)
	n := 0
	for _, line := range strings.Split(netnsMangleInput(t, ipt), "\n") {
		if !strings.HasPrefix(line, "-A INPUT ") {
			continue
		}
		n++
		switch {
		case copyAt == 0 && strings.Contains(line, "-j "+iptDNSCopyChainName):
			copyAt = n
		case acceptAt == 0 && strings.Contains(line, markHex) && strings.Contains(line, "-j ACCEPT"):
			acceptAt = n
		}
	}
	return acceptAt, copyAt
}

func TestNetnsAProxiedDNSQueryIsCopiedWhicheverInstallRanLast(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	engine := backendIPTables
	netnsRequireEngine(t, engine)
	copies := netnsCopyDNSQueries(t)

	client, st, cfg := netnsFirmwareInputRig(t, engine, netnsFirmwareLocal443Drop)
	netnsRequireDNSCopy(t, engine)
	port, _ := portFromState(st)
	s := &netnsSockets{tcp: map[string][]string{}, udp: map[string][]string{}}
	netnsServeUDP(t, netnsTransparentUDP(t, port), "tproxy", s, true)
	netnsServeTCP(t, netnsMarkedTransparentListener(t, port, 0), "tproxy", "tproxy:", s)
	header := strings.Repeat("o", 170)
	want := "reply=tproxy:" + header

	accept, dnscopy := netnsMangleInputOrder(t, engine, st.mark)
	t.Logf("after start (capture, then routing): accept at %d, B4_DNSCOPY jump at %d:\n%s", accept, dnscopy, netnsMangleInput(t, engine))
	copies.collect(t, 200*time.Millisecond)
	quicNsUDP(client, quicNsMatched, 53, string(netnsDNSQueryPayload))
	if !s.got("udp", "tproxy", string(netnsDNSQueryPayload)+"@"+quicNsMatched+":53") {
		t.Fatalf("the client's DNS query never reached the transparent socket: %v", s.udp)
	}
	afterStart := netnsInputCopies(copies.collect(t, 500*time.Millisecond))
	t.Logf("after start: %d INPUT copies of the proxied DNS query", afterStart)
	if got := quicNsTCP(client, quicNsMatched, 443, header); got != want {
		t.Errorf("after start, a client's 443 payload through the proxy set: %.40s; the exemption must stay above the firmware's drop", got)
	}

	if err := RefreshRules(cfg); err != nil {
		t.Fatalf("RefreshRules: %v", err)
	}
	accept, dnscopy = netnsMangleInputOrder(t, engine, st.mark)
	t.Logf("after a capture refresh: accept at %d, B4_DNSCOPY jump at %d:\n%s", accept, dnscopy, netnsMangleInput(t, engine))
	quicNsUDP(client, quicNsMatched, 53, string(netnsDNSQueryPayload))
	afterRefresh := netnsInputCopies(copies.collect(t, 500*time.Millisecond))
	t.Logf("after a capture refresh: %d INPUT copies of the proxied DNS query", afterRefresh)
	if got := quicNsTCP(client, quicNsMatched, 443, header); got != want {
		t.Errorf("after a capture refresh, a client's 443 payload through the proxy set: %.40s; the exemption must stay above the firmware's drop", got)
	}

	if afterStart == 0 || afterRefresh == 0 {
		t.Errorf("a DNS query a proxy set diverts must be copied whichever install ran last, as before the mangle accept existed: %d copies after start (routing installed last), %d after a capture refresh", afterStart, afterRefresh)
	}
}
