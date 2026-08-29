package convert

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestAnalyze_Splitting(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		strategy    string
		middleSNI   bool
		sniPosition int
	}{
		{"sniStart", "-s1+s", "tcp", true, 0},
		{"sniMiddle", "-s0+sm", "tcp", true, 0},
		{"fixedPosition", "-s5", "tcp", false, 5},
		{"firstByte", "-s1", "tcp", false, 1},
		{"disorder", "-d0+sm", "disorder", true, 0},
		{"splitAndDisorder", "-s1 -d0+sm", "combo", true, 0},
		{"noSplit", "-t8", "none", true, 1},
		{"negativeOffset", "-s-1", "tcp", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			set := res.Sets[0]
			if set.Fragmentation.Strategy != tt.strategy {
				t.Fatalf("strategy: got %q, want %q", set.Fragmentation.Strategy, tt.strategy)
			}
			if tt.strategy == config.ConfigNone {
				return
			}
			if set.Fragmentation.MiddleSNI != tt.middleSNI {
				t.Fatalf("middle_sni: got %v, want %v", set.Fragmentation.MiddleSNI, tt.middleSNI)
			}
			if set.Fragmentation.SNIPosition != tt.sniPosition {
				t.Fatalf("sni_position: got %d, want %d", set.Fragmentation.SNIPosition, tt.sniPosition)
			}
		})
	}
}

func TestAnalyze_OOBUsesByedpiDefaultByte(t *testing.T) {
	res := analyze(t, "-o1")
	set := res.Sets[0]
	if set.Fragmentation.Strategy != "oob" {
		t.Fatalf("strategy: got %q", set.Fragmentation.Strategy)
	}
	if set.Fragmentation.OOBPosition != 1 {
		t.Fatalf("oob_position: got %d", set.Fragmentation.OOBPosition)
	}
	if set.Fragmentation.OOBChar != 'a' {
		t.Fatalf("oob_char: got %d, want %d (byedpi default), b4 default is %d",
			set.Fragmentation.OOBChar, 'a', config.DefaultSetConfig.Fragmentation.OOBChar)
	}
}

func TestAnalyze_OOBByteOverride(t *testing.T) {
	res := analyze(t, "-o1 -eb")
	if got := res.Sets[0].Fragmentation.OOBChar; got != 'b' {
		t.Fatalf("oob_char: got %d, want %d", got, 'b')
	}
}

func TestAnalyze_TLSRecord(t *testing.T) {
	res := analyze(t, "-r2")
	set := res.Sets[0]
	if set.Fragmentation.Strategy != "tls" || set.Fragmentation.TLSRecordPosition != 2 {
		t.Fatalf("got strategy=%q pos=%d", set.Fragmentation.Strategy, set.Fragmentation.TLSRecordPosition)
	}
}

func TestAnalyze_FakeDefaults(t *testing.T) {
	res := analyze(t, "-f-1")
	set := res.Sets[0]
	if !set.Faking.SNI {
		t.Fatal("expected faking.sni to be enabled")
	}
	if set.Faking.Strategy != "ttl" || !set.Faking.ApplyTTL {
		t.Fatalf("got strategy=%q apply_ttl=%v", set.Faking.Strategy, set.Faking.ApplyTTL)
	}
	if set.Faking.TTL != byedpiFakeTTL(t) {
		t.Fatalf("ttl: got %d, want %d", set.Faking.TTL, byedpiFakeTTL(t))
	}
}

func TestAnalyze_FakeSNIBecomesGeneratedPayload(t *testing.T) {
	res := analyze(t, "-f-1 -Qr -n https://www.gosuslugi.ru/")
	set := res.Sets[0]
	if set.Faking.SNIType != config.FakePayloadDomain {
		t.Fatalf("sni_type: got %d, want %d", set.Faking.SNIType, config.FakePayloadDomain)
	}
	if set.Faking.PayloadDomain != "www.gosuslugi.ru" {
		t.Fatalf("payload_domain: got %q", set.Faking.PayloadDomain)
	}
	if len(set.Faking.TLSMod) != 1 || set.Faking.TLSMod[0] != "rnd" {
		t.Fatalf("tls_mod: got %v", set.Faking.TLSMod)
	}
}

