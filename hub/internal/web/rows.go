package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	GroupPending    = "pending"
	GroupListed     = "listed"
	GroupSuperseded = "superseded"
	GroupWithheld   = "withheld"
	GroupHidden     = "hidden"
	GroupRejected   = "rejected"

	rowsDefault = 50
	rowsMax     = 500
	domainsShow = 4
)

var setGroups = []string{GroupPending, GroupListed, GroupSuperseded, GroupWithheld, GroupHidden, GroupRejected}

type TargetsBriefView struct {
	Domains      []string `json:"domains"`
	DomainsTotal int      `json:"domains_total"`
	IPs          int      `json:"ips"`
	GeoSite      []string `json:"geosite"`
	GeoIP        []string `json:"geoip"`
	ASNs         []string `json:"asns"`
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
	Config             map[string]interface{} `json:"config,omitempty"`
	DecodeError        bool                   `json:"decode_error,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	EditedAt           *time.Time             `json:"edited_at,omitempty"`
}

type SetRowsView struct {
	Group     string         `json:"group"`
	Total     int            `json:"total"`
	Groups    map[string]int `json:"groups"`
	Attention int            `json:"attention"`
	Rows      []SetRowView   `json:"rows"`
}

type QueueView struct {
	Items []EntryView `json:"items"`
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
		Filters:      t.FilterTerms(),
	}
	if len(b.Domains) > domainsShow {
		b.Domains = b.Domains[:domainsShow]
	}
	return b
}

func rowMatches(row SetRowView, v store.Version, needle string) bool {
	if needle == "" {
		return true
	}
	hay := []string{row.Title, row.SetID, row.Author, row.AuthorHMAC, row.Family, row.StatusReason, v.Description}
	hay = append(hay, store.TargetList(v.Projection, "sni_domains")...)
	hay = append(hay, row.Targets.GeoSite...)
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
	group := q.Get("group")
	if group == "" {
		group = GroupListed
	}
	valid := false
	for _, g := range setGroups {
		valid = valid || g == group
	}
	if !valid {
		writeError(w, http.StatusBadRequest, codeBadRequest, "unknown group")
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
	g, err := s.groups(ctx)
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
	var versions []store.Version
	switch group {
	case GroupPending:
		versions = g.pending
	case GroupListed:
		versions = g.listed
	case GroupSuperseded:
		versions = g.superseded
	case GroupWithheld:
		versions = g.withheld
	case GroupHidden:
		versions = g.hidden
	case GroupRejected:
		versions = g.rejected
	}
	latest := s.latest()
	now := s.now().UTC()
	at := now
	if latest != nil {
		if t := parseDay(latest.Catalogue.GeneratedAt); !t.IsZero() {
			at = t
		}
	}
	needle := strings.ToLower(strings.TrimSpace(q.Get("q")))
	filter := q.Get("filter")
	rows := make([]SetRowView, 0, len(versions))
	projections := make(map[string]map[string]interface{}, len(versions))
	for _, v := range versions {
		row := s.setRow(v, ec, pool, at, now)
		if group == GroupListed {
			if latest != nil {
				if cs, ok := latest.ByID[v.SetID]; ok && cs.Version == v.Version {
					published := scoreView(cs.Scores.Global)
					row.Published = &published
				}
			}
			row.Attention = orEmpty(attention[v.SetID])
			row.Versions = g.versions[v.SetID]
		}
		if group == GroupSuperseded {
			current := g.newest[v.SetID]
			row.SupersededBy = current.Version
			row.SupersededAt = optionalTime(current.UpdatedAt)
		}
		if !rowMatches(row, v, needle) {
			continue
		}
		switch filter {
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
	view := SetRowsView{
		Group:     group,
		Total:     len(rows),
		Attention: len(attention),
		Groups: map[string]int{
			GroupPending:    len(g.pending),
			GroupListed:     len(g.listed),
			GroupSuperseded: len(g.superseded),
			GroupWithheld:   len(g.withheld),
			GroupHidden:     len(g.hidden),
			GroupRejected:   len(g.rejected),
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
	g, err := s.groups(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	ec, err := s.entryContext(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := QueueView{Items: s.entries(g.pending, ec, 0)}
	for i := range view.Items {
		view.Items[i].Lineage = lineage(g.pending[i], g.newest)
	}
	writeJSON(w, http.StatusOK, view)
}
