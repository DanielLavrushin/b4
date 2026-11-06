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
		Name:    "fake",
		DNS:     config.DNSConfig{FakeIP: config.FakeIPConfig{Enabled: true, CIDR: cidr, TTLSec: 30}},
		Routing: config.RoutingConfig{Enabled: true, Mode: config.RoutingModeProxy},
	}
}

func fakeIPCfg() *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.Mode = "nfqueue"
	return &cfg
}

func TestFakeIPAnswerBindsTheNameToTheBlock(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	query := dns.BuildQuery("assets.example", 0x1234, dnsTypeA)
	resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "assets.example", net.ParseIP("192.168.1.20"))
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
	if again := dns.ParseResponseIPs((&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "assets.example", nil)); len(again) != 1 || !again[0].Equal(ips[0]) {
		t.Fatalf("the same name got %v and %v", ips, again)
	}
}

func TestFakeIPAnswerEmptiesTheAAAAQuery(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	query := dns.BuildQuery("assets.example", 0x1235, dnsTypeAAAA)
	resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "assets.example", net.ParseIP("192.168.1.20"))
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
	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "assets.example", nil); resp != nil {
		t.Fatalf("an answer was built with fake DNS off: %v", dns.ParseResponseIPs(resp))
	}
}

func TestFakeIPAnswerLeavesOtherTypesAlone(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	query := dns.BuildQuery("assets.example", 0x1237, 16)
	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "assets.example", nil); resp != nil {
		t.Fatal("a TXT query was answered by fake DNS")
	}
	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "", nil); resp != nil {
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
	resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "late.example", net.ParseIP("192.168.1.20"))
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

func fakeScope(name, mac string, router bool) *ingressScope {
	return &ingressScope{name: name, mac: mac, router: router, resolved: true}
}

func TestFakeIPAnswerSkipsAClientOutsideTheSetsInterfaces(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	set.Routing.SourceInterfaces = []string{"lan1"}
	query := dns.BuildQuery("outside.example", 0x1301, dnsTypeA)

	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "outside.example", net.ParseIP("192.168.2.30"), fakeScope("lan2", "aa:bb:cc:dd:ee:01", false)); resp != nil {
		t.Fatalf("a client on lan2 got %v from a set scoped to lan1", dns.ParseResponseIPs(resp))
	}
	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "outside.example", net.ParseIP("192.168.1.30"), fakeScope("lan1", "aa:bb:cc:dd:ee:01", false)); resp == nil {
		t.Fatal("a client on lan1 got no fake answer")
	}
}

func TestFakeIPAnswerSkipsTheRoutersOwnQuery(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	query := dns.BuildQuery("routerasked.example", 0x1302, dnsTypeA)
	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "routerasked.example", net.ParseIP("192.168.0.1"), fakeScope("", "", true)); resp != nil {
		t.Fatalf("the router's own query got %v, and no rule would carry that address anywhere", dns.ParseResponseIPs(resp))
	}
}

func TestFakeIPAnswerSkipsTheSetsOwnResolver(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	set.DNS.TargetDNS = "192.168.1.5"
	query := dns.BuildQuery("ownresolver.example", 0x1303, dnsTypeA)

	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "ownresolver.example", net.ParseIP("192.168.1.5"), nil); resp != nil {
		t.Fatalf("the set's own resolver got %v and could not resolve it anywhere", dns.ParseResponseIPs(resp))
	}
	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "ownresolver.example", net.ParseIP("192.168.1.30"), nil); resp == nil {
		t.Fatal("a device got no fake answer")
	}
}

func TestFakeIPAnswerSkipsADeviceScopedSetForAnUnattributedQuery(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	set.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:02"}
	query := dns.BuildQuery("devicescoped.example", 0x1304, dnsTypeA)

	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "devicescoped.example", net.ParseIP("192.168.1.30"), &ingressScope{resolved: true}); resp != nil {
		t.Fatalf("a query with no MAC got %v from a device-scoped set", dns.ParseResponseIPs(resp))
	}
	if resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "devicescoped.example", net.ParseIP("192.168.1.30"), fakeScope("lan1", "aa:bb:cc:dd:ee:02", false)); resp == nil {
		t.Fatal("a scoped device got no fake answer")
	}
}

func TestFakeIPAnswerEmptiesTheHintTypes(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	for _, qtype := range []uint16{dnsTypeHTTPS, dnsTypeSVCB, dnsTypeCNAME, dnsTypeANY} {
		query := dns.BuildQuery("hints.example", 0x1305, qtype)
		resp := (&Worker{}).fakeIPAnswer(fakeIPCfg(), set, query, "hints.example", net.ParseIP("192.168.1.30"), nil)
		if len(resp) == 0 {
			t.Fatalf("type %d reached a real resolver instead of an empty answer", qtype)
		}
		if ips := dns.ParseResponseIPs(resp); len(ips) != 0 {
			t.Fatalf("type %d answered %v, want no addresses", qtype, ips)
		}
		if rcode, _ := dns.ResponseRcode(resp); rcode != 0 {
			t.Fatalf("type %d answered with rcode %d, want NOERROR", qtype, rcode)
		}
	}
}

func TestFakeIPAnswerStaysOffWhileIPv4IsNotCaptured(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	cfg := fakeIPCfg()
	cfg.Queue.IPv4Enabled = false
	query := dns.BuildQuery("nocapture.example", 0x1306, dnsTypeA)

	if resp := (&Worker{}).fakeIPAnswer(cfg, set, query, "nocapture.example", net.ParseIP("192.168.1.30")); resp != nil {
		t.Fatalf("an A answer was faked while IPv4 capture is off: %v", dns.ParseResponseIPs(resp))
	}
}

func TestFakeIPAnswerStaysOffWhileTheRouteIsNotInstalled(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	cfg := fakeIPCfg()
	config.SetRoutingLiveProbe(func(string) bool { return false })
	t.Cleanup(func() { config.SetRoutingLiveProbe(nil) })
	query := dns.BuildQuery("noroute.example", 0x1307, dnsTypeA)
	client := net.ParseIP("192.168.1.30")

	if resp := (&Worker{}).fakeIPAnswer(cfg, set, query, "noroute.example", client); resp != nil {
		t.Fatalf("an answer was faked while the set has no route: %v", dns.ParseResponseIPs(resp))
	}
	config.SetRoutingLiveProbe(func(string) bool { return true })
	if resp := (&Worker{}).fakeIPAnswer(cfg, set, query, "noroute.example", client); resp == nil {
		t.Fatal("a live route got no fake answer")
	}
}

func TestFakeIPAnswerTrustsTheTunEngineForTheRoute(t *testing.T) {
	set := fakeIPSet("198.18.0.0/15")
	cfg := fakeIPCfg()
	cfg.Queue.Mode = "tun"
	config.SetRoutingLiveProbe(func(string) bool { return false })
	t.Cleanup(func() { config.SetRoutingLiveProbe(nil) })
	query := dns.BuildQuery("tun.example", 0x1308, dnsTypeA)

	if resp := (&Worker{}).fakeIPAnswer(cfg, set, query, "tun.example", net.ParseIP("192.168.1.30")); resp == nil {
		t.Fatal("the tun engine routes the block itself, so the answer must still be faked")
	}
}
