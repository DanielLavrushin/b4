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

func TestFindOptimalTTLConfirmsTheMemoisedSweepOnce(t *testing.T) {
	ds := &DiscoverySuite{optimalTTL: 7, ttlProbed: true}
	base := ConfigPreset{Name: "combo-optimize", Config: baseConfig()}
	base.Config.Faking.Strategy = "pastseq"
	preset, ok := ds.memoisedTTLPreset(base)
	if !ok {
		t.Fatal("a sweep that found a TTL must hand later families that TTL")
	}
	if preset.Name != "combo-optimize-ttl7-confirm" || preset.Config.Faking.TTL != 7 || preset.Config.Faking.Strategy != "ttl" {
		t.Fatalf("confirm preset = %s ttl=%d strategy=%s, want combo-optimize-ttl7-confirm with the ttl strategy on the caller's own base, so it never overwrites the sweep's own entry", preset.Name, preset.Config.Faking.TTL, preset.Config.Faking.Strategy)
	}
	if ds.ttlSweepChecks() != 1 {
		t.Fatalf("a memoised sweep pre-counts the one confirming fetch, got %d", ds.ttlSweepChecks())
	}
	if _, ok := (&DiscoverySuite{ttlProbed: true}).memoisedTTLPreset(base); ok {
		t.Fatal("a failed sweep memoises nothing to confirm")
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
