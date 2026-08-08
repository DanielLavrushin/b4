package convert

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var zapretConfigs = []struct {
	name string
	line string
}{
	{
		"perPortProfiles",
		"--filter-tcp=80 --dpi-desync=fake,multisplit --dpi-desync-split-pos=method+2 " +
			"--dpi-desync-fooling=md5sig --hostlist-domains=example.com --new " +
			"--filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld " +
			"--dpi-desync-fooling=badseq,md5sig --hostlist-domains=example.com --new " +
			"--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=6 --hostlist-domains=example.com",
	},
	{
		"envVarWithQUICAndDup",
		`NFQWS_OPT="--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11 ` +
			`--dpi-desync-fake-quic=/opt/zapret/files/fake/quic.bin --new --filter-tcp=443 ` +
			`--dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld ` +
			`--dpi-desync-fooling=badseq --dpi-desync-split-seqovl=1 --dup=2 ` +
			`--hostlist=/opt/zapret/ipset/zapret-hosts-user.txt"`,
	},
	{
		"legacyNamesAndSkip",
		"--dpi-desync=split2 --dpi-desync-split-pos=sniext --wssize=1:6 --hostcase " +
			"--new --skip --dpi-desync=rst",
	},
}

func TestZapretCorpus_EveryRecognizedOptionIsReported(t *testing.T) {
	for _, tc := range zapretConfigs {
		t.Run(tc.name, func(t *testing.T) {
			res := analyze(t, tc.line)
			if res.Tool != "zapret" {
				t.Fatalf("tool: got %q", res.Tool)
			}
			reported := map[string]bool{}
			for _, n := range res.Notes {
				reported[n.Token] = true
			}
			all, err := loadSpecs()
			if err != nil {
				t.Fatal(err)
			}
			table := all["zapret"].tableFor(res.Version)
			for _, tok := range getoptLong(res.Argv, table, true) {
				if tok.Spec.Target == "_.ignore" {
					continue
				}
				if !reported[tok.Raw] {
					t.Fatalf("option %q produced no entry in the report", tok.Raw)
				}
			}
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

func TestZapretCorpus_NoDomainIsClaimedTwice(t *testing.T) {
	for _, tc := range zapretConfigs {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Analyze(tc.line, Options{Domains: []string{"example.org"}})
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
					t.Fatalf("%q is claimed by %d enabled sets", domain, n)
				}
			}
		})
	}
}

func TestZapret_DesyncModes(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		strategy string
		faking   bool
		desync   string
	}{
		{"multisplit", "--dpi-desync=multisplit --dpi-desync-split-pos=1,midsld", "tcp", false, "off"},
		{"multidisorder", "--dpi-desync=multidisorder --dpi-desync-split-pos=1,midsld", "disorder", false, "off"},
		{"fakePlusSplit", "--dpi-desync=fake,multisplit --dpi-desync-split-pos=midsld", "tcp", true, "off"},
		{"fakedsplit", "--dpi-desync=fakedsplit --dpi-desync-split-pos=midsld", "tcp", true, "off"},
		{"fakeddisorder", "--dpi-desync=fakeddisorder --dpi-desync-split-pos=midsld", "disorder", true, "off"},
		{"legacySplit2", "--dpi-desync=split2 --dpi-desync-split-pos=midsld", "tcp", false, "off"},
		{"legacyDisorder2", "--dpi-desync=disorder2 --dpi-desync-split-pos=midsld", "disorder", false, "off"},
		{"legacyDisorder", "--dpi-desync=disorder --dpi-desync-split-pos=midsld", "disorder", true, "off"},
		{"rst", "--dpi-desync=rst", "none", false, "rst"},
		{"rstack", "--dpi-desync=rstack", "none", false, "rst"},
		{"ipfrag2", "--dpi-desync=ipfrag2", "ip", false, "off"},
		{"sniext", "--dpi-desync=multisplit --dpi-desync-split-pos=sniext", "extsplit", false, "off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			set := res.Sets[0]
			if set.Fragmentation.Strategy != tt.strategy {
				t.Fatalf("strategy: got %q, want %q", set.Fragmentation.Strategy, tt.strategy)
			}
			if set.Faking.SNI != tt.faking {
				t.Fatalf("faking.sni: got %v, want %v", set.Faking.SNI, tt.faking)
			}
			if set.TCP.Desync.Mode != tt.desync {
				t.Fatalf("desync.mode: got %q, want %q", set.TCP.Desync.Mode, tt.desync)
			}
		})
	}
}

