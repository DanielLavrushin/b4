package tables

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const discoveryDSCPPostrouting = "Chain POSTROUTING (policy ACCEPT)\n" +
	"num  target     prot opt source               destination\n" +
	"1    B4_DSCP    all  --  0.0.0.0/0            0.0.0.0/0\n" +
	"2    B4         all  --  0.0.0.0/0            0.0.0.0/0\n"

func discoveryDSCPListings(postrouting string) map[string]string {
	listings := map[string]string{}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		listings[bin+" -w -t mangle -L POSTROUTING -n --line-numbers"] = postrouting
	}
	return listings
}

func discoveryRecordNftScripts(t *testing.T, fail error) *[]string {
	t.Helper()
	orig := runNftStdin
	t.Cleanup(func() { runNftStdin = orig })
	scripts := []string{}
	runNftStdin = func(script string) (string, error) {
		scripts = append(scripts, script)
		if fail != nil {
			return "Error: syntax error", fail
		}
		return "", nil
	}
	return &scripts
}

func discoveryRefuseDSCP(bins ...string) {
	recorded := run
	run = func(args ...string) (string, error) {
		out, err := recorded(args...)
		if slices.Contains(bins, args[0]) && slices.Contains(args, "DSCP") && slices.Contains(args, "-A") {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 2")
		}
		return out, err
	}
}

func discoveryProbe(value int) *config.DSCPProbe {
	return &config.DSCPProbe{Value: value, IPv4: true, IPv6: true}
}

func discoveryIpsetReady(t *testing.T, ipt6 bool) {
	t.Helper()
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: ipt6, "ipset": true})
	dscpIptResetRecords(t)
}

func discoveryRefuseSetMatch(bin string) {
	dscpIptProbeMu.Lock()
	dscpIptProbes[bin] = dscpIptProbe{err: errors.New(bin + " rejected the set match, which needs the xt_set kernel module"), at: dscpIptNow()}
	dscpIptProbeMu.Unlock()
}

func TestDiscoveryDSCPRunsAfterB4DSCPOnIptables(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})

	for _, tc := range []struct {
		name, listing, slot string
	}{
		{"below the B4_DSCP jump", discoveryDSCPPostrouting, "2"},
		{"no B4_DSCP chain yet", "Chain POSTROUTING (policy ACCEPT)\nnum  target     prot opt source               destination\n", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := discoveryRecordRun(t, discoveryDSCPListings(tc.listing))
			if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 25, v4: true, v6: true}); err != nil {
				t.Fatalf("apply: %v", err)
			}
			for _, bin := range []string{backendIPTables, backendIP6Tables} {
				jump := discoveryCallIndex(*calls, bin+" -w -t mangle -I POSTROUTING "+tc.slot+" -j B4_DISCOVERY_DSCP")
				if jump < 0 {
					t.Fatalf("%s: the jump to the run's DSCP chain must sit at slot %s of mangle POSTROUTING, after b4's own DSCP chain, which would overwrite the value: %v", bin, tc.slot, *calls)
				}
				for _, mark := range []string{"0x8001", "0x8002"} {
					rule := discoveryCallIndex(*calls, bin+" -w -t mangle -A B4_DISCOVERY_DSCP -m mark --mark "+mark+"/0xffffffff -j DSCP --set-dscp 25")
					if rule < 0 || rule > jump {
						t.Errorf("%s: packets with mark %s must get DSCP 25 before the jump goes live (rule at %d, jump at %d)", bin, mark, rule, jump)
					}
				}
			}
		})
	}
}

func TestDiscoveryDSCPFollowsTheDSCPInterfaceScopeOnIptables(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))

	if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 7, ifaces: []string{"wan0", "wg1"}, v4: true, v6: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, iface := range []string{"wan0", "wg1"} {
		for _, mark := range []string{"0x8001", "0x8002"} {
			want := "iptables -w -t mangle -A B4_DISCOVERY_DSCP -o " + iface + " -m mark --mark " + mark + "/0xffffffff -j DSCP --set-dscp 7"
			if discoveryCallIndex(*calls, want) < 0 {
				t.Errorf("the DSCP setting names its interfaces, and a set's own value is written only there; missing %q", want)
			}
		}
	}
	for _, c := range *calls {
		if strings.Contains(c, "-A B4_DISCOVERY_DSCP -m mark") {
			t.Errorf("with interfaces named, no rule may stamp every interface: %q", c)
		}
	}
}

func TestDiscoveryWithoutDSCPLeavesNoStampOnIptables(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))

	if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, c := range *calls {
		if strings.Contains(c, "-A B4_DISCOVERY_DSCP") || strings.Contains(c, "-I POSTROUTING") {
			t.Errorf("a run without a DSCP value must not touch the packets' DSCP field: %q", c)
		}
	}
	for _, want := range []string{
		"iptables -w -t mangle -D POSTROUTING -j B4_DISCOVERY_DSCP",
		"iptables -w -t mangle -X B4_DISCOVERY_DSCP",
	} {
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("a stamp an earlier run left behind (b4 killed mid-run) must go; missing %q", want)
		}
	}

	*calls = nil
	(&discoveryIptBackend{}).clear(0x8001, 0x8002)
	for _, want := range []string{
		"iptables -w -t mangle -D POSTROUTING -j B4_DISCOVERY_DSCP",
		"iptables -w -t mangle -F B4_DISCOVERY_DSCP",
		"iptables -w -t mangle -X B4_DISCOVERY_DSCP",
	} {
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("the end of a run must take its DSCP chain away; missing %q", want)
		}
	}
}

