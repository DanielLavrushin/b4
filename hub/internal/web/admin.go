package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	maxReasonRunes  = 500
	recentDecisions = 50
	feedbackLimit   = 200
	maxFeedback     = 1000

	ActionApprove   = moderation.ActionApprove
	ActionReject    = moderation.ActionReject
	ActionHide      = moderation.ActionHide
	ActionRestore   = moderation.ActionRestore
	ActionBan       = moderation.ActionBan
	ActionUnban     = moderation.ActionUnban
	ActionTrust     = moderation.ActionTrust
	ActionUntrust   = moderation.ActionUntrust
	ActionRemove    = moderation.ActionRemove
	ActionWithdraw  = moderation.ActionWithdraw
	ActionReinstate = moderation.ActionReinstate
	ActionDismiss   = moderation.ActionDismiss
	ActionResolve   = moderation.ActionResolve
	ActionReopen    = moderation.ActionReopen
)

type actionRequest struct {
	Reason       string `json:"reason"`
	ExpectStatus string `json:"expect_status,omitempty"`
	Force        bool   `json:"force,omitempty"`
	Withdraw     bool   `json:"withdraw,omitempty"`
	KeepReports  bool   `json:"keep_reports,omitempty"`
}

type noteRequest struct {
	Note string `json:"note"`
}

type revokeRequest struct {
	KeyID        string `json:"key_id"`
	Confirm      string `json:"confirm"`
	AllowBuiltin bool   `json:"allow_builtin,omitempty"`
}

type deleteRequest struct {
	Confirm string `json:"confirm"`
}

type buildRequest struct {
	Wait bool `json:"wait"`
}

