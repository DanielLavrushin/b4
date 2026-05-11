package config

import (
	"math/rand"

	"github.com/daniellavrushin/b4/log"
)

const (
	ConfigOff  = "off"
	ConfigNone = "none"
)

const FakePayloadAutoQUIC = "@quic_initial"

const (
	RoutingModeInterface = "interface"
	RoutingModeProxy     = "proxy"
)

const (
	FakePayloadRandom = iota
	FakePayloadCustom
	FakePayloadDefault1
	FakePayloadDefault2
	FakePayloadCapture
	FakePayloadZero     // All-zero payload (0x00000000)
	FakePayloadInverted // Bitwise-inverted original TLS payload
	FakePayloadDomain
)

type ApiConfig struct {
	IPInfoToken string `json:"ipinfo_token"`
}

type QueueConfig struct {
	StartNum          int            `json:"start_num"`
	Threads           int            `json:"threads"`
	Mark              uint           `json:"mark"` // Main injected packets mark
	IPv4Enabled       bool           `json:"ipv4"`
	IPv6Enabled       bool           `json:"ipv6"`
	TCPConnBytesLimit int            `json:"tcp_conn_bytes_limit"`
	UDPConnBytesLimit int            `json:"udp_conn_bytes_limit"`
	Interfaces        []string       `json:"interfaces"`
	Devices           DevicesConfig  `json:"devices"`
	MSSClamp          MSSClampConfig `json:"mss_clamp"`
	IsDiscovery       bool           `json:"-"`
}

type DevicesConfig struct {
	Enabled      bool     `json:"enabled"`
	VendorLookup bool     `json:"vendor_lookup"`
	WhiteIsBlack bool     `json:"wisb"`
	Devices      []Device `json:"devices"`
}

type Device struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip,omitempty"`
	Name     string `json:"name,omitempty"`
	MSSClamp int    `json:"mss_clamp,omitempty"`
	Selected bool   `json:"selected"`
	IsManual bool   `json:"is_manual,omitempty"`
}

type TCPConfig struct {
	ConnBytesLimit int    `json:"conn_bytes_limit"`
	Seg2Delay      int    `json:"seg2delay"`
	Seg2DelayMax   int    `json:"seg2delay_max"`
	SynFake        bool   `json:"syn_fake"`
	SynFakeLen     int    `json:"syn_fake_len"`
	SynTTL         uint8  `json:"syn_ttl"`
	DropSACK       bool   `json:"drop_sack"`
	DPortFilter    string `json:"dport_filter"` // comma separated list of ports and port ranges, e.g. "80,443,5222"

	Incoming      IncomingConfig      `json:"incoming"`
	Desync        DesyncConfig        `json:"desync"`
	Win           WinConfig           `json:"win"`
	Duplicate     DuplicateConfig     `json:"duplicate"`
	IPBlockDetect IPBlockDetectConfig `json:"ip_block_detect"`
	RSTProtection RSTProtectionConfig `json:"rst_protection"`
}

type IPBlockDetectConfig struct {
	Enabled             bool `json:"enabled"`
	RetransmitThreshold int  `json:"retransmit_threshold"`
	TimeoutMs           int  `json:"timeout_ms"`
	CacheBlockedIPs     bool `json:"cache_blocked_ips"`
}

type RSTProtectionConfig struct {
	Enabled      bool `json:"enabled"`
	TTLTolerance int  `json:"ttl_tolerance"`
}

type WinConfig struct {
	Mode   string `json:"mode"`   // "off", "oscillate", "zero", "random", "escalate"
	Values []int  `json:"values"` // Custom window values
}

type DesyncConfig struct {
	Mode       string `json:"mode"`        // "off" "rst", "fin", "ack", "combo", "full"
	TTL        uint8  `json:"ttl"`         // TTL for desync packets
	Count      int    `json:"count"`       // Number of desync packets
	PostDesync bool   `json:"post_desync"` // Send fake RST after ClientHello
}

