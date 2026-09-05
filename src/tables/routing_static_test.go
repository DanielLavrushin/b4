package tables

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestRouteEntriesGone(t *testing.T) {
	for _, tc := range []struct {
		name string
		prev []string
		cur  []string
		want []string
	}{
		{"halves removed", []string{"0.0.0.0/1", "128.0.0.0/1", "203.0.113.9"}, []string{"203.0.113.9"}, []string{"0.0.0.0/1", "128.0.0.0/1"}},
		{"identical", []string{"203.0.113.9"}, []string{"203.0.113.9"}, nil},
		{"nothing before", nil, []string{"203.0.113.9"}, nil},
		{"everything removed", []string{"203.0.113.9", "203.0.113.10"}, nil, []string{"203.0.113.9", "203.0.113.10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeEntriesGone(tc.prev, tc.cur); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("routeEntriesGone(%v, %v) = %v, want %v", tc.prev, tc.cur, got, tc.want)
			}
		})
	}
}

type staticCall struct {
	set string
	ips []string
	ttl int
}

func staticResetGlobals(t *testing.T) {
	t.Helper()
	familyResetGlobals(t)
	applied := routeStaticApplied
	origRun := run
	origSysctl := writeSysctl
	t.Cleanup(func() {
		routeStaticApplied = applied
		run = origRun
		writeSysctl = origSysctl
		proxyTableForget()
	})
	routeStaticApplied = make(map[string]routeStaticEntries)
	run = func(args ...string) (string, error) { return "", nil }
	writeSysctl = func(path, value string) {}
	proxyTableForget()
}

func staticTestSet(mode string, ips []string) *config.SetConfig {
	s := config.NewSetConfig()
	s.Id = "statictest"
	s.Name = "statictest"
	s.Enabled = true
	s.Routing.Enabled = true
	s.Routing.Mode = mode
	switch mode {
	case config.RoutingModeInterface:
		s.Routing.EgressInterface = "b4fam0"
		s.Routing.FWMark = 0x7e11
		s.Routing.Table = 233
	case config.RoutingModeProxy:
		s.Routing.Upstream.Host = "127.0.0.1"
		s.Routing.Upstream.Port = 1080
	}
	s.Targets.IPs = ips
	s.Targets.IpsToMatch = ips
	return &s
}

func staticSyncBackend(added, deleted *[]staticCall) *mockRouteBackend {
	return &mockRouteBackend{
		addElementsFn: func(name string, ips []string, ttl int) {
			*added = append(*added, staticCall{set: name, ips: append([]string{}, ips...), ttl: ttl})
		},
		delElementsFn: func(name string, ips []string) {
			*deleted = append(*deleted, staticCall{set: name, ips: append([]string{}, ips...)})
		},
	}
}

