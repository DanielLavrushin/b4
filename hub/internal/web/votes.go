package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	AppliedCounted      = "counted"
	AppliedNotPublished = "not_published"
	AppliedAfterBuild   = "after_build"
	AppliedExcluded     = "excluded"
	AppliedCapped       = "capped"
)

var appliedStates = []string{AppliedCounted, AppliedNotPublished, AppliedAfterBuild, AppliedExcluded, AppliedCapped}

type AppliedView struct {
	At       *time.Time `json:"at,omitempty"`
	Base     float64    `json:"base"`
	Origin   float64    `json:"origin"`
	YoungKey float64    `json:"young_key"`
	Decay    float64    `json:"decay"`
	Weight   float64    `json:"weight"`
	State    string     `json:"state"`
	Reason   string     `json:"reason,omitempty"`
}

type VoteRowView struct {
	VoteView
	Title      string      `json:"title,omitempty"`
	SetStatus  string      `json:"set_status,omitempty"`
	AuthorVote bool        `json:"author_vote,omitempty"`
	CountedIn  []string    `json:"counted_in"`
	Applied    AppliedView `json:"applied"`
}

type VotesPageView struct {
	Items []VoteRowView `json:"items"`
	Total int           `json:"total"`
	Next  string        `json:"next,omitempty"`
}

type VoteOriginsView struct {
	ASNs      []MixView `json:"asns"`
	Countries []MixView `json:"countries"`
}

func (s *Server) voteOrigins(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	asns, err := toMix(s.Store.VoteASNs(ctx))
	if err != nil {
		s.fail(w, err)
		return
	}
	countries, err := toMix(s.Store.VoteCountries(ctx))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, VoteOriginsView{ASNs: asns, Countries: countries})
}

func (s *Server) voteRows(ctx context.Context, votes []store.Vote) ([]VoteRowView, error) {
	out := make([]VoteRowView, 0, len(votes))
	if len(votes) == 0 {
		return out, nil
	}
	heads, err := s.Store.VersionHeads(ctx)
	if err != nil {
		return nil, err
	}
	sets, err := s.Store.Sets(ctx)
	if err != nil {
		return nil, err
	}
	notable, err := s.Store.NotableKeys(ctx)
	if err != nil {
		return nil, err
	}
	excluded := map[string]string{}
	for _, k := range notable {
		switch {
		case k.Banned:
			excluded[k.KeyHMAC] = "banned"
		case k.Tag == store.TagTest:
			excluded[k.KeyHMAC] = "test"
		}
	}
	latest := s.latest()
	var at time.Time
	fpIndex := map[string][]string{}
	if latest != nil {
		at = parseDay(latest.Catalogue.GeneratedAt)
		for i := range latest.Catalogue.Sets {
			cs := &latest.Catalogue.Sets[i]
			fpIndex[cs.FP] = append(fpIndex[cs.FP], cs.ID)
		}
	}
	pools := votePools{}
	for _, v := range votes {
		row := VoteRowView{VoteView: voteView(v), CountedIn: orEmpty(fpIndex[v.FP])}
		if h, ok := heads[v.SetID][v.Version]; ok {
			row.Title = h.Title
			row.SetStatus = h.Status
		}
		row.AuthorVote = sets[v.SetID].AuthorHMAC == v.KeyHMAC
		when := at
		if when.IsZero() {
			when = s.now().UTC()
		}
		f := score.WeightFactors(v.ScoreVote(), when)
		row.Applied = AppliedView{At: optionalTime(at), Base: f.Base, Origin: f.Origin, YoungKey: f.YoungKey, Decay: round4(f.Decay), Weight: round4(f.Weight)}
		switch {
		case excluded[v.KeyHMAC] != "":
			row.Applied.State = AppliedExcluded
			row.Applied.Reason = excluded[v.KeyHMAC]
		case latest == nil || v.ReceivedAt.After(at):
			row.Applied.State = AppliedAfterBuild
		case len(fpIndex[v.FP]) == 0:
			row.Applied.State = AppliedNotPublished
		case !score.IsHuman(v.Kind) && !automatedCounted(ctx, s, &pools, v, at):
			row.Applied.State = AppliedCapped
		default:
			row.Applied.State = AppliedCounted
		}
		out = append(out, row)
	}
	return out, nil
}

type votePools struct {
	loaded  bool
	byFP    map[string][]store.Vote
	counted map[int64]bool
}

func automatedCounted(ctx context.Context, s *Server, p *votePools, v store.Vote, at time.Time) bool {
	if !p.loaded {
		p.loaded = true
		p.counted = map[int64]bool{}
		byFP, err := s.Store.ScoringVotesByFP(ctx)
		if err != nil {
			return true
		}
		p.byFP = byFP
	}
	if counted, ok := p.counted[v.ID]; ok {
		return counted
	}
	pool := make([]store.Vote, 0)
	for _, pv := range p.byFP[v.FP] {
		if !pv.ReceivedAt.After(at) {
			pool = append(pool, pv)
		}
	}
	scoreVotes := make([]score.Vote, 0, len(pool))
	for _, pv := range pool {
		scoreVotes = append(scoreVotes, pv.ScoreVote())
	}
	for i, flag := range score.Counted(scoreVotes, at) {
		p.counted[pool[i].ID] = flag
	}
	return p.counted[v.ID]
}

func (s *Server) votes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.VoteFilter{
		SetID:    q.Get("set"),
		FP:       q.Get("fp"),
		KeyHMAC:  strings.ToLower(q.Get("key")),
		Sign:     q.Get("sign"),
		Kind:     q.Get("kind"),
		ASN:      strings.TrimPrefix(strings.ToUpper(q.Get("asn")), "AS"),
		Country:  q.Get("cc"),
		Verified: q.Get("verified"),
		Author:   q.Get("author"),
		Before:   q.Get("before"),
		Limit:    100,
	}
	if raw := q.Get("version"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "version must be a positive number")
			return
		}
		f.Version = n
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "limit must be a positive number")
			return
		}
		f.Limit = min(n, 500)
	}
	var ok bool
	if f.Since, ok = parseStamp(q.Get("since")); !ok {
		writeError(w, http.StatusBadRequest, codeBadRequest, "since must be an RFC 3339 time")
		return
	}
	if f.Until, ok = parseStamp(q.Get("until")); !ok {
		writeError(w, http.StatusBadRequest, codeBadRequest, "until must be an RFC 3339 time")
		return
	}
	ctx := r.Context()
	votes, total, next, err := s.Store.QueryVotes(ctx, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.voteRows(ctx, votes)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, VotesPageView{Items: rows, Total: total, Next: next})
}
