package nfq

import (
	"net"
	"strconv"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

func fakeIPSet(cidr string) *config.SetConfig {
	return &config.SetConfig{
		Name: "fake",
		DNS:  config.DNSConfig{FakeIP: config.FakeIPConfig{Enabled: true, CIDR: cidr, TTLSec: 30}},
	}
}

func TestFakeIPAnswerBindsTheNameToTheBlock(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	query := dns.BuildQuery("assets.example", 0x1234, dnsTypeA)
	resp := (&Worker{}).fakeIPAnswer(set, query, "assets.example", net.ParseIP("192.168.1.20"))
	if resp == nil {
		t.Fatal("no answer for an A query")
	}
	ips := dns.ParseResponseIPs(resp)
	if len(ips) != 1 {
		t.Fatalf("answer carries %d addresses, want one", len(ips))
	}
	network, _, _ := set.DNS.FakeIPNetwork()
	if !network.Contains(ips[0]) {
		t.Fatalf("%s is outside %s", ips[0], network)
	}
	if again := dns.ParseResponseIPs((&Worker{}).fakeIPAnswer(set, query, "assets.example", nil)); len(again) != 1 || !again[0].Equal(ips[0]) {
		t.Fatalf("the same name got %v and %v", ips, again)
	}
}

func TestFakeIPAnswerEmptiesTheAAAAQuery(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	query := dns.BuildQuery("assets.example", 0x1235, dnsTypeAAAA)
	resp := (&Worker{}).fakeIPAnswer(set, query, "assets.example", net.ParseIP("192.168.1.20"))
	if resp == nil {
		t.Fatal("no answer for an AAAA query")
	}
	if ips := dns.ParseResponseIPs(resp); len(ips) != 0 {
		t.Fatalf("the AAAA answer carries %v, want no addresses", ips)
	}
}

func TestFakeIPAnswerStaysOffWithoutTheSwitch(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	set.DNS.FakeIP.Enabled = false
	query := dns.BuildQuery("assets.example", 0x1236, dnsTypeA)
	if resp := (&Worker{}).fakeIPAnswer(set, query, "assets.example", nil); resp != nil {
		t.Fatalf("an answer was built with fake DNS off: %v", dns.ParseResponseIPs(resp))
	}
}

func TestFakeIPAnswerLeavesOtherTypesAlone(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	query := dns.BuildQuery("assets.example", 0x1237, 16)
	if resp := (&Worker{}).fakeIPAnswer(set, query, "assets.example", nil); resp != nil {
		t.Fatal("a TXT query was answered by fake DNS")
	}
	if resp := (&Worker{}).fakeIPAnswer(set, query, "", nil); resp != nil {
		t.Fatal("a query without a name was answered by fake DNS")
	}
}

func TestFakeIPAnswerServesServfailWhenTheBlockIsFull(t *testing.T) {
	old := fakeIPPool
	pool := dns.NewFakeIPPool()
	fakeIPPool = pool
	t.Cleanup(func() { fakeIPPool = old })

	set := fakeIPSet("198.18.0.0/24")
	network, _, _ := set.DNS.FakeIPNetwork()
	for i := 0; i < 254; i++ {
		if ip := pool.Address("full"+strconv.Itoa(i)+".example", network); ip == nil {
			t.Fatalf("the block stopped at %d leased names", i)
		}
	}

	query := dns.BuildQuery("late.example", 0x1240, dnsTypeA)
	resp := (&Worker{}).fakeIPAnswer(set, query, "late.example", net.ParseIP("192.168.1.20"))
	if len(resp) == 0 {
		t.Fatal("an exhausted block must still send an answer so the client stops waiting")
	}
	if rcode, ok := dns.ResponseRcode(resp); !ok || rcode != rcodeServfail {
		t.Fatalf("rcode = %d (%v), want a SERVFAIL", rcode, ok)
	}
	if ips := dns.ParseResponseIPs(resp); len(ips) != 0 {
		t.Fatalf("an exhausted block still handed out %v", ips)
	}
	if owner, ok := pool.Owner(net.ParseIP("198.18.0.1")); ok && owner == "late.example" {
		t.Fatal("the late name took an address that belongs to another name")
	}
}
