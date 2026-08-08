package convert

import (
	"errors"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func analyze(t *testing.T, line string) *Result {
	t.Helper()
	res, err := Analyze(line, Options{})
	if err != nil {
		t.Fatalf("Analyze(%q): %v", line, err)
	}
	return res
}

func noteFor(t *testing.T, res *Result, token string) Note {
	t.Helper()
	for _, n := range res.Notes {
		if n.Token == token {
			return n
		}
	}
	t.Fatalf("no note for token %q; notes: %+v", token, res.Notes)
	return Note{}
}

func hasField(n Note, field string) bool {
	for _, f := range n.Fields {
		if f == field || strings.HasPrefix(f, field+"=") {
			return true
		}
	}
	return false
}

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
	if set.Faking.TTL != byedpiDefaultFakeTTL {
		t.Fatalf("ttl: got %d, want %d", set.Faking.TTL, byedpiDefaultFakeTTL)
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

func TestAnalyze_PerProfileDomains(t *testing.T) {
	res, err := Analyze("-An -s1+s -An -d0+sm", Options{
		Domains:        []string{"fallback.example"},
		ProfileDomains: map[int][]string{1: {"custom.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Sets[0].Targets.SNIDomains; len(got) != 1 || got[0] != "fallback.example" {
		t.Fatalf("profile 0 should use the shared domains, got %v", got)
	}
	if got := res.Sets[1].Targets.SNIDomains; len(got) != 1 || got[0] != "custom.example" {
		t.Fatalf("profile 1 should use its override, got %v", got)
	}
}

func TestAnalyze_PlanDescribesRoles(t *testing.T) {
	res := analyze(t, "-H:example.com -s1+s -At -d0+sm")
	if len(res.Plan) != 2 {
		t.Fatalf("expected a plan entry per set, got %d", len(res.Plan))
	}
	if res.Plan[0].Role != "entry" || !res.Plan[0].AcceptsTargets {
		t.Fatalf("plan[0]: got %+v", res.Plan[0])
	}
	if res.Plan[1].Role != "fallback" || res.Plan[1].AcceptsTargets {
		t.Fatalf("plan[1]: got %+v", res.Plan[1])
	}
	if res.Plan[1].FallbackFor != 0 {
		t.Fatalf("plan[1] should be the fallback for set 0, got %d", res.Plan[1].FallbackFor)
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

func TestAnalyze_DomainsOption(t *testing.T) {
	res, err := Analyze("-s1+s -At -d0+sm", Options{Domains: []string{"youtube.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sets[0].Targets.SNIDomains) != 1 {
		t.Fatalf("head set should get the supplied domains, got %v", res.Sets[0].Targets.SNIDomains)
	}
	if len(res.Sets[1].Targets.SNIDomains) != 0 {
		t.Fatalf("escalation set must stay target-less, got %v", res.Sets[1].Targets.SNIDomains)
	}
}

func TestAnalyze_Rejects(t *testing.T) {
	if _, err := Analyze("   ", Options{}); !errors.Is(err, ErrNothingToParse) {
		t.Fatalf("got %v, want ErrNothingToParse", err)
	}
	if _, err := Analyze("-s1", Options{Tool: "nosuchtool"}); err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
	zapret := "--filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld --new"
	if _, err := Analyze(zapret, Options{}); !errors.Is(err, ErrUnsupportedTool) {
		t.Fatalf("got %v, want ErrUnsupportedTool for a zapret command line", err)
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