func (s *Server) mountAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PathAPI+"/session", s.session)
	mux.HandleFunc("POST "+PathAPI+"/login", s.login)
	mux.HandleFunc("POST "+PathAPI+"/logout", s.logout)

	mux.Handle("GET "+PathAPI+"/overview", s.guard(s.overview))
	mux.Handle("GET "+PathAPI+"/counts", s.guard(s.counts))
	mux.Handle("GET "+PathAPI+"/health", s.guard(s.healthHandler))
	mux.Handle("GET "+PathAPI+"/sets", s.guard(s.sets))
	mux.Handle("GET "+PathAPI+"/sets/rows", s.guard(s.setRows))
	mux.Handle("GET "+PathAPI+"/queue", s.guard(s.queue))
	mux.Handle("GET "+PathAPI+"/sets/{id}", s.guard(s.setDetail))
	mux.Handle("POST "+PathAPI+"/sets/{id}/{version}/{action}", s.guard(s.setAction))
	mux.Handle("POST "+PathAPI+"/sets/{id}/{version}/preview", s.guard(s.setPreview))
	mux.Handle("POST "+PathAPI+"/sets/{id}/{version}/edit", s.guard(s.setEdit))
	mux.Handle("POST "+PathAPI+"/sets/{id}/{version}/reports/{action}", s.guard(s.versionReports))
	mux.Handle("GET "+PathAPI+"/sets/{id}/{version}/similar", s.guard(s.similar))
	mux.Handle("POST "+PathAPI+"/sets/{id}/{version}/text", s.guard(s.setText))
	mux.Handle("POST "+PathAPI+"/sets/{id}/withdraw", s.guard(s.setWithdraw))
	mux.Handle("POST "+PathAPI+"/sets/{id}/reinstate", s.guard(s.setReinstate))
	mux.Handle("POST "+PathAPI+"/sets/{id}/delete", s.guard(s.setDelete))
	mux.Handle("POST "+PathAPI+"/moderation", s.guard(s.moderate))
	mux.Handle("GET "+PathAPI+"/keys", s.guard(s.keyList))
	mux.Handle("GET "+PathAPI+"/keys/notable", s.guard(s.notableKeys))
	mux.Handle("GET "+PathAPI+"/keys/{key}", s.guard(s.keyDetail))
	mux.Handle("PUT "+PathAPI+"/keys/{key}", s.guard(s.keyProfile))
	mux.Handle("GET "+PathAPI+"/keys/{key}/impact", s.guard(s.keyImpact))
	mux.Handle("POST "+PathAPI+"/keys/{key}/{action}", s.guard(s.keyAction))
	mux.Handle("GET "+PathAPI+"/mirrors", s.guard(s.mirrors))
	mux.Handle("POST "+PathAPI+"/mirrors/check", s.guard(s.mirrorsCheck))
	mux.Handle("POST "+PathAPI+"/mirrors/{id}/check", s.guard(s.mirrorCheck))
	mux.Handle("POST "+PathAPI+"/mirrors/{id}/{action}", s.guard(s.mirrorAction))
	mux.Handle("GET "+PathAPI+"/feedback", s.guard(s.feedback))
	mux.Handle("GET "+PathAPI+"/votes", s.guard(s.votes))
	mux.Handle("GET "+PathAPI+"/reports", s.guard(s.reports))
	mux.Handle("POST "+PathAPI+"/reports/bulk", s.guard(s.reportsBulk))
	mux.Handle("POST "+PathAPI+"/reports/{id}/{action}", s.guard(s.reportAction))
	mux.Handle("GET "+PathAPI+"/catalogue/status", s.guard(s.catalogueStatus))
	mux.Handle("GET "+PathAPI+"/catalogue/builds", s.guard(s.catalogueBuilds))
	mux.Handle("POST "+PathAPI+"/catalogue/build", s.guard(s.catalogueBuild))
	mux.Handle("POST "+PathAPI+"/catalogue/epoch", s.guard(s.catalogueEpoch))
	mux.Handle("POST "+PathAPI+"/catalogue/revoke", s.guard(s.catalogueRevoke))
	mux.Handle("GET "+PathAPI+"/audit", s.guard(s.audit))
	mux.Handle("GET "+PathAPI+"/stats", s.guard(s.stats))
	mux.Handle("GET "+PathAPI+"/reasons", s.guard(s.reasons))
	mux.Handle("POST "+PathAPI+"/reasons", s.guard(s.createReason))
	mux.Handle("PUT "+PathAPI+"/reasons/{id}", s.guard(s.updateReason))
	mux.Handle("DELETE "+PathAPI+"/reasons/{id}", s.guard(s.deleteReason))
	mux.Handle("GET "+PathAPI+"/settings", s.guard(s.settings))
	mux.Handle("PUT "+PathAPI+"/settings", s.guard(s.saveSettings))
	mux.Handle("GET "+PathAPI+"/notify", s.guard(s.notifySettings))
	mux.Handle("PUT "+PathAPI+"/notify", s.guard(s.saveNotify))
	mux.Handle("POST "+PathAPI+"/notify/test", s.guard(s.testNotify))
	mux.Handle("GET "+PathAPI+"/{rest...}", s.guard(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, codeNotFound, "no such console endpoint")
	}))
}

func clipRunes(raw string, max int) string {
	return moderation.CleanText(raw, max)
}

func cleanReason(raw string) string {
	return clipRunes(raw, maxReasonRunes)
}

func (s *Server) actor(r *http.Request) moderation.Actor {
	a := moderation.Actor{Kind: store.ActorBasic}
	if c, err := r.Cookie(sessionCookie); err == nil && s.validToken(c.Value) {
		sum := sha256.Sum256([]byte(c.Value))
		a.Kind = store.ActorConsole
		a.Ref = hex.EncodeToString(sum[:4])
	}
	if ip := asn.ClientIP(r); ip != nil {
		a.IP = ip.String()
	}
	return a
}

var moderationStatus = map[string]int{
	moderation.CodeNotFound:          http.StatusNotFound,
	moderation.CodeUnknownKey:        http.StatusNotFound,
	moderation.CodeInvalidTransition: http.StatusConflict,
	moderation.CodeStale:             http.StatusConflict,
	moderation.CodeAuthorBanned:      http.StatusConflict,
	moderation.CodeSetWithdrawn:      http.StatusConflict,
	moderation.CodeNotWithdrawn:      http.StatusConflict,
	moderation.CodeBatchInvalid:      http.StatusConflict,
	moderation.CodeNotPending:        http.StatusConflict,
	moderation.CodeDuplicate:         http.StatusConflict,
	moderation.CodeNotEditable:       http.StatusConflict,
}

