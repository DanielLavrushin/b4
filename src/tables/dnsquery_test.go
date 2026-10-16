package tables

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func stubNoExec(t *testing.T) {
	t.Helper()
	orig := run
	t.Cleanup(func() { run = orig })
	run = func(args ...string) (string, error) {
		return "", errors.New("no command runs in this test")
	}
}

func manifestSpecs(m Manifest, ipt, table, chain string) []string {
	var out []string
	for _, r := range m.Rules {
		if r.IPT == ipt && r.Table == table && r.Chain == chain {
			out = append(out, strings.Join(r.Spec, " "))
		}
	}
	return out
}

func hasSpecWith(specs []string, parts ...string) bool {
	for _, s := range specs {
		all := true
		for _, p := range parts {
			if !strings.Contains(s, p) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func manifestHasChain(m Manifest, ipt, table, name string) bool {
	for _, c := range m.Chains {
		if c.IPT == ipt && c.Table == table && c.Name == name {
			return true
		}
	}
	return false
}

func forgetRawFallbackWarnings(t *testing.T) {
	t.Helper()
	forget := func() {
		for _, m := range []*sync.Map{&rawFallbackWarned, &copyFallbackWarned, &dnsCopyNoted, &dnsNatPortWarned, &rawQueueUnusable, &dnsCopyUnusable, &dnsCopyErrors} {
			m.Clear()
		}
	}
	forget()
	t.Cleanup(forget)
}

func dnsQueryTestManager(t *testing.T, rawErr error) *IPTablesManager {
	t.Helper()
	stubNoExec(t)
	forgetRawFallbackWarnings(t)
	stubDNSKernel(t, unaffectedDNSKernel)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true
	cfg.Queue.Mark = 0x8000
	manager := NewIPTablesManager(&cfg, false)
	stubProbes(manager, backendIPTables, backendIP6Tables)
	manager.rawQueueSupport[backendIPTables] = rawErr
	manager.rawQueueSupport[backendIP6Tables] = rawErr
	return manager
}

func TestDNSQueriesAreQueuedFromTheRawTableBeforeConntrack(t *testing.T) {
	manager := dnsQueryTestManager(t, nil)
	m, err := manager.buildManifest()
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		if m.DNSQueries[ipt] != dnsHeldRaw {
			t.Errorf("%s: the manifest does not record that DNS queries go through the raw table", ipt)
		}
		if hasSpecWith(manifestSpecs(m, ipt, "mangle", "B4_PREROUTING"), "-p udp --dport 53 -j NFQUEUE") {
			t.Errorf("%s: a DNS query queued in mangle PREROUTING waits in the queue with a conntrack entry the kernel has not confirmed, and a second query from the same socket is dropped when it clashes with it", ipt)
		}
		if hasSpecWith(manifestSpecs(m, ipt, "mangle", "OUTPUT"), "-p udp --dport 53 -j NFQUEUE") {
			t.Errorf("%s: the router's own DNS queries are still queued in mangle OUTPUT, after conntrack", ipt)
		}
		if !hasSpecWith(manifestSpecs(m, ipt, "mangle", "B4_PREROUTING"), "--sport 53", "NFQUEUE") {
			t.Errorf("%s: DNS answers must stay queued in mangle PREROUTING", ipt)
		}
		if !hasSpecWith(manifestSpecs(m, ipt, "mangle", "OUTPUT"), "--sport 53", "NFQUEUE") {
			t.Errorf("%s: DNS answers must stay queued in mangle OUTPUT", ipt)
		}
		if !manifestHasChain(m, ipt, "raw", iptRawChainName) {
			t.Fatalf("%s: the raw table chain %s is missing", ipt, iptRawChainName)
		}
		chain := manifestSpecs(m, ipt, "raw", iptRawChainName)
		if len(chain) != 2 || !strings.Contains(chain[0], "--mark 0x8000/0x8efd8000 -j RETURN") || !hasSpecWith(chain[1:], "-p udp --dport 53", "-j NFQUEUE", "--queue-bypass") {
			t.Errorf("%s: %s must let b4's own packets through and then queue DNS queries, got %q", ipt, iptRawChainName, chain)
		}
		jump := strings.Join(iptRawDNSJump(), " ")
		for _, hook := range []string{"PREROUTING", "OUTPUT"} {
			specs := manifestSpecs(m, ipt, "raw", hook)
			if len(specs) != 1 || specs[0] != jump {
				t.Errorf("%s: raw %s must jump to %s for DNS queries, got %q", ipt, hook, iptRawChainName, specs)
			}
		}
	}
	for _, r := range m.Rules {
		if r.Table == "raw" && (r.Chain == "PREROUTING" || r.Chain == "OUTPUT") && r.Action != "A" {
			t.Errorf("the raw %s jump must be appended: a query b4 releases skips the rest of the raw table, so a blocklist drop or a conntrack zone another service keeps there has to run first; got action %q", r.Chain, r.Action)
		}
		if r.Table == "raw" && r.Chain == iptRawChainName {
			spec := strings.Join(r.Spec, " ")
			want := "A"
			if strings.HasSuffix(spec, "-j RETURN") {
				want = "I"
			}
			if r.Action != want {
				t.Errorf("%s: in %s the return for b4's own packets must be inserted and the queue rule appended, so a restore of either one alone keeps the return first; got action %q for %s", r.IPT, iptRawChainName, r.Action, spec)
			}
		}
	}
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		for _, chain := range []string{"B4_PREROUTING", "OUTPUT"} {
			if !hasSpecWith(manifestSpecs(m, ipt, "mangle", chain), "--sport 53 ! --dport 53", "NFQUEUE") {
				t.Errorf("%s %s: a packet from port 53 to port 53 is already queued from the raw table, so the answer rule must leave it alone or b4 handles it twice", ipt, chain)
			}
		}
	}
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		capture := manifestSpecs(m, ipt, "mangle", iptChainName)
		ret, queue := -1, -1
		for i, s := range capture {
			if s == strings.Join(iptDNSQueryCaptureReturn(), " ") {
				ret = i
			}
			if queue < 0 && strings.HasPrefix(s, "-p udp") && strings.Contains(s, "NFQUEUE") {
				queue = i
			}
		}
		if ret < 0 || queue < 0 || ret > queue {
			t.Errorf("%s: with a set listing UDP port 53 the capture chain would queue a query a second time, after conntrack; %s must return UDP to port 53 ahead of its UDP queue rule, got %q", ipt, iptChainName, capture)
		}
	}
}

