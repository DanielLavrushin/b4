package config

import (
	"github.com/daniellavrushin/b4/log"
)

var (
	CreateSetSentinel = "00000000-0000-0000-0000-000000000000"
)

type Config struct {
	Version    int    `json:"version"`
	ConfigPath string `json:"-"`

	Queue  QueueConfig  `json:"queue"`
	System SystemConfig `json:"system"`
	Sets   []*SetConfig `json:"sets"`

	tcpPortMap map[uint16]bool // pre-computed TCP port set for fast lookup in packet handler
}

var DefaultSetConfig = SetConfig{
	Id:      "",
	Name:    "",
	Enabled: true,

	UDP: UDPConfig{
		Mode:           "fake",
		FakeSeqLength:  6,
		FakeLen:        64,
		FakingStrategy: "none",
		DPortFilter:    "",
		FilterQUIC:     "disabled",
		FilterSTUN:     true,
		ConnBytesLimit: 8,
		Seg2Delay:      0,
	},

	TCP: TCPConfig{
		ConnBytesLimit: 19,
		Seg2Delay:      0,
		SynFake:        false,
		SynFakeLen:     0,
		SynTTL:         7,
		DPortFilter:    "",

		DropSACK: false,

		Win: WinConfig{
			Mode:   ConfigOff,
			Values: []int{0, 1460, 8192, 65535},
		},

		Desync: DesyncConfig{
			Mode:       ConfigOff,
			TTL:        7,
			Count:      3,
			PostDesync: false,
		},

		Incoming: IncomingConfig{
			Mode:      ConfigOff,
			Min:       14,
			Max:       14,
			FakeTTL:   7,
			FakeCount: 3,
			Strategy:  "badsum",
		},

		Duplicate: DuplicateConfig{
			Enabled: false,
			Count:   3,
		},

		IPBlockDetect: IPBlockDetectConfig{
			Enabled:             false,
			RetransmitThreshold: 3,
			TimeoutMs:           3000,
			CacheBlockedIPs:     true,
		},

		RSTProtection: RSTProtectionConfig{
			Enabled:      false,
			TTLTolerance: 3,
		},
	},

	DNS: DNSConfig{
		Enabled:       false,
		FragmentQuery: false,
		TargetDNS:     "",
	},

	Routing: RoutingConfig{
		Enabled:          false,
		Mode:             RoutingModeInterface,
		EgressInterface:  "",
		Upstream:         UpstreamProxyConfig{UseDomain: true},
		FWMark:           0,
		Table:            0,
		SourceInterfaces: []string{},
		IPTTLSeconds:     3600,
	},

	Fragmentation: FragmentationConfig{
		Strategy:             "combo", // "tcp", "ip", "tls", "oob", "none", "combo", "hybrid", "disorder",  "extsplit", "firstbyte"
		ReverseOrder:         true,
		StrategyPool:         []string{},
		MiddleSNI:            true,
		SNIPosition:          1,
		SNIPositionMax:       0,
		OOBPosition:          0,
		OOBPositionMax:       0,
		OOBChar:              'x',
		TLSRecordPosition:    0,
		TLSRecordPositionMax: 0,

		SeqOverlapBytes:   []byte{},
		SeqOverlapPattern: []string{},

		Combo: ComboFragConfig{
			FirstByteSplit:     true,
			ExtensionSplit:     true,
			ShuffleMode:        "full",
			FirstDelayMs:       30,
			JitterMaxUs:        1000,
			DecoyEnabled:       false,
			FakePerSegCount:    1,
			FakePerSegCountMax: 0,
		},

		Disorder: DisorderFragConfig{
			ShuffleMode:        "full",
			MinJitterUs:        1000,
			MaxJitterUs:        3000,
			FakePerSegCount:    1,
			FakePerSegCountMax: 0,
		},
	},

	Faking: FakingConfig{
		SNI:               true,
		TTL:               7,
		SNISeqLength:      1,
		SNIType:           FakePayloadDefault1,
		CustomPayload:     "",
		Strategy:          "pastseq",
		SeqOffset:         10000,
		PayloadFile:       "",
		PayloadDomain:     "",
		PayloadData:       []byte{},
		TLSMod:            []string{},
		TimestampDecrease: 600000, // Default value for timestamp faking strategy
		TCPMD5:            false,

		SNIMutation: SNIMutationConfig{
			Mode:         ConfigOff, // "off", "random", "grease", "padding", "fakeext", "fakesni", "advanced"
			GreaseCount:  3,
			PaddingSize:  2048,
			FakeExtCount: 5,
			FakeSNIs:     []string{"ya.ru", "vk.com", "max.ru"},
		},
	},

	Targets: TargetsConfig{
		SNIDomains:        []string{},
		IPs:               []string{},
		GeoSiteCategories: []string{},
		GeoIpCategories:   []string{},
		SourceDevices:     []string{},
	},

	Escalate: EscalateConfig{
		To:           "",
		RstThreshold: 3,
		RstWindowSec: 30,
		TtlSec:       3600,
	},
}