func TestZapret_Fooling(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		strategy string
		md5      bool
	}{
		{"badsum", "--dpi-desync=fake --dpi-desync-fooling=badsum", "tcp_check", false},
		{"badseq", "--dpi-desync=fake --dpi-desync-fooling=badseq", "pastseq", false},
		{"ts", "--dpi-desync=fake --dpi-desync-fooling=ts", "timestamp", false},
		{"md5sig", "--dpi-desync=fake --dpi-desync-fooling=md5sig", "ttl", true},
		{"badseqPlusMd5", "--dpi-desync=fake --dpi-desync-fooling=badseq,md5sig", "pastseq", true},
		{"none", "--dpi-desync=fake --dpi-desync-fooling=none", "ttl", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			set := res.Sets[0]
			if set.Faking.Strategy != tt.strategy {
				t.Fatalf("faking.strategy: got %q, want %q", set.Faking.Strategy, tt.strategy)
			}
			if set.Faking.MD5OnFake != tt.md5 {
				t.Fatalf("md5_on_fake: got %v, want %v", set.Faking.MD5OnFake, tt.md5)
			}
		})
	}
}

func TestZapret_FoolingConflictIsReported(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fooling=badsum,badseq")
	n := noteFor(t, res, "--dpi-desync-fooling=badsum,badseq")
	if n.Status != StatusApproximated || n.Reason != "foolingPartial" {
		t.Fatalf("a set has one faking strategy, the conflict must be reported: %+v", n)
	}
	if n.Params["dropped"] != "badseq" {
		t.Fatalf("dropped: got %v", n.Params["dropped"])
	}
}

func TestZapret_IncrementsBecomeOffsets(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fooling=badseq --dpi-desync-badseq-increment=-20000")
	if got := res.Sets[0].Faking.SeqOffset; got != 20000 {
		t.Fatalf("seq_offset: got %d, want 20000", got)
	}
	res = analyze(t, "--dpi-desync=fake --dpi-desync-fooling=ts --dpi-desync-ts-increment=-900000")
	if got := res.Sets[0].Faking.TimestampDecrease; got != 900000 {
		t.Fatalf("timestamp_decrease: got %d, want 900000", got)
	}
}

func TestZapret_ExactMappings(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld "+
		"--dpi-desync-repeats=6 --dpi-desync-split-seqovl=3 --dup=4 "+
		"--filter-tcp=443 --hostlist-domains=a.com,b.com --ipset-ip=1.2.3.0/24 --filter-l3=ipv4")
	set := res.Sets[0]

	if set.Faking.SNISeqLength != 6 {
		t.Fatalf("repeats: got %d", set.Faking.SNISeqLength)
	}
	if set.Fragmentation.SeqOverlapLength != 3 || len(set.Fragmentation.SeqOverlapPattern) == 0 {
		t.Fatalf("seqovl: got %d / %v",
			set.Fragmentation.SeqOverlapLength, set.Fragmentation.SeqOverlapPattern)
	}
	if !set.TCP.Duplicate.Enabled || set.TCP.Duplicate.Count != 4 {
		t.Fatalf("duplicate: got %+v", set.TCP.Duplicate)
	}
	if set.TCP.DPortFilter != "443" {
		t.Fatalf("dport_filter: got %q", set.TCP.DPortFilter)
	}
	if strings.Join(set.Targets.SNIDomains, ",") != "a.com,b.com" {
		t.Fatalf("domains: got %v", set.Targets.SNIDomains)
	}
	if len(set.Targets.IPs) != 1 || set.Targets.IPs[0] != "1.2.3.0/24" {
		t.Fatalf("ips: got %v", set.Targets.IPs)
	}
	if set.Targets.IPVersion != "4" {
		t.Fatalf("ip_version: got %q", set.Targets.IPVersion)
	}
}

