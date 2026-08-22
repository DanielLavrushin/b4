package discovery

import (
	"fmt"

	"github.com/daniellavrushin/b4/config"
)

func GetPhase1Presets() []ConfigPreset {
	combo := comboFrag()
	udp := defaultUDP()

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

		// 1. Core combo + pastseq faking (most common working config for TSPU)
		{
			Name:        "combo-pastseq",
			Description: "Combo fragmentation with pastseq faking and randomized delay",
			Family:      FamilyCombo,
			Phase:       PhaseBaseline,
			Priority:    1,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   70,
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
				},
			},
		},

		// 2. Core combo + TCP timestamp faking strategy
		{
			Name:        "combo-timestamp",
			Description: "Combo fragmentation with TCP timestamp faking",
			Family:      FamilyCombo,
			Phase:       PhaseBaseline,
			Priority:    2,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   60,
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:               true,
					Strategy:          "timestamp",
					SeqOffset:         10000,
					SNISeqLength:      1,
					SNIType:           config.FakePayloadSTUN,
					TimestampDecrease: 600000,
				},
			},
		},

		// 3. Core combo + random sequence faking
		{
			Name:        "combo-random",
			Description: "Combo fragmentation with random sequence faking",
			Family:      FamilyCombo,
			Phase:       PhaseBaseline,
			Priority:    3,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   50,
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "randseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
				},
			},
		},

		// 4. TCP MD5 signature + combo + pastseq
		{
			Name:        "md5-combo-pastseq",
			Description: "TCP MD5 option with combo fragmentation and pastseq faking",
			Family:      FamilyTCPMD5,
			Phase:       PhaseBaseline,
			Priority:    4,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      30,
					Seg2DelayMax:   70,
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
					TCPMD5:       true,
				},
			},
		},

		// 5. TCP MD5 signature + combo + timestamp faking
		{
			Name:        "md5-combo-timestamp",
			Description: "TCP MD5 option with combo fragmentation and timestamp faking",
			Family:      FamilyTCPMD5,
			Phase:       PhaseBaseline,
			Priority:    5,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   60,
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:               true,
					Strategy:          "timestamp",
					SeqOffset:         10000,
					SNISeqLength:      1,
					SNIType:           config.FakePayloadSTUN,
					TCPMD5:            true,
					TimestampDecrease: 600000,
				},
			},
		},

		// 6. Post-ClientHello RST + combo + pastseq
		{
			Name:        "postrst-combo-pastseq",
			Description: "Post-ClientHello RST injection with combo and pastseq faking",
			Family:      FamilyDesync,
			Phase:       PhaseBaseline,
			Priority:    6,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      30,
					Seg2DelayMax:   70,
					Desync: config.DesyncConfig{
						Mode:       "rst",
						TTL:        7,
						Count:      3,
						PostDesync: true,
					},
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
				},
			},
		},

		// 7. Post-ClientHello RST + combo + timestamp faking
		{
			Name:        "postrst-combo-timestamp",
			Description: "Post-ClientHello RST injection with combo and timestamp faking",
			Family:      FamilyDesync,
			Phase:       PhaseBaseline,
			Priority:    7,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   60,
					Desync: config.DesyncConfig{
						Mode:       "rst",
						TTL:        7,
						Count:      3,
						PostDesync: true,
					},
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:               true,
					Strategy:          "timestamp",
					SeqOffset:         10000,
					SNISeqLength:      1,
					SNIType:           config.FakePayloadSTUN,
					TimestampDecrease: 600000,
				},
			},
		},

		// 8. TCP MD5 + Post-ClientHello RST + combo (full TSPU combo)
		{
			Name:        "md5-postrst-combo",
			Description: "TCP MD5 + Post-ClientHello RST + combo fragmentation",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    8,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      30,
					Seg2DelayMax:   90,
					Desync: config.DesyncConfig{
						Mode:       "rst",
						TTL:        7,
						Count:      3,
						PostDesync: true,
					},
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
					TCPMD5:       true,
				},
			},
		},

		// 9. Incoming fake + combo (bypasses TSPU 16KB throttling)
		{
			Name:        "incoming-fake-combo",
			Description: "Incoming fake packets to bypass TSPU 16KB throttling with combo",
			Family:      FamilyIncoming,
			Phase:       PhaseBaseline,
			Priority:    9,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   50,
					Incoming: config.IncomingConfig{
						Mode:      "fake",
						Min:       14,
						Max:       14,
						FakeTTL:   7,
						FakeCount: 5,
						Strategy:  "badsum",
					},
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
				},
			},
		},

		// 10. Incoming reset + combo (RST injection at 16KB threshold)
		{
			Name:        "incoming-reset-combo",
			Description: "Incoming RST injection at threshold to reset DPI byte counter",
			Family:      FamilyIncoming,
			Phase:       PhaseBaseline,
			Priority:    10,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   50,
					Incoming: config.IncomingConfig{
						Mode:      "reset",
						Min:       10,
						Max:       18,
						FakeTTL:   5,
						FakeCount: 3,
						Strategy:  "badsum",
					},
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
				},
			},
		},

		// 11. Combo + packet duplication (survives ISP packet drops)
		{
			Name:        "combo-duplicate",
			Description: "Combo fragmentation with packet duplication",
			Family:      FamilyCombo,
			Phase:       PhaseBaseline,
			Priority:    11,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   50,
					Duplicate: config.DuplicateConfig{
						Enabled: true,
						Count:   2,
					},
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
				},
			},
		},

		// 12. Disorder + desync ACK (for aggressive DPI like Meta/Instagram)
		{
			Name:        "disorder-aggressive",
			Description: "Disorder with desync ACK attack for aggressive DPI",
			Family:      FamilyDisorder,
			Phase:       PhaseBaseline,
			Priority:    12,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   50,
					DropSACK:       true,
					Desync: config.DesyncConfig{
						Mode:  "ack",
						TTL:   7,
						Count: 15,
					},
				},
				UDP: config.UDPConfig{
					Mode:           "fake",
					FakeSeqLength:  15,
					FakeLen:        64,
					FakingStrategy: "checksum",
					FilterQUIC:     "sni",
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
					Strategy:     "pastseq",
					SeqOffset:    1000000,
					SNISeqLength: 12,
					SNIType:      config.FakePayloadSTUN,
					TLSMod:       []string{"rnd", "dupsid"},
				},
			},
		},

		// 14. Multidisorder combo (fake per segment + reverse order)
		{
			Name:        "multidisorder-combo",
			Description: "Multidisorder: fake overlap before every segment with reverse order",
			Family:      FamilyCombo,
			Phase:       PhaseBaseline,
			Priority:    14,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   50,
				},
				UDP: udp,
				Fragmentation: config.FragmentationConfig{
					Strategy:          "combo",
					ReverseOrder:      true,
					MiddleSNI:         true,
					SNIPosition:       1,
					SeqOverlapPattern: []string{"0x16", "0x03", "0x03", "0x00", "0x00"},
					Combo: config.ComboFragConfig{
						FirstByteSplit:  true,
						ExtensionSplit:  true,
						ShuffleMode:     "reverse",
						FirstDelayMs:    30,
						JitterMaxUs:     1000,
						FakePerSegment:  true,
						FakePerSegCount: 3,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 11,
					SNIType:      config.FakePayloadSTUN,
					TLSMod:       []string{"rnd", "dupsid"},
					TCPMD5:       true,
				},
			},
		},

		// 15. Multidisorder disorder - fake per segment with inverted payload
		{
			Name:        "multidisorder-inverted",
			Description: "Multidisorder with inverted original payload for aggressive DPI",
			Family:      FamilyDisorder,
			Phase:       PhaseBaseline,
			Priority:    15,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
					Seg2DelayMax:   50,
					DropSACK:       true,
					Desync: config.DesyncConfig{
						Mode:  "rst",
						TTL:   5,
						Count: 10,
					},
				},
				UDP: udp,
				Fragmentation: config.FragmentationConfig{
					Strategy:          "disorder",
					ReverseOrder:      true,
					MiddleSNI:         true,
					SNIPosition:       1,
					SeqOverlapPattern: []string{"0x16", "0x03", "0x03", "0x00", "0x00"},
					Disorder: config.DisorderFragConfig{
						ShuffleMode:     "reverse",
						MinJitterUs:     500,
						MaxJitterUs:     2100,
						FakePerSegment:  true,
						FakePerSegCount: 3,
					},
				},
				Faking: config.FakingConfig{
					SNI:          true,
					Strategy:     "pastseq",
					SeqOffset:    10000,
					SNISeqLength: 11,
					SNIType:      config.FakePayloadInverted,
					TLSMod:       []string{"rnd", "dupsid"},
				},
			},
		},

		// 13. Full bypass - kitchen sink (all techniques combined)
		{
			Name:        "full-bypass",
			Description: "All TSPU bypass techniques combined: MD5 + PostRST + incoming + timestamp",
			Family:      FamilyNone,
			Phase:       PhaseBaseline,
			Priority:    13,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      30,
					Seg2DelayMax:   90,
					Desync: config.DesyncConfig{
						Mode:       "rst",
						TTL:        7,
						Count:      3,
						PostDesync: true,
					},
					Incoming: config.IncomingConfig{
						Mode:      "fake",
						Min:       14,
						Max:       14,
						FakeTTL:   7,
						FakeCount: 5,
						Strategy:  "badsum",
					},
				},
				UDP:           udp,
				Fragmentation: combo,
				Faking: config.FakingConfig{
					SNI:               true,
					Strategy:          "timestamp",
					SeqOffset:         10000,
					SNISeqLength:      1,
					SNIType:           config.FakePayloadSTUN,
					TCPMD5:            true,
					TimestampDecrease: 600000,
				},
			},
		},

		// 17. Fake only, no fragmentation. Every other preset that carries a fake
		// also fragments, and GetPhase2Presets builds the FamilyFakeSNI sweep on
		// baseConfig(), whose fragmentation strategy is combo. Against a DPI that
		// reassembles TCP well enough to see through any split, that shape is the
		// one thing discovery could not reach at any phase.
		{
			Name:        "fake-ttl6-md5",
			Description: "Low-TTL fake sized to the ClientHello and signed with TCP MD5, no fragmentation",
			Family:      FamilyFakeSNI,
			Phase:       PhaseBaseline,
			Priority:    17,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: udp,
				Fragmentation: config.FragmentationConfig{
					Strategy: config.ConfigNone,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          6,
					ApplyTTL:     true,
					Strategy:     "ttl",
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
					FakeLenMode:  "match",
					MD5OnFake:    true,
				},
			},
		},

		// 18. Same shape without MD5, for paths where the option is stripped.
		{
			Name:        "fake-ttl8",
			Description: "Low-TTL fake sized to the ClientHello, no fragmentation, no MD5",
			Family:      FamilyFakeSNI,
			Phase:       PhaseBaseline,
			Priority:    18,
			Config: config.SetConfig{
				TCP: config.TCPConfig{
					ConnBytesLimit: 19,
				},
				UDP: udp,
				Fragmentation: config.FragmentationConfig{
					Strategy: config.ConfigNone,
				},
				Faking: config.FakingConfig{
					SNI:          true,
					TTL:          8,
					ApplyTTL:     true,
					Strategy:     "ttl",
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
					FakeLenMode:  "match",
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
		FilterQUIC:     "sni",
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

		// Fake per segment (multidisorder) variations
		for _, count := range []int{1, 3, 5} {
			for _, mode := range []string{"reverse", "full"} {
				presets = append(presets, ConfigPreset{
					Name:     formatName("combo-fakeperseg%d-%s", count, mode),
					Family:   FamilyCombo,
					Phase:    PhaseOptimize,
					Priority: 200 + count,
					Config: withTCP(withFragmentation(base, config.FragmentationConfig{
						Strategy:          "combo",
						ReverseOrder:      true,
						MiddleSNI:         true,
						SNIPosition:       1,
						SeqOverlapPattern: []string{"0x16", "0x03", "0x03", "0x00", "0x00"},
						Combo: config.ComboFragConfig{
							FirstByteSplit:  true,
							ExtensionSplit:  true,
							ShuffleMode:     mode,
							FirstDelayMs:    30,
							JitterMaxUs:     1000,
							FakePerSegment:  true,
							FakePerSegCount: count,
						},
					}), config.TCPConfig{
						ConnBytesLimit: 19,
						Seg2Delay:      20,
					}),
				})
			}
		}

		// Sequence overlap length variations (prepends pattern bytes to first segment, shifts seq back)
		for _, n := range []int{64, 256, 681} {
			presets = append(presets, ConfigPreset{
				Name:     formatName("combo-seqovl%d", n),
				Family:   FamilyCombo,
				Phase:    PhaseOptimize,
				Priority: 300 + n,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy:          "combo",
					ReverseOrder:      true,
					MiddleSNI:         true,
					SNIPosition:       1,
					SeqOverlapPattern: []string{"0x16", "0x03", "0x03", "0x00", "0x00"},
					SeqOverlapLength:  n,
					Combo: config.ComboFragConfig{
						FirstByteSplit: true,
						ExtensionSplit: true,
						ShuffleMode:    "full",
						FirstDelayMs:   30,
						JitterMaxUs:    1000,
					},
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
				}),
			})
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

		// Fake per segment (multidisorder) variations
		for _, count := range []int{1, 3, 5} {
			for _, mode := range []string{"reverse", "full"} {
				presets = append(presets, ConfigPreset{
					Name:     formatName("disorder-fakeperseg%d-%s", count, mode),
					Family:   FamilyDisorder,
					Phase:    PhaseOptimize,
					Priority: 200 + count,
					Config: withTCP(withFragmentation(base, config.FragmentationConfig{
						Strategy:          "disorder",
						ReverseOrder:      true,
						MiddleSNI:         true,
						SeqOverlapPattern: []string{"0x16", "0x03", "0x03", "0x00", "0x00"},
						Disorder: config.DisorderFragConfig{
							ShuffleMode:     mode,
							MinJitterUs:     500,
							MaxJitterUs:     2100,
							FakePerSegment:  true,
							FakePerSegCount: count,
						},
					}), config.TCPConfig{
						ConnBytesLimit: 19,
						Seg2Delay:      20,
						DropSACK:       true,
					}),
				})
			}
		}

		// Sequence overlap length variations (prepends pattern bytes to second segment, shifts seq back)
		// SNIPosition is set explicitly large enough that the second segment starts past seqovlLength.
		seqovlDisorder := []struct {
			length int
			pos    int
		}{
			{128, 256},
			{256, 512},
		}
		for _, v := range seqovlDisorder {
			presets = append(presets, ConfigPreset{
				Name:     formatName("disorder-seqovl%d-pos%d", v.length, v.pos),
				Family:   FamilyDisorder,
				Phase:    PhaseOptimize,
				Priority: 300 + v.length,
				Config: withTCP(withFragmentation(base, config.FragmentationConfig{
					Strategy:          "disorder",
					ReverseOrder:      true,
					SNIPosition:       v.pos,
					SeqOverlapPattern: []string{"0x16", "0x03", "0x03", "0x00", "0x00"},
					SeqOverlapLength:  v.length,
					Disorder: config.DisorderFragConfig{
						ShuffleMode: "full",
						MinJitterUs: 500,
						MaxJitterUs: 2100,
					},
				}), config.TCPConfig{
					ConnBytesLimit: 19,
					Seg2Delay:      20,
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
					ApplyTTL:     true,
					Strategy:     "ttl",
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
					FakeLenMode:  "match",
				}),
			})

			presets = append(presets, ConfigPreset{
				Name:     formatName("fake-ttl%d-md5", ttl),
				Family:   FamilyFakeSNI,
				Phase:    PhaseOptimize,
				Priority: int(ttl),
				Config: withFaking(base, config.FakingConfig{
					SNI:          true,
					TTL:          ttl,
					ApplyTTL:     true,
					Strategy:     "ttl",
					SNISeqLength: 1,
					SNIType:      config.FakePayloadSTUN,
					FakeLenMode:  "match",
					MD5OnFake:    true,
				}),
			})

			// base carries combo fragmentation, so without this the whole sweep
			// only ever tests a TTL fake paired with a split.
			presets = append(presets, ConfigPreset{
				Name:     formatName("fake-ttl%d-only", ttl),
				Family:   FamilyFakeSNI,
				Phase:    PhaseOptimize,
				Priority: int(ttl),
				Config: withFragmentation(
					withFaking(base, config.FakingConfig{
						SNI:          true,
						TTL:          ttl,
						ApplyTTL:     true,
						Strategy:     "ttl",
						SNISeqLength: 1,
						SNIType:      config.FakePayloadSTUN,
						FakeLenMode:  "match",
						MD5OnFake:    true,
					}),
					config.FragmentationConfig{Strategy: config.ConfigNone},
				),
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
					SNIType:      config.FakePayloadSTUN,
				}),
			})
		}

		// Strategy variations
		strategies := []string{"ttl", "pastseq", "randseq", "tcp_check", "md5sum", "timestamp"}
		for i, strat := range strategies {
			cfg := config.FakingConfig{
				SNI:          true,
				TTL:          7,
				Strategy:     strat,
				SeqOffset:    10000,
				SNISeqLength: 1,
				SNIType:      config.FakePayloadSTUN,
			}
			// Set default timestamp decrease for timestamp strategy
			if strat == "timestamp" {
				cfg.TimestampDecrease = 600000
			}
			presets = append(presets, ConfigPreset{
				Name:     formatName("fake-%s", strat),
				Family:   FamilyFakeSNI,
				Phase:    PhaseOptimize,
				Priority: i + 20,
				Config:   withFaking(base, cfg),
			})
		}

		// Payload type variations
		payloadTypes := []struct {
			name    string
			sniType int
		}{
			{"payloadStun", config.FakePayloadSTUN},
			{"payload1", config.FakePayloadDefault1},
			{"payload2", config.FakePayloadDefault2},
			{"payloadRand", config.FakePayloadRandom},
			{"payloadZero", config.FakePayloadZero},
			{"payloadInverted", config.FakePayloadInverted},
		}
		for _, pt := range payloadTypes {
			presets = append(presets, ConfigPreset{
				Name:         formatName("fake-%s", pt.name),
				Family:       FamilyFakeSNI,
				Phase:        PhaseOptimize,
				Priority:     30 + pt.sniType,
				FixedPayload: true,
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
				SNIType:      config.FakePayloadSTUN,
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

func comboFrag() config.FragmentationConfig {
	return config.FragmentationConfig{
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
	}
}

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
			FilterQUIC:     "sni",
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