var DefaultConfig = Config{
	Version:    MinSupportedVersion,
	ConfigPath: "",

	Queue: QueueConfig{
		StartNum:          537,
		Mark:              1 << 15,
		Threads:           4,
		IPv4Enabled:       true,
		IPv6Enabled:       false,
		TCPConnBytesLimit: 19,
		UDPConnBytesLimit: 8,
		Interfaces:        []string{},
		Devices: DevicesConfig{
			Enabled:      false,
			VendorLookup: false,
			WhiteIsBlack: false,
			Devices:      []Device{},
		},
		MSSClamp: MSSClampConfig{
			Enabled: false,
			Size:    88,
		},
	},

	Sets: []*SetConfig{},

	System: SystemConfig{
		Geo: GeoDatConfig{
			GeoSitePath: "",
			GeoIpPath:   "",
			GeoSiteURL:  "",
			GeoIpURL:    "",
		},

		Tables: TablesConfig{
			MonitorInterval:     10,
			SkipSetup:           false,
			Engine:              "",
			Masquerade:          false,
			MasqueradeInterface: "",
		},

		WebServer: WebServerConfig{
			Port:        7000,
			BindAddress: "0.0.0.0",
			Language:    "en",
			IsEnabled:   true,
		},

		Socks5: Socks5Config{
			Enabled:        false,
			Port:           1080,
			BindAddress:    "0.0.0.0",
			UDPTimeout:     300,
			UDPReadTimeout: 5,
		},

		MTProto: MTProtoConfig{
			Enabled:        false,
			Port:           3128,
			BindAddress:    "0.0.0.0",
			FakeSNI:        "storage.googleapis.com",
			UpstreamMode:   "auto",
			WSEndpointHost: "149.154.167.220",
		},

		Logging: Logging{
			Level:      log.LevelInfo,
			Instaflush: true,
			Syslog:     false,
			ErrorFile:  "/var/log/b4/errors.log",
		},

		Checker: DiscoveryConfig{
			DiscoveryTimeoutSec: 5,
			ConfigPropagateMs:   1500,
			ReferenceDomain:     "yandex.ru",
			ReferenceDNS:        []string{"9.9.9.9", "1.1.1.1", "8.8.8.8", "9.9.1.1", "8.8.4.4"},
			ValidationTries:     1,
			Watchdog: WatchdogConfig{
				Enabled:         false,
				Domains:         []string{},
				IntervalSec:     300,
				FailureInterval: 60,
				Cooldown:        900,
				TimeoutSec:      15,
				MaxRetries:      3,
			},
		},
		API: ApiConfig{
			IPInfoToken: "",
		},

		AI: AIConfig{
			Enabled:     false,
			Provider:    "",
			Model:       "",
			Endpoint:    "",
			APIKeyRef:   "",
			MaxTokens:   1024,
			Temperature: 0.2,
			TimeoutSec:  120,
		},

		Timezone:    "",
		MemoryLimit: "",
	},
}

func NewSetConfig() SetConfig {
	cfg := DefaultSetConfig

	cfg.TCP.Win.Values = append(make([]int, 0), DefaultSetConfig.TCP.Win.Values...)
	cfg.Faking.SNIMutation.FakeSNIs = append(make([]string, 0), DefaultSetConfig.Faking.SNIMutation.FakeSNIs...)
	cfg.Targets.SNIDomains = append(make([]string, 0), DefaultSetConfig.Targets.SNIDomains...)
	cfg.Targets.IPs = append(make([]string, 0), DefaultSetConfig.Targets.IPs...)
	cfg.Targets.GeoSiteCategories = append(make([]string, 0), DefaultSetConfig.Targets.GeoSiteCategories...)
	cfg.Targets.GeoIpCategories = append(make([]string, 0), DefaultSetConfig.Targets.GeoIpCategories...)
	cfg.Targets.SourceDevices = append(make([]string, 0), DefaultSetConfig.Targets.SourceDevices...)
	cfg.Fragmentation.SeqOverlapPattern = append(make([]string, 0), DefaultSetConfig.Fragmentation.SeqOverlapPattern...)
	cfg.Fragmentation.StrategyPool = append(make([]string, 0), DefaultSetConfig.Fragmentation.StrategyPool...)
	cfg.Faking.TLSMod = append(make([]string, 0), DefaultSetConfig.Faking.TLSMod...)
	cfg.Routing.SourceInterfaces = append(make([]string, 0), DefaultSetConfig.Routing.SourceInterfaces...)

	return cfg
}

func NewConfig() Config {
	cfg := DefaultConfig

	cfg.Sets = []*SetConfig{}

	return cfg
}
