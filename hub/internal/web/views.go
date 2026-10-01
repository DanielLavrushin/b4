package web

import (
	"context"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	LineageFirst    = "first"
	LineageReplaces = "replaces"
	LineageOlder    = "older"
	LineageRelists  = "relists"
)

var lineageKinds = []string{LineageFirst, LineageReplaces, LineageOlder, LineageRelists}

type TargetsView struct {
	Domains []string `json:"domains"`
	IPs     []string `json:"ips"`
	GeoSite []string `json:"geosite"`
	GeoIP   []string `json:"geoip"`
	ASNs    []string `json:"asns"`
	Filters []Term   `json:"filters"`
}

type EmittedView struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	Unreadable bool   `json:"unreadable,omitempty"`
}

type PinView struct {
	Domain    string   `json:"domain"`
	Addresses []string `json:"addresses"`
}

type ReportView struct {
	ID          int64      `json:"id"`
	SetID       string     `json:"set_id"`
	Version     int        `json:"version"`
	Title       string     `json:"title,omitempty"`
	SetStatus   string     `json:"set_status,omitempty"`
	Key         string     `json:"key"`
	KeyHMAC     string     `json:"key_hmac"`
	KeyBanned   bool       `json:"key_banned,omitempty"`
	KeyTest     bool       `json:"key_test,omitempty"`
	ASNObserved string     `json:"asn_observed,omitempty"`
	Reason      string     `json:"reason"`
	ReceivedAt  time.Time  `json:"received_at"`
	State       string     `json:"state"`
	Resolution  string     `json:"resolution,omitempty"`
	Note        string     `json:"note,omitempty"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	Counts      bool       `json:"counts"`
}

type VoteView struct {
	ID              int64     `json:"id"`
	SetID           string    `json:"set_id"`
	Version         int       `json:"version"`
	FP              string    `json:"fp"`
	Key             string    `json:"key"`
	KeyHMAC         string    `json:"key_hmac"`
	Kind            string    `json:"kind"`
	Weight          float64   `json:"weight"`
	ASNObserved     string    `json:"asn_observed,omitempty"`
	CountryObserved string    `json:"country_observed,omitempty"`
	ASNHint         string    `json:"asn_hint,omitempty"`
	CountryHint     string    `json:"country_hint,omitempty"`
	OriginVerified  bool      `json:"origin_verified"`
	Domain          string    `json:"domain,omitempty"`
	B4Version       string    `json:"b4_version,omitempty"`
	Engine          string    `json:"engine,omitempty"`
	ReceivedAt      time.Time `json:"received_at"`
}

type VotesView struct {
	Works  int `json:"works"`
	Broken int `json:"broken"`
}

type LineageView struct {
	Kind           string `json:"kind"`
	CurrentVersion int    `json:"current_version,omitempty"`
}

type EntryView struct {
	SetID               string                 `json:"set_id"`
	Version             int                    `json:"version"`
	Title               string                 `json:"title"`
	Description         string                 `json:"description,omitempty"`
	Family              string                 `json:"family,omitempty"`
	Engine              string                 `json:"engine,omitempty"`
	B4Version           string                 `json:"b4_version,omitempty"`
	B4Min               string                 `json:"b4_min,omitempty"`
	FP                  string                 `json:"fp"`
	Status              string                 `json:"status"`
	StatusReason        string                 `json:"status_reason,omitempty"`
	Flags               []string               `json:"flags"`
	Author              string                 `json:"author"`
	UploaderHMAC        string                 `json:"uploader_hmac"`
	ASNObserved         string                 `json:"asn_observed,omitempty"`
	CountryObserved     string                 `json:"country_observed,omitempty"`
	ASNHint             string                 `json:"asn_hint,omitempty"`
	CountryHint         string                 `json:"country_hint,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	Targets             TargetsView            `json:"targets"`
	Strategy            []Term                 `json:"strategy"`
	Emitted             []EmittedView          `json:"emitted"`
	Pins                []PinView              `json:"pins"`
	DoHHost             string                 `json:"doh_host,omitempty"`
	Payloads            []hubwire.BlobRef      `json:"payloads"`
	Projection          map[string]interface{} `json:"projection"`
	DecodeError         string                 `json:"decode_error,omitempty"`
	Reports             []ReportView           `json:"reports"`
	Independent         int                    `json:"independent_reports"`
	OpenReports         int                    `json:"open_reports"`
	AuthorBanned        bool                   `json:"author_banned,omitempty"`
	Withheld            string                 `json:"withheld,omitempty"`
	HiddenFrom          string                 `json:"hidden_from,omitempty"`
	Votes               VotesView              `json:"votes"`
	Versions            []int                  `json:"versions,omitempty"`
	SupersededBy        int                    `json:"superseded_by,omitempty"`
	SupersededAt        *time.Time             `json:"superseded_at,omitempty"`
	Lineage             *LineageView           `json:"lineage,omitempty"`
	EditedAt            *time.Time             `json:"edited_at,omitempty"`
	EditNote            string                 `json:"edit_note,omitempty"`
	OriginalProjection  map[string]interface{} `json:"original_projection,omitempty"`
	OriginalTitle       string                 `json:"original_title,omitempty"`
	OriginalDescription string                 `json:"original_description,omitempty"`
}

