package tables

import (
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestParseNftInputChainsLeavesBanListsAndMangleChainsAlone(t *testing.T) {
	out := `table inet fw4 {
	chain input {
		type filter hook input priority filter; policy drop;
	}
	chain mangle_input {
		type filter hook input priority mangle; policy accept;
	}
}
table ip crowdsec {
	chain crowdsec-chain-input {
		type filter hook input priority filter - 10; policy accept;
	}
}
table inet f2b-table {
	chain f2b-chain {
		type filter hook input priority -1; policy accept;
	}
}
table inet banIP {
	chain wan-input {
		type filter hook input priority -100; policy accept;
	}
}
table inet guard {
	chain early {
		type filter hook input priority -5; policy drop;
	}
}
table inet filter {
	chain input {
		type filter hook input priority filter; policy accept;
	}
}
table ip filter {
	chain input {
		type filter hook input priority 0; policy drop;
	}
	chain INPUT {
		type filter hook input priority filter; policy accept;
	}
}`
	var got []string
	for _, target := range parseNftInputChains(out, map[string]bool{"ip": true}) {
		got = append(got, target.name())
	}
	want := []string{"nft inet fw4 input", "nft inet guard early", "nft inet filter input", "nft ip filter input"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("an accept above a ban list's drop would exempt b4's ports from the ban, and a lowercase native chain in ip filter is not iptables-nft's:\n got %v\nwant %v", got, want)
	}
}

func TestExposeJumpGoesBelowBanListRulesOnIptables(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	f.xt["iptables"].input = []string{"f2b-sshd", "ACCEPT", "DROP match-set crowdsec-blacklists", "logdrop"}

	SyncExposure(mtprotoWildcard, nil, false)

	want := []string{"f2b-sshd", "ACCEPT", "DROP match-set crowdsec-blacklists", exposeChain, "logdrop"}
	if got := f.xt["iptables"].input; !reflect.DeepEqual(got, want) {
		t.Fatalf("the jump must sit below every source ban so a banned address stays banned on b4's ports:\n got %v\nwant %v", got, want)
	}
	if got := f.xt["ip6tables"].input; got[0] != exposeChain {
		t.Fatalf("without ban rules the jump goes to the top: %v", got)
	}
}

func TestExposeOwnedTableIsReportedWithTheFirewalldStepAndNotRetriedEveryTick(t *testing.T) {
	resetExposeState(t)
	f := newFakeExposeFW()
	f.addNftChain("inet", "firewalld", "filter_INPUT", `reject with icmpx admin-prohibited`)
	f.nftHooks["inet firewalld filter_INPUT"] = "type filter hook input priority filter + 10; policy accept;"
	f.nftOwned["inet firewalld"] = true
	f.install(t, map[string]bool{
		"iptables": false, "ip6tables": false, "iptables-legacy": false, "ip6tables-legacy": false,
		"iptables-nft": false, "ip6tables-nft": false, "nft": true,
	}, map[string]string{})
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	exposeNow = func() time.Time { return now }

	SyncExposure(mtprotoWildcard, nil, false)
	s := ExposureStatus()
	if len(s.Chains) != 0 || !strings.Contains(s.Error, "firewall-cmd --permanent --add-port=3258/tcp") || !strings.Contains(s.Error, "NftablesTableOwner=no") {
		t.Fatalf("an owned table must be reported with the step that opens the port, got %+v", s)
	}

	scripts := len(f.scripts)
	exposeCheck()
	exposeCheck()
	if len(f.scripts) != scripts {
		t.Fatalf("a table that refused the rule must not be written to on every tick, ran %d more scripts", len(f.scripts)-scripts)
	}

	now = now.Add(exposeOwnedRetry + time.Second)
	f.nftOwned["inet firewalld"] = false
	exposeCheck()
	if texts := f.nftTexts("inet firewalld filter_INPUT"); !strings.Contains(texts[0], "b4-expose:mtproto") || ExposureStatus().Error != "" {
		t.Fatalf("after the retry interval b4 tries again and succeeds once the table is no longer owned: %q %+v", texts, ExposureStatus())
	}
}

func TestExposeFailedRemovalIsReportedAndRetried(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)

	f.xtListErr["iptables"] = "Another app is currently holding the xtables lock. Stopped waiting after 15s."
	SyncExposure(nil, nil, false)
	if s := ExposureStatus(); !strings.Contains(s.Error, "iptables filter INPUT") {
		t.Fatalf("a removal that failed must show up instead of claiming the port is closed: %+v", s)
	}
	if !f.xt["iptables"].exists {
		t.Fatal("the fake must still hold the chain for this test to mean anything")
	}

	delete(f.xtListErr, "iptables")
	exposeCheck()
	if f.xt["iptables"].exists || strings.Contains(strings.Join(f.xt["iptables"].input, " "), exposeChain) {
		t.Fatalf("the next check must finish the removal: %+v", f.xt["iptables"])
	}
	if s := ExposureStatus(); s.Error != "" {
		t.Fatalf("once removed, the error must clear: %+v", s)
	}
}

func TestExposeStopsForGoodOnceClearedAtShutdown(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)
	ClearExposure()
	f.calls = nil

	SyncExposure(mtprotoWildcard, nil, false)
	exposeCheck()
	if len(f.calls) != 0 || f.xt["iptables"].exists {
		t.Fatalf("a save still in flight at shutdown must not reopen the port after the cleanup, ran:\n%s", strings.Join(f.calls, "\n"))
	}
}

