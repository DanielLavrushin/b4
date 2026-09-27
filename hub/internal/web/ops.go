package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	LagUnknown  = "unknown"
	LagCurrent  = "current"
	LagBehind   = "behind"
	LagStale    = "stale"
	LagAhead    = "ahead"
	lagPatience = 15 * time.Minute

	mirrorCheckTimeout = 15 * time.Second
)

type MirrorsManifestView struct {
	Published   bool     `json:"published"`
	Epoch       int64    `json:"epoch"`
	Seq         int64    `json:"seq"`
	GeneratedAt string   `json:"generated_at,omitempty"`
	HubURL      string   `json:"hub_url,omitempty"`
	Listed      []string `json:"listed"`
}

type MirrorsView struct {
	Manifest      MirrorsManifestView `json:"manifest"`
	CheckInterval int64               `json:"check_interval_s"`
	Window        int64               `json:"window_s"`
	Mirrors       []MirrorView        `json:"mirrors"`
	Orphans       []string            `json:"orphans"`
}

type MirrorCheckResult struct {
	ActionResult
	Mirror MirrorView `json:"mirror"`
}

func mirrorLag(m store.Mirror, ours *hubwire.Manifest) (string, int64) {
	if m.ServedEpoch == 0 || ours == nil {
		return LagUnknown, 0
	}
	switch {
	case m.ServedEpoch > ours.Epoch:
		return LagAhead, 0
	case m.ServedEpoch < ours.Epoch:
		return LagStale, 0
	case m.ServedSeq == ours.Seq:
		return LagCurrent, 0
	case m.ServedSeq > ours.Seq:
		return LagAhead, 0
	}
	behind := ours.Seq - m.ServedSeq
	generated, err := time.Parse(time.RFC3339, ours.GeneratedAt)
	if err == nil && m.LastCheck.Sub(generated) > lagPatience {
		return LagStale, behind
	}
	return LagBehind, behind
}

func (s *Server) health() *catalogue.MirrorHealth {
	if s.Catalogue == nil {
		return nil
	}
	return s.Catalogue.Mirrors
}

func (s *Server) mirrorViews(ctx context.Context, mirrors []store.Mirror) []MirrorView {
	var latest *catalogue.Result
	if s.Catalogue != nil {
		latest = s.Catalogue.Latest()
	}
	listed := map[string]bool{}
	var manifest *hubwire.Manifest
	if latest != nil {
		manifest = latest.Manifest
		for _, u := range manifest.Mirrors {
			listed[u] = true
		}
	}
	window := catalogue.DefaultMirrorWindow
	if h := s.health(); h != nil {
		window = h.AnnounceWindow()
	}
	now := s.now().UTC()
	out := make([]MirrorView, 0, len(mirrors))
	for _, m := range mirrors {
		v := mirrorView(m)
		v.Announced = listed[m.URL]
		if m.Status == store.MirrorApproved && !m.LastOK.IsZero() {
			v.AnnounceNext = now.Sub(m.LastOK) <= window
			drops := m.LastOK.Add(window)
			v.DropsAt = &drops
		}
		v.Lag, v.BehindBy = mirrorLag(m, manifest)
		out = append(out, v)
	}
	return out
}

