package metrics

const (
	FrameHello = "hello"
	FrameTick  = "tick"

	EngineStarting = "starting"
	EngineRunning  = "running"
	EngineFailed   = "failed"
	EngineStopping = "stopping"

	EngineModeNFQueue = "nfqueue"
	EngineModeTUN     = "tun"

	FirewallExternal = "external"

	SetKindBypass = "bypass"
	SetKindRoute  = "route"
	SetKindProxy  = "proxy"
	SetKindBlock  = "block"

	LevelInfo    = "info"
	LevelWarning = "warning"
	LevelError   = "error"

	EventStarted         = "started"
	EventEngineFailed    = "engine_failed"
	EventSettingsApplied = "settings_applied"
	EventTargetsWarning  = "targets_warning"
	EventSOCKS5Failed    = "socks5_failed"
	EventMTProtoFailed   = "mtproto_failed"
	EventWebFailed       = "web_failed"
	EventWebTLSUnusable  = "web_tls_unusable"
	EventRulesRestored   = "rules_restored"
	EventWatchdogHealed  = "watchdog_healed"
	EventWatchdogGaveUp  = "watchdog_gave_up"
	EventMCPWrite        = "mcp_write"
	EventUpdateInstalled = "update_installed"

	MinuteBuckets         = 61
	TenMinuteBuckets      = 145
	RSSBuckets            = 145
	EventsKept            = 50
	ErrorEventsKept       = 10
	BlockedDomainsKept    = 100
	BlockedDevicesKept    = 50
	OverloadWindowSeconds = 600
)

type Frame struct {
	Type          string          `json:"type"`
	Now           int64           `json:"now"`
	UptimeS       int64           `json:"uptime_s"`
	StatsSince    int64           `json:"stats_since"`
	Engine        EngineInfo      `json:"engine"`
	Rules         RulesInfo       `json:"rules"`
	LastPacketAt  int64           `json:"last_packet_at"`
	Process       ProcessInfo     `json:"process"`
	Activity      Activity        `json:"activity"`
	RSSHistory    []RSSBucket     `json:"rss_history"`
	Sets          []SetActivity   `json:"sets"`
	SetsDisabled  int             `json:"sets_disabled"`
	Totals        Totals          `json:"totals"`
	Blocked       *BlockedLists   `json:"blocked,omitempty"`
	Escalations   *EscalationList `json:"escalations,omitempty"`
	Events        *EventLog       `json:"events,omitempty"`
	Attention     Attention       `json:"attention"`
	MTProto       *MTProtoStats   `json:"mtproto,omitempty"`
	EngineFailure *EngineFailure  `json:"engine_failure,omitempty"`
}

type EngineInfo struct {
	State    string `json:"state"`
	Mode     string `json:"mode"`
	Threads  int    `json:"threads"`
	Firewall string `json:"firewall"`
}

type RulesInfo struct {
	Monitored   bool  `json:"monitored"`
	IntervalS   int   `json:"interval_s"`
	LastCheck   int64 `json:"last_check"`
	Restores    int64 `json:"restores"`
	LastRestore int64 `json:"last_restore"`
}

type ProcessInfo struct {
	RSSBytes       uint64  `json:"rss_bytes"`
	MemTotalBytes  uint64  `json:"mem_total_bytes"`
	CPUPercent     float64 `json:"cpu_percent"`
	CPUPeakPercent float64 `json:"cpu_peak_percent"`
	CPUPeakAt      int64   `json:"cpu_peak_at"`
	CPUCores       int     `json:"cpu_cores"`
	OSThreads      int     `json:"os_threads"`
	ThreadWarn     int     `json:"thread_warn"`
	ThreadLimit    int     `json:"thread_limit"`
}

type ActivityBucket struct {
	T        int64  `json:"t"`
	InSets   uint64 `json:"in_sets"`
	NotInSet uint64 `json:"not_in_set"`
}

type Activity struct {
	Minute    []ActivityBucket `json:"minute"`
	TenMinute []ActivityBucket `json:"ten_minute"`
}

type RSSBucket struct {
	T   int64  `json:"t"`
	Max uint64 `json:"max"`
}

type SetMeta struct {
	ID      string
	Name    string
	Kind    string
	Watched bool
}

type SetActivity struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Watched    bool   `json:"watched"`
	Conns60m   uint64 `json:"conns_60m"`
	LastMatch  int64  `json:"last_match"`
	Open       *int64 `json:"open,omitempty"`
	DNSBlocked uint64 `json:"dns_blocked"`
}

type Totals struct {
	Conns        uint64 `json:"conns"`
	ConnsInSets  uint64 `json:"conns_in_sets"`
	RSTDropped   uint64 `json:"rst_dropped"`
	Escalations  uint64 `json:"escalations"`
	BlockedDNS   uint64 `json:"blocked_dns"`
	BlockedConns uint64 `json:"blocked_conns"`
}

type BlockedEntry struct {
	Key   string `json:"key"`
	Count uint64 `json:"count"`
	Last  int64  `json:"last"`
}

type BlockedLists struct {
	Rev     uint64         `json:"rev"`
	Domains []BlockedEntry `json:"domains"`
	Devices []BlockedEntry `json:"devices"`
}

type EscalationList struct {
	Rev   uint64            `json:"rev"`
	Items []EscalationEntry `json:"items"`
}

type Event struct {
	ID      uint64            `json:"id"`
	T       int64             `json:"t"`
	Level   string            `json:"level"`
	Code    string            `json:"code"`
	Args    map[string]string `json:"args,omitempty"`
	Message string            `json:"message"`
}

type EventLog struct {
	Rev    uint64  `json:"rev"`
	Items  []Event `json:"items"`
	Errors []Event `json:"errors"`
}

type UpstreamAttention struct {
	SetID       string `json:"set_id"`
	SetName     string `json:"set_name"`
	Upstream    string `json:"upstream"`
	FailOpen    bool   `json:"fail_open"`
	Failures    int64  `json:"failures"`
	LastError   string `json:"last_error"`
	LastFailure int64  `json:"last_failure"`
}

type Overload struct {
	WindowS        int    `json:"window_s"`
	InjectSkipped  uint64 `json:"inject_skipped"`
	RawSendDropped uint64 `json:"raw_send_dropped"`
	QueueOverflow  uint64 `json:"queue_overflow"`
}

type Conntrack struct {
	Count uint64 `json:"count"`
	Max   uint64 `json:"max"`
}

type Attention struct {
	Upstreams      []UpstreamAttention `json:"upstreams"`
	BinaryReplaced bool                `json:"binary_replaced"`
	Overload       Overload            `json:"overload"`
	Conntrack      *Conntrack          `json:"conntrack,omitempty"`
}

type FlowKey struct {
	Addr  [32]byte
	SPort uint16
	DPort uint16
	Proto uint8
}
