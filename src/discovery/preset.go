package discovery

import (
	"fmt"

	"github.com/daniellavrushin/b4/config"
)

func GetPhase1Presets() []ConfigPreset {
	return []ConfigPreset{

		// 0. Raw baseline - no bypass at all (to detect if DPI even blocks)
		{
			Name:        "no-bypass",
			Description: "No bypass techniques - test raw connectivity",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    0,
			Config:      baselineConfig(),
		},

		// 1a. TCP MD5 option - bypasses TSPU 16KB block
		{
			Name:        "tcpmd5-combo",
			Description: "TCP MD5 option to bypass TSPU 16KB throttling block",
			Family:      FamilyTCPMD5,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: config.UDPConfig{
					Mode:           "fake",
					FakeSeqLength:  6,
					FakeLen:        64,
					FakingStrategy: "none",
					FilterQUIC:     "disabled",
					FilterSTUN:     true,
					ConnBytesLimit: 8,
				},
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          8,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
					TCPMD5:       true,
				},
			},
		},

		// 1d. Incoming fake bypass - TSPU behavioral throttling bypass
		{
			Name:        "incoming-fake",
			Description: "Incoming fake packets to bypass TSPU behavioral throttling",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      "fake",
						Min:       14,
						Max:       14,
						FakeTTL:   7,
						FakeCount: 5,
						Strategy:  "badsum",
					},
				},
				UDP: config.UDPConfig{
					Mode:           "fake",
					FakeSeqLength:  6,
					FakeLen:        64,
					FakingStrategy: "none",
					FilterQUIC:     "disabled",
					FilterSTUN:     true,
					ConnBytesLimit: 8,
				},
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          8,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 1e. Incoming fake with random corruption strategy
		{
			Name:        "incoming-fake-rand",
			Description: "Incoming fake with random corruption for harder DPI fingerprinting",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      "fake",
						Min:       14,
						Max:       14,
						FakeTTL:   7,
						FakeCount: 3,
						Strategy:  "rand",
					},
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          8,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 1f. Incoming reset mode - threshold-based RST injection
		{
			Name:        "incoming-reset",
			Description: "Inject fake RST at threshold to reset DPI byte counter",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      "reset",
						Min:       10,
						Max:       18,
						FakeTTL:   5,
						FakeCount: 3,
						Strategy:  "badsum",
					},
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          8,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 1. Proven working config - this is the baseline that works for most Russian DPI
		{
			Name:        "proven-combo",
			Description: "Proven combination: TCP frag + reverse + middle SNI + fake pastseq",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: config.UDPConfig{
					Mode:           "fake",
					FakeSeqLength:  6,
					FakeLen:        64,
					FakingStrategy: "none",
					FilterQUIC:     "disabled",
					FilterSTUN:     true,
					ConnBytesLimit: 8,
				},
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          8,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 1b. Proven config with alternate payload
		{
			Name:        "proven-combo-alt",
			Description: "Proven combination with alternate fake payload",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: config.UDPConfig{
					Mode:           "fake",
					FakeSeqLength:  6,
					FakeLen:        64,
					FakingStrategy: "none",
					FilterQUIC:     "disabled",
					FilterSTUN:     true,
					ConnBytesLimit: 8,
				},
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          8,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault2,
				},
			},
		},

		// 1c. Proven disorder config for aggressive DPI (Meta/Instagram style)
		{
			Name:        "proven-disorder",
			Description: "Proven disorder combination with aggressive desync for Meta-style DPI",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					DropSACK:       true,
					Desync: config.DesyncConfig{
						Mode:       "ack",
						TTL:        7,
						Count:      15,
						PostDesync: false,
					},
				},
				UDP: config.UDPConfig{
					Mode:           "fake",
					FakeSeqLength:  15,
					FakeLen:        64,
					FakingStrategy: "checksum",
					FilterQUIC:     "parse",
					FilterSTUN:     true,
					ConnBytesLimit: 8,
				},
				Fragmentation: config.FragmentationConfig{
					Strategy:          "disorder",
					ReverseOrder:      true,
					MiddleSNI:         true,
					SNIPosition:       1,
					SeqOverlapPattern: []string{"0x16", "0x03", "0x03", "0x00", "0x00"},
					Disorder: config.DisorderFragConfig{
						ShuffleMode: "full",
						MinJitterUs: 500,
						MaxJitterUs: 2100,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    1000000,
					SNISeqLength: 12,
					SNIType:      config.FakePayloadDefault2,
					TLSMod:       []string{"rnd", "dupsid"},
				},
			},
		},

		// 2. TCP Frag + Fake (common combo)
		{
			Name:        "tcp-frag-fake",
			Description: "TCP fragmentation with fake SNI",
			Family:      FamilyTCPFrag,
			Phase:       PhaseStrategy,
			Priority:    2,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:    "tcp",
					SNIPosition: 1,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 3. TCP Frag + Reverse + Fake
		{
			Name:        "tcp-frag-rev-fake",
			Description: "TCP frag reverse order with fake SNI",
			Family:      FamilyTCPFrag,
			Phase:       PhaseStrategy,
			Priority:    3,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 4. TLS Record + Fake
		{
			Name:        "tls-rec-fake",
			Description: "TLS record splitting with fake SNI",
			Family:      FamilyTLSRec,
			Phase:       PhaseStrategy,
			Priority:    4,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:          "tls",
					TLSRecordPosition: 1,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 5. OOB + Fake
		{
			Name:        "oob-fake",
			Description: "Out-of-band with fake SNI",
			Family:      FamilyOOB,
			Phase:       PhaseStrategy,
			Priority:    5,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:    "oob",
					OOBPosition: 1,
					OOBChar:     'x',
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 6. Fake only (low TTL)
		{
			Name:        "fake-ttl-low",
			Description: "Fake SNI with low TTL (no fragmentation)",
			Family:      FamilyFakeSNI,
			Phase:       PhaseStrategy,
			Priority:    6,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy: "none",
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          3,
					Strategy:     "ttl",
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 7. SACK Drop + TCP Frag + Fake
		{
			Name:        "sack-frag-fake",
			Description: "SACK drop with TCP frag and fake",
			Family:      FamilySACK,
			Phase:       PhaseStrategy,
			Priority:    7,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					DropSACK:       true,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 8. Desync RST + Frag
		{
			Name:        "desync-rst-frag",
			Description: "TCP desync RST attack with fragmentation",
			Family:      FamilyDesync,
			Phase:       PhaseStrategy,
			Priority:    8,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Desync: config.DesyncConfig{
						Mode:       "rst",
						TTL:        3,
						Count:      3,
						PostDesync: false,
					},
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 9. Desync Combo (RST+FIN+ACK flood)
		{
			Name:        "desync-combo",
			Description: "TCP desync combo attack",
			Family:      FamilyDesync,
			Phase:       PhaseStrategy,
			Priority:    9,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Desync: config.DesyncConfig{
						Mode:       "combo",
						TTL:        7,
						Count:      5,
						PostDesync: false,
					},
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
					MiddleSNI:    true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 2,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 9.1  Combo + Decoy SNI
		{
			Name:        "combo-decoy",
			Description: "Combo with decoy packet (fake SNI before real)",
			Family:      FamilyCombo,
			Phase:       PhaseStrategy,
			Priority:    22,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      50,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "middle",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
						DecoyEnabled:   true,
						DecoySNIs:      []string{"ya.ru", "vk.com", "mail.ru"},
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 10. SYN Fake with TCP frag
		{
			Name:        "synfake-frag",
			Description: "SYN fake packets with fragmentation",
			Family:      FamilySynFake,
			Phase:       PhaseStrategy,
			Priority:    10,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					SynFake:        true,
					SynFakeLen:     0,
					SynTTL:         7,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 11. Seg2Delay with fragmentation (timing-based)
		{
			Name:        "delay-frag",
			Description: "Delayed segments with fragmentation",
			Family:      FamilyDelay,
			Phase:       PhaseStrategy,
			Priority:    11,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      10,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
					MiddleSNI:    true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 12. Very low TTL fake (TTL=1-2)
		{
			Name:        "fake-ttl-ultra-low",
			Description: "Fake SNI with ultra-low TTL",
			Family:      FamilyFakeSNI,
			Phase:       PhaseStrategy,
			Priority:    12,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          2,
					Strategy:     "ttl",
					SNISeqLength: 3,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 13. Full desync attack
		{
			Name:        "desync-full",
			Description: "Full desync attack sequence",
			Family:      FamilyDesync,
			Phase:       PhaseStrategy,
			Priority:    13,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Desync: config.DesyncConfig{
						Mode:  "full",
						TTL:   7,
						Count: 5,
					},
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
					MiddleSNI:    true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    50000,
					SNISeqLength: 3,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},
		// 14. Disorder - out-of-order segments
		{
			Name:        "disorder-basic",
			Description: "Out-of-order TCP segments with timing jitter",
			Family:      FamilyDisorder,
			Phase:       PhaseStrategy,
			Priority:    14,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      5,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy: "disorder",
					Disorder: config.DisorderFragConfig{
						ShuffleMode: "full",
						MinJitterUs: 1000,
						MaxJitterUs: 3000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 16. ExtSplit - split before SNI extension
		{
			Name:        "extsplit-basic",
			Description: "Split TLS ClientHello before SNI extension",
			Family:      FamilyExtSplit,
			Phase:       PhaseStrategy,
			Priority:    16,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      5,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "extsplit",
					ReverseOrder: true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 17. FirstByte - single byte desync
		{
			Name:        "firstbyte-basic",
			Description: "First byte desync exploiting DPI timeouts",
			Family:      FamilyFirstByte,
			Phase:       PhaseStrategy,
			Priority:    17,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      100,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy: "firstbyte",
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 18. Combo - multi-technique (recommended)
		{
			Name:        "combo-multi",
			Description: "Multi-technique: firstbyte + extsplit + SNI split + disorder",
			Family:      FamilyCombo,
			Phase:       PhaseStrategy,
			Priority:    18,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      100,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 5,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 18.1 Combo + Desync
		{
			Name:        "combo-desync",
			Description: "Combo fragmentation with desync attack",
			Family:      FamilyCombo,
			Phase:       PhaseStrategy,
			Priority:    19,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      10,

					Desync: config.DesyncConfig{
						Mode:  "ack",
						TTL:   7,
						Count: 3,
					},
				},
				UDP: config.UDPConfig{
					Mode:           "fake",
					FakeSeqLength:  6,
					FakeLen:        64,
					FakingStrategy: "none",
					FilterQUIC:     "parse",
					FilterSTUN:     true,
					ConnBytesLimit: 8,
				},
				Fragmentation: config.FragmentationConfig{
					Strategy:     "combo",
					ReverseOrder: true,
					MiddleSNI:    true,
					SNIPosition:  1,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "middle",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 5,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 19. Hybrid Adaptive - auto-select best techniques
		{
			Name:        "hybrid-adaptive",
			Description: "Adaptive evasion: auto-selects combo/disorder/extsplit/firstbyte",
			Family:      FamilyHybrid,
			Phase:       PhaseStrategy,
			Priority:    19,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      50,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:     "hybrid",
					MiddleSNI:    true,
					ReverseOrder: true,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 20. Combo + SeqOverlap
		{
			Name:        "combo-seqovl",
			Description: "Combo with sequence overlap bytes",
			Family:      FamilyCombo,
			Phase:       PhaseStrategy,
			Priority:    20,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      50,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:          "combo",
					ReverseOrder:      true,
					MiddleSNI:         true,
					SNIPosition:       1,
					SeqOverlapPattern: []string{"0x00", "0x00", "0x00", "0x00"},
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},

		// 21. Disorder + SeqOverlap
		{
			Name:        "disorder-seqovl",
			Description: "Disorder with sequence overlap",
			Family:      FamilyDisorder,
			Phase:       PhaseStrategy,
			Priority:    21,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      10,
				},
				UDP: defaultUDP(),
				Fragmentation: config.FragmentationConfig{
					Strategy:          "disorder",
					SeqOverlapPattern: []string{"0x00", "0x00", "0x00", "0x00"},
					Disorder: config.DisorderFragConfig{
						ShuffleMode: "full",
						MinJitterUs: 1000,
						MaxJitterUs: 3000,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				},
			},
		},
	}

}

func defaultUDP() config.UDPConfig {
	return config.UDPConfig{
		Mode:           "fake",
		FakeSeqLength:  6,
		FakeLen:        64,
		FakingStrategy: "none",
		FilterQUIC:     "disabled",
		FilterSTUN:     true,
		ConnBytesLimit: 8,
	}
}

// GetPhase2Presets generates optimization presets for a specific working family
func GetPhase2Presets(family StrategyFamily) []ConfigPreset {
	base := baseConfig()
	presets := []ConfigPreset{}

	switch family {
	case FamilyIncoming:
		// Mode variations
		modes := []string{"fake", "reset", "fin", "desync"}
		for _, mode := range modes {
			presets = append(presets, ConfigPreset{
				Name:     formatName("incoming-%s", mode),
				Family:   FamilyIncoming,
				Phase:    PhaseOptimize,
				Priority: 1,
				Config: withTCP(base, config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      mode,
						Min:       14,
						Max:       14,
						FakeTTL:   7,
						FakeCount: 5,
						Strategy:  "badsum",
					},
				}),
			})
		}

		// Strategy variations for fake mode
		strategies := []string{"badsum", "badseq", "badack", "rand", "all"}
		for _, strat := range strategies {
			presets = append(presets, ConfigPreset{
				Name:     formatName("incoming-fake-%s", strat),
				Family:   FamilyIncoming,
				Phase:    PhaseOptimize,
				Priority: 2,
				Config: withTCP(base, config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      "fake",
						Min:       14,
						Max:       14,
						FakeTTL:   7,
						FakeCount: 5,
						Strategy:  strat,
					},
				}),
			})
		}

		// TTL variations
		ttls := []uint8{4, 7, 8, 9, 10, 13}
		for _, ttl := range ttls {
			presets = append(presets, ConfigPreset{
				Name:     formatName("incoming-ttl%d", ttl),
				Family:   FamilyIncoming,
				Phase:    PhaseOptimize,
				Priority: int(ttl),
				Config: withTCP(base, config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      "fake",
						Min:       14,
						Max:       14,
						FakeTTL:   ttl,
						FakeCount: 5,
						Strategy:  "badsum",
					},
				}),
			})
		}

		// FakeCount variations
		counts := []int{1, 3, 5, 7, 10}
		for _, cnt := range counts {
			presets = append(presets, ConfigPreset{
				Name:     formatName("incoming-count%d", cnt),
				Family:   FamilyIncoming,
				Phase:    PhaseOptimize,
				Priority: cnt,
				Config: withTCP(base, config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      "fake",
						Min:       14,
						Max:       14,
						FakeTTL:   7,
						FakeCount: cnt,
						Strategy:  "badsum",
					},
				}),
			})
		}

		// Threshold variations for reset mode
		thresholds := []struct{ min, max int }{{10, 10}, {12, 16}, {14, 14}, {10, 19}}
		for _, t := range thresholds {
			presets = append(presets, ConfigPreset{
				Name:     formatName("incoming-reset-%d-%d", t.min, t.max),
				Family:   FamilyIncoming,
				Phase:    PhaseOptimize,
				Priority: t.max,
				Config: withTCP(base, config.TCPConfig{
					ConnBytesLimit: 19,
					Incoming: config.IncomingConfig{
						Mode:      "reset",
						Min:       t.min,
						Max:       t.max,
						FakeTTL:   7,
						FakeCount: 3,
						Strategy:  "badsum",
					},
				}),
			})
		}
	case FamilyCombo:
		shuffleModes := []string{"middle", "full", "edges"}
		delays := []int{50, 100, 150, 200}
		for _, mode := range shuffleModes {
			for _, d := range delays {
				presets = append(presets, ConfigPreset{
					Name:     formatName("combo-%s-delay%d", mode, d),
					Family:   FamilyCombo,
					Phase:    PhaseOptimize,
					Priority: d,
					Config: withTCP(withFragmentation(base, config.FragmentationConfig{
						Strategy:     "combo",
						ReverseOrder: true,
						MiddleSNI:    true,
						SNIPosition:  1,
						Combo: config.ComboFragConfig{
							FirstByteSplit: true,
							ExtensionSplit: true,
							ShuffleMode:    mode,
							FirstDelayMs:   d,
							JitterMaxUs:    2000,
						},
					}), config.TCPConfig{
						ConnBytesLimit: 19,
						Seg2Delay:      d,
					}),
				})
			}
		}

	case FamilyTCPFrag:
		positions := []int{1, 2, 3, 5, 10}
		for _, pos := range positions {
			for _, reverse := range []bool{false, true} {
				name := formatName("tcp-pos%d", pos)
				if reverse {
					name += "-rev"
				}
				presets = append(presets, ConfigPreset{
					Name:     name,
					Family:   FamilyTCPFrag,
					Phase:    PhaseOptimize,
					Priority: pos,
					Config: withFragmentation(base, config.FragmentationConfig{
						Strategy:     "tcp",
						SNIPosition:  pos,
						ReverseOrder: reverse,
					}),
				})
			}
		}
		// Add middle SNI variant
		presets = append(presets, ConfigPreset{
			Name:     "tcp-middle-sni",
			Family:   FamilyTCPFrag,
			Phase:    PhaseOptimize,
			Priority: 10,
			Config: withFragmentation(base, config.FragmentationConfig{
				Strategy:    "tcp",
				SNIPosition: 1,
				MiddleSNI:   true,
			}),
		})

	case FamilyDisorder:
		shuffleModes := []string{"full", "middle", "edges"}
		for _, mode := range shuffleModes {
			for _, d := range []int{0, 5, 10, 20} {
				presets = append(presets, ConfigPreset{
					Name:     formatName("disorder-%s-delay%d", mode, d),
					Family:   FamilyDisorder,
					Phase:    PhaseOptimize,
					Priority: d,
					Config: withTCP(withFragmentation(base, config.FragmentationConfig{
						Strategy: "disorder",
						Disorder: config.DisorderFragConfig{
							ShuffleMode: mode,
							MinJitterUs: 1000,
							MaxJitterUs: 3000,
						},
					}), config.TCPConfig{
						ConnBytesLimit: 19,
						Seg2Delay:      d,
					}),
				})
			}
		}
		// Jitter variations
		jitters := []struct{ min, max int }{{500, 1500}, {1000, 3000}, {2000, 5000}}
		for _, j := range jitters {
			presets = append(presets, ConfigPreset{
				Name:     formatName("disorder-jitter%d-%d", j.min, j.max),
				Family:   FamilyDisorder,
				Phase:    PhaseOptimize,
				Priority: j.max,
				Config: withFragmentation(base, config.FragmentationConfig{
					Strategy: "disorder",
					Disorder: config.DisorderFragConfig{
						ShuffleMode: "full",
						MinJitterUs: j.min,
						MaxJitterUs: j.max,
					},
				}),
			})
		}

		// TLS header overlap pattern
		tlsOverlapPatterns := [][]string{
			{"0x16", "0x03", "0x03", "0x00", "0x00"}, // TLS record header
			{"0x16", "0x03", "0x01", "0x00", "0x00"}, // TLS 1.0 variant
		}
		for i, pattern := range tlsOverlapPatterns {
			presets = append(presets, ConfigPreset{
				Name:     formatName("disorder-tlsovl%d", i+1),
				Family:   FamilyDisorder,
				Phase:    PhaseOptimize,
				Priority: 100 + i,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy:          "disorder",
					SeqOverlapPattern: pattern,
					Disorder: config.DisorderFragConfig{
						ShuffleMode: "full",
						MinJitterUs: 500,
						MaxJitterUs: 2100,
					},
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					DropSACK:       true,

					Desync: config.DesyncConfig{
						Mode:  "ack",
						TTL:   7,
						Count: 15,
					},
				}),
			})
		}

	case FamilyExtSplit:
		for _, reverse := range []bool{false, true} {
			name := "extsplit"
			if reverse {
				name += "-rev"
			}
			presets = append(presets, ConfigPreset{
				Name:     name,
				Family:   FamilyExtSplit,
				Phase:    PhaseOptimize,
				Priority: 1,
				Config: withFragmentation(base, config.FragmentationConfig{
					Strategy:     "extsplit",
					ReverseOrder: reverse,
				}),
			})
		}
		// Also test with different delays
		for _, d := range []int{0, 5, 10} {
			presets = append(presets, ConfigPreset{
				Name:     formatName("extsplit-delay%d", d),
				Family:   FamilyExtSplit,
				Phase:    PhaseOptimize,
				Priority: d + 10,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy:     "extsplit",
					ReverseOrder: true,
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      d,
				}),
			})
		}

	case FamilyFirstByte:
		delays := []int{50, 100, 150, 200, 300}
		for _, d := range delays {
			presets = append(presets, ConfigPreset{
				Name:     formatName("firstbyte-delay%d", d),
				Family:   FamilyFirstByte,
				Phase:    PhaseOptimize,
				Priority: d,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy: "firstbyte",
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      d,
				}),
			})
		}

	case FamilyTLSRec:
		positions := []int{1, 5, 10, 20, 50}
		for _, pos := range positions {
			for _, reverse := range []bool{false, true} {
				name := formatName("tls-pos%d", pos)
				if reverse {
					name += "-rev"
				}
				presets = append(presets, ConfigPreset{
					Name:     name,
					Family:   FamilyTLSRec,
					Phase:    PhaseOptimize,
					Priority: pos,
					Config: withFragmentation(base, config.FragmentationConfig{
						Strategy:          "tls",
						TLSRecordPosition: pos,
						ReverseOrder:      reverse,
					}),
				})
			}
		}

	case FamilyOOB:
		positions := []int{1, 2, 3, 5}
		chars := []byte{'x', 'a', 0x00, 0xFF}
		for _, pos := range positions {
			for _, ch := range chars {
				name := formatName("oob-pos%d-0x%02x", pos, ch)
				presets = append(presets, ConfigPreset{
					Name:     name,
					Family:   FamilyOOB,
					Phase:    PhaseOptimize,
					Priority: pos,
					Config: withFragmentation(base, config.FragmentationConfig{
						Strategy:    "oob",
						OOBPosition: pos,
						OOBChar:     ch,
					}),
				})
			}
		}

	case FamilyFakeSNI:
		// TTL variations
		ttls := []uint8{3, 5, 6, 7, 8, 9}
		for _, ttl := range ttls {
			presets = append(presets, ConfigPreset{
				Name:     formatName("fake-ttl%d", ttl),
				Family:   FamilyFakeSNI,
				Phase:    PhaseOptimize,
				Priority: int(ttl),
				Config: withFaking(base, config.FakingConfig{
					SNI:          true,
					TTL:          ttl,
					Strategy:     "ttl",
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				}),
			})
		}

		// Sequence length variations
		seqLens := []int{1, 2, 3, 5}
		for _, sl := range seqLens {
			presets = append(presets, ConfigPreset{
				Name:     formatName("fake-seq%d", sl),
				Family:   FamilyFakeSNI,
				Phase:    PhaseOptimize,
				Priority: sl + 10,
				Config: withFaking(base, config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: sl,
					SNIType:      config.FakePayloadDefault1,
				}),
			})
		}

		// Strategy variations
		strategies := []string{"ttl", "pastseq", "randseq", "tcp_check", "md5sum"}
		for i, strat := range strategies {
			presets = append(presets, ConfigPreset{
				Name:     formatName("fake-%s", strat),
				Family:   FamilyFakeSNI,
				Phase:    PhaseOptimize,
				Priority: i + 20,
				Config: withFaking(base, config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     strat,
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadDefault1,
				}),
			})
		}

		// Payload type variations
		payloadTypes := []struct {
			name    string
			sniType int
		}{
			{"payload1", config.FakePayloadDefault1},
			{"payload2", config.FakePayloadDefault2},
			{"payloadRand", config.FakePayloadRandom},
		}
		for _, pt := range payloadTypes {
			presets = append(presets, ConfigPreset{
				Name:     formatName("fake-%s", pt.name),
				Family:   FamilyFakeSNI,
				Phase:    PhaseOptimize,
				Priority: 30 + pt.sniType,
				Config: withFaking(base, config.FakingConfig{
					SNI:          true,
					TTL:          7,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      pt.sniType,
				}),
			})
		}

	case FamilyIPFrag:
		positions := []int{1, 8, 16, 24}
		for _, pos := range positions {
			for _, reverse := range []bool{false, true} {
				name := formatName("ip-pos%d", pos)
				if reverse {
					name += "-rev"
				}
				presets = append(presets, ConfigPreset{
					Name:     name,
					Family:   FamilyIPFrag,
					Phase:    PhaseOptimize,
					Priority: pos,
					Config: withFragmentation(base, config.FragmentationConfig{
						Strategy:     "ip",
						SNIPosition:  pos,
						ReverseOrder: reverse,
					}),
				})
			}
		}

	case FamilySACK:
		// SACK + different fragmentation strategies
		fragStrategies := []string{"tcp", "tls", "oob"}
		for i, fs := range fragStrategies {
			cfg := withTCP(base, config.TCPConfig{
				ConnBytesLimit: 19,
				DropSACK:       true,
			})
			switch fs {
			case "tcp":
				cfg = withFragmentation(cfg, config.FragmentationConfig{Strategy: "tcp", SNIPosition: 1})
			case "tls":
				cfg = withFragmentation(cfg, config.FragmentationConfig{Strategy: "tls", TLSRecordPosition: 1})
			case "oob":
				cfg = withFragmentation(cfg, config.FragmentationConfig{Strategy: "oob", OOBPosition: 1, OOBChar: 'x'})
			}
			presets = append(presets, ConfigPreset{
				Name:     formatName("sack-%s", fs),
				Family:   FamilySACK,
				Phase:    PhaseOptimize,
				Priority: i,
				Config:   cfg,
			})
		}

	case FamilyDesync:
		modes := []string{"rst", "fin", "ack", "combo", "full"}
		ttls := []uint8{3, 5, 6, 7, 8, 9}
		counts := []int{2, 5, 10, 15}

		for _, mode := range modes {
			for _, ttl := range ttls {
				for _, count := range counts {
					presets = append(presets, ConfigPreset{
						Name:     formatName("desync-%s-ttl%d-c%d", mode, ttl, count),
						Family:   FamilyDesync,
						Phase:    PhaseOptimize,
						Priority: int(ttl),
						Config: withTCP(withFragmentation(base, config.FragmentationConfig{
							Strategy:     "tcp",
							SNIPosition:  1,
							ReverseOrder: true,
						}), config.TCPConfig{
							ConnBytesLimit: 19,

							Desync: config.DesyncConfig{
								Mode:  mode,
								TTL:   ttl,
								Count: count,
							},
						}),
					})
				}
			}
		}

	case FamilySynFake:
		lengths := []int{0, 64, 128, 256, 512}
		for _, l := range lengths {
			presets = append(presets, ConfigPreset{
				Name:     formatName("synfake-len%d", l),
				Family:   FamilySynFake,
				Phase:    PhaseOptimize,
				Priority: l,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					SynFake:        true,
					SynFakeLen:     l,
				}),
			})
		}

	case FamilyDelay:
		delays := []int{1, 5, 10, 20, 50, 100}
		for _, d := range delays {
			presets = append(presets, ConfigPreset{
				Name:     formatName("delay-%dms", d),
				Family:   FamilyDelay,
				Phase:    PhaseOptimize,
				Priority: d,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy:     "tcp",
					SNIPosition:  1,
					ReverseOrder: true,
					MiddleSNI:    true,
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      d,
				}),
			})
		}

	case FamilyHybrid:
		delays := []int{30, 50, 100, 150}
		for _, d := range delays {
			presets = append(presets, ConfigPreset{
				Name:     formatName("hybrid-delay%d", d),
				Family:   FamilyHybrid,
				Phase:    PhaseOptimize,
				Priority: d,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy:     "hybrid",
					MiddleSNI:    true,
					ReverseOrder: true,
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      d,
				}),
			})
		}
	}

	return presets
}

