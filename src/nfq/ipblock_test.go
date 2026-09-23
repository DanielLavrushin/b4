package nfq

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/iphealth"
)

func (w *Worker) healDNSResponse(cfg *config.Config, set *config.SetConfig, domain string, resp []byte, overTCP bool) []byte {
	out, _ := w.filterDNSAnswer(cfg, set, domain, resp, overTCP)
	return out
}

func encodeTestName(name string) []byte {
	buf := make([]byte, 0, len(name)+2)
	for _, label := range splitLabels(name) {
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	return append(buf, 0)
}

func splitLabels(name string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i > start {
				out = append(out, name[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func buildTestAResponse(domain string, ips ...string) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x4242)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(len(ips)))

	msg = append(msg, encodeTestName(domain)...)
	var q [4]byte
	binary.BigEndian.PutUint16(q[0:2], 1)
	binary.BigEndian.PutUint16(q[2:4], 1)
	msg = append(msg, q[:]...)

	for _, ip := range ips {
		msg = append(msg, 0xC0, 0x0C)
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], 1)
		binary.BigEndian.PutUint16(fixed[2:4], 1)
		binary.BigEndian.PutUint32(fixed[4:8], 3600)
		binary.BigEndian.PutUint16(fixed[8:10], 4)
		msg = append(msg, fixed[:]...)
		msg = append(msg, net.ParseIP(ip).To4()...)
	}
	return msg
}

func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

func healWorker(t *testing.T, dead ...string) *Worker {
	t.Helper()
	health := iphealth.NewTracker(func(string, uint16, int) bool { return false })
	t.Cleanup(health.Stop)

	for _, ip := range dead {
		for i := 0; i < 3; i++ {
			health.RecordSyn(ip, 443, 0, 3, time.Hour)
		}
	}
	if len(dead) > 0 && !waitFor(t, func() bool { return health.IsDead(dead[len(dead)-1]) }) {
		t.Fatal("seed addresses never became dead")
	}

	return &Worker{ipHealth: health, goodIPs: iphealth.NewKnownGood(), hostHints: newHostHintCache()}
}

func healSetWith(healDNS bool) *config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "github"
	set.Enabled = true
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.HealDNS = healDNS
	return &set
}

func TestHealDNSResponseStripsUnreachableAddress(t *testing.T) {
	w := healWorker(t, "185.199.110.133")
	set := healSetWith(true)
	resp := buildTestAResponse("raw.githubusercontent.com",
		"185.199.108.133", "185.199.109.133", "185.199.110.133", "185.199.111.133")

	healed := w.healDNSResponse(&config.Config{}, set, "raw.githubusercontent.com", resp, false)
	if healed == nil {
		t.Fatal("expected a curated answer")
	}

	ips := dns.ParseResponseIPs(healed)
	if len(ips) != 3 {
		t.Fatalf("kept %d addresses, want 3", len(ips))
	}
	for _, ip := range ips {
		if ip.String() == "185.199.110.133" {
			t.Errorf("the unreachable address survived")
		}
	}
}

func TestHealDNSResponseIgnoredWhenHealingOff(t *testing.T) {
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	w := healWorker(t, "2.2.2.2")
	if healed := w.healDNSResponse(&config.Config{}, healSetWith(false), "example.com", resp, false); healed != nil {
		t.Errorf("DNS answers must be left alone while heal_dns is off")
	}
}

func TestHealDNSResponseSkippedWhenDetectionOff(t *testing.T) {
	w := healWorker(t, "2.2.2.2")
	set := healSetWith(true)
	set.TCP.IPBlockDetect.Enabled = false
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	if healed := w.healDNSResponse(&config.Config{}, set, "example.com", resp, false); healed != nil {
		t.Errorf("expected no rewrite while IP block detection is off")
	}
}

func TestHealDNSResponseSkippedInDiscovery(t *testing.T) {
	w := healWorker(t, "2.2.2.2")
	cfg := &config.Config{}
	cfg.Queue.IsDiscovery = true
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	if healed := w.healDNSResponse(cfg, healSetWith(true), "example.com", resp, false); healed != nil {
		t.Errorf("a discovery run must not rewrite DNS answers")
	}
}

