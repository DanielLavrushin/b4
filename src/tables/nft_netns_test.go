package tables

import (
	"fmt"
	"strings"
	"testing"
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
