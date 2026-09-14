package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	maxReasonRunes  = 500
	recentDecisions = 50
	feedbackLimit   = 200
	maxFeedback     = 1000

	ActionApprove = "approve"
	ActionReject  = "reject"
	ActionHide    = "hide"
	ActionBan     = "ban"
	ActionUnban   = "unban"
	ActionTrust   = "trust"
	ActionUntrust = "untrust"
	ActionRemove  = "remove"
)

type actionRequest struct {
	Reason string `json:"reason"`
}

type revokeRequest struct {
	KeyID string `json:"key_id"`
}

func (s *Server) mountAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PathAPI+"/session", s.session)
	mux.HandleFunc("POST "+PathAPI+"/login", s.login)
	mux.HandleFunc("POST "+PathAPI+"/logout", s.logout)

	mux.Handle("GET "+PathAPI+"/overview", s.guard(s.overview))
	mux.Handle("GET "+PathAPI+"/sets", s.guard(s.sets))
	mux.Handle("GET "+PathAPI+"/sets/{id}", s.guard(s.setDetail))
	mux.Handle("POST "+PathAPI+"/sets/{id}/{version}/{action}", s.guard(s.setAction))
	mux.Handle("GET "+PathAPI+"/keys", s.guard(s.keys))
	mux.Handle("POST "+PathAPI+"/keys/{key}/{action}", s.guard(s.keyAction))
	mux.Handle("GET "+PathAPI+"/mirrors", s.guard(s.mirrors))
	mux.Handle("POST "+PathAPI+"/mirrors/{id}/{action}", s.guard(s.mirrorAction))
	mux.Handle("GET "+PathAPI+"/feedback", s.guard(s.feedback))
	mux.Handle("POST "+PathAPI+"/catalogue/build", s.guard(s.catalogueBuild))
	mux.Handle("POST "+PathAPI+"/catalogue/epoch", s.guard(s.catalogueEpoch))
	mux.Handle("POST "+PathAPI+"/catalogue/revoke", s.guard(s.catalogueRevoke))
	mux.Handle("GET "+PathAPI+"/settings", s.guard(s.settings))
	mux.Handle("PUT "+PathAPI+"/settings", s.guard(s.saveSettings))
}

func (s *Server) rebuild() {
	if s.Rebuild == nil {
		return
	}
	if err := s.Rebuild(); err != nil {
		log.Printf("web: catalogue rebuild after moderation: %v", err)
	}
}

func cleanReason(raw string) string {
	reason := strings.TrimSpace(raw)
	runes := []rune(reason)
	if len(runes) > maxReasonRunes {
		reason = strings.TrimSpace(string(runes[:maxReasonRunes]))
	}
	return reason
}

func (s *Server) readAction(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req actionRequest
	if err := readBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return "", false
	}
	return cleanReason(req.Reason), true
}

type versionGroups struct {
	pending    []store.Version
	listed     []store.Version
	superseded []store.Version
	hidden     []store.Version
	rejected   []store.Version
	newest     map[string]store.Version
	versions   map[string][]int
}

func (s *Server) groups(ctx context.Context) (*versionGroups, error) {
	g := &versionGroups{}
	var err error
	if g.pending, err = s.Store.PendingVersions(ctx); err != nil {
		return nil, err
	}
	if g.listed, err = s.Store.ListedVersions(ctx); err != nil {
		return nil, err
	}
	active, err := s.Store.ActiveVersions(ctx)
	if err != nil {
		return nil, err
	}
	if g.hidden, err = s.Store.VersionsByStatus(ctx, hubwire.SetStatusHidden); err != nil {
		return nil, err
	}
	if g.rejected, err = s.Store.VersionsByStatus(ctx, hubwire.SetStatusRejected); err != nil {
		return nil, err
	}
	g.newest = make(map[string]store.Version, len(g.listed))
	for _, v := range g.listed {
		g.newest[v.SetID] = v
	}
	g.versions = make(map[string][]int, len(g.listed))
	for _, v := range active {
		g.versions[v.SetID] = append(g.versions[v.SetID], v.Version)
		if v.Version < g.newest[v.SetID].Version {
			g.superseded = append(g.superseded, v)
		}
	}
	sort.SliceStable(g.listed, func(i, j int) bool { return g.listed[i].UpdatedAt.After(g.listed[j].UpdatedAt) })
	return g, nil
}

