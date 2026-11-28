package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	TabPending    = "pending"
	TabListed     = "listed"
	TabSuperseded = "superseded"
	TabWithheld   = "withheld"
	TabHidden     = "hidden"
	TabRejected   = "rejected"

	rowsDefault = 50
	rowsMax     = 500
	domainsShow = 4
)

var setTabs = []string{TabPending, TabListed, TabSuperseded, TabWithheld, TabHidden, TabRejected}

type TargetsBriefView struct {
	Domains      []string `json:"domains"`
	DomainsTotal int      `json:"domains_total"`
	IPs          int      `json:"ips"`
	GeoSite      []string `json:"geosite"`
	GeoIP        []string `json:"geoip"`
	ASNs         []string `json:"asns"`
	UDPL7        []string `json:"udp_l7"`
	Filters      []Term   `json:"filters"`
}

type SetRowView struct {
	SetID              string                 `json:"set_id"`
	Version            int                    `json:"version"`
	Title              string                 `json:"title"`
	Status             string                 `json:"status"`
	StatusReason       string                 `json:"status_reason,omitempty"`
	Family             string                 `json:"family,omitempty"`
	Flags              []string               `json:"flags"`
	Techniques         []Term                 `json:"techniques"`
	Targets            TargetsBriefView       `json:"targets"`
	AuthorHMAC         string                 `json:"author_hmac"`
	Author             string                 `json:"author"`
	AuthorBanned       bool                   `json:"author_banned,omitempty"`
	ASNObserved        string                 `json:"asn_observed,omitempty"`
	CountryObserved    string                 `json:"country_observed,omitempty"`
	Published          *ScoreView             `json:"published,omitempty"`
	Live               ScoreView              `json:"live"`
	Evidence           EvidenceView           `json:"evidence"`
	Attention          []string               `json:"attention"`
	Reports            int                    `json:"reports"`
	OpenReports        int                    `json:"open_reports"`
	IndependentReports int                    `json:"independent_reports"`
	Versions           []int                  `json:"versions,omitempty"`
	SupersededBy       int                    `json:"superseded_by,omitempty"`
	SupersededAt       *time.Time             `json:"superseded_at,omitempty"`
	Withheld           string                 `json:"withheld,omitempty"`
	HiddenFrom         string                 `json:"hidden_from,omitempty"`
	Groups             []SetMembershipView    `json:"groups,omitempty"`
	Config             map[string]interface{} `json:"config,omitempty"`
	DecodeError        bool                   `json:"decode_error,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	EditedAt           *time.Time             `json:"edited_at,omitempty"`
}

type SetRowsView struct {
	Tab       string         `json:"tab"`
	Total     int            `json:"total"`
	Tabs      map[string]int `json:"tabs"`
	Attention int            `json:"attention"`
	Rows      []SetRowView   `json:"rows"`
}

type QueueView struct {
	Items  []EntryView        `json:"items"`
	Groups []GroupVersionView `json:"groups"`
}

func briefTargets(v store.Version) TargetsBriefView {
	t := TargetsOf(v.Projection)
	b := TargetsBriefView{
		Domains:      orEmpty(t.Domains),
		DomainsTotal: len(t.Domains),
		IPs:          len(t.IPs),
		GeoSite:      orEmpty(t.GeoSite),
		GeoIP:        orEmpty(t.GeoIP),
		ASNs:         orEmpty(t.ASNs),
		UDPL7:        orEmpty(t.UDPL7),
		Filters:      t.FilterTerms(),
	}
	if len(b.Domains) > domainsShow {
		b.Domains = b.Domains[:domainsShow]
	}
	return b
}

func gatherGroups(rows []SetRowView) []SetRowView {
	members := make(map[string][]SetRowView)
	for _, row := range rows {
		if len(row.Groups) > 0 {
			id := row.Groups[0].GroupID
			members[id] = append(members[id], row)
		}
	}
	out := make([]SetRowView, 0, len(rows))
	for _, row := range rows {
		if len(row.Groups) == 0 {
			out = append(out, row)
			continue
		}
		block, ok := members[row.Groups[0].GroupID]
		if !ok {
			continue
		}
		delete(members, row.Groups[0].GroupID)
		sort.SliceStable(block, func(i, j int) bool { return block[i].Groups[0].Position < block[j].Groups[0].Position })
		out = append(out, block...)
	}
	return out
}

func rowGroups(memberships []store.Membership) []SetMembershipView {
	var out []SetMembershipView
	for _, m := range memberships {
		if m.Status == hubwire.SetStatusRejected {
			continue
		}
		view := membershipView(m)
		if n := len(out); n > 0 && out[n-1].GroupID == m.GroupID {
			out[n-1] = view
			continue
		}
		out = append(out, view)
	}
	return out
}

func rowMatches(row SetRowView, v store.Version, needle string) bool {
	if needle == "" {
		return true
	}
	hay := []string{row.Title, row.SetID, row.Author, row.AuthorHMAC, row.Family, row.StatusReason, v.Description}
	for _, g := range row.Groups {
		hay = append(hay, g.Title, g.GroupID)
	}
	hay = append(hay, store.TargetList(v.Projection, "sni_domains")...)
	hay = append(hay, row.Targets.GeoSite...)
	hay = append(hay, row.Targets.UDPL7...)
	for _, asn := range row.Targets.ASNs {
		hay = append(hay, "as"+asn)
	}
	for _, t := range row.Techniques {
		hay = append(hay, t.Code)
		for _, p := range t.Params {
			if s, ok := p.(string); ok {
				hay = append(hay, s)
			}
		}
	}
	return strings.Contains(strings.ToLower(strings.Join(hay, "\n")), needle)
}

func rowSortKey(row SetRowView, by string) (float64, string, bool) {
	switch by {
	case "created":
		return float64(row.CreatedAt.Unix()), "", true
	case "title":
		return 0, strings.ToLower(row.Title), false
	case "author":
		return 0, row.Author, false
	case "score":
		if row.Published != nil {
			return row.Published.Score, "", true
		}
		return row.Live.Score, "", true
	case "n":
		if row.Published != nil {
			return row.Published.N, "", true
		}
		return row.Live.N, "", true
	case "devices":
		return float64(row.Evidence.Independent.Devices), "", true
	case "reports":
		return float64(row.OpenReports*1000 + row.Reports), "", true
	}
	return float64(row.UpdatedAt.Unix()), "", true
}

func (s *Server) setRows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tab := q.Get("tab")
	if tab == "" {
		tab = TabListed
	}
	valid := false
	for _, name := range setTabs {
		valid = valid || name == tab
	}
	if !valid {
		writeError(w, http.StatusBadRequest, codeBadRequest, "unknown tab")
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	offset = max(offset, 0)
	limit := rowsDefault
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "limit must be a positive number")
			return
		}
		limit = min(n, rowsMax)
	}
	ctx := r.Context()
	g, err := s.tabs(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	ec, err := s.entryContext(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	pool, err := s.Store.ScoringVotesByFP(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	attention, err := s.attentionMap(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	memberships, err := s.Store.AllMemberships(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	var versions []store.Version
	switch tab {
	case TabPending:
		versions = g.pending
	case TabListed:
		versions = g.listed
	case TabSuperseded:
		versions = g.superseded
	case TabWithheld:
		versions = g.withheld
	case TabHidden:
		versions = g.hidden
	case TabRejected:
		versions = g.rejected
	}
	latest := s.latest()
	at, now := s.rowClock()
	needle := strings.ToLower(strings.TrimSpace(q.Get("q")))
	filter := q.Get("filter")
	rows := make([]SetRowView, 0, len(versions))
	projections := make(map[string]map[string]interface{}, len(versions))
	for _, v := range versions {
		row := s.setRow(v, ec, pool, at, now)
		if tab == TabListed {
			if latest != nil {
				if cs, ok := latest.ByID[v.SetID]; ok && cs.Version == v.Version {
					published := scoreView(cs.Scores.Global)
					row.Published = &published
				}
			}
			row.Attention = orEmpty(attention[v.SetID])
			row.Versions = g.versions[v.SetID]
		}
		if tab == TabSuperseded {
			row.SupersededBy, row.SupersededAt = g.supersededBy(v)
		}
		row.Groups = rowGroups(memberships[v.SetID])
		if !rowMatches(row, v, needle) {
			continue
		}
		switch filter {
		case "grouped":
			if len(row.Groups) == 0 {
				continue
			}
		case "attention":
			if len(row.Attention) == 0 {
				continue
			}
		case "reports":
			if row.OpenReports == 0 {
				continue
			}
		case "edited":
			if row.EditedAt == nil {
				continue
			}
		}
		projections[v.SetID+"/"+strconv.Itoa(v.Version)] = v.Projection
		rows = append(rows, row)
	}
	by := q.Get("sort")
	desc := q.Get("dir") != "asc"
	sort.SliceStable(rows, func(i, j int) bool {
		ai, as, an := rowSortKey(rows[i], by)
		bi, bs, _ := rowSortKey(rows[j], by)
		less := ai < bi
		if !an {
			less = as < bs
		}
		if desc {
			if an {
				return ai > bi
			}
			return as > bs
		}
		return less
	})
	rows = gatherGroups(rows)
	view := SetRowsView{
		Tab:       tab,
		Total:     len(rows),
		Attention: len(attention),
		Tabs: map[string]int{
			TabPending:    len(g.pending),
			TabListed:     len(g.listed),
			TabSuperseded: len(g.superseded),
			TabWithheld:   len(g.withheld),
			TabHidden:     len(g.hidden),
			TabRejected:   len(g.rejected),
		},
	}
	if offset < len(rows) {
		view.Rows = rows[offset:min(len(rows), offset+limit)]
	} else {
		view.Rows = []SetRowView{}
	}
	for i, row := range view.Rows {
		if set, err := DecodeSet(projections[row.SetID+"/"+strconv.Itoa(row.Version)]); err == nil {
			view.Rows[i].Config = ConfigOf(&set)
		}
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) setRow(v store.Version, ec *entryContext, pool map[string][]store.Vote, at, now time.Time) SetRowView {
	author := ec.authors[v.SetID]
	if author == "" {
		author = v.UploaderHMAC
	}
	row := SetRowView{
		SetID:           v.SetID,
		Version:         v.Version,
		Title:           v.Title,
		Status:          v.Status,
		StatusReason:    v.StatusReason,
		Family:          v.Family,
		Flags:           orEmpty(v.Flags),
		Techniques:      []Term{},
		Targets:         briefTargets(v),
		AuthorHMAC:      author,
		Author:          hubdata.AuthorLabel(author),
		AuthorBanned:    ec.banned[author],
		ASNObserved:     v.ASNObserved,
		CountryObserved: v.CountryObserved,
		Attention:       []string{},
		Withheld:        ec.withheld[v.SetID],
		HiddenFrom:      v.HiddenFrom,
		CreatedAt:       v.CreatedAt,
		UpdatedAt:       v.UpdatedAt,
		EditedAt:        optionalTime(v.EditedAt),
	}
	if set, err := DecodeSet(v.Projection); err == nil {
		row.Techniques = Techniques(&set, v.Payloads)
	} else {
		row.DecodeError = true
	}
	votes := pool[v.FP]
	scoreVotes := make([]score.Vote, 0, len(votes))
	for _, vote := range votes {
		scoreVotes = append(scoreVotes, vote.ScoreVote())
	}
	row.Live = scoreView(score.Aggregate(scoreVotes, now).Global)
	row.Evidence = evidenceOf(votes, v.SetID, author, at)
	if reports := ec.reports[v.SetID][v.Version]; len(reports) > 0 {
		row.Reports = len(reports)
		for _, rep := range reports {
			if rep.State == store.ReportOpen {
				row.OpenReports++
			}
		}
		row.IndependentReports = store.IndependentOf(reports)
	}
	return row
}

func (s *Server) queue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.tabs(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	ec, err := s.entryContext(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	histories, err := s.Store.PendingGroupHistories(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	inside, err := s.Store.FoldedVersions(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := QueueView{Items: make([]EntryView, 0, len(g.pending)), Groups: make([]GroupVersionView, 0, len(histories))}
	for _, v := range g.pending {
		if inside[v.SetID][v.Version] {
			continue
		}
		e := s.entry(v, ec)
		e.Lineage = lineage(v, g.newest)
		view.Items = append(view.Items, e)
	}
	if len(histories) > 0 {
		gc, err := s.groupContext(ctx, g, ec, histories)
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, gv := range histories {
			if gv.Status != hubwire.SetStatusPending {
				continue
			}
			item, err := s.groupVersionView(ctx, gv, gc)
			if err != nil {
				s.fail(w, err)
				return
			}
			view.Groups = append(view.Groups, item)
		}
	}
	writeJSON(w, http.StatusOK, view)
}