func TestAnalyze_MD5SigWithoutFakeIsDegenerate(t *testing.T) {
	res := analyze(t, "-d0+sm -S")
	n := noteFor(t, res, "-S")
	if n.Status != StatusDegenerate || n.Reason != "requiresFake" {
		t.Fatalf("got %+v", n)
	}
}

func TestAnalyze_RepeatsWithoutSkipIsDegenerate(t *testing.T) {
	res := analyze(t, "-d1:11+sm")
	n := noteFor(t, res, "-d1:11+sm")
	if n.Status != StatusDegenerate || n.Reason != "repeatsWithoutSkip" {
		t.Fatalf("got %+v", n)
	}
}

func TestAnalyze_RepeatsWithSkipIsApproximated(t *testing.T) {
	res := analyze(t, "-s1:3:5")
	n := noteFor(t, res, "-s1:3:5")
	if n.Status != StatusApproximated || n.Reason != "repeatsUnsupported" {
		t.Fatalf("got %+v", n)
	}
}

func TestAnalyze_HostsInlineBecomeTargets(t *testing.T) {
	res := analyze(t, "-H:youtube.com,googlevideo.com -s1+s")
	set := res.Sets[0]
	if len(set.Targets.SNIDomains) != 2 {
		t.Fatalf("sni_domains: got %v", set.Targets.SNIDomains)
	}
	if !set.Enabled {
		t.Fatal("a set with targets should be enabled")
	}
}

func TestAnalyze_HostsFileIsUnresolved(t *testing.T) {
	res := analyze(t, "-H /etc/byedpi/hosts.txt -s1+s")
	if len(res.Unresolved) != 1 || res.Unresolved[0].Kind != "hostlist" {
		t.Fatalf("unresolved: got %+v", res.Unresolved)
	}
	if res.Sets[0].Enabled {
		t.Fatal("a set with no resolved targets must stay disabled")
	}
}

func TestAnalyze_ProxyRuntimeIsNotApplicable(t *testing.T) {
	res := analyze(t, "-i 0.0.0.0 -p 1080 -c 512 -s1+s")
	for _, tok := range []string{"-i 0.0.0.0", "-p 1080", "-c 512"} {
		n := noteFor(t, res, tok)
		if n.Status != StatusNotApplicable {
			t.Fatalf("%s: got %+v", tok, n)
		}
	}
	if res.Fidelity.NotApplicable != 3 {
		t.Fatalf("not_applicable: got %d", res.Fidelity.NotApplicable)
	}
	bare := analyze(t, "-s1+s")
	if res.Fidelity.Score != bare.Fidelity.Score {
		t.Fatalf("proxy plumbing must not change the score: got %d with, %d without",
			res.Fidelity.Score, bare.Fidelity.Score)
	}
}

func TestAnalyze_UnsupportedOptions(t *testing.T) {
	tests := []struct {
		token  string
		line   string
		reason string
	}{
		{"-Mh,d,r", "-f1 -Mh,d,r", "httpTamper"},
		{"-O5", "-f1 -O5", "fakeOffsetUnsupported"},
		{"-m3", "-f1 -m3", "noEquivalent"},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			res := analyze(t, tt.line)
			n := noteFor(t, res, tt.token)
			if n.Status != StatusUnsupported || n.Reason != tt.reason {
				t.Fatalf("got %+v", n)
			}
		})
	}
}

func TestAnalyze_UDPProfile(t *testing.T) {
	res := analyze(t, "-Ku -a1")
	set := res.Sets[0]
	if set.UDP.Mode != "fake" || set.UDP.FakeSeqLength != 1 || set.UDP.FilterQUIC != "all" {
		t.Fatalf("udp: got mode=%q len=%d quic=%q", set.UDP.Mode, set.UDP.FakeSeqLength, set.UDP.FilterQUIC)
	}
	if set.Fragmentation.Strategy != config.ConfigNone || set.Faking.SNI {
		t.Fatalf("a UDP-only profile must not carry TCP strategies: %q / %v",
			set.Fragmentation.Strategy, set.Faking.SNI)
	}
}

