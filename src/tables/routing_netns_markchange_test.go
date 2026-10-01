package tables

import (
	"fmt"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestNetnsDiscoveryProbesStayOutOfTheMainQueue(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequireEngine(t, engine)
			for _, marks := range []struct{ flow, injected uint32 }{
				{0x8001, 0x8002},
				{0x1001, 0x1002},
			} {
				netnsDiscoveryProbesStayOutOfTheMainQueue(t, engine, marks.flow, marks.injected)
			}
		})
	}
}

func netnsDiscoveryProbesStayOutOfTheMainQueue(t *testing.T, engine string, flow, injected uint32) {
	t.Helper()
	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(engine)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	seen, stop := netnsMarkedQueueListener(t, uint16(cfg.Queue.StartNum))
	defer stop()

	defer ClearDiscoverySteeringRules(cfg, uint(flow), uint(injected))
	if err := ApplyDiscoverySteeringRules(cfg, uint(flow), uint(injected), cfg.Queue.StartNum+cfg.Queue.Threads, 1); err != nil {
		t.Fatalf("ApplyDiscoverySteeringRules: %v", err)
	}
	netnsSendMarked(t, flow, 42001, 443)
	netnsSendMarked(t, injected, 42002, 443)
	netnsSendMarked(t, 0, 42003, 443)
	ClearDiscoverySteeringRules(cfg, uint(flow), uint(injected))

	got := seen()
	if got[flow] != 0 || got[injected] != 0 {
		t.Errorf("marks 0x%x/0x%x: the main queue took %d probe and %d injected packet(s) of a Discovery run; the live sets then process Discovery's traffic on top of the strategy under test",
			flow, injected, got[flow], got[injected])
	}
	if got[0] == 0 {
		t.Fatalf("marks 0x%x/0x%x: an unmarked packet to port 443 never reached the main queue, so the test proves nothing: %v", flow, injected, got)
	}

	chain := netnsQueueChain(t, engine)
	for _, mark := range []uint32{flow, injected} {
		if netnsQueueChainHasMark(engine, chain, mark) {
			t.Errorf("the end of the run left its return for 0x%x in the queue chain:\n%s", mark, chain)
		}
	}
}

func TestNetnsDiscoveryKeepsItsRulesThroughAFirewallRefresh(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequireEngine(t, engine)
			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := netnsConfig(engine)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()

			seen, stop := netnsMarkedQueueListener(t, uint16(cfg.Queue.StartNum))
			defer stop()

			flow, injected := uint32(0x1001), uint32(0x1002)
			defer ClearDiscoverySteeringRules(cfg, uint(flow), uint(injected))
			if err := ApplyDiscoverySteeringRules(cfg, uint(flow), uint(injected), cfg.Queue.StartNum+cfg.Queue.Threads, 1); err != nil {
				t.Fatalf("ApplyDiscoverySteeringRules: %v", err)
			}

			again := netnsConfig(engine)
			if err := RefreshRules(again); err != nil {
				t.Fatalf("RefreshRules: %v", err)
			}

			netnsSendMarked(t, flow, 42011, 443)
			netnsSendMarked(t, injected, 42012, 443)
			netnsSendMarked(t, 0, 42013, 443)

			got := seen()
			if got[flow] != 0 || got[injected] != 0 {
				t.Errorf("after a firewall refresh during a run the main queue took %d probe and %d injected packet(s) of Discovery", got[flow], got[injected])
			}
			if got[0] == 0 {
				t.Fatalf("an unmarked packet to port 443 never reached the main queue, so the test proves nothing: %v", got)
			}
			chain := netnsQueueChain(t, engine)
			if !netnsQueueChainHasMark(engine, chain, flow) || !netnsQueueChainHasMark(engine, chain, injected) {
				t.Errorf("the refresh rebuilt the queue chain without Discovery's returns:\n%s", chain)
			}
		})
	}
}