func TestDiscoveryDSCPRunsAfterB4DSCPOnNftables(t *testing.T) {
	calls := discoveryRecordRun(t, nil)
	scripts := discoveryRecordNftScripts(t, nil)

	if err := (&discoveryNftBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 25, ifaces: []string{"wan0"}, v4: true, v6: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(*scripts) != 1 {
		t.Fatalf("one nft script for the run's DSCP chain, got %d", len(*scripts))
	}
	script := (*scripts)[0]
	chain := fmt.Sprintf("add chain inet b4_mangle b4_discovery_dscp { type filter hook postrouting priority %d ; policy accept ; }", dscpNftPriority+1)
	if !strings.Contains(script, chain) {
		t.Errorf("the run's chain must hook postrouting after the %s table at priority %d, which would overwrite the value:\n%s", dscpNftTable, dscpNftPriority, script)
	}
	for _, mark := range []string{"0x8001", "0x8002"} {
		for _, family := range []string{"meta nfproto ipv4 ip dscp set 25", "meta nfproto ipv6 ip6 dscp set 25"} {
			want := `add rule inet b4_mangle b4_discovery_dscp oifname "wan0" meta mark ` + mark + " " + family
			if !strings.Contains(script, want) {
				t.Errorf("missing %q in:\n%s", want, script)
			}
		}
	}

	*calls = nil
	(&discoveryNftBackend{}).clear(0x8001, 0x8002)
	for _, want := range []string{
		"nft flush chain inet b4_mangle b4_discovery_dscp",
		"nft delete chain inet b4_mangle b4_discovery_dscp",
	} {
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("the end of a run must take its DSCP chain away; missing %q", want)
		}
	}
}

func TestDiscoveryWithoutDSCPLeavesNoStampOnNftables(t *testing.T) {
	calls := discoveryRecordRun(t, nil)
	scripts := discoveryRecordNftScripts(t, nil)

	if err := (&discoveryNftBackend{}).apply(0x8001, 0x8002, 541, 1, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(*scripts) != 0 {
		t.Errorf("a run without a DSCP value must not load a DSCP chain:\n%s", strings.Join(*scripts, "\n"))
	}
	if discoveryCallIndex(*calls, "nft delete chain inet b4_mangle b4_discovery_dscp") < 0 {
		t.Errorf("a stamp an earlier run left behind must go: %v", *calls)
	}
}

func TestDiscoveryDSCPStampsOnlyTheFamiliesTheSetsTrafficCarriesItIn(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	discoveryIpsetReady(t, true)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	stamped := func(bin string) bool {
		return discoveryCallIndex(*calls, bin+" -w -t mangle -I POSTROUTING 2 -j B4_DISCOVERY_DSCP") >= 0
	}
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	discoveryRefuseSetMatch(backendIP6Tables)
	lost := make(chan error, 4)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), func(err error) { lost <- err }); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !stamped(backendIPTables) || stamped(backendIP6Tables) {
		t.Errorf("ip6tables cannot match ipsets, so the set's IPv6 traffic leaves with the global value only; the run's IPv6 probes must too, and IPv4 keeps the set's value: %v", *calls)
	}
	if got, path := ActiveDiscoveryDSCP(); got == nil || *got != (config.DSCPProbe{Value: 25, IPv4: true}) || dscpPathText(path) != "IPv4 25, IPv6 -, on []" {
		t.Errorf("IPv4 probes still carry DSCP 25 and the run reports that its IPv6 probes do not, got %+v, %s", got, dscpPathText(path))
	}

	*calls = nil
	discoveryRefuseSetMatch(backendIPTables)
	rulesMu.Lock()
	applyDSCPLogged(&cfg, backendIPTables)
	reapplyDiscoverySteering()
	rulesMu.Unlock()
	if stamped(backendIPTables) || stamped(backendIP6Tables) {
		t.Errorf("once IPv4 also lost the set match, no live packet carries the set's value, and the run's probes must not either: %v", *calls)
	}
	if got, _ := ActiveDiscoveryDSCP(); got != nil {
		t.Errorf("a run that stamps nothing must not report a value, got %+v", got)
	}
	select {
	case err := <-lost:
		if !errors.Is(err, errDiscoveryDSCPRewritten) {
			t.Errorf("the run should learn that the DSCP rules were rewritten, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the rebuild moved the run's IPv4 probes from the set's path to the global one, and the run was not told, so it would mix results from two paths")
	}

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	dscpIptResetRecords(t)
	*calls = nil
	ipv4Only := &config.DSCPProbe{Value: 25, IPv4: true}
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, ipv4Only, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !stamped(backendIPTables) || stamped(backendIP6Tables) {
		t.Errorf("a set limited to IPv4 writes its value into IPv4 packets only: %v", *calls)
	}

	nft := &discoveryNftBackend{}
	dscpNftSets.Store(dscpNftSetsRejected)
	if got, err := discoveryStampFor(&cfg, discoveryProbe(25), nft); got != nil || err != nil {
		t.Errorf("nftables refused the per-set rules, so the set's traffic carries no set value and the probes must not either: %+v, %v", got, err)
	}
	dscpNftSets.Store(dscpNftSetsTaken)
	if got, err := discoveryStampFor(&cfg, discoveryProbe(25), nft); err != nil || got == nil || !got.v4 || !got.v6 {
		t.Errorf("with the per-set rules loaded both families take the value, got %+v, %v", got, err)
	}

	scripts := discoveryRecordNftScripts(t, nil)
	if err := nft.apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 25, v4: true}); err != nil {
		t.Fatalf("nft apply: %v", err)
	}
	if len(*scripts) != 1 || strings.Contains((*scripts)[0], "ip6 dscp") || !strings.Contains((*scripts)[0], "ip dscp set 25") {
		t.Errorf("on nftables an IPv4-only stamp must hold no IPv6 rule: %v", *scripts)
	}
}

