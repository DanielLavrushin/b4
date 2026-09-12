package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
)

func (s *Server) mountAdmin(mux *http.ServeMux) {
	guard := s.adminGuard
	mux.Handle("GET "+PathAdmin, guard(http.HandlerFunc(s.adminQueue)))
	mux.Handle("GET "+PathAdmin+"/{$}", guard(http.HandlerFunc(s.adminQueue)))
	mux.Handle("GET "+PathAdmin+"/keys", guard(http.HandlerFunc(s.adminKeys)))
	mux.Handle("POST "+PathAdmin+"/sets/{id}/{version}/{action}", guard(http.HandlerFunc(s.adminSetAction)))
	mux.Handle("POST "+PathAdmin+"/keys/{key}/{action}", guard(http.HandlerFunc(s.adminKeyAction)))
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
		if r.Method == http.MethodPost {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				s.message(w, http.StatusForbidden, true, "Refused", "Moderation actions are accepted only from this site.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type QueueEntry struct {
	Version     store.Version
	Author      string
	Targets     Targets
	Strategy    []string
	Emitted     []EmittedName
	Pins        []Pin
	DoHHost     string
	Projection  string
	Reports     []store.Report
	Independent int
	DecodeError string
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

type AdminPage struct {
	Base
	Pending  []QueueEntry
	Active   []QueueEntry
	Hidden   []QueueEntry
	Rejected []QueueEntry
	Notice   string
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
	active, err := s.Store.VersionsByStatus(ctx, hubwire.SetStatusActive)
	if err != nil {
		s.message(w, http.StatusInternalServerError, true, "Error", err.Error())
		return
	}
	page.Active = s.entries(ctx, active, 0)
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
	s.render(w, http.StatusOK, "admin", page)
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