func TestShrinkExposureOnlyEverRemoves(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	mtproto := config.ExposedPort{Service: config.ExposeMTProto, Port: 3258, V4: true, V6: true}
	socks := config.ExposedPort{Service: config.ExposeSocks5, Port: 1080, V4: true, V6: true}
	SyncExposure([]config.ExposedPort{mtproto, socks}, nil, false)

	moved := config.ExposedPort{Service: config.ExposeSocks5, Port: 1081, V4: true, V6: true}
	ShrinkExposure([]config.ExposedPort{mtproto, moved})

	if !reflect.DeepEqual(f.xt["iptables"].rules, []string{"-p tcp --dport 3258 -j ACCEPT"}) {
		t.Fatalf("the early step of a save closes what the new configuration drops and never opens anything: %v", f.xt["iptables"].rules)
	}
}

func TestExposeWatchFollowsThePlanner(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)

	var planned atomic.Bool
	stop := StartExposureWatch(time.Hour, func() {
		planned.Store(true)
		SyncExposure(nil, []config.ExposeBlock{{Service: config.ExposeMTProto, Reason: config.ExposeBlockedNotListening}}, false)
	})
	defer stop()
	KickExposure()

	deadline := time.Now().Add(2 * time.Second)
	for {
		exposeMu.Lock()
		gone := !f.xt["iptables"].exists
		exposeMu.Unlock()
		if planned.Load() && gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("when the listener stops, the watch must take the port's rule away")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s := ExposureStatus(); len(s.Blocked) != 1 || s.Blocked[0].Reason != config.ExposeBlockedNotListening {
		t.Fatalf("status must say why the port is closed: %+v", s)
	}
}

func TestXtListingCanDrop(t *testing.T) {
	empty := "Chain INPUT (policy ACCEPT)\ntarget     prot opt source               destination\n"
	if xtListingCanDrop(empty) {
		t.Fatal("an empty accept-all INPUT drops nothing; listing it through iptables-nft must not make b4 create the table")
	}
	if !xtListingCanDrop("Chain INPUT (policy DROP)\ntarget     prot opt source               destination\n") {
		t.Fatal("a DROP policy drops")
	}
	if !xtListingCanDrop(empty + "ufw-before-input  all  --  0.0.0.0/0            0.0.0.0/0\n") {
		t.Fatal("any rule may drop")
	}
}

func TestExposeKeepsASingleJumpWhenINPUTCannotBeListed(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	f.inputErr["iptables"] = "iptables v1.8.11 (nf_tables): chain `INPUT' in table `filter' is incompatible, use 'nft' tool."

	SyncExposure(mtprotoWildcard, nil, false)
	f.calls = nil
	for i := 0; i < 3; i++ {
		exposeCheck()
	}

	jumps := 0
	for _, j := range f.xt["iptables"].input {
		if j == exposeChain {
			jumps++
		}
	}
	if jumps != 1 {
		t.Fatalf("an unlistable INPUT must not collect a new jump on every check, found %d: %v", jumps, f.xt["iptables"].input)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "iptables -w -t filter -I INPUT") {
			t.Fatalf("with the rules in place the check must not insert again: %s", c)
		}
	}
}

func TestExposeMovesTheJumpBelowABanListAddedLater(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)

	f.xt["iptables"].input = append(f.xt["iptables"].input, "f2b-b4")
	exposeCheck()

	input := f.xt["iptables"].input
	if input[len(input)-1] != exposeChain || strings.Count(strings.Join(input, " "), exposeChain) != 1 {
		t.Fatalf("a ban chain added below b4's jump must end up above it: %v", input)
	}
}

func TestExposeRetriesARemovalFromAStackThatNoLongerCarriesAPort(t *testing.T) {
	resetExposeState(t)
	f := newLegacyRouter(t)
	SyncExposure(mtprotoWildcard, nil, false)

	f.xtListErr["ip6tables"] = "Another app is currently holding the xtables lock. Stopped waiting after 15s."
	SyncExposure([]config.ExposedPort{{Service: config.ExposeMTProto, Port: 3258, Address: "203.0.113.5", V4: true}}, nil, false)
	if s := ExposureStatus(); !strings.Contains(s.Error, "ip6tables filter INPUT") {
		t.Fatalf("the failed IPv6 removal must be reported: %+v", s)
	}

	exposeCheck()
	if s := ExposureStatus(); !strings.Contains(s.Error, "ip6tables filter INPUT") {
		t.Fatalf("a check must not drop the pending removal from the status while it still fails: %+v", s)
	}

	delete(f.xtListErr, "ip6tables")
	exposeCheck()
	if f.xt["ip6tables"].exists || strings.Contains(strings.Join(f.xt["ip6tables"].input, " "), exposeChain) {
		t.Fatalf("the next check must finish the IPv6 removal: %+v", f.xt["ip6tables"])
	}
	if s := ExposureStatus(); s.Error != "" {
		t.Fatalf("once removed the error must clear: %+v", s)
	}
}

func TestXtListingCanDropIgnoresTheLegacyTablesWarning(t *testing.T) {
	empty := "Chain INPUT (policy ACCEPT)\ntarget     prot opt source               destination\n"
	for _, out := range []string{
		empty + "# Warning: iptables-legacy tables present, use iptables-legacy to see them\n",
		"# Warning: iptables-legacy tables present, use iptables-legacy to see them\n" + empty,
		empty + "# Warning: ip6tables-legacy tables present, use ip6tables-legacy to see them\n",
	} {
		if xtListingCanDrop(out) {
			t.Fatalf("a warning printed next to an empty accept-all INPUT is not a rule:\n%s", out)
		}
	}
}