func TestRoutingSyncConfig_RemovedStaticEntryIsDeletedWithoutARebuild(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("RoutingSyncConfig gives up before it touches a set when the ip binary is missing")
	}
	for _, mode := range []string{config.RoutingModeInterface, config.RoutingModeProxy, config.RoutingModeBlock} {
		t.Run(mode, func(t *testing.T) {
			staticResetGlobals(t)

			set := staticTestSet(mode, []string{"0.0.0.0/0", "203.0.113.9"})
			setV4, _ := routeBuildSetNames(set.Id)
			cfg := familyTestConfig(true, false)
			cfg.Sets = []*config.SetConfig{set}

			var added, deleted []staticCall
			be := staticSyncBackend(&added, &deleted)
			routeEngine = be

			RoutingSyncConfig(cfg)
			if len(deleted) != 0 {
				t.Fatalf("the first sync has nothing to delete: %+v", deleted)
			}
			if len(added) == 0 || added[len(added)-1].set != setV4 || added[len(added)-1].ttl != 0 {
				t.Fatalf("the static entries were not pushed as permanent members of %s: %+v", setV4, added)
			}
			if got := routeStaticApplied[set.Id].v4; !reflect.DeepEqual(got, []string{"0.0.0.0/1", "128.0.0.0/1", "203.0.113.9"}) {
				t.Fatalf("the applied record holds %v, want the expanded halves and the plain address", got)
			}

			jumps, deletedJumps := len(be.jumps), len(be.deletedJumps)
			chainOps := map[string]int{}
			for chain, ops := range be.chainOps {
				chainOps[chain] = len(ops)
			}
			added, deleted, be.setOps = nil, nil, nil

			set.Targets.IPs = []string{"203.0.113.9"}
			set.Targets.IpsToMatch = []string{"203.0.113.9"}
			RoutingSyncConfig(cfg)

			if len(deleted) != 1 || deleted[0].set != setV4 || !reflect.DeepEqual(deleted[0].ips, []string{"0.0.0.0/1", "128.0.0.0/1"}) {
				t.Errorf("expected one delete of the expanded halves from %s, got %+v", setV4, deleted)
			}
			if len(added) != 1 || !reflect.DeepEqual(added[0].ips, []string{"203.0.113.9"}) || added[0].ttl != 0 {
				t.Errorf("the surviving static entry must be re-asserted after the delete: %+v", added)
			}
			if !reflect.DeepEqual(be.setOps, []string{"del " + setV4, "add " + setV4}) {
				t.Errorf("nft folds the halves and every other static prefix into one element, so the delete must run before the add: %v", be.setOps)
			}
			if len(be.jumps) != jumps || len(be.deletedJumps) != deletedJumps {
				t.Errorf("editing the address list must not rebuild the set (jumps %d->%d, deleted jumps %d->%d)", jumps, len(be.jumps), deletedJumps, len(be.deletedJumps))
			}
			for chain, n := range chainOps {
				if len(be.chainOps[chain]) != n {
					t.Errorf("chain %s was rewritten by an address-list edit: %v", chain, be.chainOps[chain][n:])
				}
			}
			if got := routeStaticApplied[set.Id].v4; !reflect.DeepEqual(got, []string{"203.0.113.9"}) {
				t.Errorf("the applied record holds %v after the removal", got)
			}
		})
	}
}

func TestRoutingSyncConfig_UnchangedStaticListDeletesNothing(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)

	set := staticTestSet(config.RoutingModeProxy, []string{"0.0.0.0/0", "203.0.113.9"})
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	for i := 0; i < 3; i++ {
		RoutingSyncConfig(cfg)
	}
	if len(deleted) != 0 {
		t.Errorf("an unchanged list issued deletes: %+v", deleted)
	}
	if len(added) != 3 {
		t.Errorf("every sync re-asserts the static entries, got %d adds", len(added))
	}
}

func TestRoutingSyncConfig_ComparesExpandedHalvesAndBothFamilies(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	for _, ipv6 := range []bool{true, false} {
		t.Run(map[bool]string{true: "ipv6 on", false: "ipv6 off"}[ipv6], func(t *testing.T) {
			staticResetGlobals(t)

			set := staticTestSet(config.RoutingModeProxy, []string{"0.0.0.0/0", "::/0", "203.0.113.9"})
			setV4, setV6 := routeBuildSetNames(set.Id)
			cfg := familyTestConfig(true, ipv6)
			cfg.Sets = []*config.SetConfig{set}

			var added, deleted []staticCall
			routeEngine = staticSyncBackend(&added, &deleted)
			RoutingSyncConfig(cfg)

			set.Targets.IPs = []string{"203.0.113.9"}
			set.Targets.IpsToMatch = []string{"203.0.113.9"}
			RoutingSyncConfig(cfg)

			got := map[string][]string{}
			for _, d := range deleted {
				got[d.set] = append(got[d.set], d.ips...)
			}
			if !reflect.DeepEqual(got[setV4], []string{"0.0.0.0/1", "128.0.0.0/1"}) {
				t.Errorf("v4 halves not deleted: %v", got[setV4])
			}
			if ipv6 {
				if !reflect.DeepEqual(got[setV6], []string{"::/1", "8000::/1"}) {
					t.Errorf("v6 halves not deleted: %v", got[setV6])
				}
			} else if len(got[setV6]) != 0 || len(routeStaticApplied[set.Id].v6) != 0 {
				t.Errorf("with IPv6 disabled nothing may be recorded or deleted for %s: %v %v", setV6, got[setV6], routeStaticApplied[set.Id].v6)
			}
		})
	}
}