func lineage(v store.Version, newest map[string]store.Version) *LineageView {
	current, ok := newest[v.SetID]
	switch {
	case ok:
		return &LineageView{Kind: LineageReplaces, CurrentVersion: current.Version}
	case v.Version > 1:
		return &LineageView{Kind: LineageRelists}
	default:
		return &LineageView{Kind: LineageFirst}
	}
}

func (s *Server) entries(ctx context.Context, versions []store.Version, ec *entryContext, limit int) []EntryView {
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	out := make([]EntryView, 0, len(versions))
	for _, v := range versions {
		out = append(out, s.entry(ctx, v, ec))
	}
	return out
}

func (s *Server) sets(w http.ResponseWriter, r *http.Request) {
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
	view := SetsView{
		Pending:    s.entries(ctx, g.pending, ec, 0),
		Listed:     s.entries(ctx, g.listed, ec, 0),
		Superseded: s.entries(ctx, g.superseded, ec, 0),
		Hidden:     s.entries(ctx, g.hidden, ec, recentDecisions),
		Rejected:   s.entries(ctx, g.rejected, ec, recentDecisions),
	}
	for i := range view.Pending {
		view.Pending[i].Lineage = lineage(g.pending[i], g.newest)
	}
	for i := range view.Listed {
		view.Listed[i].Versions = g.versions[view.Listed[i].SetID]
	}
	for i := range view.Superseded {
		current := g.newest[view.Superseded[i].SetID]
		view.Superseded[i].SupersededBy = current.Version
		view.Superseded[i].SupersededAt = optionalTime(current.UpdatedAt)
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) setDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hubdata.ValidSetID(id) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set")
		return
	}
	ctx := r.Context()
	set, versions, err := s.Store.GetSet(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	ec, err := s.entryContext(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := SetDetailView{
		ID:                 set.ID,
		Author:             hubdata.AuthorLabel(set.AuthorHMAC),
		AuthorHMAC:         set.AuthorHMAC,
		CurrentVersion:     set.CurrentVersion,
		DerivedFromID:      set.DerivedFromID,
		DerivedFromVersion: set.DerivedFromVersion,
		CreatedAt:          set.CreatedAt,
		UpdatedAt:          set.UpdatedAt,
		Versions:           s.entries(ctx, versions, ec, 0),
		Votes:              []VoteView{},
	}
	for _, v := range versions {
		votes, err := s.Store.VotesForVersion(ctx, v.SetID, v.Version)
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, vote := range votes {
			view.Votes = append(view.Votes, voteView(vote))
		}
	}
	sort.SliceStable(view.Votes, func(i, j int) bool { return view.Votes[i].ReceivedAt.After(view.Votes[j].ReceivedAt) })
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) setAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if !hubdata.ValidSetID(id) || err != nil || version <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return
	}
	ctx := r.Context()
	if _, err := s.Store.GetVersion(ctx, id, version); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	action := r.PathValue("action")
	reason, ok := s.readAction(w, r)
	if !ok {
		return
	}
	now := s.now()
	ref := id + "/" + strconv.Itoa(version)
	var notice string
	switch action {
	case ActionApprove:
		err = s.Store.Approve(ctx, id, version, now)
		notice = "approved " + ref
	case ActionReject:
		if reason == "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, "a rejection needs a reason")
			return
		}
		err = s.Store.Reject(ctx, id, version, reason, now)
		notice = "rejected " + ref
	case ActionHide:
		if reason == "" {
			reason = "hidden by moderator"
		}
		err = s.Store.Hide(ctx, id, version, reason, now)
		notice = "hidden " + ref
	default:
		writeError(w, http.StatusNotFound, codeNotFound, "moderation knows approve, reject and hide")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.rebuild()
	writeJSON(w, http.StatusOK, ActionResult{Notice: notice})
}