func TestAnalyze_ProtoFilterBecomesPortFilter(t *testing.T) {
	res := analyze(t, "-Kt,h -H:example.com -s1+s")
	if got := res.Sets[0].TCP.DPortFilter; got != "80,443" {
		t.Fatalf("dport_filter: got %q", got)
	}
}

func TestAnalyze_PortFilter(t *testing.T) {
	res := analyze(t, "-V443-444 -H:example.com -s1+s")
	if got := res.Sets[0].TCP.DPortFilter; got != "443-444" {
		t.Fatalf("dport_filter: got %q", got)
	}
}

func TestAnalyze_EscalationChain(t *testing.T) {
	res := analyze(t, "-H:example.com -s1+s -At -d0+sm -At -f-1")
	if len(res.Sets) != 3 {
		t.Fatalf("expected 3 sets, got %d", len(res.Sets))
	}
	if res.Sets[0].Escalate.To != res.Sets[1].Id {
		t.Fatalf("set 0 should escalate to set 1, got %q", res.Sets[0].Escalate.To)
	}
	if res.Sets[1].Escalate.To != res.Sets[2].Id {
		t.Fatalf("set 1 should escalate to set 2, got %q", res.Sets[1].Escalate.To)
	}
	if res.Sets[2].Escalate.To != "" {
		t.Fatalf("last set must not escalate, got %q", res.Sets[2].Escalate.To)
	}
	for i := 1; i < 3; i++ {
		if !res.Sets[i].Enabled {
			t.Fatalf("escalation target %d must be enabled", i)
		}
		if res.Sets[i].TCP.DPortFilter != "" {
			t.Fatalf("escalation target %d must not match on ports alone", i)
		}
	}
}

func TestAnalyze_AutoNoneIsNotAnEscalation(t *testing.T) {
	res := analyze(t, "-Ku -a1 -An -s1+s")
	if res.Sets[0].Escalate.To != "" {
		t.Fatalf("-An must not create an escalation link, got %q", res.Sets[0].Escalate.To)
	}
	n := noteFor(t, res, "-An")
	if n.Status != StatusMapped || n.Reason != "autoNoneEntrySet" {
		t.Fatalf("got %+v", n)
	}
}

func TestAnalyze_UDPOnlyProfileIsFoldedIntoTheEntrySet(t *testing.T) {
	res := analyze(t, "-Ku -a1 -An -s1+s -At -d0+sm")

	if len(res.Sets) != 2 {
		t.Fatalf("the UDP profile should not become a set of its own, got %d sets", len(res.Sets))
	}
	entry := res.Sets[0]
	if entry.Fragmentation.Strategy != "tcp" {
		t.Fatalf("entry set lost its TCP strategy: %q", entry.Fragmentation.Strategy)
	}
	if entry.UDP.Mode != "fake" || entry.UDP.FakeSeqLength != 1 || entry.UDP.FilterQUIC != "all" {
		t.Fatalf("entry set did not inherit the UDP handling: %+v", entry.UDP)
	}
	n := noteFor(t, res, "-Ku")
	if n.Reason != "udpFoldedIntoSet" {
		t.Fatalf("got %+v", n)
	}
}

func TestAnalyze_NoEntrySetIsShadowedByAnother(t *testing.T) {
	res, err := Analyze("-Ku -a1 -An -s1+s -At -d0+sm", Options{Domains: []string{"youtube.com"}})
	if err != nil {
		t.Fatal(err)
	}
	claimed := map[string]int{}
	for _, s := range res.Sets {
		if !s.Enabled {
			continue
		}
		for _, d := range s.Targets.SNIDomains {
			claimed[d]++
		}
	}
	for domain, n := range claimed {
		if n > 1 {
			t.Fatalf("%q is claimed by %d enabled sets; b4 applies only the first and ignores the rest", domain, n)
		}
	}
}