func netnsQueueChain(t *testing.T, engine string) string {
	t.Helper()
	if engine == backendNFTables {
		return netnsRun(t, "nft", "list", "chain", "inet", nftTableName, nftChainName)
	}
	return netnsRun(t, "iptables", "-w", "-t", "mangle", "-S", iptChainName)
}

func netnsQueueChainHasMark(engine, chain string, mark uint32) bool {
	for _, line := range strings.Split(chain, "\n") {
		line = strings.TrimSpace(line)
		if engine == backendNFTables {
			if strings.Contains(line, "return") && nftLineHasMark(line, uint(mark)) {
				return true
			}
			continue
		}
		hex := fmt.Sprintf("0x%x", mark)
		if strings.Contains(line, "--mark "+hex+" ") || strings.Contains(line, "--mark "+hex+"/0xffffffff ") {
			return true
		}
	}
	return false
}

func netnsRulesetText(t *testing.T, engine string) string {
	t.Helper()
	if engine == backendNFTables {
		return netnsRun(t, "nft", "list", "ruleset")
	}
	var b strings.Builder
	for _, table := range []string{"mangle", "nat", "filter"} {
		out, _ := run("iptables", "-w", "-t", table, "-S")
		b.WriteString(out)
		b.WriteString("\n")
	}
	return b.String()
}

func TestNetnsAChangedQueueMarkLeavesNoRuleOfTheOldOne(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequireEngine(t, engine)
			netnsAChangedQueueMarkLeavesNoRuleOfTheOldOne(t, engine)
		})
	}
}

func netnsAChangedQueueMarkLeavesNoRuleOfTheOldOne(t *testing.T, engine string) {
	t.Helper()
	routeEngine = nil
	defer func() { routeEngine = nil }()

	before := netnsConfig(engine)
	after := netnsConfig(engine)
	after.Queue.Mark = 0x4000000
	oldToken, newToken := "0x8000/", "0x4000000/"
	if engine == backendNFTables {
		oldToken, newToken = "0x00008000", "0x04000000"
	}

	if err := AddRules(before); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(before); _ = ClearRules(after) }()
	RoutingSyncConfig(before)
	defer RoutingClearAll()

	if len(netnsRulesetLinesWith(t, engine, oldToken)) == 0 {
		t.Fatalf("the first install carries no rule with %s, so the test proves nothing", oldToken)
	}

	if err := RefreshRules(after); err != nil {
		t.Fatalf("RefreshRules: %v", err)
	}
	RoutingSyncConfig(after)
	for _, line := range netnsRulesetLinesWith(t, engine, oldToken) {
		t.Errorf("a rule of the old queue mark survived the save of the new one: %s", line)
	}
	if len(netnsRulesetLinesWith(t, engine, newToken)) == 0 {
		t.Errorf("the refresh installed no rule with the new queue mark")
	}

	if err := ClearRules(after); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	RoutingClearAll()
	for _, line := range netnsRulesetLinesWith(t, engine, oldToken, newToken) {
		t.Errorf("a queue-mark rule survived b4's cleanup: %s", line)
	}
}

func netnsRulesetLinesWith(t *testing.T, engine string, tokens ...string) []string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(netnsRulesetText(t, engine), "\n") {
		for _, token := range tokens {
			if strings.Contains(line, token) {
				lines = append(lines, strings.TrimSpace(line))
				break
			}
		}
	}
	return lines
}

