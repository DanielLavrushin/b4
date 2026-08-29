package tables

import "testing"

func rpFilterHarness(t *testing.T, initial map[string]string) map[string]string {
	t.Helper()
	state := make(map[string]string, len(initial))
	for k, v := range initial {
		state[k] = v
	}
	oldRead, oldWrite := routeReadRPFilter, routeWriteRPFilter
	routeReadRPFilter = func(iface string) (string, bool) {
		v, ok := state[iface]
		return v, ok
	}
	routeWriteRPFilter = func(iface, value string) bool {
		if _, ok := state[iface]; !ok {
			return false
		}
		state[iface] = value
		return true
	}
	routeForgetRPFilterState()
	t.Cleanup(func() {
		routeReadRPFilter, routeWriteRPFilter = oldRead, oldWrite
		routeForgetRPFilterState()
	})
	return state
}

func TestLoosenRPFilterRelaxesTheInterfaceAndLeavesAllAlone(t *testing.T) {
	state := rpFilterHarness(t, map[string]string{"all": "1", "xray0": "1"})

	routeLoosenRPFilter("xray0", "set-a")
	if state["xray0"] != "2" {
		t.Fatalf("strict reverse-path filtering must be relaxed on the set's interface, got %v", state)
	}
	if state["all"] != "1" {
		t.Fatalf("relaxing every interface on the box to route one set is a spoofing regression, and the "+
			"kernel takes the higher of all and the device, so the device alone is enough: %v", state)
	}

	routeReleaseRPFilter("xray0", "set-a")
	if state["xray0"] != "1" {
		t.Fatalf("the previous value must come back when the last set stops using the interface, got %v", state)
	}
}

func TestLoosenRPFilterActsOnTheEffectiveValueNotTheInterfaceAlone(t *testing.T) {
	state := rpFilterHarness(t, map[string]string{"all": "1", "xray0": "0"})

	routeLoosenRPFilter("xray0", "set-a")
	if state["xray0"] != "2" {
		t.Fatalf("the kernel validates against the higher of all and the device, so a device reading 0 under "+
			"an all of 1 is still filtering strictly and must be relaxed: %v", state)
	}
}

func TestLoosenRPFilterWorksWhereAllReadsSomethingUnexpected(t *testing.T) {
	state := rpFilterHarness(t, map[string]string{"all": "-1", "xray0": "1"})

	routeLoosenRPFilter("xray0", "set-a")
	if state["xray0"] != "2" {
		t.Fatalf("ASUS Merlin reports -1 for all, and taking that as the whole answer left the relaxation "+
			"dead on that entire platform: %v", state)
	}
}

func TestLoosenRPFilterLeavesADisabledOrLooseSettingAlone(t *testing.T) {
	state := rpFilterHarness(t, map[string]string{"all": "0", "wg0": "2"})

	routeLoosenRPFilter("wg0", "set-a")
	if state["all"] != "0" || state["wg0"] != "2" {
		t.Fatalf("only strict filtering is touched, got %v", state)
	}

	routeReleaseRPFilter("wg0", "set-a")
	if state["all"] != "0" || state["wg0"] != "2" {
		t.Fatalf("nothing was changed, so nothing may be restored, got %v", state)
	}
}

func TestLoosenRPFilterHoldsWhileAnotherSetStillNeedsIt(t *testing.T) {
	state := rpFilterHarness(t, map[string]string{"all": "1", "xray0": "1"})

	routeLoosenRPFilter("xray0", "set-a")
	routeLoosenRPFilter("xray0", "set-b")

	routeReleaseRPFilter("xray0", "set-a")
	if state["xray0"] != "2" {
		t.Fatalf("set-b still routes out xray0, so the relaxation must stand, got %v", state)
	}

	routeReleaseRPFilter("xray0", "set-b")
	if state["xray0"] != "1" {
		t.Fatalf("the last set is gone, so the original value must come back, got %v", state)
	}
}

func TestLoosenRPFilterIgnoresAnInterfaceWithNoSuchKnob(t *testing.T) {
	state := rpFilterHarness(t, map[string]string{"all": "1"})

	routeLoosenRPFilter("ppp0", "set-a")
	if state["all"] != "1" {
		t.Fatalf("an interface b4 cannot read must not cause a box-wide change, got %v", state)
	}
	if _, ok := state["ppp0"]; ok {
		t.Fatalf("the harness must not invent a knob that does not exist")
	}

	routeReleaseRPFilter("ppp0", "set-a")
}

func TestLoosenRPFilterIgnoresAnEmptyInterfaceOrSet(t *testing.T) {
	state := rpFilterHarness(t, map[string]string{"all": "1", "xray0": "1"})

	routeLoosenRPFilter("", "set-a")
	routeLoosenRPFilter("xray0", "")
	if state["all"] != "1" || state["xray0"] != "1" {
		t.Fatalf("a proxy set has no egress interface, so nothing may be touched, got %v", state)
	}
}
