package detector

import (
	"context"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

type SuiteStatus string

const (
	StatusPending  SuiteStatus = "pending"
	StatusRunning  SuiteStatus = "running"
	StatusComplete SuiteStatus = "complete"
	StatusFailed   SuiteStatus = "failed"
	StatusCanceled SuiteStatus = "canceled"
)

type Scope string

const (
	ScopeSites    Scope = "sites"
	ScopeDNS      Scope = "dns"
	ScopeHosting  Scope = "hosting"
	ScopeTelegram Scope = "telegram"
)

const (
	FetchBoth   = "both"
	FetchDirect = "direct"
)

type Options struct {
	Sites     []string `json:"sites"`
	Scopes    []Scope  `json:"scopes"`
	IPVersion string   `json:"ip_version,omitempty"`
	Parallel  int      `json:"parallel,omitempty"`
	FetchMode string   `json:"fetch_mode,omitempty"`
	SkipTLS12 bool     `json:"skip_tls12,omitempty"`
	SNISearch bool     `json:"sni_search,omitempty"`
}

type Progress struct {
	Phase   Scope  `json:"phase,omitempty"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current,omitempty"`
}

type NetworkInfo struct {
	WANIP   string `json:"wan_ip,omitempty"`
	ASN     string `json:"asn,omitempty"`
	Org     string `json:"org,omitempty"`
	Country string `json:"country,omitempty"`
	IPv6    bool   `json:"ipv6"`
}

type FetchStatus = netprobe.DomainStatus

const (
	FetchOk                   = netprobe.DomainOk
	FetchPending  FetchStatus = "PENDING"
	FetchChecking FetchStatus = "CHECKING"
	FetchSkipped  FetchStatus = "SKIPPED"
	FetchServer   FetchStatus = "SERVER_ERROR"
)

type Fetch struct {
	Status     FetchStatus `json:"status"`
	Detail     string      `json:"detail,omitempty"`
	LatencyMs  int64       `json:"latency_ms,omitempty"`
	Bytes      int64       `json:"bytes,omitempty"`
	StatusCode int         `json:"status_code,omitempty"`
	RedirectTo string      `json:"redirect_to,omitempty"`
	TLS12      FetchStatus `json:"tls12,omitempty"`
	HTTP       FetchStatus `json:"http,omitempty"`
	HTTPDetail string      `json:"http_detail,omitempty"`
}

type SiteOutcome string

const (
	OutcomePending      SiteOutcome = "pending"
	OutcomeOk           SiteOutcome = "ok"
	OutcomeFixed        SiteOutcome = "fixed"
	OutcomeStillBlocked SiteOutcome = "still_blocked"
	OutcomeBlocked      SiteOutcome = "blocked"
	OutcomeBrokenByB4   SiteOutcome = "broken_by_b4"
	OutcomeServer       SiteOutcome = "server"
	OutcomeError        SiteOutcome = "error"
)

type SiteResult struct {
	Input      string      `json:"input"`
	Domain     string      `json:"domain"`
	URL        string      `json:"url"`
	IP         string      `json:"ip,omitempty"`
	HonestIP   string      `json:"honest_ip,omitempty"`
	FakeDNS    bool        `json:"fake_dns,omitempty"`
	Direct     *Fetch      `json:"direct,omitempty"`
	ThroughB4  *Fetch      `json:"through_b4,omitempty"`
	Outcome    SiteOutcome `json:"outcome"`
	SetId      string      `json:"set_id,omitempty"`
	SetName    string      `json:"set_name,omitempty"`
	SetEnabled bool        `json:"set_enabled,omitempty"`
	SetDNS     bool        `json:"set_dns,omitempty"`
	Done       bool        `json:"done"`
}

type SitesResult struct {
	Sites        []SiteResult `json:"sites"`
	Ok           int          `json:"ok"`
	Blocked      int          `json:"blocked"`
	Fixed        int          `json:"fixed"`
	StillBlocked int          `json:"still_blocked"`
	BrokenByB4   int          `json:"broken_by_b4"`
	Server       int          `json:"server"`
	Errors       int          `json:"errors"`
	StubIPs      []string     `json:"stub_ips,omitempty"`
}

type DNSProbeStatus string

const (
	DNSProbeOk      DNSProbeStatus = "ok"
	DNSProbeTimeout DNSProbeStatus = "timeout"
	DNSProbeBlocked DNSProbeStatus = "blocked"
	DNSProbeError   DNSProbeStatus = "error"
)

type DNSHonesty string

const (
	HonestyHonest      DNSHonesty = "honest"
	HonestySubstituted DNSHonesty = "substituted"
	HonestyFiltered    DNSHonesty = "filtered"
	HonestyDiffers     DNSHonesty = "differs"
	HonestyUnknown     DNSHonesty = "unknown"
)

type DNSProbe struct {
	Address       string         `json:"address"`
	Status        DNSProbeStatus `json:"status"`
	LatencyMs     float64        `json:"latency_ms,omitempty"`
	Honesty       DNSHonesty     `json:"honesty,omitempty"`
	Substituted   int            `json:"substituted,omitempty"`
	Checked       int            `json:"checked,omitempty"`
	AnsweredBy    string         `json:"answered_by,omitempty"`
	AnsweredByASN string         `json:"answered_by_asn,omitempty"`
	AnsweredByOrg string         `json:"answered_by_org,omitempty"`
	Hijacked      bool           `json:"hijacked,omitempty"`
	Detail        string         `json:"detail,omitempty"`
}

type DNSProvider struct {
	Name   string    `json:"name"`
	Router bool      `json:"router,omitempty"`
	UDP    *DNSProbe `json:"udp,omitempty"`
	DoH    *DNSProbe `json:"doh,omitempty"`
	DoT    *DNSProbe `json:"dot,omitempty"`
}

type DNSResult struct {
	Providers      []DNSProvider `json:"providers"`
	UDPOk          int           `json:"udp_ok"`
	UDPTotal       int           `json:"udp_total"`
	DoHOk          int           `json:"doh_ok"`
	DoHTotal       int           `json:"doh_total"`
	DoTOk          int           `json:"dot_ok"`
	DoTTotal       int           `json:"dot_total"`
	Hijacked       int           `json:"hijacked"`
	HijackedBy     string        `json:"hijacked_by,omitempty"`
	HijackedByASN  string        `json:"hijacked_by_asn,omitempty"`
	Substituting   int           `json:"substituting"`
	HonestDoH      []string      `json:"honest_doh,omitempty"`
	StubIPs        []string      `json:"stub_ips,omitempty"`
	TruthAvailable bool          `json:"truth_available"`
	RouterServers  []string      `json:"router_servers,omitempty"`
}

type HostingStatus string

const (
	HostingOk      HostingStatus = "ok"
	HostingDropped HostingStatus = "dropped"
	HostingMixed   HostingStatus = "mixed"
	HostingTimeout HostingStatus = "timeout"
	HostingError   HostingStatus = "error"
)

type TCPTarget struct {
	ID        string `json:"id"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	ASN       string `json:"asn"`
	Provider  string `json:"provider"`
	SNI       string `json:"sni,omitempty"`
	Reference bool   `json:"reference,omitempty"`
}

type TargetResult struct {
	Target   TCPTarget     `json:"target"`
	Status   HostingStatus `json:"status"`
	DropAtKB int           `json:"drop_at_kb,omitempty"`
	RTTMs    float64       `json:"rtt_ms,omitempty"`
	Detail   string        `json:"detail,omitempty"`
	Done     bool          `json:"done"`
}

type HostingGroup struct {
	ASN         string         `json:"asn"`
	Provider    string         `json:"provider"`
	Reference   bool           `json:"reference,omitempty"`
	Status      HostingStatus  `json:"status"`
	Total       int            `json:"total"`
	Dropped     int            `json:"dropped"`
	Ok          int            `json:"ok"`
	Timeouts    int            `json:"timeouts"`
	DropMinKB   int            `json:"drop_min_kb,omitempty"`
	DropMaxKB   int            `json:"drop_max_kb,omitempty"`
	WorkingSNIs []string       `json:"working_snis,omitempty"`
	SNISearched bool           `json:"sni_searched,omitempty"`
	Targets     []TargetResult `json:"targets"`
}

type HostingResult struct {
	Groups        []HostingGroup `json:"groups"`
	DroppedGroups int            `json:"dropped_groups"`
	OkGroups      int            `json:"ok_groups"`
	Total         int            `json:"total"`
	Dropped       int            `json:"dropped"`
	Ok            int            `json:"ok"`
}

type TelegramVerdict string

const (
	TGOk      TelegramVerdict = "ok"
	TGSlow    TelegramVerdict = "slow"
	TGStalled TelegramVerdict = "stalled"
	TGBlocked TelegramVerdict = "blocked"
	TGPartial TelegramVerdict = "partial"
	TGError   TelegramVerdict = "error"
)

type TelegramThroughput struct {
	Verdict    TelegramVerdict `json:"verdict"`
	Bytes      int64           `json:"bytes"`
	Expected   int64           `json:"expected"`
	PctOk      float64         `json:"pct_ok"`
	DurationMs int64           `json:"duration_ms"`
	MbpsAvg    float64         `json:"mbps_avg"`
	MbpsPeak   float64         `json:"mbps_peak"`
	DropAtSec  int             `json:"drop_at_sec,omitempty"`
	Detail     string          `json:"detail,omitempty"`
}

type TelegramDCPing struct {
	DC      int     `json:"dc"`
	Address string  `json:"address"`
	Ok      bool    `json:"ok"`
	RTTMs   float64 `json:"rtt_ms,omitempty"`
}

type TelegramResult struct {
	Download    TelegramThroughput `json:"download"`
	Upload      TelegramThroughput `json:"upload"`
	DCPings     []TelegramDCPing   `json:"dc_pings"`
	DCReachable int                `json:"dc_reachable"`
	DCTotal     int                `json:"dc_total"`
	Verdict     TelegramVerdict    `json:"verdict"`
}

type Verdict struct {
	BlockedByISP   int            `json:"blocked_by_isp"`
	FixedByB4      int            `json:"fixed_by_b4"`
	StillBlocked   int            `json:"still_blocked"`
	BrokenByB4     int            `json:"broken_by_b4"`
	NotBlocked     int            `json:"not_blocked"`
	Sites          int            `json:"sites"`
	BlockKinds     map[string]int `json:"block_kinds,omitempty"`
	StillBlockedAt []string       `json:"still_blocked_sites,omitempty"`
	DNSHijacked    bool           `json:"dns_hijacked"`
	DNSSubstituted bool           `json:"dns_substituted"`
	DoHWorks       bool           `json:"doh_works"`
	DoTWorks       bool           `json:"dot_works"`
	DroppedNets    []string       `json:"dropped_networks,omitempty"`
	Telegram       string         `json:"telegram,omitempty"`
}

type Suite struct {
	Id        string      `json:"id"`
	Status    SuiteStatus `json:"status"`
	StartTime time.Time   `json:"start_time"`
	EndTime   time.Time   `json:"end_time,omitempty"`
	Options   Options     `json:"options"`
	Progress  Progress    `json:"progress"`
	ListsDate string      `json:"lists_date,omitempty"`

	Network  *NetworkInfo    `json:"network,omitempty"`
	Sites    *SitesResult    `json:"sites,omitempty"`
	DNS      *DNSResult      `json:"dns,omitempty"`
	Hosting  *HostingResult  `json:"hosting,omitempty"`
	Telegram *TelegramResult `json:"telegram,omitempty"`
	Verdict  Verdict         `json:"verdict"`

	directMark uint
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	setLookup  SetLookup
}

type SetMatch struct {
	Id      string
	Name    string
	Enabled bool
	DNS     bool
}

type SetLookup func(domain string) *SetMatch
