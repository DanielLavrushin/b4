package web

import (
	"net/http"
	"testing"
)

func TestStatsCountActivityPerDay(t *testing.T) {
	f := newFixture(t, password)
	id, env := f.share("Stats", authorAddress, "stats.example")
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "approve"), nil))
	f.build()
	f.vote(id, 1, env.Fingerprint, voterAddress)
	f.vote(id, 1, env.Fingerprint, otherAddress)
	f.build()

	var view StatsView
	resp := f.admin(http.MethodGet, PathAPI+"/stats?days=7", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("stats: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &view)
	if view.Days != 7 || len(view.Daily) != 7 || view.To != f.clock.UTC().Format("2006-01-02") {
		t.Fatalf("seven days ending today: %+v", view)
	}
	tot := view.Totals
	if tot.Shares != 1 || tot.Works != 2 || tot.Approved != 1 || tot.NewKeys != 3 || tot.Builds < 2 {
		t.Fatalf("totals: %+v", tot)
	}
	if view.Daily[6].Works != 2 || view.Coverage.Votes != 2 || view.Coverage.Devices != 2 {
		t.Fatalf("today and coverage: %+v %+v", view.Daily[6], view.Coverage)
	}
	if len(view.ASNs) != 2 || len(view.Countries) != 2 || len(view.TopSets) != 1 || view.TopSets[0].Title != "Stats" || view.TopSets[0].Keys != 2 {
		t.Fatalf("mixes: %+v %+v %+v", view.ASNs, view.Countries, view.TopSets)
	}
	if view.Scores.Listed != 1 || view.Scores.Rated+view.Scores.Bins[0].LowN+sumLowN(view.Scores.Bins) == 0 {
		t.Fatalf("score distribution: %+v", view.Scores)
	}
	if resp := f.admin(http.MethodGet, PathAPI+"/stats?days=400", nil); resp.status != http.StatusBadRequest {
		t.Fatalf("days out of range must be refused, got %d", resp.status)
	}
}

func sumLowN(bins []ScoreBinView) int {
	n := 0
	for _, b := range bins {
		n += b.LowN + b.Sets
	}
	return n
}

func TestHealthReportsCodedIssues(t *testing.T) {
	f := newFixture(t, password)
	var view HealthView
	f.admin(http.MethodGet, PathAPI+"/health", nil).decode(t, &view)
	if view.Worst != SeverityError || !hasIssue(view, IssueNotPublished) {
		t.Fatalf("an unpublished hub is an error: %+v", view)
	}
	f.share("Waiting", authorAddress, "waiting.example")
	f.build()
	f.clock = f.clock.Add(80 * 60 * 60 * 1e9)
	f.admin(http.MethodGet, PathAPI+"/health", nil).decode(t, &view)
	if hasIssue(view, IssueNotPublished) || !hasIssue(view, IssueQueueOld) || view.Pending.Count != 1 || view.Pending.Oldest == nil || view.Pending.Oldest.Title != "Waiting" {
		t.Fatalf("an old pending share is flagged: %+v", view)
	}
	if !hasIssue(view, IssueBuildOverdue) || view.Manifest.ExpiresInS <= 0 {
		t.Fatalf("a build older than a day and a bit is overdue: %+v", view)
	}
	if view.BuildInfo.Version != "test" || view.BuildInfo.Dev {
		t.Fatalf("build info: %+v", view.BuildInfo)
	}
}

func hasIssue(v HealthView, code string) bool {
	for _, i := range v.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