func TestDiscoveryDSCPKnowsSetValuesAreOutOfReachWithNoDSCPRulesApplied(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false, "ipset": false})
	dscpIptResetRecords(t)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	if dscpApplied.Load() != nil {
		t.Fatal("precondition: with the global stamp off and no ipset, no DSCP state is kept")
	}
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, c := range *calls {
		if strings.Contains(c, "-A B4_DISCOVERY_DSCP") {
			t.Errorf("without the ipset command the set's traffic leaves unstamped, and the run's probes must too: %q", c)
		}
	}
	if got, _ := ActiveDiscoveryDSCP(); got != nil {
		t.Errorf("the run must not claim a value, got %+v", got)
	}

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	stubBinaryPresence(t, map[string]bool{"ipset": true})
	discoveryRefuseSetMatch(backendIPTables)
	*calls = nil
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, c := range *calls {
		if strings.Contains(c, "-A B4_DISCOVERY_DSCP") {
			t.Errorf("iptables refused the set match earlier, so the set's traffic leaves unstamped, and the run's probes must too: %q", c)
		}
	}
}

func TestDiscoverySetRunWaitsForPendingDSCPRules(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	discoveryIpsetReady(t, false)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	for _, backend := range []string{backendIPTables, backendNFTables} {
		dscpApplied.Store(&dscpState{cfg: &cfg, backend: backend, pending: true})
		if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
			t.Errorf("%s: while b4 is still applying its DSCP rules the set's traffic does not carry its value yet, so a run for the set must wait, got %v", backend, err)
		}
	}
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
		t.Errorf("the DSCP rules b4 is still applying decide the path of every probe, so a run for no set value waits too, got %v", err)
	}
	if len(*calls) != 0 || activeSteering != nil {
		t.Errorf("a refused run must leave the firewall alone: %v", *calls)
	}
	dscpApplied.Store(nil)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Errorf("with no DSCP rules at all a run for no set value starts, got %v", err)
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
}

func TestDiscoveryRunWaitsWhileOldDSCPObjectsAwaitCleanup(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	dscpStale.Store(&dscpState{cfg: &cfg, backend: backendNFTables})
	for _, probe := range []*config.DSCPProbe{nil, discoveryProbe(25)} {
		if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, probe, nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
			t.Errorf("a DSCP table b4 could not remove still writes its values until the monitor takes it away, so a run must wait (probe %+v), got %v", probe, err)
		}
	}

	dscpStale.Store(nil)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Fatalf("with the old objects gone the run starts, got %v", err)
	}
	dscpStale.Store(&dscpState{cfg: &cfg, backend: backendNFTables})
	rulesMu.Lock()
	retryStaleDSCP()
	rulesMu.Unlock()
	if !DiscoveryRunInvalidated() {
		t.Error("taking old DSCP objects away changes the path of the run's probes, so the run must count as invalid")
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
}

func TestDiscoveryRunWaitsWhileASavedChangeStillHasToTakeDSCPRulesAway(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	running := dscpSyncRunning.Load()
	t.Cleanup(func() {
		dscpSyncRunning.Store(running)
		dscpSyncWanted.Store(nil)
		dscpSyncSettled.Store(nil)
	})
	tagged := dscpPlanTestConfig(dscpPlanTestSet("a", 25, "10.1.2.0/24"))
	tagged.System.Tables.Engine = backendIPTables
	switchedOff := config.NewConfig()
	switchedOff.System.Tables.Engine = backendIPTables

	dscpApplied.Store(&dscpState{cfg: tagged, backend: backendIPTables})
	dscpSyncRunning.Store(true)
	dscpSyncSettled.Store(tagged)
	dscpSyncWanted.Store(&switchedOff)
	if err := ApplyDiscoverySteeringRules(&switchedOff, 0x8001, 0x8002, 541, 1, nil, nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
		t.Errorf("the set's DSCP was switched off, but its rules still write 25 into packets to its addresses until the worker takes them away, so a run for the set must wait, got %v", err)
	}

	dscpApplied.Store(nil)
	dscpSyncSettled.Store(&switchedOff)
	if err := ApplyDiscoverySteeringRules(&switchedOff, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Errorf("once the rules are gone the run starts, got %v", err)
	}
	ClearDiscoverySteeringRules(&switchedOff, 0x8001, 0x8002)

	plain := config.NewConfig()
	plain.System.Tables.Engine = backendIPTables
	other := config.NewConfig()
	dscpSyncSettled.Store(&other)
	dscpSyncWanted.Store(&plain)
	if err := ApplyDiscoverySteeringRules(&plain, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Errorf("with no DSCP rules before or after a save, a run for no set value does not wait for it, got %v", err)
	}
	ClearDiscoverySteeringRules(&plain, 0x8001, 0x8002)
}

