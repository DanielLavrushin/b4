package web

import (
	"context"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	AttentionLowScore = "low_score"
	AttentionStale    = "stale"
	AttentionReports  = "reports"
	AttentionWeak     = "weak"

	weakScore = 0.5
)

var attentionCodes = []string{AttentionLowScore, AttentionStale, AttentionReports, AttentionWeak}

type ScoreView struct {
	Score   float64 `json:"score"`
	N       float64 `json:"n"`
	Devices int     `json:"devices"`
	Newest  string  `json:"newest,omitempty"`
	Bucket  string  `json:"bucket"`
}

type EvidenceSideView struct {
	Works    int     `json:"works"`
	Broken   int     `json:"broken"`
	Devices  int     `json:"devices"`
	Positive float64 `json:"positive"`
	Negative float64 `json:"negative"`
}

type EvidenceView struct {
	Independent EvidenceSideView `json:"independent"`
	Author      EvidenceSideView `json:"author"`
	Pooled      int              `json:"pooled"`
}

func scoreView(s hubwire.Score) ScoreView {
	bucket := hubwire.BucketNone
	if s.N >= 1 && s.Devices >= 2 {
		bucket = hubwire.BucketGlobal
	}
	return ScoreView{Score: s.Score, N: s.N, Devices: s.Devices, Newest: s.Newest, Bucket: bucket}
}

func parseDay(raw string) time.Time {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func attentionOf(cs *hubwire.CatalogueSet, openReports int, now time.Time) []string {
	out := []string{}
	if cs != nil {
		last := parseDay(cs.Scores.Global.Newest)
		if last.IsZero() {
			last = parseDay(cs.UpdatedAt)
		}
		if reason, ok := score.Demotion(cs.Scores.Global, last, now); ok {
			out = append(out, reason)
		} else if cs.Scores.Global.N >= 1 && cs.Scores.Global.Devices >= 2 && cs.Scores.Global.Score < weakScore {
			out = append(out, AttentionWeak)
		}
	}
	if openReports > 0 {
		out = append(out, AttentionReports)
	}
	return out
}

func (s *Server) latest() *catalogue.Result {
	if s.Catalogue == nil {
		return nil
	}
	return s.Catalogue.Latest()
}

func (s *Server) attentionMap(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	latest := s.latest()
	open, err := s.Store.OpenReportCounts(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if latest != nil {
		for i := range latest.Catalogue.Sets {
			cs := &latest.Catalogue.Sets[i]
			if codes := attentionOf(cs, open[cs.ID][cs.Version], now); len(codes) > 0 {
				out[cs.ID] = codes
			}
		}
	}
	return out, nil
}

func (s *Server) attentionCount(ctx context.Context) int {
	m, err := s.attentionMap(ctx)
	if err != nil {
		return 0
	}
	return len(m)
}

func evidenceOf(pool []store.Vote, setID, author string, at time.Time) EvidenceView {
	var ev EvidenceView
	independent := map[string]struct{}{}
	authors := map[string]struct{}{}
	pooled := map[string]struct{}{}
	for _, v := range pool {
		side := &ev.Independent
		devices := independent
		if v.KeyHMAC == author {
			side = &ev.Author
			devices = authors
		}
		if v.SetID != setID {
			pooled[v.SetID] = struct{}{}
		}
		w := score.EffectiveWeight(v.ScoreVote(), at)
		if v.Weight >= 0 {
			side.Works++
		} else {
			side.Broken++
		}
		if w >= 0 {
			side.Positive += w
		} else {
			side.Negative += -w
		}
		devices[v.KeyHMAC] = struct{}{}
	}
	ev.Independent.Devices = len(independent)
	ev.Author.Devices = len(authors)
	ev.Pooled = len(pooled)
	ev.Independent.Positive = round4(ev.Independent.Positive)
	ev.Independent.Negative = round4(ev.Independent.Negative)
	ev.Author.Positive = round4(ev.Author.Positive)
	ev.Author.Negative = round4(ev.Author.Negative)
	return ev
}

func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}
