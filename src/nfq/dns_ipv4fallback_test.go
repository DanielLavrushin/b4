package nfq

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/iphealth"
)

func buildTestMixedResponse(domain string, qtype uint16, ips ...string) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x7f7f)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(len(ips)))

	msg = append(msg, encodeTestName(domain)...)
	var q [4]byte
	binary.BigEndian.PutUint16(q[0:2], qtype)
	binary.BigEndian.PutUint16(q[2:4], 1)
	msg = append(msg, q[:]...)

	for _, raw := range ips {
		ip := net.ParseIP(raw)
		rdata := ip.To4()
		rrType := uint16(dnsTypeA)
		if rdata == nil {
			rdata = ip.To16()
			rrType = dnsTypeAAAA
		}
		msg = append(msg, 0xC0, 0x0C)
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], rrType)
		binary.BigEndian.PutUint16(fixed[2:4], 1)
		binary.BigEndian.PutUint32(fixed[4:8], 300)
		binary.BigEndian.PutUint16(fixed[8:10], uint16(len(rdata)))
		msg = append(msg, fixed[:]...)
		msg = append(msg, rdata...)
	}
	return msg
}

func fallbackWorker(t *testing.T) *Worker {
	t.Helper()
	health := iphealth.NewTracker(func(string, uint16, int) bool { return false })
	t.Cleanup(health.Stop)
	return &Worker{ipHealth: health, goodIPs: iphealth.NewKnownGood(), hostHints: newHostHintCache()}
}

func fallbackSet() *config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "youtube"
	set.Enabled = true
	return &set
}

func TestStripIPv6AddressesDropsAAAAFromMixedAnswer(t *testing.T) {
	w := fallbackWorker(t)
	resp := buildTestMixedResponse("youtube.com", dnsTypeA, "142.250.74.14", "2a00:1450:4010:c0d::5d")

	out, action := w.filterDNSAnswer(&config.Config{}, fallbackSet(), "youtube.com", resp, false)
	if out == nil {
		t.Fatal("expected the IPv6 addresses to be stripped")
	}
	if action != dnsActionIPv6Stripped {
		t.Errorf("action = %q, want %q", action, dnsActionIPv6Stripped)
	}
	ips := dns.ParseResponseIPs(out)
	if len(ips) != 1 || ips[0].String() != "142.250.74.14" {
		t.Errorf("addresses = %v, want [142.250.74.14]", ips)
	}
}

func TestStripIPv6AddressesAnswersEmptyForAAAAOnlyAnswer(t *testing.T) {
	w := fallbackWorker(t)
	resp := buildTestMixedResponse("youtube.com", dnsTypeAAAA,
		"2a00:1450:4010:c0d::5d", "2a00:1450:4010:c0d::88")

	out, action := w.filterDNSAnswer(&config.Config{}, fallbackSet(), "youtube.com", resp, false)
	if out == nil {
		t.Fatal("expected an empty NOERROR answer so the client falls back to IPv4")
	}
	if action != dnsActionIPv6Stripped {
		t.Errorf("action = %q, want %q", action, dnsActionIPv6Stripped)
	}
	if rcode, ok := dns.ResponseRcode(out); !ok || rcode != dns.RcodeNoError {
		t.Errorf("rcode = %d ok=%v, want NOERROR", rcode, ok)
	}
	if got := binary.BigEndian.Uint16(out[6:8]); got != 0 {
		t.Errorf("ANCOUNT = %d, want 0", got)
	}
	if len(dns.ParseResponseIPs(out)) != 0 {
		t.Errorf("the empty answer still carries addresses")
	}
}

func TestStripIPv6AddressesLeavesIPv4OnlyAnswerAlone(t *testing.T) {
	w := fallbackWorker(t)
	resp := buildTestAResponse("youtube.com", "142.250.74.14", "142.250.74.15")

	if out, action := w.filterDNSAnswer(&config.Config{}, fallbackSet(), "youtube.com", resp, false); out != nil {
		t.Errorf("an IPv4-only answer must pass through untouched, got action %q", action)
	}
}

func TestStripIPv6AddressesSkippedWhenIPv6Processed(t *testing.T) {
	w := fallbackWorker(t)
	cfg := &config.Config{}
	cfg.Queue.IPv6Enabled = true
	resp := buildTestMixedResponse("youtube.com", dnsTypeAAAA, "2a00:1450:4010:c0d::5d")

	if out, _ := w.filterDNSAnswer(cfg, fallbackSet(), "youtube.com", resp, false); out != nil {
		t.Errorf("AAAA answers must survive while b4 processes IPv6")
	}
}