func TestCaptureSkipsDNSQueriesExactlyWhileTheRawTableQueuesThem(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rawErr error
		ifaces []string
		skip   bool
	}{
		{"raw table unavailable", errors.New("no raw table"), nil, false},
		{"interface filter set", nil, []string{"eth0"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := dnsQueryTestManager(t, tc.rawErr)
			manager.cfg.Queue.Interfaces = tc.ifaces
			m, err := manager.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			skips := false
			for _, s := range manifestSpecs(m, backendIPTables, "mangle", iptChainName) {
				if s == strings.Join(iptDNSQueryCaptureReturn(), " ") {
					skips = true
				}
			}
			switch {
			case skips && !tc.skip:
				t.Errorf("the capture chain skips UDP to port 53 although DNS queries stay in mangle, where the capture rules keep their old behavior")
			case !skips && tc.skip:
				t.Errorf("the capture chain queues UDP to port 53 although the raw table already queued every DNS query; with a set on UDP port 53 the query waits a second time, after conntrack, whatever Capture Interfaces holds")
			}
		})
	}
}

func TestDNSQueriesStayInMangleWhenTheRawTableCannotQueue(t *testing.T) {
	manager := dnsQueryTestManager(t, errors.New("can't initialize iptables table `raw'"))
	m, err := manager.buildManifest()
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		if m.DNSQueries[ipt] == dnsHeldRaw {
			t.Errorf("%s: the manifest claims the raw table placement without raw table support", ipt)
		}
		if !hasSpecWith(manifestSpecs(m, ipt, "mangle", "B4_PREROUTING"), "-p udp --dport 53 -j NFQUEUE") {
			t.Errorf("%s: without the raw table DNS queries must still be queued in mangle PREROUTING", ipt)
		}
		if !hasSpecWith(manifestSpecs(m, ipt, "mangle", "OUTPUT"), "-p udp --dport 53 -j NFQUEUE") {
			t.Errorf("%s: without the raw table the router's own DNS queries must still be queued in mangle OUTPUT", ipt)
		}
		if manifestHasChain(m, ipt, "raw", iptRawChainName) || len(manifestSpecs(m, ipt, "raw", "PREROUTING")) > 0 {
			t.Errorf("%s: raw table rules were emitted although the raw table cannot queue", ipt)
		}
	}
}

func TestARawPlacementThatWorkedSurvivesAFailedProbe(t *testing.T) {
	stubNoExec(t)
	forgetRawFallbackWarnings(t)
	t.Cleanup(func() { rawQueueProven.Delete(backendIPTables) })
	origSleep := probeSleep
	probeSleep = func(time.Duration) {}
	t.Cleanup(func() { probeSleep = origSleep })

	cfg := config.NewConfig()
	rawQueueProven.Delete(backendIPTables)
	if err := NewIPTablesManager(&cfg, false).checkRawQueueSupport(backendIPTables); err == nil {
		t.Fatalf("a probe that fails on a box where raw never worked must report the raw table as unusable")
	}

	rawQueueProven.Store(backendIPTables, true)
	if err := NewIPTablesManager(&cfg, false).checkRawQueueSupport(backendIPTables); err != nil {
		t.Errorf("a probe that fails once raw has worked in this process, as when another program rewrites the raw table mid-probe, must not move DNS queries back after conntrack: %v", err)
	}
}

func TestAMovedDNSQueryPlacementIsDetected(t *testing.T) {
	t.Cleanup(func() { noteDNSQueryPlacement(nil) })
	dnsQueryPlacementChanged := func(placements map[string]dnsQueryPlacement) bool {
		return len(dnsQueryPlacementMoves(placements)) > 0
	}

	for _, first := range []dnsQueryPlacement{dnsHeldMangle, dnsHeldRaw, dnsCopied} {
		noteDNSQueryPlacement(nil)
		if dnsQueryPlacementChanged(map[string]dnsQueryPlacement{backendIPTables: first}) {
			t.Errorf("a first apply that places DNS queries in %s has nothing to move", first)
		}
	}
	for _, tc := range []struct {
		was, now dnsQueryPlacement
		why      string
	}{
		{dnsHeldMangle, dnsHeldRaw, "the mangle answer rules keep a stale shape and sit above the queue-mark accept"},
		{dnsHeldRaw, dnsHeldMangle, "the answer rules keep skipping packets to port 53, so a query to port 53 from port 53 is never handled"},
		{dnsHeldRaw, dnsCopied, "the raw table keeps holding the queries that NFLOG now copies, so the clash stays"},
		{dnsCopied, dnsHeldMangle, "the answer rules keep skipping packets to port 53 while the queries are held in mangle again"},
		{dnsHeldMangle, dnsCopied, "the mangle query rules keep holding the queries that NFLOG now copies, so the clash stays"},
	} {
		noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: tc.was})
		if !dnsQueryPlacementChanged(map[string]dnsQueryPlacement{backendIPTables: tc.now}) {
			t.Errorf("DNS queries moving from %s to %s on a restore must rebuild the rules, or %s", tc.was, tc.now, tc.why)
		}
		if dnsQueryPlacementChanged(map[string]dnsQueryPlacement{backendIPTables: tc.was}) {
			t.Errorf("an unchanged %s placement must not rebuild the rules", tc.was)
		}
	}
	noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle})
	if dnsQueryPlacementChanged(map[string]dnsQueryPlacement{backendIP6Tables: dnsHeldRaw}) {
		t.Errorf("a family without a recorded placement has nothing to move")
	}
}

