package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func egressSet(id, iface, egressIP string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeInterface
	set.Routing.EgressInterface = iface
	set.Routing.EgressIP = egressIP
	return &set
}

func resolveFresh(t *testing.T, sets ...*config.SetConfig) []routeState {
	t.Helper()

	origCache := routeRuleCache
	origAuto := routeIfaceAuto
	t.Cleanup(func() {
		routeRuleCache = origCache
		routeIfaceAuto = origAuto
	})
	routeRuleCache = make(map[string]routeState)
	routeIfaceAuto = make(map[string]routeState)

	cfg := config.NewConfig()
	out := make([]routeState, 0, len(sets))
	for _, set := range sets {
		mark, table := routeResolveIDs(&cfg, set)
		st := routeState{
			mark:      mark,
			table:     table,
			iface:     set.Routing.EgressInterface,
			egressIP:  set.Routing.EgressIP,
			setV4:     "b4r_" + set.Id + "_v4",
			chainSNAT: "b4r_" + set.Id + "_snat",
		}
		routeRuleCache[set.Id] = st
		out = append(out, st)
	}
	return out
}

func TestDifferentEgressIPsOnOneInterfaceGetTheirOwnMarkAndTable(t *testing.T) {
	got := resolveFresh(t,
		egressSet("phone", "eth0", "192.0.2.51"),
		egressSet("laptop", "eth0", "192.0.2.52"),
	)

	if got[0].mark == got[1].mark {
		t.Errorf("both sets got mark 0x%x, so the routing table and the SNAT rule cannot tell their traffic apart", got[0].mark)
	}
	if got[0].table == got[1].table {
		t.Errorf("both sets got table %d, so whichever is written last owns the default route source for both", got[0].table)
	}
}

func TestSameEgressIPOnOneInterfaceStillSharesAMark(t *testing.T) {
	got := resolveFresh(t,
		egressSet("a", "eth0", "192.0.2.51"),
		egressSet("b", "eth0", "192.0.2.51"),
	)

	if got[0].mark != got[1].mark || got[0].table != got[1].table {
		t.Errorf("sets that leave with the same source address should share a mark and table, got 0x%x/%d and 0x%x/%d",
			got[0].mark, got[0].table, got[1].mark, got[1].table)
	}
}

func TestSetsWithNoEgressIPStillShareAMark(t *testing.T) {
	got := resolveFresh(t,
		egressSet("a", "eth0", ""),
		egressSet("b", "eth0", ""),
	)

	if got[0].mark != got[1].mark || got[0].table != got[1].table {
		t.Errorf("plain interface sets should keep sharing one mark and table, got 0x%x/%d and 0x%x/%d",
			got[0].mark, got[0].table, got[1].mark, got[1].table)
	}
}

func TestAllocatedMarksNeverOverlap(t *testing.T) {
	got := resolveFresh(t,
		egressSet("a", "eth0", "192.0.2.51"),
		egressSet("b", "eth0", "192.0.2.52"),
		egressSet("c", "eth0", "192.0.2.53"),
		egressSet("d", "eth1", "192.0.2.54"),
		egressSet("e", "eth1", ""),
	)

	for i := range got {
		for j := range got {
			if i == j {
				continue
			}
			a, b := got[i].mark, got[j].mark
			if a&b == b {
				t.Errorf("mark 0x%x carries every bit of 0x%x, so a packet marked for one set matches the other's fwmark rule and SNAT rule", a, b)
			}
		}
	}
}

func TestSNATRuleCarriesTheSetMark(t *testing.T) {
	states := resolveFresh(t,
		egressSet("phone", "lo", "127.0.0.1"),
		egressSet("laptop", "lo", "127.0.0.2"),
	)

	origOnIface := routeEgressIPOnIface
	t.Cleanup(func() { routeEgressIPOnIface = origOnIface })
	routeEgressIPOnIface = func(iface, egressIP string) bool { return true }

	be := &mockRouteBackend{}
	for _, st := range states {
		routeAddEgressRules(be, st, true, false)
	}

	if len(be.snat) != 2 {
		t.Fatalf("expected one SNAT rule per set, got %+v", be.snat)
	}
	if be.snat[0].mark == 0 || be.snat[1].mark == 0 {
		t.Fatalf("SNAT rules must carry the set's mark so identity survives an overlapping destination list: %+v", be.snat)
	}
	if be.snat[0].mark == be.snat[1].mark {
		t.Errorf("both SNAT rules match mark 0x%x, so the first one rewrites the other set's traffic too", be.snat[0].mark)
	}
	if be.snat[0].srcIP == be.snat[1].srcIP {
		t.Errorf("expected a distinct source per set, got %q twice", be.snat[0].srcIP)
	}
}