func TestZapret_UDPProfileFoldsIntoTCPSet(t *testing.T) {
	res := analyze(t, "--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11 --new "+
		"--filter-tcp=443 --dpi-desync=fake,multidisorder --hostlist-domains=example.com")

	if len(res.Sets) != 1 {
		t.Fatalf("the UDP-only profile should fold into the TCP set, got %d sets", len(res.Sets))
	}
	set := res.Sets[0]
	if set.TCP.DPortFilter != "443" || set.UDP.DPortFilter != "443" {
		t.Fatalf("ports: tcp=%q udp=%q", set.TCP.DPortFilter, set.UDP.DPortFilter)
	}
	if set.Fragmentation.Strategy != "disorder" {
		t.Fatalf("the TCP strategy must survive the fold: %q", set.Fragmentation.Strategy)
	}
	if set.UDP.Mode != "fake" || set.UDP.FakeSeqLength != 11 {
		t.Fatalf("udp: got mode=%q len=%d", set.UDP.Mode, set.UDP.FakeSeqLength)
	}
}

func TestZapret_QUICPayloadBecomesGeneratedInitial(t *testing.T) {
	res := analyze(t, "--filter-udp=443 --dpi-desync=fake --dpi-desync-fake-quic=/opt/zapret/quic.bin")
	if got := res.Sets[0].UDP.FakePayloadFile; got != config.FakePayloadAutoQUIC {
		t.Fatalf("fake_payload_file: got %q, want %q", got, config.FakePayloadAutoQUIC)
	}
}

func TestZapret_SkipDisablesTheProfile(t *testing.T) {
	res := analyze(t, "--dpi-desync=multisplit --hostlist-domains=a.com --new --skip "+
		"--dpi-desync=rst --hostlist-domains=b.com")
	if !res.Sets[0].Enabled {
		t.Fatal("the first profile should stay enabled")
	}
	if res.Sets[1].Enabled {
		t.Fatal("--skip must disable its profile")
	}
}

func TestZapret_SharedHostlistShadowingIsReported(t *testing.T) {
	res := analyze(t, "--filter-tcp=80 --dpi-desync=multisplit --hostlist-domains=example.com --new "+
		"--filter-tcp=443 --dpi-desync=multidisorder --hostlist-domains=example.com")

	if !res.Sets[0].Enabled {
		t.Fatal("the first set should stay enabled")
	}
	if res.Sets[1].Enabled {
		t.Fatal("a second set claiming the same domain would never be reached and must be disabled")
	}
	var found bool
	for _, n := range res.Notes {
		if n.Reason == "shadowedByEarlierSet" {
			found = true
			if n.Params["domain"] != "example.com" {
				t.Fatalf("note should name the contested domain: %+v", n)
			}
		}
	}
	if !found {
		t.Fatal("shadowing must be reported, not silent")
	}
	var warned bool
	for _, w := range res.Warnings {
		if w.Code == "shadowedSets" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("shadowing must raise a warning")
	}
}

func TestZapret_NewIsNotAnEscalation(t *testing.T) {
	res := analyze(t, "--dpi-desync=multisplit --hostlist-domains=a.com --new --dpi-desync=rst --hostlist-domains=b.com")
	if res.Sets[0].Escalate.To != "" {
		t.Fatalf("zapret profiles are alternatives, not a retry chain: %q", res.Sets[0].Escalate.To)
	}
	n := noteFor(t, res, "--new")
	if n.Status != StatusMapped || n.Reason != "newProfileAsSet" {
		t.Fatalf("got %+v", n)
	}
}

