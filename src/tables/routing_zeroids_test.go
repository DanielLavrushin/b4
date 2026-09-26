package tables

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func zeroIDsTakeEveryTable(t *testing.T) {
	t.Helper()
	for table := 100; table <= 249; table++ {
		id := fmt.Sprintf("other%d", table)
		routeRuleCache[id] = routeState{setID: id, mode: config.RoutingModeInterface, mark: uint32(0x100 + table), table: table}
	}
}

func zeroIDsRecord(t *testing.T) *[]string {
	t.Helper()
	origRun := run
	t.Cleanup(func() { run = origRun })
	var ops []string
	run = func(args ...string) (string, error) {
		ops = append(ops, strings.Join(args, " "))
		return "", fmt.Errorf("stubbed")
	}
	runLogged = func(op string, args ...string) bool {
		ops = append(ops, strings.Join(args, " "))
		return true
	}
	routeDelRuleLoop = func(ipv6 bool, mark, table string) {
		ops = append(ops, fmt.Sprintf("ip rule del fwmark %s lookup %s", mark, table))
	}
	return &ops
}

func zeroIDsAssertNothingTouchedTableZero(t *testing.T, ops []string) {
	t.Helper()
	var bad []string
	for _, op := range ops {
		f := strings.Fields(op)
		for i := 0; i+1 < len(f); i++ {
			if ((f[i] == "table" || f[i] == "lookup") && f[i+1] == "0") || (f[i] == "fwmark" && strings.HasPrefix(f[i+1], "0x0/")) {
				bad = append(bad, op)
				break
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("table 0 is the kernel's 'no table' and fwmark 0 matches unmarked traffic, so these act on the main table or on rules b4 never added (%d of them): %q", len(bad), bad[0])
	}
}

func TestRoutingHandleDNSSkipsASetWithoutARoutingTable(t *testing.T) {
	familyResetGlobals(t)
	stubBinaries(t, "ip")
	ops := zeroIDsRecord(t)
	var chains []string
	routeEngine = &mockRouteBackend{ensureChainFn: func(chain string, isMangle bool) error {
		chains = append(chains, chain)
		return nil
	}}
	zeroIDsTakeEveryTable(t)

	set := familyTestSet()
	set.Routing.FWMark = 0
	set.Routing.Table = 0
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.7")})

	if _, cached := routeRuleCache[set.Id]; cached {
		t.Errorf("a set with no routing table of its own was installed from a DNS answer")
	}
	if len(chains) != 0 {
		t.Errorf("a set with no routing table of its own had its chains built from a DNS answer: %v", chains)
	}
	zeroIDsAssertNothingTouchedTableZero(t, *ops)
}

func TestRouteEnsureRuleRefusesMarkOrTableZero(t *testing.T) {
	familyResetGlobals(t)
	stubBinaries(t, "ip")
	ops := zeroIDsRecord(t)
	be := &mockRouteBackend{}
	set := familyTestSet()
	cfg := familyTestConfig(true, false)

	for _, ids := range []struct {
		mark  uint32
		table int
	}{{0, 0}, {0x7e10, 0}, {0, 232}} {
		st := buildRouteState(cfg, set)
		st.mark, st.table = ids.mark, ids.table
		if err := routeEnsureRule(be, cfg, set, st, nil); err == nil {
			t.Errorf("mark 0x%x table %d was accepted", ids.mark, ids.table)
		}
	}
	if len(be.chainOps) != 0 || len(be.jumps) != 0 {
		t.Errorf("a refused set still wrote rules: %v %v", be.chainOps, be.jumps)
	}
	zeroIDsAssertNothingTouchedTableZero(t, *ops)
}

func TestRuleDeletionRefusesMarkOrTableZero(t *testing.T) {
	familyResetGlobals(t)
	ops := zeroIDsRecord(t)

	routeDelRuleLoopExec(false, "0x0/0x0", "0")
	routeDelRuleLoopExec(true, "0x66/0x27fff", "0")
	routeDelRuleLoopExec(false, "0x0/0x27fff", "120")
	routeDelRuleLoopExec(false, "0x66/0x27fff", "")
	routeDeleteOwnRoutes("wg0", "0")
	if len(*ops) != 0 {
		t.Errorf("with mark 0 or table 0 `ip rule del` removes every rule, main and local included, and `ip route del default` works on the main table; b4 ran %v", *ops)
	}

	routeDelRuleLoopExec(false, "0x66/0x27fff", "120")
	routeDeleteOwnRoutes("wg0", "120")
	if len(*ops) == 0 {
		t.Errorf("a real mark and table must still be cleaned up")
	}
	zeroIDsAssertNothingTouchedTableZero(t, *ops)
}

func TestRoutingHandleDNSKeepsTheWorkingStateOfASetThatLostItsTable(t *testing.T) {
	familyResetGlobals(t)
	stubBinaries(t, "ip")
	ops := zeroIDsRecord(t)
	be := &mockRouteBackend{}
	routeEngine = be

	set := familyTestSet()
	installed := buildRouteState(familyTestConfig(true, true), set)
	routeRuleCache[set.Id] = installed
	zeroIDsTakeEveryTable(t)

	set.Routing.FWMark = 0
	set.Routing.Table = 0
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.7")})

	if len(be.deletedJumps) != 0 {
		t.Errorf("a set that cannot get a table must keep the rules it already has, yet its jumps were removed: %+v", be.deletedJumps)
	}
	for _, op := range *ops {
		if strings.HasPrefix(op, "ip rule del") {
			t.Errorf("a set that cannot get a table must keep the rules it already has, yet b4 ran %q", op)
		}
	}
	if got := routeRuleCache[set.Id]; !routeStateEqual(got, installed) {
		t.Errorf("the cached state no longer describes the rules that are installed: %+v", got)
	}
}

func TestRoutingHandleDNSStillInstallsABlockSet(t *testing.T) {
	familyResetGlobals(t)
	stubBinaries(t, "ip")
	zeroIDsRecord(t)
	run = func(args ...string) (string, error) { return "", nil }
	routeEngine = &mockRouteBackend{}
	zeroIDsTakeEveryTable(t)

	set := familyTestSet()
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.FWMark = 0
	set.Routing.Table = 0
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{set}

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.7")})

	if _, cached := routeRuleCache[set.Id]; !cached {
		t.Errorf("a block set has no mark or table of its own by design, so the table check must not keep it from installing")
	}
}
