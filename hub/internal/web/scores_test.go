package web

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestARestoreIsNotAVote(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Old", authorAddress, "old.example")
	f.approve(id)
	f.clock = f.clock.Add(400 * 24 * time.Hour)
	if n, err := f.store.PruneVotes(context.Background(), f.clock, 365*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("the upload vote must expire, got %d %v", n, err)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionHide), map[string]string{"reason": "checking"}))
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, ActionRestore), nil))
	f.build()
	m, err := f.web.attentionMap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if codes := m[id]; len(codes) != 1 || codes[0] != AttentionStale {
		t.Errorf("a set without a vote for a year is stale, restored or not, got %v", codes)
	}
	if n := f.web.scoreStats(f.clock).Stale; n != 1 {
		t.Errorf("the statistics must count it, got %d", n)
	}
}
