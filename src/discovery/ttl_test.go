package discovery

import "testing"

func TestGetOptimalTTLReturnsCachedWinner(t *testing.T) {
	ds := &DiscoverySuite{optimalTTL: 5}
	ttl, ok := ds.getOptimalTTL()
	if !ok || ttl != 5 {
		t.Fatalf("got (%d, %v), want (5, true)", ttl, ok)
	}
}

func TestGetOptimalTTLDoesNotInventOneAfterAFailedSweep(t *testing.T) {
	ds := &DiscoverySuite{ttlProbed: true}
	ttl, ok := ds.getOptimalTTL()
	if ok {
		t.Fatal("a sweep where every TTL failed must not report a usable TTL")
	}
	if ttl != 0 {
		t.Fatalf("got TTL %d, want 0; picking a value is what sent a harmful fake to the censor", ttl)
	}
}

func TestGetOptimalTTLDoesNotRepeatAFailedSweep(t *testing.T) {
	ds := &DiscoverySuite{ttlProbed: true}
	for i := 0; i < 3; i++ {
		if _, ok := ds.getOptimalTTL(); ok {
			t.Fatal("repeated calls must keep reporting no usable TTL")
		}
	}
	if ds.optimalTTL != 0 {
		t.Fatalf("optimalTTL drifted to %d", ds.optimalTTL)
	}
}
