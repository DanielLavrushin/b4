package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	adminUser       = "admin"
	adminRealm      = `Basic realm="b4hub moderation"`
	maxReasonRunes  = 500
	recentDecisions = 50

	ActionApprove = "approve"
	ActionReject  = "reject"
	ActionHide    = "hide"
	ActionBan     = "ban"
	ActionUnban   = "unban"
	ActionRemove  = "remove"
)

func (s *Server) mountAdmin(mux *http.ServeMux) {
	guard := s.adminGuard
	mux.Handle("GET "+PathAdmin, guard(http.HandlerFunc(s.adminQueue)))
	mux.Handle("GET "+PathAdmin+"/{$}", guard(http.HandlerFunc(s.adminQueue)))
	mux.Handle("GET "+PathAdmin+"/keys", guard(http.HandlerFunc(s.adminKeys)))
	mux.Handle("POST "+PathAdmin+"/sets/{id}/{version}/{action}", guard(http.HandlerFunc(s.adminSetAction)))
	mux.Handle("POST "+PathAdmin+"/keys/{key}/{action}", guard(http.HandlerFunc(s.adminKeyAction)))
	mux.Handle("POST "+PathAdmin+"/mirrors/{id}/{action}", guard(http.HandlerFunc(s.adminMirrorAction)))
}

