package convert

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var sharedConfigs = []struct {
	name string
	line string
}{
	{
		"gosuslugiEscalation",
		"-Ku -a1 -An -o1 -At,r,s -f-1 -At,r,s -d1:11+sm -S -At,r,s " +
			"-n https://www.gosuslugi.ru/ -Qr -f1 -d1:11+sm -s1:11+sm -S",
	},
	{
		"vkLadderTwoProfiles",
		"-Ku -a3 -An -Kt,h -n vk.com -d1 -d3+s -s6+s -d9+s -s12+s -d15+s -s20+s " +
			"-d25+s -s30+s -d35+s -r1+s -S -Mh,d -As -Kt,h -n vk.com -d1 -d3+s -s6+s " +
			"-d9+s -s12+s -d15+s -s20+s -d25+s -s30+s -d35+s -S -Mh,d",
	},
	{
		"sevenProfileEscalation",
		"-Ku -a1 -An -d1 -s0+s -d3+s -s6+s -d9+s -s12+s -d15+s -s20+s -d25+s -s30+s " +
			"-d35+s -At,r,s -s1 -q1 -At,r,s -s5 -o25000+s -At,r,s -o1 -d1 -r1+s -t10 " +
			"-b1500 -s0+s -d3+s -At,r,s -f-1 -r1+s -At,r,s -s1 -o1+s -s-1",
	},
	{
		"inlineFakePayloadUDP",
		`-Ku -l':\x16\x03\x01\x02\x87\x01\x00\x02\x83\x03\x03\x5f\x15\x63\xcb\x06' ` +
			`-a1 -An -s1 -q1 -Y -At -f-1 -r1+s -As`,
	},
}

func TestCorpus_EveryRecognizedOptionIsReported(t *testing.T) {
	for _, tc := range sharedConfigs {
		t.Run(tc.name, func(t *testing.T) {
			res := analyze(t, tc.line)
			reported := map[string]bool{}
			for _, n := range res.Notes {
				reported[n.Token] = true
			}
			all, err := loadSpecs()
			if err != nil {
				t.Fatal(err)
			}
			table := all[res.Tool].tableFor(res.Version)
			for _, tok := range getoptLong(res.Argv, table, false) {
				if tok.Spec.Target == "_.ignore" {
					continue
				}
				if !reported[tok.Raw] {
					t.Fatalf("option %q produced no entry in the report", tok.Raw)
				}
			}
		})
	}
}

func TestCorpus_NoUnaccountedOptions(t *testing.T) {
	for _, tc := range sharedConfigs {
		t.Run(tc.name, func(t *testing.T) {
			res := analyze(t, tc.line)
			for _, n := range res.Notes {
				if n.Reason == "unaccountedOption" {
					t.Fatalf("%q fell through every emit rule", n.Token)
				}
				if n.Status == StatusUnknown || n.Status == StatusInvalid {
					t.Fatalf("%q was not understood: %s/%s", n.Token, n.Status, n.Reason)
				}
			}
		})
	}
}

func TestCorpus_EscalationChainsAreAcyclic(t *testing.T) {
	for _, tc := range sharedConfigs {
		t.Run(tc.name, func(t *testing.T) {
			res := analyze(t, tc.line)
			byID := map[string]int{}
			for i, s := range res.Sets {
				byID[s.Id] = i
			}
			for i, s := range res.Sets {
				if s.Escalate.To == "" {
					continue
				}
				target, ok := byID[s.Escalate.To]
				if !ok {
					t.Fatalf("set %d escalates to an unknown id %q", i, s.Escalate.To)
				}
				if target <= i {
					t.Fatalf("set %d escalates backwards to %d", i, target)
				}
				if !res.Sets[target].Enabled {
					t.Fatalf("set %d escalates to a disabled set %d", i, target)
				}
			}
		})
	}
}

