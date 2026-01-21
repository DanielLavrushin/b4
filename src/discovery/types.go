package discovery

import (
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

type CheckStatus string

const (
	CheckStatusPending  CheckStatus = "pending"
	CheckStatusRunning  CheckStatus = "running"
	CheckStatusComplete CheckStatus = "complete"
	CheckStatusFailed   CheckStatus = "failed"
	CheckStatusCanceled CheckStatus = "canceled"
)

type DiscoveryPhase string

const (
	PhaseBaseline    DiscoveryPhase = "baseline"
	PhaseStrategy    DiscoveryPhase = "strategy_detection"
	PhaseOptimize    DiscoveryPhase = "optimization"
	PhaseCombination DiscoveryPhase = "combination"
	PhaseDNS         DiscoveryPhase = "dns_detection"
)

type StrategyFamily string

const (
	FamilyNone      StrategyFamily = "none"
	FamilyTCPFrag   StrategyFamily = "tcp_frag"
	FamilyTLSRec    StrategyFamily = "tls_record"
	FamilyOOB       StrategyFamily = "oob"
	FamilyIPFrag    StrategyFamily = "ip_frag"
	FamilyFakeSNI   StrategyFamily = "fake_sni"
	FamilySACK      StrategyFamily = "sack"
	FamilySynFake   StrategyFamily = "syn_fake"
	FamilyDesync    StrategyFamily = "desync"
	FamilyWindow    StrategyFamily = "window"
	FamilyDelay     StrategyFamily = "delay"
	FamilyMutation  StrategyFamily = "mutation"
	FamilyDisorder  StrategyFamily = "disorder"
	FamilyOverlap   StrategyFamily = "overlap"
	FamilyExtSplit  StrategyFamily = "extsplit"
	FamilyFirstByte StrategyFamily = "firstbyte"
	FamilyCombo     StrategyFamily = "combo"
	FamilyHybrid    StrategyFamily = "hybrid"
	FamilyIncoming  StrategyFamily = "incoming"
	FamilyTCPMD5    StrategyFamily = "tcpmd5"
)

type CheckResult struct {
	ContentSize int64             `json:"content_size,omitempty"`
	Domain      string            `json:"domain"`
	Status      CheckStatus       `json:"status"`
	Duration    time.Duration     `json:"duration"`
	Speed       float64           `json:"speed"`
	BytesRead   int64             `json:"bytes_read"`
	Error       string            `json:"error,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	StatusCode  int               `json:"status_code"`
	Set         *config.SetConfig `json:"set"`
}

type CheckSuite struct {
	Id                     string                            `json:"id"`
	Status                 CheckStatus                       `json:"status"`
	StartTime              time.Time                         `json:"start_time"`
	EndTime                time.Time                         `json:"end_time"`
	TotalChecks            int                               `json:"total_checks"`
	CompletedChecks        int                               `json:"completed_checks"`
	SuccessfulChecks       int                               `json:"successful_checks"`
	FailedChecks           int                               `json:"failed_checks"`
	DomainDiscoveryResults map[string]*DomainDiscoveryResult `json:"domain_discovery_results,omitempty"`
	CheckURL               string                            `json:"check_url"`
	Domain                 string                            `json:"domain"`
	CurrentPhase           DiscoveryPhase                    `json:"current_phase,omitempty"`
	mu                     sync.RWMutex                      `json:"-"`
	cancel                 chan struct{}                     `json:"-"`
}

type DomainPresetResult struct {
	PresetName string            `json:"preset_name"`
	Family     StrategyFamily    `json:"family,omitempty"`
	Phase      DiscoveryPhase    `json:"phase,omitempty"`
	Status     CheckStatus       `json:"status"`
	Duration   time.Duration     `json:"duration"`
	Speed      float64           `json:"speed"`
	BytesRead  int64             `json:"bytes_read"`
	Error      string            `json:"error,omitempty"`
	StatusCode int               `json:"status_code"`
	Set        *config.SetConfig `json:"set"`
}

type DomainDiscoveryResult struct {
	Domain        string                         `json:"domain"`
	Url           string                         `json:"url"`
	BestPreset    string                         `json:"best_preset"`
	BestSpeed     float64                        `json:"best_speed"`
	BestSuccess   bool                           `json:"best_success"`
	Results       map[string]*DomainPresetResult `json:"results"`
	BaselineSpeed float64                        `json:"baseline_speed,omitempty"`
	Improvement   float64                        `json:"improvement,omitempty"`
	DNSResult     *DNSDiscoveryResult            `json:"dns_result,omitempty"`
}

type ConfigPreset struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Family      StrategyFamily   `json:"family"`
	Phase       DiscoveryPhase   `json:"phase"`
	Config      config.SetConfig `json:"config"`
	Priority    int              `json:"priority"`
}

type DNSProbeResult struct {
	Server     string        `json:"server"`
	Fragmented bool          `json:"fragmented"`
	ResolvedIP string        `json:"resolved_ip"`
	ExpectedIP string        `json:"expected_ip"`
	IsPoisoned bool          `json:"is_poisoned"`
	Works      bool          `json:"works"`
	Latency    time.Duration `json:"latency"`
}

type DNSDiscoveryResult struct {
	IsPoisoned    bool             `json:"is_poisoned"`
	ExpectedIPs   []string         `json:"expected_ips,omitempty"`
	BestServer    string           `json:"best_server,omitempty"`
	NeedsFragment bool             `json:"needs_fragment"`
	ProbeResults  []DNSProbeResult `json:"probe_results,omitempty"`
}

type PayloadTestResult struct {
	Speed   float64 `json:"speed"`
	Payload int     `json:"payload"`
	Works   bool    `json:"works"`
}

type DiscoverySuite struct {
	*CheckSuite
	networkBaseline float64
	optimalTTL      uint8

	pool         *nfq.Pool
	cfg          *config.Config
	domainResult *DomainDiscoveryResult

	workingPayloads []PayloadTestResult
	bestPayload     int
	bestPayloadFile string

	customPayloads []CustomPayload

	dnsResult       *DNSDiscoveryResult
	skipDNS         bool
	validationTries int
}

type CustomPayload struct {
	Name     string `json:"name"`
	Filepath string `json:"filepath"`
	Data     []byte `json:"-"`
}