func TestTeardownRemovesBothDNSQueryPlacements(t *testing.T) {
	manager := dnsQueryTestManager(t, errors.New("never consulted on teardown"))
	delete(manager.rawQueueSupport, backendIPTables)
	delete(manager.rawQueueSupport, backendIP6Tables)
	m, err := manager.buildTeardownManifest()
	if err != nil {
		t.Fatalf("buildTeardownManifest: %v", err)
	}
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		if !manifestHasChain(m, ipt, "raw", iptRawChainName) {
			t.Errorf("%s: teardown leaves the raw table chain behind", ipt)
		}
		for _, hook := range []string{"PREROUTING", "OUTPUT"} {
			if !hasSpecWith(manifestSpecs(m, ipt, "raw", hook), "-j "+iptRawChainName) {
				t.Errorf("%s: teardown leaves the raw %s jump behind", ipt, hook)
			}
		}
		if !hasSpecWith(manifestSpecs(m, ipt, "mangle", "B4_PREROUTING"), "-p udp --dport 53 -j NFQUEUE") ||
			!hasSpecWith(manifestSpecs(m, ipt, "mangle", "OUTPUT"), "-p udp --dport 53 -j NFQUEUE") {
			t.Errorf("%s: teardown must still remove DNS query rules a mangle placement left", ipt)
		}
	}
	if _, probed := manager.rawQueueSupport[backendIPTables]; probed {
		t.Errorf("teardown probed the raw table, which a stop must never depend on")
	}
	if len(m.DNSQueries) != 0 {
		t.Errorf("a teardown manifest must not record a placement, got %v", m.DNSQueries)
	}
	assertNoDNSCopyTeardown(t, m, "an unaffected kernel never copies DNS queries, so its teardown must not grow")

	t.Run("an affected kernel also removes the copy chain and its jumps", func(t *testing.T) {
		stubDNSKernel(t, affectedDNSKernel)
		stubDNSCopyListener(t, func() (uint16, error) {
			t.Errorf("teardown opened the DNS query copy listener")
			return 537, nil
		})
		m, err := manager.buildTeardownManifest()
		if err != nil {
			t.Fatalf("buildTeardownManifest: %v", err)
		}
		assertDNSCopyTeardown(t, m)
		for _, ipt := range []string{backendIPTables, backendIP6Tables} {
			if !manifestHasChain(m, ipt, "raw", iptRawChainName) {
				t.Errorf("%s: a vanilla kernel keeps removing the raw table chain an older b4 left", ipt)
			}
		}
		if len(manager.rawQueueSupport) != 0 || len(manager.dnsCopySupport) != 0 {
			t.Errorf("teardown probed a placement: raw %v, copy %v", manager.rawQueueSupport, manager.dnsCopySupport)
		}
	})

	t.Run("KeeneticOS leaves the raw table alone unless its module is loaded", func(t *testing.T) {
		stubDNSKernel(t, keeneticDNSKernel)
		stubDNSCopyListener(t, func() (uint16, error) {
			t.Errorf("teardown opened the DNS query copy listener")
			return 537, nil
		})
		origSys := kmodSysRoot
		kmodSysRoot = t.TempDir()
		t.Cleanup(func() { kmodSysRoot = origSys })
		if err := os.Mkdir(filepath.Join(kmodSysRoot, "iptable_raw"), 0o755); err != nil {
			t.Fatal(err)
		}

		m, err := manager.buildTeardownManifest()
		if err != nil {
			t.Fatalf("buildTeardownManifest: %v", err)
		}
		if !manifestHasChain(m, backendIPTables, "raw", iptRawChainName) || !hasSpecWith(manifestSpecs(m, backendIPTables, "raw", "PREROUTING"), "-j "+iptRawChainName) {
			t.Errorf("iptable_raw is loaded, so teardown must still remove a %s that an older b4 left in it", iptRawChainName)
		}
		for _, r := range m.Rules {
			if r.IPT == backendIP6Tables && r.Table == "raw" {
				t.Errorf("without ip6table_raw every raw command makes the KeeneticOS kernel try to load it, got %v", r)
			}
		}
		if manifestHasChain(m, backendIP6Tables, "raw", iptRawChainName) {
			t.Errorf("without ip6table_raw teardown must not touch the raw table chain")
		}
		for _, ipt := range []string{backendIPTables, backendIP6Tables} {
			if !hasSpecWith(manifestSpecs(m, ipt, "mangle", "B4_PREROUTING"), "-p udp --dport 53 -j NFQUEUE") {
				t.Errorf("%s: teardown must still remove the DNS query rules of a mangle placement", ipt)
			}
		}
		assertDNSCopyTeardown(t, m)
		if len(manager.rawQueueSupport) != 0 || len(manager.dnsCopySupport) != 0 {
			t.Errorf("teardown probed a placement: raw %v, copy %v", manager.rawQueueSupport, manager.dnsCopySupport)
		}
	})
}

func assertDNSCopyTeardown(t *testing.T, m Manifest) {
	t.Helper()
	for _, ipt := range []string{backendIPTables, backendIP6Tables} {
		if !manifestHasChain(m, ipt, "mangle", iptDNSCopyChainName) {
			t.Errorf("%s: teardown leaves the %s chain behind", ipt, iptDNSCopyChainName)
		}
		for chain, spec := range dnsCopyTestJumps {
			if !slices.Contains(manifestSpecs(m, ipt, "mangle", chain), spec) {
				t.Errorf("%s: teardown leaves the mangle %s jump to %s behind", ipt, chain, iptDNSCopyChainName)
			}
		}
		if body := manifestSpecs(m, ipt, "mangle", iptDNSCopyChainName); len(body) != 0 {
			t.Errorf("%s: the chain body is flushed with the chain and depends on the group, which teardown must not need; got %q", ipt, body)
		}
	}
}

func assertNoDNSCopyTeardown(t *testing.T, m Manifest, why string) {
	t.Helper()
	for _, c := range m.Chains {
		if c.Name == iptDNSCopyChainName {
			t.Errorf("%s: got the chain %s %s", why, c.IPT, c.Name)
		}
	}
	for _, r := range m.Rules {
		if spec := strings.Join(r.Spec, " "); strings.Contains(spec, iptDNSCopyChainName) || strings.Contains(spec, "NFLOG") || r.Chain == "INPUT" {
			t.Errorf("%s: got %s %s %s", why, r.IPT, r.Chain, spec)
		}
	}
}

func TestKeeneticWithoutTheRawModuleRunsNoRawCommand(t *testing.T) {
	manager := dnsQueryTestManager(t, errors.New("KeeneticOS must never probe the raw table"))
	delete(manager.rawQueueSupport, backendIPTables)
	delete(manager.rawQueueSupport, backendIP6Tables)
	stubDNSKernel(t, keeneticDNSKernel)
	stubDNSCopyListener(t, nil)
	origSys := kmodSysRoot
	kmodSysRoot = t.TempDir()
	t.Cleanup(func() { kmodSysRoot = origSys })
	noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: dnsHeldRaw, backendIP6Tables: dnsHeldMangle})
	t.Cleanup(func() { noteDNSQueryPlacement(nil) })
	rec := recordFirewall(t, nil)

	if err := manager.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := dnsQueryPlacementOf(backendIPTables); got != dnsHeldMangle {
		t.Errorf("without a way to copy DNS queries KeeneticOS must hold them in mangle, got %s", got)
	}
	manager.dropUnusedDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: dnsHeldMangle, backendIP6Tables: dnsHeldMangle})
	if err := manager.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if raw := commandsMatching(rec.commands(), rawTableCommand); len(raw) > 0 {
		t.Errorf("without iptable_raw each raw command makes the KeeneticOS kernel try to load it, and b4 never uses the raw table there:\n%s", strings.Join(raw, "\n"))
	}
}