func TestDiscoveryStampTakesTheInterfacesOfTheAppliedDSCPRules(t *testing.T) {
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	discoveryRecordRun(t, nil)
	start := config.NewConfig()
	start.System.Tables.DSCP.Interfaces = []string{"wan0"}
	applied := config.NewConfig()
	applied.System.Tables.DSCP.Interfaces = []string{"wan1"}

	dscpApplied.Store(&dscpState{cfg: &applied, backend: backendIPTables})
	stamp, err := discoveryStampFor(&start, discoveryProbe(25), &discoveryIptBackend{})
	if err != nil || stamp == nil || !slices.Equal(stamp.ifaces, []string{"wan1"}) {
		t.Errorf("the sets' values are written on the interfaces the applied rules name, so the run's must be too, got %+v, %v", stamp, err)
	}
}

func TestDiscoverySetRunStopsWhenTheDSCPRulesAreRewritten(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: true, Value: 7}
	expectLost := func(lost chan error, want bool, why string) {
		t.Helper()
		select {
		case err := <-lost:
			if !want {
				t.Errorf("%s: unexpected stop signal %v", why, err)
			} else if !errors.Is(err, errDiscoveryDSCPRewritten) {
				t.Errorf("%s: got %v", why, err)
			}
		case <-time.After(200 * time.Millisecond):
			if want {
				t.Errorf("%s: the run was not told", why)
			}
		}
	}

	lost := make(chan error, 4)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), func(err error) { lost <- err }); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ApplyDSCPOnly(&cfg); err != nil {
		t.Fatalf("ApplyDSCPOnly: %v", err)
	}
	expectLost(lost, true, "a DSCP-only save rewrote the rules the set's traffic follows, so the run for the set must stop")
	if err := ApplyDSCPOnly(&cfg); err != nil {
		t.Fatalf("ApplyDSCPOnly: %v", err)
	}
	expectLost(lost, false, "the run is told once")

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	plain := make(chan error, 4)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, func(err error) { plain <- err }); err != nil {
		t.Fatalf("apply: %v", err)
	}
	ClearDSCPOnly(&cfg)
	expectLost(plain, true, "the run's probes carried the global stamp that was just removed, so a run for no set value must stop too")
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	if err := ApplyDSCPOnly(&cfg); err != nil {
		t.Fatalf("ApplyDSCPOnly after the run: %v", err)
	}
	expectLost(lost, false, "after the run ended")
}

func TestDiscoverySetRunIsInvalidatedBeforeTheRulesChange(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: true, Value: 7}

	recorded := run
	var firstCommand []bool
	run = func(args ...string) (string, error) {
		firstCommand = append(firstCommand, DiscoveryRunInvalidated())
		return recorded(args...)
	}

	for _, op := range []struct {
		name string
		do   func() error
	}{
		{"a DSCP-only apply", func() error { return ApplyDSCPOnly(&cfg) }},
		{"a firewall rebuild", func() error { return AddRules(&cfg) }},
		{"a firewall teardown", func() error { return ClearRules(&cfg) }},
	} {
		ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
		if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
			t.Fatalf("%s: apply: %v", op.name, err)
		}
		if DiscoveryRunInvalidated() {
			t.Fatalf("%s: a fresh run starts valid", op.name)
		}
		firstCommand = nil
		_ = op.do()
		if len(firstCommand) == 0 || !firstCommand[0] {
			t.Errorf("%s: the run must count as invalid before the first rule changes, or a run that finishes meanwhile publishes results from the old path: %v", op.name, firstCommand)
		}
	}

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	if DiscoveryRunInvalidated() {
		t.Error("with no run there is nothing to invalidate")
	}
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	_ = ApplyDSCPOnly(&cfg)
	if !DiscoveryRunInvalidated() {
		t.Error("the run's probes pass the global DSCP stamp too, so a run for no set value is invalid once that stamp is rewritten")
	}

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	dscpApplied.Store(nil)
	dscpStale.Store(nil)
	plain := config.NewConfig()
	plain.System.Tables.Engine = backendIPTables
	if err := ApplyDiscoverySteeringRules(&plain, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	_ = AddRules(&plain)
	_ = ClearRules(&plain)
	if DiscoveryRunInvalidated() {
		t.Error("with no DSCP rules before or after, a firewall rebuild leaves the probes' path alone, so the run stays valid")
	}
}

