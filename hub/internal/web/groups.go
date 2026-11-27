package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

type GroupMemberView struct {
	Position      int                      `json:"position"`
	SetID         string                   `json:"set_id"`
	Version       int                      `json:"version"`
	Brought       bool                     `json:"brought"`
	BroughtIn     int                      `json:"brought_in,omitempty"`
	Missing       bool                     `json:"missing,omitempty"`
	ListedVersion int                      `json:"listed_version,omitempty"`
	Escalate      *hubwire.GroupEscalation `json:"escalate,omitempty"`
	Entry         *EntryView               `json:"entry,omitempty"`
	Row           *SetRowView              `json:"row,omitempty"`
}

type GroupVersionView struct {
	GroupID         string            `json:"group_id"`
	Version         int               `json:"version"`
	Title           string            `json:"title"`
	Description     string            `json:"description,omitempty"`
	Status          string            `json:"status"`
	StatusReason    string            `json:"status_reason,omitempty"`
	HiddenFrom      string            `json:"hidden_from,omitempty"`
	Author          string            `json:"author"`
	AuthorHMAC      string            `json:"author_hmac"`
	AuthorBanned    bool              `json:"author_banned,omitempty"`
	ASNObserved     string            `json:"asn_observed,omitempty"`
	CountryObserved string            `json:"country_observed,omitempty"`
	ASNHint         string            `json:"asn_hint,omitempty"`
	CountryHint     string            `json:"country_hint,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Members         []GroupMemberView `json:"members"`
}

type GroupDetailView struct {
	ID                 string             `json:"id"`
	Author             string             `json:"author"`
	AuthorHMAC         string             `json:"author_hmac"`
	AuthorBanned       bool               `json:"author_banned,omitempty"`
	DerivedFromID      string             `json:"derived_from_id,omitempty"`
	DerivedFromVersion int                `json:"derived_from_version,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	PublishedVersion   int                `json:"published_version,omitempty"`
	Versions           []GroupVersionView `json:"versions"`
}

type GroupActionRequest struct {
	Reason       string                  `json:"reason"`
	ExpectStatus string                  `json:"expect_status,omitempty"`
	Members      []ModerationItemRequest `json:"members,omitempty"`
	Force        bool                    `json:"force,omitempty"`
	KeepReports  bool                    `json:"keep_reports,omitempty"`
	DryRun       bool                    `json:"dry_run,omitempty"`
}

