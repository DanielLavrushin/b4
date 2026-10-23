package tables

import (
	"errors"
	"fmt"
	"slices"
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
	dscpIptProbes[bin] = dscpIptProbe{err: errors.New(bin + " rejected the set match, which needs the xt_set kernel module")}
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
	if got := ActiveDiscoveryDSCP(); got == nil || *got != 25 {
		t.Errorf("IPv4 probes still carry DSCP 25, got %v", got)
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
	if got := ActiveDiscoveryDSCP(); got != nil {
		t.Errorf("a run that stamps nothing must not report DSCP %d", *got)
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
	dscpNftSetsRefused.Store(true)
	if got := discoveryStampFor(&cfg, discoveryProbe(25), nft); got != nil {
		t.Errorf("nftables refused the per-set rules, so the set's traffic carries no set value and the probes must not either: %+v", got)
	}
	dscpNftSetsRefused.Store(false)
	if got := discoveryStampFor(&cfg, discoveryProbe(25), nft); got == nil || !got.v4 || !got.v6 {
		t.Errorf("with the per-set rules loaded both families take the value, got %+v", got)
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
	if got := ActiveDiscoveryDSCP(); got != nil {
		t.Errorf("the run must not claim DSCP %d", *got)
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
	if len(*calls) != 0 || activeSteering != nil {
		t.Errorf("a refused run must leave the firewall alone: %v", *calls)
	}
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, nil, nil); err != nil {
		t.Errorf("a run for no set value does not depend on the DSCP rules and starts, got %v", err)
	}
}

func TestDiscoveryStampTakesTheInterfacesOfTheAppliedDSCPRules(t *testing.T) {
	resetDSCPState(t)
	discoveryIpsetReady(t, false)
	start := config.NewConfig()
	start.System.Tables.DSCP.Interfaces = []string{"wan0"}
	applied := config.NewConfig()
	applied.System.Tables.DSCP.Interfaces = []string{"wan1"}

	dscpApplied.Store(&dscpState{cfg: &applied, backend: backendIPTables})
	stamp := discoveryStampFor(&start, discoveryProbe(25), &discoveryIptBackend{})
	if stamp == nil || !slices.Equal(stamp.ifaces, []string{"wan1"}) {
		t.Errorf("the sets' values are written on the interfaces the applied rules name, so the run's must be too, got %+v", stamp)
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
	expectLost(plain, false, "a run for no set value keeps running through a DSCP change, as before")
	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	if err := ApplyDSCPOnly(&cfg); err != nil {
		t.Fatalf("ApplyDSCPOnly after the run: %v", err)
	}
	expectLost(lost, false, "after the run ended")
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
