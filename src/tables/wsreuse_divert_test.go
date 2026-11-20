package tables

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func countTProxyProbes(t *testing.T, missing []string) *int {
	t.Helper()
	calls := 0
	tproxyCapabilityProbe = func(string) ([]string, bool) {
		calls++
		return missing, true
	}
	return &calls
}

func seedTProxyCapability(key string, c tproxyCapability, present bool) {
	tproxyCapMu.Lock()
	defer tproxyCapMu.Unlock()
	tproxyCapCache = map[string]tproxyCapability{}
	if present {
		tproxyCapCache[key] = c
	}
}

func publishRouteMarks() {
	routeMu.Lock()
	defer routeMu.Unlock()
	routePublishMarksLocked()
}

func TestWSReuseDivertsNeedsTheRuleAndTProxy(t *testing.T) {
	familyResetGlobals(t)
	bridgeResetReadiness(t)
	probes := countTProxyProbes(t, nil)
	routeEngine = &mockRouteBackend{}
	routeRuleCache = map[string]routeState{
		"reuse":   {setID: "reuse", mode: config.RoutingModeWSReuse, diverts: true, tproxied: true},
		"nodiv":   {setID: "nodiv", mode: config.RoutingModeWSReuse, tproxied: true},
		"refused": {setID: "refused", mode: config.RoutingModeWSReuse, diverts: true},
		"proxy":   {setID: "proxy", mode: config.RoutingModeProxy, diverts: true, tproxied: true},
		"bridged": {setID: "bridged", mode: config.RoutingModeMTProtoWS},
	}

	publishRouteMarks()
	if !WSReuseDiverts("reuse") {
		t.Fatal("a reuse view installed while TPROXY was usable is not reported as diverted")
	}
	if WSReuseDiverts("nodiv") {
		t.Fatal("a reuse view installed without a usable TPROXY is reported as diverted, so the queue passes its TCP/443 untouched while no TPROXY rule takes it")
	}
	if WSReuseDiverts("refused") {
		t.Fatal("a reuse view whose TPROXY rule the firewall refused is reported as diverted, so the queue passes its TCP/443 untouched while no TPROXY rule takes it")
	}
	if WSReuseDiverts("proxy") || WSReuseDiverts("bridged") || WSReuseDiverts("absent") {
		t.Fatal("a set that does not reuse WSSIZE is reported as a diverted reuse set")
	}

	delete(routeRuleCache, "reuse")
	publishRouteMarks()
	if WSReuseDiverts("reuse") {
		t.Fatal("a reuse set whose rules were removed is still reported as diverted")
	}
	if *probes != 0 {
		t.Fatalf("publishing ran the TPROXY probe %d time(s); it runs on every DNS-learned address and cache change, so it must only read what the sync recorded", *probes)
	}
}

