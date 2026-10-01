package web

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	keyActivityDays = 90
	keyDetailRows   = 100
)

type KeyRowView struct {
	KeyHMAC          string     `json:"key_hmac"`
	Label            string     `json:"label"`
	Name             string     `json:"name,omitempty"`
	Note             string     `json:"note,omitempty"`
	Tag              string     `json:"tag,omitempty"`
	FirstSeen        time.Time  `json:"first_seen"`
	LastSeen         *time.Time `json:"last_seen,omitempty"`
	Banned           bool       `json:"banned"`
	BanReason        string     `json:"ban_reason,omitempty"`
	BannedAt         *time.Time `json:"banned_at,omitempty"`
	Trusted          bool       `json:"trusted"`
	TrustedAt        *time.Time `json:"trusted_at,omitempty"`
	ProfileUpdatedAt *time.Time `json:"profile_updated_at,omitempty"`
	Records          int        `json:"records"`
	Sets             int        `json:"sets"`
	ListedSets       int        `json:"listed_sets"`
	PendingVersions  int        `json:"pending_versions"`
	Votes            int        `json:"votes"`
	ManualVotes      int        `json:"manual_votes"`
	Reports          int        `json:"reports"`
	ReportsAgainst   int        `json:"reports_against"`
	LastASN          string     `json:"last_asn,omitempty"`
	LastCountry      string     `json:"last_country,omitempty"`
}

type KeyCountsView struct {
	All      int `json:"all"`
	Banned   int `json:"banned"`
	Trusted  int `json:"trusted"`
	Staff    int `json:"staff"`
	Test     int `json:"test"`
	WithSets int `json:"with_sets"`
	Active7d int `json:"active_7d"`
}

type KeyListView struct {
	Items   []KeyRowView  `json:"items"`
	Total   int           `json:"total"`
	Counts  KeyCountsView `json:"counts"`
	MatchBy string        `json:"match_by,omitempty"`
}

type ActivityKindView struct {
	Kind  string    `json:"kind"`
	Count int       `json:"count"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

type ActivityDayView struct {
	Day    string `json:"day"`
	Share  int    `json:"share"`
	Vote   int    `json:"vote"`
	Report int    `json:"report"`
	Mirror int    `json:"mirror"`
}

type KeySetVersionView struct {
	Version int    `json:"version"`
	Status  string `json:"status"`
}

type KeySetView struct {
	SetID         string              `json:"set_id"`
	Title         string              `json:"title"`
	ListedVersion int                 `json:"listed_version,omitempty"`
	Withheld      string              `json:"withheld,omitempty"`
	Versions      []KeySetVersionView `json:"versions"`
	Score         *ScoreView          `json:"score,omitempty"`
	Reports       int                 `json:"reports"`
}

type KeyOriginView struct {
	ASN     string    `json:"asn"`
	Name    string    `json:"name,omitempty"`
	Country string    `json:"country,omitempty"`
	Count   int       `json:"count"`
	First   time.Time `json:"first"`
	Last    time.Time `json:"last"`
	Sources []string  `json:"sources"`
}

type KeyClientView struct {
	B4Version string    `json:"b4_version"`
	Engine    string    `json:"engine,omitempty"`
	Count     int       `json:"count"`
	Last      time.Time `json:"last"`
}

type KeyDetailView struct {
	Key        KeyRowView         `json:"key"`
	Kinds      []ActivityKindView `json:"kinds"`
	Days       []ActivityDayView  `json:"days"`
	Sets       []KeySetView       `json:"sets"`
	Votes      []VoteRowView      `json:"votes"`
	VotesTotal int                `json:"votes_total"`
	Reports    []ReportView       `json:"reports"`
	Origins    []KeyOriginView    `json:"origins"`
	Clients    []KeyClientView    `json:"clients"`
	Mirrors    []MirrorView       `json:"mirrors"`
	History    []AuditEntryView   `json:"history"`
}

type KeyProfileRequest struct {
	Name string `json:"name"`
	Note string `json:"note"`
	Tag  string `json:"tag"`
}

type NotableKeyView struct {
	KeyHMAC string `json:"key_hmac"`
	Name    string `json:"name,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Banned  bool   `json:"banned,omitempty"`
	Trusted bool   `json:"trusted,omitempty"`
}