type IncomingConfig struct {
	Mode      string `json:"mode"` // "off", "fake", "reset", "fin", "desync"
	Min       int    `json:"min"`  // threshold min (KB)
	Max       int    `json:"max"`  // threshold max (KB), if 0 or eq MinKB -> uses MinKB
	FakeTTL   uint8  `json:"fake_ttl"`
	FakeCount int    `json:"fake_count"`
	Strategy  string `json:"strategy"` // "badsum", "badseq", "badack",  "rand", "all"
}

type UDPConfig struct {
	Mode            string `json:"mode"`
	FakeSeqLength   int    `json:"fake_seq_length"`
	FakeLen         int    `json:"fake_len"`
	FakingStrategy  string `json:"faking_strategy"`
	FakePayloadFile string `json:"fake_payload_file"` // "" = zero fill, "@quic_initial" = generate fresh QUIC Initial per packet, "@preset:quic1"/"@preset:quic2" = bundled presets, otherwise capture filename relative to config dir (e.g. "captures/quic_youtube_com.bin")
	FakePayloadData []byte `json:"-"`
	DPortFilter     string `json:"dport_filter"` // can be a comma separated list of ports and port ranges, e.g. "80,443,1000-2000"
	FilterQUIC      string `json:"filter_quic"`
	FilterSTUN      bool   `json:"filter_stun"`
	ConnBytesLimit  int    `json:"conn_bytes_limit"`
	Seg2Delay       int    `json:"seg2delay"`
	Seg2DelayMax    int    `json:"seg2delay_max"`
}

type FragmentationConfig struct {
	Strategy     string   `json:"strategy"` // Values: "tcp", "ip", "oob", "tls", "disorder",  "extsplit", "firstbyte", "combo", "none"
	ReverseOrder bool     `json:"reverse_order"`
	StrategyPool []string `json:"strategy_pool"`

	TLSRecordPosition    int `json:"tlsrec_pos"`     // where to split TLS record
	TLSRecordPositionMax int `json:"tlsrec_pos_max"` // max for randomization (0 = use fixed)

	MiddleSNI      bool `json:"middle_sni"`
	SNIPosition    int  `json:"sni_position"`
	SNIPositionMax int  `json:"sni_position_max"` // max for randomization (0 = use fixed)

	OOBPosition    int  `json:"oob_position"`     // Position for OOB (0=disabled)
	OOBPositionMax int  `json:"oob_position_max"` // max for randomization (0 = use fixed)
	OOBChar        byte `json:"oob_char"`         // Character for OOB data

	SeqOverlapPattern []string `json:"seq_overlap_pattern"`
	SeqOverlapBytes   []byte   `json:"-"`
	SeqOverlapLength  int      `json:"seq_overlap_length"`

	Combo    ComboFragConfig    `json:"combo"`
	Disorder DisorderFragConfig `json:"disorder"`
}

type FakingConfig struct {
	SNI               bool     `json:"sni"`
	TTL               uint8    `json:"ttl"`
	Strategy          string   `json:"strategy"`
	SeqOffset         int32    `json:"seq_offset"`
	SNISeqLength      int      `json:"sni_seq_length"`
	SNIType           int      `json:"sni_type"`
	CustomPayload     string   `json:"custom_payload"`
	PayloadFile       string   `json:"payload_file"`
	PayloadDomain     string   `json:"payload_domain"`
	PayloadData       []byte   `json:"-"`
	TLSMod            []string `json:"tls_mod"`            // e.g. ["rnd", "dupsid"]
	TimestampDecrease uint32   `json:"timestamp_decrease"` // Amount to decrease TCP timestamp option

	SNIMutation SNIMutationConfig `json:"sni_mutation"`
	TCPMD5      bool              `json:"tcp_md5"` // Enable TCP MD5 option insertion
}

type SNIMutationConfig struct {
	Mode         string   `json:"mode"` // "off", "duplicate", "grease", "padding", "reorder", "full"
	GreaseCount  int      `json:"grease_count"`
	PaddingSize  int      `json:"padding_size"`
	FakeExtCount int      `json:"fake_ext_count"`
	FakeSNIs     []string `json:"fake_snis"` // Additional SNIs to inject
}

