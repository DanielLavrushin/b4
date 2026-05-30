package mtproto

import (
	"testing"
	"time"
)

func TestBalancer_PenalizeCooldownSkipsDomain(t *testing.T) {
	b := newCFBalancer()
	all := b.domainsForDC(2)
	if len(all) < 2 {
		t.Fatalf("need >=2 default domains, got %d", len(all))
	}

	victim := all[0]
	b.penalize(victim, cfProxyDomainCooldown)

	got := b.domainsForDC(2)
	for _, d := range got {
		if d == victim {
			t.Fatalf("penalized domain %q should be skipped, got %v", victim, got)
		}
	}
	if len(got) != len(all)-1 {
		t.Fatalf("expected %d domains after cooldown, got %d", len(all)-1, len(got))
	}
}

func TestBalancer_AllCooledDownFallsBackToAll(t *testing.T) {
	b := newCFBalancer()
	all := b.domainsForDC(2)
	for _, d := range all {
		b.penalize(d, cfProxyDomainCooldown)
	}
	got := b.domainsForDC(2)
	if len(got) != len(all) {
		t.Fatalf("when all cooled, expect fallback to all %d, got %d", len(all), len(got))
	}
}

func TestBalancer_CooldownExpires(t *testing.T) {
	b := newCFBalancer()
	all := b.domainsForDC(2)
	victim := all[0]
	b.penalize(victim, -time.Second) // already expired
	got := b.domainsForDC(2)
	found := false
	for _, d := range got {
		if d == victim {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expired cooldown should restore %q, got %v", victim, got)
	}
}