func TestDNSQueryQueueLinesAreMatchedByPortToken(t *testing.T) {
	for line, want := range map[string]bool{
		"1    NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53 NFQUEUE balance 537:540 bypass":      true,
		"2    NFQUEUE    udp  --  ::/0  ::/0  udp dpt:53 NFQUEUE num 537 bypass":                        true,
		"3    NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:5353 NFQUEUE num 537 bypass":            false,
		"4    NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp spt:53 NFQUEUE balance 537:540 bypass":      false,
		"5    ACCEPT     udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53":                                     false,
		"6    NFQUEUE    tcp  --  0.0.0.0/0  0.0.0.0/0  multiport sports 53,443 NFQUEUE num 537 bypass": false,
	} {
		if got := iptIsDNSQueryQueueLine(line); got != want {
			t.Errorf("iptIsDNSQueryQueueLine(%q) = %v, want %v", line, got, want)
		}
	}
	if !iptIsRawDNSJumpLine("1    B4_RAW     udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53") {
		t.Errorf("a raw jump to %s was not recognised", iptRawChainName)
	}
	if iptIsRawDNSJumpLine("1    NOTRACK    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53") {
		t.Errorf("a foreign raw rule was taken for b4's jump")
	}
}

type fakeListing struct {
	chains map[string]string
	calls  []string
}

func (f *fakeListing) run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	table, op, at := "", "", -1
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "-t":
			table = args[i+1]
		case "-S", "-C":
			op, at = args[i], i
		}
	}
	if at < 0 {
		return "", errors.New("exit status 1")
	}
	chain := args[at+1]
	listing, ok := f.chains[table+" "+chain]
	if !ok {
		return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
	}
	if op == "-S" {
		return listing, nil
	}
	if strings.Contains(listing, "-A "+chain+" "+strings.Join(args[at+2:], " ")+"\n") {
		return "", nil
	}
	return "", errors.New("exit status 1")
}

func monitorDNSTestListing(placement dnsQueryPlacement, iptables1421 bool) *fakeListing {
	pre := "-N B4_PREROUTING\n-A B4_PREROUTING -m connmark --mark 0x8000/0x8000 -j RETURN\n-A B4_PREROUTING -p udp -m udp --sport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
	out := "-P OUTPUT ACCEPT\n-A OUTPUT -m mark --mark 0x8000/0x8000 -j ACCEPT\n-A OUTPUT -p udp -m udp --sport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
	if placement == dnsHeldMangle {
		pre += "-A B4_PREROUTING -p udp -m udp --dport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
		out += "-A OUTPUT -p udp -m udp --dport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
	}
	if placement == dnsCopied {
		out = "-P OUTPUT ACCEPT\n-A OUTPUT " + dnsCopyTestJumps["OUTPUT"] + "\n" + strings.TrimPrefix(out, "-P OUTPUT ACCEPT\n")
	}
	pre += "-A B4_PREROUTING -p tcp -m multiport --sports 443 -m tcp --tcp-flags SYN,ACK SYN,ACK -j NFQUEUE --queue-num 537 --queue-bypass\n"
	out += "-A OUTPUT -j B4\n"
	f := &fakeListing{chains: map[string]string{
		"mangle B4":            "-N B4\n-A B4 -m mark --mark 0x8000/0x8efd8000 -j RETURN\n",
		"mangle POSTROUTING":   "-P POSTROUTING ACCEPT\n-A POSTROUTING -j B4\n",
		"mangle PREROUTING":    "-P PREROUTING ACCEPT\n-A PREROUTING -j B4_PREROUTING\n",
		"mangle B4_PREROUTING": pre,
		"mangle OUTPUT":        out,
	}}
	switch placement {
	case dnsHeldRaw:
		jump := strings.Join(iptRawDNSJump(), " ") + "\n"
		f.chains["raw PREROUTING"] = "-P PREROUTING ACCEPT\n-A PREROUTING " + jump
		f.chains["raw OUTPUT"] = "-P OUTPUT ACCEPT\n-A OUTPUT " + jump
		f.chains["raw "+iptRawChainName] = "-N B4_RAW\n-A B4_RAW -m mark --mark 0x8000/0x8efd8000 -j RETURN\n-A B4_RAW -p udp -m udp --dport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
	case dnsCopied:
		prefix := "--nflog-prefix b4dns"
		if iptables1421 {
			prefix = "--nflog-prefix  b4dns"
		}
		f.chains["mangle INPUT"] = "-P INPUT ACCEPT\n-A INPUT " + dnsCopyTestJumps["INPUT"] + "\n"
		f.chains["mangle FORWARD"] = "-P FORWARD ACCEPT\n-A FORWARD " + dnsCopyTestJumps["FORWARD"] + "\n"
		f.chains["mangle "+iptDNSCopyChainName] = "-N B4_DNSCOPY\n" +
			"-A B4_DNSCOPY -m mark --mark 0x8000/0x8efd8000 -j RETURN\n" +
			"-A B4_DNSCOPY -p udp -m udp --dport 53 -j NFLOG " + prefix + " --nflog-group 537 --nflog-threshold 1\n"
	}
	return f
}