func reuseDivertTestConfig(t *testing.T, reuse bool) *config.Config {
	t.Helper()
	cfg := familyTestConfig(true, false)
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "reuse", "reuse", true
	set.Targets.IPs = []string{"203.0.113.2"}
	set.Targets.IpsToMatch = set.Targets.IPs
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = reuse
	cfg.Sets = []*config.SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRoutingSyncPublishesTheReuseDiversion(t *testing.T) {
	staticResetGlobals(t)
	bridgeResetReadiness(t)
	stubBinaries(t, "ip")
	run = refuseDeletes(run)
	runLogged = func(op string, args ...string) bool {
		_, err := run(args...)
		return err == nil
	}
	var added, deleted []staticCall
	routeEngine = staticSyncBackend(&added, &deleted)
	cfg := reuseDivertTestConfig(t, true)
	if views := cfg.RoutingSets(); len(views) != 1 || views[0].Routing.Mode != config.RoutingModeWSReuse {
		t.Fatalf("the validated reuse set gives no reuse routing view: %+v", views)
	}

	probes := countTProxyProbes(t, nil)
	RoutingSyncConfig(cfg)
	if _, ok := routeRuleCache["reuse"]; !ok || !WSReuseDiverts("reuse") {
		t.Fatalf("an installed reuse view on a firewall with TPROXY is not reported as diverted: cached=%v", ok)
	}
	if *probes != 1 {
		t.Fatalf("a sync with a reuse view ran the TPROXY probe %d time(s), want once", *probes)
	}

	seedTProxyCapability(backendIPTables, tproxyCapability{}, false)
	probes = countTProxyProbes(t, []string{"xt_TPROXY"})
	RoutingSyncConfig(cfg)
	if WSReuseDiverts("reuse") {
		t.Fatal("a firewall without TPROXY was reported as diverting the reuse set")
	}
	if _, warned := routeWSReuseUndivertedWarned.Load("reuse"); !warned {
		t.Fatal("a reuse set the firewall cannot divert was not warned about")
	}
	RoutingSyncConfig(cfg)
	if WSReuseDiverts("reuse") || *probes != 1 {
		t.Fatalf("a second sync within the recheck interval: diverts=%v probes=%d, want false and the one cached probe", WSReuseDiverts("reuse"), *probes)
	}

	seedTProxyCapability(backendIPTables, tproxyCapability{}, false)
	countTProxyProbes(t, nil)
	RoutingSyncConfig(cfg)
	if !WSReuseDiverts("reuse") {
		t.Fatal("the reuse set is not reported as diverted once TPROXY works again")
	}
	if _, warned := routeWSReuseUndivertedWarned.Load("reuse"); warned {
		t.Fatal("the warning memo outlived the missing TPROXY, so a later loss is never reported")
	}

	seedTProxyCapability(backendIPTables, tproxyCapability{}, false)
	probes = countTProxyProbes(t, nil)
	RoutingSyncConfig(reuseDivertTestConfig(t, false))
	if WSReuseDiverts("reuse") {
		t.Fatal("turning reuse off left the set reported as diverted")
	}
	if *probes != 0 {
		t.Fatalf("a sync without a reuse view ran the TPROXY probe %d time(s)", *probes)
	}
}

func TestAReuseViewBuiltWithoutTProxyIsRebuiltOnceTProxyWorks(t *testing.T) {
	staticResetGlobals(t)
	bridgeResetReadiness(t)
	localGuardReset(t)
	stubBinaries(t, backendIPTables, "ip", "ipset")
	origStdin := runStdin
	t.Cleanup(func() { runStdin = origStdin })
	runStdin = func(string, ...string) error { return nil }
	routeDelRuleLoop = func(bool, string, string) {}
	t.Cleanup(func() {
		routeMu.Lock()
		routeClearSyncRetry()
		routeSyncOutcome = routeSyncReport{}
		routeMu.Unlock()
	})
	missing := true
	var tproxyAdds []string
	run = refuseDeletes(func(args ...string) (string, error) {
		if slices.Contains(args, "TPROXY") && slices.Contains(args, "-A") {
			if missing {
				return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
			}
			tproxyAdds = append(tproxyAdds, strings.Join(args, " "))
		}
		return "", nil
	})
	runLogged = func(op string, args ...string) bool {
		_, err := run(args...)
		return err == nil
	}
	tproxyCapabilityProbe = func(string) ([]string, bool) {
		if missing {
			return []string{"xt_TPROXY"}, true
		}
		return nil, true
	}
	routeEngine = &routeIptBackend{}
	cfg := reuseDivertTestConfig(t, true)

	RoutingSyncConfig(cfg)
	if _, ok := routeRuleCache["reuse"]; !ok {
		t.Fatal("the reuse view was not installed without TPROXY, so the test cannot show what a later sync does with it")
	}
	if WSReuseDiverts("reuse") {
		t.Fatal("a reuse view whose TPROXY rule the firewall refused is reported as diverted")
	}

	missing = false
	seedTProxyCapability(backendIPTables, tproxyCapability{}, false)
	RoutingSyncConfig(cfg)
	if WSReuseDiverts("reuse") && len(tproxyAdds) == 0 {
		t.Fatal("once TPROXY works the reuse set is reported as diverted, but no sync has added its TPROXY rule: the queue passes its TCP/443 untouched while nothing diverts it")
	}
	if !WSReuseDiverts("reuse") || len(tproxyAdds) == 0 {
		t.Fatalf("the next sync after TPROXY started working left the reuse view as it was built without it (diverted=%v, TPROXY rules added=%d), so the set never reaches the relay until it is edited or b4 restarts", WSReuseDiverts("reuse"), len(tproxyAdds))
	}
}

func TestFamilyResetGlobalsStartsWithoutAReuseDiversion(t *testing.T) {
	routeWSReuseDiverted.Store(&map[string]struct{}{"leftover": {}})
	routeWSReuseUndivertedWarned.Store("leftover", struct{}{})
	t.Run("reset", func(t *testing.T) {
		familyResetGlobals(t)
		if WSReuseDiverts("leftover") {
			t.Error("a reuse diversion published by an earlier test survived the reset, so an nfq-facing test reads it")
		}
		if _, warned := routeWSReuseUndivertedWarned.Load("leftover"); warned {
			t.Error("a reuse warning memo from an earlier test survived the reset")
		}
	})
	if !WSReuseDiverts("leftover") {
		t.Error("the reset did not restore the published diversion it found")
	}
	routeWSReuseDiverted.Store(&map[string]struct{}{})
	routeWSReuseUndivertedWarned.Delete("leftover")
}

func refusedTProxyRig(t *testing.T, engine string, refuse func(args []string) bool) (attempts, added *[]string) {
	t.Helper()
	staticResetGlobals(t)
	bridgeResetReadiness(t)
	localGuardReset(t)
	stubBinaries(t, backendIPTables, backendIP6Tables, "ip", "ipset", "nft")
	origStdin := runStdin
	t.Cleanup(func() { runStdin = origStdin })
	runStdin = func(string, ...string) error { return nil }
	routeDelRuleLoop = func(bool, string, string) {}
	t.Cleanup(func() {
		routeMu.Lock()
		routeClearSyncRetry()
		routeSyncOutcome = routeSyncReport{}
		routeMu.Unlock()
	})
	attempts, added = &[]string{}, &[]string{}
	run = refuseDeletes(func(args ...string) (string, error) {
		tproxy := slices.Contains(args, "TPROXY") || slices.Contains(args, "tproxy")
		add := slices.Contains(args, "-A") || len(args) > 2 && args[0] == "nft" && args[1] == "add" && args[2] == "rule"
		ownSet := slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "b4r_") })
		if tproxy && add && ownSet {
			line := strings.Join(args, " ")
			*attempts = append(*attempts, line)
			if refuse(args) {
				return "No chain/target/match by that name.", errors.New("exit status 1")
			}
			*added = append(*added, line)
		}
		return "", nil
	})
	runLogged = func(op string, args ...string) bool {
		_, err := run(args...)
		return err == nil
	}
	tproxyCapabilityProbe = func(string) ([]string, bool) { return nil, true }
	if engine == backendNFTables {
		routeEngine = &routeNftBackend{}
	} else {
		routeEngine = &routeIptBackend{}
	}
	return attempts, added
}