func TestHealDNSResponseFallsBackToRememberedAddress(t *testing.T) {
	w := healWorker(t, "1.1.1.1", "2.2.2.2")
	w.goodIPs.Remember("example.com", net.ParseIP("3.3.3.3"))
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	healed := w.healDNSResponse(&config.Config{}, healSetWith(true), "example.com", resp, false)
	if healed == nil {
		t.Fatal("expected an answer built from the remembered address")
	}
	ips := dns.ParseResponseIPs(healed)
	if len(ips) != 1 || ips[0].String() != "3.3.3.3" {
		t.Errorf("addresses = %v, want [3.3.3.3]", ips)
	}
}

func TestHealDNSResponsePassesThroughWhenNothingRemembered(t *testing.T) {
	w := healWorker(t, "1.1.1.1", "2.2.2.2")
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	if healed := w.healDNSResponse(&config.Config{}, healSetWith(true), "example.com", resp, false); healed != nil {
		t.Errorf("expected the original answer to pass through rather than an empty one")
	}
}

func TestSynDetectEnabled(t *testing.T) {
	set := healSetWith(false)
	if !synDetectEnabled(set) {
		t.Errorf("a new set with detection on should watch SYNs by default")
	}
	set.TCP.IPBlockDetect.SynDetect = false
	if synDetectEnabled(set) {
		t.Errorf("SYN watching should follow its own switch")
	}
	if synDetectEnabled(nil) {
		t.Errorf("a nil set must not enable SYN watching")
	}
}

func TestRecordDestAliveSurvivesNilCaches(t *testing.T) {
	health := iphealth.NewTracker(func(string, uint16, int) bool { return false })
	defer health.Stop()

	cases := map[string]*Worker{
		"nil host hints": {ipHealth: health, goodIPs: iphealth.NewKnownGood()},
		"nil good ips":   {ipHealth: health, hostHints: newHostHintCache()},
		"both nil":       {ipHealth: health},
		"nil ip health":  {goodIPs: iphealth.NewKnownGood(), hostHints: newHostHintCache()},
	}

	for name, w := range cases {
		t.Run(name, func(t *testing.T) {
			w.recordDestAlive("185.199.110.133", "192.168.1.10", true)
			w.recordDestAlive("185.199.110.133", "192.168.1.10", false)
			w.recordDestAlive("", "", true)
		})
	}

	var nilWorker *Worker
	nilWorker.recordDestAlive("1.2.3.4", "5.6.7.8", true)
}

func TestHealDNSResponseSubstitutesAnObservedAddress(t *testing.T) {
	w := healWorker(t, "157.240.205.174")
	w.goodIPs.Observe("www.instagram.com", net.ParseIP("157.240.205.174"))
	w.goodIPs.Observe("www.instagram.com", net.ParseIP("157.240.0.174"))

	resp := buildTestAResponse("www.instagram.com", "157.240.205.174")

	healed := w.healDNSResponse(&config.Config{}, healSetWith(true), "www.instagram.com", resp, false)
	if healed == nil {
		t.Fatal("expected the single dead address to be replaced by one seen earlier for the same name")
	}
	ips := dns.ParseResponseIPs(healed)
	if len(ips) != 1 || ips[0].String() != "157.240.0.174" {
		t.Errorf("addresses = %v, want [157.240.0.174]", ips)
	}
}

func TestHealDNSResponseNeverSubstitutesADeadAddress(t *testing.T) {
	w := healWorker(t, "1.1.1.1", "2.2.2.2")
	w.goodIPs.Observe("example.com", net.ParseIP("1.1.1.1"))
	w.goodIPs.Observe("example.com", net.ParseIP("2.2.2.2"))

	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	if healed := w.healDNSResponse(&config.Config{}, healSetWith(true), "example.com", resp, false); healed != nil {
		t.Errorf("every remembered address is unreachable, so the original must pass through rather than a dead substitute")
	}
}

func TestStoreHostHintsObservesOnlyForHealingSets(t *testing.T) {
	ips := []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2")}

	healing := healWorker(t)
	healing.storeHostHints(net.ParseIP("192.168.1.10"), healSetWith(true), "example.com", ips)
	if got := healing.goodIPs.Lookup("example.com", false); len(got) != 2 {
		t.Errorf("healing set recorded %d observed addresses, want 2", len(got))
	}

	plain := healWorker(t)
	plain.storeHostHints(net.ParseIP("192.168.1.10"), healSetWith(false), "example.com", ips)
	if got := plain.goodIPs.Lookup("example.com", false); len(got) != 0 {
		t.Errorf("a set without DNS healing recorded %v, want nothing kept", got)
	}
}