func TestDiscoveryDSCPNeedsTheFamilysFirewallBinary(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	discoveryIpsetReady(t, false)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	ipv6Only := &config.DSCPProbe{Value: 25, IPv6: true}
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, ipv6Only, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := ActiveDiscoveryDSCP(); got != nil {
		t.Errorf("without ip6tables no IPv6 rule can be installed, so a run for an IPv6-only set must not report a value, got %+v", got)
	}
	for _, c := range *calls {
		if strings.Contains(c, "-A B4_DISCOVERY_DSCP") {
			t.Errorf("no family can carry the value here, yet a stamp rule went in: %q", c)
		}
	}
}

func TestDiscoverySteeringRefusesADSCPOutOfRange(t *testing.T) {
	appliedResetGlobals(t)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	calls := discoveryRecordRun(t, nil)

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	for _, bad := range []int{-1, config.MaxDSCPValue + 1} {
		if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(bad), nil); err == nil {
			t.Errorf("DSCP %d is not a DSCP value, the run must not start", bad)
		}
	}
	if len(*calls) != 0 || activeSteering != nil {
		t.Errorf("a refused value must leave the firewall alone: %v", *calls)
	}
}

func TestDiscoverySteeringIsTakenBackWhenItsStampFails(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	calls := discoveryRecordRun(t, nil)
	discoveryRefuseDSCP(backendIPTables)

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil)
	if err == nil || !strings.Contains(err.Error(), "DSCP 25") {
		t.Fatalf("a run whose probes cannot carry the set's DSCP value would test another path; it must not start, got %v", err)
	}
	if activeSteering != nil {
		t.Error("a failed start must not be put back by the next firewall rebuild")
	}
	for _, want := range []string{
		"iptables -w -t mangle -D OUTPUT -j B4_DISCOVERY",
		"iptables -w -t mangle -X B4_DISCOVERY_DSCP",
	} {
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("the half-applied steering must be taken back; missing %q", want)
		}
	}
}

func TestDiscoveryDSCPFailureLeavesTheOtherFamilysSteeringInPlace(t *testing.T) {
	t.Cleanup(kmodResetForTest)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
	calls := discoveryRecordRun(t, nil)
	discoveryRefuseDSCP(backendIPTables)

	if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 25, v4: true, v6: true}); err == nil {
		t.Fatal("the IPv4 DSCP rule was refused, so apply must report it")
	}
	for _, want := range []string{
		"ip6tables -w -t mangle -I OUTPUT 1 -j B4_DISCOVERY",
		"ip6tables -w -t mangle -I B4 1 -m mark --mark 0x8001/0xffffffff -j RETURN",
	} {
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("a refused DSCP rule must not cost IPv6 its steering, or a rebuild during a run hands IPv6 probes to the main queue; missing %q", want)
		}
	}
}

func TestDiscoveryRunIsToldWhenARebuildCannotPutItsStampBack(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, true)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	lost := make(chan error, 2)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), func(err error) { lost <- err }); err != nil {
		t.Fatalf("apply: %v", err)
	}

	*calls = nil
	discoveryRefuseDSCP(backendIPTables)
	rulesMu.Lock()
	reapplyDiscoverySteering()
	rulesMu.Unlock()

	select {
	case err := <-lost:
		if !strings.Contains(err.Error(), "DSCP 25") {
			t.Errorf("the run should learn why it has to stop, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a rebuild left the run's probes without the set's DSCP value and the run was never told, so it goes on testing another path")
	}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		want := bin + " -w -t mangle -I B4 1 -m mark --mark 0x8001/0xffffffff -j RETURN"
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("the run's probes must stay out of the main queue until the run stops; missing %q", want)
		}
	}
	if activeSteering == nil {
		t.Error("the steering stays registered until the run itself clears it")
	}
}

