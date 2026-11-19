package tables

import (
	"errors"
	"github.com/daniellavrushin/b4/config"
	"slices"
	"strings"
	"testing"
)

func TestCollectedGuardKeepsEstablishedOwnersBothBackends(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	for _, backend := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
		t.Run(backend.name(), func(t *testing.T) {
			var calls []string
			run = refuseDeletes(func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil })
			cfg := familyTestConfig(true, true)
			set := staticTestSet(config.RoutingModeProxy, nil)
			set.Targets.UseCollectedIPs = true
			state := routeState{setID: set.Id, mode: config.RoutingModeProxy, mark: 0x7133, ipv4: true, ipv6: true, setV4: "b4r_collect_v4", setV6: "b4r_collect_v6", chainPre: "b4r_collect_pre"}
			if err := routeCollectedGuard(backend, cfg, set, state, state.chainPre, []string{"br-test"}); err != nil {
				t.Fatal(err)
			}
			all := strings.Join(calls, "\n")
			want := 6
			if backend.name() == backendNFTables {
				want = 5
			}
			if len(calls) != want || !strings.Contains(all, "br-test") {
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

func TestCollectedReuseGuardScopesPort443BeforeProtocolOptions(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	var calls []string
	run = func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil }
	cfg := familyTestConfig(true, false)
	set := staticTestSet(config.RoutingModeWSReuse, nil)
	set.Targets.UseCollectedIPs = true
	st := routeState{mode: config.RoutingModeWSReuse, mark: 0x7133, ipv4: true, setV4: "b4r_collect_v4", chainPre: "b4r_collect_pre"}
	if err := routeCollectedGuard(&routeIptBackend{}, cfg, set, st, st.chainPre, nil); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		port := strings.Index(call, "--dport 443")
		proto := strings.Index(call, "-p tcp")
		if port < 0 || proto < 0 || proto > port || strings.Count(call, "-p tcp") != 1 {
			t.Fatalf("unscoped or invalid reuse rule: %s", call)
		}
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

type collectedGuardBackend struct {
	*mockRouteBackend
	ensured []string
}

func (b *collectedGuardBackend) ensureIPSet(name string, v6 bool) error {
	b.ensured = append(b.ensured, name)
	return b.mockRouteBackend.ensureIPSet(name, v6)
}

func collectedGuardTestConfig(mode string, collect bool) *config.Config {
	cfg := familyTestConfig(true, false)
	set := staticTestSet(mode, []string{"203.0.113.9"})
	set.Targets.UseCollectedIPs = collect
	cfg.Sets = []*config.SetConfig{set}
	return cfg
}

func collectedGuardSwitched(cfg *config.Config, collect bool) *config.Config {
	next := *cfg
	set := *cfg.Sets[0]
	set.Targets.UseCollectedIPs = collect
	next.Sets = []*config.SetConfig{&set}
	return &next
}

func collectedGuardPrepare(t *testing.T, be routeBackend, cfg *config.Config) routeState {
	t.Helper()
	st := buildRouteState(cfg, cfg.Sets[0])
	if err := routePrepareCollected(be, cfg, cfg.Sets[0], st); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return st
}

func collectedGuardLastAdd(calls []staticCall, set string) []string {
	var last []string
	for _, c := range calls {
		if c.set == set {
			last = c.ips
		}
	}
	return last
}

func TestCollectedGuardSetDropsARemovedAddress(t *testing.T) {
	staticResetGlobals(t)
	run = refuseDeletes(run)
	cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
	var added, deleted []staticCall
	be := staticSyncBackend(&added, &deleted)

	before := cfg.WithCollectedIPs([]string{"198.51.100.1", "198.51.100.2"})
	after := cfg.WithCollectedIPs([]string{"198.51.100.2"})
	if !routeStateEqual(buildRouteState(before, before.Sets[0]), buildRouteState(after, after.Sets[0])) {
		t.Fatal("a pool change alone rebuilds the set, so the sync never reaches the cache-hit path this test is about")
	}
	collectedGuardPrepare(t, be, before)
	added, deleted = nil, nil
	st := collectedGuardPrepare(t, be, after)
	guard := st.setV4 + "c"

	if len(deleted) != 1 || deleted[0].set != guard || !slices.Equal(deleted[0].ips, []string{"198.51.100.1"}) {
		t.Errorf("removing 198.51.100.1 from the pool deleted %+v, want exactly that address from %s; a stale guard entry keeps marking new connections to it for the proxy that no longer takes them", deleted, guard)
	}
	if got := collectedGuardLastAdd(added, guard); !slices.Equal(got, []string{"198.51.100.2"}) {
		t.Errorf("the last add to %s was %v, want the whole remaining pool [198.51.100.2]", guard, got)
	}
}

func TestCollectedGuardSetIsFlushedWhenTheSetStopsUsingCollectedIPs(t *testing.T) {
	staticResetGlobals(t)
	run = refuseDeletes(run)
	cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
	var added, deleted []staticCall
	be := staticSyncBackend(&added, &deleted)

	st := collectedGuardPrepare(t, be, cfg.WithCollectedIPs([]string{"198.51.100.1"}))
	guard := st.setV4 + "c"
	be.setOps = nil
	collectedGuardPrepare(t, be, collectedGuardSwitched(cfg, false))
	if !slices.Contains(be.setOps, "flush "+guard) {
		t.Errorf("turning collected IPs off ran %q, want %s flushed so an address collected before cannot be marked once the switch is back on", be.setOps, guard)
	}

	added, deleted = nil, nil
	collectedGuardPrepare(t, be, cfg.WithCollectedIPs([]string{"198.51.100.2"}))
	for _, c := range deleted {
		if c.set == guard {
			t.Errorf("turning collected IPs back on deleted %v from %s, which the switch-off already emptied", c.ips, guard)
		}
	}
	if got := collectedGuardLastAdd(added, guard); !slices.Equal(got, []string{"198.51.100.2"}) {
		t.Errorf("turning collected IPs back on added %v to %s, want [198.51.100.2]", got, guard)
	}
}

func TestCollectedGuardSetARoutingSetWithoutCollectedIPsTouchesNoGuardSet(t *testing.T) {
	for _, mode := range []string{config.RoutingModeProxy, config.RoutingModeInterface, config.RoutingModeBlock} {
		t.Run(mode, func(t *testing.T) {
			staticResetGlobals(t)
			run = refuseDeletes(run)
			cfg := collectedGuardTestConfig(mode, false)
			cfg.Queue.IPv6Enabled = true
			be := &collectedGuardBackend{mockRouteBackend: &mockRouteBackend{}}
			collectedGuardPrepare(t, be, cfg)
			collectedGuardPrepare(t, be, cfg.WithCollectedIPs([]string{"198.51.100.1"}))
			for _, op := range append(append([]string{}, be.setOps...), be.ensured...) {
				if strings.HasSuffix(op, "c") {
					t.Errorf("a set in %s mode that never used collected IPs ran %q; a user without the feature must not get a guard set or a command for one", mode, op)
				}
			}
		})
	}
}

func TestCollectedGuardRestoresHeldConnectionsWithoutTheGuardSet(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	for _, backend := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
		for _, mode := range []string{config.RoutingModeProxy, config.RoutingModeWSReuse} {
			t.Run(backend.name()+"/"+mode, func(t *testing.T) {
				var calls []string
				run = refuseDeletes(func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil })
				cfg := familyTestConfig(true, true)
				set := staticTestSet(mode, nil)
				set.Targets.UseCollectedIPs = true
				st := routeState{setID: set.Id, mode: mode, mark: 0x7133, ipv4: true, ipv6: true, setV4: "b4r_collect_v4", setV6: "b4r_collect_v6", chainPre: "b4r_collect_pre"}
				if err := routeCollectedGuard(backend, cfg, set, st, st.chainPre, []string{"br-test"}); err != nil {
					t.Fatal(err)
				}
				marker, want := " meta mark set ", 1
				if backend.name() != backendNFTables {
					marker, want = " -j MARK ", 2
				}
				var restores []string
				for _, call := range calls {
					if strings.Contains(call, marker) {
						restores = append(restores, call)
					}
				}
				if len(restores) != want {
					t.Fatalf("got %d connection-mark restore rule(s), want %d (once per rule family):\n%s", len(restores), want, strings.Join(calls, "\n"))
				}
				for _, line := range restores {
					if strings.Contains(line, "@b4r_collect_v") || strings.Contains(line, "--match-set") {
						t.Errorf("the restore matches the guard set, so a held connection to an address that leaves the pool loses its mark and breaks without early demux: %s", line)
					}
					if !strings.Contains(line, "br-test") {
						t.Errorf("the restore lost the source-interface match: %s", line)
					}
					if mode == config.RoutingModeWSReuse && !strings.Contains(line, "443") {
						t.Errorf("the reuse restore lost its port 443 scope: %s", line)
					}
				}
			})
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