func keyRowView(k store.KeyRow) KeyRowView {
	v := KeyRowView{
		KeyHMAC:          k.KeyHMAC,
		Label:            hubdata.AuthorLabel(k.KeyHMAC),
		Name:             k.Name,
		Note:             k.Note,
		Tag:              k.Tag,
		FirstSeen:        k.FirstSeen,
		LastSeen:         optionalTime(k.LastSeen),
		Banned:           k.Banned,
		BanReason:        k.BanReason,
		BannedAt:         optionalTime(k.BannedAt),
		Trusted:          k.Trusted,
		TrustedAt:        optionalTime(k.TrustedAt),
		ProfileUpdatedAt: optionalTime(k.ProfileUpdatedAt),
		Records:          k.Records,
		Sets:             k.Sets,
		ListedSets:       k.ListedSets,
		PendingVersions:  k.PendingVersions,
		Votes:            k.Votes,
		ManualVotes:      k.ManualVotes,
		Reports:          k.Reports,
		ReportsAgainst:   k.ReportsAgainst,
		LastASN:          k.LastASN,
		LastCountry:      k.LastCountry,
	}
	return v
}

func (s *Server) keyQuery(r *http.Request) (store.KeyQuery, string, bool) {
	q := r.URL.Query()
	f := store.KeyQuery{Status: q.Get("status"), Tag: q.Get("tag"), Sets: q.Get("sets"), Sort: q.Get("sort"), Desc: q.Get("dir") != "asc", Limit: 50}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return f, "", false
		}
		f.Limit = min(n, 500)
	}
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	if raw := q.Get("active"); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days <= 0 {
			return f, "", false
		}
		f.ActiveSince = s.now().Add(-time.Duration(days) * 24 * time.Hour)
	}
	match := ""
	text := strings.TrimSpace(q.Get("q"))
	if text != "" {
		if pub, err := hubwire.DecodeKey(text); err == nil && len(s.Secret) > 0 {
			f.Exact = hubdata.KeyHMAC(s.Secret, hubwire.EncodeKey(pub))
			match = "key_id"
		} else {
			lower := strings.ToLower(text)
			if isHex(lower) {
				f.Prefix = lower
			}
			f.Text = lower
		}
	}
	return f, match, true
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) keyList(w http.ResponseWriter, r *http.Request) {
	f, match, ok := s.keyQuery(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeBadRequest, "limit and active must be positive numbers")
		return
	}
	rows, total, counts, err := s.Store.KeyList(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := KeyListView{Items: make([]KeyRowView, 0, len(rows)), Total: total, MatchBy: match, Counts: KeyCountsView{
		All: counts.All, Banned: counts.Banned, Trusted: counts.Trusted, Staff: counts.Staff, Test: counts.Test, WithSets: counts.WithSets, Active7d: counts.Active7d,
	}}
	for _, k := range rows {
		view.Items = append(view.Items, keyRowView(k))
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) resolveKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.ToLower(strings.TrimSpace(r.PathValue("key")))
	if hubdata.ValidKeyHMAC(raw) {
		return raw, true
	}
	if len(raw) < 8 || !isHex(raw) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a key")
		return "", false
	}
	found, err := s.Store.ResolveKeyPrefix(r.Context(), raw)
	if err != nil {
		s.fail(w, err)
		return "", false
	}
	switch len(found) {
	case 0:
		writeError(w, http.StatusNotFound, codeNotFound, "no key starts with "+raw)
		return "", false
	case 1:
		return found[0], true
	}
	writeJSON(w, http.StatusConflict, ErrorBody{Code: "ambiguous", Error: "several keys start with " + raw, Params: map[string]interface{}{"candidates": found}})
	return "", false
}