func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.Keys(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]KeyView, 0, len(keys))
	for _, k := range keys {
		out = append(out, keyView(k))
	}
	writeJSON(w, http.StatusOK, out)
}

func validKeyHMAC(key string) bool {
	if len(key) != 64 {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) keyAction(w http.ResponseWriter, r *http.Request) {
	key := strings.ToLower(r.PathValue("key"))
	if !validKeyHMAC(key) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a key")
		return
	}
	reason, ok := s.readAction(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var err error
	var notice string
	switch r.PathValue("action") {
	case ActionBan:
		if reason == "" {
			reason = "banned by moderator"
		}
		err = s.Store.BanKey(ctx, key, reason, s.now())
		notice = "banned " + hubdata.AuthorLabel(key)
	case ActionUnban:
		err = s.Store.UnbanKey(ctx, key)
		notice = "unbanned " + hubdata.AuthorLabel(key)
	case ActionTrust:
		err = s.Store.TrustKey(ctx, key, s.now())
		notice = "trusted " + hubdata.AuthorLabel(key)
	case ActionUntrust:
		err = s.Store.UntrustKey(ctx, key)
		notice = "untrusted " + hubdata.AuthorLabel(key)
	default:
		writeError(w, http.StatusNotFound, codeNotFound, "keys can be banned, unbanned, trusted or untrusted")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if r.PathValue("action") == ActionBan || r.PathValue("action") == ActionUnban {
		s.rebuild()
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: notice})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	current, err := s.Store.Settings(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsView(current))
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var req SettingsView
	if err := readBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	in := req.Limits.settings()
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if err := s.Store.SaveSettings(r.Context(), in); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsView(in))
}