func TestAnalyze_UDPProfileStillReportsFakeOptions(t *testing.T) {
	res := analyze(t, `-Ku -l':abc' -a1`)
	n := noteFor(t, res, "-l:abc")
	if n.Status != StatusDegenerate || n.Reason != "requiresFake" {
		t.Fatalf("a fake payload in a UDP-only profile must be reported, got %+v", n)
	}
}

func TestAnalyze_ComboHonoursFirstByteSplit(t *testing.T) {
	res := analyze(t, "-d1 -s3+s")
	set := res.Sets[0]
	if set.Fragmentation.Strategy != "combo" {
		t.Fatalf("strategy: got %q", set.Fragmentation.Strategy)
	}
	if !set.Fragmentation.Combo.FirstByteSplit {
		t.Fatal("offset 1 should enable combo.first_byte_split")
	}
	n := noteFor(t, res, "-d1")
	if n.Status != StatusMapped || n.Reason != "firstByteMapped" {
		t.Fatalf("offset 1 is representable in combo, got %+v", n)
	}
	if !hasField(n, "fragmentation.combo.first_byte_split") {
		t.Fatalf("note should name the field it set, got %+v", n)
	}
}

func TestAnalyze_SplitLadderIsSummarised(t *testing.T) {
	res := analyze(t, "-s1 -d3+s -s6+s -d9+s -s12+s -d15+s")
	var found *Note
	for i := range res.Notes {
		if res.Notes[i].Reason == "splitPointsCollapsed" {
			found = &res.Notes[i]
		}
	}
	if found == nil {
		t.Fatal("a ladder of split points should be summarised once for the profile")
	}
	if found.Params["count"] != 6 {
		t.Fatalf("count: got %v, want 6", found.Params["count"])
	}
}

func TestAnalyze_ProfileWithoutDesyncIsReported(t *testing.T) {
	res := analyze(t, "-s1 -At -f-1 -As")
	if len(res.Sets) != 3 {
		t.Fatalf("expected 3 sets, got %d", len(res.Sets))
	}
	last := res.Sets[2]
	if last.Fragmentation.Strategy != "none" || last.Faking.SNI {
		t.Fatalf("a trailing -A with no options is a pass-through set, got %+v", last.Fragmentation)
	}
	var found bool
	for _, n := range res.Notes {
		if n.Reason == "profileWithoutDesync" && n.Profile == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("an empty set must be explained rather than left as a mystery")
	}
}

func TestAnalyze_CompetingFixedPositions(t *testing.T) {
	res := analyze(t, "-s5 -s7")
	if got := res.Sets[0].Fragmentation.SNIPosition; got != 5 {
		t.Fatalf("sni_position: got %d, want 5", got)
	}
	if n := noteFor(t, res, "-s5"); n.Status != StatusMapped {
		t.Fatalf("-s5: got %+v", n)
	}
	n := noteFor(t, res, "-s7")
	if n.Status != StatusApproximated || n.Reason != "fixedPositionIgnored" {
		t.Fatalf("a second fixed position cannot be kept, got %+v", n)
	}
}

func TestAnalyze_PositionBeyondRangeIsClamped(t *testing.T) {
	res := analyze(t, "-s25000")
	if got := res.Sets[0].Fragmentation.SNIPosition; got != maxSNIPosition {
		t.Fatalf("sni_position: got %d, want %d", got, maxSNIPosition)
	}
	n := noteFor(t, res, "-s25000")
	if n.Status != StatusApproximated || n.Reason != "positionClamped" {
		t.Fatalf("got %+v", n)
	}
}

func TestAnalyze_DroppedOOBDoesNotLeaveItsByteBehind(t *testing.T) {
	res := analyze(t, "-s5 -o25000+s")
	set := res.Sets[0]
	if set.Fragmentation.Strategy == "oob" {
		t.Fatal("a plain split should win over oob here")
	}
	if set.Fragmentation.OOBChar != config.DefaultSetConfig.Fragmentation.OOBChar {
		t.Fatalf("oob_char should stay at the b4 default when oob was dropped, got %d",
			set.Fragmentation.OOBChar)
	}
}
