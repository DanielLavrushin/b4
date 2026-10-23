package tables

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

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
			if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 25}); err != nil {
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

	if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 7, ifaces: []string{"wan0", "wg1"}}); err != nil {
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

	if err := (&discoveryNftBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 25, ifaces: []string{"wan0"}}); err != nil {
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

func TestDiscoverySteeringRefusesADSCPOutOfRange(t *testing.T) {
	appliedResetGlobals(t)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	calls := discoveryRecordRun(t, nil)

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	for _, bad := range []int{-1, config.MaxDSCPValue + 1} {
		if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, &bad); err == nil {
			t.Errorf("DSCP %d is not a DSCP value, the run must not start", bad)
		}
	}
	if len(*calls) != 0 || activeSteering != nil {
		t.Errorf("a refused value must leave the firewall alone: %v", *calls)
	}
}

func TestDiscoverySteeringIsTakenBackWhenItsStampFails(t *testing.T) {
	appliedResetGlobals(t)
	t.Cleanup(kmodResetForTest)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	calls := discoveryRecordRun(t, nil)
	recorded := run
	run = func(args ...string) (string, error) {
		out, err := recorded(args...)
		if slices.Contains(args, "DSCP") && slices.Contains(args, "-A") {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 2")
		}
		return out, err
	}

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	value := 25
	err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, &value)
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
	recorded := run
	run = func(args ...string) (string, error) {
		out, err := recorded(args...)
		if args[0] == backendIPTables && slices.Contains(args, "DSCP") && slices.Contains(args, "-A") {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 2")
		}
		return out, err
	}

	if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1, &discoveryStamp{value: 25}); err == nil {
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

func TestDiscoveryDSCPComesBackAfterAFirewallRebuild(t *testing.T) {
	appliedResetGlobals(t)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	calls := discoveryRecordRun(t, discoveryDSCPListings(discoveryDSCPPostrouting))

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	cfg.System.Tables.DSCP.Interfaces = []string{"wan0"}
	value := 25
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1, &value); err != nil {
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

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	*calls = nil
	rulesMu.Lock()
	reapplyDiscoverySteering()
	rulesMu.Unlock()
	if len(*calls) != 0 {
		t.Errorf("after the run ended a rebuild put its rules back: %v", *calls)
	}
}