func pinnedSet(pins map[string][]string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "meta-set"
	set.Name = "meta"
	set.Enabled = true
	set.DNS.Pins = pins
	return &set
}

func TestPinnedAnswerReplacesTheAddress(t *testing.T) {
	w := healWorker(t)
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})

	query := dns.BuildQuery("www.instagram.com", 0x1234, 1)
	pinned := w.pinnedAnswer(set, query, "www.instagram.com")
	if pinned == nil {
		t.Fatal("expected a pinned answer for a subdomain of the pinned name")
	}

	ips := dns.ParseResponseIPs(pinned)
	if len(ips) != 1 || ips[0].String() != "157.240.0.174" {
		t.Fatalf("addresses = %v, want [157.240.0.174]", ips)
	}
	if domain, ok := dns.ParseQueryDomain(pinned); !ok || domain != "www.instagram.com" {
		t.Errorf("question = %q ok=%v, want the queried name preserved", domain, ok)
	}
	if pinned[2]&0x80 == 0 {
		t.Errorf("response bit not set, the client would discard this")
	}
}

func expectEmptyPinnedAnswer(t *testing.T, got []byte, query []byte, what string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: expected an empty answer, the query would otherwise reach the resolver and defeat the pin", what)
	}
	if got[2]&0x80 == 0 {
		t.Errorf("%s: response bit not set", what)
	}
	if got[3]&0x0F != 0 {
		t.Errorf("%s: rcode = %d, want NOERROR", what, got[3]&0x0F)
	}
	if got[0] != query[0] || got[1] != query[1] {
		t.Errorf("%s: txid = %x%x, want the query's %x%x", what, got[0], got[1], query[0], query[1])
	}
	if qd := binary.BigEndian.Uint16(got[4:6]); qd != 1 {
		t.Errorf("%s: qdcount = %d, want 1", what, qd)
	}
	if an := binary.BigEndian.Uint16(got[6:8]); an != 0 {
		t.Errorf("%s: ancount = %d, want 0", what, an)
	}
	if ips := dns.ParseResponseIPs(got); len(ips) != 0 {
		t.Errorf("%s: addresses = %v, want none", what, ips)
	}
	if qt, ok := dns.QuestionType(got); !ok || qt != mustQuestionType(query) {
		t.Errorf("%s: question type = %d ok=%v, want the query's %d", what, qt, ok, mustQuestionType(query))
	}
}

func mustQuestionType(query []byte) uint16 {
	qt, _ := dns.QuestionType(query)
	return qt
}

func TestPinnedAnswerAnswersOtherTypesEmpty(t *testing.T) {
	w := healWorker(t)
	set := pinnedSet(map[string][]string{"whatsapp.com": {"157.240.0.174"}})

	https := dns.BuildQuery("web.whatsapp.com", 0x4321, 65)
	expectEmptyPinnedAnswer(t, w.pinnedAnswer(set, https, "web.whatsapp.com"), https, "HTTPS query")

	aaaa := dns.BuildQuery("web.whatsapp.com", 0x2222, 28)
	expectEmptyPinnedAnswer(t, w.pinnedAnswer(set, aaaa, "web.whatsapp.com"), aaaa, "AAAA query against IPv4-only pins")

	cname := dns.BuildQuery("web.whatsapp.com", 9, 5)
	expectEmptyPinnedAnswer(t, w.pinnedAnswer(set, cname, "web.whatsapp.com"), cname, "CNAME query")

	svcb := dns.BuildQuery("web.whatsapp.com", 10, 64)
	expectEmptyPinnedAnswer(t, w.pinnedAnswer(set, svcb, "web.whatsapp.com"), svcb, "SVCB query")

	for _, other := range []struct {
		name  string
		qtype uint16
	}{{"MX", 15}, {"TXT", 16}, {"SRV", 33}, {"NS", 2}} {
		q := dns.BuildQuery("web.whatsapp.com", 7, other.qtype)
		if got := w.pinnedAnswer(set, q, "web.whatsapp.com"); got != nil {
			t.Errorf("%s query for a pinned name must reach the resolver, it carries no address the pin replaces", other.name)
		}
	}

	if action := w.applyPinnedAnswer(&config.Config{}, set, net.ParseIP("192.168.1.10"), "web.whatsapp.com", w.pinnedAnswer(set, https, "web.whatsapp.com")); action != dnsActionPinEmpty {
		t.Errorf("action = %q, want %q for an empty pin answer", action, dnsActionPinEmpty)
	}
	if action := w.applyPinnedAnswer(&config.Config{}, set, net.ParseIP("192.168.1.10"), "web.whatsapp.com", w.pinnedAnswer(set, dns.BuildQuery("web.whatsapp.com", 8, 1), "web.whatsapp.com")); action != dnsActionPin {
		t.Errorf("action = %q, want %q for an address answer", action, dnsActionPin)
	}
}

