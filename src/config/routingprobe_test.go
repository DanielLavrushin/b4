package config

import "testing"

func TestRoutingLiveDefaultsToTrue(t *testing.T) {
	SetRoutingLiveProbe(nil)
	if !RoutingLive("any-set") {
		t.Fatal("without a probe every set is treated as routed")
	}
}

func TestRoutingLiveAsksTheProbe(t *testing.T) {
	SetRoutingLiveProbe(func(setID string) bool { return setID == "routed" })
	t.Cleanup(func() { SetRoutingLiveProbe(nil) })

	if !RoutingLive("routed") {
		t.Fatal("a routed set was reported as unrouted")
	}
	if RoutingLive("missing") {
		t.Fatal("an unrouted set was reported as routed")
	}
}
