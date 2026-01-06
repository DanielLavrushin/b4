package config

import (
	"github.com/daniellavrushin/b4/log"
)

var (
	MAIN_SET_ID = "11111111-1111-1111-1111-111111111111"
	NEW_SET_ID  = "00000000-0000-0000-0000-000000000000"
)

type Config struct {
	Version    int    `json:"version" bson:"version"`
	ConfigPath string `json:"-" bson:"-"`

	Queue   QueueConfig  `json:"queue" bson:"queue"`
	MainSet *SetConfig   `json:"-" bson:"-"`
	System  SystemConfig `json:"system" bson:"system"`
	Sets    []*SetConfig `json:"sets" bson:"sets"`
}

var DefaultSetConfig = SetConfig{
	Id:      MAIN_SET_ID,
	Name:    "default",
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
	},

	DNS: DNSConfig{
		Enabled:       false,
		FragmentQuery: false,
		TargetDNS:     "",
	},

	Fragmentation: FragmentationConfig{
		Strategy:          "tcp", // "tcp", "ip", "tls", "oob", "none", "combo", "hybrid", "disorder",  "extsplit", "firstbyte"
		ReverseOrder:      true,
		MiddleSNI:         true,
		SNIPosition:       1,
		OOBPosition:       0,
		OOBChar:           'x',
		TLSRecordPosition: 0,

		SeqOverlapBytes:   []byte{},
		SeqOverlapPattern: []string{},

		Combo: ComboFragConfig{
			FirstByteSplit: true,
			ExtensionSplit: true,
			ShuffleMode:    "full",
			FirstDelayMs:   30,
			JitterMaxUs:    1000,
			DecoyEnabled:   false,
			DecoySNIs:      []string{"ya.ru", "vk.com", "mail.ru", "dzen.ru"},
		},

		Disorder: DisorderFragConfig{
			ShuffleMode: "full",
			MinJitterUs: 1000,
			MaxJitterUs: 3000,
		},
	},

	Faking: FakingConfig{
		SNI:           true,
		TTL:           7,
		SNISeqLength:  1,
		SNIType:       FakePayloadDefault1,
		CustomPayload: "",
		Strategy:      "pastseq",
		SeqOffset:     10000,
		PayloadFile:   "",
		PayloadData:   []byte{},
		TLSMod:        []string{},

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
	},
}

var DefaultConfig = Config{
	Version:    MinSupportedVersion,
	ConfigPath: "",

	Queue: QueueConfig{
		StartNum:    537,
		Mark:        1 << 15,
		Threads:     4,
		IPv4Enabled: true,
		IPv6Enabled: false,
		Interfaces:  []string{},
		Devices: DevicesConfig{
			Enabled:      false,
			WhiteIsBlack: false,
			Mac:          []string{},
		},
	},

	Sets: []*SetConfig{},

	MainSet: nil,

	System: SystemConfig{
		Geo: GeoDatConfig{
			GeoSitePath: "",
			GeoIpPath:   "",
			GeoSiteURL:  "",
			GeoIpURL:    "",
		},

		Tables: TablesConfig{
			MonitorInterval: 10,
			SkipSetup:       false,
		},

		WebServer: WebServerConfig{
			Port:        7000,
			BindAddress: "0.0.0.0",
			IsEnabled:   true,
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
		},
		API: ApiConfig{
			IPInfoToken: "",
		},
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
	cfg.Fragmentation.Combo.DecoySNIs = append(make([]string, 0), DefaultSetConfig.Fragmentation.Combo.DecoySNIs...)
	cfg.Fragmentation.SeqOverlapPattern = append(make([]string, 0), DefaultSetConfig.Fragmentation.SeqOverlapPattern...)
	cfg.Faking.TLSMod = append(make([]string, 0), DefaultSetConfig.Faking.TLSMod...)

	return cfg
}

func NewConfig() Config {
	cfg := DefaultConfig

	mainSet := NewSetConfig()
	cfg.MainSet = &mainSet

	cfg.Sets = []*SetConfig{}

	return cfg
}