func (s *Server) failModeration(w http.ResponseWriter, err error) {
	var me *moderation.Error
	if errors.As(err, &me) {
		status, ok := moderationStatus[me.Code]
		if !ok {
			status = http.StatusBadRequest
		}
		body := ErrorBody{Code: me.Code, Error: me.Message, Params: me.Params}
		if len(me.Items) > 0 {
			body.Items = moderationItems(me.Items)
		}
		writeJSON(w, status, body)
		return
	}
	s.fail(w, err)
}

func (s *Server) result(ctx context.Context, res moderation.Result) ActionResult {
	return ActionResult{Notice: res.Notice, Code: res.Code, Params: res.Params, Build: s.buildState(ctx)}
}

func (s *Server) readJSON(w http.ResponseWriter, r *http.Request, into interface{}) bool {
	if err := readBody(w, r, into); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return false
	}
	return true
}

type versionGroups struct {
	pending    []store.Version
	listed     []store.Version
	withheld   []store.Version
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
	if g.listed, err = s.Store.CatalogueVersions(ctx); err != nil {
		return nil, err
	}
	if g.withheld, err = s.Store.WithheldVersions(ctx); err != nil {
		return nil, err
	}
	eligible, err := s.Store.ListedVersions(ctx)
	if err != nil {
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
	g.newest = make(map[string]store.Version, len(eligible))
	for _, v := range eligible {
		g.newest[v.SetID] = v
	}
	g.versions = make(map[string][]int, len(eligible))
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
	case ok && v.Version > current.Version:
		return &LineageView{Kind: LineageReplaces, CurrentVersion: current.Version}
	case ok:
		return &LineageView{Kind: LineageOlder, CurrentVersion: current.Version}
	case v.Version > 1:
		return &LineageView{Kind: LineageRelists}
	default:
		return &LineageView{Kind: LineageFirst}
	}
}

func (s *Server) entries(versions []store.Version, ec *entryContext, limit int) []EntryView {
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	out := make([]EntryView, 0, len(versions))
	for _, v := range versions {
		out = append(out, s.entry(v, ec))
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
		Pending:    s.entries(g.pending, ec, 0),
		Listed:     s.entries(g.listed, ec, 0),
		Withheld:   s.entries(g.withheld, ec, 0),
		Superseded: s.entries(g.superseded, ec, 0),
		Hidden:     s.entries(g.hidden, ec, recentDecisions),
		Rejected:   s.entries(g.rejected, ec, recentDecisions),
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
		WithdrawnAt:        optionalTime(set.WithdrawnAt),
		WithdrawReason:     set.WithdrawReason,
		Withheld:           ec.withheld[set.ID],
		AuthorBanned:       ec.banned[set.AuthorHMAC],
		Versions:           s.entries(versions, ec, 0),
		Votes:              []VoteView{},
	}
	if view.Withheld == "" {
		for _, v := range versions {
			if v.Status == hubwire.SetStatusActive && v.Version > view.ListedVersion {
				view.ListedVersion = v.Version
			}
		}
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

func versionPath(r *http.Request) (string, int, bool) {
	id := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if !hubdata.ValidSetID(id) || err != nil || version <= 0 {
		return "", 0, false
	}
	return id, version, true
}

func (s *Server) setAction(w http.ResponseWriter, r *http.Request) {
	id, version, ok := versionPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return
	}
	action := r.PathValue("action")
	if !moderation.IsVersionAction(action) {
		writeError(w, http.StatusNotFound, codeNotFound, "moderation knows approve, reject, hide and restore")
		return
	}
	var req actionRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	res, err := s.Moderation.Moderate(ctx, s.actor(r), action, []moderation.Ref{{SetID: id, Version: version, ExpectStatus: req.ExpectStatus}}, moderation.Options{
		Reason:      req.Reason,
		Force:       req.Force,
		Withdraw:    req.Withdraw,
		KeepReports: req.KeepReports,
	})
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(ctx, res))
}

