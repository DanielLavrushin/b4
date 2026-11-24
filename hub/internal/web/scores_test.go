package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/daniellavrushin/b4hub/internal/testkit"
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

func TestEveryAuthorOfAStrategyIsMarkedAsOne(t *testing.T) {
	f := newFixture(t, password)
	first, second, rejected, voter := testkit.Identity(t), testkit.Identity(t), testkit.Identity(t), testkit.Identity(t)
	listed, _ := f.shareAs(first, testkit.SampleSet("YouTube", "youtube.com"), authorAddress)
	f.approve(listed)
	same, _ := f.shareAs(second, testkit.SampleSet("Discord", "discord.com"), otherAddress)
	f.approve(same)
	dropped, _ := f.shareAs(rejected, testkit.SampleSet("Signal", "signal.org"), thirdAddress)
	if err := f.store.Reject(context.Background(), dropped, 1, "duplicate", f.clock); err != nil {
		t.Fatal(err)
	}
	fp := f.versionOf(listed, 1).FP
	if f.versionOf(same, 1).FP != fp || f.versionOf(dropped, 1).FP != fp {
		t.Fatal("the three sets must share a strategy")
	}
	f.voteAs(second, listed, 1, fp, otherAddress)
	f.voteAs(voter, listed, 1, fp, voterAddress)
	f.build()

	evidence := func(group, id string) EvidenceView {
		t.Helper()
		var rows SetRowsView
		f.admin(http.MethodGet, PathAPI+"/sets/rows?group="+group, nil).decode(t, &rows)
		for _, row := range rows.Rows {
			if row.SetID == id {
				return row.Evidence
			}
		}
		t.Fatalf("no row of %s in %s", id, group)
		return EvidenceView{}
	}
	for _, id := range []string{listed, same} {
		if ev := evidence(GroupListed, id); ev.Author.Works != 2 || ev.Author.Devices != 2 || ev.Independent.Works != 2 || ev.Independent.Devices != 2 {
			t.Errorf("both authors stand apart from the voter and the author of the rejected set: %+v", ev)
		}
	}
	if ev := evidence(GroupRejected, dropped); ev.Author.Devices != 3 || ev.Independent.Devices != 1 {
		t.Errorf("a rejected set's own author is no independent device on its card: %+v", ev)
	}

	wants := map[string]bool{f.keyOf(first): true, f.keyOf(second): true, f.keyOf(rejected): true, f.keyOf(voter): false}
	for query, want := range map[string]int{"": 4, "?author=only": 3, "?author=exclude": 1} {
		var page VotesPageView
		f.admin(http.MethodGet, PathAPI+"/votes"+query, nil).decode(t, &page)
		if page.Total != want || len(page.Items) != want {
			t.Errorf("%q must select %d votes, got %d of %d", query, want, len(page.Items), page.Total)
		}
		for _, row := range page.Items {
			if row.AuthorVote != wants[row.KeyHMAC] {
				t.Errorf("%q: the author mark of %s is %v", query, row.KeyHMAC, row.AuthorVote)
			}
			if query != "" && row.AuthorVote != (query == "?author=only") {
				t.Errorf("%q must not select a vote whose author mark is %v", query, row.AuthorVote)
			}
		}
	}
}
