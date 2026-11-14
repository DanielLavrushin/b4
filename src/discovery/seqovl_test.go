package discovery

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func allDiscoveryPresets() []ConfigPreset {
	families := []StrategyFamily{
		FamilyTCPFrag, FamilyTLSRec, FamilyOOB, FamilyIPFrag, FamilyFakeSNI, FamilySACK, FamilySynFake,
		FamilyDesync, FamilyWindow, FamilyDelay, FamilyMutation, FamilyDisorder, FamilyOverlap,
		FamilyExtSplit, FamilyFirstByte, FamilyCombo, FamilyHybrid, FamilyIncoming,
	}
	presets := GetPhase1Presets()
	best := map[StrategyFamily]ConfigPreset{}
	for _, family := range families {
		optimized := GetPhase2Presets(family)
		if len(optimized) > 0 {
			best[family] = optimized[0]
		}
		presets = append(presets, optimized...)
	}
	return append(presets, GetCombinationPresets(families, best)...)
}

func patternBytes(t *testing.T, pattern []string) []byte {
	t.Helper()
	out := make([]byte, len(pattern))
	for i, s := range pattern {
		b, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("pattern byte %q: %v", s, err)
		}
		out[i] = byte(b)
	}
	return out
}

func TestTestConfigsCarryTheSeqOverlapBytesOfTheirPreset(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.Domain = "a.example"
	ds.cfg = &config.Config{ConfigPath: filepath.Join(t.TempDir(), "b4.json")}

	presets := allDiscoveryPresets()
	for _, name := range []string{"combo-seqovl64", "disorder-seqovl128-pos256", "combo-fakeperseg1-reverse", "disorder-tlsovl2"} {
		if len(presetByName(t, presets, name).Config.Fragmentation.SeqOverlapPattern) == 0 {
			t.Fatalf("%s must carry a sequence-overlap pattern", name)
		}
	}

	checked := 0
	for _, preset := range presets {
		pattern := preset.Config.Fragmentation.SeqOverlapPattern
		if len(pattern) == 0 {
			continue
		}
		checked++
		want := patternBytes(t, pattern)
		builds := map[string]*config.Config{
			"single": ds.buildTestConfig(preset),
			"multi":  ds.buildTestConfigMulti(preset),
		}
		for kind, cfg := range builds {
			if got := cfg.Sets[0].Fragmentation.SeqOverlapBytes; !bytes.Equal(got, want) {
				t.Errorf("%s %s: the test set must send the pattern the saved set sends, got % x, want % x", preset.Name, kind, got, want)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no preset carries a sequence-overlap pattern")
	}
}
