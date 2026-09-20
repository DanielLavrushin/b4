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

func TestFindOptimalTTLReturnsTheMemoisedSweep(t *testing.T) {
	ds := &DiscoverySuite{optimalTTL: 7, optimalTTLSpeed: 4096, ttlProbed: true}
	ttl, speed := ds.findOptimalTTL(ConfigPreset{Name: "combo-optimize"})
	if ttl != 7 || speed != 4096 {
		t.Fatalf("got (%d, %.0f), want the first sweep's (7, 4096) without a second sweep", ttl, speed)
	}
	if ds.ttlSweepChecks() != 0 {
		t.Fatalf("a memoised sweep must not pre-count checks, got %d", ds.ttlSweepChecks())
	}
}

func TestFindOptimalTTLDoesNotRepeatAFailedSweep(t *testing.T) {
	ds := &DiscoverySuite{ttlProbed: true}
	for i := 0; i < 2; i++ {
		ttl, speed := ds.findOptimalTTL(ConfigPreset{Name: "fake-optimize"})
		if ttl != 0 || speed != 0 {
			t.Fatalf("got (%d, %.0f), want (0, 0) after a sweep where every TTL failed", ttl, speed)
		}
	}
	if ds.ttlSweepChecks() != 0 {
		t.Fatalf("a skipped sweep must not pre-count checks, got %d", ds.ttlSweepChecks())
	}
}

func TestTTLSweepChecksCountsTheFirstSweepOnly(t *testing.T) {
	ds := &DiscoverySuite{}
	if got := ds.ttlSweepChecks(); got != len(ttlSweepValues) {
		t.Fatalf("first sweep should pre-count %d checks, got %d", len(ttlSweepValues), got)
	}
	ds.ttlProbed = true
	if got := ds.ttlSweepChecks(); got != 0 {
		t.Fatalf("second sweep should pre-count 0 checks, got %d", got)
	}
}