func (s *Server) mirrors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mirrors, err := s.Store.Mirrors(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := MirrorsView{
		CheckInterval: int64(catalogue.DefaultMirrorCheckInterval / time.Second),
		Window:        int64(catalogue.DefaultMirrorWindow / time.Second),
		Mirrors:       s.mirrorViews(ctx, mirrors),
		Orphans:       []string{},
		Manifest:      MirrorsManifestView{Listed: []string{}},
	}
	if h := s.health(); h != nil {
		view.Window = int64(h.AnnounceWindow() / time.Second)
	}
	if s.Catalogue != nil {
		view.Manifest.HubURL = s.Catalogue.HubBase()
		if latest := s.Catalogue.Latest(); latest != nil {
			view.Manifest.Published = true
			view.Manifest.Epoch = latest.Manifest.Epoch
			view.Manifest.Seq = latest.Manifest.Seq
			view.Manifest.GeneratedAt = latest.Manifest.GeneratedAt
			known := map[string]bool{}
			for _, m := range mirrors {
				known[m.URL] = true
			}
			for _, u := range latest.Manifest.Mirrors {
				view.Manifest.Listed = append(view.Manifest.Listed, u)
				if u != view.Manifest.HubURL && !known[u] {
					view.Orphans = append(view.Orphans, u)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) requestIfDrifted(ctx context.Context) bool {
	if s.Catalogue == nil {
		return false
	}
	drift, err := s.Catalogue.MirrorsDrift(ctx)
	if err != nil || !drift {
		return false
	}
	if err := s.Store.MarkDirty(ctx); err != nil {
		return false
	}
	s.builds().Request(catalogue.TriggerMirrors)
	return true
}

func (s *Server) mirrorAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a mirror")
		return
	}
	action := r.PathValue("action")
	if !moderation.IsMirrorAction(action) {
		writeError(w, http.StatusNotFound, codeNotFound, "mirrors can be approved, rejected or removed")
		return
	}
	var req actionRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	res, m, err := s.Moderation.Mirror(ctx, s.actor(r), id, action, req.Reason)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	if action == ActionApprove && s.health() != nil {
		approved := *m
		approved.Status = store.MirrorApproved
		go func() {
			checkCtx, cancel := context.WithTimeout(context.Background(), mirrorCheckTimeout)
			defer cancel()
			s.health().CheckOne(checkCtx, approved)
			s.requestIfDrifted(checkCtx)
		}()
	}
	writeJSON(w, http.StatusOK, s.result(ctx, res))
}

func (s *Server) mirrorCheck(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a mirror")
		return
	}
	h := s.health()
	if h == nil {
		writeError(w, http.StatusServiceUnavailable, codeNotBuilt, "this hub has no mirror checker")
		return
	}
	ctx := r.Context()
	m, err := s.Store.GetMirror(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mirrorCheckTimeout)
	defer cancel()
	c := h.CheckOne(checkCtx, *m)
	s.requestIfDrifted(checkCtx)
	fresh, err := s.Store.GetMirror(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	a := s.actor(r)
	s.recordAudit(r, store.AuditEntry{At: s.now().UTC(), Actor: a.Kind, ActorRef: a.Ref, ActorIP: a.IP, Action: "mirror.check", TargetKind: store.TargetMirror,
		TargetID: strconv.FormatInt(id, 10), After: map[string]interface{}{"ok": c.OK, "code": c.Code}})
	res := ActionResult{Notice: "checked " + m.URL, Code: "mirror.checked", Params: map[string]interface{}{"url": m.URL}, Build: s.buildState(ctx)}
	if !c.OK {
		res.Code = "mirror.check_failed"
		res.Params["stage"] = c.Code
	}
	writeJSON(w, http.StatusOK, MirrorCheckResult{ActionResult: res, Mirror: s.mirrorViews(ctx, []store.Mirror{*fresh})[0]})
}

func (s *Server) mirrorsCheck(w http.ResponseWriter, r *http.Request) {
	h := s.health()
	if h == nil {
		writeError(w, http.StatusServiceUnavailable, codeNotBuilt, "this hub has no mirror checker")
		return
	}
	ctx := r.Context()
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mirrorCheckTimeout)
	defer cancel()
	results, err := h.CheckAll(checkCtx)
	if err != nil {
		s.fail(w, err)
		return
	}
	failed := 0
	for _, c := range results {
		if !c.OK {
			failed++
		}
	}
	s.requestIfDrifted(checkCtx)
	writeJSON(w, http.StatusOK, ActionResult{Notice: "checked mirrors", Code: "mirrors.checked", Params: map[string]interface{}{"checked": len(results), "failed": failed}, Build: s.buildState(ctx)})
}

func parseStamp(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	return t, err == nil
}

func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ReportFilter{State: q.Get("state"), SetID: q.Get("set"), KeyHMAC: strings.ToLower(q.Get("key")), ASN: strings.TrimPrefix(strings.ToUpper(q.Get("asn")), "AS"), Before: q.Get("before"), Limit: 100}
	switch f.State {
	case "":
		f.State = store.ReportOpen
	case "all":
		f.State = ""
	default:
		if !store.ValidReportState(f.State) {
			writeError(w, http.StatusBadRequest, codeBadRequest, "state must be open, dismissed, resolved or all")
			return
		}
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
	items, total, next, err := s.Store.QueryReports(ctx, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	heads, err := s.Store.VersionHeads(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	counts, err := s.Store.ReportCounts(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := ReportsPageView{Items: make([]ReportView, 0, len(items)), Total: total, Next: next, Counts: counts}
	for _, rep := range items {
		rv := reportView(rep)
		if h, ok := heads[rep.SetID][rep.Version]; ok {
			rv.Title = h.Title
			rv.SetStatus = h.Status
		}
		view.Items = append(view.Items, rv)
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) reportAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a report")
		return
	}
	var req noteRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.Reports(r.Context(), s.actor(r), []int64{id}, r.PathValue("action"), req.Note)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: res.Notice, Code: res.Code, Params: res.Params})
}

func (s *Server) reportsBulk(w http.ResponseWriter, r *http.Request) {
	var req ReportsActionRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	res, err := s.Moderation.Reports(r.Context(), s.actor(r), req.IDs, req.Action, req.Note)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: res.Notice, Code: res.Code, Params: res.Params})
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AuditQuery{Limit: 50, Action: q.Get("action"), Actor: q.Get("actor"), TargetKind: q.Get("target_kind"), TargetID: q.Get("target_id"), BatchID: q.Get("batch")}
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
			writeError(w, http.StatusBadRequest, codeBadRequest, "before must be an entry id")
			return
		}
		f.Before = n
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
	entries, next, err := s.Store.AuditLog(ctx, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	heads, err := s.Store.VersionHeads(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := AuditPageView{Items: make([]AuditEntryView, 0, len(entries)), Next: next}
	for _, e := range entries {
		v := auditView(e)
		if e.TargetKind == store.TargetSet {
			if h, ok := heads[e.TargetID][e.Version]; ok && e.Version > 0 {
				v.TargetLabel = h.Title
			} else if _, h := store.LatestHead(heads[e.TargetID]); h.Title != "" {
				v.TargetLabel = h.Title
			} else if title, ok := e.Before["title"].(string); ok {
				v.TargetLabel = title
			}
		}
		if e.TargetKind == store.TargetKey && !hubdata.ValidKeyHMAC(e.TargetID) {
			v.TargetLabel = ""
		}
		view.Items = append(view.Items, v)
	}
	writeJSON(w, http.StatusOK, view)
}
