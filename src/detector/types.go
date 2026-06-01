package detector

import (
	"sync"
	"time"
)

type SuiteStatus string

const (
	StatusPending  SuiteStatus = "pending"
	StatusRunning  SuiteStatus = "running"
	StatusComplete SuiteStatus = "complete"
	StatusFailed   SuiteStatus = "failed"
	StatusCanceled SuiteStatus = "canceled"
)

type TestType string

const (
	TestDNS      TestType = "dns"
	TestDNSAvail TestType = "dns-availability"
	TestDomains  TestType = "domains"
	TestTCP      TestType = "tcp"
	TestSNI      TestType = "sni"
	TestTelegram TestType = "telegram"
)

// DNS check types

type DNSStatus string

const (
	DNSOk           DNSStatus = "OK"
	DNSSpoofing     DNSStatus = "DNS_SPOOFING"
	DNSInterception DNSStatus = "DNS_INTERCEPTION"
	DNSFakeIP       DNSStatus = "FAKE_IP"
	DNSFakeNXDomain DNSStatus = "FAKE_NXDOMAIN"
	DNSFakeEmpty    DNSStatus = "FAKE_EMPTY"
	DNSDoHBlocked   DNSStatus = "DOH_BLOCKED"
	DNSBothUnavail  DNSStatus = "BOTH_UNAVAILABLE"
	DNSTimeout      DNSStatus = "TIMEOUT"
	DNSBlocked      DNSStatus = "BLOCKED"
)

type DNSDomainResult struct {
	Domain   string    `json:"domain"`
	DoHIP    string    `json:"doh_ip"`
	UDPIP    string    `json:"udp_ip"`
	Status   DNSStatus `json:"status"`
	IsStubIP bool      `json:"is_stub_ip,omitempty"`
}

type DNSResult struct {
	Status         DNSStatus         `json:"status"`
	DoHServer      string            `json:"doh_server"`
	UDPServer      string            `json:"udp_server"`
	DoHBlocked     bool              `json:"doh_blocked"`
	UDPBlocked     bool              `json:"udp_blocked"`
	StubIPs        []string          `json:"stub_ips,omitempty"`
	Domains        []DNSDomainResult `json:"domains"`
	Summary        string            `json:"summary"`
	SpoofCount     int               `json:"spoof_count"`
	InterceptCount int               `json:"intercept_count"`
	FakeIPCount    int               `json:"fakeip_count"`
	OkCount        int               `json:"ok_count"`
}

// DNS availability check types

type DNSAvailKind string

const (
	DNSAvailDoH DNSAvailKind = "doh"
	DNSAvailUDP DNSAvailKind = "udp"
)

type DNSAvailProviderResult struct {
	Provider string       `json:"provider"`
	Kind     DNSAvailKind `json:"kind"`
	Address  string       `json:"address"`
	AvgMs    float64      `json:"avg_ms"`
	Ok       bool         `json:"ok"`
	OkCount  int          `json:"ok_count"`
	Total    int          `json:"total"`
}

type DNSAvailResult struct {
	Providers []DNSAvailProviderResult `json:"providers"`
	DoHOk     int                      `json:"doh_ok"`
	DoHTotal  int                      `json:"doh_total"`
	UDPOk     int                      `json:"udp_ok"`
	UDPTotal  int                      `json:"udp_total"`
	Summary   string                   `json:"summary"`
}

// Telegram reachability/throughput check types

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
	DCPings     []TelegramDCPing   `json:"dc_pings"`
	DCReachable int                `json:"dc_reachable"`
	DCTotal     int                `json:"dc_total"`
	Verdict     TelegramVerdict    `json:"verdict"`
	Summary     string             `json:"summary"`
}

// Domain accessibility check types

type DomainStatus string

const (
	DomainOk       DomainStatus = "OK"
	DomainTLSDPI   DomainStatus = "TLS_DPI"
	DomainTLSMITM  DomainStatus = "TLS_MITM"
	DomainTLSSpoof DomainStatus = "TLS_SPOOF"
	DomainTLSAlert DomainStatus = "TLS_ALERT"
	DomainTLSReset DomainStatus = "TLS_RST"
	DomainTLSDrop  DomainStatus = "TLS_DROP"
	DomainSYNDrop  DomainStatus = "SYN_DROP"
	DomainTCP16    DomainStatus = "TCP16"
	DomainISPPage  DomainStatus = "ISP_PAGE"
	DomainBlocked  DomainStatus = "BLOCKED"
	DomainDNSFake  DomainStatus = "DNS_FAKE"
	DomainTimeout  DomainStatus = "TIMEOUT"
	DomainError    DomainStatus = "ERROR"
)