func TestMonitorChecksWhereDNSQueriesWereQueued(t *testing.T) {
	origRun := run
	t.Cleanup(func() {
		run = origRun
		noteDNSQueryPlacement(nil)
		hasBinaryCache.Delete(backendIPTables)
	})
	hasBinaryCache.Store(backendIPTables, true)
	prevV4 := dnsTCPListenerReadyV4.Load()
	dnsTCPListenerReadyV4.Store(false)
	t.Cleanup(func() { dnsTCPListenerReadyV4.Store(prevV4) })
	stubDNSCopyListener(t, nil)
	alive := true
	stubDNSCopyAlive(t, func() bool { return alive })

	m, _ := newLockTestMonitor(t)
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.Mark = 0x8000

	removeLine := func(f *fakeListing, chain, line string) {
		f.chains[chain] = strings.Replace(f.chains[chain], line+"\n", "", 1)
	}
	editCopyRule := func(edit func(string) string) func(f *fakeListing) {
		return func(f *fakeListing) {
			f.chains["mangle "+iptDNSCopyChainName] = edit(f.chains["mangle "+iptDNSCopyChainName])
		}
	}
	for _, tc := range []struct {
		name      string
		placement dnsQueryPlacement
		mutate    func(f *fakeListing)
		group     uint32
		dead      bool
		want      bool
	}{
		{name: "raw placement complete", placement: dnsHeldRaw, want: true},
		{name: "mangle placement complete", placement: dnsHeldMangle, want: true},
		{name: "raw PREROUTING jump wiped", placement: dnsHeldRaw, mutate: func(f *fakeListing) { delete(f.chains, "raw PREROUTING") }},
		{name: "raw OUTPUT jump wiped", placement: dnsHeldRaw, mutate: func(f *fakeListing) { delete(f.chains, "raw OUTPUT") }},
		{name: "raw chain flushed", placement: dnsHeldRaw, mutate: func(f *fakeListing) { f.chains["raw "+iptRawChainName] = "-N B4_RAW\n" }},
		{name: "raw table gone", placement: dnsHeldRaw, mutate: func(f *fakeListing) {
			delete(f.chains, "raw PREROUTING")
			delete(f.chains, "raw OUTPUT")
			delete(f.chains, "raw "+iptRawChainName)
		}},
		{name: "mangle query rule wiped", placement: dnsHeldMangle, mutate: func(f *fakeListing) {
			f.chains["mangle B4_PREROUTING"] = strings.Replace(f.chains["mangle B4_PREROUTING"], "--dport 53", "--dport 853", 1)
		}},
		{name: "copy placement complete", placement: dnsCopied, group: 537, want: true},
		{name: "copy INPUT jump wiped", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			removeLine(f, "mangle INPUT", "-A INPUT "+dnsCopyTestJumps["INPUT"])
		}},
		{name: "copy FORWARD jump wiped", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			removeLine(f, "mangle FORWARD", "-A FORWARD "+dnsCopyTestJumps["FORWARD"])
		}},
		{name: "copy OUTPUT jump wiped", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			removeLine(f, "mangle OUTPUT", "-A OUTPUT "+dnsCopyTestJumps["OUTPUT"])
		}},
		{name: "copy chain flushed", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			f.chains["mangle "+iptDNSCopyChainName] = "-N B4_DNSCOPY\n"
		}},
		{name: "copy chain gone", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			delete(f.chains, "mangle "+iptDNSCopyChainName)
		}},
		{name: "copy mark RETURN wiped", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			removeLine(f, "mangle "+iptDNSCopyChainName, "-A B4_DNSCOPY -m mark --mark 0x8000/0x8efd8000 -j RETURN")
		}},
		{name: "copy mark RETURN for another mark", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			f.chains["mangle "+iptDNSCopyChainName] = strings.Replace(f.chains["mangle "+iptDNSCopyChainName], "0x8000/0x8efd8000", "0x400000/0x8efd8000", 1)
		}},
		{name: "raw mark RETURN wiped", placement: dnsHeldRaw, mutate: func(f *fakeListing) {
			removeLine(f, "raw "+iptRawChainName, "-A B4_RAW -m mark --mark 0x8000/0x8efd8000 -j RETURN")
		}},
		{name: "raw mark RETURN for another mark", placement: dnsHeldRaw, mutate: func(f *fakeListing) {
			f.chains["raw "+iptRawChainName] = strings.Replace(f.chains["raw "+iptRawChainName], "0x8000/0x8efd8000", "0x400000/0x8efd8000", 1)
		}},
		{name: "capture chain mark RETURN wiped", placement: dnsHeldRaw, mutate: func(f *fakeListing) {
			removeLine(f, "mangle B4", "-A B4 -m mark --mark 0x8000/0x8efd8000 -j RETURN")
		}},
		{name: "capture chain mark RETURN for another mark", placement: dnsCopied, group: 537, mutate: func(f *fakeListing) {
			f.chains["mangle B4"] = strings.Replace(f.chains["mangle B4"], "0x8000/0x8efd8000", "0x400000/0x8efd8000", 1)
		}},
		{name: "copy rule logs to another group", placement: dnsCopied, group: 537, mutate: editCopyRule(func(s string) string {
			return strings.Replace(s, "--nflog-group 537", "--nflog-group 538", 1)
		})},
		{name: "copy rule without the b4 prefix", placement: dnsCopied, group: 537, mutate: editCopyRule(func(s string) string {
			return strings.NewReplacer(" --nflog-prefix  b4dns", "", " --nflog-prefix b4dns", "").Replace(s)
		})},
		{name: "copy rule with a foreign prefix", placement: dnsCopied, group: 537, mutate: editCopyRule(func(s string) string {
			return strings.Replace(s, "b4dns", "ndmdns", 1)
		})},
		{name: "copy listener dead", placement: dnsCopied, group: 537, dead: true},
		{name: "copy to group 0 without a group token", placement: dnsCopied, group: 0, want: true, mutate: editCopyRule(func(s string) string {
			return strings.Replace(s, " --nflog-group 537", "", 1)
		})},
		{name: "copy to group 0 by a rule naming a group", placement: dnsCopied, group: 0},
	} {
		formats := []bool{false}
		if tc.placement == dnsCopied {
			formats = []bool{true, false}
		}
		for _, iptables1421 := range formats {
			name := tc.name
			if tc.placement == dnsCopied {
				name += map[bool]string{true: ", iptables 1.4.21", false: ", iptables 1.8"}[iptables1421]
			}
			t.Run(name, func(t *testing.T) {
				f := monitorDNSTestListing(tc.placement, iptables1421)
				if tc.mutate != nil {
					tc.mutate(f)
				}
				run = f.run
				dnsCopyGroup.Store(tc.group)
				alive = !tc.dead
				noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: tc.placement})
				if got := m.checkIPTablesRules(&cfg); got != tc.want {
					t.Errorf("checkIPTablesRules = %v, want %v; commands:\n%s", got, tc.want, strings.Join(f.calls, "\n"))
				}
			})
		}
	}

	t.Run("copy placement without a listener func", func(t *testing.T) {
		stubDNSCopyAlive(t, nil)
		run = monitorDNSTestListing(dnsCopied, false).run
		dnsCopyGroup.Store(537)
		noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: dnsCopied})
		if m.checkIPTablesRules(&cfg) {
			t.Errorf("copy rules without a listener reading them hold nothing, so the monitor must restore")
		}
	})

	alive = true
	for _, placement := range []dnsQueryPlacement{dnsHeldMangle, dnsCopied} {
		t.Run(placement.String()+" placement whose OUTPUT jump to B4 is gone", func(t *testing.T) {
			f := monitorDNSTestListing(placement, false)
			removeLine(f, "mangle OUTPUT", "-A OUTPUT -j B4")
			run = f.run
			dnsCopyGroup.Store(537)
			noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: placement})
			if m.checkIPTablesRules(&cfg) {
				t.Errorf("the router's own traffic no longer reaches B4, so the monitor must restore; commands:\n%s", strings.Join(f.calls, "\n"))
			}
		})
		t.Run(placement.String()+" placement with a device filter", func(t *testing.T) {
			devices := cfg
			devices.Queue.Devices.Enabled = true
			devices.Queue.Devices.Devices = []config.Device{{MAC: "AA:BB:CC:DD:EE:01", Selected: true}}
			f := monitorDNSTestListing(placement, false)
			run = f.run
			dnsCopyGroup.Store(537)
			noteDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: placement})
			if m.checkIPTablesRules(&devices) {
				t.Errorf("mangle FORWARD holds no jump to B4 for the selected device, so the monitor must restore; FORWARD is:\n%s", f.chains["mangle FORWARD"])
			}
			f.chains["mangle FORWARD"] += "-A FORWARD -m mac --mac-source AA:BB:CC:DD:EE:01 -j B4\n"
			if !m.checkIPTablesRules(&devices) {
				t.Errorf("the device's FORWARD jump to B4 is in place, yet the monitor wants a restore; commands:\n%s", strings.Join(f.calls, "\n"))
			}
		})
	}
}

