package web

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	statsDefaultDays = 30
	statsMaxDays     = 365
	mixLimit         = 30
	topSetsLimit     = 15
)

type StatsDayView struct {
	Day         string `json:"day"`
	Shares      int    `json:"shares"`
	Duplicates  int    `json:"duplicates"`
	Works       int    `json:"works"`
	Broken      int    `json:"broken"`
	Reports     int    `json:"reports"`
	NewKeys     int    `json:"new_keys"`
	Mirrors     int    `json:"mirrors"`
	Approved    int    `json:"approved"`
	Rejected    int    `json:"rejected"`
	Hidden      int    `json:"hidden"`
	Withdrawn   int    `json:"withdrawn"`
	AutoHidden  int    `json:"auto_hidden"`
	Builds      int    `json:"builds"`
	BuildFailed int    `json:"build_failed"`
}

type ScoreBinView struct {
	Lo   float64 `json:"lo"`
	Hi   float64 `json:"hi"`
	Sets int     `json:"sets"`
	LowN int     `json:"low_n"`
}

type ScoreStatsView struct {
	Listed   int            `json:"listed"`
	Rated    int            `json:"rated"`
	Median   float64        `json:"median"`
	Bins     []ScoreBinView `json:"bins"`
	LowScore int            `json:"low_score"`
	Stale    int            `json:"stale"`
}

type MixView struct {
	Key     string `json:"key"`
	Name    string `json:"name,omitempty"`
	Country string `json:"country,omitempty"`
	Votes   int    `json:"votes"`
	Keys    int    `json:"keys"`
}

type CoverageView struct {
	Votes      int `json:"votes"`
	Unverified int `json:"unverified"`
	Devices    int `json:"devices"`
}

type TopSetView struct {
	SetID  string `json:"set_id"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
	Votes  int    `json:"votes"`
	Keys   int    `json:"keys"`
	Works  int    `json:"works"`
	Broken int    `json:"broken"`
}

type StatsView struct {
	From      string         `json:"from"`
	To        string         `json:"to"`
	Days      int            `json:"days"`
	Daily     []StatsDayView `json:"daily"`
	Totals    StatsDayView   `json:"totals"`
	Coverage  CoverageView   `json:"coverage"`
	Scores    ScoreStatsView `json:"scores"`
	Countries []MixView      `json:"countries"`
	ASNs      []MixView      `json:"asns"`
	Clients   []MixView      `json:"clients"`
	TopSets   []TopSetView   `json:"top_sets"`
}

func (d *StatsDayView) add(kind string, n int) {
	switch kind {
	case "share":
		d.Shares += n
	case "duplicate":
		d.Duplicates += n
	case "works":
		d.Works += n
	case "broken":
		d.Broken += n
	case "report":
		d.Reports += n
	case "new_key":
		d.NewKeys += n
	case "mirror":
		d.Mirrors += n
	case "approve":
		d.Approved += n
	case "reject":
		d.Rejected += n
	case "hide":
		d.Hidden += n
	case "withdraw":
		d.Withdrawn += n
	case "auto_hidden":
		d.AutoHidden += n
	case "build":
		d.Builds += n
	case "build_failed":
		d.BuildFailed += n
	}
}

func toMix(rows []store.MixRow, err error) ([]MixView, error) {
	if err != nil {
		return nil, err
	}
	out := make([]MixView, 0, len(rows))
	for _, r := range rows {
		out = append(out, MixView{Key: r.Key, Name: r.Name, Country: r.Country, Votes: r.Votes, Keys: r.Keys})
	}
	return out, nil
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	days := statsDefaultDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > statsMaxDays {
			writeError(w, http.StatusBadRequest, codeBadRequest, "days must be between 1 and 365")
			return
		}
		days = n
	}
	ctx := r.Context()
	now := s.now().UTC()
	today := now.Truncate(24 * time.Hour)
	since := today.AddDate(0, 0, -days+1)
	view := StatsView{From: since.Format("2006-01-02"), To: today.Format("2006-01-02"), Days: days, Daily: make([]StatsDayView, 0, days)}
	index := map[string]int{}
	for i := 0; i < days; i++ {
		day := since.AddDate(0, 0, i).Format("2006-01-02")
		index[day] = i
		view.Daily = append(view.Daily, StatsDayView{Day: day})
	}
	counts, err := s.Store.DailyActivity(ctx, since)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, c := range counts {
		if i, ok := index[c.Day]; ok {
			view.Daily[i].add(c.Kind, c.Count)
			view.Totals.add(c.Kind, c.Count)
		}
	}
	coverage, err := s.Store.VoteCoverage(ctx, since)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.Coverage = CoverageView{Votes: coverage.Votes, Unverified: coverage.Unverified, Devices: coverage.Devices}
	for _, q := range []struct {
		into *[]MixView
		load func() ([]MixView, error)
	}{
		{&view.Countries, func() ([]MixView, error) { return toMix(s.Store.CountryMix(ctx, since, mixLimit)) }},
		{&view.ASNs, func() ([]MixView, error) { return toMix(s.Store.ASNMix(ctx, since, mixLimit)) }},
		{&view.Clients, func() ([]MixView, error) { return toMix(s.Store.ClientMix(ctx, since)) }},
	} {
		rows, err := q.load()
		if err != nil {
			s.fail(w, err)
			return
		}
		*q.into = rows
	}
	top, err := s.Store.TopSets(ctx, since, topSetsLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	heads, err := s.Store.VersionHeads(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.TopSets = make([]TopSetView, 0, len(top))
	for _, a := range top {
		_, head := store.LatestHead(heads[a.SetID])
		view.TopSets = append(view.TopSets, TopSetView{SetID: a.SetID, Title: head.Title, Status: head.Status, Votes: a.Votes, Keys: a.Keys, Works: a.Works, Broken: a.Broken})
	}
	view.Scores = s.scoreStats(now)
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) scoreStats(now time.Time) ScoreStatsView {
	out := ScoreStatsView{Bins: make([]ScoreBinView, 10)}
	for i := range out.Bins {
		out.Bins[i] = ScoreBinView{Lo: float64(i) / 10, Hi: float64(i+1) / 10}
	}
	latest := s.latest()
	if latest == nil {
		return out
	}
	rated := make([]float64, 0, len(latest.Catalogue.Sets))
	for i := range latest.Catalogue.Sets {
		cs := &latest.Catalogue.Sets[i]
		out.Listed++
		g := cs.Scores.Global
		bin := int(math.Max(0, math.Min(9, math.Floor(g.Score*10))))
		if g.N >= 1 && g.Devices >= 2 {
			out.Rated++
			rated = append(rated, g.Score)
			out.Bins[bin].Sets++
		} else {
			out.Bins[bin].LowN++
		}
		last := parseDay(g.Newest)
		if last.IsZero() {
			last = parseDay(cs.UpdatedAt)
		}
		switch reason, ok := score.Demotion(g, last, now); {
		case ok && reason == score.ReasonLowScore:
			out.LowScore++
		case ok && reason == score.ReasonStale:
			out.Stale++
		}
	}
	if len(rated) > 0 {
		sort.Float64s(rated)
		mid := len(rated) / 2
		out.Median = rated[mid]
		if len(rated)%2 == 0 {
			out.Median = (rated[mid-1] + rated[mid]) / 2
		}
	}
	return out
}