func (s *Server) keyDetail(w http.ResponseWriter, r *http.Request) {
	key, ok := s.resolveKey(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	row, err := s.Store.KeyRowOf(ctx, key)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := KeyDetailView{Key: keyRowView(*row), Kinds: []ActivityKindView{}, Days: []ActivityDayView{}, Sets: []KeySetView{}, Votes: []VoteRowView{}, Reports: []ReportView{},
		Origins: []KeyOriginView{}, Clients: []KeyClientView{}, Mirrors: []MirrorView{}, History: []AuditEntryView{}}
	since := s.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -keyActivityDays+1)
	kinds, days, err := s.Store.KeyActivity(ctx, key, since)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, k := range kinds {
		view.Kinds = append(view.Kinds, ActivityKindView{Kind: k.Kind, Count: k.Count, First: k.First, Last: k.Last})
	}
	byDay := map[string]*ActivityDayView{}
	for _, d := range days {
		day := byDay[d.Day]
		if day == nil {
			view.Days = append(view.Days, ActivityDayView{Day: d.Day})
			day = &view.Days[len(view.Days)-1]
			byDay[d.Day] = day
		}
		switch d.Kind {
		case hubwire.RecordShare:
			day.Share += d.Count
		case hubwire.RecordVote:
			day.Vote += d.Count
		case hubwire.RecordReport:
			day.Report += d.Count
		case hubwire.RecordMirror:
			day.Mirror += d.Count
		}
	}
	if err := s.fillKeySets(ctx, key, &view); err != nil {
		s.fail(w, err)
		return
	}
	votes, total, _, err := s.Store.QueryVotes(ctx, store.VoteFilter{KeyHMAC: key, Limit: keyDetailRows})
	if err != nil {
		s.fail(w, err)
		return
	}
	view.VotesTotal = total
	if view.Votes, err = s.voteRows(ctx, votes); err != nil {
		s.fail(w, err)
		return
	}
	reports, err := s.Store.ReportsByKey(ctx, key, keyDetailRows)
	if err != nil {
		s.fail(w, err)
		return
	}
	heads, err := s.Store.VersionHeads(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, rep := range reports {
		rv := reportView(rep)
		if h, ok := heads[rep.SetID][rep.Version]; ok {
			rv.Title = h.Title
			rv.SetStatus = h.Status
		}
		view.Reports = append(view.Reports, rv)
	}
	origins, err := s.Store.KeyOrigins(ctx, key)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, o := range origins {
		view.Origins = append(view.Origins, KeyOriginView{ASN: o.ASN, Name: o.Name, Country: o.Country, Count: o.Count, First: o.First, Last: o.Last, Sources: orEmpty(o.Sources)})
	}
	clients, err := s.Store.KeyClients(ctx, key)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, c := range clients {
		view.Clients = append(view.Clients, KeyClientView{B4Version: c.B4Version, Engine: c.Engine, Count: c.Count, Last: c.Last})
	}
	mirrors, err := s.Store.MirrorsByKey(ctx, key)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.Mirrors = s.mirrorViews(ctx, mirrors)
	history, _, err := s.Store.AuditLog(ctx, store.AuditQuery{TargetKind: store.TargetKey, TargetID: key, Limit: 50})
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, e := range history {
		view.History = append(view.History, auditView(e))
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) fillKeySets(ctx context.Context, key string, view *KeyDetailView) error {
	versions, err := s.Store.VersionsByAuthor(ctx, key)
	if err != nil {
		return err
	}
	withheld, err := s.Store.Withheld(ctx)
	if err != nil {
		return err
	}
	reports, err := s.Store.AllReports(ctx)
	if err != nil {
		return err
	}
	reportCount := map[string]int{}
	for _, rep := range reports {
		reportCount[rep.SetID]++
	}
	latest := s.latest()
	index := map[string]int{}
	for _, v := range versions {
		i, ok := index[v.SetID]
		if !ok {
			view.Sets = append(view.Sets, KeySetView{SetID: v.SetID, Versions: []KeySetVersionView{}, Withheld: withheld[v.SetID], Reports: reportCount[v.SetID]})
			i = len(view.Sets) - 1
			index[v.SetID] = i
		}
		set := &view.Sets[i]
		set.Title = v.Title
		set.Versions = append(set.Versions, KeySetVersionView{Version: v.Version, Status: v.Status})
		if v.Status == hubwire.SetStatusActive && v.Version > set.ListedVersion && set.Withheld == "" {
			set.ListedVersion = v.Version
		}
	}
	for i := range view.Sets {
		if latest == nil {
			break
		}
		if cs, ok := latest.ByID[view.Sets[i].SetID]; ok {
			sv := scoreView(cs.Scores.Global)
			view.Sets[i].Score = &sv
		}
	}
	sort.SliceStable(view.Sets, func(i, j int) bool { return view.Sets[i].ListedVersion > view.Sets[j].ListedVersion })
	return nil
}

func (s *Server) keyProfile(w http.ResponseWriter, r *http.Request) {
	key, ok := s.resolveKey(w, r)
	if !ok {
		return
	}
	var req KeyProfileRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.KeyProfile(r.Context(), s.actor(r), key, store.KeyProfile{Name: req.Name, Note: req.Note, Tag: req.Tag})
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
}

func (s *Server) notableKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.NotableKeys(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]NotableKeyView, 0, len(keys))
	for _, k := range keys {
		out = append(out, NotableKeyView{KeyHMAC: k.KeyHMAC, Name: k.Name, Tag: k.Tag, Banned: k.Banned, Trusted: k.Trusted})
	}
	writeJSON(w, http.StatusOK, out)
}
