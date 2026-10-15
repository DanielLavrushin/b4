package tables

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func netnsRequireNft(t *testing.T) {
	t.Helper()
	netnsRequire(t)
	if !hasBinary("nft") {
		t.Skip("nft is not installed")
	}
}

func netnsDropNftTable(t *testing.T, table string) {
	t.Helper()
	_, _ = run("nft", "delete", "table", "inet", table)
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", table) })
}

func TestNetnsNftDuplicateSetsAcceptOverlappingPrefixes(t *testing.T) {
	netnsRequireNft(t)
	netnsDropNftTable(t, nftTableName)

	n := NewNFTablesManager(dupTestConfig())
	if err := n.createTable(); err != nil {
		t.Fatalf("createTable: %v", err)
	}
	if err := n.createChain(nftChainName, "", 0, ""); err != nil {
		t.Fatalf("createChain: %v", err)
	}

	if err := n.createSet("b4_dup_control", "ipv4_addr", "flags interval ;"); err != nil {
		t.Fatalf("createSet: %v", err)
	}
	err := n.addSetElements("b4_dup_control", []string{"8.8.0.0/16", "8.8.8.0/24"})
	if err == nil || !strings.Contains(err.Error(), "conflicting intervals") {
		t.Fatalf("the control set without auto-merge must reject overlapping prefixes, or this test proves nothing: %v", err)
	}

	if err := n.addDuplicateQueueRules("443"); err != nil {
		t.Fatalf("two duplicate sets with overlapping prefixes must load: %v", err)
	}
	v4 := netnsRun(t, "nft", "list", "set", "inet", nftTableName, "b4_dup_v4")
	if !strings.Contains(v4, "8.8.0.0/16") || !strings.Contains(v4, "auto-merge") {
		t.Errorf("expected the wide prefix to absorb the narrow ones:\n%s", v4)
	}
	v6 := netnsRun(t, "nft", "list", "set", "inet", nftTableName, "b4_dup_v6")
	if !strings.Contains(v6, "2001:4860::/32") {
		t.Errorf("expected the wide ipv6 prefix to absorb the narrow one:\n%s", v6)
	}
	chain := netnsRun(t, "nft", "list", "chain", "inet", nftTableName, nftChainName)
	if !strings.Contains(chain, "@b4_dup_v4") || !strings.Contains(chain, "@b4_dup_v6") {
		t.Errorf("the queue rules for the duplicate sets are missing:\n%s", chain)
	}
}

func TestNetnsNftRoutingStaticEntriesLoadInOneScript(t *testing.T) {
	netnsRequireNft(t)
	netnsDropNftTable(t, routeNftTable)

	be := &routeNftBackend{}
	if err := be.ensureBase(); err != nil {
		t.Fatalf("ensureBase: %v", err)
	}
	const setName = "b4r_netnsscript_v4"
	if err := be.ensureIPSet(setName, false); err != nil {
		t.Fatalf("ensureIPSet: %v", err)
	}

	origRun, origStdin := run, runNftStdin
	t.Cleanup(func() {
		run = origRun
		runNftStdin = origStdin
	})
	var scripts, elementCalls int
	runNftStdin = func(script string) (string, error) {
		scripts++
		return origStdin(script)
	}
	run = func(args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), " element ") {
			elementCalls++
		}
		return origRun(args...)
	}

	entries := []string{"8.8.0.0/16", "8.8.8.0/24", "203.0.113.9"}
	for i := 0; i < 2000; i++ {
		entries = append(entries, fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
	}
	be.addElements(setName, entries, 0)
	if scripts != 1 || elementCalls != 0 {
		t.Errorf("2003 static entries must load in one script, got scripts=%d element execs=%d", scripts, elementCalls)
	}
	listed := netnsRun(t, "nft", "list", "set", "inet", routeNftTable, setName)
	for _, want := range []string{"8.8.0.0/16", "203.0.113.9"} {
		if !strings.Contains(listed, want) {
			t.Errorf("%s is missing from the set:\n%.600s", want, listed)
		}
	}

	scripts, elementCalls = 0, 0
	be.addElements(setName, []string{"198.51.100.7", "bogus"}, 0)
	if scripts != 1 || elementCalls == 0 {
		t.Errorf("a rejected script must fall back to the batch path, got scripts=%d element execs=%d", scripts, elementCalls)
	}
	if listed := netnsRun(t, "nft", "list", "set", "inet", routeNftTable, setName); !strings.Contains(listed, "198.51.100.7") {
		t.Errorf("the good entry must survive a bad one next to it:\n%.600s", listed)
	}

	scripts, elementCalls = 0, 0
	be.delElements(setName, []string{"203.0.113.9"})
	if scripts != 1 || elementCalls != 0 {
		t.Errorf("a delete must go through one script, got scripts=%d element execs=%d", scripts, elementCalls)
	}
	if listed := netnsRun(t, "nft", "list", "set", "inet", routeNftTable, setName); strings.Contains(listed, "203.0.113.9") {
		t.Errorf("the deleted entry is still in the set:\n%.600s", listed)
	}
}

func netnsNftScript(t *testing.T, script string) {
	t.Helper()
	if out, err := runNftStdin(script); err != nil {
		t.Fatalf("nft -f: %v (%s)", err, strings.TrimSpace(out))
	}
}