type GroupEffectView struct {
	GroupID     string                 `json:"group_id"`
	Version     int                    `json:"version"`
	Title       string                 `json:"title,omitempty"`
	From        string                 `json:"from,omitempty"`
	To          string                 `json:"to,omitempty"`
	Listed      int                    `json:"listed"`
	ListedAfter int                    `json:"listed_after"`
	Withheld    string                 `json:"withheld,omitempty"`
	OK          bool                   `json:"ok"`
	Code        string                 `json:"code,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

type GroupMemberEffectView struct {
	ModerationItemView
	Position int  `json:"position"`
	Brought  bool `json:"brought"`
	Missing  bool `json:"missing,omitempty"`
}

type GroupModerationView struct {
	Notice  string                  `json:"notice"`
	Code    string                  `json:"code"`
	Params  map[string]interface{}  `json:"params,omitempty"`
	BatchID string                  `json:"batch_id,omitempty"`
	Group   GroupEffectView         `json:"group"`
	Members []GroupMemberEffectView `json:"members"`
	Build   *BuildStateView         `json:"build,omitempty"`
}

type groupContext struct {
	tabs    *versionTabs
	ec      *entryContext
	pool    map[string][]store.Vote
	brought map[string]int
	at      time.Time
	now     time.Time
}

func broughtKey(groupID, setID string, version int) string {
	return groupID + "/" + setID + "/" + strconv.Itoa(version)
}

func (s *Server) rowClock() (time.Time, time.Time) {
	now := s.now().UTC()
	at := now
	if latest := s.latest(); latest != nil {
		if t := parseDay(latest.Catalogue.GeneratedAt); !t.IsZero() {
			at = t
		}
	}
	return at, now
}

func (s *Server) groupContext(ctx context.Context, tabs *versionTabs, ec *entryContext, versions []store.GroupVersion) (*groupContext, error) {
	pool, err := s.Store.ScoringVotesByFP(ctx)
	if err != nil {
		return nil, err
	}
	gc := &groupContext{tabs: tabs, ec: ec, pool: pool, brought: map[string]int{}}
	for _, gv := range versions {
		for _, m := range gv.Members {
			if key := broughtKey(gv.GroupID, m.SetID, m.SetVersion); m.Brought && gc.brought[key] == 0 {
				gc.brought[key] = gv.Version
			}
		}
	}
	gc.at, gc.now = s.rowClock()
	return gc, nil
}

func (s *Server) groupVersionView(ctx context.Context, gv store.GroupVersion, gc *groupContext) (GroupVersionView, error) {
	view := GroupVersionView{
		GroupID:         gv.GroupID,
		Version:         gv.Version,
		Title:           gv.Title,
		Description:     gv.Description,
		Status:          gv.Status,
		StatusReason:    gv.StatusReason,
		HiddenFrom:      gv.HiddenFrom,
		Author:          hubdata.AuthorLabel(gv.UploaderHMAC),
		AuthorHMAC:      gv.UploaderHMAC,
		AuthorBanned:    gc.ec.banned[gv.UploaderHMAC],
		ASNObserved:     gv.ASNObserved,
		CountryObserved: gv.CountryObserved,
		ASNHint:         gv.ASNHint,
		CountryHint:     gv.CountryHint,
		CreatedAt:       gv.CreatedAt,
		UpdatedAt:       gv.UpdatedAt,
		Members:         make([]GroupMemberView, 0, len(gv.Members)),
	}
	for _, m := range gv.Members {
		member := GroupMemberView{Position: m.Position, SetID: m.SetID, Version: m.SetVersion, Brought: m.Brought, Escalate: m.Escalate}
		if !m.Brought {
			member.BroughtIn = gc.brought[broughtKey(gv.GroupID, m.SetID, m.SetVersion)]
		}
		if gc.ec.withheld[m.SetID] == "" {
			member.ListedVersion = gc.tabs.newest[m.SetID].Version
		}
		v, err := s.Store.GetVersion(ctx, m.SetID, m.SetVersion)
		switch {
		case errors.Is(err, store.ErrNotFound):
			member.Missing = true
		case err != nil:
			return view, err
		case m.Brought || (member.BroughtIn > 0 && v.Status == hubwire.SetStatusPending):
			entry := s.entry(*v, gc.ec)
			entry.Lineage = lineage(*v, gc.tabs.newest)
			member.Entry = &entry
		default:
			row := s.setRow(*v, gc.ec, gc.pool, gc.at, gc.now)
			member.Row = &row
		}
		view.Members = append(view.Members, member)
	}
	return view, nil
}

func groupPath(r *http.Request) (string, int, bool) {
	id := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if !hubdata.ValidSetID(id) || err != nil || version <= 0 {
		return "", 0, false
	}
	return id, version, true
}

func (s *Server) groupDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hubdata.ValidSetID(id) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a group")
		return
	}
	ctx := r.Context()
	g, versions, err := s.Store.GetGroup(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	tabs, err := s.tabs(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	ec, err := s.entryContext(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	gc, err := s.groupContext(ctx, tabs, ec, versions)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := GroupDetailView{
		ID:                 g.ID,
		Author:             hubdata.AuthorLabel(g.AuthorHMAC),
		AuthorHMAC:         g.AuthorHMAC,
		AuthorBanned:       ec.banned[g.AuthorHMAC],
		DerivedFromID:      g.DerivedFromID,
		DerivedFromVersion: g.DerivedFromVersion,
		CreatedAt:          g.CreatedAt,
		UpdatedAt:          g.UpdatedAt,
		Versions:           make([]GroupVersionView, 0, len(versions)),
	}
	if latest := s.latest(); latest != nil {
		for _, cg := range latest.Catalogue.Groups {
			if cg.ID == id {
				view.PublishedVersion = cg.Version
			}
		}
	}
	for _, gv := range versions {
		v, err := s.groupVersionView(ctx, gv, gc)
		if err != nil {
			s.fail(w, err)
			return
		}
		view.Versions = append(view.Versions, v)
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) groupAction(w http.ResponseWriter, r *http.Request) {
	id, version, ok := groupPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a group version")
		return
	}
	action := r.PathValue("action")
	if !moderation.IsVersionAction(action) {
		writeError(w, http.StatusNotFound, codeNotFound, "moderation knows approve, reject, hide and restore")
		return
	}
	var req GroupActionRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	members := make([]moderation.Ref, 0, len(req.Members))
	for _, m := range req.Members {
		members = append(members, moderation.Ref{SetID: m.SetID, Version: m.Version, ExpectStatus: m.ExpectStatus, ExpectKept: m.ExpectKept})
	}
	ctx := r.Context()
	res, err := s.Moderation.ModerateGroup(ctx, s.actor(r), action, moderation.GroupRef{GroupID: id, Version: version, ExpectStatus: req.ExpectStatus, Members: members}, moderation.Options{
		Reason:      req.Reason,
		Force:       req.Force,
		KeepReports: req.KeepReports,
		DryRun:      req.DryRun,
	})
	if err != nil {
		s.failModeration(w, err)
		return
	}
	view := GroupModerationView{
		Notice:  res.Notice,
		Code:    res.Code,
		Params:  res.Params,
		BatchID: res.BatchID,
		Group: GroupEffectView{
			GroupID:     res.Group.GroupID,
			Version:     res.Group.Version,
			Title:       res.Group.Title,
			From:        res.Group.From,
			To:          res.Group.To,
			Listed:      res.Group.Listed,
			ListedAfter: res.Group.ListedAfter,
			Withheld:    res.Group.Withheld,
			OK:          res.Group.OK,
			Code:        res.Group.Code,
			Params:      res.Group.Params,
		},
		Members: make([]GroupMemberEffectView, 0, len(res.Members)),
		Build:   s.buildState(ctx),
	}
	for _, m := range res.Members {
		view.Members = append(view.Members, GroupMemberEffectView{ModerationItemView: moderationItems([]moderation.Item{m.Item})[0], Position: m.Position, Brought: m.Brought, Missing: m.Missing})
	}
	writeJSON(w, http.StatusOK, view)
}