func TestRouteStaticRecordSurvivesForceResyncAndDiesWithTheSets(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)

	set := staticTestSet(config.RoutingModeProxy, []string{"0.0.0.0/0", "203.0.113.9"})
	setV4, _ := routeBuildSetNames(set.Id)
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	var added, deleted []staticCall
	be := staticSyncBackend(&added, &deleted)
	routeEngine = be
	RoutingSyncConfig(cfg)

	RoutingForceResync(cfg)
	if len(deleted) != 0 {
		t.Fatalf("a forced resync keeps the kernel sets, so it has nothing to delete: %+v", deleted)
	}
	if _, ok := routeStaticApplied[set.Id]; !ok {
		t.Fatal("the applied record was dropped by the resync, so a later removal would never delete anything")
	}

	set.Targets.IPs = []string{"203.0.113.9"}
	set.Targets.IpsToMatch = []string{"203.0.113.9"}
	RoutingSyncConfig(cfg)
	if len(deleted) != 1 || deleted[0].set != setV4 || !reflect.DeepEqual(deleted[0].ips, []string{"0.0.0.0/1", "128.0.0.0/1"}) {
		t.Errorf("the removal after the resync issued no delete of the halves: %+v", deleted)
	}

	st := routeRuleCache[set.Id]
	routeStaticApplied[set.Id] = routeStaticEntries{v4: []string{"203.0.113.9"}}
	routeDropSets(be, st, true)
	if _, ok := routeStaticApplied[set.Id]; !ok {
		t.Error("keeping the sets must keep the record")
	}
	routeDropSets(be, st, false)
	if _, ok := routeStaticApplied[set.Id]; ok {
		t.Error("destroying the sets must forget the record, or the next sync would add without ever deleting")
	}

	routeStaticApplied[set.Id] = routeStaticEntries{v4: []string{"203.0.113.9"}}
	RoutingClearAll()
	if len(routeStaticApplied) != 0 {
		t.Error("RoutingClearAll must forget every record")
	}
}

func TestIptDelElementsUsesRestoreThenFallsBack(t *testing.T) {
	stubBinaries(t, "ipset")
	origStdin, origLogged := runStdin, runLogged
	t.Cleanup(func() {
		runStdin = origStdin
		runLogged = origLogged
	})

	var stdin string
	var stdinArgs []string
	var logged []string
	runStdin = func(in string, args ...string) error {
		stdin = in
		stdinArgs = args
		return nil
	}
	runLogged = func(op string, args ...string) bool {
		logged = append(logged, strings.Join(args, " "))
		return true
	}

	(&routeIptBackend{}).delElements("b4r_x_v4", []string{"0.0.0.0/1", "128.0.0.0/1"})
	if stdin != "del b4r_x_v4 0.0.0.0/1\ndel b4r_x_v4 128.0.0.0/1\n" {
		t.Errorf("unexpected restore batch: %q", stdin)
	}
	if strings.Join(stdinArgs, " ") != "ipset restore -exist" {
		t.Errorf("unexpected restore argv: %v", stdinArgs)
	}
	if len(logged) != 0 {
		t.Errorf("the batch succeeded, so no per-entry delete is needed: %v", logged)
	}

	runStdin = func(string, ...string) error { return errors.New("restore failed") }
	(&routeIptBackend{}).delElements("b4r_x_v4", []string{"0.0.0.0/1", "128.0.0.0/1"})
	want := []string{"ipset del b4r_x_v4 0.0.0.0/1 -exist", "ipset del b4r_x_v4 128.0.0.0/1 -exist"}
	if !reflect.DeepEqual(logged, want) {
		t.Errorf("per-entry fallback = %v, want %v", logged, want)
	}
}

