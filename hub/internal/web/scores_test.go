package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
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
	f.build()

	evidence := func(tab, id string) EvidenceView {
		t.Helper()
		var rows SetRowsView
		f.admin(http.MethodGet, PathAPI+"/sets/rows?tab="+tab, nil).decode(t, &rows)
		for _, row := range rows.Rows {
			if row.SetID == id {
				return row.Evidence
			}
		}
		t.Fatalf("no row of %s in %s", id, tab)
		return EvidenceView{}
	}
	cards := func(name string) {
		t.Helper()
		for _, id := range []string{listed, same} {
			if ev := evidence(TabListed, id); ev.Author.Works != 1 || ev.Author.Broken != 1 || ev.Author.Devices != 2 || ev.Independent.Works != 2 || ev.Independent.Broken != 0 || ev.Independent.Devices != 2 {
				t.Errorf("%s: both authors stand apart from the voter and the author of the rejected set: %+v", name, ev)
			}
		}
		if ev := evidence(TabRejected, dropped); ev.Author.Works != 2 || ev.Author.Broken != 1 || ev.Author.Devices != 3 || ev.Independent.Devices != 1 {
			t.Errorf("%s: a rejected set's own author is no independent device on its card: %+v", name, ev)
		}
	}
	broken := testkit.Sign(t, first, hubwire.RecordVote, hubwire.VoteBody{SetID: same, Version: 1, FP: fp, Kind: hubwire.VoteBroken}, f.clock)
	if resp := f.ingest.Handle(context.Background(), broken, parseIP(authorAddress)); resp.Status != http.StatusAccepted {
		t.Fatalf("vote: %d %v", resp.Status, resp.Body)
	}
	f.voteAs(second, listed, 1, fp, otherAddress)
	f.voteAs(voter, listed, 1, fp, voterAddress)
	cards("the rejected author's vote names the rejected set")
	f.voteAs(rejected, listed, 1, fp, thirdAddress)
	cards("the rejected author's vote names a listed set")

	votes := func(query string) VotesPageView {
		t.Helper()
		var page VotesPageView
		f.admin(http.MethodGet, PathAPI+"/votes"+query, nil).decode(t, &page)
		return page
	}
	authors := map[string]bool{f.keyOf(first): true, f.keyOf(second): true}
	for query, want := range map[string]int{
		"":                                       4,
		"?author=only":                           2,
		"?author=exclude":                        2,
		"?author=only&set=" + listed:             1,
		"?author=only&set=" + same:               1,
		"?author=exclude&set=" + listed:          2,
		"?author=exclude&set=" + same:            0,
		"?author=only&sign=broken":               1,
		"?author=only&key=" + f.keyOf(voter):     0,
		"?author=exclude&key=" + f.keyOf(second): 0,
	} {
		page := votes(query)
		if page.Total != want || len(page.Items) != want {
			t.Errorf("%q must select %d votes, got %d of %d", query, want, len(page.Items), page.Total)
		}
		for _, row := range page.Items {
			if row.AuthorVote != authors[row.KeyHMAC] {
				t.Errorf("%q: the author mark of %s is %v", query, row.KeyHMAC, row.AuthorVote)
			}
			if strings.Contains(query, "author=") && row.AuthorVote != strings.Contains(query, "author=only") {
				t.Errorf("%q must not select a vote whose author mark is %v", query, row.AuthorVote)
			}
		}
	}
	page := votes("?author=only&limit=1")
	next := votes("?author=only&limit=1&before=" + page.Next)
	if len(page.Items) != 1 || len(next.Items) != 1 || page.Items[0].KeyHMAC == next.Items[0].KeyHMAC || next.Next != "" {
		t.Errorf("paging must show each author's vote once: %+v then %+v", page, next)
	}
}
