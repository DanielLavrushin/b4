package tables

import (
	"github.com/daniellavrushin/b4/config"
	"os"
	"path/filepath"
	"testing"
)

func TestRouteParseRtTables(t *testing.T) {
	names := map[int]string{}
	routeParseRtTables("#\n# reserved values\n#\n255\tlocal\n254\tmain\n253\tdefault\n0\tunspec\n#\n252\tvpn\n1  inr.ruhep # trailing\nbogus\tname\n", names)

	if got := names[252]; got != "vpn" {
		t.Fatalf("252 = %q, want vpn", got)
	}
	if got := names[254]; got != "main" {
		t.Fatalf("254 = %q, want main", got)
	}
	if got := names[1]; got != "inr.ruhep" {
		t.Fatalf("1 = %q, want inr.ruhep", got)
	}
	if _, ok := names[0]; !ok {
		t.Fatalf("0 missing")
	}
}

func TestRouteTableNameReadsRtTablesDir(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "rt_tables")
	if err := os.WriteFile(main, []byte("255 local\n252 vpn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, "rt_tables.d")
	if err := os.Mkdir(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "mwan3.conf"), []byte("251 mwan3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "ignored.txt"), []byte("250 nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldFile, oldDir := rtTablesFile, rtTablesDir
	rtTablesFile, rtTablesDir = main, extra
	routeForgetRtTableNames()
	t.Cleanup(func() {
		rtTablesFile, rtTablesDir = oldFile, oldDir
		routeForgetRtTableNames()
	})

	if name, ok := routeTableName(252); !ok || name != "vpn" {
		t.Fatalf("252 = %q,%v; want vpn,true", name, ok)
	}
	if name, ok := routeTableName(251); !ok || name != "mwan3" {
		t.Fatalf("251 = %q,%v; want mwan3,true", name, ok)
	}
	if _, ok := routeTableName(250); ok {
		t.Fatalf("250 should not be named, .txt files are not read")
	}
}

func TestRouteTableTakenByOthersRejectsANamedTable(t *testing.T) {
	oldNames, oldForeign := routeRtTableNames, routeTableForeignRoutes
	routeRtTableNames = func() map[int]string {
		return map[int]string{255: "local", 254: "main", 253: "default", 252: "vpn"}
	}
	routeTableForeignRoutes = func(int, string) bool { return false }
	routeForgetRtTableNames()
	t.Cleanup(func() {
		routeRtTableNames = oldNames
		routeTableForeignRoutes = oldForeign
		routeForgetRtTableNames()
	})

	if !hasBinary("ip") {
		t.Skip("ip binary not present")
	}
	if !routeTableTakenByOthers(proxyLocalDeliveryTable, "eth0", nil) {
		t.Fatalf("table %d is named 'vpn' in rt_tables and must read as taken", proxyLocalDeliveryTable)
	}
	if routeTableTakenByOthers(137, "eth0", nil) {
		t.Fatalf("table 137 is unnamed and holds no foreign routes, it must read as free")
	}
}

func TestProxyTableCachesTheChoiceUntilCleared(t *testing.T) {
	oldPick := routePickProxyTable
	calls := 0
	routePickProxyTable = func() int {
		calls++
		return 317
	}
	proxyTableForget()
	t.Cleanup(func() {
		routePickProxyTable = oldPick
		proxyTableForget()
	})

	if got := proxyTable(); got != 317 {
		t.Fatalf("proxyTable() = %d, want 317", got)
	}
	if got := proxyTable(); got != 317 {
		t.Fatalf("second proxyTable() = %d, want 317", got)
	}
	if calls != 1 {
		t.Fatalf("picker ran %d times, want 1", calls)
	}

	proxyTableForget()
	if got := proxyTable(); got != 317 || calls != 2 {
		t.Fatalf("after forget: table=%d calls=%d, want 317 and 2", got, calls)
	}
}

func TestRouteProxyTableCandidatesPrefer252AndAvoidPerSetBand(t *testing.T) {
	cands := routeProxyTableCandidates()
	if len(cands) == 0 || cands[0] != proxyLocalDeliveryTable {
		t.Fatalf("first candidate = %v, want %d", cands, proxyLocalDeliveryTable)
	}
	for _, c := range cands {
		if c >= 100 && c <= 249 {
			t.Fatalf("candidate %d falls inside the per-set routing table band 100-249", c)
		}
		if c == 253 || c == 254 || c == 255 || c == 0 {
			t.Fatalf("candidate %d is a reserved table", c)
		}
	}
}

func TestRouteLineIsProxyLocal(t *testing.T) {
	yes := []string{
		"local default dev lo scope host",
		"local 0.0.0.0/0 dev lo",
		"local ::/0 dev lo",
	}
	for _, l := range yes {
		if !routeLineIsProxyLocal(l) {
			t.Fatalf("%q should read as b4's own local-delivery route", l)
		}
	}
	no := []string{
		"default via 10.0.0.1 dev eth0",
		"local 10.0.0.1 dev eth0 scope host",
		"local default dev eth0",
		"192.168.1.0/24 dev br-lan",
		"",
	}
	for _, l := range no {
		if routeLineIsProxyLocal(l) {
			t.Fatalf("%q should not read as b4's own local-delivery route", l)
		}
	}
}

func TestRouteRuleField(t *testing.T) {
	line := "32764:\tfrom all to 1.0.0.1 lookup vpn"
	if got := routeRuleField(line, "lookup"); got != "vpn" {
		t.Fatalf("lookup = %q, want vpn", got)
	}
	if got := routeRuleField(line, "fwmark"); got != "" {
		t.Fatalf("fwmark = %q, want empty", got)
	}
	line2 := "3:\tfrom all fwmark 0x237a0/0x27fff lookup 252"
	if got := routeRuleField(line2, "fwmark"); got != "0x237a0/0x27fff" {
		t.Fatalf("fwmark = %q", got)
	}
	if got := routeRuleField(line2, "lookup"); got != "252" {
		t.Fatalf("lookup = %q", got)
	}
}

func TestRouteRuleRefsInMatchesByNumberAndByName(t *testing.T) {
	oldNames := routeRtTableNames
	routeRtTableNames = func() map[int]string { return map[int]string{252: "vpn"} }
	routeForgetRtTableNames()
	t.Cleanup(func() {
		routeRtTableNames = oldNames
		routeForgetRtTableNames()
	})

	targets := map[string][]string{
		"vpn":  {"32764:\tfrom all to 1.0.0.1 lookup vpn"},
		"9999": {"100:\tfrom all fwmark 0x10000000/0x10000000 lookup 9999"},
	}
	if refs := routeRuleRefsIn(targets, 252); len(refs) != 1 {
		t.Fatalf("table 252 is named vpn and one rule looks it up, got %v", refs)
	}
	if refs := routeRuleRefsIn(targets, 9999); len(refs) != 1 {
		t.Fatalf("table 9999 is referenced by number, got %v", refs)
	}
	if refs := routeRuleRefsIn(targets, 317); len(refs) != 0 {
		t.Fatalf("table 317 is referenced by nothing, got %v", refs)
	}
}

func TestRouteTableHeldInIgnoresOurOwnLocalRoute(t *testing.T) {
	oldNames := routeRtTableNames
	routeRtTableNames = func() map[int]string { return map[int]string{252: "vpn"} }
	routeForgetRtTableNames()
	t.Cleanup(func() {
		routeRtTableNames = oldNames
		routeForgetRtTableNames()
	})

	if routeTableHeldIn(map[string]bool{}, 252) {
		t.Fatalf("an empty table must read as free even when it has a name")
	}
	if !routeTableHeldIn(map[string]bool{"vpn": true}, 252) {
		t.Fatalf("routes listed under the table's name must count")
	}
	if !routeTableHeldIn(map[string]bool{"317": true}, 317) {
		t.Fatalf("routes listed under the table's number must count")
	}
}

func TestRouteRuleRefsInIgnoresB4sOwnRules(t *testing.T) {
	oldNames := routeRtTableNames
	routeRtTableNames = func() map[int]string { return map[int]string{} }
	routeForgetRtTableNames()
	t.Cleanup(func() {
		routeRtTableNames = oldNames
		routeForgetRtTableNames()
	})

	own := "3:\tfrom all fwmark 0x237a0/0x27fff lookup 252"
	foreign := "32764:\tfrom all to 1.0.0.1 lookup 252"

	if refs := routeRuleRefsIn(map[string][]string{"252": {own}}, 252); len(refs) != 0 {
		t.Fatalf("a rule b4 left behind after an unclean stop must not make 252 look taken, got %v", refs)
	}
	if refs := routeRuleRefsIn(map[string][]string{"252": {own, foreign}}, 252); len(refs) != 1 {
		t.Fatalf("a rule somebody else added must still count, got %v", refs)
	}
}

func TestRouteRuleIsOwn(t *testing.T) {
	if !routeRuleIsOwn("3:\tfrom all fwmark 0x237a0/0x27fff lookup 252") {
		t.Fatalf("a tproxy mark must read as b4's own")
	}
	if routeRuleIsOwn("100:\tfrom all fwmark 0x10000000/0x10000000 lookup 9999") {
		t.Fatalf("the TUN reinject mark is outside the tproxy range")
	}
	if routeRuleIsOwn("32764:\tfrom all to 1.0.0.1 lookup vpn") {
		t.Fatalf("a rule with no fwmark cannot be b4's proxy rule")
	}
}

func TestRouteRuleIsOwnDemandsB4sExactSignature(t *testing.T) {
	own := []string{
		"3:\tfrom all fwmark 0x237a0/0x27fff lookup 252",
		"10137:\tfrom all fwmark 0x1f4/0x27fff lookup 137",
	}
	for _, l := range own {
		if !routeRuleIsOwn(l) {
			t.Errorf("b4 installs this rule itself: %q", l)
		}
	}
	foreign := []string{
		"32765:\tfrom all fwmark 0x1/0x1 lookup 300",
		"32765:\tfrom all fwmark 0x237a0/0x27fff lookup 300",
		"10137:\tfrom all fwmark 0x1f4/0x3f00 lookup 137",
		"10200:\tfrom all fwmark 0x1f4/0x27fff lookup 137",
		"1000:\tfrom all fwmark 0x100/0x3f00 lookup 1",
		"32764:\tfrom all to 1.0.0.1 lookup vpn",
		"10137:\tfrom all fwmark 0x1f4 lookup 137",
		"3:\tfrom all fwmark 0x237a0 lookup 252",
	}
	for _, l := range foreign {
		if routeRuleIsOwn(l) {
			t.Errorf("this rule does not carry b4's signature: %q", l)
		}
	}
}

func TestAnUnmaskedRuleIsNeverReadAsB4s(t *testing.T) {
	for _, line := range []string{
		"10229:\tfrom all fwmark 0x5179 lookup 229",
		"3:\tfrom all fwmark 0x20000 lookup 252",
	} {
		if routeRuleIsOwn(line) {
			t.Fatalf("every rule b4 installs carries the 0x27fff mask, for a routed set and for a proxy set "+
				"alike, so a rule without one belongs to another program. Reading it as b4's own drops it from "+
				"the list of foreign references to that table, and b4 then takes a table something else is "+
				"already using: %q", line)
		}
	}
}

func TestRoutePickProxyTableReportsExhaustionInsteadOfClaiming252(t *testing.T) {
	oldNames := routeRtTableNames
	named := map[int]string{}
	for _, c := range routeProxyTableCandidates() {
		named[c] = "taken"
	}
	routeRtTableNames = func() map[int]string { return named }
	routeForgetRtTableNames()
	t.Cleanup(func() {
		routeRtTableNames = oldNames
		routeForgetRtTableNames()
	})

	if !hasBinary("ip") {
		t.Skip("ip binary not present")
	}
	if got := routePickProxyTableExec(); got != 0 {
		t.Fatalf("every candidate is taken, so the picker must report exhaustion, got table %d", got)
	}
}

func TestAllocationRefusesWhenEveryTableIsTaken(t *testing.T) {
	prevTaken := routeTableForeignRoutes
	prevNames := routeRtTableNames
	routeTableForeignRoutes = func(int, string) bool { return true }
	routeRtTableNames = func() map[int]string { return map[int]string{} }
	t.Cleanup(func() {
		routeTableForeignRoutes = prevTaken
		routeRtTableNames = prevNames
		routeForgetRtTableNames()
		routeIfaceAuto = make(map[string]routeState)
	})
	routeForgetRtTableNames()
	routeIfaceAuto = make(map[string]routeState)

	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id = "exhausted"
	set.Routing.Enabled = true
	set.Routing.EgressInterface = "wg0"

	mark, table := routeResolveIDs(&cfg, &set)
	if mark != 0 || table != 0 {
		t.Fatalf("with every table between 100 and 249 already carrying another program's routes there is "+
			"nothing left to take. Handing back an occupied one puts b4's default route into a table something "+
			"else steers traffic with, and a zero mark makes the policy rule 'fwmark 0x0/0x27fff', which matches "+
			"nearly every packet on the box: got mark 0x%x table %d", mark, table)
	}
}