type EditRequest struct {
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Projection  map[string]interface{} `json:"projection"`
	Note        string                 `json:"note"`
	Approve     bool                   `json:"approve"`
	Expect      *time.Time             `json:"expect_updated_at,omitempty"`
}

type DuplicateView struct {
	SetID   string `json:"set_id"`
	Version int    `json:"version"`
	Title   string `json:"title"`
	Status  string `json:"status"`
}

type EditPreview struct {
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Projection  map[string]interface{} `json:"projection"`
	Payloads    []hubwire.BlobRef      `json:"payloads"`
	Warnings    []hubwire.Warning      `json:"warnings"`
	Stripped    []hubwire.Stripped     `json:"stripped"`
	FP          string                 `json:"fp"`
	FPChanged   bool                   `json:"fp_changed"`
	Changed     bool                   `json:"changed"`
	Targets     TargetsView            `json:"targets"`
	Strategy    []Term                 `json:"strategy"`
	Flags       []string               `json:"flags"`
	Family      string                 `json:"family"`
	B4Min       string                 `json:"b4_min"`
	Duplicate   *DuplicateView         `json:"duplicate,omitempty"`
	Tidy        *Tidy                  `json:"tidy,omitempty"`
}

type SetsView struct {
	Pending    []EntryView `json:"pending"`
	Listed     []EntryView `json:"listed"`
	Superseded []EntryView `json:"superseded"`
	Withheld   []EntryView `json:"withheld"`
	Hidden     []EntryView `json:"hidden"`
	Rejected   []EntryView `json:"rejected"`
}