func TestPinnedAnswerSkipsUnpinnedNames(t *testing.T) {
	w := healWorker(t)
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})

	if got := w.pinnedAnswer(set, dns.BuildQuery("example.com", 1, 1), "example.com"); got != nil {
		t.Errorf("an unpinned name must not be answered")
	}
	if got := w.pinnedAnswer(pinnedSet(nil), dns.BuildQuery("www.instagram.com", 1, 1), "www.instagram.com"); got != nil {
		t.Errorf("a set without pins must not answer")
	}
	if got := w.pinnedAnswer(nil, dns.BuildQuery("www.instagram.com", 1, 1), "www.instagram.com"); got != nil {
		t.Errorf("a nil set must not answer")
	}
}

func TestPinnedAnswerServesIPv6ForAAAA(t *testing.T) {
	w := healWorker(t)
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174", "2a03:2880::1"}})

	pinned := w.pinnedAnswer(set, dns.BuildQuery("www.instagram.com", 1, 28), "www.instagram.com")
	if pinned == nil {
		t.Fatal("expected the IPv6 pin to answer an AAAA query")
	}
	ips := dns.ParseResponseIPs(pinned)
	if len(ips) != 1 || ips[0].String() != "2a03:2880::1" {
		t.Errorf("addresses = %v, want only the IPv6 pin", ips)
	}
}

func TestApplyPinnedAnswerRecordsTheHostHint(t *testing.T) {
	w := healWorker(t)
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})
	client := net.ParseIP("192.168.1.10")

	pinned := w.pinnedAnswer(set, dns.BuildQuery("www.instagram.com", 1, 1), "www.instagram.com")
	w.applyPinnedAnswer(&config.Config{}, set, client, "www.instagram.com", pinned)

	gotSet, host := w.lookupHostHint(&config.Config{Sets: []*config.SetConfig{set}}, client.String(), "157.240.0.174", "")
	if gotSet == nil || host != "www.instagram.com" {
		t.Errorf("host hint = %v/%q, want the pinned address tied back to the set so the connection still matches", gotSet, host)
	}
}

func pinWorker(t *testing.T, dead ...string) *Worker {
	t.Helper()
	w := healWorker(t)
	w.pinHealth = newPinHealth(func(context.Context, string, int) pinVerdict { return pinUnknown })
	t.Cleanup(w.pinHealth.stop)
	for _, ip := range dead {
		w.pinHealth.dead[ip] = struct{}{}
	}
	return w
}

func TestPinnedAnswerSkipsAddressesKnownToBeDead(t *testing.T) {
	w := pinWorker(t, "157.240.0.174")
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174", "157.240.253.174"}})
	if set.TCP.IPBlockDetect.Enabled {
		t.Fatal("this test relies on ip_block_detect being off by default")
	}

	query := dns.BuildQuery("www.instagram.com", 0x1234, 1)
	pinned := w.pinnedAnswer(set, query, "www.instagram.com")
	ips := dns.ParseResponseIPs(pinned)
	if len(ips) != 1 || ips[0].String() != "157.240.253.174" {
		t.Fatalf("a pin that stopped answering must not be handed out while another one works, got %v", ips)
	}

	only := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})
	got := w.pinnedAnswer(only, query, "www.instagram.com")
	if ips := dns.ParseResponseIPs(got); len(ips) != 1 || ips[0].String() != "157.240.0.174" {
		t.Fatalf("a pin that only the reachability check calls dead must still be answered when it is the only one, got %v", ips)
	}

	untracked := &Worker{goodIPs: iphealth.NewKnownGood(), hostHints: newHostHintCache()}
	if got := untracked.pinnedAnswer(only, query, "www.instagram.com"); got == nil {
		t.Fatal("without a liveness store, a pin is answered as configured")
	}
}

