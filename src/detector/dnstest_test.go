package detector

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func dnsReply(t *testing.T, rcode dnsmessage.RCode, ips ...string) []byte {
	t.Helper()
	name := dnsmessage.MustNewName("blocked.test.")
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 1, Response: true, RecursionAvailable: true, RCode: rcode})
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	if err := b.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	for _, ip := range ips {
		hdr := dnsmessage.ResourceHeader{Name: name, Class: dnsmessage.ClassINET, TTL: 60}
		parsed := net.ParseIP(ip)
		var err error
		if v4 := parsed.To4(); v4 != nil {
			err = b.AResource(hdr, dnsmessage.AResource{A: [4]byte(v4)})
		} else {
			err = b.AAAAResource(hdr, dnsmessage.AAAAResource{AAAA: [16]byte(parsed.To16())})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestParseAnswerKeepsTheRcode(t *testing.T) {
	ok := parseAnswer(dnsReply(t, dnsmessage.RCodeSuccess, "93.184.216.34", "2606:2800:220:1::1", "93.184.216.35"), nil, "A")
	if !reflect.DeepEqual(ok.ips, []string{"93.184.216.34", "93.184.216.35"}) || ok.failed() || ok.nx || ok.empty {
		t.Fatalf("NOERROR with addresses = %+v, want the two A addresses", ok)
	}

	empty := parseAnswer(dnsReply(t, dnsmessage.RCodeSuccess), nil, "A")
	if !empty.empty || empty.failed() || empty.nx {
		t.Fatalf("NOERROR without an address = %+v, want empty and not failed", empty)
	}

	onlyV6 := parseAnswer(dnsReply(t, dnsmessage.RCodeSuccess, "2606:2800:220:1::1"), nil, "A")
	if !onlyV6.empty || onlyV6.failed() {
		t.Fatalf("NOERROR without an address of the family = %+v, want empty", onlyV6)
	}

	nx := parseAnswer(dnsReply(t, dnsmessage.RCodeNameError), nil, "A")
	if !nx.nx || nx.failed() || nx.rcode != 3 {
		t.Fatalf("NXDOMAIN = %+v, want nx and not failed", nx)
	}

	for _, tc := range []struct {
		rcode dnsmessage.RCode
		name  string
	}{
		{dnsmessage.RCodeServerFailure, "SERVFAIL"},
		{dnsmessage.RCodeRefused, "REFUSED"},
		{dnsmessage.RCodeNotImplemented, "rcode 4"},
	} {
		ans := parseAnswer(dnsReply(t, tc.rcode), nil, "A")
		if !ans.failed() || ans.nx || ans.empty || len(ans.ips) > 0 {
			t.Fatalf("rcode %d = %+v, want a failure, not an empty answer", tc.rcode, ans)
		}
		if got := ans.failure(nil); got != tc.name {
			t.Fatalf("rcode %d is described as %q, want %q", tc.rcode, got, tc.name)
		}
	}

	timeout := parseAnswer(nil, context.DeadlineExceeded, "A")
	if !timeout.timeout || !timeout.failed() {
		t.Fatalf("a timeout = %+v, want failed", timeout)
	}
	if short := parseAnswer([]byte{0, 1, 2}, nil, "A"); !short.failed() {
		t.Fatalf("a truncated reply = %+v, want failed", short)
	}
}

var honestyTruth = map[string]string{
	"a.test": "93.184.216.34",
	"b.test": "151.101.1.1",
	"c.test": "104.16.0.1",
}

func judgeAgainstTruth(answers map[string]dnsAnswer) *DNSProbe {
	var runs []*serverRun
	for i := 0; i < 2; i++ {
		ref := &serverRun{server: DNSServer{Brand: "Reference", Kind: "doh"}, probe: &DNSProbe{Status: DNSProbeOk}, answers: map[string]dnsAnswer{}}
		for dom, ip := range honestyTruth {
			ref.answers[dom] = dnsAnswer{ips: []string{ip}}
		}
		runs = append(runs, ref)
	}
	subject := &serverRun{server: DNSServer{Brand: "Subject", Kind: "udp"}, probe: &DNSProbe{Status: DNSProbeOk, Honesty: HonestyUnknown}, answers: answers}
	runs = append(runs, subject)
	truth := buildTruth(runs, []string{"a.test", "b.test", "c.test"})
	judgeHonesty(subject, truth, findStubs(runs, truth))
	return subject.probe
}

func TestJudgeHonestyVerdicts(t *testing.T) {
	match := func(dom string) dnsAnswer { return dnsAnswer{ips: []string{honestyTruth[dom]}} }
	servfail := dnsAnswer{rcode: 2}
	for _, tc := range []struct {
		name                             string
		answers                          map[string]dnsAnswer
		want                             DNSHonesty
		checked, sub, noAnswer, filtered int
	}{
		{"every answer matches", map[string]dnsAnswer{"a.test": match("a.test"), "b.test": match("b.test"), "c.test": match("c.test")}, HonestyHonest, 3, 0, 0, 0},
		{"a stub address", map[string]dnsAnswer{"a.test": {ips: []string{"10.10.34.36"}}, "b.test": match("b.test"), "c.test": match("c.test")}, HonestySubstituted, 3, 1, 0, 0},
		{"one public address for two names", map[string]dnsAnswer{"a.test": {ips: []string{"195.208.4.1"}}, "b.test": {ips: []string{"195.208.4.1"}}, "c.test": match("c.test")}, HonestySubstituted, 3, 2, 0, 0},
		{"a stub outranks a missing answer", map[string]dnsAnswer{"a.test": {ips: []string{"10.10.34.36"}}, "b.test": {timeout: true}, "c.test": match("c.test")}, HonestySubstituted, 3, 1, 1, 0},
		{"one timeout among matches", map[string]dnsAnswer{"a.test": match("a.test"), "b.test": match("b.test"), "c.test": {timeout: true}}, HonestyNoAnswer, 3, 0, 1, 0},
		{"a SERVFAIL among matches", map[string]dnsAnswer{"a.test": match("a.test"), "b.test": servfail, "c.test": match("c.test")}, HonestyNoAnswer, 3, 0, 1, 0},
		{"a missing answer outranks NXDOMAIN", map[string]dnsAnswer{"a.test": {nx: true, rcode: 3}, "b.test": {err: true}, "c.test": match("c.test")}, HonestyNoAnswer, 3, 0, 1, 1},
		{"NXDOMAIN only", map[string]dnsAnswer{"a.test": {nx: true, rcode: 3}, "b.test": {nx: true, rcode: 3}, "c.test": {nx: true, rcode: 3}}, HonestyFiltered, 3, 0, 0, 3},
		{"empty only", map[string]dnsAnswer{"a.test": {empty: true}, "b.test": {empty: true}, "c.test": {empty: true}}, HonestyFiltered, 3, 0, 0, 3},
		{"other addresses only", map[string]dnsAnswer{"a.test": {ips: []string{"203.0.113.10"}}, "b.test": {ips: []string{"198.51.100.20"}}, "c.test": {ips: []string{"192.0.2.30"}}}, HonestyDiffers, 3, 0, 0, 0},
		{"no name with a known truth", map[string]dnsAnswer{"x.test": {timeout: true}}, HonestyUnknown, 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := judgeAgainstTruth(tc.answers)
			if p.Honesty != tc.want {
				t.Fatalf("honesty = %s, want %s (%+v)", p.Honesty, tc.want, p)
			}
			if p.Checked != tc.checked || p.Substituted != tc.sub || p.NoAnswer != tc.noAnswer || p.Filtered != tc.filtered {
				t.Fatalf("counts checked %d substituted %d no_answer %d filtered %d, want %d %d %d %d",
					p.Checked, p.Substituted, p.NoAnswer, p.Filtered, tc.checked, tc.sub, tc.noAnswer, tc.filtered)
			}
		})
	}
}

func TestSubstitutingAndNoAnswerCountProviders(t *testing.T) {
	probe := func(h DNSHonesty) *DNSProbe { return &DNSProbe{Status: DNSProbeOk, Honesty: h} }
	runs := []*serverRun{
		{server: DNSServer{Name: "127.0.0.53", Brand: "Router", Address: "127.0.0.53", Kind: "udp"}, router: true, probe: probe(HonestySubstituted)},
		{server: DNSServer{Name: "Google", Brand: "Google", Kind: "udp"}, probe: probe(HonestySubstituted)},
		{server: DNSServer{Name: "Google DoH", Brand: "Google", Kind: "doh"}, probe: probe(HonestySubstituted)},
		{server: DNSServer{Name: "Google DoT", Brand: "Google", Kind: "dot"}, probe: probe(HonestyHonest)},
		{server: DNSServer{Name: "Quad9", Brand: "Quad9", Kind: "udp"}, probe: probe(HonestyNoAnswer)},
		{server: DNSServer{Name: "Quad9 DoH", Brand: "Quad9", Kind: "doh"}, probe: probe(HonestyHonest)},
		{server: DNSServer{Name: "AdGuard", Brand: "AdGuard", Kind: "udp"}, probe: probe(HonestySubstituted)},
		{server: DNSServer{Name: "AdGuard DoH", Brand: "AdGuard", Kind: "doh"}, probe: probe(HonestyNoAnswer)},
		{server: DNSServer{Name: "Cloudflare", Brand: "Cloudflare", Kind: "udp"}, probe: probe(HonestyFiltered)},
	}
	providers := buildProviders(runs, true)

	if got, want := providersJudged(providers, HonestySubstituted), []string{"127.0.0.53", "Google", "AdGuard"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("substituting providers = %v, want %v: one provider counts once, in table order", got, want)
	}
	if got, want := providersJudged(providers, HonestyNoAnswer), []string{"Quad9", "AdGuard"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("providers without an answer = %v, want %v", got, want)
	}

	s := &Suite{ctx: context.Background(), DNS: &DNSResult{Substituting: 3, NoAnswer: 2}}
	s.refreshVerdict()
	if !s.Verdict.DNSSubstituted || !s.Verdict.DNSNoAnswer {
		t.Fatalf("verdict = %+v, want dns_substituted and dns_no_answer", s.Verdict)
	}
	s.DNS = &DNSResult{}
	s.refreshVerdict()
	if s.Verdict.DNSSubstituted || s.Verdict.DNSNoAnswer {
		t.Fatalf("verdict = %+v, want neither flag", s.Verdict)
	}
}

func TestBrandMatchSurvivesAnEmptyBrand(t *testing.T) {
	if brandMatches("GOOGLE", "") || brandMatches("GOOGLE", "   ") {
		t.Fatal("an empty brand must not match, nor index its first word")
	}
	if !brandMatches("CLOUDFLARENET", "Cloudflare Family") {
		t.Fatal("the first word of the brand matches the egress organisation")
	}
	if !knownResolverOrg("CDNEXT, GB") {
		t.Fatal("ControlD answers from CDNEXT (Datacamp); it is a known resolver network, not a hijack")
	}
}

func TestParseNameserversFollowsGoResolver(t *testing.T) {
	for _, tc := range []struct {
		name string
		conf string
		want []string
	}{
		{
			"loopback and zoned IPv6 kept, other lines ignored",
			"# generated by NetworkManager\n; comment\nsearch lan\noptions edns0 trust-ad\nnameserver 127.0.0.53\ndomain lan\nnameserver fe80::1%eth0\nnameserver ::1\n",
			[]string{"127.0.0.53", "fe80::1%eth0", "::1"},
		},
		{
			"at most three",
			"nameserver 1.1.1.1\nnameserver 8.8.8.8\nnameserver 9.9.9.9\nnameserver 172.20.25.100\n",
			[]string{"1.1.1.1", "8.8.8.8", "9.9.9.9"},
		},
		{
			"duplicates dropped, not an address skipped",
			"nameserver 172.20.25.100\nnameserver resolver.lan\nnameserver\nnameserver 172.20.25.100\nnameserver 8.8.8.8\n",
			[]string{"172.20.25.100", "8.8.8.8"},
		},
		{
			"a duplicate uses one of the three slots, as in Go",
			"nameserver 1.1.1.1\nnameserver 1.1.1.1\nnameserver 8.8.8.8\nnameserver 9.9.9.9\n",
			[]string{"1.1.1.1", "8.8.8.8"},
		},
		{"no nameserver", "search lan\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseNameservers(strings.NewReader(tc.conf)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseNameservers = %v, want %v", got, tc.want)
			}
		})
	}
}