func TestZapret_UnsupportedFamilies(t *testing.T) {
	tests := []struct {
		token  string
		line   string
		reason string
	}{
		{"--hostcase", "--dpi-desync=multisplit --hostcase", "httpTamper"},
		{"--dpi-desync-autottl=-1:3-20", "--dpi-desync=fake --dpi-desync-autottl=-1:3-20", "autoTTLUnsupported"},
		{"--orig-ttl=5", "--dpi-desync=fake --orig-ttl=5", "origPacketTTL"},
		{"--dpi-desync-cutoff=n3", "--dpi-desync=fake --dpi-desync-cutoff=n3", "packetWindowUnsupported"},
		{"--ipset-exclude-ip=1.2.3.4", "--dpi-desync=fake --ipset-exclude-ip=1.2.3.4", "excludeListUnsupported"},
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

func TestZapret_DroppedDesyncModesAreReported(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake,udplen")
	n := noteFor(t, res, "--dpi-desync=fake,udplen")
	if n.Status != StatusUnsupported || n.Reason != "desyncModesDropped" {
		t.Fatalf("got %+v", n)
	}
	if n.Params["dropped"] != "udplen" {
		t.Fatalf("dropped: got %v", n.Params["dropped"])
	}
}

func TestZapret_NegatedPortFilterIsUnsupported(t *testing.T) {
	res := analyze(t, "--filter-tcp=~80 --dpi-desync=multisplit")
	n := noteFor(t, res, "--filter-tcp=~80")
	if n.Status != StatusUnsupported || n.Reason != "negatedPortFilter" {
		t.Fatalf("got %+v", n)
	}
}

func TestZapret_SplitPositionGrammar(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		anchor Anchor
		rel    Rel
		offset int
	}{
		{"absolute", "5", AnchorAbs, RelStart, 5},
		{"negative", "-10", AnchorAbs, RelStart, -10},
		{"host", "host", AnchorSNI, RelStart, 0},
		{"endhost", "endhost-2", AnchorSNI, RelEnd, -2},
		{"midsld", "midsld", AnchorSNI, RelMid, 0},
		{"sniext", "sniext+1", AnchorSNIExt, RelStart, 1},
		{"method", "method+2", AnchorHost, RelStart, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parseZapretPos(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if p.Anchor != tt.anchor || p.Rel != tt.rel || p.Offset != tt.offset {
				t.Fatalf("got %+v", p)
			}
		})
	}
	for _, bad := range []string{"0", "nosuch+1", ""} {
		if _, err := parseZapretPos(bad); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}

func TestZapret_ConfigFileFormExtracts(t *testing.T) {
	res := analyze(t, "# zapret config\nNFQWS_OPT_DESYNC=\"--dpi-desync=fake --dpi-desync-repeats=6\"\n")
	if res.Tool != "zapret" {
		t.Fatalf("tool: got %q", res.Tool)
	}
	if res.Sets[0].Faking.SNISeqLength != 6 {
		t.Fatalf("repeats: got %d", res.Sets[0].Faking.SNISeqLength)
	}
}

const sharedGroupConfig = `--filter-tcp=80 --hostlist=/opt/zapret/ipset/zapret-hosts-user.txt --hostlist-exclude=/opt/zapret/ipset/zapret-hosts-user-exclude.txt --ipset-exclude=/opt/zapret/ipset/zapret-ip-exclude.txt --dpi-desync=fake,split2 --dpi-desync-autottl=2 --dpi-desync-fooling=md5sig --new
--filter-tcp=443 --ipset=/opt/zapret/ipset/zapret-ip-user.txt --dpi-desync=split2 --dpi-desync-split-seqovl=681 --dpi-desync-split-seqovl-pattern=/opt/zapret/files/fake/tls_clienthello_www_google_com.bin --new
--filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-fake-tls=0x00000000 --dpi-desync-fake-tls=! --dpi-desync-split-pos=1,midsld --dpi-desync-repeats=2 --dpi-desync-fooling=badseq --dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com --new
--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=8 --dpi-desync-fake-quic=/opt/zapret/files/fake/quic_initial_www_google_com.bin --new
--filter-tcp=443 --hostlist-domains=.googlevideo.com,.ytimg.com --dpi-desync=fake,split2 --dpi-desync-repeats=4 --dpi-desync-fooling=md5sig --new
--filter-udp=50000-50099 --filter-l7=discord,stun --dpi-desync=fake --new
--filter-udp=443,590-1400,3478 <HOSTLIST_NOAUTO> --dpi-desync=fake,multidisorder --dpi-desync-repeats=2 --dpi-desync-cutoff=d3 --new`

func TestZapret_SharedGroupConfig(t *testing.T) {
	res := analyze(t, sharedGroupConfig)

	t.Run("noOrphanedNotes", func(t *testing.T) {
		for _, n := range res.Notes {
			if n.Profile < 0 || n.Profile >= len(res.Sets) {
				t.Fatalf("note %q points at profile %d but there are %d sets",
					n.Token, n.Profile, len(res.Sets))
			}
		}
	})

	t.Run("everyOptionAccountedFor", func(t *testing.T) {
		for _, n := range res.Notes {
			if n.Reason == "unaccountedOption" || n.Status == StatusUnknown {
				t.Fatalf("%q was not accounted for: %s/%s", n.Token, n.Status, n.Reason)
			}
		}
	})

	t.Run("trailingSeparatorMakesNoSet", func(t *testing.T) {
		last := res.Sets[len(res.Sets)-1]
		if last.Fragmentation.Strategy == "none" && last.TCP.DPortFilter == "" &&
			last.UDP.DPortFilter == "" && !last.Faking.SNI {
			t.Fatal("a trailing --new must not produce an empty set")
		}
	})

	t.Run("repeatedFakeTLSBothReported", func(t *testing.T) {
		var hex, builtin bool
		for _, n := range res.Notes {
			switch n.Token {
			case "--dpi-desync-fake-tls=0x00000000":
				hex = n.Reason == "fakeHexPayload"
			case "--dpi-desync-fake-tls=!":
				builtin = n.Reason == "fakeBuiltinPayload"
			}
		}
		if !hex || !builtin {
			t.Fatalf("both --dpi-desync-fake-tls options must be reported (hex=%v builtin=%v)", hex, builtin)
		}
	})

	t.Run("templatePlaceholderIsNotAnError", func(t *testing.T) {
		n := noteFor(t, res, "<HOSTLIST_NOAUTO>")
		if n.Status != StatusNotApplicable || n.Reason != "templatePlaceholder" {
			t.Fatalf("got %+v", n)
		}
	})

	t.Run("udpProfilesKeepTheirOwnPorts", func(t *testing.T) {
		want := map[string]bool{"50000-50099": false, "443,590-1400,3478": false}
		for _, s := range res.Sets {
			if _, ok := want[s.UDP.DPortFilter]; ok {
				want[s.UDP.DPortFilter] = true
			}
		}
		for ports, seen := range want {
			if !seen {
				t.Fatalf("no set carries the UDP port filter %q", ports)
			}
		}
	})
}

func TestZapret_FakeTLSModFullMapping(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake,multidisorder --dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com")
	set := res.Sets[0]

	if strings.Join(set.Faking.TLSMod, ",") != "rnd,dupsid" {
		t.Fatalf("tls_mod: got %v, want [rnd dupsid]", set.Faking.TLSMod)
	}
	if set.Faking.SNIType != config.FakePayloadDomain || set.Faking.PayloadDomain != "www.google.com" {
		t.Fatalf("sni=: got type=%d domain=%q", set.Faking.SNIType, set.Faking.PayloadDomain)
	}
	n := noteFor(t, res, "--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com")
	if n.Status != StatusMapped {
		t.Fatalf("all three parts map exactly, got %+v", n)
	}
}

func TestZapret_FakeTLSModPartial(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls-mod=rnd,padencap")
	n := noteFor(t, res, "--dpi-desync-fake-tls-mod=rnd,padencap")
	if n.Status != StatusApproximated || n.Reason != "fakeTLSModPartial" {
		t.Fatalf("got %+v", n)
	}
	if n.Params["dropped"] != "padencap" {
		t.Fatalf("dropped: got %v", n.Params["dropped"])
	}
}

func TestZapret_ManyUDPProfilesAreNotFolded(t *testing.T) {
	res := analyze(t, "--filter-tcp=443 --dpi-desync=multisplit --new "+
		"--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=8 --new "+
		"--filter-udp=3478 --dpi-desync=fake --dpi-desync-repeats=2")

	if len(res.Sets) != 3 {
		t.Fatalf("with more than one UDP profile nothing may be folded away, got %d sets", len(res.Sets))
	}
	seen := map[int]bool{}
	for _, s := range res.Sets {
		if s.UDP.FakeSeqLength > 1 {
			seen[s.UDP.FakeSeqLength] = true
		}
	}
	if !seen[8] || !seen[2] {
		t.Fatalf("both UDP profiles must keep their settings, got %v", seen)
	}
}

func TestZapret_UDPOnlySetWarnsAboutProtocolScope(t *testing.T) {
	res := analyze(t, "--filter-tcp=443 --dpi-desync=multisplit --new "+
		"--filter-udp=443 --dpi-desync=fake --new --filter-udp=3478 --dpi-desync=fake")
	var found bool
	for _, n := range res.Notes {
		if n.Reason == "udpOnlySetNotProtocolScoped" {
			found = true
		}
	}
	if !found {
		t.Fatal("a UDP-only set that could steal domains from a TCP set must say so")
	}
}