// GetCombinationPresets generates presets combining multiple working families
func GetCombinationPresets(workingFamilies []StrategyFamily, bestParams map[StrategyFamily]ConfigPreset) []ConfigPreset {
	presets := []ConfigPreset{}

	// If we have both fragmentation and faking working, combine them
	hasFrag := containsFamily(workingFamilies, FamilyTCPFrag) || containsFamily(workingFamilies, FamilyTLSRec) || containsFamily(workingFamilies, FamilyOOB)
	hasFake := containsFamily(workingFamilies, FamilyFakeSNI)
	hasSACK := containsFamily(workingFamilies, FamilySACK)

	base := baseConfig()

	if hasFrag && hasFake {
		// Combine best frag with best fake
		var fragConfig config.FragmentationConfig
		var fakingConfig config.FakingConfig

		// Get best fragmentation params
		for _, fam := range []StrategyFamily{FamilyTCPFrag, FamilyTLSRec, FamilyOOB} {
			if bp, ok := bestParams[fam]; ok {
				fragConfig = bp.Config.Fragmentation
				break
			}
		}

		// Get best faking params
		if bp, ok := bestParams[FamilyFakeSNI]; ok {
			fakingConfig = bp.Config.Faking
		}

		combined := withFragmentation(base, fragConfig)
		combined = withFaking(combined, fakingConfig)

		presets = append(presets, ConfigPreset{
			Name:        "combo-frag-fake",
			Description: "Combined fragmentation + fake SNI",
			Family:      FamilyNone,
			Phase:       PhaseCombination,
			Priority:    1,
			Config:      combined,
		})
	}

	if hasSACK && hasFrag {
		// SACK + fragmentation
		var fragConfig config.FragmentationConfig
		for _, fam := range []StrategyFamily{FamilyTCPFrag, FamilyTLSRec, FamilyOOB} {
			if bp, ok := bestParams[fam]; ok {
				fragConfig = bp.Config.Fragmentation
				break
			}
		}

		combined := withTCP(base, config.TCPConfig{ConnBytesLimit: 19, DropSACK: true})
		combined = withFragmentation(combined, fragConfig)

		presets = append(presets, ConfigPreset{
			Name:        "combo-sack-frag",
			Description: "SACK drop + fragmentation",
			Family:      FamilyNone,
			Phase:       PhaseCombination,
			Priority:    2,
			Config:      combined,
		})
	}

	// Aggressive combo - everything together
	if len(workingFamilies) >= 2 {
		aggressive := config.SetConfig{
			TCP: config.TCPConfig{
				ConnBytesLimit: 1,
				Seg2Delay:      5,
				DropSACK:       hasSACK,
				SynFake:        true,
				SynFakeLen:     256,
			},
			UDP: config.UDPConfig{
				Mode:           "fake",
				FakeSeqLength:  10,
				FakeLen:        128,
				FakingStrategy: "checksum",
				FilterQUIC:     "all",
				FilterSTUN:     true,
				ConnBytesLimit: 1,
			},
			Fragmentation: config.FragmentationConfig{
				Strategy:     "tcp",
				SNIPosition:  1,
				ReverseOrder: true,
				MiddleSNI:    true,
			},
			Faking: config.FakingConfig{
				SNI:          true,
				TTL:          7,
				Strategy:     "pastseq",
				SeqOffset:    50000,
				SNISeqLength: 3,
				SNIType:      config.FakePayloadDefault1,
			},
		}

		presets = append(presets, ConfigPreset{
			Name:        "aggressive",
			Description: "All bypass techniques combined",
			Family:      FamilyNone,
			Phase:       PhaseCombination,
			Priority:    10,
			Config:      aggressive,
		})
	}

	return presets
}