type TargetsConfig struct {
	SNIDomains        []string `json:"sni_domains"`
	IPs               []string `json:"ip"`
	GeoSiteCategories []string `json:"geosite_categories"`
	GeoIpCategories   []string `json:"geoip_categories"`
	SourceDevices     []string `json:"source_devices"`
	TLSVersion        string   `json:"tls"` // "1.2", "1.3", or "" (match any)
	DomainsToMatch    []string `json:"-"`
	IpsToMatch        []string `json:"-"`
}

type SystemConfig struct {
	Tables      TablesConfig    `json:"tables"`
	Logging     Logging         `json:"logging"`
	WebServer   WebServerConfig `json:"web_server"`
	Socks5      Socks5Config    `json:"socks5"`
	MTProto     MTProtoConfig   `json:"mtproto"`
	Checker     DiscoveryConfig `json:"checker"`
	Geo         GeoDatConfig    `json:"geo"`
	API         ApiConfig       `json:"api"`
	AI          AIConfig        `json:"ai"`
	Timezone    string          `json:"timezone"`
	MemoryLimit string          `json:"memory_limit,omitempty"`
}

type AIConfig struct {
	Enabled     bool    `json:"enabled"`
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	Endpoint    string  `json:"endpoint"`
	APIKeyRef   string  `json:"api_key_ref"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
	TimeoutSec  int     `json:"timeout_sec"`
}

type MTProtoConfig struct {
	Enabled        bool   `json:"enabled"`
	Port           int    `json:"port"`
	BindAddress    string `json:"bind_address"`
	Secret         string `json:"secret"`
	FakeSNI        string `json:"fake_sni"`
	DCRelay        string `json:"dc_relay"`
	UpstreamMode   string `json:"upstream_mode"`
	WSCustomDomain string `json:"ws_custom_domain"`
	WSFallbackTCP  bool   `json:"ws_fallback_tcp"`
	WSEndpointHost string `json:"ws_endpoint_host"`
}

type Socks5Config struct {
	Enabled        bool   `json:"enabled"`
	Port           int    `json:"port"`
	BindAddress    string `json:"bind_address"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	UDPTimeout     int    `json:"udp_timeout"`
	UDPReadTimeout int    `json:"udp_read_timeout"`
}

type TablesConfig struct {
	MonitorInterval     int    `json:"monitor_interval"`
	SkipSetup           bool   `json:"skip_setup"`
	Engine              string `json:"engine"`
	Masquerade          bool   `json:"masquerade"`
	MasqueradeInterface string `json:"masquerade_interface"`
}