func TestNetnsB4LeavesAnotherServicesMasqueradeRuleAlone(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, backendIPTables)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	foreign := []string{"-o", netnsPrimary, "-j", "MASQUERADE"}
	netnsRun(t, append([]string{"iptables", "-w", "-t", "nat", "-A", "POSTROUTING"}, foreign...)...)
	defer func() {
		_, _ = run(append([]string{"iptables", "-w", "-t", "nat", "-D", "POSTROUTING"}, foreign...)...)
	}()
	present := func() bool {
		_, err := run(append([]string{"iptables", "-w", "-t", "nat", "-C", "POSTROUTING"}, foreign...)...)
		return err == nil
	}

	cfg := netnsConfig(backendIPTables)
	cfg.System.Tables.Masquerade = config.MasqueradeConfig{Enabled: true, Interfaces: []string{netnsPrimary}}
	moved := netnsConfig(backendIPTables)
	moved.System.Tables.Masquerade = config.MasqueradeConfig{Enabled: true, Interfaces: []string{netnsSecondary}}

	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(moved) }()
	if !present() {
		t.Fatalf("installing b4's rules removed the router's own masquerade rule on %s", netnsPrimary)
	}

	if err := RefreshRules(moved); err != nil {
		t.Fatalf("RefreshRules: %v", err)
	}
	if !present() {
		t.Errorf("moving b4's masquerade to %s deleted the router's own `-o %s -j MASQUERADE`, which b4 never added", netnsSecondary, netnsPrimary)
	}

	if err := RefreshRules(cfg); err != nil {
		t.Fatalf("RefreshRules: %v", err)
	}
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if !present() {
		t.Errorf("stopping b4 deleted the router's own `-o %s -j MASQUERADE`: b4 keeps its masquerade in %s", netnsPrimary, masqChainName)
	}
}

func TestNetnsARefreshMovesTheStampWithTheSettings(t *testing.T) {
	netnsRequireNft(t)
	netnsSetupLinks(t)
	dscpWireCounters(t)
	dscpNftIn(t, 0, fmt.Sprintf("add counter inet %s udp31\ninsert rule inet %s in iifname \"%sp\" udp dport 443 ip dscp 31 counter name \"udp31\"\n",
		dscpTestCounters, dscpTestCounters, netnsPrimary))

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := dscpNetnsConfig(engine, true)
			stop := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
			defer stop()
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearAppliedRules(cfg) }()

			moved := dscpNetnsConfig(engine, true)
			moved.Queue.Mark = 0x4000000
			moved.System.Tables.DSCP.Value = 31
			moved.System.Tables.DSCP.Interfaces = []string{netnsPrimary}
			if err := RefreshRules(moved); err != nil {
				t.Fatalf("RefreshRules with a new Packet Mark and DSCP value: %v", err)
			}
			if engine == backendIPTables {
				dscpAssertSeated(t, "after a refresh that changed the DSCP value and the Packet Mark")
			}
			before, before31 := dscpRead(t, 0, "udp"), dscpCounter(t, 0, "udp31")
			dscpSendLocalUDP(t)
			after := dscpRead(t, 0, "udp")
			dscpExpectPlain(t, "local UDP after a refresh moved the value from 7 to 31", before, after)
			if got, sent := dscpCounter(t, 0, "udp31")-before31, after.all-before.all; got != sent {
				t.Errorf("after a refresh moved the value to 31, %d of %d packets carried it", got, sent)
			}

			off := dscpNetnsConfig(engine, false)
			off.Queue.Mark = 0x4000000
			if err := RefreshRules(off); err != nil {
				t.Fatalf("RefreshRules with Set DSCP off: %v", err)
			}
			dscpAssertGone(t, "after a refresh that turned Set DSCP off")
			if dscpApplied.Load() != nil {
				t.Errorf("a refresh that turned Set DSCP off left the stamp recorded as applied, so the monitor would put it back")
			}
			before, before31 = dscpRead(t, 0, "udp"), dscpCounter(t, 0, "udp31")
			dscpSendLocalUDP(t)
			after = dscpRead(t, 0, "udp")
			dscpExpectPlain(t, "local UDP after a refresh turned Set DSCP off", before, after)
			if got := dscpCounter(t, 0, "udp31") - before31; got != 0 {
				t.Errorf("after a refresh turned Set DSCP off, %d packets still carried DSCP 31", got)
			}
		})
	}
}
