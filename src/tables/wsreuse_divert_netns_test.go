package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func netnsTProxyRuleCount(t *testing.T, engine, chain string) int {
	t.Helper()
	if engine == backendNFTables {
		out, _ := run("nft", "list", "chain", "inet", routeNftTable, chain)
		return strings.Count(out, "tproxy ")
	}
	out, _ := run(engine, "-w", "-t", "mangle", "-L", chain, "-n")
	return strings.Count(out, "TPROXY ")
}

func netnsTheReuseDiversionFollowsTheTProxyProbe(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	tproxyCapMu.Lock()
	cache := tproxyCapCache
	tproxyCapCache = map[string]tproxyCapability{}
	tproxyCapMu.Unlock()
	t.Cleanup(func() {
		tproxyCapMu.Lock()
		tproxyCapCache = cache
		tproxyCapMu.Unlock()
		routeWSReuseUndivertedWarned.Delete("netns-reuse")
	})

	routeEngine = nil
	cfg := netnsConfig(engine)
	set := config.NewSetConfig()
	set.Id = "netns-reuse"
	set.Name = "netnsreuse"
	set.Enabled = true
	set.Targets.IPs = []string{netnsTarget}
	set.Targets.IpsToMatch = []string{netnsTarget}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = true
	cfg.Sets = []*config.SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the reuse set does not validate: %v", err)
	}
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	RoutingSyncConfig(cfg)
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(cfg)
		routeEngine = nil
	})

	routeMu.Lock()
	st, installed := routeRuleCache[set.Id]
	be := routeEngine
	routeMu.Unlock()
	if !installed || be == nil {
		netnsLogState(t, engine)
		t.Fatal("the reuse set's routing view built no rules")
	}
	tproxyCapMu.Lock()
	c, known := tproxyCapCache[routeBackendCapKey(be)]
	tproxyCapMu.Unlock()
	rules := netnsTProxyRuleCount(t, engine, st.chainPre)
	t.Logf("%s, capability key %s: probed=%v usable=%v missing=%v; TPROXY rules in %s: %d; reported diverted: %v",
		engine, routeBackendCapKey(be), c.probed, c.usable, c.missing, st.chainPre, rules, WSReuseDiverts(set.Id))
	if !known {
		t.Fatal("a sync with a reuse view left no TPROXY capability for its routing backend, so the diversion can never be reported")
	}
	if got := WSReuseDiverts(set.Id); got != c.usable {
		t.Fatalf("the reuse set is reported as diverted=%v while the firewall's TPROXY probe says usable=%v (missing %v)", got, c.usable, c.missing)
	}
	if c.usable && rules == 0 {
		netnsLogState(t, engine)
		t.Fatalf("the probe reports TPROXY usable and the set is reported as diverted, yet %s holds no TPROXY rule", st.chainPre)
	}

	RoutingClearAll()
	if WSReuseDiverts(set.Id) {
		t.Fatal("clearing routing left the reuse set reported as diverted")
	}
}

func TestNetnsTheReuseDiversionFollowsTheTProxyProbe(t *testing.T) {
	netnsTheReuseDiversionFollowsTheTProxyProbe(t, backendIPTables)
}

func TestNetnsTheReuseDiversionFollowsTheTProxyProbeNft(t *testing.T) {
	netnsTheReuseDiversionFollowsTheTProxyProbe(t, backendNFTables)
}

func TestNetnsTheReuseDiversionFollowsTheTProxyProbeLegacy(t *testing.T) {
	netnsTheReuseDiversionFollowsTheTProxyProbe(t, backendIPTablesLegacy)
}