func TestDiscoveryDSCPComesBackAfterAFirewallRebuild(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	discoveryIpsetReady(t, false)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	cfg.System.Tables.DSCP.Interfaces = []string{"wan0"}
	lost := make(chan error, 1)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), func(err error) { lost <- err }); err != nil {
		t.Fatalf("apply: %v", err)
	}

	*calls = nil
	rulesMu.Lock()
	reapplyDiscoverySteering()
	rulesMu.Unlock()
	for _, want := range []string{
		"iptables -w -t mangle -A B4_DISCOVERY_DSCP -o wan0 -m mark --mark 0x8001/0xffffffff -j DSCP --set-dscp 25",
		"iptables -w -t mangle -I POSTROUTING 2 -j B4_DISCOVERY_DSCP",
	} {
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("a firewall rebuild during a run must put the run's DSCP rules back; missing %q", want)
		}
	}
	select {
	case err := <-lost:
		t.Errorf("the rules came back, yet the run was told they were lost: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	*calls = nil
	rulesMu.Lock()
	reapplyDiscoverySteering()
	rulesMu.Unlock()
	if len(*calls) != 0 {
		t.Errorf("after the run ended a rebuild put its rules back: %v", *calls)
	}
}

func TestDiscoverySetRunWaitsForADSCPSaveTheWorkerHasNotApplied(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	dscpSyncIsolate(t)
	dscpSyncTick = time.Hour
	discoveryIpsetReady(t, false)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	entered, release, settled := make(chan struct{}, 1), make(chan struct{}), make(chan struct{}, 1)
	dscpSyncPassFn = func(cfg *config.Config, periodic bool, stop <-chan struct{}) {
		entered <- struct{}{}
		<-release
		dscpSyncPass(cfg, periodic, stop)
		settled <- struct{}{}
	}
	saved := config.NewConfig()
	saved.System.Tables.Engine = backendIPTables

	StartDSCPSync()
	SyncDSCP(&saved)
	<-entered
	if err := ApplyDiscoverySteeringRules(&saved, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
		t.Errorf("b4 has saved a DSCP change it has not applied yet, so the set's traffic still carries the old value and a run for the set must wait, got %v", err)
	}
	if len(*calls) != 0 || activeSteering != nil {
		t.Errorf("a refused run must leave the firewall alone: %v", *calls)
	}
	if err := ApplyDiscoverySteeringRules(&saved, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Errorf("a run for no set value does not depend on the DSCP rules and starts, got %v", err)
	}
	ClearDiscoverySteeringRules(&saved, 0x8001, 0x8002)

	close(release)
	<-settled
	if err := ApplyDiscoverySteeringRules(&saved, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Errorf("once the worker has applied the saved change the run for the set starts, got %v", err)
	}
	ClearDiscoverySteeringRules(&saved, 0x8001, 0x8002)

	StopDSCPSync()
	later := config.NewConfig()
	SyncDSCP(&later)
	if err := ApplyDiscoverySteeringRules(&saved, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Errorf("with the worker stopped nothing applies a saved change, so a run must not wait for one, got %v", err)
	}
	ClearDiscoverySteeringRules(&saved, 0x8001, 0x8002)
}

func TestDiscoveryRunWithoutDSCPCountsAsInvalidOnceItsSteeringIsLost(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	discoveryRecordRun(t, nil)
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	lost := make(chan error, 1)
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, func(err error) { lost <- err }); err != nil {
		t.Fatalf("apply: %v", err)
	}
	recorded := run
	run = func(args ...string) (string, error) {
		if slices.Contains(args, "NFQUEUE") {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 2")
		}
		return recorded(args...)
	}
	rulesMu.Lock()
	reapplyDiscoverySteering()
	rulesMu.Unlock()

	if !DiscoveryRunInvalidated() {
		t.Error("a rebuild left the run's probes outside its queue, and the run must count as invalid before the stop signal lands, or a run that finishes meanwhile publishes results b4 did not test")
	}
	select {
	case <-lost:
	case <-time.After(5 * time.Second):
		t.Fatal("the run was never told that its steering is gone")
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	if DiscoveryRunInvalidated() {
		t.Error("with no run there is nothing to invalidate")
	}
}

func TestDiscoveryTriesTheSetMatchBeforeStampingAFamilyNoSetHasUsed(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	setMatch := ""
	stubIPSetProbe(t, nil)
	ipsetMatchProbe = func(*IPTablesManager, string) error {
		switch setMatch {
		case "rejected":
			return errors.New("iptables rejected the set match, which needs the xt_set kernel module and the iptables set extension (command [iptables -A B4_MODULE_TEST] failed: exit status 2 (Couldn't load match `set':No such file or directory))")
		case "busy":
			return errors.New("iptables rejected the set match, which needs the xt_set kernel module and the iptables set extension (command [iptables -A B4_MODULE_TEST] failed: exit status 4 (Another app is currently holding the xtables lock))")
		}
		return nil
	}
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	stamped := func() bool {
		return discoveryCallIndex(*calls, "iptables -w -t mangle -I POSTROUTING 2 -j B4_DISCOVERY_DSCP") >= 0
	}

	setMatch = "busy"
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
		t.Errorf("a probe that could not run says nothing about the kernel, so the run must wait, got %v", err)
	}

	setMatch = "rejected"
	*calls = nil
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if stamped() {
		t.Error("no set has used the set match yet and the kernel rejects it, so the set's traffic would leave without its value, and the run's probes must too")
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)

	dscpIptResetRecords(t)
	setMatch = ""
	*calls = nil
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !stamped() {
		t.Error("the kernel takes the set match, so the run's probes carry the set's value")
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
}

func TestDiscoveryFollowsAFamilysFallbackUntilItsRulesAreRestored(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	discoveryIpsetReady(t, false)
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))
	probed := 0
	stubIPSetProbe(t, nil)
	ipsetMatchProbe = func(*IPTablesManager, string) error {
		probed++
		return nil
	}
	dscpIptProbeMu.Lock()
	dscpIptProbes[backendIPTables] = dscpIptProbe{err: errors.New("iptables rejected the set match"), at: dscpIptNow().Add(-time.Hour)}
	dscpIptProbeMu.Unlock()
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if probed != 0 || discoveryCallIndex(*calls, "iptables -w -t mangle -I POSTROUTING 2 -j B4_DISCOVERY_DSCP") >= 0 {
		t.Errorf("the set's rules fell back to the global value an hour ago and nothing has put them back, so its traffic still leaves without its value and the run must follow that, not a new probe (probed %d times)", probed)
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)

	dscpIptProbeMu.Lock()
	dscpIptProbes[backendIPTables] = dscpIptProbe{at: dscpIptNow()}
	dscpIptProbeMu.Unlock()
	stubBinaryPresence(t, map[string]bool{"ipset": false})
	*calls = nil
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if discoveryCallIndex(*calls, "iptables -w -t mangle -I POSTROUTING 2 -j B4_DISCOVERY_DSCP") >= 0 {
		t.Error("the ipset command is gone, so the set's rules fall back although the last probe passed, and the run must follow the fallback")
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
}