type TLSProbeResult struct {
	Status  DomainStatus `json:"status"`
	Detail  string       `json:"detail,omitempty"`
	Latency int64        `json:"latency_ms"`
}

type HTTPProbeResult struct {
	Status     DomainStatus `json:"status"`
	Detail     string       `json:"detail,omitempty"`
	StatusCode int          `json:"status_code,omitempty"`
	RedirectTo string       `json:"redirect_to,omitempty"`
}

type DomainCheckResult struct {
	Domain   string           `json:"domain"`
	IP       string           `json:"ip"`
	TLS13    *TLSProbeResult  `json:"tls13"`
	TLS12    *TLSProbeResult  `json:"tls12"`
	HTTP     *HTTPProbeResult `json:"http"`
	IsFakeIP bool             `json:"is_fake_ip,omitempty"`
	Overall  DomainStatus     `json:"overall"`
}

type DomainsResult struct {
	Domains      []DomainCheckResult `json:"domains"`
	BlockedCount int                 `json:"blocked_count"`
	OkCount      int                 `json:"ok_count"`
	DPICount     int                 `json:"dpi_count"`
	Summary      string              `json:"summary"`
}

// TCP fat probe test types

type TCPStatus string

const (
	TCPOk       TCPStatus = "OK"
	TCPDetected TCPStatus = "DETECTED"
	TCPMixed    TCPStatus = "MIXED"
	TCPTimeout  TCPStatus = "TIMEOUT"
	TCPError    TCPStatus = "ERROR"
)

type TCPTarget struct {
	ID       string `json:"id"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	ASN      string `json:"asn"`
	Provider string `json:"provider"`
	SNI      string `json:"sni,omitempty"`
}

type TCPTargetResult struct {
	Target   TCPTarget `json:"target"`
	Status   TCPStatus `json:"status"`
	Alive    bool      `json:"alive"`
	DropAtKB int       `json:"drop_at_kb,omitempty"`
	RTT      float64   `json:"rtt_ms,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

type TCPResult struct {
	Targets       []TCPTargetResult `json:"targets"`
	DetectedCount int               `json:"detected_count"`
	OkCount       int               `json:"ok_count"`
	Summary       string            `json:"summary"`
}

// SNI whitelist brute-force test types

type SNIStatus string

const (
	SNIFound      SNIStatus = "FOUND"
	SNINotFound   SNIStatus = "NOT_FOUND"
	SNINotBlocked SNIStatus = "NOT_BLOCKED"
)

type SNIASNResult struct {
	ASN      string    `json:"asn"`
	Provider string    `json:"provider"`
	IP       string    `json:"ip"`
	FoundSNI string    `json:"found_sni,omitempty"`
	Status   SNIStatus `json:"status"`
}

type SNIResult struct {
	ASNResults  []SNIASNResult `json:"asn_results"`
	FoundCount  int            `json:"found_count"`
	TestedCount int            `json:"tested_count"`
	Summary     string         `json:"summary"`
}

// Overall detection suite

type DetectorSuite struct {
	Id        string      `json:"id"`
	Status    SuiteStatus `json:"status"`
	StartTime time.Time   `json:"start_time"`
	EndTime   time.Time   `json:"end_time,omitempty"`

	Tests           []TestType `json:"tests"`
	CurrentTest     TestType   `json:"current_test,omitempty"`
	TotalChecks     int        `json:"total_checks"`
	CompletedChecks int        `json:"completed_checks"`

	DNSResult      *DNSResult      `json:"dns_result,omitempty"`
	DNSAvailResult *DNSAvailResult `json:"dnsavail_result,omitempty"`
	DomainsResult  *DomainsResult  `json:"domains_result,omitempty"`
	TCPResult      *TCPResult      `json:"tcp_result,omitempty"`
	SNIResult      *SNIResult      `json:"sni_result,omitempty"`
	TelegramResult *TelegramResult `json:"telegram_result,omitempty"`

	mark   uint          `json:"-"`
	mu     sync.RWMutex  `json:"-"`
	cancel chan struct{} `json:"-"`
}