func (s *Server) adminGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AdminPassword == "" {
			s.message(w, http.StatusServiceUnavailable, false, "Moderation is not configured", "Set B4HUB_ADMIN_PASSWORD in the service environment to enable this page.")
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(adminUser)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(s.AdminPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", adminRealm)
			s.message(w, http.StatusUnauthorized, false, "Moderator credentials required", "Sign in as admin with the moderation password.")
			return
		}
		if r.Method == http.MethodPost && !sameSiteRequest(r) {
			s.message(w, http.StatusForbidden, true, "Refused", "Moderation actions are accepted only from forms on this site.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameSiteRequest(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	for _, header := range []string{"Origin", "Referer"} {
		raw := strings.TrimSpace(r.Header.Get(header))
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	}
	return false
}

type QueueEntry struct {
	Version      store.Version
	Author       string
	Targets      Targets
	Strategy     []string
	Emitted      []EmittedName
	Pins         []Pin
	DoHHost      string
	Projection   string
	Reports      []store.Report
	Independent  int
	DecodeError  string
	Versions     []string
	SupersededBy int
	SupersededAt time.Time
	Lineage      string
}

func (s *Server) queueEntry(ctx context.Context, v store.Version) QueueEntry {
	e := QueueEntry{Version: v, Author: hubdata.AuthorLabel(v.UploaderHMAC), Targets: TargetsOf(v.Projection)}
	if raw, err := json.MarshalIndent(v.Projection, "", "  "); err == nil {
		e.Projection = string(raw)
	}
	set, err := DecodeSet(v.Projection)
	if err != nil {
		e.DecodeError = err.Error()
	} else {
		e.Strategy = StrategyWords(&set, v.Payloads)
		e.Emitted = EmittedNames(&set, v.Payloads)
		e.Pins = PinsOf(&set)
		e.DoHHost = DoHHostOf(&set)
	}
	if reports, err := s.Store.ReportsForVersion(ctx, v.SetID, v.Version); err == nil && len(reports) > 0 {
		e.Reports = reports
		e.Independent, _ = s.Store.IndependentReports(ctx, v.SetID, v.Version)
	}
	return e
}

type MirrorEntry struct {
	store.Mirror
	KeyLabel string
	Health   string
}

func mirrorHealth(m store.Mirror) string {
	switch {
	case m.LastCheck.IsZero():
		return "not checked yet"
	case m.Healthy():
		return "ok at " + templateFuncs["when"].(func(time.Time) string)(m.LastCheck)
	}
	text := "failed at " + templateFuncs["when"].(func(time.Time) string)(m.LastCheck)
	if m.Reason != "" {
		text += ": " + m.Reason
	}
	if !m.LastOK.IsZero() {
		text += ", last ok " + templateFuncs["when"].(func(time.Time) string)(m.LastOK)
	}
	return text
}

type AdminPage struct {
	Base
	Pending    []QueueEntry
	Active     []QueueEntry
	Superseded []QueueEntry
	Hidden     []QueueEntry
	Rejected   []QueueEntry
	Mirrors    []MirrorEntry
	Notice     string
}

func lineage(v store.Version, newest map[string]store.Version) string {
	current, ok := newest[v.SetID]
	switch {
	case ok:
		return "new version of " + v.SetID + ", currently listed as /" + strconv.Itoa(current.Version) + "; approving replaces it in the catalogue"
	case v.Version > 1:
		return "new version of " + v.SetID + ", nothing currently listed; approving lists it"
	default:
		return "first version"
	}
}

func (s *Server) entries(ctx context.Context, versions []store.Version, limit int) []QueueEntry {
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	out := make([]QueueEntry, 0, len(versions))
	for _, v := range versions {
		out = append(out, s.queueEntry(ctx, v))
	}
	return out
}

func (s *Server) adminQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := AdminPage{Base: Base{Title: "Moderation queue", Admin: true}, Notice: r.URL.Query().Get("notice")}
	pending, err := s.Store.PendingVersions(ctx)
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	page.Pending = s.entries(ctx, pending, 0)
	listed, err := s.Store.ListedVersions(ctx)
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	active, err := s.Store.ActiveVersions(ctx)
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	newest := make(map[string]store.Version, len(listed))
	for _, v := range listed {
		newest[v.SetID] = v
	}
	versions := make(map[string][]string, len(listed))
	superseded := make([]store.Version, 0)
	for _, v := range active {
		versions[v.SetID] = append(versions[v.SetID], strconv.Itoa(v.Version))
		if v.Version < newest[v.SetID].Version {
			superseded = append(superseded, v)
		}
	}
	sort.SliceStable(listed, func(i, j int) bool { return listed[i].UpdatedAt.After(listed[j].UpdatedAt) })
	page.Active = s.entries(ctx, listed, 0)
	for i := range page.Active {
		page.Active[i].Versions = versions[page.Active[i].Version.SetID]
	}
	page.Superseded = s.entries(ctx, superseded, 0)
	for i := range page.Superseded {
		current := newest[page.Superseded[i].Version.SetID]
		page.Superseded[i].SupersededBy = current.Version
		page.Superseded[i].SupersededAt = current.UpdatedAt
	}
	for i := range page.Pending {
		page.Pending[i].Lineage = lineage(page.Pending[i].Version, newest)
	}
	hidden, err := s.Store.VersionsByStatus(ctx, hubwire.SetStatusHidden)
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	page.Hidden = s.entries(ctx, hidden, recentDecisions)
	rejected, err := s.Store.VersionsByStatus(ctx, hubwire.SetStatusRejected)
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	page.Rejected = s.entries(ctx, rejected, recentDecisions)
	mirrors, err := s.Store.Mirrors(ctx)
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	page.Mirrors = make([]MirrorEntry, 0, len(mirrors))
	for _, m := range mirrors {
		page.Mirrors = append(page.Mirrors, MirrorEntry{Mirror: m, KeyLabel: hubdata.AuthorLabel(m.KeyHMAC), Health: mirrorHealth(m)})
	}
	s.render(w, http.StatusOK, "admin", page)
}

func (s *Server) adminMirrorAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.message(w, http.StatusNotFound, true, "No such mirror", "The address does not name a mirror.")
		return
	}
	ctx := r.Context()
	m, err := s.Store.GetMirror(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		s.message(w, http.StatusNotFound, true, "No such mirror", "The address does not name a mirror.")
		return
	} else if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	var notice string
	switch r.PathValue("action") {
	case ActionApprove:
		err = s.Store.SetMirrorStatus(ctx, id, store.MirrorApproved, "", s.now())
		notice = "approved mirror " + m.URL
	case ActionReject:
		reason := reasonOf(r)
		if reason == "" {
			s.message(w, http.StatusBadRequest, true, "Reason required", "A rejection needs a reason.")
			return
		}
		err = s.Store.SetMirrorStatus(ctx, id, store.MirrorRejected, reason, s.now())
		notice = "rejected mirror " + m.URL
	case ActionRemove:
		err = s.Store.DeleteMirror(ctx, id)
		notice = "removed mirror " + m.URL
	default:
		s.message(w, http.StatusNotFound, true, "Unknown action", "Mirrors can be approved, rejected or removed.")
		return
	}
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	s.rebuild()
	http.Redirect(w, r, PathAdmin+"?notice="+url.QueryEscape(notice), http.StatusSeeOther)
}