func TestDNSCopyListingIsReadByToken(t *testing.T) {
	for _, tc := range []struct {
		line  string
		group uint16
		want  bool
	}{
		{"-A B4_DNSCOPY -p udp -m udp --dport 53 -j NFLOG --nflog-prefix  b4dns --nflog-group 537 --nflog-threshold 1", 537, true},
		{"-A B4_DNSCOPY -p udp -m udp --dport 53 -j NFLOG --nflog-prefix b4dns --nflog-group 537 --nflog-threshold 1", 537, true},
		{"-A B4_DNSCOPY -p udp -m udp --dport 53 -j NFLOG --nflog-prefix b4dns --nflog-group 5370 --nflog-threshold 1", 537, false},
		{"-A B4_DNSCOPY -p udp -m udp --dport 53 -j NFLOG --nflog-prefix b4dnsx --nflog-group 537", 537, false},
		{"-A B4_DNSCOPY -p udp -m udp --dport 5353 -j NFLOG --nflog-prefix b4dns --nflog-group 537", 537, false},
		{"-A B4_DNSCOPY -p tcp -m tcp --dport 53 -j NFLOG --nflog-prefix b4dns --nflog-group 537 --nflog-threshold 1", 537, false},
		{"-A B4_DNSCOPY -p udp -m udp --dport 53 -j LOG --log-prefix b4dns", 537, false},
		{"-A INPUT -p udp -m udp --dport 53 -j NFLOG --nflog-prefix b4dns --nflog-group 537", 537, false},
		{"-A B4_DNSCOPY -p udp -m udp --dport 53 -j NFLOG --nflog-prefix b4dns --nflog-threshold 1", 0, true},
		{"-A B4_DNSCOPY -p udp -m udp --dport 53 -j NFLOG --nflog-prefix b4dns --nflog-group 537 --nflog-threshold 1", 0, false},
	} {
		if got := iptIsDNSCopyLine(tc.line, tc.group); got != tc.want {
			t.Errorf("iptIsDNSCopyLine(%q, %d) = %v, want %v", tc.line, tc.group, got, tc.want)
		}
	}
}

type fakeNumberedChains struct {
	rules   map[string][]string
	specs   map[string][]string
	chains  map[string]bool
	calls   []string
	deleted []string
}

func (f *fakeNumberedChains) run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	table := ""
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-t" {
			table = args[i+1]
		}
	}
	for i := 0; i < len(args)-1; i++ {
		key := table + " " + args[i+1]
		switch args[i] {
		case "-nL":
			var b strings.Builder
			b.WriteString("Chain " + args[i+1] + "\nnum  target  prot opt source  destination\n")
			for n, r := range f.rules[key] {
				b.WriteString(strconv.Itoa(n+1) + "    " + r + "\n")
			}
			return b.String(), nil
		case "-S":
			if f.chains[key] {
				return "-N " + args[i+1] + "\n", nil
			}
			return "", errors.New("exit status 1")
		case "-D":
			if n, err := strconv.Atoi(args[len(args)-1]); err == nil && len(args) == i+3 {
				if n < 1 || n > len(f.rules[key]) {
					return "", errors.New("exit status 1")
				}
				f.deleted = append(f.deleted, key+" "+f.rules[key][n-1])
				f.rules[key] = append(f.rules[key][:n-1:n-1], f.rules[key][n:]...)
				return "", nil
			}
			spec := strings.Join(args[i+2:], " ")
			at := slices.Index(f.specs[key], spec)
			if at < 0 {
				return "", errors.New("exit status 1")
			}
			f.deleted = append(f.deleted, key+" "+spec)
			f.specs[key] = slices.Delete(f.specs[key], at, at+1)
			return "", nil
		case "-F", "-X":
			f.deleted = append(f.deleted, table+" "+args[i]+" "+args[i+1])
			if args[i] == "-X" {
				delete(f.chains, key)
			}
			return "", nil
		}
	}
	return "", errors.New("exit status 1")
}

