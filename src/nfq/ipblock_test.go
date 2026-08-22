package nfq

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/iphealth"
)

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

func TestPinnedAnswerSkipsWrongFamilyAndTypes(t *testing.T) {
	w := healWorker(t)
	set := pinnedSet(map[string][]string{"instagram.com": {"157.240.0.174"}})

	if got := w.pinnedAnswer(set, dns.BuildQuery("www.instagram.com", 1, 28), "www.instagram.com"); got != nil {
		t.Errorf("an AAAA query must not be answered from an IPv4-only pin, got %d bytes", len(got))
	}
	if got := w.pinnedAnswer(set, dns.BuildQuery("www.instagram.com", 1, 15), "www.instagram.com"); got != nil {
		t.Errorf("an MX query must not be answered from a pin")
	}
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