type SetDetailView struct {
	ID                 string      `json:"id"`
	Author             string      `json:"author"`
	AuthorHMAC         string      `json:"author_hmac"`
	CurrentVersion     int         `json:"current_version"`
	DerivedFromID      string      `json:"derived_from_id,omitempty"`
	DerivedFromVersion int         `json:"derived_from_version,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
	WithdrawnAt        *time.Time  `json:"withdrawn_at,omitempty"`
	WithdrawReason     string      `json:"withdraw_reason,omitempty"`
	Withheld           string      `json:"withheld,omitempty"`
	AuthorBanned       bool        `json:"author_banned,omitempty"`
	ListedVersion      int         `json:"listed_version,omitempty"`
	Versions           []EntryView `json:"versions"`
	Votes              []VoteView  `json:"votes"`
}

type LimitsView struct {
	SharesPerDay    int `json:"shares_per_day"`
	VotesPerDay     int `json:"votes_per_day"`
	ReportsPerDay   int `json:"reports_per_day"`
	MirrorsPerDay   int `json:"mirrors_per_day"`
	NewKeysPerDay   int `json:"new_keys_per_day"`
	RequestsPerHour int `json:"requests_per_hour"`
}

type SettingsView struct {
	Limits   LimitsView `json:"limits"`
	Defaults LimitsView `json:"defaults"`
}

func limitsView(s store.Settings) LimitsView {
	return LimitsView{
		SharesPerDay:    s.SharesPerDay,
		VotesPerDay:     s.VotesPerDay,
		ReportsPerDay:   s.ReportsPerDay,
		MirrorsPerDay:   s.MirrorsPerDay,
		NewKeysPerDay:   s.NewKeysPerDay,
		RequestsPerHour: s.RequestsPerHour,
	}
}

func (v LimitsView) settings() store.Settings {
	return store.Settings{
		SharesPerDay:    v.SharesPerDay,
		VotesPerDay:     v.VotesPerDay,
		ReportsPerDay:   v.ReportsPerDay,
		MirrorsPerDay:   v.MirrorsPerDay,
		NewKeysPerDay:   v.NewKeysPerDay,
		RequestsPerHour: v.RequestsPerHour,
	}
}

func settingsView(s store.Settings) SettingsView {
	return SettingsView{Limits: limitsView(s), Defaults: limitsView(store.DefaultSettings())}
}

type MirrorView struct {
	ID                int64      `json:"id"`
	URL               string     `json:"url"`
	KeyHMAC           string     `json:"key_hmac"`
	Key               string     `json:"key"`
	Status            string     `json:"status"`
	Healthy           bool       `json:"healthy"`
	FirstSeen         time.Time  `json:"first_seen"`
	LastSeen          time.Time  `json:"last_seen"`
	LastCheck         *time.Time `json:"last_check,omitempty"`
	LastOK            *time.Time `json:"last_ok,omitempty"`
	Reason            string     `json:"reason,omitempty"`
	Version           string     `json:"version,omitempty"`
	CheckCode         string     `json:"check_code,omitempty"`
	CheckError        string     `json:"check_error,omitempty"`
	CheckMillis       int64      `json:"check_ms,omitempty"`
	ServedEpoch       int64      `json:"served_epoch,omitempty"`
	ServedSeq         int64      `json:"served_seq,omitempty"`
	ServedGeneratedAt string     `json:"served_generated_at,omitempty"`
	Announced         bool       `json:"announced"`
	AnnounceNext      bool       `json:"announce_next"`
	Kept              bool       `json:"kept,omitempty"`
	DropsAt           *time.Time `json:"drops_at,omitempty"`
	Lag               string     `json:"lag"`
	BehindBy          int64      `json:"behind_by,omitempty"`
}

type FeedbackView struct {
	Votes   []VoteView   `json:"votes"`
	Reports []ReportView `json:"reports"`
}

type CatalogueView struct {
	Published   bool       `json:"published"`
	File        string     `json:"file,omitempty"`
	Size        int64      `json:"size,omitempty"`
	Epoch       int64      `json:"epoch"`
	Seq         int64      `json:"seq"`
	GeneratedAt string     `json:"generated_at,omitempty"`
	ExpiresAt   string     `json:"expires_at,omitempty"`
	Sets        int        `json:"sets"`
	Blobs       int        `json:"blobs"`
	Mirrors     []string   `json:"mirrors"`
	Announced   []string   `json:"announced_mirrors"`
	HubListed   bool       `json:"hub_listed"`
	RevokedKeys []string   `json:"revoked_keys"`
	SigningKey  string     `json:"signing_key"`
	BuiltinKeys []string   `json:"builtin_keys"`
	BuiltAt     *time.Time `json:"built_at,omitempty"`
	Dirty       bool       `json:"dirty"`
}

type CountsView struct {
	Pending         int `json:"pending"`
	Listed          int `json:"listed"`
	Superseded      int `json:"superseded"`
	Withheld        int `json:"withheld"`
	Hidden          int `json:"hidden"`
	Rejected        int `json:"rejected"`
	ReportsOpen     int `json:"reports_open"`
	Keys            int `json:"keys"`
	Banned          int `json:"banned"`
	MirrorsPending  int `json:"mirrors_pending"`
	MirrorsApproved int `json:"mirrors_approved"`
	MirrorsRejected int `json:"mirrors_rejected"`
	Votes           int `json:"votes"`
	Reports         int `json:"reports"`
}

type OverviewView struct {
	Version   string         `json:"version"`
	Source    string         `json:"source,omitempty"`
	KeyID     string         `json:"key_id"`
	PublicURL string         `json:"public_url,omitempty"`
	Now       time.Time      `json:"now"`
	Catalogue CatalogueView  `json:"catalogue"`
	Build     BuildStateView `json:"build"`
	Counts    CountsView     `json:"counts"`
}

type ActionResult struct {
	Notice string                 `json:"notice"`
	Code   string                 `json:"code"`
	Params map[string]interface{} `json:"params,omitempty"`
	Build  *BuildStateView        `json:"build,omitempty"`
}

type BuildRunView struct {
	ID         int64              `json:"id"`
	Trigger    string             `json:"trigger"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
	OK         bool               `json:"ok"`
	Error      string             `json:"error,omitempty"`
	Epoch      int64              `json:"epoch,omitempty"`
	Seq        int64              `json:"seq,omitempty"`
	File       string             `json:"file,omitempty"`
	Size       int64              `json:"size,omitempty"`
	Sets       int                `json:"sets"`
	Blobs      int                `json:"blobs"`
	Mirrors    int                `json:"mirrors"`
	DurationMs int64              `json:"duration_ms"`
	Changes    store.BuildChanges `json:"changes"`
	Changed    bool               `json:"content_changed"`
}