func reasonOf(r *http.Request) string {
	reason := strings.TrimSpace(r.FormValue("reason"))
	runes := []rune(reason)
	if len(runes) > maxReasonRunes {
		reason = strings.TrimSpace(string(runes[:maxReasonRunes]))
	}
	return reason
}

func (s *Server) rebuild() {
	if s.Rebuild == nil {
		return
	}
	if err := s.Rebuild(); err != nil {
		log.Printf("web: catalogue rebuild after moderation: %v", err)
	}
}

func (s *Server) adminSetAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if !hubdata.ValidSetID(id) || err != nil || version <= 0 {
		s.message(w, http.StatusNotFound, true, "No such set version", "The address does not name a set version.")
		return
	}
	ctx := r.Context()
	if _, err := s.Store.GetVersion(ctx, id, version); errors.Is(err, store.ErrNotFound) {
		s.message(w, http.StatusNotFound, true, "No such set version", "The address does not name a set version.")
		return
	} else if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	now := s.now()
	action := r.PathValue("action")
	reason := reasonOf(r)
	var notice string
	switch action {
	case ActionApprove:
		err = s.Store.Approve(ctx, id, version, now)
		notice = "approved " + id + "/" + strconv.Itoa(version)
	case ActionReject:
		if reason == "" {
			s.message(w, http.StatusBadRequest, true, "Reason required", "A rejection needs a reason.")
			return
		}
		err = s.Store.Reject(ctx, id, version, reason, now)
		notice = "rejected " + id + "/" + strconv.Itoa(version)
	case ActionHide:
		if reason == "" {
			reason = "hidden by moderator"
		}
		err = s.Store.Hide(ctx, id, version, reason, now)
		notice = "hidden " + id + "/" + strconv.Itoa(version)
	default:
		s.message(w, http.StatusNotFound, true, "Unknown action", "Moderation knows approve, reject and hide.")
		return
	}
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	s.rebuild()
	http.Redirect(w, r, PathAdmin+"?notice="+url.QueryEscape(notice), http.StatusSeeOther)
}

type KeysPage struct {
	Base
	Keys   []store.KeySummary
	Notice string
}

func (s *Server) adminKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.Keys(r.Context())
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	s.render(w, http.StatusOK, "keys", KeysPage{Base: Base{Title: "Keys", Admin: true}, Keys: keys, Notice: r.URL.Query().Get("notice")})
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

func (s *Server) adminKeyAction(w http.ResponseWriter, r *http.Request) {
	key := strings.ToLower(r.PathValue("key"))
	if !validKeyHMAC(key) {
		s.message(w, http.StatusNotFound, true, "No such key", "The address does not name a key.")
		return
	}
	ctx := r.Context()
	var err error
	var notice string
	switch r.PathValue("action") {
	case ActionBan:
		reason := reasonOf(r)
		if reason == "" {
			reason = "banned by moderator"
		}
		err = s.Store.BanKey(ctx, key, reason, s.now())
		notice = "banned " + hubdata.AuthorLabel(key)
	case ActionUnban:
		err = s.Store.UnbanKey(ctx, key)
		notice = "unbanned " + hubdata.AuthorLabel(key)
	default:
		s.message(w, http.StatusNotFound, true, "Unknown action", "Keys can be banned or unbanned.")
		return
	}
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	s.rebuild()
	http.Redirect(w, r, PathAdmin+"/keys?notice="+url.QueryEscape(notice), http.StatusSeeOther)
}