func TestDiscoveryFollowsAFamilyThatRefusedTheDSCPTarget(t *testing.T) {
	appliedResetGlobals(t)
	h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
	h.mangle.rejectDSCP[backendIP6Tables] = true
	cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 25, "10.1.2.0/24"))
	cfg.System.Tables.Engine = backendIPTables
	_ = applyDSCPFor(cfg, backendIPTables)

	be := &discoveryIptBackend{}
	if ok, err := be.takesSetValues(cfg, true); ok || err != nil {
		t.Errorf("ip6tables refused the DSCP target, so the set's IPv6 traffic leaves without any value, and the run's IPv6 probes must too: %t, %v", ok, err)
	}
	if ok, err := be.takesSetValues(cfg, false); !ok || err != nil {
		t.Errorf("IPv4 took the set's rules, so its probes keep the set's value: %t, %v", ok, err)
	}

	h.mangle.rejectDSCP[backendIP6Tables] = false
	_ = applyDSCPFor(cfg, backendIPTables)
	if ok, err := be.takesSetValues(cfg, true); !ok || err != nil {
		t.Errorf("once ip6tables takes the DSCP rules again, the run's IPv6 probes carry the value again: %t, %v", ok, err)
	}
}

func TestDiscoveryDSCPStampNamesTheValueARunWouldCarryNow(t *testing.T) {
	appliedResetGlobals(t)
	h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
	h.mangle.rejectDSCP[backendIP6Tables] = true
	cfg := dscpIptTestConfig(7, true, []string{"wan0"}, dscpPlanTestSet("a", 25, "10.1.2.0/24"))
	cfg.System.Tables.Engine = backendIPTables
	_ = applyDSCPFor(cfg, backendIPTables)

	for _, tc := range []struct {
		name        string
		probe       *config.DSCPProbe
		stamp, path string
	}{
		{"a set without a value of its own carries the global value where ip6tables took it", nil, "<nil>", "IPv4 7, IPv6 -, on [wan0]"},
		{"a set limited to IPv6, which refused the DSCP target", &config.DSCPProbe{Value: 25, IPv6: true}, "<nil>", "IPv4 7, IPv6 -, on [wan0]"},
		{"a set over both families gets its value where ip6tables took the rules", discoveryProbe(25), "&{Value:25 IPv4:true IPv6:false}", "IPv4 25, IPv6 -, on [wan0]"},
	} {
		stamp, path, err := DiscoveryDSCPStamp(cfg, tc.probe)
		if err != nil || fmt.Sprintf("%+v", stamp) != tc.stamp || dscpPathText(path) != tc.path {
			t.Errorf("%s: got %+v, %s, %v; want %s, %s", tc.name, stamp, dscpPathText(path), err, tc.stamp, tc.path)
		}
	}

	dscpApplied.Store(&dscpState{cfg: cfg, backend: backendIPTables, pending: true})
	for _, probe := range []*config.DSCPProbe{nil, discoveryProbe(25)} {
		if _, _, err := DiscoveryDSCPStamp(cfg, probe); !errors.Is(err, ErrDiscoveryDSCPPending) {
			t.Errorf("while b4 is still applying its DSCP rules the value the traffic carries is unknown, with or without a value of the set's own (%+v), got %v", probe, err)
		}
	}
}

func TestDiscoveryDSCPStampWaitsForTheRulesOfASetThatJustDroppedItsValue(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	cfg := config.NewConfig()
	dscpStale.Store(&dscpState{cfg: &cfg, backend: backendIPTables})
	if _, _, err := DiscoveryDSCPStamp(&cfg, nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
		t.Errorf("the set's old DSCP rules still wait to be removed, so its traffic may still carry the old value, got %v", err)
	}
	dscpStale.Store(nil)
	if stamp, path, err := DiscoveryDSCPStamp(&cfg, nil); err != nil || stamp != nil || dscpPathText(path) != "IPv4 -, IPv6 -, on []" {
		t.Errorf("with no DSCP rules at all the traffic carries no value, got %+v, %s, %v", stamp, dscpPathText(path), err)
	}
}

func dscpPathText(p *config.DSCPPath) string {
	if p == nil {
		return "<nil>"
	}
	value := func(v *int) string {
		if v == nil {
			return "-"
		}
		return strconv.Itoa(*v)
	}
	return fmt.Sprintf("IPv4 %s, IPv6 %s, on %v", value(p.IPv4), value(p.IPv6), p.Interfaces)
}