func netnsLoadIntervalMap(t *testing.T, table string, n int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\nadd chain inet %s v31\nadd rule inet %s v31 ip dscp set 31\n", table, table, table)
	fmt.Fprintf(&b, "add map inet %s s4_1 { type ipv4_addr : verdict ; flags interval ; }\n", table)
	netnsNftScript(t, b.String())
	for start := 0; start < n; start += 1000 {
		b.Reset()
		fmt.Fprintf(&b, "add element inet %s s4_1 { ", table)
		for i := start; i < min(start+1000, n); i++ {
			if i > start {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%d.%d.%d.0/25 : jump v31", 10+i>>16, (i>>8)&255, i&255)
		}
		b.WriteString(" }\n")
		netnsNftScript(t, b.String())
	}
}

func TestNetnsNftLearnedElementsOverNetlink(t *testing.T) {
	netnsRequireNft(t)
	netnsDropNftTable(t, routeNftTable)
	netnsDropNftTable(t, dscpNftTable)

	be := &routeNftBackend{}
	if err := be.ensureBase(); err != nil {
		t.Fatalf("ensureBase: %v", err)
	}
	const setV4, setV6 = "b4r_netnsnl_v4", "b4r_netnsnl_v6"
	for name, v6 := range map[string]bool{setV4: false, setV6: true} {
		if err := be.ensureIPSet(name, v6); err != nil {
			t.Fatalf("ensureIPSet %s: %v", name, err)
		}
	}
	dynV4, dynV6 := routeNftDynSet(setV4), routeNftDynSet(setV6)
	listed := func(set string) string { return netnsRun(t, "nft", "list", "set", "inet", routeNftTable, set) }
	addrs := func(list ...string) []netip.Addr {
		out := make([]netip.Addr, len(list))
		for i, a := range list {
			out[i] = netip.MustParseAddr(a)
		}
		return out
	}

	if err := nftRefreshElementsNetlink(routeNftTable, dynV4, addrs("203.0.113.9", "203.0.113.10"), time.Hour); err != nil {
		t.Fatalf("the netlink refresh of a learned set failed: %v", err)
	}
	for _, want := range []string{"203.0.113.9 timeout 1h", "203.0.113.10 timeout 1h"} {
		if got := listed(dynV4); !strings.Contains(got, want) {
			t.Errorf("%q is missing after the first write:\n%s", want, got)
		}
	}
	if err := nftRefreshElementsNetlink(routeNftTable, dynV4, addrs("203.0.113.9"), 10*time.Second); err != nil {
		t.Fatalf("refreshing an element that is already there failed: %v", err)
	}
	if got := listed(dynV4); !strings.Contains(got, "203.0.113.9 timeout 10s") || !strings.Contains(got, "203.0.113.10 timeout 1h") {
		t.Errorf("the refresh must give the element its new timeout and leave the others alone:\n%s", got)
	}
	if err := nftRefreshElementsNetlink(routeNftTable, dynV6, addrs("2001:db8::9"), time.Hour); err != nil {
		t.Fatalf("the netlink refresh of an IPv6 learned set failed: %v", err)
	}
	if got := listed(dynV6); !strings.Contains(got, "2001:db8::9 timeout 1h") {
		t.Errorf("the IPv6 element is missing:\n%s", got)
	}
	if err := nftRefreshElementsNetlink(routeNftTable, "b4r_missing_v4_d", addrs("203.0.113.9"), time.Hour); err == nil {
		t.Error("a write into a set that does not exist was reported as done")
	}
	if err := nftRefreshElementsNetlink(routeNftTable, dynV4, addrs("2001:db8::10"), time.Hour); err == nil {
		t.Error("an IPv6 address written into an IPv4 set was reported as done")
	}

	const intervals = 30000
	const dnsHold = 250 * time.Millisecond
	netnsLoadIntervalMap(t, dscpNftTable, intervals)
	start := time.Now()
	if out, err := run(routeNftRefreshArgs(routeNftTable, dynV4, []string{"198.51.100.1", "198.51.100.2"}, 3600)...); err != nil {
		t.Fatalf("the nft refresh failed: %v (%s)", err, strings.TrimSpace(out))
	}
	viaNft := time.Since(start)

	origRun := run
	t.Cleanup(func() { run = origRun })
	var spawned []string
	run = func(args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), " element ") {
			spawned = append(spawned, strings.Join(args, " "))
		}
		return origRun(args...)
	}
	prevApplied := dscpApplied.Load()
	t.Cleanup(func() { dscpApplied.Store(prevApplied) })
	dscpApplied.Store(nil)
	if failed := be.addElements(setV4, []string{"198.51.100.5"}, 3600); len(failed) != 0 || len(spawned) == 0 {
		t.Fatalf("with no per-set DSCP objects routing's learned write must keep using the nft command: spawned %q, failed %v", spawned, failed)
	}
	spawned = nil

	withDSCPNftPlanApplied(t)
	start = time.Now()
	failed := be.addElements(setV4, []string{"198.51.100.3", "198.51.100.4"}, 3600)
	viaNetlink := time.Since(start)
	run = origRun
	t.Logf("a learned refresh with %d intervals in another table: %v through nft, %v over netlink", intervals, viaNft, viaNetlink)
	if len(failed) != 0 || len(spawned) != 0 {
		t.Fatalf("routing's learned write spawned nft %q or failed %v", spawned, failed)
	}
	if got := listed(dynV4); !strings.Contains(got, "198.51.100.3 timeout 1h") || !strings.Contains(got, "198.51.100.4 timeout 1h") {
		t.Errorf("routing's learned write did not land:\n%s", got)
	}
	if viaNetlink > dnsHold {
		t.Errorf("a learned write took %v with %d intervals in another table, longer than the %v a DNS answer waits for it", viaNetlink, intervals, dnsHold)
	}
}