func (s *Server) moderate(w http.ResponseWriter, r *http.Request) {
	var req ModerationRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	refs := make([]moderation.Ref, 0, len(req.Items))
	for _, it := range req.Items {
		refs = append(refs, moderation.Ref{SetID: it.SetID, Version: it.Version, ExpectStatus: it.ExpectStatus})
	}
	ctx := r.Context()
	res, err := s.Moderation.Moderate(ctx, s.actor(r), req.Action, refs, moderation.Options{
		Reason:      req.Reason,
		Force:       req.Force,
		Withdraw:    req.Withdraw,
		KeepReports: req.KeepReports,
		Partial:     req.Partial,
		DryRun:      req.DryRun,
	})
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ModerationView{
		Notice:  res.Notice,
		Code:    res.Code,
		Params:  res.Params,
		BatchID: res.BatchID,
		Items:   moderationItems(res.Items),
		Build:   s.buildState(ctx),
	})
}

func (s *Server) setWithdraw(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hubdata.ValidSetID(id) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set")
		return
	}
	var req actionRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.Withdraw(r.Context(), s.actor(r), id, req.Reason)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
}

func (s *Server) setReinstate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hubdata.ValidSetID(id) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set")
		return
	}
	res, err := s.Moderation.Reinstate(r.Context(), s.actor(r), id)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
}

func (s *Server) setDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hubdata.ValidSetID(id) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set")
		return
	}
	var req deleteRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.Delete(r.Context(), s.actor(r), id, req.Confirm)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
}

func (s *Server) versionReports(w http.ResponseWriter, r *http.Request) {
	id, version, ok := versionPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return
	}
	var req noteRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.VersionReports(r.Context(), s.actor(r), id, version, r.PathValue("action"), req.Note)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: res.Notice, Code: res.Code, Params: res.Params})
}

func (s *Server) keyImpact(w http.ResponseWriter, r *http.Request) {
	impact, err := s.Moderation.KeyImpact(r.Context(), r.PathValue("key"))
	if err != nil {
		s.failModeration(w, err)
		return
	}
	view := KeyImpactView{Listed: []SetRefView{}, Pending: []SetRefView{}, Votes: impact.Votes, VotedSets: impact.VotedSets, Reports: impact.Reports, Mirrors: []MirrorView{}}
	for _, v := range impact.Listed {
		view.Listed = append(view.Listed, SetRefView{SetID: v.SetID, Version: v.Version, Title: v.Title})
	}
	for _, v := range impact.Pending {
		view.Pending = append(view.Pending, SetRefView{SetID: v.SetID, Version: v.Version, Title: v.Title})
	}
	for _, m := range impact.Mirrors {
		view.Mirrors = append(view.Mirrors, mirrorView(m))
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) keyAction(w http.ResponseWriter, r *http.Request) {
	key := strings.ToLower(r.PathValue("key"))
	if !hubdata.ValidKeyHMAC(key) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a key")
		return
	}
	action := r.PathValue("action")
	if !moderation.IsKeyAction(action) {
		writeError(w, http.StatusNotFound, codeNotFound, "keys can be banned, unbanned, trusted or untrusted")
		return
	}
	var req actionRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.Key(r.Context(), s.actor(r), key, action, moderation.KeyOptions{Reason: req.Reason, Create: true})
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
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
	if !s.readJSON(w, r, &req) {
		return
	}
	in := req.Limits.settings()
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	before, err := s.Store.Settings(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	a := s.actor(r)
	entry := store.AuditEntry{
		At: s.now().UTC(), Actor: a.Kind, ActorRef: a.Ref, ActorIP: a.IP, Action: "settings.save", TargetKind: store.TargetSettings,
		Before: limitsMap(limitsView(before)), After: limitsMap(limitsView(in)),
	}
	if err := s.Store.SaveSettings(ctx, in, entry); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsView(in))
}

