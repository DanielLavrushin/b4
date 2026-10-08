package tables

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		for _, bin := range []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy} {
			rawFallbackWarned.Delete(bin)
		}
	}
	forget()
	t.Cleanup(forget)
}

func dnsQueryTestManager(t *testing.T, rawErr error) *IPTablesManager {
	t.Helper()
	stubNoExec(t)
	forgetRawFallbackWarnings(t)
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
		if !m.RawDNS[ipt] {
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
		if len(chain) != 2 || !strings.Contains(chain[0], "--mark 0x8000/0x8000 -j RETURN") || !hasSpecWith(chain[1:], "-p udp --dport 53", "-j NFQUEUE", "--queue-bypass") {
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
		if m.RawDNS[ipt] {
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

	noteDNSQueryPlacement(nil)
	if dnsQueryPlacementChanged(map[string]bool{backendIPTables: true}) {
		t.Errorf("a first apply has nothing to move")
	}
	noteDNSQueryPlacement(map[string]bool{backendIPTables: false})
	if !dnsQueryPlacementChanged(map[string]bool{backendIPTables: true}) {
		t.Errorf("queries moving from mangle to raw on a restore must rebuild the rules, or the mangle answer rules keep a stale shape and sit above the queue-mark accept")
	}
	if dnsQueryPlacementChanged(map[string]bool{backendIPTables: false}) {
		t.Errorf("an unchanged placement must not rebuild the rules")
	}
	if dnsQueryPlacementChanged(map[string]bool{backendIP6Tables: true}) {
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
	if len(m.RawDNS) != 0 {
		t.Errorf("a teardown manifest must not record a placement, got %v", m.RawDNS)
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

func monitorDNSTestListing(fromRaw bool) *fakeListing {
	pre := "-N B4_PREROUTING\n-A B4_PREROUTING -m connmark --mark 0x8000/0x8000 -j RETURN\n-A B4_PREROUTING -p udp -m udp --sport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
	out := "-P OUTPUT ACCEPT\n-A OUTPUT -m mark --mark 0x8000/0x8000 -j ACCEPT\n-A OUTPUT -p udp -m udp --sport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
	if !fromRaw {
		pre += "-A B4_PREROUTING -p udp -m udp --dport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
		out += "-A OUTPUT -p udp -m udp --dport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
	}
	pre += "-A B4_PREROUTING -p tcp -m multiport --sports 443 -m tcp --tcp-flags SYN,ACK SYN,ACK -j NFQUEUE --queue-num 537 --queue-bypass\n"
	out += "-A OUTPUT -j B4\n"
	f := &fakeListing{chains: map[string]string{
		"mangle B4":            "-N B4\n",
		"mangle POSTROUTING":   "-P POSTROUTING ACCEPT\n-A POSTROUTING -j B4\n",
		"mangle PREROUTING":    "-P PREROUTING ACCEPT\n-A PREROUTING -j B4_PREROUTING\n",
		"mangle B4_PREROUTING": pre,
		"mangle OUTPUT":        out,
	}}
	if fromRaw {
		jump := strings.Join(iptRawDNSJump(), " ") + "\n"
		f.chains["raw PREROUTING"] = "-P PREROUTING ACCEPT\n-A PREROUTING " + jump
		f.chains["raw OUTPUT"] = "-P OUTPUT ACCEPT\n-A OUTPUT " + jump
		f.chains["raw "+iptRawChainName] = "-N B4_RAW\n-A B4_RAW -m mark --mark 0x8000/0x8000 -j RETURN\n-A B4_RAW -p udp -m udp --dport 53 -j NFQUEUE --queue-num 537 --queue-bypass\n"
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

	m, _ := newLockTestMonitor(t)
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.Mark = 0x8000

	for _, tc := range []struct {
		name    string
		fromRaw bool
		mutate  func(f *fakeListing)
		want    bool
	}{
		{"raw placement complete", true, nil, true},
		{"mangle placement complete", false, nil, true},
		{"raw PREROUTING jump wiped", true, func(f *fakeListing) { delete(f.chains, "raw PREROUTING") }, false},
		{"raw OUTPUT jump wiped", true, func(f *fakeListing) { delete(f.chains, "raw OUTPUT") }, false},
		{"raw chain flushed", true, func(f *fakeListing) { f.chains["raw "+iptRawChainName] = "-N B4_RAW\n" }, false},
		{"raw table gone", true, func(f *fakeListing) {
			delete(f.chains, "raw PREROUTING")
			delete(f.chains, "raw OUTPUT")
			delete(f.chains, "raw "+iptRawChainName)
		}, false},
		{"mangle query rule wiped", false, func(f *fakeListing) {
			f.chains["mangle B4_PREROUTING"] = strings.Replace(f.chains["mangle B4_PREROUTING"], "--dport 53", "--dport 853", 1)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := monitorDNSTestListing(tc.fromRaw)
			if tc.mutate != nil {
				tc.mutate(f)
			}
			run = f.run
			noteDNSQueryPlacement(map[string]bool{backendIPTables: tc.fromRaw})
			if got := m.checkIPTablesRules(&cfg); got != tc.want {
				t.Errorf("checkIPTablesRules = %v, want %v; commands:\n%s", got, tc.want, strings.Join(f.calls, "\n"))
			}
		})
	}
}

type fakeNumberedChains struct {
	rules   map[string][]string
	deleted []string
}

func (f *fakeNumberedChains) run(args ...string) (string, error) {
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
			if args[i+1] == iptRawChainName {
				return "-N " + iptRawChainName + "\n", nil
			}
			return "", errors.New("exit status 1")
		case "-D":
			n, err := strconv.Atoi(args[len(args)-1])
			if err != nil || n < 1 || n > len(f.rules[key]) {
				return "", errors.New("exit status 1")
			}
			f.deleted = append(f.deleted, key+" "+f.rules[key][n-1])
			f.rules[key] = append(f.rules[key][:n-1:n-1], f.rules[key][n:]...)
			return "", nil
		case "-F", "-X":
			f.deleted = append(f.deleted, table+" "+args[i]+" "+args[i+1])
			return "", nil
		}
	}
	return "", errors.New("exit status 1")
}

func TestSwitchingPlacementRemovesTheOtherOne(t *testing.T) {
	origRun := run
	t.Cleanup(func() { run = origRun })

	f := &fakeNumberedChains{rules: map[string][]string{
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
		"raw PREROUTING": {
			"NOTRACK    udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:123",
			"B4_RAW     udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53",
		},
		"raw OUTPUT": {
			"B4_RAW     udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53",
		},
	}}
	run = f.run

	cfg := config.NewConfig()
	cfg.Queue.StartNum = 537
	cfg.Queue.Threads = 1
	manager := NewIPTablesManager(&cfg, false)
	manager.dropUnusedDNSQueryPlacement(map[string]bool{backendIPTables: true})
	got := strings.Join(f.deleted, "\n")
	if strings.Count(got, "dpt:53 ") != 2 || strings.Contains(got, "spt:53") || strings.Contains(got, "dpt:5353") || strings.Contains(got, "ACCEPT") {
		t.Errorf("moving queries to the raw table must delete exactly the two mangle query rules, deleted:\n%s", got)
	}
	if strings.Contains(got, "num 200") {
		t.Errorf("another service's DNS queue rule in mangle OUTPUT was deleted:\n%s", got)
	}
	if strings.Contains(got, "B4_RAW") {
		t.Errorf("the raw placement in use was removed:\n%s", got)
	}

	f.deleted = nil
	manager.dropUnusedDNSQueryPlacement(map[string]bool{backendIPTables: false})
	got = strings.Join(f.deleted, "\n")
	if strings.Count(got, "raw PREROUTING B4_RAW") != 1 || strings.Count(got, "raw OUTPUT B4_RAW") != 1 {
		t.Errorf("falling back to mangle must delete both raw jumps, deleted:\n%s", got)
	}
	if strings.Contains(got, "NOTRACK") {
		t.Errorf("a foreign raw rule was deleted:\n%s", got)
	}
	if !strings.Contains(got, "raw -F B4_RAW") || !strings.Contains(got, "raw -X B4_RAW") {
		t.Errorf("falling back to mangle must remove the raw chain, deleted:\n%s", got)
	}
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
	if err := n.addDNSQueryQueueChains("0x8000"); err != nil {
		t.Fatalf("addDNSQueryQueueChains: %v", err)
	}
	argv := readLog(t, argvLog)
	queue := n.buildNFQueueAction()
	for _, want := range []string{
		"add chain inet b4_mangle raw_prerouting { type filter hook prerouting priority -300 ; policy accept ; }",
		"add chain inet b4_mangle raw_output { type filter hook output priority -300 ; policy accept ; }",
		"add rule inet b4_mangle raw_output oifname \"lo\" return",
		"add rule inet b4_mangle raw_prerouting meta mark & 0x8000 == 0x8000 return",
		"add rule inet b4_mangle raw_output meta mark & 0x8000 == 0x8000 return",
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
	fromRaw, err := n.addDNSQueryRules("0x8000")
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