func TestDiscoveryTriesTheNftablesSetObjectsBeforeStampingTheirValue(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	stubBinaryPresence(t, map[string]bool{"nft": true, backendIPTables: false, backendIP6Tables: false})
	discoveryRecordRun(t, nil)
	rulesMu.Lock()
	rulesAppliedBackend = ""
	rulesMu.Unlock()
	orig := runNftStdin
	t.Cleanup(func() { runNftStdin = orig })
	var scripts []string
	mode := ""
	runNftStdin = func(script string) (string, error) {
		scripts = append(scripts, script)
		if strings.Contains(script, dscpNftProbeTable) {
			switch mode {
			case "rejected":
				return "Error: Could not process rule: Operation not supported", errors.New("exit status 1")
			case "busy":
				return "", fmt.Errorf("command [nft -f -] gave up after 15s: %w", context.DeadlineExceeded)
			}
		}
		return "", nil
	}
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendNFTables
	stamped := func() bool {
		return slices.ContainsFunc(scripts, func(s string) bool {
			return strings.Contains(s, discoveryStampChainNFT) && strings.Contains(s, "dscp set 25")
		})
	}

	mode = "busy"
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); !errors.Is(err, ErrDiscoveryDSCPPending) {
		t.Errorf("a test table nftables did not answer for says nothing about the kernel, so the run must wait, got %v", err)
	}
	if dscpNftSets.Load() != dscpNftSetsUntested {
		t.Error("an unanswered test must leave the capability untested")
	}

	mode = "rejected"
	scripts = nil
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if stamped() || dscpNftSets.Load() != dscpNftSetsRejected {
		t.Errorf("no set has loaded the per-set objects yet and nftables rejects them, so the set's traffic would leave without its value, and the run's probes must too: %v", scripts)
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)

	dscpNftSets.Store(dscpNftSetsUntested)
	mode = ""
	scripts = nil
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, discoveryProbe(25), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !stamped() || dscpNftSets.Load() != dscpNftSetsTaken {
		t.Errorf("nftables takes the per-set objects, so the run's probes carry the set's value: %v", scripts)
	}
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
}

func TestDiscoveryReadsEveryWayIptablesSaysTheStampChainIsGone(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	discoveryRecordRun(t, nil)
	rulesMu.Lock()
	rulesAppliedBackend = ""
	rulesMu.Unlock()
	recorded := run
	deleted, listedGone := "", false
	run = func(args ...string) (string, error) {
		switch {
		case slices.Contains(args, "-X") && slices.Contains(args, discoveryStampChainIPT):
			return deleted, errors.New("exit status 1")
		case listedGone && slices.Contains(args, "-S") && slices.Contains(args, discoveryStampChainIPT):
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		return recorded(args...)
	}
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables

	for _, tc := range []struct {
		name, deleted string
		listedGone    bool
	}{
		{"an older iptables-nft", "iptables v1.8.4 (nf_tables):  CHAIN_DEL failed (No such file or directory): chain B4_DISCOVERY_DSCP", false},
		{"an even older iptables-nft", "iptables v1.8.2 (nf_tables):  CHAIN_USER_DEL failed (No such file or directory): chain B4_DISCOVERY_DSCP", false},
		{"an answer b4 does not know, with no such chain listed", "iptables v1.8.7 (nf_tables): an answer b4 does not know", true},
	} {
		deleted, listedGone = tc.deleted, tc.listedGone
		if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
			t.Errorf("%s: the chain is already gone, so the run must start, got %v", tc.name, err)
		}
		ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	}
}

func TestDiscoveryRunDoesNotStartOverAStampItCouldNotRemove(t *testing.T) {
	appliedResetGlobals(t)
	resetDSCPState(t)
	t.Cleanup(kmodResetForTest)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false, "nft": true})
	discoveryRecordRun(t, nil)
	rulesMu.Lock()
	rulesAppliedBackend = ""
	rulesMu.Unlock()
	recorded := run
	busy := false
	run = func(args ...string) (string, error) {
		switch {
		case slices.Contains(args, "-X") && slices.Contains(args, discoveryStampChainIPT):
			if busy {
				return "iptables v1.8.11 (nf_tables):  CHAIN_DEL failed (Device or resource busy): chain B4_DISCOVERY_DSCP", errors.New("exit status 4")
			}
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		case args[0] == "nft" && slices.Contains(args, "delete") && slices.Contains(args, discoveryStampChainNFT):
			if busy {
				return "Error: Could not process rule: Device or resource busy", errors.New("exit status 1")
			}
			return "Error: Could not process rule: No such file or directory", errors.New("exit status 1")
		}
		return recorded(args...)
	}

	for _, engine := range []string{backendIPTables, backendNFTables} {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = engine
		busy = true
		err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "earlier run's DSCP value") {
			t.Errorf("%s: an earlier run's stamp may still write its value into these marks, so the run must not start on top of it, got %v", engine, err)
		}
		if activeSteering != nil {
			t.Errorf("%s: a refused run must not stay registered", engine)
		}
		busy = false
		if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
			t.Errorf("%s: a stamp that is already gone is no reason to refuse, got %v", engine, err)
		}
		ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	}
}