func newPlacementSwitchFake() *fakeNumberedChains {
	return &fakeNumberedChains{
		rules: map[string][]string{
			"mangle B4_PREROUTING": {
				"NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp spt:53 NFQUEUE num 537 bypass",
				"NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53 NFQUEUE num 537 bypass",
			},
			"mangle OUTPUT": {
				"ACCEPT     all  --  0.0.0.0/0  0.0.0.0/0  mark match 0x8000/0x8000",
				"NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53 NFQUEUE num 200 bypass",
				"NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53 NFQUEUE num 537 bypass",
				"NFQUEUE    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:5353 NFQUEUE num 537 bypass",
			},
			"mangle INPUT": {
				"NFLOG      udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53 nflog-group 100",
			},
			"raw PREROUTING": {
				"NOTRACK    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:123",
				"B4_RAW     udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53",
			},
			"raw OUTPUT": {
				"B4_RAW     udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53",
			},
		},
		specs: map[string][]string{
			"mangle INPUT":   {"-p udp --dport 53 -j NFLOG --nflog-group 100", dnsCopyTestJumps["INPUT"]},
			"mangle FORWARD": {dnsCopyTestJumps["FORWARD"]},
			"mangle OUTPUT":  {dnsCopyTestJumps["OUTPUT"]},
		},
		chains: map[string]bool{"raw " + iptRawChainName: true, "mangle " + iptDNSCopyChainName: true},
	}
}

func TestSwitchingPlacementRemovesTheOtherOne(t *testing.T) {
	origRun := run
	t.Cleanup(func() { run = origRun })
	fx := newKmodFixture(t)

	cfg := config.NewConfig()
	cfg.Queue.StartNum = 537
	cfg.Queue.Threads = 1
	manager := NewIPTablesManager(&cfg, false)
	drop := func(facts dnsKernel, placement dnsQueryPlacement) (*fakeNumberedChains, string) {
		t.Helper()
		stubDNSKernel(t, facts)
		f := newPlacementSwitchFake()
		run = f.run
		manager.dropUnusedDNSQueryPlacement(map[string]dnsQueryPlacement{backendIPTables: placement})
		return f, strings.Join(f.deleted, "\n")
	}
	copyRemoved := func(got string) bool {
		return strings.Contains(got, iptDNSCopyChainName)
	}
	mangleQueriesRemoved := func(got string) bool {
		return strings.Count(got, "dpt:53 ") == 2 && !strings.Contains(got, "spt:53") && !strings.Contains(got, "dpt:5353") &&
			!strings.Contains(got, "ACCEPT") && !strings.Contains(got, "num 200")
	}
	rawRemoved := func(got string) bool {
		return strings.Count(got, "raw PREROUTING B4_RAW") == 1 && strings.Count(got, "raw OUTPUT B4_RAW") == 1 &&
			strings.Contains(got, "raw -F B4_RAW") && strings.Contains(got, "raw -X B4_RAW")
	}

	_, got := drop(unaffectedDNSKernel, dnsHeldRaw)
	if !mangleQueriesRemoved(got) {
		t.Errorf("moving queries to the raw table must delete exactly the two mangle query rules of this queue, deleted:\n%s", got)
	}
	if strings.Contains(got, "B4_RAW") {
		t.Errorf("the raw placement in use was removed:\n%s", got)
	}

	f, got := drop(unaffectedDNSKernel, dnsHeldMangle)
	if !rawRemoved(got) {
		t.Errorf("falling back to mangle must delete both raw jumps and the raw chain, deleted:\n%s", got)
	}
	if strings.Contains(got, "NOTRACK") {
		t.Errorf("a foreign raw rule was deleted:\n%s", got)
	}
	for _, c := range f.calls {
		if strings.Contains(c, iptDNSCopyChainName) {
			t.Errorf("an unaffected kernel never copies DNS queries, so it must not touch %s: %s", iptDNSCopyChainName, c)
		}
	}

	t.Run("copying on a vanilla kernel drops the held placements", func(t *testing.T) {
		_, got := drop(affectedDNSKernel, dnsCopied)
		if !mangleQueriesRemoved(got) {
			t.Errorf("copying must delete exactly the two mangle query rules of this queue, deleted:\n%s", got)
		}
		if !rawRemoved(got) {
			t.Errorf("copying must delete the raw jumps and chain an earlier placement left, deleted:\n%s", got)
		}
		if strings.Contains(got, "NOTRACK") || strings.Contains(got, "nflog-group 100") || copyRemoved(got) {
			t.Errorf("copying removed a foreign rule or its own copy chain, deleted:\n%s", got)
		}
	})

	t.Run("copying on KeeneticOS removes the raw chain only while its module is loaded", func(t *testing.T) {
		f, got := drop(keeneticDNSKernel, dnsCopied)
		if !mangleQueriesRemoved(got) {
			t.Errorf("copying must delete exactly the two mangle query rules of this queue, deleted:\n%s", got)
		}
		if raw := commandsMatching(f.calls, rawTableCommand); len(raw) > 0 {
			t.Errorf("without iptable_raw each raw command makes the KeeneticOS kernel try to load it:\n%s", strings.Join(raw, "\n"))
		}
		fx.loaded("iptable_raw")
		_, got = drop(keeneticDNSKernel, dnsCopied)
		if !rawRemoved(got) {
			t.Errorf("with iptable_raw loaded, copying must remove a raw chain an older b4 left, deleted:\n%s", got)
		}
	})

	t.Run("holding on an affected kernel drops the copy chain and its jumps", func(t *testing.T) {
		for _, placement := range []dnsQueryPlacement{dnsHeldMangle, dnsHeldRaw} {
			_, got := drop(affectedDNSKernel, placement)
			for chain, spec := range dnsCopyTestJumps {
				if strings.Count(got, "mangle "+chain+" "+spec) != 1 {
					t.Errorf("%s: the mangle %s copy jump was not deleted:\n%s", placement, chain, got)
				}
			}
			if !strings.Contains(got, "mangle -F "+iptDNSCopyChainName) || !strings.Contains(got, "mangle -X "+iptDNSCopyChainName) {
				t.Errorf("%s: the copy chain was not removed, deleted:\n%s", placement, got)
			}
			if strings.Contains(got, "nflog-group 100") {
				t.Errorf("%s: a foreign NFLOG rule was deleted:\n%s", placement, got)
			}
		}
	})
}

func nftFailingListStub(t *testing.T) string {
	t.Helper()
	return nftStubFailing(t, "")
}

func nftStubFailing(t *testing.T, failPrefix string) string {
	t.Helper()
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.log")
	fail := ""
	if failPrefix != "" {
		fail = "case \"$*\" in \"" + failPrefix + "\"*) echo 'Error: Could not process rule' >&2; exit 1 ;; esac\n"
	}
	stub := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + argvLog + "\n" +
		"if [ \"$1\" = list ]; then exit 1; fi\n" +
		fail +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvLog
}

