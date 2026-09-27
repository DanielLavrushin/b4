package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"

	IssueNotPublished    = "not_published"
	IssueBuildFailed     = "build_failed"
	IssueBuildOverdue    = "build_overdue"
	IssueManifestExpires = "manifest_expiring"
	IssueQueueOld        = "queue_old"
	IssueMirrorFailing   = "mirror_failing"
	IssueMirrorStale     = "mirror_stale"
	IssueMirrorPending   = "mirror_pending"
	IssueReportsOpen     = "reports_open"
	IssueDevBuild        = "dev_build"
	IssueDirtySource     = "dirty_source"

	expiryWarn  = 7 * 24 * time.Hour
	expiryError = 2 * 24 * time.Hour
	queueOld    = 72 * time.Hour
)

var issueCodes = []string{IssueNotPublished, IssueBuildFailed, IssueBuildOverdue, IssueManifestExpires, IssueQueueOld, IssueMirrorFailing, IssueMirrorStale, IssueMirrorPending, IssueReportsOpen, IssueDevBuild, IssueDirtySource}

var severities = []string{SeverityError, SeverityWarning, SeverityInfo}

type IssueView struct {
	Code     string                 `json:"code"`
	Severity string                 `json:"severity"`
	Params   map[string]interface{} `json:"params,omitempty"`
}

type HealthManifestView struct {
	Published   bool   `json:"published"`
	Epoch       int64  `json:"epoch,omitempty"`
	Seq         int64  `json:"seq,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	ExpiresInS  int64  `json:"expires_in_s,omitempty"`
}

type HealthPendingView struct {
	Count  int         `json:"count"`
	Oldest *SetRefView `json:"oldest,omitempty"`
	Since  *time.Time  `json:"since,omitempty"`
}

type HealthMirrorsView struct {
	Approved  int `json:"approved"`
	Announced int `json:"announced"`
	Failing   int `json:"failing"`
	Stale     int `json:"stale"`
	Pending   int `json:"pending"`
}

type BuildInfoView struct {
	Version     string `json:"version"`
	Source      string `json:"source,omitempty"`
	Dev         bool   `json:"dev"`
	DirtySource bool   `json:"dirty_source"`
}

type HealthView struct {
	Now         time.Time          `json:"now"`
	Worst       string             `json:"worst"`
	Issues      []IssueView        `json:"issues"`
	Manifest    HealthManifestView `json:"manifest"`
	Build       BuildStateView     `json:"build"`
	Pending     HealthPendingView  `json:"pending"`
	ReportsOpen int                `json:"reports_open"`
	Mirrors     HealthMirrorsView  `json:"mirrors"`
	BuildInfo   BuildInfoView      `json:"build_info"`
}

func (s *Server) healthView(ctx context.Context) (HealthView, error) {
	now := s.now().UTC()
	v := HealthView{Now: now, Issues: []IssueView{}, Worst: ""}
	add := func(code, severity string, params map[string]interface{}) {
		v.Issues = append(v.Issues, IssueView{Code: code, Severity: severity, Params: params})
	}
	v.Build = *s.buildState(ctx)
	latest := s.latest()
	if latest == nil {
		add(IssueNotPublished, SeverityError, nil)
	} else {
		m := latest.Manifest
		v.Manifest = HealthManifestView{Published: true, Epoch: m.Epoch, Seq: m.Seq, GeneratedAt: m.GeneratedAt, ExpiresAt: m.ExpiresAt}
		if expires, err := time.Parse(time.RFC3339, m.ExpiresAt); err == nil {
			left := expires.Sub(now)
			v.Manifest.ExpiresInS = int64(left / time.Second)
			switch {
			case left < expiryError:
				add(IssueManifestExpires, SeverityError, map[string]interface{}{"days": int(left.Hours() / 24)})
			case left < expiryWarn:
				add(IssueManifestExpires, SeverityWarning, map[string]interface{}{"days": int(left.Hours() / 24)})
			}
		}
	}
	if v.Build.LastError != nil {
		add(IssueBuildFailed, SeverityError, map[string]interface{}{"message": v.Build.LastError.Error})
	}
	builtAt, err := s.Store.BuiltAt(ctx)
	if err != nil {
		return v, err
	}
	if !builtAt.IsZero() && now.Sub(builtAt) > catalogue.DefaultMaxAge+2*catalogue.DefaultInterval {
		add(IssueBuildOverdue, SeverityWarning, map[string]interface{}{"hours": int(now.Sub(builtAt).Hours())})
	}
	badges, err := s.Store.Badges(ctx)
	if err != nil {
		return v, err
	}
	v.Pending.Count = badges.Pending
	v.ReportsOpen = badges.ReportsOpen
	if badges.Pending > 0 {
		pending, err := s.Store.PendingVersions(ctx)
		if err != nil {
			return v, err
		}
		if len(pending) > 0 {
			oldest := pending[0]
			v.Pending.Oldest = &SetRefView{SetID: oldest.SetID, Version: oldest.Version, Title: oldest.Title}
			v.Pending.Since = optionalTime(oldest.CreatedAt)
			if age := now.Sub(oldest.CreatedAt); age > queueOld {
				add(IssueQueueOld, SeverityWarning, map[string]interface{}{"hours": int(age.Hours()), "count": badges.Pending})
			}
		}
	}
	if badges.ReportsOpen > 0 {
		add(IssueReportsOpen, SeverityInfo, map[string]interface{}{"count": badges.ReportsOpen})
	}
	mirrors, err := s.Store.Mirrors(ctx)
	if err != nil {
		return v, err
	}
	for _, mv := range s.mirrorViews(ctx, mirrors) {
		switch mv.Status {
		case store.MirrorPending:
			v.Mirrors.Pending++
		case store.MirrorApproved:
			v.Mirrors.Approved++
			if mv.Announced {
				v.Mirrors.Announced++
			}
			if mv.LastCheck != nil && !mv.Healthy {
				v.Mirrors.Failing++
			}
			if mv.Lag == LagStale {
				v.Mirrors.Stale++
			}
		}
	}
	if v.Mirrors.Failing > 0 {
		add(IssueMirrorFailing, SeverityWarning, map[string]interface{}{"count": v.Mirrors.Failing})
	}
	if v.Mirrors.Stale > 0 {
		add(IssueMirrorStale, SeverityWarning, map[string]interface{}{"count": v.Mirrors.Stale})
	}
	if v.Mirrors.Pending > 0 {
		add(IssueMirrorPending, SeverityInfo, map[string]interface{}{"count": v.Mirrors.Pending})
	}
	v.BuildInfo = BuildInfoView{Version: s.Version, Source: s.Source, Dev: s.Version == "" || s.Version == "dev", DirtySource: strings.HasSuffix(s.Source, "-dirty")}
	if v.BuildInfo.Dev {
		add(IssueDevBuild, SeverityInfo, map[string]interface{}{"version": s.Version})
	}
	if v.BuildInfo.DirtySource {
		add(IssueDirtySource, SeverityInfo, map[string]interface{}{"source": s.Source})
	}
	for _, sev := range severities {
		for _, issue := range v.Issues {
			if issue.Severity == sev && v.Worst == "" {
				v.Worst = sev
			}
		}
	}
	if v.Worst == "" {
		v.Worst = "ok"
	}
	return v, nil
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	v, err := s.healthView(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