func TestPinnedAnswerIgnoresTheStoreForRoutedSets(t *testing.T) {
	w := pinWorker(t, "157.240.0.174")
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174", "157.240.253.174"}})
	set.Routing.Enabled = true

	ips := dns.ParseResponseIPs(w.pinnedAnswer(set, dns.BuildQuery("www.instagram.com", 1, 1), "www.instagram.com"))
	if len(ips) != 2 {
		t.Fatalf("a routed set's pins are reached through its own route, the router's direct-path verdict must not thin them, got %v", ips)
	}
}

func TestPinnedAnswerFallbackLeavesOutClientBlockedPins(t *testing.T) {
	w := pinWorker(t, "157.240.253.174")
	tracker := healWorker(t, "157.240.0.174")
	w.ipHealth = tracker.ipHealth
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174", "157.240.253.174"}})
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.SynDetect = true

	ips := dns.ParseResponseIPs(w.pinnedAnswer(set, dns.BuildQuery("www.instagram.com", 1, 1), "www.instagram.com"))
	if len(ips) != 1 || ips[0].String() != "157.240.253.174" {
		t.Fatalf("when the store calls the last candidate dead the answer falls back to it, never to the pin the clients' own SYNs proved blocked, got %v", ips)
	}
}

func TestPinnedAnswerHonoursBlockDetectionOnlyWhenEnabled(t *testing.T) {
	w := healWorker(t, "157.240.0.174")
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174", "157.240.253.174"}})
	query := dns.BuildQuery("www.instagram.com", 0x1234, 1)

	if ips := dns.ParseResponseIPs(w.pinnedAnswer(set, query, "www.instagram.com")); len(ips) != 2 {
		t.Fatalf("with ip_block_detect off the client-observed tracker must not thin the pins, got %v", ips)
	}

	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.SynDetect = true
	if ips := dns.ParseResponseIPs(w.pinnedAnswer(set, query, "www.instagram.com")); len(ips) != 1 || ips[0].String() != "157.240.253.174" {
		t.Fatalf("with syn_detect on a tracker-dead pin is skipped, got %v", ips)
	}

	only := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})
	only.TCP.IPBlockDetect.Enabled = true
	only.TCP.IPBlockDetect.SynDetect = true
	if got := w.pinnedAnswer(only, query, "www.instagram.com"); got != nil {
		t.Fatal("with syn_detect on, a query whose every pin is dead passes through to the resolver")
	}
}

func TestPinnedAnswerPassesIPv4QueryThroughForIPv6OnlyPins(t *testing.T) {
	w := healWorker(t)
	set := pinnedSet(map[string][]string{"instagram.com": {"2a03:2880::1"}})
	if got := w.pinnedAnswer(set, dns.BuildQuery("www.instagram.com", 1, 1), "www.instagram.com"); got != nil {
		t.Fatal("an A query for a name pinned to IPv6 only must reach the resolver, an IPv4-only client has nothing else")
	}
	v4 := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})
	aaaa := dns.BuildQuery("www.instagram.com", 2, 28)
	expectEmptyPinnedAnswer(t, w.pinnedAnswer(v4, aaaa, "www.instagram.com"), aaaa, "AAAA query against IPv4-only pins")
}

func TestPinnedAddressesCollectsEnabledSetsOnly(t *testing.T) {
	on := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174", "2A03:2880:0:0::1"}, "whatsapp.com": {"157.240.0.174", "157.240.253.174"}})
	off := pinnedSet(map[string][]string{"example.com": {"93.184.216.34"}})
	off.Enabled = false
	routed := pinnedSet(map[string][]string{"proxied.example": {"10.20.30.40"}})
	routed.Routing.Enabled = true
	cfg := &config.Config{Sets: []*config.SetConfig{on, off, routed}}

	got := pinnedAddresses(cfg)
	want := []string{"157.240.0.174", "157.240.253.174"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pins = %v, want %v (deduplicated, IPv4 only while IPv6 is off, disabled and routed sets skipped)", got, want)
	}

	cfg.Queue.IPv6Enabled = true
	got = pinnedAddresses(cfg)
	want = []string{"157.240.0.174", "157.240.253.174", "2a03:2880::1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pins with IPv6 = %v, want %v (canonical form)", got, want)
	}
}

