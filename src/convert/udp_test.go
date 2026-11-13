package convert

import (
	"reflect"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestZapret_AProfileThatFakesTCPAndUDPKeepsItsUDPFakePayload(t *testing.T) {
	bases := []struct{ name, line string }{
		{"tcpAndUDPPorts", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=fake"},
		{"noPortFilters", "--hostlist-domains=example.com --dpi-desync=fake,multisplit"},
	}
	tests := []struct {
		token   string
		status  Status
		reason  string
		payload string
		data    []byte
	}{
		{"--dpi-desync-fake-quic=/opt/zapret/files/fake/quic_initial_www_google_com.bin",
			StatusApproximated, "fakeQUICApproximated", config.FakePayloadAutoQUIC, nil},
		{"--dpi-desync-fake-unknown-udp=/opt/zapret/files/fake/udp.bin",
			StatusApproximated, "fakeUnknownUDPApproximated", config.FakePayloadAutoQUIC, nil},
		{"--dpi-desync-fake-quic=0x0d0a0bfc", StatusMapped, "fakeDataMapped", "", []byte{0x0d, 0x0a, 0x0b, 0xfc}},
		{"--dpi-desync-fake-unknown-udp=0x0d0a0bfc", StatusMapped, "fakeDataMapped", "", []byte{0x0d, 0x0a, 0x0b, 0xfc}},
		{"--dpi-desync-fake-quic=0x00000000", StatusMapped, "fakeZeroPayload", "", nil},
		{"--dpi-desync-fake-unknown-udp=0x00000000", StatusMapped, "fakeZeroPayload", "", nil},
	}
	for _, b := range bases {
		without := analyze(t, b.line).Sets[0]
		for _, tt := range tests {
			t.Run(b.name+"/"+tt.token, func(t *testing.T) {
				res := analyze(t, b.line+" "+tt.token)
				if len(res.Sets) != 1 {
					t.Fatalf("got %d sets", len(res.Sets))
				}
				set := res.Sets[0]
				if tt.data != nil {
					assertPendingUDPPayload(t, set, tt.data)
				} else if set.UDP.FakePayloadFile != tt.payload || len(set.UDP.FakePayloadData) > 0 {
					t.Fatalf("udp.fake_payload_file=%q, %d bytes, want %q", set.UDP.FakePayloadFile, len(set.UDP.FakePayloadData), tt.payload)
				}
				if n := noteFor(t, res, tt.token); n.Status != tt.status || n.Reason != tt.reason {
					t.Fatalf("got %+v", n)
				}
				set.UDP.FakePayloadFile, set.UDP.FakePayloadData = without.UDP.FakePayloadFile, without.UDP.FakePayloadData
				if !reflect.DeepEqual(set, without) {
					t.Fatalf("the fake payload must change nothing else in the set:\nwith    %+v\nwithout %+v", set.UDP, without.UDP)
				}
				assertNoLostOptions(t, res)
				assertSetsSave(t, res)
			})
		}
	}
}

func TestZapret_AUDPFakePayloadOnAProfileThatSendsNoUDPFakeIsUnused(t *testing.T) {
	profiles := []struct{ name, line string }{
		{"tcpPortsOnly", "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake"},
		{"tcpOnlyL7", "--filter-l7=tls --hostlist-domains=example.com --dpi-desync=fake"},
		{"tcpAndUDPPortsWithoutAFakeMode", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=multisplit"},
		{"noPortFiltersWithoutAFakeMode", "--hostlist-domains=example.com --dpi-desync=multisplit"},
		{"tcpFakeOnly", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=fakedsplit"},
	}
	tokens := []string{
		"--dpi-desync-fake-quic=q.bin",
		"--dpi-desync-fake-quic=0x0d0a",
		"--dpi-desync-fake-quic=0x00000000",
		"--dpi-desync-fake-unknown-udp=u.bin",
	}
	for _, p := range profiles {
		for _, token := range tokens {
			t.Run(p.name+"/"+token, func(t *testing.T) {
				res := analyze(t, p.line+" "+token)
				if u := res.Sets[0].UDP; u.FakePayloadFile != "" || len(u.FakePayloadData) > 0 {
					t.Fatalf("zapret sends no UDP fake for this profile, so the set takes no UDP payload from it: file %q, %d bytes",
						u.FakePayloadFile, len(u.FakePayloadData))
				}
				if n := noteFor(t, res, token); n.Status != StatusDegenerate || n.Reason != "udpFakeUnused" {
					t.Fatalf("got %+v", n)
				}
				assertNoLostOptions(t, res)
				assertSetsSave(t, res)
			})
		}
	}

	res := analyze(t, "--filter-tcp=443 --dpi-desync=fake --dpi-desync-fake-quic=0x0d0")
	if n := noteFor(t, res, "--dpi-desync-fake-quic=0x0d0"); n.Status != StatusInvalid || n.Reason != "fakeHexInvalid" {
		t.Fatalf("an undecodable hex payload is invalid wherever it stands: %+v", n)
	}
}

func TestZapret_AnyProtocolOnAProfileThatCoversTCP(t *testing.T) {
	const both = "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com "
	tests := []struct {
		name   string
		line   string
		token  string
		status Status
		reason string
		quic   string
	}{
		{"fakesUDPOnItsPorts", both + "--dpi-desync=fake --dpi-desync-any-protocol",
			"--dpi-desync-any-protocol", StatusApproximated, "anyProtocolUDP", config.QUICFilterAll},
		{"fakesUDPWithoutPortFilters", "--hostlist-domains=example.com --dpi-desync=fake --dpi-desync-any-protocol=1",
			"--dpi-desync-any-protocol=1", StatusApproximated, "anyProtocolUDP", config.QUICFilterAll},
		{"neverAppliesToUDP", "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --dpi-desync-any-protocol",
			"--dpi-desync-any-protocol", StatusUnsupported, "anyProtocol", config.QUICFilterSNI},
		{"sendsNoUDPFake", both + "--dpi-desync=multisplit --dpi-desync-any-protocol",
			"--dpi-desync-any-protocol", StatusUnsupported, "anyProtocol", config.QUICFilterSNI},
		{"switchedOff", both + "--dpi-desync=fake --dpi-desync-any-protocol=0",
			"--dpi-desync-any-protocol=0", StatusMapped, "anyProtocolOff", config.QUICFilterSNI},
		{"foldedUDPProfile", "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit --new " +
			"--filter-udp=443 --dpi-desync=fake --dpi-desync-any-protocol",
			"--dpi-desync-any-protocol", StatusApproximated, "anyProtocolUDP", config.QUICFilterAll},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			if len(res.Sets) != 1 {
				t.Fatalf("got %d sets", len(res.Sets))
			}
			if got := res.Sets[0].UDP.FilterQUIC; got != tt.quic {
				t.Fatalf("udp.filter_quic=%q, want %q", got, tt.quic)
			}
			if n := noteFor(t, res, tt.token); n.Status != tt.status || n.Reason != tt.reason {
				t.Fatalf("got %+v", n)
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}

	res := analyze(t, "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit --dpi-desync-any-protocol --new "+
		"--filter-udp=443 --dpi-desync=fake")
	if n := noteFor(t, res, "--dpi-desync-any-protocol"); n.Status != StatusUnsupported || n.Reason != "anyProtocol" {
		t.Fatalf("the option stands on the TCP profile, and the folded UDP profile did not ask for it: %+v", n)
	}
}

func TestZapret_RepeatsReachTheUDPFakesOfAProfileThatFakesTCPAndUDP(t *testing.T) {
	const token = "--dpi-desync-repeats=11"
	udpDefault := config.DefaultSetConfig.UDP.FakeSeqLength
	tests := []struct {
		name string
		line string
		udp  int
	}{
		{"tcpAndUDPPorts", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=fake", 11},
		{"noPortFilters", "--hostlist-domains=example.com --dpi-desync=fake", 11},
		{"tcpPortsOnly", "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake", udpDefault},
		{"tcpFakeOnly", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=fakedsplit", udpDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line+" "+token)
			set := res.Sets[0]
			if set.Faking.SNISeqLength != 11 || set.UDP.FakeSeqLength != tt.udp {
				t.Fatalf("faking.sni_seq_length=%d udp.fake_seq_length=%d, want 11 and %d",
					set.Faking.SNISeqLength, set.UDP.FakeSeqLength, tt.udp)
			}
			n := noteFor(t, res, token)
			if n.Status != StatusMapped || n.Reason != "fakeRepeatsMapped" || !hasField(n, "faking.sni_seq_length") {
				t.Fatalf("got %+v", n)
			}
			if got := hasField(n, "udp.fake_seq_length"); got != (tt.udp == 11) {
				t.Fatalf("the note must name the UDP count only when the option reaches it: %+v", n)
			}
			assertSetsSave(t, res)
		})
	}
}

func TestZapret_TheFakeTTLOfAUDPProfileBecomesTheSetsFakeTTL(t *testing.T) {
	const carrier = "--filter-tcp=443 --hostlist-domains=example.com "
	const udp = " --new --filter-udp=443 --dpi-desync=fake --dpi-desync-ttl=5"
	tests := []struct {
		name   string
		line   string
		token  string
		ttl    uint8
		status Status
		reason string
	}{
		{"udpOnly", "--filter-udp=443 --dpi-desync=fake --dpi-desync-ttl=4",
			"--dpi-desync-ttl=4", 4, StatusMapped, "udpFakeTTLMapped"},
		{"foldedIntoASetWithoutTCPFakes", carrier + "--dpi-desync=multisplit" + udp,
			"--dpi-desync-ttl=5", 5, StatusMapped, "udpFakeTTLMapped"},
		{"foldedIntoASetWithTCPFakes", carrier + "--dpi-desync=fake --dpi-desync-ttl=3" + udp,
			"--dpi-desync-ttl=5", 3, StatusApproximated, "udpFakeTTLShared"},
		{"tcpAndUDPPorts", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=fake --dpi-desync-ttl=4",
			"--dpi-desync-ttl=4", 4, StatusMapped, "fakeTTLMapped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			if len(res.Sets) != 1 {
				t.Fatalf("got %d sets", len(res.Sets))
			}
			set := res.Sets[0]
			if set.Faking.TTL != tt.ttl {
				t.Fatalf("faking.ttl=%d, want %d: b4's UDP fakes take their TTL from the set's Fake TTL", set.Faking.TTL, tt.ttl)
			}
			if set.UDP.FakingStrategy == "ttl" {
				t.Fatal("udp.faking_strategy=ttl is never read by the packet path, so the import must not rely on it")
			}
			if n := noteFor(t, res, tt.token); n.Status != tt.status || n.Reason != tt.reason {
				t.Fatalf("got %+v", n)
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}
	if n := noteFor(t, analyze(t, tests[0].line), "--dpi-desync-ttl=4"); !slices.Contains(n.Fields, "faking.ttl=4") {
		t.Fatalf("the note must name the field that holds the TTL: %+v", n)
	}

}

func TestZapret_AUDPProfileWithoutAFakeModeLeavesTheSetsUDPSideAlone(t *testing.T) {
	defaults := config.DefaultSetConfig
	for _, desync := range []string{"udplen", "multisplit", "fakedsplit"} {
		t.Run(desync, func(t *testing.T) {
			res := analyze(t, "--filter-udp=443 --dpi-desync="+desync+
				" --dpi-desync-fake-quic=q.bin --dpi-desync-repeats=4 --dpi-desync-ttl=4")
			set := res.Sets[0]
			if set.UDP.FakeSeqLength != defaults.UDP.FakeSeqLength || set.UDP.FakePayloadFile != "" || set.Faking.TTL != defaults.Faking.TTL {
				t.Fatalf("zapret sends no UDP fake in this mode, so its fake options are not this set's: fake_seq_length=%d file=%q faking.ttl=%d",
					set.UDP.FakeSeqLength, set.UDP.FakePayloadFile, set.Faking.TTL)
			}
			want := []struct {
				token  string
				reason string
			}{
				{"--dpi-desync-fake-quic=q.bin", "udpFakeUnused"},
				{"--dpi-desync-repeats=4", "requiresFake"},
				{"--dpi-desync-ttl=4", "requiresFake"},
			}
			for _, w := range want {
				if n := noteFor(t, res, w.token); n.Status != StatusDegenerate || n.Reason != w.reason {
					t.Errorf("%s: got %s/%s, want %s", w.token, n.Status, n.Reason, w.reason)
				}
			}
			if got := reasonNotes(res, "udpDefaultFakes"); len(got) != 1 {
				t.Fatalf("the set still sends the UDP fakes of a new b4 set, and must say so: %+v", got)
			}
			assertSetsSave(t, res)
		})
	}
}

func TestZapret_AProfileLeavesUDPToAnEarlierUDPProfileThatTakesItsQUIC(t *testing.T) {
	const list = "--hostlist=/opt/zapret/list.txt"
	const later = " --dpi-desync=fake,multisplit --dpi-desync-repeats=2 --dpi-desync-fake-quic=q.bin"
	tests := []struct {
		name  string
		line  string
		taken bool
	}{
		{"sameHostList", "--filter-udp=443 " + list + " --dpi-desync=fake --dpi-desync-repeats=11 --new " + list + later, true},
		{"sameHostListInAPortRange", "--filter-udp=400-500 " + list + " --dpi-desync=fake --new " + list + later, true},
		{"noTargetsAtAll", "--filter-udp=443 --dpi-desync=fake --new --filter-tcp=80 --hostlist-domains=a.com --dpi-desync=multisplit --new " +
			"--hostlist-domains=example.com" + later, true},
		{"otherHosts", "--filter-udp=443 --hostlist-domains=quic.example.com --dpi-desync=fake --dpi-desync-repeats=11 --new " +
			"--hostlist-domains=example.com" + later, false},
		{"otherHostList", "--filter-udp=443 --hostlist=/opt/zapret/quic.txt --dpi-desync=fake --new " + list + later, false},
		{"otherPorts", "--filter-udp=50000-50100 " + list + " --dpi-desync=fake --new " + list + later, false},
		{"callsOnly", "--filter-udp=443,3478 --filter-l7=discord,stun --dpi-desync=fake --new " + list + later, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			set := res.Sets[len(res.Sets)-1]
			if set.Faking.SNISeqLength != 2 {
				t.Fatalf("the last set is not the profile under test: %+v", set.Faking)
			}
			wantCount, wantFile, wantReason := 2, config.FakePayloadAutoQUIC, "fakeQUICApproximated"
			if tt.taken {
				wantCount, wantFile, wantReason = config.DefaultSetConfig.UDP.FakeSeqLength, "", "udpFakeUnused"
			}
			if set.UDP.FakeSeqLength != wantCount || set.UDP.FakePayloadFile != wantFile {
				t.Fatalf("nfqws gives a packet to the first profile that matches it, so this one keeps its UDP fake options "+
					"unless an earlier UDP profile takes QUIC for the same targets: fake_seq_length=%d file=%q, want %d and %q",
					set.UDP.FakeSeqLength, set.UDP.FakePayloadFile, wantCount, wantFile)
			}
			if n := noteFor(t, res, "--dpi-desync-repeats=2"); hasField(n, "udp.fake_seq_length") == tt.taken {
				t.Fatalf("got %+v", n)
			}
			if n := noteFor(t, res, "--dpi-desync-fake-quic=q.bin"); n.Reason != wantReason {
				t.Fatalf("got %+v, want %s", n, wantReason)
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}

	keenetic := analyze(t, loadFixture(t, "nfqws-keenetic.conf"))
	checked := 0
	for i, set := range keenetic.Sets {
		if set.Fragmentation.Strategy == config.ConfigNone || set.TCP.DPortFilter != "" {
			continue
		}
		checked++
		if set.UDP.FakeSeqLength != config.DefaultSetConfig.UDP.FakeSeqLength {
			t.Fatalf("set %d: the launcher puts its QUIC profiles ahead of NFQWS_ARGS, so the repeats of NFQWS_ARGS stay on TCP: udp.fake_seq_length=%d",
				i, set.UDP.FakeSeqLength)
		}
	}
	if checked == 0 {
		t.Fatal("the fixture no longer holds a TCP profile without port filters")
	}
}

func TestConvert_ASetThatFakesUDPOnlyByDefaultSaysSo(t *testing.T) {
	said := []string{
		"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit",
		"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake",
		"--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=multisplit",
		"--hostlist-domains=example.com --dpi-desync=multisplit",
		"--filter-udp=443 --dpi-desync=multisplit",
		"-s1+s",
	}
	for _, line := range said {
		t.Run("said/"+line, func(t *testing.T) {
			res := analyze(t, line)
			if res.Sets[0].UDP.Mode != config.UDPModeFake {
				t.Fatalf("udp.mode=%q: the set keeps the UDP side of a new b4 set", res.Sets[0].UDP.Mode)
			}
			got := reasonNotes(res, "udpDefaultFakes")
			if len(got) != 1 || !got[0].Synthetic || got[0].Status != StatusApproximated || got[0].Profile != 0 ||
				!slices.Equal(got[0].Fields, []string{"udp.mode=fake"}) {
				t.Fatalf("the profile does nothing to UDP, so the report must say where the set's UDP fakes come from: %+v", got)
			}
			options := slices.DeleteFunc(slices.Clone(res.Notes), func(n Note) bool { return n.Reason == "udpDefaultFakes" })
			if res.Fidelity.Score != score(options).Score {
				t.Fatalf("the line is about the set, not about an option of the source, so it must not move the score: %d, want %d",
					res.Fidelity.Score, score(options).Score)
			}
			assertSetsSave(t, res)
		})
	}

	silent := []string{
		"--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=fake",
		"--hostlist-domains=example.com --dpi-desync=fake",
		"--filter-udp=443 --dpi-desync=fake",
		"--filter-udp=50000-50100 --filter-l7=discord,stun --dpi-desync=multisplit",
		"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=multisplit --new --filter-udp=443 --dpi-desync=fake",
		"--skip --hostlist-domains=example.com --dpi-desync=multisplit",
		"-Ku -a1",
		"-Ku -a1 -An -s1+s",
	}
	for _, line := range silent {
		t.Run("silent/"+line, func(t *testing.T) {
			res := analyze(t, line)
			if got := reasonNotes(res, "udpDefaultFakes"); len(got) != 0 {
				t.Fatalf("the profile fakes UDP itself, is skipped, or its set is created disabled for sending no fake: %+v", got)
			}
		})
	}
}

func TestZapret_AProfileThatStatesItsUDPSideTakesNoFold(t *testing.T) {
	const udp = " --new --filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11"
	udpDefault := config.DefaultSetConfig.UDP.FakeSeqLength
	separate := []struct {
		name    string
		line    string
		payload string
	}{
		{"udpPortsAndPayload", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=fake " +
			"--dpi-desync-fake-quic=/opt/zapret/quic.bin" + udp, config.FakePayloadAutoQUIC},
		{"udpPortsOnly", "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=multisplit" + udp, ""},
		{"payloadWithoutPortFilters", "--hostlist-domains=example.com --dpi-desync=fake " +
			"--dpi-desync-fake-quic=/opt/zapret/quic.bin" + udp, config.FakePayloadAutoQUIC},
	}
	for _, tt := range separate {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			if len(res.Sets) != 2 {
				t.Fatalf("nfqws gives UDP to the first profile that matches it, so the UDP profile must not replace this one's UDP side: got %d sets",
					len(res.Sets))
			}
			first, second := res.Sets[0], res.Sets[1]
			if first.UDP.FakePayloadFile != tt.payload || first.UDP.FakeSeqLength != udpDefault {
				t.Fatalf("the first profile lost its own UDP side: file %q, fake_seq_length %d", first.UDP.FakePayloadFile, first.UDP.FakeSeqLength)
			}
			if second.UDP.FakeSeqLength != 11 || second.UDP.FakePayloadFile != "" {
				t.Fatalf("the UDP profile keeps its own settings: file %q, fake_seq_length %d", second.UDP.FakePayloadFile, second.UDP.FakeSeqLength)
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}

	res := analyze(t, "--filter-tcp=443 --filter-udp=443 --hostlist-domains=example.com --dpi-desync=multisplit --new "+
		"--filter-udp=50000-50100 --dpi-desync=fake")
	if len(res.Sets) != 2 || res.Sets[0].UDP.DPortFilter != "443" || res.Sets[1].UDP.DPortFilter != "50000-50100" {
		t.Fatalf("each profile keeps its own UDP ports: %d sets", len(res.Sets))
	}

	for _, carrier := range []string{
		"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake",
		"--hostlist-domains=example.com --dpi-desync=fake,multisplit",
	} {
		res := analyze(t, carrier+udp)
		if len(res.Sets) != 1 || res.Sets[0].UDP.FakeSeqLength != 11 {
			t.Fatalf("a profile that states nothing about UDP still takes the fold: %q gave %d sets", carrier, len(res.Sets))
		}
	}
}

func TestZapret_AFoldedUDPProfileNamesItsOwnOptions(t *testing.T) {
	res := analyze(t, "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --dpi-desync-repeats=3 "+
		"--dpi-desync-fake-quic=/opt/zapret/tcp-side.bin --new "+
		"--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11 --dpi-desync-fake-quic=0x0d0a")
	if len(res.Sets) != 1 {
		t.Fatalf("got %d sets", len(res.Sets))
	}
	set := res.Sets[0]
	assertPendingUDPPayload(t, set, []byte{0x0d, 0x0a})
	if set.Faking.SNISeqLength != 3 || set.UDP.FakeSeqLength != 11 {
		t.Fatalf("faking.sni_seq_length=%d udp.fake_seq_length=%d, want 3 and 11", set.Faking.SNISeqLength, set.UDP.FakeSeqLength)
	}
	want := []struct{ token, reason string }{
		{"--dpi-desync-repeats=3", "fakeRepeatsMapped"},
		{"--dpi-desync-repeats=11", "udpFakeRepeatsMapped"},
		{"--dpi-desync-fake-quic=/opt/zapret/tcp-side.bin", "udpFakeUnused"},
		{"--dpi-desync-fake-quic=0x0d0a", "fakeDataMapped"},
	}
	for _, w := range want {
		if n := noteFor(t, res, w.token); n.Reason != w.reason {
			t.Errorf("%s: got %s/%s, want %s", w.token, n.Status, n.Reason, w.reason)
		}
	}
	assertNoLostOptions(t, res)
	assertSetsSave(t, res)
}

func TestZapret_APortFilterForEveryPortIsStillAFilter(t *testing.T) {
	const token = "--dpi-desync-fake-quic=q.bin"
	tests := []struct {
		name    string
		filters string
		payload string
		udp     int
		reason  string
	}{
		{"everyUDPPort", "--filter-tcp=443 --filter-udp=*", config.FakePayloadAutoQUIC, 8, "fakeQUICApproximated"},
		{"everyTCPPort", "--filter-tcp=*", "", config.DefaultSetConfig.UDP.FakeSeqLength, "udpFakeUnused"},
		{"negatedTCPPort", "--filter-tcp=~80", "", config.DefaultSetConfig.UDP.FakeSeqLength, "udpFakeUnused"},
		{"negatedUDPPort", "--filter-tcp=443 --filter-udp=~53", config.FakePayloadAutoQUIC, 8, "fakeQUICApproximated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.filters+" --hostlist-domains=example.com --dpi-desync=fake --dpi-desync-repeats=8 "+token)
			udp := res.Sets[0].UDP
			if udp.FakePayloadFile != tt.payload || udp.FakeSeqLength != tt.udp {
				t.Fatalf("nfqws denies UDP to a profile with a TCP filter and no UDP filter, whatever ports they name: "+
					"file %q fake_seq_length %d, want %q and %d", udp.FakePayloadFile, udp.FakeSeqLength, tt.payload, tt.udp)
			}
			if n := noteFor(t, res, token); n.Reason != tt.reason {
				t.Fatalf("got %+v, want %s", n, tt.reason)
			}
			assertNoLostOptions(t, res)
			assertSetsSave(t, res)
		})
	}

	res := analyze(t, "--filter-tcp=443 --filter-udp=* --hostlist-domains=example.com --dpi-desync=multisplit --new "+
		"--filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11")
	if len(res.Sets) != 2 {
		t.Fatalf("a profile that filters UDP for every port has a UDP side of its own and takes no fold: got %d sets", len(res.Sets))
	}
}
