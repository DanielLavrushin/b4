package tables

import (
	"fmt"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func recordProxyRuleOrder(t *testing.T) *[]string {
	t.Helper()
	origRun, origLogged, origStdin := run, runLogged, runNftStdin
	origSysctl, origDelRule := writeSysctl, routeDelRuleLoop
	t.Cleanup(func() {
		run, runLogged, runNftStdin = origRun, origLogged, origStdin
		writeSysctl, routeDelRuleLoop = origSysctl, origDelRule
	})
	calls := []string{}
	run = refuseDeletes(func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	runLogged = func(op string, args ...string) bool {
		_, err := run(args...)
		return err == nil
	}
	runNftStdin = func(script string) (string, error) {
		calls = append(calls, "nft -f - "+strings.TrimSpace(script))
		return "", nil
	}
	writeSysctl = func(path, value string) {}
	routeDelRuleLoop = func(ipv6 bool, mark, table string) {}
	return &calls
}

func TestTheOutChainRepeatsTheClaimedBypassOnlyBelowACollectedGuard(t *testing.T) {
	claimed := map[string]string{
		backendNFTables: fmt.Sprintf("meta mark & 0x%x != 0x0 return", routeSetMarkMask),
		backendIPTables: fmt.Sprintf("-m mark ! --mark 0x0/0x%x -j RETURN", routeSetMarkMask),
	}
	for _, tc := range []struct {
		mode      string
		collected bool
	}{
		{config.RoutingModeProxy, false},
		{config.RoutingModeMTProtoWS, false},
		{config.RoutingModeProxy, true},
		{config.RoutingModeWSReuse, true},
	} {
		for _, be := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
			t.Run(fmt.Sprintf("%s/collected=%t/%s", tc.mode, tc.collected, be.name()), func(t *testing.T) {
				staticResetGlobals(t)
				localGuardReset(t)
				stubBinaries(t, backendIPTables)
				calls := recordProxyRuleOrder(t)

				cfg := config.NewConfig()
				cfg.Queue.IPv4Enabled = true
				cfg.Queue.IPv6Enabled = false
				set := orderTestSet("guard", tc.mode, nil)
				set.Targets.UseCollectedIPs = tc.collected
				st := proxyGuardState(0x239c9)
				st.mode = tc.mode
				st.collected = tc.collected

				if err := routeEnsureProxyRule(be, &cfg, set, st, nil); err != nil {
					t.Fatalf("routeEnsureProxyRule: %v", err)
				}

				out := chainRuleLines(*calls, st.chainOut)
				var sequence []string
				for _, line := range out {
					if be.name() == backendIPTables && !strings.HasPrefix(line, backendIPTables+" ") {
						continue
					}
					switch {
					case strings.HasSuffix(line, claimed[be.name()]):
						sequence = append(sequence, "bypass")
					case routeCollectedGuardLine(line) || strings.Contains(line, "@"+st.setV4+"c"):
						if len(sequence) == 0 || sequence[len(sequence)-1] != "guard" {
							sequence = append(sequence, "guard")
						}
					}
				}
				want := []string{"bypass"}
				if tc.collected {
					want = []string{"bypass", "guard", "bypass"}
				}
				if strings.Join(sequence, ",") != strings.Join(want, ",") {
					t.Errorf("%s holds %v, want %v:\n%s", st.chainOut, sequence, want, strings.Join(out, "\n"))
				}
			})
		}
	}
}

func TestCollectedOutGuardRunsBelowTheClaimedBypass(t *testing.T) {
	claimed := map[string]string{
		backendNFTables: fmt.Sprintf("meta mark & 0x%x != 0x0 return", routeSetMarkMask),
		backendIPTables: fmt.Sprintf("-m mark ! --mark 0x0/0x%x -j RETURN", routeSetMarkMask),
	}
	for _, mode := range []string{config.RoutingModeProxy, config.RoutingModeWSReuse} {
		for _, be := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
			t.Run(mode+"/"+be.name(), func(t *testing.T) {
				staticResetGlobals(t)
				localGuardReset(t)
				stubBinaries(t, backendIPTables)
				calls := recordProxyRuleOrder(t)

				cfg := config.NewConfig()
				cfg.Queue.IPv4Enabled = true
				cfg.Queue.IPv6Enabled = false
				set := orderTestSet("guard", mode, nil)
				set.Targets.UseCollectedIPs = true
				st := proxyGuardState(0x239c9)
				st.mode = mode
				st.collected = true

				if err := routeEnsureProxyRule(be, &cfg, set, st, nil); err != nil {
					t.Fatalf("routeEnsureProxyRule: %v", err)
				}

				out := chainRuleLines(*calls, st.chainOut)
				firstBypass, lastGuard, rateGuard := -1, -1, -1
				var bypasses []int
				for i, line := range out {
					if strings.HasSuffix(line, claimed[be.name()]) {
						bypasses = append(bypasses, i)
						if firstBypass < 0 {
							firstBypass = i
						}
					}
					if strings.Contains(line, "ct mark") || strings.Contains(line, "connmark") || strings.Contains(line, "CONNMARK") {
						lastGuard = i
					}
					if rateGuard < 0 && (strings.Contains(line, "limit rate over") || strings.Contains(line, "hashlimit")) {
						rateGuard = i
					}
				}
				if firstBypass < 0 || lastGuard < 0 || rateGuard < 0 {
					t.Fatalf("the out chain lacks the claimed bypass (%d), the collected guard (%d) or the rate guard (%d):\n%s", firstBypass, lastGuard, rateGuard, strings.Join(out, "\n"))
				}
				for i, line := range out[:firstBypass] {
					if strings.Contains(line, " mark set ") || strings.Contains(line, "-j CONNMARK") || strings.Contains(line, "-j MARK") {
						t.Errorf("rule %d of %s writes a mark above the claimed bypass, so a packet another owner already marked, such as a Discovery probe with flow mark 0x8002 under queue mark 0x8001, is taken over by this set and proxied: %q\n%s", i, st.chainOut, line, strings.Join(out, "\n"))
					}
				}
				between := false
				for _, i := range bypasses {
					if i > lastGuard && i < rateGuard {
						between = true
					}
				}
				if !between {
					t.Errorf("no claimed bypass sits between the collected guard and the rate guard, so the router's own SYN the guard just marked uses up the rate guard's budget:\n%s", strings.Join(out, "\n"))
				}
			})
		}
	}
}