func TestPinHealthRoundMarksOnlyTimeoutsDead(t *testing.T) {
	verdicts := map[string]pinVerdict{"157.240.0.174": pinDead, "157.240.253.174": pinAlive, "163.70.132.60": pinUnknown}
	h := newPinHealth(func(_ context.Context, ip string, _ int) pinVerdict { return verdicts[ip] })
	t.Cleanup(h.stop)
	pins := []string{"157.240.0.174", "157.240.253.174", "163.70.132.60"}

	h.check(pins, 0)
	if !waitFor(t, func() bool { return h.isDead("157.240.0.174") }) {
		t.Fatal("a pin whose connect timed out never became dead")
	}
	if h.isDead("157.240.253.174") || h.isDead("163.70.132.60") {
		t.Error("an answering pin and an inconclusive pin must not be dead")
	}
	if h.due(time.Hour) {
		t.Error("a round that just ran must not be due again before the retest interval")
	}
	if !h.due(0) {
		t.Error("a completed round must count as a run")
	}

	verdicts["157.240.0.174"] = pinAlive
	h.check(pins, 0)
	if !waitFor(t, func() bool { return !h.isDead("157.240.0.174") }) {
		t.Fatal("a pin that answers again must return to the answers")
	}
}

func TestPinHealthDiscardsARoundWhereNothingAnswered(t *testing.T) {
	h := newPinHealth(func(context.Context, string, int) pinVerdict { return pinDead })
	t.Cleanup(h.stop)
	h.dead["157.240.253.174"] = struct{}{}

	h.check([]string{"157.240.0.174", "157.240.253.174"}, 0)
	if !waitFor(t, func() bool { return h.due(0) }) {
		t.Fatal("round never finished")
	}
	if h.isDead("157.240.0.174") {
		t.Error("a round in which no pin answered proves nothing about any single pin and must not mark it dead")
	}
	if !h.isDead("157.240.253.174") {
		t.Error("a previous verdict survives an inconclusive round")
	}
}

func TestCheckDNSPinsSkipsDiscoveryAndStopsPromptly(t *testing.T) {
	probed := make(chan string, 8)
	blocking := newPinHealth(func(ctx context.Context, ip string, _ int) pinVerdict {
		probed <- ip
		<-ctx.Done()
		return pinUnknown
	})
	pool := &Pool{state: &runtimeState{pinHealth: blocking}}
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})

	discovery := &config.Config{Sets: []*config.SetConfig{set}}
	discovery.Queue.IsDiscovery = true
	pool.checkDNSPins(discovery)
	select {
	case ip := <-probed:
		t.Fatalf("a discovery pool must not probe pins, probed %s", ip)
	case <-time.After(20 * time.Millisecond):
	}

	pool.checkDNSPins(&config.Config{Sets: []*config.SetConfig{set}})
	select {
	case <-probed:
	case <-time.After(time.Second):
		t.Fatal("the pin was never probed")
	}
	done := make(chan struct{})
	go func() {
		blocking.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop must cancel an in-flight probe instead of waiting for it")
	}
}

func TestPinHealthChecksASaveMadeDuringARound(t *testing.T) {
	probed := make(chan string, 8)
	release := make(chan struct{})
	h := newPinHealth(func(ctx context.Context, ip string, _ int) pinVerdict {
		probed <- ip
		if ip == "157.240.0.174" {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return pinAlive
	})
	t.Cleanup(h.stop)

	h.check([]string{"157.240.0.174"}, 0)
	<-probed
	h.check([]string{"157.240.253.174"}, 0)
	h.check([]string{"163.70.132.60"}, 0)
	close(release)

	select {
	case ip := <-probed:
		if ip != "163.70.132.60" {
			t.Fatalf("the round after the running one probed %s, want the pins of the latest save", ip)
		}
	case <-time.After(time.Second):
		t.Fatal("a save made while a round was running was never checked, its pins would wait for the retest interval")
	}
	select {
	case ip := <-probed:
		t.Fatalf("only the latest save may run, %s was probed as well", ip)
	case <-time.After(50 * time.Millisecond):
	}
}