func TestAnalyze_UDPOnlyProfileSurvivesWithoutACarrier(t *testing.T) {
	res := analyze(t, "-Ku -a1")
	if len(res.Sets) != 1 {
		t.Fatalf("expected the UDP profile to stay as its own set, got %d", len(res.Sets))
	}
	if res.Sets[0].UDP.FakeSeqLength != 1 {
		t.Fatalf("udp: got %+v", res.Sets[0].UDP)
	}
}

func TestAnalyze_UDPProfileWithOwnHostsIsNotFolded(t *testing.T) {
	res := analyze(t, "-Ku -H:quic.example.com -a1 -An -H:www.example.com -s1+s")
	if len(res.Sets) != 2 {
		t.Fatalf("a UDP profile with its own host list is a separate set, got %d", len(res.Sets))
	}
}

func TestAnalyze_ExplicitVersionOverride(t *testing.T) {
	res, err := Analyze("-n example.com -f1", Options{Tool: "byedpi", Version: "0.13"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "0.13" {
		t.Fatalf("version: got %q", res.Version)
	}
	if res.Sets[0].Faking.PayloadDomain != "example.com" {
		t.Fatalf("payload_domain: got %q", res.Sets[0].Faking.PayloadDomain)
	}
}

func TestAnalyze_UserReportedLine(t *testing.T) {
	line := "-Ku -a1 -An -o1 -At,r,s -f-1 -At,r,s -d1:11+sm -S -At,r,s " +
		"-n https://www.gosuslugi.ru/ -Qr -f1 -d1:11+sm -s1:11+sm -S"
	res := analyze(t, line)

	if res.Tool != "byedpi" {
		t.Fatalf("tool: got %q", res.Tool)
	}
	if res.Version != "0.17" {
		t.Fatalf("version: got %q, want 0.17 (uses -Q and pos:repeats syntax)", res.Version)
	}
	if res.VersionInferred {
		t.Fatal("version should be detected from markers, not guessed")
	}
	if len(res.Sets) != 4 {
		t.Fatalf("expected 4 sets, got %d", len(res.Sets))
	}

	if res.Sets[0].UDP.FakeSeqLength != 1 {
		t.Fatalf("set 0 should carry the folded UDP handling: got %d", res.Sets[0].UDP.FakeSeqLength)
	}
	if res.Sets[0].Fragmentation.Strategy != "oob" || res.Sets[0].Fragmentation.OOBPosition != 1 {
		t.Fatalf("set 0: got %+v", res.Sets[0].Fragmentation)
	}
	if res.Sets[0].Escalate.To != res.Sets[1].Id ||
		res.Sets[1].Escalate.To != res.Sets[2].Id ||
		res.Sets[2].Escalate.To != res.Sets[3].Id {
		t.Fatal("expected sets 0 -> 1 -> 2 -> 3 escalation chain")
	}
	if res.Sets[3].Faking.PayloadDomain != "www.gosuslugi.ru" || !res.Sets[3].Faking.MD5OnFake {
		t.Fatalf("set 3 faking: got %+v", res.Sets[3].Faking)
	}
	if res.Sets[3].Fragmentation.Strategy != "combo" {
		t.Fatalf("set 3 strategy: got %q", res.Sets[3].Fragmentation.Strategy)
	}
	if !res.Sets[3].Fragmentation.Combo.DecoyEnabled {
		t.Fatal("a combo profile that also carries -f should enable the decoy")
	}

	n := noteFor(t, res, "-n https://www.gosuslugi.ru/")
	if n.Status != StatusApproximated || n.Reason != "fakeSNINormalised" {
		t.Fatalf("URL passed to --fake-sni should be flagged, got %+v", n)
	}
	if !hasField(n, "faking.payload_domain") {
		t.Fatalf("note should name the field it set, got %+v", n)
	}

	var needsTargets bool
	for _, w := range res.Warnings {
		if w.Code == "needsTargets" {
			needsTargets = true
		}
	}
	if !needsTargets {
		t.Fatal("a byedpi line with no host filter must warn that targets are required")
	}
}

func TestParsePosV013_Valid(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		offset int
		anchor Anchor
		rel    Rel
	}{
		{"plain", "1", 1, AnchorAbs, RelStart},
		{"negative", "-1", -1, AnchorAbs, RelStart},
		{"hex", "0x10", 16, AnchorAbs, RelStart},
		{"sni", "2+s", 2, AnchorSNI, RelStart},
		{"host", "3+h", 3, AnchorHost, RelStart},
		{"end", "4+e", 4, AnchorPacket, RelEnd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parsePosV013(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Offset != tt.offset || p.Anchor != tt.anchor || p.Rel != tt.rel {
				t.Fatalf("got offset=%d anchor=%s rel=%s, want %d/%s/%s", p.Offset, p.Anchor, p.Rel, tt.offset, tt.anchor, tt.rel)
			}
		})
	}
}

func TestParsePosV013_Rejects(t *testing.T) {
	for _, in := range []string{"1:11+sm", "1+sm", "1+x", "abc", "1+", "1junk"} {
		t.Run(in, func(t *testing.T) {
			if _, err := parsePosV013(in); err == nil {
				t.Fatalf("expected %q to be rejected", in)
			}
		})
	}
}

func TestParsePosV017_Valid(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		offset  int
		repeats int
		skip    int
		anchor  Anchor
		rel     Rel
	}{
		{"plain", "1", 1, 0, 0, AnchorAbs, RelStart},
		{"negative", "-1", -1, 0, 0, AnchorAbs, RelStart},
		{"sniMid", "1:11+sm", 1, 11, 0, AnchorSNI, RelMid},
		{"repeatsSkip", "1:3:5", 1, 3, 5, AnchorAbs, RelStart},
		{"sniStart", "0+s", 0, 0, 0, AnchorSNI, RelStart},
		{"sniEnd", "0+se", 0, 0, 0, AnchorSNI, RelEnd},
		{"sniRand", "0+sr", 0, 0, 0, AnchorSNI, RelRand},
		{"hostMid", "2+hm", 2, 0, 0, AnchorHost, RelMid},
		{"packetMid", "0+nm", 0, 0, 0, AnchorPacket, RelMid},
		{"nullBase", "5+n", 5, 0, 0, AnchorAbs, RelStart},
		{"unknownSecondCharIgnored", "5+sX", 5, 0, 0, AnchorSNI, RelStart},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parsePosV017(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Offset != tt.offset || p.Repeats != tt.repeats || p.Skip != tt.skip || p.Anchor != tt.anchor || p.Rel != tt.rel {
				t.Fatalf("got %+v, want offset=%d repeats=%d skip=%d anchor=%s rel=%s",
					p, tt.offset, tt.repeats, tt.skip, tt.anchor, tt.rel)
			}
		})
	}
}

