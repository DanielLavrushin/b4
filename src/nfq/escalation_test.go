package nfq

import (
	"testing"
	"time"
)

func newDestState() *destStateTracker {
	return &destStateTracker{
		conns:       make(map[string]*ipBlockEntry),
		blocked:     make(map[string]time.Time),
		escalations: make(map[string]*escalationEntry),
		rstKills:    make(map[string]*failEntry),
		dnsFails:    make(map[string]*failEntry),
	}
}

func TestEscalation_GetMissReturnsFalse(t *testing.T) {
	b := newDestState()
	if id, hops, ok := b.GetEscalation("youtube.com"); ok || id != "" || hops != 0 {
		t.Fatalf("expected miss, got id=%q hops=%d ok=%v", id, hops, ok)
	}
}

func TestEscalation_SetThenGet(t *testing.T) {
	b := newDestState()
	if !b.SetEscalation("youtube.com", "set-b", "test", 0) {
		t.Fatal("SetEscalation should succeed on first hop")
	}
	id, hops, ok := b.GetEscalation("youtube.com")
	if !ok || id != "set-b" || hops != 1 {
		t.Fatalf("expected set-b hop=1, got id=%q hops=%d ok=%v", id, hops, ok)
	}
}

func TestEscalation_ChainIncrementsHops(t *testing.T) {
	b := newDestState()
	b.SetEscalation("youtube.com", "set-b", "test", 0)
	b.SetEscalation("youtube.com", "set-c", "test", 0)
	id, hops, ok := b.GetEscalation("youtube.com")
	if !ok || id != "set-c" || hops != 2 {
		t.Fatalf("expected set-c hop=2, got id=%q hops=%d ok=%v", id, hops, ok)
	}
}

func TestEscalation_StopsAtMaxHops(t *testing.T) {
	b := newDestState()
	for i := 0; i < MaxEscalationHops; i++ {
		if !b.SetEscalation("youtube.com", "set-x", "test", 0) {
			t.Fatalf("hop %d should still be allowed", i)
		}
	}
	if b.SetEscalation("youtube.com", "set-y", "test", 0) {
		t.Fatal("escalation past MaxEscalationHops must be rejected")
	}
}

func TestEscalation_Clear(t *testing.T) {
	b := newDestState()
	b.SetEscalation("youtube.com", "set-b", "test", 0)
	b.ClearEscalation("youtube.com")
	if _, _, ok := b.GetEscalation("youtube.com"); ok {
		t.Fatal("ClearEscalation should drop the entry")
	}
}

func TestEscalation_Reset(t *testing.T) {
	b := newDestState()
	b.SetEscalation("youtube.com", "set-b", "test", 0)
	b.SetEscalation("discord.com", "set-c", "test", 0)
	b.ResetEscalations()
	if _, _, ok := b.GetEscalation("youtube.com"); ok {
		t.Fatal("ResetEscalations should drop all entries")
	}
	if _, _, ok := b.GetEscalation("discord.com"); ok {
		t.Fatal("ResetEscalations should drop all entries")
	}
}

func TestEscalation_SetAfterExpiryRestartsChain(t *testing.T) {
	b := newDestState()
	b.SetEscalation("youtube.com", "set-b", "test", 0)
	b.SetEscalation("youtube.com", "set-c", "test", 0)
	// chain is at hops=2; backdate it past the default TTL
	b.mu.Lock()
	b.escalations["youtube.com"].setAt = time.Now().Add(-EscalationTTL - time.Minute)
	b.mu.Unlock()

	if !b.SetEscalation("youtube.com", "set-d", "test", 0) {
		t.Fatal("SetEscalation must succeed when prev entry is past its TTL")
	}
	id, hops, ok := b.GetEscalation("youtube.com")
	if !ok || id != "set-d" || hops != 1 {
		t.Fatalf("expected chain restart at hops=1, got id=%q hops=%d ok=%v", id, hops, ok)
	}
}

