package tables

import (
	"errors"
	"github.com/daniellavrushin/b4/config"
	"strings"
	"testing"
)

func TestCollectedGuardKeepsEstablishedOwnersBothBackends(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	for _, backend := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
		t.Run(backend.name(), func(t *testing.T) {
			var calls []string
			run = func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil }
			cfg := familyTestConfig(true, true)
			set := staticTestSet(config.RoutingModeProxy, nil)
			set.Targets.UseCollectedIPs = true
			state := routeState{setID: set.Id, mode: config.RoutingModeProxy, mark: 0x7133, ipv4: true, ipv6: true, setV4: "b4r_collect_v4", setV6: "b4r_collect_v6", chainPre: "b4r_collect_pre"}
			if err := routeCollectedGuard(backend, cfg, set, state, state.chainPre, []string{"br-test"}); err != nil {
				t.Fatal(err)
			}
			all := strings.Join(calls, "\n")
			if len(calls) != 6 || !strings.Contains(all, "br-test") {
				t.Fatalf("family/source guards missing: %s", all)
			}
			if backend.name() == backendNFTables {
				if !strings.Contains(all, "tcp flags & (syn|ack) == syn ct state new") || !strings.Contains(all, "ct state != new ct mark & 0x27fff != 0x7133 counter return") {
					t.Fatal(all)
				}
			} else {
				if !strings.Contains(all, "--syn -m conntrack --ctstate NEW") || !strings.Contains(all, "! --ctstate NEW -m connmark ! --mark 0x7133/0x27fff -j RETURN") {
					t.Fatal(all)
				}
			}
			run = func(...string) (string, error) { return "", errors.New("kernel refused guard") }
			if err := routeCollectedGuard(backend, cfg, set, state, state.chainPre, nil); err == nil {
				t.Fatal("guard failure was ignored")
			}
		})
	}
}

func TestCollectedKernelUnionAndEmpty(t *testing.T) {
	cfg := familyTestConfig(true, true)
	set := staticTestSet(config.RoutingModeProxy, []string{"8.8.8.0/24"})
	set.Targets.UseCollectedIPs = true
	cfg.Sets = []*config.SetConfig{set}
	view := cfg.WithCollectedIPs([]string{"1.1.1.1", "2606:4700:4700::1111"})
	v4, v6 := routeCollectEntries(view.Sets[0])
	if len(v4) != 2 || len(v6) != 1 {
		t.Fatalf("kernel union lost targets: %v %v", v4, v6)
	}
	set.Targets.IpsToMatch = nil
	set.Targets.IPs = nil
	v4, v6 = routeCollectEntries(cfg.WithCollectedIPs(nil).Sets[0])
	if len(v4)+len(v6) != 0 {
		t.Fatal("empty pool expanded kernel membership")
	}
}

func TestCollectedGuardMonitorDetectsMissingProtection(t *testing.T) {
	staticResetGlobals(t)
	cfg := familyTestConfig(true, false)
	set := staticTestSet(config.RoutingModeInterface, nil)
	set.Targets.UseCollectedIPs = true
	st := routeState{collected: true, set: set, setID: set.Id, mode: config.RoutingModeInterface, mark: 0x7133, ipv4: true, setV4: "b4r_collect_v4", chainPre: "b4r_collect_pre", chainOut: "b4r_collect_out"}
	routeRuleCache = map[string]routeState{set.Id: st}
	for _, be := range []routeBackend{&routeIptBackend{}, &routeNftBackend{}} {
		run = func(args ...string) (string, error) { return "ip daddr @b4r_collect_v4c ct state != new return", nil }
		if !routeCollectedGuardsPresent(be, cfg) {
			t.Fatalf("installed guard missed on %s", be.name())
		}
		run = func(args ...string) (string, error) {
			for _, arg := range args {
				if arg == "-C" {
					return "", errors.New("missing guard")
				}
			}
			return "chain without a collected guard", nil
		}
		if routeCollectedGuardsPresent(be, cfg) {
			t.Fatalf("lost guard accepted on %s", be.name())
		}
	}
}

func TestCollectedOrdinaryConsumerNeedsNoRoutingState(t *testing.T) {
	staticResetGlobals(t)
	cfg := familyTestConfig(true, false)
	set := staticTestSet(config.RoutingModeProxy, nil)
	set.Routing.Enabled = false
	set.Targets.UseCollectedIPs = true
	cfg.Sets = []*config.SetConfig{set}
	if err := CollectedTargetsReady(cfg.WithCollectedIPs([]string{"1.1.1.1"})); err != nil {
		t.Fatal(err)
	}
}