func refusedTProxyConfig(t *testing.T, v6 bool) *config.Config {
	t.Helper()
	cfg := familyTestConfig(true, v6)
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "reuse", "reuse", true
	set.Targets.IPs = []string{"203.0.113.2"}
	if v6 {
		set.Targets.IPs = append(set.Targets.IPs, "2001:db8::2")
	}
	set.Targets.IpsToMatch = set.Targets.IPs
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = true
	cfg.Sets = []*config.SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAReuseSetWhoseTProxyRuleIsRefusedIsNotDiverted(t *testing.T) {
	v6Rule := func(args []string) bool {
		return args[0] == backendIP6Tables || slices.Contains(args, "ip6")
	}
	learnedRule := func(args []string) bool {
		return slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "@") && strings.HasSuffix(a, "_d") })
	}
	every := func([]string) bool { return true }
	for _, tc := range []struct {
		name   string
		engine string
		v6     bool
		refuse func([]string) bool
	}{
		{"iptables, every TPROXY rule", backendIPTables, false, every},
		{"nftables, every TPROXY rule", backendNFTables, false, every},
		{"iptables, only the IPv6 rule", backendIPTables, true, v6Rule},
		{"nftables, only the IPv6 rule", backendNFTables, true, v6Rule},
		{"nftables, only the learned-address rule", backendNFTables, false, learnedRule},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts, added := refusedTProxyRig(t, tc.engine, tc.refuse)
			cfg := refusedTProxyConfig(t, tc.v6)
			RoutingSyncConfig(cfg)
			st, cached := routeRuleCache["reuse"]
			if !cached || len(*attempts) == len(*added) {
				t.Fatalf("setup: cached=%v, %d TPROXY adds tried and %d taken, want the view installed with a refused add", cached, len(*attempts), len(*added))
			}
			if WSReuseDiverts("reuse") {
				t.Fatalf("the reuse set is published as diverted although the firewall refused %d of its %d TPROXY rules, so the queue passes its TCP/443 untouched with no relay and no strategy", len(*attempts)-len(*added), len(*attempts))
			}
			st.collected = true
			if _, routed := routeCollectedRouteOf(st); routed {
				t.Fatal("the collected route view counts the undiverted reuse set as taking its TCP/443")
			}
			tried := len(*attempts)
			RoutingSyncConfig(cfg)
			if len(*attempts) != tried {
				t.Fatalf("an unchanged second sync tried %d more TPROXY adds: a refused add must not rebuild the set on every sync", len(*attempts)-tried)
			}
		})
	}
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine+", nothing refused", func(t *testing.T) {
			_, added := refusedTProxyRig(t, engine, func([]string) bool { return false })
			RoutingSyncConfig(refusedTProxyConfig(t, true))
			st := routeRuleCache["reuse"]
			st.collected = true
			if _, routed := routeCollectedRouteOf(st); !WSReuseDiverts("reuse") || !routed || len(*added) == 0 {
				t.Fatalf("a reuse set whose TPROXY rules all went in: diverted=%v collected view=%v rules=%d, want diverted", WSReuseDiverts("reuse"), routed, len(*added))
			}
		})
	}
}

func TestAMissingIP6TablesBinaryLeavesAReuseSetDiverted(t *testing.T) {
	attempts, added := refusedTProxyRig(t, backendIPTables, func([]string) bool { return false })
	hasBinaryCache.Store(backendIP6Tables, false)
	RoutingSyncConfig(refusedTProxyConfig(t, true))
	v6 := 0
	for _, line := range *attempts {
		if strings.HasPrefix(line, backendIP6Tables+" ") {
			v6++
		}
	}
	if v6 != 0 || len(*added) == 0 {
		t.Fatalf("setup: %d ip6tables TPROXY adds tried and %d TPROXY adds taken, want no IPv6 add and the IPv4 rule in", v6, len(*added))
	}
	if !WSReuseDiverts("reuse") {
		t.Fatal("a reuse set is published as undiverted because ip6tables is missing, although no IPv6 packet reaches b4's queue without it and the IPv4 TPROXY rule went in")
	}
}
