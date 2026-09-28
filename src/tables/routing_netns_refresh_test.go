package tables

import (
	"net"
	"strings"
	"testing"
	"time"
)

func netnsLearnedPresent(engine, setName, ip string) bool {
	if engine == backendNFTables {
		_, err := run("nft", "get", "element", "inet", routeNftTable, routeNftDynSet(setName), "{", ip, "}")
		return err == nil
	}
	_, err := run("ipset", "test", setName, ip)
	return err == nil
}

func TestNetnsALearnedAddressLivesOnWhileItIsReAdded(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequireEngine(t, engine)
			routeEngine = nil
			defer func() { routeEngine = nil }()
			defer RoutingClearAll()

			RoutingSyncConfig(netnsConfig(engine))
			st, ok := routeRuleCache["netns-egress-set"]
			if !ok || routeEngine == nil {
				t.Fatal("the routing set built no rules")
			}

			const ip, ttl = "198.51.100.77", 3
			routeEngine.addElements(st.setV4, []string{ip}, ttl)
			time.Sleep(2 * time.Second)
			routeEngine.addElements(st.setV4, []string{ip}, ttl)
			time.Sleep(2 * time.Second)
			if !netnsLearnedPresent(engine, st.setV4, ip) {
				t.Fatalf("%s expired %ds after its first add although it was added again after 2s", ip, ttl)
			}
			time.Sleep(time.Duration(ttl)*time.Second + 500*time.Millisecond)
			if netnsLearnedPresent(engine, st.setV4, ip) {
				t.Fatalf("%s outlived its timeout without a re-add, so the check above proves nothing", ip)
			}
		})
	}
}

func TestNetnsALearnedAddressLeavesAListedHostPermanent(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, backendIPTables)
	routeEngine = nil
	defer func() { routeEngine = nil }()
	defer RoutingClearAll()

	cfg := netnsConfig(backendIPTables)
	RoutingSyncConfig(cfg)
	st, ok := routeRuleCache["netns-egress-set"]
	if !ok {
		t.Fatal("the routing set built no rules")
	}
	routeAddResolvedIPs(cfg, cfg.Sets[0], []net.IP{net.ParseIP(netnsTarget)})

	listing := netnsRun(t, "ipset", "list", st.setV4)
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), netnsTarget+" ") && !strings.Contains(line, "timeout 0") {
			t.Fatalf("the listed host %s took the learned timeout and will expire: %q", netnsTarget, line)
		}
	}
	if !strings.Contains(listing, netnsTarget) {
		t.Fatalf("the listed host %s is missing:\n%s", netnsTarget, listing)
	}
}