type BuildStateView struct {
	State     string        `json:"state"`
	Trigger   string        `json:"trigger,omitempty"`
	QueuedAt  *time.Time    `json:"queued_at,omitempty"`
	StartedAt *time.Time    `json:"started_at,omitempty"`
	Dirty     bool          `json:"dirty"`
	LastOK    *BuildRunView `json:"last_ok,omitempty"`
	LastError *BuildRunView `json:"last_error,omitempty"`
}

type BuildsPageView struct {
	Items []BuildRunView `json:"items"`
	Next  int64          `json:"next,omitempty"`
}

type ModerationItemView struct {
	SetID       string                 `json:"set_id"`
	Version     int                    `json:"version"`
	Title       string                 `json:"title,omitempty"`
	From        string                 `json:"from,omitempty"`
	To          string                 `json:"to,omitempty"`
	Listed      int                    `json:"listed"`
	ListedAfter int                    `json:"listed_after"`
	Withheld    string                 `json:"withheld,omitempty"`
	Reports     int                    `json:"reports"`
	OK          bool                   `json:"ok"`
	Code        string                 `json:"code,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

type ModerationView struct {
	Notice  string                 `json:"notice"`
	Code    string                 `json:"code"`
	Params  map[string]interface{} `json:"params,omitempty"`
	BatchID string                 `json:"batch_id,omitempty"`
	Items   []ModerationItemView   `json:"items"`
	Build   *BuildStateView        `json:"build,omitempty"`
}

type ModerationItemRequest struct {
	SetID        string `json:"set_id"`
	Version      int    `json:"version"`
	ExpectStatus string `json:"expect_status,omitempty"`
}

type ModerationRequest struct {
	Action      string                  `json:"action"`
	Reason      string                  `json:"reason"`
	Items       []ModerationItemRequest `json:"items"`
	Force       bool                    `json:"force"`
	Withdraw    bool                    `json:"withdraw"`
	KeepReports bool                    `json:"keep_reports"`
	Partial     bool                    `json:"partial"`
	DryRun      bool                    `json:"dry_run"`
}

type SetRefView struct {
	SetID   string `json:"set_id"`
	Version int    `json:"version"`
	Title   string `json:"title"`
}

type KeyImpactView struct {
	Listed    []SetRefView `json:"listed"`
	Pending   []SetRefView `json:"pending"`
	Votes     int          `json:"votes"`
	VotedSets int          `json:"voted_sets"`
	Reports   int          `json:"reports"`
	Mirrors   []MirrorView `json:"mirrors"`
}

type ReportsPageView struct {
	Items  []ReportView   `json:"items"`
	Total  int            `json:"total"`
	Next   string         `json:"next,omitempty"`
	Counts map[string]int `json:"counts"`
}

type ReportsActionRequest struct {
	IDs    []int64 `json:"ids"`
	Action string  `json:"action"`
	Note   string  `json:"note"`
}

type AuditEntryView struct {
	ID          int64                  `json:"id"`
	At          time.Time              `json:"at"`
	Actor       string                 `json:"actor"`
	ActorRef    string                 `json:"actor_ref,omitempty"`
	ActorIP     string                 `json:"actor_ip,omitempty"`
	Action      string                 `json:"action"`
	TargetKind  string                 `json:"target_kind"`
	TargetID    string                 `json:"target_id,omitempty"`
	TargetLabel string                 `json:"target_label,omitempty"`
	Version     int                    `json:"version,omitempty"`
	Reason      string                 `json:"reason,omitempty"`
	Before      map[string]interface{} `json:"before,omitempty"`
	After       map[string]interface{} `json:"after,omitempty"`
	BatchID     string                 `json:"batch_id,omitempty"`
}

type AuditPageView struct {
	Items []AuditEntryView `json:"items"`
	Next  int64            `json:"next,omitempty"`
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func targetsView(t Targets) TargetsView {
	return TargetsView{
		Domains: orEmpty(t.Domains),
		IPs:     orEmpty(t.IPs),
		GeoSite: orEmpty(t.GeoSite),
		GeoIP:   orEmpty(t.GeoIP),
		ASNs:    orEmpty(t.ASNs),
		Filters: t.FilterTerms(),
	}
}

func orEmpty(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func reportView(r store.Report) ReportView {
	return ReportView{
		ID:          r.ID,
		SetID:       r.SetID,
		Version:     r.Version,
		Key:         hubdata.AuthorLabel(r.KeyHMAC),
		KeyHMAC:     r.KeyHMAC,
		KeyBanned:   r.KeyBanned,
		KeyTest:     r.KeyTest,
		ASNObserved: r.ASNObserved,
		Reason:      r.Reason,
		ReceivedAt:  r.ReceivedAt,
		State:       r.State,
		Resolution:  r.Resolution,
		Note:        r.Note,
		ResolvedAt:  optionalTime(r.ResolvedAt),
		Counts:      store.Counts(r),
	}
}

func buildRunView(b *store.BuildRun) *BuildRunView {
	if b == nil {
		return nil
	}
	return &BuildRunView{
		ID:         b.ID,
		Trigger:    b.Trigger,
		StartedAt:  b.StartedAt,
		FinishedAt: optionalTime(b.FinishedAt),
		OK:         b.OK,
		Error:      b.Error,
		Epoch:      b.Epoch,
		Seq:        b.Seq,
		File:       b.File,
		Size:       b.Size,
		Sets:       b.Sets,
		Blobs:      b.Blobs,
		Mirrors:    b.Mirrors,
		DurationMs: b.DurationMs,
		Changes:    b.Changes,
		Changed:    b.Changes.ContentChanged(),
	}
}

func moderationItems(items []moderation.Item) []ModerationItemView {
	out := make([]ModerationItemView, 0, len(items))
	for _, it := range items {
		out = append(out, ModerationItemView{
			SetID:       it.SetID,
			Version:     it.Version,
			Title:       it.Title,
			From:        it.From,
			To:          it.To,
			Listed:      it.Listed,
			ListedAfter: it.ListedAfter,
			Withheld:    it.Withheld,
			Reports:     it.Reports,
			OK:          it.OK,
			Code:        it.Code,
			Params:      it.Params,
		})
	}
	return out
}

func auditView(e store.AuditEntry) AuditEntryView {
	v := AuditEntryView{
		ID:         e.ID,
		At:         e.At,
		Actor:      e.Actor,
		ActorRef:   e.ActorRef,
		ActorIP:    e.ActorIP,
		Action:     e.Action,
		TargetKind: e.TargetKind,
		TargetID:   e.TargetID,
		Version:    e.Version,
		Reason:     e.Reason,
		Before:     e.Before,
		After:      e.After,
		BatchID:    e.BatchID,
	}
	if e.TargetKind == store.TargetKey {
		v.TargetLabel = hubdata.AuthorLabel(e.TargetID)
	}
	return v
}

func voteView(v store.Vote) VoteView {
	return VoteView{
		ID:              v.ID,
		SetID:           v.SetID,
		Version:         v.Version,
		FP:              v.FP,
		Key:             hubdata.AuthorLabel(v.KeyHMAC),
		KeyHMAC:         v.KeyHMAC,
		Kind:            v.Kind,
		Weight:          v.Weight,
		ASNObserved:     v.ASNObserved,
		CountryObserved: v.CountryObserved,
		ASNHint:         v.ASNHint,
		CountryHint:     v.CountryHint,
		OriginVerified:  v.OriginVerified,
		Domain:          v.Domain,
		B4Version:       v.B4Version,
		Engine:          v.Engine,
		ReceivedAt:      v.ReceivedAt,
	}
}

func mirrorView(m store.Mirror) MirrorView {
	return MirrorView{
		ID:                m.ID,
		URL:               m.URL,
		KeyHMAC:           m.KeyHMAC,
		Key:               hubdata.AuthorLabel(m.KeyHMAC),
		Status:            m.Status,
		Healthy:           m.Healthy(),
		FirstSeen:         m.FirstSeen,
		LastSeen:          m.LastSeen,
		LastCheck:         optionalTime(m.LastCheck),
		LastOK:            optionalTime(m.LastOK),
		Reason:            m.Reason,
		Version:           m.Version,
		CheckCode:         m.CheckCode,
		CheckError:        m.CheckError,
		CheckMillis:       m.CheckMillis,
		ServedEpoch:       m.ServedEpoch,
		ServedSeq:         m.ServedSeq,
		ServedGeneratedAt: m.ServedGeneratedAt,
		Lag:               LagUnknown,
	}
}

type entryContext struct {
	votes    map[string]map[int]store.VoteTotals
	reports  map[string]map[int][]store.Report
	withheld map[string]string
	authors  map[string]string
	banned   map[string]bool
}

func (s *Server) entryContext(ctx context.Context) (*entryContext, error) {
	votes, err := s.Store.VoteTotals(ctx)
	if err != nil {
		return nil, err
	}
	reports, err := s.Store.AllReports(ctx)
	if err != nil {
		return nil, err
	}
	byVersion := make(map[string]map[int][]store.Report)
	for _, r := range reports {
		if byVersion[r.SetID] == nil {
			byVersion[r.SetID] = make(map[int][]store.Report)
		}
		byVersion[r.SetID][r.Version] = append(byVersion[r.SetID][r.Version], r)
	}
	withheld, err := s.Store.Withheld(ctx)
	if err != nil {
		return nil, err
	}
	sets, err := s.Store.Sets(ctx)
	if err != nil {
		return nil, err
	}
	authors := make(map[string]string, len(sets))
	for id, set := range sets {
		authors[id] = set.AuthorHMAC
	}
	banned, err := s.Store.BannedKeySet(ctx)
	if err != nil {
		return nil, err
	}
	return &entryContext{votes: votes, reports: byVersion, withheld: withheld, authors: authors, banned: banned}, nil
}

func (s *Server) entry(v store.Version, ec *entryContext) EntryView {
	e := EntryView{
		SetID:               v.SetID,
		Version:             v.Version,
		Title:               v.Title,
		Description:         v.Description,
		Family:              v.Family,
		Engine:              v.Engine,
		B4Version:           v.B4Version,
		B4Min:               v.B4Min,
		FP:                  v.FP,
		Status:              v.Status,
		StatusReason:        v.StatusReason,
		Flags:               orEmpty(v.Flags),
		Author:              hubdata.AuthorLabel(v.UploaderHMAC),
		UploaderHMAC:        v.UploaderHMAC,
		ASNObserved:         v.ASNObserved,
		CountryObserved:     v.CountryObserved,
		ASNHint:             v.ASNHint,
		CountryHint:         v.CountryHint,
		CreatedAt:           v.CreatedAt,
		UpdatedAt:           v.UpdatedAt,
		Targets:             targetsView(TargetsOf(v.Projection)),
		Strategy:            []Term{},
		Emitted:             []EmittedView{},
		Pins:                []PinView{},
		Payloads:            v.Payloads,
		Projection:          v.Projection,
		Reports:             []ReportView{},
		EditedAt:            optionalTime(v.EditedAt),
		EditNote:            v.EditNote,
		OriginalProjection:  v.OriginalProjection,
		OriginalTitle:       v.OriginalTitle,
		OriginalDescription: v.OriginalDescription,
	}
	if e.Payloads == nil {
		e.Payloads = []hubwire.BlobRef{}
	}
	if e.Projection == nil {
		e.Projection = map[string]interface{}{}
	}
	set, err := DecodeSet(v.Projection)
	if err != nil {
		e.DecodeError = err.Error()
	} else {
		e.Strategy = Techniques(&set, v.Payloads)
		for _, name := range EmittedNames(&set, v.Payloads) {
			e.Emitted = append(e.Emitted, EmittedView{Name: name.Name, Source: name.Source, Unreadable: name.Unreadable})
		}
		for _, pin := range PinsOf(&set) {
			e.Pins = append(e.Pins, PinView{Domain: pin.Domain, Addresses: pin.Addresses})
		}
		e.DoHHost = DoHHostOf(&set)
	}
	if ec != nil {
		if totals, ok := ec.votes[v.SetID][v.Version]; ok {
			e.Votes = VotesView{Works: totals.Works, Broken: totals.Broken}
		}
		if reports := ec.reports[v.SetID][v.Version]; len(reports) > 0 {
			for _, r := range reports {
				e.Reports = append(e.Reports, reportView(r))
				if r.State == store.ReportOpen {
					e.OpenReports++
				}
			}
			e.Independent = store.IndependentOf(reports)
		}
		e.Withheld = ec.withheld[v.SetID]
		author := ec.authors[v.SetID]
		if author == "" {
			author = v.UploaderHMAC
		}
		e.AuthorBanned = ec.banned[author]
	}
	e.HiddenFrom = v.HiddenFrom
	return e
}