type WebServerConfig struct {
	Port        int    `json:"port"`
	BindAddress string `json:"bind_address"`
	TLSCert     string `json:"tls_cert"`
	TLSKey      string `json:"tls_key"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Language    string `json:"language"`
	IsEnabled   bool   `json:"-"`
}

type DiscoveryConfig struct {
	DiscoveryTimeoutSec   int            `json:"discovery_timeout"`
	ConfigPropagateMs     int            `json:"config_propagate_ms"`
	ReferenceDomain       string         `json:"reference_domain"`
	ReferenceDNS          []string       `json:"reference_dns"`
	ValidationTries       int            `json:"validation_tries"`
	DiscoveryFlowMark     uint           `json:"discovery_flow_mark"`
	DiscoveryInjectedMark uint           `json:"discovery_injected_mark"`
	Watchdog              WatchdogConfig `json:"watchdog"`
}

type WatchdogConfig struct {
	Enabled         bool     `json:"enabled"`
	Domains         []string `json:"domains"`
	IntervalSec     int      `json:"interval_sec"`
	FailureInterval int      `json:"failure_interval"`
	Cooldown        int      `json:"cooldown_sec"`
	TimeoutSec      int      `json:"timeout_sec"`
	MaxRetries      int      `json:"max_retries"`
}

type Logging struct {
	Level      log.Level `json:"level"`
	Instaflush bool      `json:"instaflush"`
	Syslog     bool      `json:"syslog"`
	ErrorFile  string    `json:"error_file"`
}

type SetConfig struct {
	Id            string              `json:"id"`
	Name          string              `json:"name"`
	TCP           TCPConfig           `json:"tcp"`
	UDP           UDPConfig           `json:"udp"`
	Fragmentation FragmentationConfig `json:"fragmentation"`
	Faking        FakingConfig        `json:"faking"`
	Targets       TargetsConfig       `json:"targets"`
	Enabled       bool                `json:"enabled"`
	DNS           DNSConfig           `json:"dns"`
	TCPPortRanges []PortRange         `json:"-"`
	UDPPortRanges []PortRange         `json:"-"`
	Routing       RoutingConfig       `json:"routing"`
	Escalate      EscalateConfig      `json:"escalate"`
}

type EscalateConfig struct {
	To           string `json:"to"`              // ID of next set to use after this set is detected as blocked for a destination
	RstThreshold int    `json:"rst_threshold"`   // 0 -> 3
	RstWindowSec int    `json:"rst_window_sec"`  // 0 -> 30
	TtlSec       int    `json:"ttl_sec"`         // 0 -> 3600
}

type GeoDatConfig struct {
	GeoSitePath string `json:"sitedat_path"`
	GeoIpPath   string `json:"ipdat_path"`
	GeoSiteURL  string `json:"sitedat_url"`
	GeoIpURL    string `json:"ipdat_url"`
}

type ComboFragConfig struct {
	FirstByteSplit     bool   `json:"first_byte_split"`
	ExtensionSplit     bool   `json:"extension_split"`
	ShuffleMode        string `json:"shuffle_mode"` // "middle", "full", "reverse"
	FirstDelayMs       int    `json:"first_delay_ms"`
	FirstDelayMsMax    int    `json:"first_delay_ms_max"`
	JitterMaxUs        int    `json:"jitter_max_us"`
	JitterMaxUsMax     int    `json:"jitter_max_us_max"`
	DecoyEnabled       bool   `json:"decoy_enabled"`
	FakePerSegment     bool   `json:"fake_per_segment"`
	FakePerSegCount    int    `json:"fake_per_seg_count"`     // Number of fake packets per segment (default 1)
	FakePerSegCountMax int    `json:"fake_per_seg_count_max"` // Max for randomization (0 = use fixed)
}

type DisorderFragConfig struct {
	ShuffleMode        string `json:"shuffle_mode"` // "full", "reverse"
	MinJitterUs        int    `json:"min_jitter_us"`
	MaxJitterUs        int    `json:"max_jitter_us"`
	FakePerSegment     bool   `json:"fake_per_segment"`
	FakePerSegCount    int    `json:"fake_per_seg_count"`     // Number of fake packets per segment (default 1)
	FakePerSegCountMax int    `json:"fake_per_seg_count_max"` // Max for randomization (0 = use fixed)
}

// ResolveRange returns a random value between min and max (inclusive).
// If max <= min (or max is 0), returns min as a single fixed value.
func ResolveRange(min, max int) int {
	if max <= min {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// ResolveSeg2Delay is an alias for ResolveRange for backward compatibility.
func ResolveSeg2Delay(min, max int) int {
	return ResolveRange(min, max)
}

// ResolveStrategyPool picks a random strategy from the pool.
// If pool is empty, returns the fallback.
func ResolveStrategyPool(pool []string, fallback string) string {
	if len(pool) == 0 {
		return fallback
	}
	return pool[rand.Intn(len(pool))]
}

type DNSConfig struct {
	Enabled       bool   `json:"enabled"`
	TargetDNS     string `json:"target_dns"`
	FragmentQuery bool   `json:"fragment_query"`
}

type DuplicateConfig struct {
	Enabled bool `json:"enabled"`
	Count   int  `json:"count"` // Number of packet copies to send (original is dropped)
}

type MSSClampConfig struct {
	Enabled bool `json:"enabled"`
	Size    int  `json:"size"` // MSS value in bytes (e.g., 88)
}

type RoutingConfig struct {
	Enabled          bool                `json:"enabled"`
	Mode             string              `json:"mode"`
	EgressInterface  string              `json:"egress_interface"`
	Upstream         UpstreamProxyConfig `json:"upstream"`
	FWMark           uint32              `json:"fwmark"`
	Table            int                 `json:"table"`
	SourceInterfaces []string            `json:"source_interfaces"`
	IPTTLSeconds     int                 `json:"ip_ttl_seconds"`
}

type UpstreamProxyConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	FailOpen  bool   `json:"fail_open"`
	UseDomain bool   `json:"use_domain"`
}