func TestEscalation_ExpiresAfterTTL(t *testing.T) {
	b := newDestState()
	b.SetEscalation("youtube.com", "set-b", "test", 0)
	// Manually backdate the entry past the TTL.
	b.mu.Lock()
	b.escalations["youtube.com"].setAt = time.Now().Add(-EscalationTTL - time.Minute)
	b.mu.Unlock()

	if _, _, ok := b.GetEscalation("youtube.com"); ok {
		t.Fatal("expired escalation must not be returned")
	}
}

func TestEscalation_CleanupRemovesExpired(t *testing.T) {
	b := newDestState()
	b.SetEscalation("a:1", "x", "test", 0)
	b.SetEscalation("b:2", "y", "test", 0)
	b.mu.Lock()
	b.escalations["a:1"].setAt = time.Now().Add(-EscalationTTL - time.Minute)
	b.mu.Unlock()

	b.Cleanup(0)

	b.mu.RLock()
	_, hasA := b.escalations["a:1"]
	_, hasB := b.escalations["b:2"]
	b.mu.RUnlock()
	if hasA {
		t.Fatal("Cleanup should drop expired entry a:1")
	}
	if !hasB {
		t.Fatal("Cleanup should keep fresh entry b:2")
	}
}

func TestEscalation_DoesNotInterfereWithBlockedCache(t *testing.T) {
	b := newDestState()
	b.AddBlocked("9.9.9.9:443")
	b.SetEscalation("youtube.com", "set-b", "test", 0)

	if !b.IsBlocked("9.9.9.9:443") {
		t.Fatal("blocked IP should still be reported as blocked")
	}
	if b.IsBlocked("youtube.com") {
		t.Fatal("escalated host must not be reported as blocked")
	}
}

func TestRSTKill_BelowThresholdReturnsFalse(t *testing.T) {
	b := newDestState()
	for i := 0; i < RSTKillThreshold-1; i++ {
		if b.RecordRSTKill("youtube.com", 0, 0) {
			t.Fatalf("hit %d should not trip threshold (= %d)", i+1, RSTKillThreshold)
		}
	}
}

func TestRSTKill_TripsAtThreshold(t *testing.T) {
	b := newDestState()
	var tripped bool
	for i := 0; i < RSTKillThreshold; i++ {
		tripped = b.RecordRSTKill("youtube.com", 0, 0)
	}
	if !tripped {
		t.Fatalf("threshold (%d) should have tripped", RSTKillThreshold)
	}
}

func TestRSTKill_ResetsAfterTrip(t *testing.T) {
	b := newDestState()
	for i := 0; i < RSTKillThreshold; i++ {
		b.RecordRSTKill("youtube.com", 0, 0)
	}
	if b.RecordRSTKill("youtube.com", 0, 0) {
		t.Fatal("immediate next kill after trip must NOT re-fire (would spam escalations)")
	}
}

func TestRSTKill_WindowExpiry(t *testing.T) {
	b := newDestState()
	b.RecordRSTKill("youtube.com", 0, 0)
	// Backdate the entry past the rolling window.
	b.mu.Lock()
	b.rstKills["youtube.com"].firstAt = time.Now().Add(-RSTKillWindow - time.Second)
	b.mu.Unlock()
	// Next kill is treated as a fresh start, not as count=2.
	if b.RecordRSTKill("youtube.com", 0, 0) {
		t.Fatal("kill after window expiry must restart counting, not trip")
	}
	b.mu.RLock()
	count := b.rstKills["youtube.com"].count
	b.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected counter reset to 1 after window expiry, got %d", count)
	}
}

func TestRSTKill_DistinctDestinationsTrackedSeparately(t *testing.T) {
	b := newDestState()
	for i := 0; i < RSTKillThreshold-1; i++ {
		b.RecordRSTKill("youtube.com", 0, 0)
	}
	if b.RecordRSTKill("discord.com", 0, 0) {
		t.Fatal("first kill on a different destination must not trip")
	}
}

func TestRSTKill_ResetEscalationsClearsKills(t *testing.T) {
	b := newDestState()
	b.RecordRSTKill("youtube.com", 0, 0)
	b.ResetEscalations()
	b.mu.RLock()
	_, has := b.rstKills["youtube.com"]
	b.mu.RUnlock()
	if has {
		t.Fatal("ResetEscalations should also drop RST-kill state")
	}
}
