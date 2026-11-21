package nfq

import (
	"net/netip"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/warmtuple"
)

func TestWSSizeACollectedPublishKeepsTheRewriteOfAnOpenConnection(t *testing.T) {
	cfg, set := wsFixture()
	set.TCP.WSSize.Cutoff = wsHello
	consumer := newPassiveSet()
	consumer.Id, consumer.Name = "collected-consumer", "collected-consumer"
	consumer.Targets.DomainOnly = false
	consumer.Targets.DomainsToMatch = nil
	consumer.Targets.UseCollectedIPs = true
	cfg.Sets = append(cfg.Sets, &consumer)
	before := cfg.WithCollectedIPs([]string{"203.0.113.8"})
	after := cfg.WithCollectedIPs([]string{"203.0.113.8", "203.0.113.9"})
	if before == cfg || after == cfg || before == after {
		t.Fatal("setup: the collected views share a pointer with each other or with the saved config, so this test proves nothing")
	}
	hello := buildClientHello("collect.example", 300, 0xab)
	c, now := newWSSizeCache(), time.Now()
	if !c.rewrite(wsPacket(IPv4, 40000, 100, 0x02, wsKernelSynOptions, nil), 20, before.Sets[0], before, now) {
		t.Fatal("setup: the SYN was not rewritten")
	}
	ack := wsPacket(IPv4, 40000, 101, 0x10, nil, nil)
	c.rewrite(ack, 20, nil, after, now)
	if got := wsWindow(ack); got != 1 {
		t.Fatalf("the handshake ACK of a connection whose SYN was rewritten left with window %d after a collected publish, want the set's 1; a publish changes only the collected addresses, so the connection's WSSize record must survive it", got)
	}
	part := wsPacket(IPv4, 40000, 101, 0x18, nil, hello[:40])
	c.rewrite(part, 20, nil, after, now)
	if got := wsWindow(part); got != 1 {
		t.Errorf("the first ClientHello segment left with window %d after a collected publish, want the set's 1", got)
	}
	if len(c.flows) != 1 {
		t.Errorf("%d WSSize records held after the first ClientHello segment, want the connection's 1", len(c.flows))
	}

	flows := warmtuple.NewFlows()
	local, remote := netip.MustParseAddrPort("10.0.0.1:63001"), netip.MustParseAddrPort("93.184.216.34:443")
	release, err := flows.Register(local, remote, before, before.Sets[0], "collect.example")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	profile := flows.Lookup(local, remote).Config
	if !c.rewrite(wsPacket(IPv4, 40001, 100, 0x02, wsKernelSynOptions, nil), 20, profile.Sets[0], profile, now) {
		t.Fatal("setup: the warm SYN was not rewritten")
	}
	warmAck := wsPacket(IPv4, 40001, 101, 0x10, nil, nil)
	c.rewrite(warmAck, 20, nil, before, now)
	if got := wsWindow(warmAck); got != wsKernelWindow {
		t.Errorf("a packet that arrived with the live config after its warm flow stopped matching left with window %d, want its own %d; a warm profile is its own config, not a collected view", got, wsKernelWindow)
	}
}