func TestNftQueuesDNSQueriesBeforeConntrack(t *testing.T) {
	argvLog := nftFailingListStub(t)
	cfg := config.NewConfig()
	cfg.Queue.StartNum = 537
	cfg.Queue.Threads = 1
	n := NewNFTablesManager(&cfg)
	if err := n.addDNSQueryQueueChains(nftOwnPacketMark(cfg.Queue.Mark)); err != nil {
		t.Fatalf("addDNSQueryQueueChains: %v", err)
	}
	argv := readLog(t, argvLog)
	queue := n.buildNFQueueAction()
	for _, want := range []string{
		"add chain inet b4_mangle raw_prerouting { type filter hook prerouting priority -300 ; policy accept ; }",
		"add chain inet b4_mangle raw_output { type filter hook output priority -300 ; policy accept ; }",
		"add rule inet b4_mangle raw_output oifname \"lo\" return",
		"add rule inet b4_mangle raw_prerouting meta mark & 0x8efd8000 == 0x8000 return",
		"add rule inet b4_mangle raw_output meta mark & 0x8efd8000 == 0x8000 return",
		"add rule inet b4_mangle raw_prerouting udp dport 53 counter " + queue,
		"add rule inet b4_mangle raw_output udp dport 53 counter " + queue,
	} {
		if !strings.Contains(argv, want+"\n") {
			t.Errorf("missing nft command %q in:\n%s", want, argv)
		}
	}
	lines := strings.Split(strings.TrimSpace(argv), "\n")
	markAt, queueAt := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "add rule inet b4_mangle raw_prerouting meta mark") {
			markAt = i
		}
		if strings.HasPrefix(l, "add rule inet b4_mangle raw_prerouting udp dport 53") {
			queueAt = i
		}
	}
	if markAt < 0 || queueAt < 0 || markAt > queueAt {
		t.Errorf("b4's own packets must be let through before the queue rule:\n%s", argv)
	}
}

func TestNftFallsBackToQueueingDNSQueriesAfterConntrack(t *testing.T) {
	argvLog := nftStubFailing(t, "add chain inet b4_mangle raw_prerouting")
	cfg := config.NewConfig()
	cfg.Queue.StartNum = 537
	cfg.Queue.Threads = 1
	n := NewNFTablesManager(&cfg)
	fromRaw, err := n.addDNSQueryRules(nftOwnPacketMark(cfg.Queue.Mark))
	if err != nil {
		t.Fatalf("a chain the kernel refuses must not fail the whole apply: %v", err)
	}
	if fromRaw {
		t.Fatalf("addDNSQueryRules reports the raw placement although its chain was refused")
	}
	argv := readLog(t, argvLog)
	queue := n.buildNFQueueAction()
	for _, want := range []string{
		"add rule inet b4_mangle prerouting udp dport 53 counter " + queue,
		"add rule inet b4_mangle output udp dport 53 counter " + queue,
		"delete chain inet b4_mangle raw_output",
	} {
		if !strings.Contains(argv, want+"\n") {
			t.Errorf("missing nft command %q in:\n%s", want, argv)
		}
	}
}

func TestDiscoveryLeavesAnAbsentRawTableAlone(t *testing.T) {
	fx := newKmodFixture(t)
	stubDNSKernel(t, keeneticDNSKernel)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
	rec := recordFirewall(t, nil)
	b := &discoveryIptBackend{}
	run := func() []string {
		t.Helper()
		rec.reset()
		if err := b.apply(0x8001, 0x8002, 537, 1); err != nil {
			t.Fatalf("apply: %v", err)
		}
		b.clear(0x8001, 0x8002)
		return rec.commands()
	}
	rawOf := func(cmds []string, ipt string) []string {
		return commandsMatching(cmds, func(args []string) bool { return args[0] == ipt && rawTableCommand(args) })
	}

	cmds := run()
	if raw := commandsMatching(cmds, rawTableCommand); len(raw) > 0 {
		t.Errorf("without iptable_raw each raw command makes the KeeneticOS kernel try to load it, and no B4_RAW can exist there:\n%s", strings.Join(raw, "\n"))
	}
	if !slices.Contains(cmds, "iptables -w -t mangle -I B4 1 -m mark --mark 0x8001/0xffffffff -j RETURN") {
		t.Errorf("Discovery must still keep its marks out of the mangle chain:\n%s", strings.Join(cmds, "\n"))
	}

	fx.loaded("iptable_raw")
	cmds = run()
	if !slices.Contains(cmds, "iptables -w -t raw -I B4_RAW 1 -m mark --mark 0x8001/0xffffffff -j RETURN") {
		t.Errorf("with iptable_raw loaded an older b4's B4_RAW may still queue DNS queries, so Discovery must keep its marks out of it:\n%s", strings.Join(rawOf(cmds, backendIPTables), "\n"))
	}
	if raw := rawOf(cmds, backendIP6Tables); len(raw) > 0 {
		t.Errorf("ip6table_raw is not loaded, yet ip6tables ran raw commands:\n%s", strings.Join(raw, "\n"))
	}
}

func TestDiscoveryKeepsItsMarksOutOfTheDNSQueryChains(t *testing.T) {
	origRun := run
	t.Cleanup(func() { run = origRun })
	var calls []string
	run = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 1 && args[0] == "nft" && args[1] == "-a" {
			return "", errors.New("exit status 1")
		}
		return "", nil
	}

	discoveryKeepOutOfQueueChain(backendIPTables, "0x10/0xffffffff", "0x20/0xffffffff")
	(&discoveryNftBackend{}).keepOutOfQueueChain("0x10", "0x20", 0x10, 0x20)
	got := strings.Join(calls, "\n")
	for _, want := range []string{
		"iptables -w -t raw -I B4_RAW 1 -m mark --mark 0x10/0xffffffff -j RETURN",
		"iptables -w -t raw -I B4_RAW 1 -m mark --mark 0x20/0xffffffff -j RETURN",
		"iptables -w -t mangle -I B4 1 -m mark --mark 0x10/0xffffffff -j RETURN",
		"nft insert rule inet b4_mangle raw_prerouting meta mark 0x10 return",
		"nft insert rule inet b4_mangle raw_output meta mark 0x20 return",
		"nft insert rule inet b4_mangle b4_chain meta mark 0x10 return",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a Discovery probe with a custom mark must stay out of the DNS query queue as well; missing %q in:\n%s", want, got)
		}
	}
}
