package tables

import (
	"errors"
	"fmt"
	"github.com/daniellavrushin/b4/config"
	"net"
	"slices"
	"strings"
	"testing"
)

func TestCollectedGuardKeepsEstablishedOwnersBothBackends(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	stubBinaries(t, backendIPTables, backendIP6Tables)
	for _, backend := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
		t.Run(backend.name(), func(t *testing.T) {
			var calls []string
			run = refuseDeletes(func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil })
			cfg := familyTestConfig(true, true)
			set := staticTestSet(config.RoutingModeProxy, nil)
			set.Targets.UseCollectedIPs = true
			state := routeState{collected: true, setID: set.Id, mode: config.RoutingModeProxy, mark: 0x7133, ipv4: true, ipv6: true, setV4: "b4r_collect_v4", setV6: "b4r_collect_v6", chainPre: "b4r_collect_pre", sources: []string{"br-test"}}
			if err := routeCollectedGuard(backend, cfg, set, state, state.chainPre); err != nil {
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
			if err := routeCollectedGuard(backend, cfg, set, state, state.chainPre); err == nil {
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
	stubBinaries(t, backendIPTables)
	var calls []string
	run = func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil }
	cfg := familyTestConfig(true, false)
	set := staticTestSet(config.RoutingModeWSReuse, nil)
	set.Targets.UseCollectedIPs = true
	st := routeState{collected: true, mode: config.RoutingModeWSReuse, mark: 0x7133, ipv4: true, setV4: "b4r_collect_v4", chainPre: "b4r_collect_pre"}
	if err := routeCollectedGuard(&routeIptBackend{}, cfg, set, st, st.chainPre); err != nil {
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
	stubBinaries(t, backendIPTables, "ipset")
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

func TestCollectedGuardSetRetriesAFailedDelete(t *testing.T) {
	staticResetGlobals(t)
	stubBinaries(t, backendIPTables, "ipset")
	run = refuseDeletes(run)
	runLogged = func(op string, args ...string) bool {
		_, err := run(args...)
		return err == nil
	}
	origStdin := runStdin
	t.Cleanup(func() { runStdin = origStdin })
	refused := false
	var deletes []string
	runStdin = func(stdin string, args ...string) error {
		for _, line := range strings.Split(strings.TrimSpace(stdin), "\n") {
			if strings.HasPrefix(line, "del ") {
				deletes = append(deletes, line)
			}
		}
		if refused && strings.Contains(stdin, "del ") {
			return errors.New("ipset v7.19: Kernel error received: Resource busy")
		}
		return nil
	}
	cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
	be := &routeIptBackend{}
	st := collectedGuardPrepare(t, be, cfg.WithCollectedIPs([]string{"198.51.100.1", "198.51.100.2"}))
	guard := st.setV4 + "c"
	after := cfg.WithCollectedIPs([]string{"198.51.100.2"})

	refused = true
	if err := routePrepareCollected(be, after, after.Sets[0], st); err == nil {
		t.Errorf("the kernel refused to delete 198.51.100.1 from %s, yet the sync reported the guard set as current, so no retry is queued", guard)
	}
	refused = false
	deletes = nil
	if err := routePrepareCollected(be, after, after.Sets[0], st); err != nil {
		t.Fatalf("the next sync failed with deletes allowed again: %v", err)
	}
	if want := "del " + guard + " 198.51.100.1"; !slices.Contains(deletes, want) {
		t.Errorf("the next sync ran deletes %q, want %q: a removal the kernel refused once is never tried again, so the address stays in the guard set and new connections to it are marked for a proxy that no longer takes them", deletes, want)
	}
	deletes = nil
	if err := routePrepareCollected(be, after, after.Sets[0], st); err != nil || len(deletes) != 0 {
		t.Errorf("once the delete went through, a further sync ran deletes %q (err %v), want none", deletes, err)
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
	stubBinaries(t, backendIPTables, backendIP6Tables)
	for _, backend := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
		for _, mode := range []string{config.RoutingModeProxy, config.RoutingModeWSReuse} {
			t.Run(backend.name()+"/"+mode, func(t *testing.T) {
				var calls []string
				run = refuseDeletes(func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil })
				cfg := familyTestConfig(true, true)
				set := staticTestSet(mode, nil)
				set.Targets.UseCollectedIPs = true
				st := routeState{collected: true, setID: set.Id, mode: mode, mark: 0x7133, ipv4: true, ipv6: true, setV4: "b4r_collect_v4", setV6: "b4r_collect_v6", chainPre: "b4r_collect_pre", sources: []string{"br-test"}}
				if err := routeCollectedGuard(backend, cfg, set, st, st.chainPre); err != nil {
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

func TestCollectedGuardRestoresOnlyTheOriginalDirection(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	stubBinaries(t, backendIPTables, backendIP6Tables)
	for _, backend := range []routeBackend{&routeNftBackend{}, &routeIptBackend{}} {
		for _, mode := range []string{config.RoutingModeProxy, config.RoutingModeWSReuse} {
			t.Run(backend.name()+"/"+mode, func(t *testing.T) {
				cfg := familyTestConfig(true, true)
				set := staticTestSet(mode, nil)
				set.Targets.UseCollectedIPs = true
				st := routeState{collected: true, setID: set.Id, mode: mode, mark: 0x7133, ipv4: true, ipv6: true, setV4: "b4r_collect_v4", setV6: "b4r_collect_v6", chainPre: "b4r_collect_pre", chainOut: "b4r_collect_out", sources: []string{"br-test"}}
				marker, scope := " meta mark set ", " ct direction original "
				if backend.name() != backendNFTables {
					marker, scope = " -j MARK ", " -m conntrack --ctdir ORIGINAL "
				}
				for _, chain := range []string{st.chainPre, st.chainOut} {
					var calls []string
					run = refuseDeletes(func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil })
					if err := routeCollectedGuard(backend, cfg, set, st, chain); err != nil {
						t.Fatal(err)
					}
					restores := 0
					for _, call := range calls {
						if !strings.Contains(call, marker) {
							continue
						}
						restores++
						if !strings.Contains(call, scope) {
							t.Errorf("the restore in %s takes the reply direction too, so the kernel's reset for a held connection whose proxy socket is gone is marked for the proxy and never reaches the device: %s", chain, call)
						}
					}
					if restores == 0 {
						t.Fatalf("no restore emitted in %s:\n%s", chain, strings.Join(calls, "\n"))
					}
				}
			})
		}
	}
}

func TestCollectedGuardRestoreFollowsTheTagOfEveryFamilyOnNft(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	for _, c := range []struct {
		name       string
		ipv4, ipv6 bool
		sources    []string
	}{
		{"dual stack", true, true, nil},
		{"dual stack, two sources", true, true, []string{"br-a", "br-b"}},
		{"ipv4 only", true, false, nil},
		{"ipv6 only", false, true, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := familyTestConfig(c.ipv4, c.ipv6)
			set := staticTestSet(config.RoutingModeProxy, nil)
			set.Targets.UseCollectedIPs = true
			set.Routing.Upstream.UDP = true
			st := routeState{collected: true, setID: set.Id, mode: config.RoutingModeProxy, mark: 0x7133, ipv4: c.ipv4, ipv6: c.ipv6, setV4: "b4r_collect_v4", setV6: "b4r_collect_v6", chainPre: "b4r_collect_pre", chainOut: "b4r_collect_out", sources: c.sources}
			for _, chain := range []string{st.chainPre, st.chainOut} {
				sources := c.sources
				if len(sources) == 0 || chain == st.chainOut {
					sources = []string{""}
				}
				var calls []string
				run = refuseDeletes(func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "", nil })
				if err := routeCollectedGuard(&routeNftBackend{}, cfg, set, st, chain); err != nil {
					t.Fatal(err)
				}
				for _, src := range sources {
					restore, lastTag := -1, -1
					restores := 0
					for i, call := range calls {
						if src != "" && !strings.Contains(call, fmt.Sprintf("iifname %q", src)) {
							continue
						}
						if strings.Contains(call, " ct mark set ") {
							lastTag = i
						}
						if strings.Contains(call, " meta mark set ") {
							restore = i
							restores++
						}
					}
					if restores != 1 || lastTag < 0 {
						t.Fatalf("%s, source %q: got %d restore(s) and tag at %d, want one restore after the tags:\n%s", chain, src, restores, lastTag, strings.Join(calls, "\n"))
					}
					if restore < lastTag {
						t.Errorf("%s, source %q: the restore sits above the tag of a later family, so a new connection of that family to a collected address is not marked at the guard; the router's own IPv6 SYN then reaches the router-traffic rate guard and past its limit leaves unmarked by the uplink:\n%s", chain, src, strings.Join(calls, "\n"))
					}
				}
			}
		})
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

func collectedGuardRecordNft(t *testing.T) *[]string {
	t.Helper()
	calls := []string{}
	origStdin := runNftStdin
	t.Cleanup(func() { runNftStdin = origStdin })
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
	return &calls
}

func TestCollectedGuardSetHasNoDynamicTwin(t *testing.T) {
	staticResetGlobals(t)
	calls := collectedGuardRecordNft(t)
	cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
	cfg.Queue.IPv6Enabled = true
	st := collectedGuardPrepare(t, &routeNftBackend{}, cfg.WithCollectedIPs([]string{"198.51.100.1", "198.51.100.2", "2001:db8::1"}))

	created := map[string]bool{}
	for _, call := range *calls {
		for _, guard := range []string{st.setV4 + "c", st.setV6 + "c"} {
			if strings.Contains(call, guard+"_d") {
				t.Errorf("the guard set got a dynamic twin it never uses: %s", call)
			}
			if strings.HasPrefix(call, "nft add set inet "+routeNftTable+" "+guard+" ") {
				created[guard] = true
				if strings.Contains(call, "interval") || strings.Contains(call, "timeout") || strings.Contains(call, "auto-merge") {
					t.Errorf("the guard set holds single addresses only, yet it is created as an interval or timeout set, where adjacent addresses merge into a range an older nft cannot delete one of: %s", call)
				}
			}
		}
	}
	if !created[st.setV4+"c"] || !created[st.setV6+"c"] {
		t.Fatalf("no guard set was created for both families:\n%s", strings.Join(*calls, "\n"))
	}

	*calls = nil
	routeDropSets(&routeNftBackend{}, st, false)
	for _, guard := range []string{st.setV4 + "c", st.setV6 + "c"} {
		for _, want := range []string{"nft flush set inet " + routeNftTable + " " + guard, "nft delete set inet " + routeNftTable + " " + guard} {
			if !slices.Contains(*calls, want) {
				t.Errorf("dropping the set did not run %q:\n%s", want, strings.Join(*calls, "\n"))
			}
		}
		for _, call := range *calls {
			if strings.Contains(call, guard+"_d") {
				t.Errorf("dropping the set touched a dynamic twin of its guard set: %s", call)
			}
		}
	}
}

func TestDroppingASetThatNeverCollectedProbesNoGuardSet(t *testing.T) {
	staticResetGlobals(t)
	calls := collectedGuardRecordNft(t)
	cfg := collectedGuardTestConfig(config.RoutingModeProxy, false)
	cfg.Queue.IPv6Enabled = true
	st := buildRouteState(cfg, cfg.Sets[0])
	routeDropSets(&routeNftBackend{}, st, false)
	for _, call := range *calls {
		if strings.Contains(call, st.setV4+"c") || strings.Contains(call, st.setV6+"c") {
			t.Errorf("dropping a set that never used collected IPs touched a guard set: %s", call)
		}
	}
}

func collectedGuardAnswerFrom(installed []string) func(args ...string) (string, error) {
	return func(args ...string) (string, error) {
		line := strings.Join(args, " ")
		if slices.Contains(args, "-C") {
			if slices.Contains(installed, strings.Replace(line, " -C ", " -A ", 1)) {
				return "", nil
			}
			return "iptables: Bad rule (does a matching rule exist in that chain?).", errors.New("exit status 1")
		}
		prefix := "nft list chain inet " + routeNftTable + " "
		if strings.HasPrefix(line, prefix) {
			chain := strings.TrimPrefix(line, prefix)
			var body []string
			for _, rule := range installed {
				if rest, ok := strings.CutPrefix(rule, "nft add rule inet "+routeNftTable+" "+chain+" "); ok {
					body = append(body, "\t\t"+rest)
				}
			}
			return "table inet " + routeNftTable + " {\n\tchain " + chain + " {\n" + strings.Join(body, "\n") + "\n\t}\n}\n", nil
		}
		return "", nil
	}
}

func TestCollectedGuardCheckMatchesWhatASourceScopedConsumerGets(t *testing.T) {
	for _, backend := range []routeBackend{&routeIptBackend{}, &routeNftBackend{}} {
		t.Run(backend.name(), func(t *testing.T) {
			staticResetGlobals(t)
			localGuardReset(t)
			emitted := captureProxyRuleArgv(t)
			run = refuseDeletes(run)
			stubBinaries(t, backendIPTables, "ipset")
			cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
			set := cfg.Sets[0]
			set.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
			st := buildRouteState(cfg, set)
			st.set = set
			if !st.srcScoped {
				t.Fatal("a set limited to one device is not source scoped")
			}
			if err := routeEnsureProxyRule(backend, cfg, set, st, nil); err != nil {
				t.Fatalf("routeEnsureProxyRule: %v", err)
			}
			installed := append([]string(nil), *emitted...)
			for _, line := range chainRuleLines(installed, st.chainOut) {
				if strings.Contains(line, st.setV4+"c") {
					t.Errorf("a source-scoped proxy set got a collected guard in %s: %s", st.chainOut, line)
				}
			}

			run = refuseDeletes(collectedGuardAnswerFrom(installed))
			routeRuleCache = map[string]routeState{set.Id: st}
			if !routeCollectedGuardsPresent(backend, cfg) {
				t.Errorf("the check misses the guard of a source-scoped collected consumer that holds exactly what the emitter installed, so the monitor rebuilds its routing on every tick:\n%s", strings.Join(installed, "\n"))
			}
		})
	}
}

func collectedConnmarkTestSet(id, mode, ip string) *config.SetConfig {
	set := staticTestSet(mode, []string{ip})
	set.Id = id
	set.Name = id
	set.Targets.UseCollectedIPs = true
	return set
}

func TestACollectedConsumerInstallsWithoutConnmark(t *testing.T) {
	staticResetGlobals(t)
	localGuardReset(t)
	emitted := captureProxyRuleArgv(t)
	stubBinaries(t, backendIPTables, "ip", "ipset")
	t.Cleanup(func() {
		routeMu.Lock()
		routeClearSyncRetry()
		routeSyncOutcome = routeSyncReport{}
		routeMu.Unlock()
	})
	run = refuseDeletes(func(args ...string) (string, error) {
		*emitted = append(*emitted, strings.Join(args, " "))
		if slices.Contains(args, "connmark") || slices.Contains(args, "CONNMARK") {
			return "iptables v1.8.7 (legacy): Couldn't load match `connmark':No such file or directory", errors.New("exit status 2")
		}
		return "", nil
	})

	proxy := collectedConnmarkTestSet("nocmproxy", config.RoutingModeProxy, "203.0.113.9")
	block := collectedConnmarkTestSet("nocmblock", config.RoutingModeBlock, "203.0.113.10")
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{proxy, block}
	cfg = cfg.WithCollectedIPs([]string{"198.51.100.1"})
	be := &routeIptBackend{}
	routeEngine = be

	RoutingSyncConfig(cfg)
	RoutingSyncConfig(cfg)

	if errs := RoutingStatus().SetErrors; len(errs) > 0 {
		t.Fatalf("on a router without the connmark match the collected consumers did not install: %+v", errs)
	}
	proxyV4, _ := routeBuildSetNames(proxy.Id)
	blockV4, _ := routeBuildSetNames(block.Id)
	blockPre, _, _ := routeBuildChainNames(block.Id)
	var tproxy, blockReturn bool
	probes := 0
	for _, line := range *emitted {
		isRule := strings.Contains(line, " -A ") || strings.Contains(line, " -I ")
		if isRule && strings.Contains(line, "--match-set "+proxyV4+" dst") && strings.Contains(line, "-j TPROXY") {
			tproxy = true
		}
		if isRule && strings.Contains(line, proxyV4+"c") {
			t.Errorf("the proxy consumer got a collected guard rule on a firewall without connmark: %s", line)
		}
		if strings.Contains(line, "-t filter -A "+blockPre+" ") && strings.Contains(line, "--match-set "+blockV4+"c dst") && strings.HasSuffix(line, "! --ctstate NEW -j RETURN") {
			blockReturn = true
		}
		if strings.Contains(line, " -N B4_CONNMARK_PROBE") {
			probes++
		}
	}
	if !tproxy {
		t.Errorf("no TPROXY rule for %s was emitted:\n%s", proxyV4, strings.Join(*emitted, "\n"))
	}
	if !blockReturn {
		t.Errorf("the block consumer lost its collected guard, which needs no connmark:\n%s", strings.Join(*emitted, "\n"))
	}
	if probes != 1 {
		t.Errorf("the connmark support was probed %d times over two syncs, want once", probes)
	}
	routeMu.Lock()
	present := routeCollectedGuardsPresent(be, cfg)
	routeMu.Unlock()
	if !present {
		t.Error("the monitor finds a collected guard missing that the emitter never installs without connmark, so it rebuilds the routing on every tick")
	}
}

func TestADNSAnswerInstallsACollectedConsumerLikeTheSync(t *testing.T) {
	for _, c := range []struct {
		name     string
		connmark bool
	}{
		{"guard set not yet created", true},
		{"no connmark on this firewall", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			staticResetGlobals(t)
			localGuardReset(t)
			emitted := captureProxyRuleArgv(t)
			stubBinaries(t, backendIPTables, "ip", "ipset")
			origStdin := runStdin
			t.Cleanup(func() { runStdin = origStdin })
			t.Cleanup(func() {
				routeMu.Lock()
				routeClearSyncRetry()
				routeSyncOutcome = routeSyncReport{}
				routeMu.Unlock()
			})
			created := map[string]bool{}
			var restored []string
			runStdin = func(stdin string, args ...string) error {
				restored = append(restored, strings.Split(strings.TrimSpace(stdin), "\n")...)
				return nil
			}
			run = refuseDeletes(func(args ...string) (string, error) {
				*emitted = append(*emitted, strings.Join(args, " "))
				if len(args) > 2 && args[0] == "ipset" && args[1] == "create" {
					created[args[2]] = true
				}
				if !c.connmark && (slices.Contains(args, "connmark") || slices.Contains(args, "CONNMARK")) {
					return "iptables v1.8.7 (legacy): Couldn't load match `connmark':No such file or directory", errors.New("exit status 2")
				}
				if i := slices.Index(args, "--match-set"); i >= 0 && i+1 < len(args) && strings.HasSuffix(args[i+1], "c") && !created[args[i+1]] {
					return fmt.Sprintf("iptables v1.8.7 (legacy): Set %s doesn't exist.", args[i+1]), errors.New("exit status 2")
				}
				return "", nil
			})

			proxy := collectedConnmarkTestSet("dnscollect", config.RoutingModeProxy, "203.0.113.9")
			cfg := familyTestConfig(true, false)
			cfg.Sets = []*config.SetConfig{proxy}
			view := cfg.WithCollectedIPs([]string{"198.51.100.1"})
			routeEngine = &routeIptBackend{}
			guard, _ := routeBuildSetNames(proxy.Id)
			guard += "c"

			RoutingHandleDNS(view, view.Sets[0], []net.IP{net.ParseIP("203.0.113.20")})

			routeMu.Lock()
			_, installed := routeRuleCache[proxy.Id]
			routeMu.Unlock()
			if !installed {
				t.Fatalf("a DNS answer for a collected consumer that no sync installed yet failed to install it, while a sync installs the same set:\n%s", strings.Join(*emitted, "\n"))
			}
			if !slices.Contains(restored, "add "+guard+" 198.51.100.1 timeout 0") {
				t.Errorf("the DNS install left the guard set %s without the collected address, ipset restore got %q", guard, restored)
			}
		})
	}
}

func TestCollectedGuardSkipsAFamilyWithoutItsIptablesBinary(t *testing.T) {
	staticResetGlobals(t)
	for name, present := range map[string]bool{backendIPTables: true, backendIP6Tables: false, "ipset": true} {
		prev, had := hasBinaryCache.Load(name)
		t.Cleanup(func() {
			if had {
				hasBinaryCache.Store(name, prev)
			} else {
				hasBinaryCache.Delete(name)
			}
		})
		hasBinaryCache.Store(name, present)
	}
	var calls []string
	run = refuseDeletes(func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == backendIP6Tables {
			return "", errors.New(`exec: "ip6tables": executable file not found in $PATH`)
		}
		return "", nil
	})
	cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
	cfg.Queue.IPv6Enabled = true
	set := cfg.Sets[0]
	st := buildRouteState(cfg, set)
	st.set = set
	be := &routeIptBackend{}

	for _, chain := range []string{st.chainPre, st.chainOut} {
		if err := routeCollectedGuard(be, cfg, set, st, chain); err != nil {
			t.Fatalf("the collected guard failed the whole consumer on a router without ip6tables, where every other routing rule skips that family: %v", err)
		}
	}
	installed := append([]string(nil), calls...)
	if len(installed) == 0 {
		t.Fatal("no IPv4 guard rule was emitted")
	}
	for _, call := range installed {
		if strings.HasPrefix(call, backendIP6Tables+" ") {
			t.Errorf("the guard ran ip6tables, which this router does not have: %s", call)
		}
	}

	run = refuseDeletes(collectedGuardAnswerFrom(installed))
	routeRuleCache = map[string]routeState{set.Id: st}
	if !routeCollectedGuardsPresent(be, cfg) {
		t.Errorf("the check demands an IPv6 guard the emitter skips on a router without ip6tables, so the monitor rebuilds the routing on every tick:\n%s", strings.Join(installed, "\n"))
	}
}

func TestCollectedGuardLeavesAnInterfaceConsumerToTheFallback(t *testing.T) {
	staticResetGlobals(t)
	stubBinaries(t, backendIPTables, "ipset")
	t.Cleanup(func() { routeCTMarkHeld.Store(true) })
	cfg := collectedGuardTestConfig(config.RoutingModeInterface, true)
	set := cfg.Sets[0]
	st := buildRouteState(cfg, set)
	st.set = set
	guard := st.setV4 + "c"
	for _, backend := range []routeBackend{&routeIptBackend{}, &routeNftBackend{}} {
		for _, held := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/held=%v", backend.name(), held), func(t *testing.T) {
				routeCTMarkHeld.Store(held)
				var calls []string
				run = refuseDeletes(func(args ...string) (string, error) {
					calls = append(calls, strings.Join(args, " "))
					return "", nil
				})
				for _, chain := range []string{st.chainPre, st.chainOut} {
					if err := routeCollectedGuard(backend, cfg, set, st, chain); err != nil {
						t.Fatalf("guard in %s: %v", chain, err)
					}
				}
				returns := 0
				for _, call := range calls {
					if strings.Contains(call, guard) && strings.Contains(strings.ToLower(call), "return") {
						returns++
					}
				}
				switch {
				case held && returns != 2:
					t.Errorf("with the connection mark held, an interface consumer returns established connections to a collected address in its pre and out chains, got %d return rule(s):\n%s", returns, strings.Join(calls, "\n"))
				case !held && len(calls) != 0:
					t.Errorf("on a router that overwrites the connection mark an interface consumer marks every packet of its set, yet the collected guard still returns established connections above that fallback, so once their mark is rewritten they leave by the ordinary uplink:\n%s", strings.Join(calls, "\n"))
				}

				run = refuseDeletes(collectedGuardAnswerFrom(calls))
				routeRuleCache = map[string]routeState{set.Id: st}
				if !routeCollectedGuardsPresent(backend, cfg) {
					t.Errorf("the check demands a different guard from the one the emitter installs (held=%v), so the monitor rebuilds the routing on every tick:\n%s", held, strings.Join(calls, "\n"))
				}
			})
		}
	}
}

func TestCollectedGuardTagsNewUDPOnlyInThePreChain(t *testing.T) {
	for _, backend := range []routeBackend{&routeIptBackend{}, &routeNftBackend{}} {
		for _, c := range []struct {
			name      string
			udp, quic bool
		}{
			{"udp not routed", false, false},
			{"udp routed", true, false},
			{"udp routed, quic refused", true, true},
		} {
			t.Run(backend.name()+"/"+c.name, func(t *testing.T) {
				staticResetGlobals(t)
				localGuardReset(t)
				emitted := captureProxyRuleArgv(t)
				run = refuseDeletes(run)
				stubBinaries(t, backendIPTables, backendIP6Tables)
				cfg := collectedGuardTestConfig(config.RoutingModeProxy, true)
				cfg.Queue.IPv6Enabled = true
				set := cfg.Sets[0]
				set.Routing.Upstream.UDP = c.udp
				if c.quic {
					set.UDP.Mode = config.UDPModeReject
					set.UDP.FilterQUIC = config.QUICFilterAll
				}
				st := buildRouteState(cfg, set)
				st.set = set
				if st.quicReject != (!c.udp || c.quic) {
					t.Fatalf("quicReject=%v does not follow the set", st.quicReject)
				}
				if err := routeEnsureProxyRule(backend, cfg, set, st, nil); err != nil {
					t.Fatalf("routeEnsureProxyRule: %v", err)
				}
				udpTags := func(chain string) []string {
					var tags []string
					for _, line := range chainRuleLines(*emitted, chain) {
						if !strings.Contains(line, "-p udp") && !strings.Contains(line, "l4proto udp") {
							continue
						}
						if strings.Contains(line, "_v4c ") || strings.Contains(line, "_v6c ") {
							if strings.Contains(line, "CONNMARK") || strings.Contains(line, "ct mark set") {
								tags = append(tags, line)
							}
						}
					}
					return tags
				}
				pre, out := udpTags(st.chainPre), udpTags(st.chainOut)
				want := 0
				if c.udp {
					want = 2
				}
				if len(pre) != want {
					t.Errorf("the pre chain holds %d UDP tag(s), want %d: a set that routes UDP through the upstream must claim a new UDP flow to a collected address, or the guard's return sends every datagram after the first out the uplink:\n%s", len(pre), want, strings.Join(chainRuleLines(*emitted, st.chainPre), "\n"))
				}
				if len(out) != 0 {
					t.Errorf("the out chain got a UDP tag, which would proxy the router's own UDP through the restore although the set marks only its TCP: %v", out)
				}
				for _, line := range pre {
					excluded := strings.Contains(line, "! --dport 443") || strings.Contains(line, "udp dport != 443")
					if c.quic && !excluded {
						t.Errorf("a set that refuses QUIC tags UDP/443 to a collected address, so the restore marks it for the proxy, the QUIC pass accepts it and it is delivered to the router instead of being refused in forward: %s", line)
					}
					if !c.quic && strings.Contains(line, "443") {
						t.Errorf("a set that lets QUIC through the upstream excludes UDP/443 from the tag: %s", line)
					}
				}
			})
		}
	}
}

func TestRefreshCollectedRulesLeavesClampAndDuplicateConsumersInPlace(t *testing.T) {
	for _, shape := range []struct {
		name  string
		shape func(*config.SetConfig)
	}{
		{"mss clamp", func(s *config.SetConfig) { s.MSSClamp.Enabled, s.MSSClamp.Size = true, 88 }},
		{"duplication", func(s *config.SetConfig) { s.TCP.Duplicate.Enabled, s.TCP.Duplicate.Count = true, 3 }},
	} {
		t.Run(shape.name, func(t *testing.T) {
			oldAdd, oldClear, oldRun := addRulesFn, clearRulesFn, run
			rulesMu.Lock()
			oldApplied := rulesAppliedCfg
			rulesMu.Unlock()
			t.Cleanup(func() {
				addRulesFn, clearRulesFn, run = oldAdd, oldClear, oldRun
				rulesMu.Lock()
				rulesAppliedCfg = oldApplied
				rulesMu.Unlock()
			})
			var calls []string
			run = refuseDeletes(func(args ...string) (string, error) {
				calls = append(calls, strings.Join(args, " "))
				return "", nil
			})
			clearRulesFn = func(*config.Config) error {
				calls = append(calls, "clear")
				return nil
			}
			addRulesFn = func(cfg *config.Config) error {
				calls = append(calls, "add")
				rulesAppliedCfg = cfg
				return nil
			}
			cfg := config.NewConfig()
			cfg.Queue.IPv6Enabled = true
			set := config.NewSetConfig()
			set.Id, set.Name, set.Enabled = "consumer", "consumer", true
			set.Targets.IPs = []string{"198.51.100.1"}
			set.Targets.IpsToMatch = []string{"198.51.100.1"}
			set.Targets.UseCollectedIPs = true
			shape.shape(&set)
			cfg.Sets = []*config.SetConfig{&set}
			rulesMu.Lock()
			rulesAppliedCfg = cfg.WithCollectedIPs(nil)
			rulesMu.Unlock()

			for _, pool := range [][]string{{"203.0.113.7"}, {"203.0.113.7", "2001:db8::7"}} {
				if err := RefreshCollectedRules(cfg.WithCollectedIPs(pool)); err != nil {
					t.Fatal(err)
				}
			}
			if len(calls) != 0 {
				t.Fatalf("collecting addresses for a consumer with %s rebuilt the firewall: %q", shape.name, calls)
			}
		})
	}
}
