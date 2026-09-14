package web

import (
	"context"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	LineageFirst    = "first"
	LineageReplaces = "replaces"
	LineageRelists  = "relists"
)

type TargetsView struct {
	Domains []string `json:"domains"`
	IPs     []string `json:"ips"`
	GeoSite []string `json:"geosite"`
	GeoIP   []string `json:"geoip"`
	Summary string   `json:"summary"`
}

type EmittedView struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type PinView struct {
	Domain    string   `json:"domain"`
	Addresses []string `json:"addresses"`
}

type ReportView struct {
	ID          int64     `json:"id"`
	SetID       string    `json:"set_id"`
	Version     int       `json:"version"`
	Key         string    `json:"key"`
	KeyHMAC     string    `json:"key_hmac"`
	ASNObserved string    `json:"asn_observed,omitempty"`
	Reason      string    `json:"reason"`
	ReceivedAt  time.Time `json:"received_at"`
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
	SetID           string                 `json:"set_id"`
	Version         int                    `json:"version"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description,omitempty"`
	Family          string                 `json:"family,omitempty"`
	Engine          string                 `json:"engine,omitempty"`
	B4Version       string                 `json:"b4_version,omitempty"`
	B4Min           string                 `json:"b4_min,omitempty"`
	FP              string                 `json:"fp"`
	Status          string                 `json:"status"`
	StatusReason    string                 `json:"status_reason,omitempty"`
	Flags           []string               `json:"flags"`
	Author          string                 `json:"author"`
	UploaderHMAC    string                 `json:"uploader_hmac"`
	ASNObserved     string                 `json:"asn_observed,omitempty"`
	CountryObserved string                 `json:"country_observed,omitempty"`
	ASNHint         string                 `json:"asn_hint,omitempty"`
	CountryHint     string                 `json:"country_hint,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	Targets         TargetsView            `json:"targets"`
	Strategy        []string               `json:"strategy"`
	Emitted         []EmittedView          `json:"emitted"`
	Pins            []PinView              `json:"pins"`
	DoHHost         string                 `json:"doh_host,omitempty"`
	Payloads        []hubwire.BlobRef      `json:"payloads"`
	Projection      map[string]interface{} `json:"projection"`
	DecodeError     string                 `json:"decode_error,omitempty"`
	Reports         []ReportView           `json:"reports"`
	Independent     int                    `json:"independent_reports"`
	Votes           VotesView              `json:"votes"`
	Versions        []int                  `json:"versions,omitempty"`
	SupersededBy    int                    `json:"superseded_by,omitempty"`
	SupersededAt    *time.Time             `json:"superseded_at,omitempty"`
	Lineage         *LineageView           `json:"lineage,omitempty"`
}

type SetsView struct {
	Pending    []EntryView `json:"pending"`
	Listed     []EntryView `json:"listed"`
	Superseded []EntryView `json:"superseded"`
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
	Versions           []EntryView `json:"versions"`
	Votes              []VoteView  `json:"votes"`
}

type KeyView struct {
	KeyHMAC   string     `json:"key_hmac"`
	Label     string     `json:"label"`
	FirstSeen time.Time  `json:"first_seen"`
	Banned    bool       `json:"banned"`
	BanReason string     `json:"ban_reason,omitempty"`
	BannedAt  *time.Time `json:"banned_at,omitempty"`
	Trusted   bool       `json:"trusted"`
	TrustedAt *time.Time `json:"trusted_at,omitempty"`
	Sets      int        `json:"sets"`
	Votes     int        `json:"votes"`
	Reports   int        `json:"reports"`
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
	ID        int64      `json:"id"`
	URL       string     `json:"url"`
	KeyHMAC   string     `json:"key_hmac"`
	Key       string     `json:"key"`
	Status    string     `json:"status"`
	Healthy   bool       `json:"healthy"`
	FirstSeen time.Time  `json:"first_seen"`
	LastSeen  time.Time  `json:"last_seen"`
	LastCheck *time.Time `json:"last_check,omitempty"`
	LastOK    *time.Time `json:"last_ok,omitempty"`
	Reason    string     `json:"reason,omitempty"`
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
	RevokedKeys []string   `json:"revoked_keys"`
	BuiltAt     *time.Time `json:"built_at,omitempty"`
	Dirty       bool       `json:"dirty"`
}

type CountsView struct {
	Pending         int `json:"pending"`
	Listed          int `json:"listed"`
	Superseded      int `json:"superseded"`
	Hidden          int `json:"hidden"`
	Rejected        int `json:"rejected"`
	Keys            int `json:"keys"`
	Banned          int `json:"banned"`
	MirrorsPending  int `json:"mirrors_pending"`
	MirrorsApproved int `json:"mirrors_approved"`
	MirrorsRejected int `json:"mirrors_rejected"`
	Votes           int `json:"votes"`
	Reports         int `json:"reports"`
}

type OverviewView struct {
	Version   string           `json:"version"`
	KeyID     string           `json:"key_id"`
	PublicURL string           `json:"public_url,omitempty"`
	Now       time.Time        `json:"now"`
	Catalogue CatalogueView    `json:"catalogue"`
	Counts    CountsView       `json:"counts"`
	Geo       []geo.FileStatus `json:"geo"`
}

type ActionResult struct {
	Notice string `json:"notice"`
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
		Summary: t.Summary(),
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
		ASNObserved: r.ASNObserved,
		Reason:      r.Reason,
		ReceivedAt:  r.ReceivedAt,
	}
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

func keyView(k store.KeySummary) KeyView {
	return KeyView{
		KeyHMAC:   k.KeyHMAC,
		Label:     hubdata.AuthorLabel(k.KeyHMAC),
		FirstSeen: k.FirstSeen,
		Banned:    k.Banned,
		BanReason: k.BanReason,
		BannedAt:  optionalTime(k.BannedAt),
		Trusted:   k.Trusted,
		TrustedAt: optionalTime(k.TrustedAt),
		Sets:      k.Sets,
		Votes:     k.Votes,
		Reports:   k.Reports,
	}
}

func mirrorView(m store.Mirror) MirrorView {
	return MirrorView{
		ID:        m.ID,
		URL:       m.URL,
		KeyHMAC:   m.KeyHMAC,
		Key:       hubdata.AuthorLabel(m.KeyHMAC),
		Status:    m.Status,
		Healthy:   m.Healthy(),
		FirstSeen: m.FirstSeen,
		LastSeen:  m.LastSeen,
		LastCheck: optionalTime(m.LastCheck),
		LastOK:    optionalTime(m.LastOK),
		Reason:    m.Reason,
	}
}

type entryContext struct {
	votes   map[string]map[int]store.VoteTotals
	reports map[string]map[int][]store.Report
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
	return &entryContext{votes: votes, reports: byVersion}, nil
}

func (s *Server) entry(ctx context.Context, v store.Version, ec *entryContext) EntryView {
	e := EntryView{
		SetID:           v.SetID,
		Version:         v.Version,
		Title:           v.Title,
		Description:     v.Description,
		Family:          v.Family,
		Engine:          v.Engine,
		B4Version:       v.B4Version,
		B4Min:           v.B4Min,
		FP:              v.FP,
		Status:          v.Status,
		StatusReason:    v.StatusReason,
		Flags:           orEmpty(v.Flags),
		Author:          hubdata.AuthorLabel(v.UploaderHMAC),
		UploaderHMAC:    v.UploaderHMAC,
		ASNObserved:     v.ASNObserved,
		CountryObserved: v.CountryObserved,
		ASNHint:         v.ASNHint,
		CountryHint:     v.CountryHint,
		CreatedAt:       v.CreatedAt,
		UpdatedAt:       v.UpdatedAt,
		Targets:         targetsView(TargetsOf(v.Projection)),
		Strategy:        []string{},
		Emitted:         []EmittedView{},
		Pins:            []PinView{},
		Payloads:        v.Payloads,
		Projection:      v.Projection,
		Reports:         []ReportView{},
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
		e.Strategy = orEmpty(StrategyWords(&set, v.Payloads))
		for _, name := range EmittedNames(&set, v.Payloads) {
			e.Emitted = append(e.Emitted, EmittedView{Name: name.Name, Source: name.Source})
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
			}
			e.Independent, _ = s.Store.IndependentReports(ctx, v.SetID, v.Version)
		}
	}
	return e
}