func TestNftDelElementsTargetsTheIntervalSetAndFallsBackPerElement(t *testing.T) {
	origRun := run
	t.Cleanup(func() { run = origRun })

	var calls []string
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, ",") {
			return "Error: Could not process rule: No such file or directory", errors.New("exit status 1")
		}
		return "", nil
	}

	(&routeNftBackend{}).delElements("b4r_x_v4", []string{"0.0.0.0/1", "128.0.0.0/1"})
	want := []string{
		"nft delete element inet b4_route b4r_x_v4 { 0.0.0.0/1 , 128.0.0.0/1 }",
		"nft delete element inet b4_route b4r_x_v4 { 0.0.0.0/1 }",
		"nft delete element inet b4_route b4r_x_v4 { 128.0.0.0/1 }",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	for _, c := range calls {
		if strings.Contains(c, "_d") {
			t.Errorf("static entries never live in the dynamic set: %q", c)
		}
	}
}

func TestRoutingSyncConfig_DisablingAFamilyDeletesItsStaticEntries(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)

	set := staticTestSet(config.RoutingModeInterface, []string{"203.0.113.9", "2001:db8::7"})
	setV4, setV6 := routeBuildSetNames(set.Id)

	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	sync := func(ipv6 bool) {
		cfg := familyTestConfig(true, ipv6)
		cfg.Sets = []*config.SetConfig{set}
		RoutingSyncConfig(cfg)
	}
	deletesFor := func(name string) []string {
		var out []string
		for _, d := range deleted {
			if d.set == name {
				out = append(out, d.ips...)
			}
		}
		return out
	}

	sync(true)
	if len(deleted) != 0 {
		t.Fatalf("the first sync has nothing to delete: %+v", deleted)
	}

	sync(false)
	if got := deletesFor(setV6); !reflect.DeepEqual(got, []string{"2001:db8::7"}) {
		t.Errorf("turning IPv6 off keeps the v6 set, so its static entries must be taken out while b4 still remembers them, got %v", got)
	}
	if got := deletesFor(setV4); len(got) != 0 {
		t.Errorf("the IPv4 family stayed on, nothing of its should be deleted: %v", got)
	}
	if got := routeStaticApplied[set.Id].v6; len(got) != 0 {
		t.Errorf("the v6 snapshot must be empty while the family is off, got %v", got)
	}

	set.Targets.IPs = []string{"203.0.113.9"}
	set.Targets.IpsToMatch = []string{"203.0.113.9"}
	deleted = nil
	sync(false)
	if len(deleted) != 0 {
		t.Errorf("removing the v6 entry while IPv6 is off has nothing left to delete: %+v", deleted)
	}

	added = nil
	sync(true)
	for _, a := range added {
		if a.set == setV6 {
			t.Errorf("the removed v6 entry came back when IPv6 was turned on again: %+v", a)
		}
	}
	if got := deletesFor(setV6); len(got) != 0 {
		t.Errorf("nothing should be deleted on re-enable, got %v", got)
	}
}

func TestRoutingSyncConfig_AFamilyThatWasNeverOnIsNeverTouched(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("needs the ip binary")
	}
	staticResetGlobals(t)

	set := staticTestSet(config.RoutingModeProxy, []string{"203.0.113.9", "2001:db8::7"})
	_, setV6 := routeBuildSetNames(set.Id)

	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}
	RoutingSyncConfig(cfg)
	RoutingSyncConfig(cfg)

	for _, c := range append(added, deleted...) {
		if c.set == setV6 {
			t.Errorf("with IPv6 off from the start the v6 set does not exist, so nothing may be pushed into or deleted from it: %+v", c)
		}
	}
}