func TestStripIPv6AddressesHonoursKeepIPv6Answers(t *testing.T) {
	w := fallbackWorker(t)
	cfg := &config.Config{}
	cfg.System.DNS.KeepIPv6Answers = true
	resp := buildTestMixedResponse("youtube.com", dnsTypeAAAA, "2a00:1450:4010:c0d::5d")

	if out, _ := w.filterDNSAnswer(cfg, fallbackSet(), "youtube.com", resp, false); out != nil {
		t.Errorf("keep_ipv6_answers must switch the fallback off")
	}
}

func TestStripIPv6AddressesSkippedForIPv6OnlySet(t *testing.T) {
	w := fallbackWorker(t)
	set := fallbackSet()
	set.Targets.IPVersion = "6"
	resp := buildTestMixedResponse("youtube.com", dnsTypeAAAA, "2a00:1450:4010:c0d::5d")

	if out, _ := w.filterDNSAnswer(&config.Config{}, set, "youtube.com", resp, false); out != nil {
		t.Errorf("a set that only targets IPv6 must keep its AAAA answers")
	}
}

func TestStripIPv6AddressesSkippedInDiscovery(t *testing.T) {
	w := fallbackWorker(t)
	cfg := &config.Config{}
	cfg.Queue.IsDiscovery = true
	resp := buildTestMixedResponse("youtube.com", dnsTypeAAAA, "2a00:1450:4010:c0d::5d")

	if out, _ := w.filterDNSAnswer(cfg, fallbackSet(), "youtube.com", resp, false); out != nil {
		t.Errorf("a discovery run must not rewrite DNS answers")
	}
}

func TestFilterDNSAnswerLeavesUnmatchedDomainsAlone(t *testing.T) {
	w := fallbackWorker(t)
	resp := buildTestMixedResponse("example.com", dnsTypeAAAA, "2a00:1450:4010:c0d::5d")

	if out, _ := w.filterDNSAnswer(&config.Config{}, nil, "example.com", resp, false); out != nil {
		t.Errorf("a domain no set matched must keep its IPv6 answer")
	}
}

func TestFilterDNSAnswerAppliesHealAndStripTogether(t *testing.T) {
	w := healWorker(t, "142.250.74.15")
	set := healSetWith(true)
	resp := buildTestMixedResponse("youtube.com", dnsTypeA,
		"142.250.74.14", "142.250.74.15", "2a00:1450:4010:c0d::5d")

	out, action := w.filterDNSAnswer(&config.Config{}, set, "youtube.com", resp, false)
	if out == nil {
		t.Fatal("expected both the unreachable address and the IPv6 one to be dropped")
	}
	if action != dnsActionHealStripped {
		t.Errorf("action = %q, want %q", action, dnsActionHealStripped)
	}
	ips := dns.ParseResponseIPs(out)
	if len(ips) != 1 || ips[0].String() != "142.250.74.14" {
		t.Errorf("addresses = %v, want [142.250.74.14]", ips)
	}
}

func TestDNSActionHealStrippedNamesBothActions(t *testing.T) {
	if !strings.HasPrefix(dnsActionHealStripped, dnsActionHeal+"+") {
		t.Errorf("%q must open with %q so a heal stays visible next to the strip", dnsActionHealStripped, dnsActionHeal)
	}
	if !strings.HasSuffix(dnsActionHealStripped, strings.TrimPrefix(dnsActionIPv6Stripped, "dns-")) {
		t.Errorf("%q must close with %q so the strip stays visible next to the heal", dnsActionHealStripped, dnsActionIPv6Stripped)
	}
	if dnsActionHealStripped == dnsActionHeal || dnsActionHealStripped == dnsActionIPv6Stripped {
		t.Errorf("%q must be its own action, not one of the two it composes", dnsActionHealStripped)
	}
}

func TestFilterDNSAnswerReportsHealWhenOnlyHealingApplies(t *testing.T) {
	w := healWorker(t, "142.250.74.15")
	resp := buildTestAResponse("youtube.com", "142.250.74.14", "142.250.74.15")

	out, action := w.filterDNSAnswer(&config.Config{}, healSetWith(true), "youtube.com", resp, false)
	if out == nil {
		t.Fatal("expected the unreachable address to be dropped")
	}
	if action != dnsActionHeal {
		t.Errorf("action = %q, want %q", action, dnsActionHeal)
	}
}

func TestIPv6AnswersKept(t *testing.T) {
	set := fallbackSet()
	if ipv6AnswersKept(&config.Config{}, set) {
		t.Errorf("an IPv4-only b4 must strip AAAA for a matched set by default")
	}
	if !ipv6AnswersKept(nil, set) {
		t.Errorf("a nil config must not trigger stripping")
	}
	if !ipv6AnswersKept(&config.Config{}, nil) {
		t.Errorf("a nil set must not trigger stripping")
	}
}