func (s *Server) mirrors(w http.ResponseWriter, r *http.Request) {
	mirrors, err := s.Store.Mirrors(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]MirrorView, 0, len(mirrors))
	for _, m := range mirrors {
		out = append(out, mirrorView(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) mirrorAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a mirror")
		return
	}
	ctx := r.Context()
	m, err := s.Store.GetMirror(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	reason, ok := s.readAction(w, r)
	if !ok {
		return
	}
	var notice string
	switch r.PathValue("action") {
	case ActionApprove:
		err = s.Store.SetMirrorStatus(ctx, id, store.MirrorApproved, "", s.now())
		notice = "approved mirror " + m.URL
	case ActionReject:
		if reason == "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, "a rejection needs a reason")
			return
		}
		err = s.Store.SetMirrorStatus(ctx, id, store.MirrorRejected, reason, s.now())
		notice = "rejected mirror " + m.URL
	case ActionRemove:
		err = s.Store.DeleteMirror(ctx, id)
		notice = "removed mirror " + m.URL
	default:
		writeError(w, http.StatusNotFound, codeNotFound, "mirrors can be approved, rejected or removed")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.rebuild()
	writeJSON(w, http.StatusOK, ActionResult{Notice: notice})
}

func (s *Server) feedback(w http.ResponseWriter, r *http.Request) {
	limit := feedbackLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "limit must be a positive number")
			return
		}
		limit = min(n, maxFeedback)
	}
	ctx := r.Context()
	votes, err := s.Store.RecentVotes(ctx, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	reports, err := s.Store.RecentReports(ctx, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := FeedbackView{Votes: make([]VoteView, 0, len(votes)), Reports: make([]ReportView, 0, len(reports))}
	for _, v := range votes {
		view.Votes = append(view.Votes, voteView(v))
	}
	for _, rep := range reports {
		view.Reports = append(view.Reports, reportView(rep))
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) catalogueView(ctx context.Context) (CatalogueView, error) {
	view := CatalogueView{Mirrors: []string{}, RevokedKeys: []string{}}
	var err error
	if view.Epoch, err = s.Store.Epoch(ctx); err != nil {
		return view, err
	}
	if view.Dirty, err = s.Store.Dirty(ctx); err != nil {
		return view, err
	}
	builtAt, err := s.Store.BuiltAt(ctx)
	if err != nil {
		return view, err
	}
	view.BuiltAt = optionalTime(builtAt)
	revoked, err := s.Store.RevokedKeys(ctx)
	if err != nil {
		return view, err
	}
	if revoked != nil {
		view.RevokedKeys = revoked
	}
	if s.Catalogue == nil {
		return view, nil
	}
	latest := s.Catalogue.Latest()
	if latest == nil {
		return view, nil
	}
	view.Published = true
	view.File = latest.Manifest.Catalogue.File
	view.Size = latest.Manifest.Catalogue.Size
	view.Epoch = latest.Manifest.Epoch
	view.Seq = latest.Manifest.Seq
	view.GeneratedAt = latest.Manifest.GeneratedAt
	view.ExpiresAt = latest.Manifest.ExpiresAt
	view.Sets = len(latest.Catalogue.Sets)
	view.Blobs = len(latest.Catalogue.Blobs)
	if latest.Manifest.Mirrors != nil {
		view.Mirrors = latest.Manifest.Mirrors
	}
	return view, nil
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	view := OverviewView{Version: s.Version, KeyID: s.KeyID, PublicURL: s.PublicURL, Now: s.now(), Geo: []geo.FileStatus{}}
	var err error
	if view.Catalogue, err = s.catalogueView(ctx); err != nil {
		s.fail(w, err)
		return
	}
	g, err := s.groups(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.Counts = CountsView{
		Pending:    len(g.pending),
		Listed:     len(g.listed),
		Superseded: len(g.superseded),
		Hidden:     len(g.hidden),
		Rejected:   len(g.rejected),
	}
	keys, err := s.Store.Keys(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.Counts.Keys = len(keys)
	for _, k := range keys {
		if k.Banned {
			view.Counts.Banned++
		}
	}
	mirrors, err := s.Store.Mirrors(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, m := range mirrors {
		switch m.Status {
		case store.MirrorPending:
			view.Counts.MirrorsPending++
		case store.MirrorApproved:
			view.Counts.MirrorsApproved++
		case store.MirrorRejected:
			view.Counts.MirrorsRejected++
		}
	}
	if view.Counts.Votes, err = s.Store.CountVotes(ctx); err != nil {
		s.fail(w, err)
		return
	}
	if view.Counts.Reports, err = s.Store.CountReports(ctx); err != nil {
		s.fail(w, err)
		return
	}
	if s.Geo != nil {
		view.Geo = s.Geo.Status()
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) catalogueBuild(w http.ResponseWriter, r *http.Request) {
	if s.Catalogue == nil {
		writeError(w, http.StatusServiceUnavailable, codeNotBuilt, "this hub has no catalogue builder")
		return
	}
	result, err := s.Catalogue.Build(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: fmt.Sprintf("published %s with %d sets", result.Manifest.Catalogue.File, len(result.Catalogue.Sets))})
}

func (s *Server) catalogueEpoch(w http.ResponseWriter, r *http.Request) {
	epoch, err := s.Store.NewEpoch(r.Context(), s.now())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.rebuild()
	writeJSON(w, http.StatusOK, ActionResult{Notice: "started epoch " + strconv.FormatInt(epoch, 10)})
}

func (s *Server) catalogueRevoke(w http.ResponseWriter, r *http.Request) {
	var req revokeRequest
	if err := readBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	keyID := strings.TrimSpace(req.KeyID)
	if _, err := hubwire.DecodeKey(keyID); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "key_id is not an ed25519 key id: "+err.Error())
		return
	}
	if keyID == s.KeyID {
		writeError(w, http.StatusBadRequest, codeBadRequest, "refusing to revoke the key this hub signs with")
		return
	}
	ctx := r.Context()
	if err := s.Store.RevokeKey(ctx, keyID); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.Store.MarkDirty(ctx); err != nil {
		s.fail(w, err)
		return
	}
	s.rebuild()
	writeJSON(w, http.StatusOK, ActionResult{Notice: "revoked " + keyID})
}