func limitsMap(l LimitsView) map[string]interface{} {
	return map[string]interface{}{
		"shares_per_day":    l.SharesPerDay,
		"votes_per_day":     l.VotesPerDay,
		"reports_per_day":   l.ReportsPerDay,
		"mirrors_per_day":   l.MirrorsPerDay,
		"new_keys_per_day":  l.NewKeysPerDay,
		"requests_per_hour": l.RequestsPerHour,
	}
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
	view := CatalogueView{Mirrors: []string{}, Announced: []string{}, RevokedKeys: []string{}, SigningKey: s.KeyID, BuiltinKeys: hubwire.BuiltinHubKeys}
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
	base := s.Catalogue.HubBase()
	for _, m := range latest.Manifest.Mirrors {
		view.Mirrors = append(view.Mirrors, m)
		if m == base {
			view.HubListed = true
		} else {
			view.Announced = append(view.Announced, m)
		}
	}
	return view, nil
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	view := OverviewView{Version: s.Version, Source: s.Source, KeyID: s.KeyID, PublicURL: s.PublicURL, Now: s.now()}
	var err error
	if view.Catalogue, err = s.catalogueView(ctx); err != nil {
		s.fail(w, err)
		return
	}
	view.Build = *s.buildState(ctx)
	g, err := s.groups(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.Counts = CountsView{
		Pending:    len(g.pending),
		Listed:     len(g.listed),
		Withheld:   len(g.withheld),
		Superseded: len(g.superseded),
		Hidden:     len(g.hidden),
		Rejected:   len(g.rejected),
	}
	badges, err := s.Store.Badges(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.Counts.Keys = badges.Keys
	view.Counts.Banned = badges.Banned
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
	reportCounts, err := s.Store.ReportCounts(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view.Counts.ReportsOpen = reportCounts[store.ReportOpen]
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) buildState(ctx context.Context) *BuildStateView {
	view := &BuildStateView{State: catalogue.BuildIdle}
	view.Dirty, _ = s.Store.Dirty(ctx)
	b := s.builds()
	if b == nil {
		return view
	}
	st := b.Status()
	view.State = st.State
	view.Trigger = st.Trigger
	view.QueuedAt = optionalTime(st.QueuedAt)
	view.StartedAt = optionalTime(st.StartedAt)
	view.LastOK = buildRunView(st.LastOK)
	view.LastError = buildRunView(st.LastError)
	return view
}

func (s *Server) catalogueStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.buildState(r.Context()))
}

func (s *Server) catalogueBuilds(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.BuildQuery{Limit: 50}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "limit must be a positive number")
			return
		}
		f.Limit = min(n, 200)
	}
	if raw := q.Get("before"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "before must be a build id")
			return
		}
		f.Before = n
	}
	switch q.Get("only") {
	case "changes":
		f.OnlyChanges = true
	case "failed":
		f.OnlyFailed = true
	}
	runs, next, err := s.Store.BuildRuns(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := BuildsPageView{Items: make([]BuildRunView, 0, len(runs)), Next: next}
	for i := range runs {
		view.Items = append(view.Items, *buildRunView(&runs[i]))
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) catalogueBuild(w http.ResponseWriter, r *http.Request) {
	if s.Catalogue == nil {
		writeError(w, http.StatusServiceUnavailable, codeNotBuilt, "this hub has no catalogue builder")
		return
	}
	var req buildRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	_ = s.Moderation.RecordBuildRequest(ctx, s.actor(r))
	if req.Wait {
		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		result, err := s.Catalogue.BuildFor(buildCtx, catalogue.TriggerManual)
		if err != nil {
			writeError(w, http.StatusInternalServerError, codeBuildFailed, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ActionResult{
			Notice: "published " + result.Manifest.Catalogue.File,
			Code:   "catalogue.published",
			Params: map[string]interface{}{"file": result.Manifest.Catalogue.File, "sets": len(result.Catalogue.Sets)},
			Build:  s.buildState(ctx),
		})
		return
	}
	s.builds().RequestForced(catalogue.TriggerManual)
	writeJSON(w, http.StatusAccepted, ActionResult{Notice: "build queued", Code: "catalogue.build_queued", Build: s.buildState(ctx)})
}

func (s *Server) catalogueEpoch(w http.ResponseWriter, r *http.Request) {
	res, err := s.Moderation.NewEpoch(r.Context(), s.actor(r))
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
}

func (s *Server) catalogueRevoke(w http.ResponseWriter, r *http.Request) {
	var req revokeRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.Revoke(r.Context(), s.actor(r), req.KeyID, moderation.RevokeOptions{Confirm: req.Confirm, AllowBuiltin: req.AllowBuiltin})
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
}