// Helper functions

func baseConfig() config.SetConfig {
	return config.NewSetConfig()
}

func baselineConfig() config.SetConfig {
	return config.SetConfig{
		Enabled: false,
		TCP: config.TCPConfig{
			ConnBytesLimit: 19,
		},
		UDP: config.UDPConfig{
			Mode:           "fake",
			FakeSeqLength:  0,
			FakeLen:        0,
			FakingStrategy: "none",
			FilterQUIC:     "disabled",
			FilterSTUN:     false,
			ConnBytesLimit: 8,
		},
		Fragmentation: config.FragmentationConfig{
			Strategy: "none",
		},
		Faking: config.FakingConfig{
			SNI: false,
		},
	}
}

func withFragmentation(base config.SetConfig, frag config.FragmentationConfig) config.SetConfig {
	base.Fragmentation = frag
	return base
}

func withFaking(base config.SetConfig, faking config.FakingConfig) config.SetConfig {
	base.Faking = faking
	return base
}

func withTCP(base config.SetConfig, tcp config.TCPConfig) config.SetConfig {
	base.TCP = tcp
	return base
}

func formatName(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

func containsFamily(families []StrategyFamily, target StrategyFamily) bool {
	for _, f := range families {
		if f == target {
			return true
		}
	}
	return false
}