func TestParsePosV017_Rejects(t *testing.T) {
	for _, in := range []string{"1:0", "abc", "1+x", "1+"} {
		t.Run(in, func(t *testing.T) {
			if _, err := parsePosV017(in); err == nil {
				t.Fatalf("expected %q to be rejected", in)
			}
		})
	}
}

func TestGHostList_InlineVsFile(t *testing.T) {
	inline, err := gHostList(":a.com b.com,c.com", grammarCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inline.List) != 3 {
		t.Fatalf("got %v", inline.List)
	}
	file, err := gHostList("/etc/byedpi/hosts.txt", grammarCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if file.Ref != "/etc/byedpi/hosts.txt" {
		t.Fatalf("got ref %q", file.Ref)
	}
}

func TestGetoptLong_VersionScopedOptions(t *testing.T) {
	v13 := getoptLong([]string{"-Qr"}, testTable(t, "0.13"), false)
	if v13[0].Err != "unknown" {
		t.Fatalf("expected -Q to be unknown in 0.13, got %+v", v13[0])
	}
	v17 := getoptLong([]string{"-Qr"}, testTable(t, "0.17"), false)
	if v17[0].Key != "fake_tls_mod" {
		t.Fatalf("expected -Q to resolve in 0.17, got %+v", v17[0])
	}

	n13 := getoptLong([]string{"-n", "example.com"}, testTable(t, "0.13"), false)
	if n13[0].Key != "tls_sni" {
		t.Fatalf("expected -n to be tls_sni in 0.13, got %q", n13[0].Key)
	}
	n17 := getoptLong([]string{"-n", "example.com"}, testTable(t, "0.17"), false)
	if n17[0].Key != "fake_sni" {
		t.Fatalf("expected -n to be fake_sni in 0.17, got %q", n17[0].Key)
	}
}

func TestDetectVersion_Markers(t *testing.T) {
	all, err := loadSpecs()
	if err != nil {
		t.Fatal(err)
	}
	spec := all["byedpi"]
	tests := []struct {
		name     string
		argv     []string
		want     string
		detected bool
	}{
		{"fakeTLSMod", []string{"-Qr"}, "0.17", true},
		{"ipOpt", []string{"-k"}, "0.13", true},
		{"posRepeats", []string{"-d1:11+sm"}, "0.17", true},
		{"ambiguousFallsBackToDefault", []string{"-s1", "-f-1"}, "0.17", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detected := detectVersion(spec, tt.argv)
			if got != tt.want || detected != tt.detected {
				t.Fatalf("got (%s, %v), want (%s, %v)", got, detected, tt.want, tt.detected)
			}
		})
	}
}

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
	frag := res.Sets[0].Fragmentation
	if frag.SNIPosition != 5 || frag.SNIPositionMax != 7 {
		t.Fatalf("several fixed positions become the range b4 splits across, got %d/%d",
			frag.SNIPosition, frag.SNIPositionMax)
	}
	if n := noteFor(t, res, "-s5"); n.Status != StatusMapped {
		t.Fatalf("-s5: got %+v", n)
	}
	n := noteFor(t, res, "-s7")
	if n.Status != StatusApproximated || n.Reason != "positionInRange" {
		t.Fatalf("a position inside the emitted range is covered, got %+v", n)
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

func byedpiFakeTTL(t *testing.T) uint8 {
	t.Helper()
	all, err := loadSpecs()
	if err != nil {
		t.Fatal(err)
	}
	return uint8(all["byedpi"].Defaults.FakeTTL)
}

func TestByedpi_DefaultsComeFromTheRuleFile(t *testing.T) {
	all, err := loadSpecs()
	if err != nil {
		t.Fatal(err)
	}
	d := all["byedpi"].Defaults
	if d.FakeTTL != 8 || !d.FakeTTLForced || d.OOBByte != 'a' {
		t.Fatalf("byedpi defaults: got %+v", d)
	}
}

func TestByedpi_ADisorderLadderNeverLosesItsPosition(t *testing.T) {
	for _, line := range []string{"-s1 -q1 -Y", "-d1 -d3 -d10 -d20", "-s1 -d5"} {
		t.Run(line, func(t *testing.T) {
			frag := analyze(t, line).Sets[0].Fragmentation
			if !frag.MiddleSNI && frag.SNIPosition == 0 &&
				!(frag.Strategy == "combo" && (frag.Combo.FirstByteSplit || frag.Combo.ExtensionSplit)) {
				t.Fatalf("the set carries no split position at all: %+v", frag)
			}
		})
	}
}

func TestByedpi_AnAbsoluteLadderBecomesTheRangeB4SplitsAcross(t *testing.T) {
	res := analyze(t, "-s3 -s6 -s9 -s12")
	frag := res.Sets[0].Fragmentation
	if frag.Strategy != "tcp" || frag.SNIPosition != 3 || frag.SNIPositionMax != 12 {
		t.Fatalf("got strategy=%q %d/%d", frag.Strategy, frag.SNIPosition, frag.SNIPositionMax)
	}
	for _, tok := range []string{"-s6", "-s9", "-s12"} {
		if n := noteFor(t, res, tok); n.Reason != "positionInRange" {
			t.Errorf("%s falls inside the emitted range: %+v", tok, n)
		}
	}
}

func TestByedpi_TheSNIMiddleNeverSilentlyOverridesAMappedPosition(t *testing.T) {
	oob := analyze(t, "-o1").Sets[0].Fragmentation
	if oob.OOBPosition != 1 || oob.MiddleSNI {
		t.Errorf("an absolute OOB position is overridden by the SNI middle at runtime: %+v", oob)
	}
	tls := analyze(t, "-f-1 -r1").Sets[0].Fragmentation
	if tls.TLSRecordPosition != 1 || tls.MiddleSNI {
		t.Errorf("an absolute TLS record position is overridden by the SNI middle: %+v", tls)
	}
}

func TestByedpi_AnAnchoredPositionSaysSoInsteadOfClaimingAnOffset(t *testing.T) {
	res := analyze(t, "-f-1 -r1+s")
	if n := noteFor(t, res, "-r1+s"); n.Reason != "tlsRecAnchorApproximated" {
		t.Fatalf("got %+v", n)
	}
	if got := res.Sets[0].Fragmentation.TLSRecordPosition; got != 0 {
		t.Fatalf("an SNI-anchored position must not become a byte offset, got %d", got)
	}
}

func TestByedpi_AClampedOOBPositionIsReported(t *testing.T) {
	res := analyze(t, "-o25000")
	n := noteFor(t, res, "-o25000")
	if n.Status != StatusApproximated || n.Reason != "positionClamped" {
		t.Fatalf("got %+v", n)
	}
}

func TestByedpi_OptionsThatNeedAStrategyTheSetDoesNotHave(t *testing.T) {
	res := analyze(t, "-f2 -e1")
	if n := noteFor(t, res, "-e1"); n.Status != StatusDegenerate || n.Reason != "requiresOOB" {
		t.Fatalf("an OOB byte on a set that does no OOB split: %+v", n)
	}
	var lost bool
	for _, n := range res.Notes {
		if n.Reason == "fakeSplitBoundaryLost" {
			lost = true
		}
	}
	if !lost {
		t.Fatal("the fake position also splits the request upstream, and that loss must be reported")
	}
}

func TestByedpi_A013FakeSNIIsOnlyMappedWhenTheSetSendsFakes(t *testing.T) {
	res, err := Analyze("-n vk.com -d1 -d3+s", Options{Tool: "byedpi", Version: "0.13"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Sets[0].Faking.PayloadDomain; got != "" {
		t.Fatalf("no profile sends a fake, so no payload domain can be set, got %q", got)
	}
	if n := noteFor(t, res, "-n vk.com"); n.Status == StatusMapped {
		t.Fatalf("the note must not claim a mapping the set does not carry: %+v", n)
	}
}

func TestByedpi_AnEscalationToAnIdenticalSetIsRemoved(t *testing.T) {
	res := analyze(t, "-d1 -d3+s -r1+s -S -As -d1 -d3+s -S")
	if res.Sets[0].Escalate.To != "" {
		t.Fatal("both profiles convert to the same set, so the escalation link buys nothing")
	}
	var told bool
	for _, n := range res.Notes {
		if n.Reason == "escalationTargetIdentical" {
			told = true
		}
	}
	if !told {
		t.Fatal("removing the escalation link must be reported")
	}
}

func TestByedpi_AProfileWithNoHostListIsNamedAsTheCatchAll(t *testing.T) {
	res := analyze(t, `-H:"a.com" -s1 -An -d1 -d3+s -r1+s`)
	if !hasWarning(res, "catchAllProfile") {
		t.Fatalf("the group with no -H handled everything upstream and matches nothing here: %+v", res.Warnings)
	}
}
