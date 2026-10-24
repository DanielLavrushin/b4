package nfq

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/florianl/go-nfqueue"
)

func rowsFor(lines []string, name string) int {
	n := 0
	for _, l := range lines {
		if f := strings.Split(l, ","); len(f) > 5 && (f[2] == name || f[5] == name) {
			n++
		}
	}
	return n
}

func guestBlockAndDPI() (*config.SetConfig, *config.SetConfig) {
	block := blockYTImages(config.BlockActionDrop, true)
	block.Targets.DomainOnly = false
	dpi := newFragmentingYTImagesSet()
	dpi.Id, dpi.Name = "dpi", "DPI"
	dpi.Targets.DomainOnly = false
	return block, &dpi
}

func guestBlockFor(domain string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "block", "Block", true
	set.Targets.DomainsToMatch = []string{domain}
	return scopeToGuest(&set, config.BlockActionDrop)
}

func appDataFrom(src string, sport uint16) []byte {
	raw := makeV4TCPPacketFromPort(tlsAppData(32), 2000, sport, 0x18)
	copy(raw[12:16], net.ParseIP(src).To4())
	return raw
}

func helloNaming(host, src string, sport uint16) []byte {
	raw := makeV4TCPPacketFromPort(buildClientHello(host, 16, 0xAB), 1000, sport, 0x18)
	copy(raw[12:16], net.ParseIP(src).To4())
	return raw
}

func TestAQUICFlowFromAnotherInterfaceGoesToTheNextSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	block := newDomainOnlyQUICSet(config.ConfigOff)
	block.Id, block.Name = "block", "Block"
	scopeToGuest(&block, config.BlockActionDrop)
	dpi := newDomainOnlyQUICSet(config.ConfigOff)
	dpi.Id, dpi.Name = "dpi", "DPI"
	w := newQUICFlowWorker(t, &block, &dpi)

	for _, c := range []struct {
		name  string
		in    uint32
		sport uint16
		cid   byte
		want  int
		why   string
	}{
		{"br0", idx["br0"], 50110, 0x71, nfqueue.NfAccept, "the DPI set takes the flow from br0, and it sends its UDP on unchanged"},
		{"br-guest", idx["br-guest"], 50111, 0x73, nfqueue.NfDrop, "the block set stops the flow from br-guest"},
	} {
		t.Run(c.name, func(t *testing.T) {
			flow := newSplitQUICHello(t, []byte{c.cid, 1, 2, 3, 4, 5, 6, 7}, []byte{c.cid + 1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
			first := makeV4UDPPacket(flow.first, quicClient, quicServer, c.sport, 443)
			if got := queuedVerdict(t, w, first, nil, 0, c.in); got != nfqueue.NfAccept {
				t.Fatalf("verdict %d: the first datagram carries no complete ClientHello yet", got)
			}
			for i, p := range [][]byte{flow.completing, flow.resend, flow.oneRTT} {
				raw := makeV4UDPPacket(p, quicClient, quicServer, c.sport, 443)
				if got := queuedVerdict(t, w, raw, nil, 0, c.in); got != c.want {
					t.Fatalf("datagram %d: verdict %d, want %d: %s", i+2, got, c.want, c.why)
				}
			}
		})
	}
}

func TestAPortOnlyScopedBlockSetLeavesOtherInterfacesToTheNextPortSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)

	t.Run("TCP", func(t *testing.T) {
		portSet := func(id, name string) *config.SetConfig {
			set := newFragmentingYTImagesSet()
			set.Id, set.Name = id, name
			set.Targets.DomainOnly = false
			set.Targets.DomainsToMatch = nil
			set.TCP.DPortFilter = "443"
			set.TCP.RSTProtection.Enabled = false
			return &set
		}
		w := newInjectingWorker(t, scopeToGuest(portSet("block", "Block"), config.BlockActionDrop), portSet("dpi", "DPI"))
		captureInjected(t, w)
		lines := connectionLines(t)

		queuedVerdict(t, w, helloNaming("www.unlisted.example", "10.0.0.1", 12500), nil, 0, idx["br0"])
		if seen := drainLines(lines); rowsFor(seen, "DPI") != 1 || rowsFor(seen, "Block") != 0 {
			t.Fatalf("a connection from br0 on the shared port belongs to the DPI port set, got %v", seen)
		}
		queuedVerdict(t, w, helloNaming("www.unlisted.example", "10.0.0.2", 12501), nil, 0, idx["br-guest"])
		if seen := drainLines(lines); rowsFor(seen, "Block") != 1 || rowsFor(seen, "DPI") != 0 {
			t.Fatalf("a connection from br-guest on the shared port belongs to the block set, got %v", seen)
		}
	})

	t.Run("UDP", func(t *testing.T) {
		portSet := func(id, name string) *config.SetConfig {
			set := config.NewSetConfig()
			set.Id, set.Name, set.Enabled = id, name, true
			set.UDP.DPortFilter = "50300"
			set.UDP.Mode = config.ConfigOff
			return &set
		}
		w := newQUICFlowWorker(t, scopeToGuest(portSet("block", "Block"), config.BlockActionDrop), portSet("dpi", "DPI"))
		lines := connectionLines(t)
		datagram := func(sport uint16) []byte {
			return makeV4UDPPacket([]byte("plain udp payload"), quicClient, quicServer, sport, 50300)
		}

		queuedVerdict(t, w, datagram(52100), nil, 0, idx["br0"])
		if seen := drainLines(lines); rowsFor(seen, "DPI") != 1 || rowsFor(seen, "Block") != 0 {
			t.Fatalf("a datagram from br0 on the shared port belongs to the DPI port set, got %v", seen)
		}
		queuedVerdict(t, w, datagram(52101), nil, 0, idx["br-guest"])
		if seen := drainLines(lines); rowsFor(seen, "Block") != 1 || rowsFor(seen, "DPI") != 0 {
			t.Fatalf("a datagram from br-guest on the shared port belongs to the block set, got %v", seen)
		}
	})
}

func TestALearnedAddressOfAScopedBlockSetGoesToTheNextSetOnOtherInterfaces(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	for _, proto := range []string{"TCP", "UDP"} {
		t.Run(proto, func(t *testing.T) {
			block, dpi := guestBlockAndDPI()
			w := newInjectingWorker(t, block, dpi)
			captureInjected(t, w)
			w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", block)
			lines := connectionLines(t)
			packet := func(src string, sport uint16) []byte {
				if proto == "TCP" {
					return appDataFrom(src, sport)
				}
				return makeV4UDPPacket([]byte("plain udp payload"), net.ParseIP(src), quicServer, sport, 443)
			}

			queuedVerdict(t, w, packet("10.0.0.1", 12510), nil, 0, idx["br0"])
			if seen := drainLines(lines); rowsFor(seen, "DPI") != 1 || rowsFor(seen, "Block") != 0 {
				t.Fatalf("the address the block set learned goes to the DPI set for its domain from br0, got %v", seen)
			}
			queuedVerdict(t, w, packet("10.0.0.2", 12511), nil, 0, idx["br-guest"])
			seen := drainLines(lines)
			if proto == "TCP" && rowsFor(seen, "DPI") != 0 {
				t.Fatalf("from br-guest the learned address stays with the block set, got %v", seen)
			}
			if proto == "UDP" && blockRows(seen) != 1 {
				t.Fatalf("from br-guest the learned domain is blocked, got %v", seen)
			}
		})
	}
}

func TestAHostHintNamingAScopedBlockSetIsSkippedOnOtherInterfaces(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	block, dpi := guestBlockAndDPI()
	w := newInjectingWorker(t, block, dpi)
	captureInjected(t, w)
	lines := connectionLines(t)

	for _, c := range []struct {
		client string
		in     uint32
		want   int
		why    string
	}{
		{"10.0.0.1", idx["br0"], 0, "a hint that names the block set does not apply to a client on br0"},
		{"10.0.0.2", idx["br-guest"], 1, "the hint applies to a client on br-guest"},
	} {
		w.hostHints.Store(c.client, "1.2.3.4", block.Id, "i.ytimg.com")
		queuedVerdict(t, w, appDataFrom(c.client, 12520), nil, 0, c.in)
		if seen := drainLines(lines); rowsFor(seen, "Block") != c.want {
			t.Fatalf("%s: %d rows name the block set, want %d: %s (%v)", c.client, rowsFor(seen, "Block"), c.want, c.why, seen)
		}
	}
}

func TestAfterAHintDropTheLearnedAddressSkipsAScopedBlockSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	for _, escalated := range []bool{false, true} {
		block, dpi := guestBlockAndDPI()
		w := newInjectingWorker(t, block, dpi)
		captureInjected(t, w)
		w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", block)
		if escalated {
			w.destState.SetEscalation("i.ytimg.com", block.Id, escalateReasonStall, time.Hour)
		}
		lines := connectionLines(t)

		for _, c := range []struct {
			client string
			in     uint32
			want   string
		}{
			{"10.0.0.1", idx["br0"], "DPI"},
			{"10.0.0.2", idx["br-guest"], "Block"},
		} {
			w.hostHints.Store(c.client, "1.2.3.4", dpi.Id, "i.ytimg.com")
			queuedVerdict(t, w, helloNaming("www.unlisted.example", c.client, 12530), nil, 0, c.in)
			if seen := drainLines(lines); len(seen) != 1 || rowsFor(seen, c.want) != 1 {
				t.Fatalf("escalated=%v, %s: a ClientHello naming no set drops the hint, and the learned address goes to %s, got %v", escalated, c.client, c.want, seen)
			}
		}
	}
}

func escalatingToGuestBlock() (*config.SetConfig, *config.SetConfig) {
	dpi := newFragmentingYTImagesSet()
	dpi.Id, dpi.Name = "dpi", "DPI"
	dpi.Targets.DomainOnly = false
	dpi.TCP.RSTProtection.Enabled = false
	dpi.Escalate.To = "block"
	return &dpi, guestBlockFor("unused.example")
}

func TestAnEscalationToAScopedBlockSetAppliesOnlyOnItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)

	t.Run("a ClientHello", func(t *testing.T) {
		dpi, block := escalatingToGuestBlock()
		w := newInjectingWorker(t, dpi, block)
		captureInjected(t, w)
		w.destState.SetEscalation("i.ytimg.com", block.Id, escalateReasonStall, time.Hour)
		lines := connectionLines(t)

		if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12540), nil, 0, idx["br0"]); got != nfqueue.NfDrop {
			t.Fatalf("verdict %d: the DPI set re-sends the ClientHello from br0", got)
		}
		if seen := drainLines(lines); blockRows(seen) != 0 || rowsFor(seen, "DPI") == 0 {
			t.Fatalf("the escalation to the block set does not apply on br0, got %v", seen)
		}
		if got := queuedVerdict(t, w, helloFrom("10.0.0.2", 12541), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
			t.Fatalf("verdict %d: the escalated block set takes the ClientHello from br-guest", got)
		}
		if seen := drainLines(lines); blockRows(seen) != 1 {
			t.Fatalf("want one block row for br-guest, got %v", seen)
		}
	})

	t.Run("a learned address before its ClientHello", func(t *testing.T) {
		dpi, block := escalatingToGuestBlock()
		w := newInjectingWorker(t, dpi, block)
		captureInjected(t, w)
		w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", dpi)
		w.destState.SetEscalation("i.ytimg.com", block.Id, escalateReasonStall, time.Hour)
		lines := connectionLines(t)

		queuedVerdict(t, w, appDataFrom("10.0.0.1", 12542), nil, 0, idx["br0"])
		if seen := drainLines(lines); rowsFor(seen, "DPI") != 1 {
			t.Fatalf("from br0 the learned address keeps the DPI set, got %v", seen)
		}
		queuedVerdict(t, w, appDataFrom("10.0.0.2", 12543), nil, 0, idx["br-guest"])
		if seen := drainLines(lines); rowsFor(seen, "DPI") != 0 {
			t.Fatalf("from br-guest the escalation replaces the DPI set, got %v", seen)
		}
	})

	t.Run("a QUIC Initial", func(t *testing.T) {
		initialSet := newDomainUDPSet(config.QUICFilterSNI)
		initialSet.UDP.Mode = config.ConfigOff
		block := guestBlockFor("unused.example")
		w := newQUICFlowWorker(t, &initialSet, block)
		w.destState.SetEscalation("rr1.googlevideo.com", block.Id, escalateReasonStall, time.Hour)

		initial := buildQUICInitialWithSNI(t, []byte{7, 7, 7, 7, 2, 2, 2, 2}, "rr1.googlevideo.com")
		for _, c := range []struct {
			in    uint32
			sport uint16
			want  int
		}{{idx["br0"], 51300, nfqueue.NfAccept}, {idx["br-guest"], 51301, nfqueue.NfDrop}} {
			if got := queuedVerdict(t, w, makeV4UDPPacket(initial, quicClient, quicServer, c.sport, 443), nil, 0, c.in); got != c.want {
				t.Fatalf("a QUIC Initial on interface %d: verdict %d, want %d", c.in, got, c.want)
			}
		}
	})

	t.Run("a QUIC flow", func(t *testing.T) {
		flowSet := newDomainOnlyQUICSet(config.ConfigOff)
		block := guestBlockFor("unused.example")
		w := newQUICFlowWorker(t, &flowSet, block)
		w.destState.SetEscalation("www.blocked.example", block.Id, escalateReasonStall, time.Hour)

		for _, c := range []struct {
			in    uint32
			sport uint16
			cid   byte
			want  int
		}{{idx["br0"], 51310, 0x81, nfqueue.NfAccept}, {idx["br-guest"], 51311, 0x83, nfqueue.NfDrop}} {
			flow := newSplitQUICHello(t, []byte{c.cid, 1, 2, 3, 4, 5, 6, 7}, []byte{c.cid + 1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
			queuedVerdict(t, w, makeV4UDPPacket(flow.first, quicClient, quicServer, c.sport, 443), nil, 0, c.in)
			for i, p := range [][]byte{flow.completing, flow.resend, flow.oneRTT} {
				if got := queuedVerdict(t, w, makeV4UDPPacket(p, quicClient, quicServer, c.sport, 443), nil, 0, c.in); got != c.want {
					t.Fatalf("datagram %d of the flow on interface %d: verdict %d, want %d", i+2, c.in, got, c.want)
				}
			}
		}
	})

	t.Run("a DNS query", func(t *testing.T) {
		dpi, block := escalatingToGuestBlock()
		w := dnsBlockWorker(t, dpi, block)
		w.destState.SetEscalation("i.ytimg.com", block.Id, escalateReasonStall, time.Hour)
		query := func(src string, sport, txid uint16) []byte {
			return makeV4UDPPacket(dns.BuildQuery("i.ytimg.com", txid, dnsTypeA), net.ParseIP(src), net.ParseIP(scopeRouter), sport, 53)
		}
		if got := queuedVerdict(t, w, query(scopeLAN, 40500, 0x901), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
			t.Fatalf("verdict %d: a query from br0 keeps the DPI set and goes to the resolver", got)
		}
		if got := queuedVerdict(t, w, query(scopeGuest, 40501, 0x902), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
			t.Fatalf("verdict %d: a query from br-guest is dropped by the escalated block set", got)
		}
	})

	t.Run("a DNS query over TCP", func(t *testing.T) {
		routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"], scopeLAN: idx["br0"]})
		pass := config.NewSetConfig()
		pass.Id, pass.Name, pass.Enabled = "pass", "Pass", true
		pass.Targets.DomainsToMatch = []string{"ads.example"}
		pass.Escalate.To = "block"
		block := guestBlockFor("unused.example")
		w := redirectWorker(t, false, &pass, block)
		w.destState.SetEscalation("www.ads.example", block.Id, escalateReasonDNS, time.Hour)
		tcpDNSUpstream(t)

		if rcode, _ := dns.ResponseRcode(tcpQueryFrom(t, w, scopeLAN, dns.BuildQuery("www.ads.example", 0x903, dnsTypeA))); rcode != dns.RcodeNoError {
			t.Fatalf("the client on br0 got rcode %d, want the upstream's own answer", rcode)
		}
		if rcode, _ := dns.ResponseRcode(tcpQueryFrom(t, w, scopeGuest, dns.BuildQuery("www.ads.example", 0x904, dnsTypeA))); rcode != dns.RcodeNXDomain {
			t.Fatalf("the client on br-guest got rcode %d, want NXDOMAIN from the escalated block set", rcode)
		}
	})
}

func TestAStallEscalatesToAScopedBlockSetOnlyFromItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	dpi, block := escalatingToGuestBlock()
	w := newInjectingWorker(t, dpi, block)
	captureInjected(t, w)

	for range 3 {
		queuedVerdict(t, w, helloFrom("10.0.0.1", 12550), nil, 0, idx["br0"])
	}
	if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); ok {
		t.Fatalf("a stall on br0 escalated the host to %q, a block set scoped to br-guest", setID)
	}
	for range 3 {
		queuedVerdict(t, w, helloFrom("10.0.0.2", 12551), nil, 0, idx["br-guest"])
	}
	if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); !ok || setID != block.Id {
		t.Fatalf("a stall on br-guest escalated to %q (%v), want the block set", setID, ok)
	}
}

func TestADNSFailureEscalatesToAScopedBlockSetOnlyFromItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"], scopeLAN: idx["br0"]})
	dpi, block := escalatingToGuestBlock()
	dpi.Escalate.DNSThreshold = 1
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{dpi, block}
	w := passiveDNSWorker(t, &cfg)

	nx := dns.BuildBlockResponse(dns.BuildQuery("i.ytimg.com", 0x911, dnsTypeA))
	answer := func(client string) {
		raw := makeV4UDPPacket(nx, net.ParseIP(rigUpstream), net.ParseIP(client), 53, 40510)
		pkt, ok := w.parseIPHeaders(raw)
		if !ok {
			t.Fatal("the answer did not parse")
		}
		w.processDnsPacket(&verdictCtx{verdict: engine.VerdictAccept}, pkt, 53, 40510, raw[28:])
	}

	answer(scopeLAN)
	if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); ok {
		t.Fatalf("a failed answer for br0 escalated the host to %q, a block set scoped to br-guest", setID)
	}
	answer(scopeGuest)
	if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); !ok || setID != block.Id {
		t.Fatalf("a failed answer for br-guest escalated to %q (%v), want the block set", setID, ok)
	}
}

func TestAConnectionDecidedBeforeAReloadIsDecidedAgainWithTheScope(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	block, dpi := guestBlockAndDPI()
	w := newInjectingWorker(t, block, dpi)
	captureInjected(t, w)
	lines := connectionLines(t)

	for _, c := range []struct {
		client string
		sport  uint16
		in     uint32
		dpi    int
		why    string
	}{
		{"10.0.0.1", 12560, idx["br0"], 1, "a connection from br0 stays with the DPI set"},
		{"10.0.0.2", 12561, idx["br-guest"], 0, "a connection from br-guest belongs to the block set, which does not take its later records"},
	} {
		queuedVerdict(t, w, helloFrom(c.client, c.sport), nil, 0, c.in)
		drainLines(lines)
		w.matcher.Store(buildMatcher(w.getConfig()))
		next := makeV4TCPPacketFromPort(tlsAppData(32), 1000+uint32(len(buildClientHello("i.ytimg.com", 16, 0xAB))), c.sport, 0x18)
		copy(next[12:16], net.ParseIP(c.client).To4())
		queuedVerdict(t, w, next, nil, 0, c.in)
		if seen := drainLines(lines); len(seen) != 1 || rowsFor(seen, "DPI") != c.dpi {
			t.Fatalf("%s: after a reload the connection is decided again: %s, got %v", c.client, c.why, seen)
		}
	}
}

func TestAFailedRedirectEscalatesToAScopedBlockSetOnlyFromItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	for _, c := range []struct {
		path        string
		unreachable bool
	}{{"UDP", false}, {"TCP", false}, {"UDP", true}, {"TCP", true}} {
		path := c.path
		name := path + " NXDOMAIN"
		if c.unreachable {
			name = path + " unreachable"
		}
		t.Run(name, func(t *testing.T) {
			thisHost(t, scopeRouter)
			routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
			redirect := config.NewSetConfig()
			redirect.Id, redirect.Name, redirect.Enabled = "redirect", "Redirect", true
			redirect.Targets.DomainsToMatch = []string{"ytimg.com"}
			redirect.Escalate.To, redirect.Escalate.DNSThreshold = "block", 1
			block := guestBlockFor("unused.example")
			captureSends(t)
			w := dnsBlockWorker(t, &redirect, block)
			redirect.DNS.Enabled, redirect.DNS.DoHURL = true, nxDoHServer(t, w)
			if c.unreachable {
				redirect.DNS.Strict, redirect.DNS.DoHURL = true, unreachableDoH
			}
			ask := func(client string, in uint32, sport, txid uint16) {
				query := dns.BuildQuery("i.ytimg.com", txid, dnsTypeA)
				if path == "TCP" {
					tcpQueryFrom(t, w, client, query)
					return
				}
				queuedVerdict(t, w, makeV4UDPPacket(query, net.ParseIP(client), net.ParseIP(rigUpstream), sport, 53), nil, 0, in)
				holdsDone(t, w)
			}

			ask(scopeLAN, idx["br0"], 40720, 0x981)
			if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); ok {
				t.Fatalf("a failed redirect for br0 escalated the host to %q, a block set scoped to br-guest", setID)
			}
			ask(scopeGuest, idx["br-guest"], 40721, 0x982)
			if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); !ok || setID != block.Id {
				t.Fatalf("a failed redirect for br-guest escalated to %q (%v), want the block set", setID, ok)
			}
		})
	}
}

func deadAddressWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	w := newInjectingWorker(t, sets...)
	captureInjected(t, w)
	w.ipHealth = iphealth.NewTracker(func(string, uint16, int) bool { return false })
	t.Cleanup(w.ipHealth.Stop)
	markDead(t, w, "1.2.3.4")
	return w
}

func synFrom(src string, sport uint16) []byte {
	raw := makeV4TCPPacketFromPort(nil, 1000, sport, 0x02)
	copy(raw[12:16], net.ParseIP(src).To4())
	return raw
}

func TestADeadAddressEscalatesToAScopedBlockSetOnlyFromItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	dpi, block := escalatingToGuestBlock()
	w := deadAddressWorker(t, dpi, block)

	for _, c := range []struct {
		client string
		in     uint32
		want   bool
	}{{"10.0.0.1", idx["br0"], false}, {"10.0.0.2", idx["br-guest"], true}} {
		w.hostHints.Store(c.client, "1.2.3.4", dpi.Id, "i.ytimg.com")
		queuedVerdict(t, w, synFrom(c.client, 12580), nil, 0, c.in)
		if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); ok != c.want || (ok && setID != block.Id) {
			t.Fatalf("%s: a SYN to a dead address escalated the host to %q (%v), want an escalation to the block set: %v", c.client, setID, ok, c.want)
		}
	}
}

func TestADeadAddressTakesNoNameFromAHintForAScopedOutBlockSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	dpi := newFragmentingYTImagesSet()
	dpi.Id, dpi.Name = "dpi", "DPI"
	dpi.Targets.DomainOnly = false
	dpi.Escalate.To = "backup"
	backup := escalationBackupSet()
	block := guestBlockFor("unused.example")
	w := deadAddressWorker(t, &dpi, &backup, block)
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", &dpi)

	w.hostHints.Store("10.0.0.1", "1.2.3.4", block.Id, "i.ytimg.com")
	queuedVerdict(t, w, synFrom("10.0.0.1", 12590), nil, 0, idx["br0"])
	if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); ok {
		t.Fatalf("a hint naming a block set scoped to br-guest gave a SYN from br0 its name, and the host escalated to %q", setID)
	}
	w.hostHints.Store("10.0.0.3", "1.2.3.4", dpi.Id, "i.ytimg.com")
	queuedVerdict(t, w, synFrom("10.0.0.3", 12591), nil, 0, idx["br0"])
	if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); !ok || setID != backup.Id {
		t.Fatalf("a hint naming the DPI set escalated the dead address to %q (%v), want the backup", setID, ok)
	}
}

func TestAStallOnAnotherInterfaceLeavesTheScopedBlockSetBlocking(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	block := blockYTImages(config.BlockActionDrop, true)
	dpi := newFragmentingYTImagesSet()
	dpi.Id, dpi.Name = "dpi", "DPI"
	dpi.Escalate.To = "backup"
	backup := escalationBackupSet()
	w := newInjectingWorker(t, block, &dpi, &backup)
	captureInjected(t, w)
	lines := connectionLines(t)

	for range 3 {
		queuedVerdict(t, w, helloFrom("10.0.0.1", 12600), nil, 0, idx["br0"])
	}
	if setID, _, ok := w.destState.GetEscalation("i.ytimg.com"); !ok || setID != backup.Id {
		t.Fatalf("a stall on br0 escalated the host to %q (%v), want the backup", setID, ok)
	}
	drainLines(lines)
	queuedVerdict(t, w, helloFrom("10.0.0.1", 12601), nil, 0, idx["br0"])
	if seen := drainLines(lines); rowsFor(seen, "Backup") == 0 || blockRows(seen) != 0 {
		t.Fatalf("after its stall a ClientHello from br0 goes to the backup set, got %v", seen)
	}
	queuedVerdict(t, w, helloFrom("10.0.0.2", 12602), nil, 0, idx["br-guest"])
	if seen := drainLines(lines); blockRows(seen) != 1 || rowsFor(seen, "Backup") != 0 {
		t.Fatalf("the escalation from br0 replaced the block set for a ClientHello from br-guest, got %v", seen)
	}
}

func TestAnEscalationLeavesALearnedOrHintedBlockSetInPlace(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	for _, c := range []struct {
		name string
		send func(w *Worker, block, dpi *config.SetConfig)
	}{
		{"a learned address", func(w *Worker, block, _ *config.SetConfig) {
			w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", block)
			queuedVerdict(t, w, appDataFrom(scopeGuest, 12610), nil, 0, idx["br-guest"])
		}},
		{"a host hint", func(w *Worker, block, _ *config.SetConfig) {
			w.hostHints.Store(scopeGuest, "1.2.3.4", block.Id, "i.ytimg.com")
			queuedVerdict(t, w, appDataFrom(scopeGuest, 12611), nil, 0, idx["br-guest"])
		}},
		{"a learned address after a hint drop", func(w *Worker, block, dpi *config.SetConfig) {
			w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", block)
			w.hostHints.Store(scopeGuest, "1.2.3.4", dpi.Id, "i.ytimg.com")
			queuedVerdict(t, w, helloNaming("www.unlisted.example", scopeGuest, 12612), nil, 0, idx["br-guest"])
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			block, dpi := guestBlockAndDPI()
			backup := escalationBackupSet()
			w := newInjectingWorker(t, block, dpi, &backup)
			captureInjected(t, w)
			w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Hour)
			lines := connectionLines(t)
			c.send(w, block, dpi)
			if seen := drainLines(lines); len(seen) == 0 || rowsFor(seen, "Backup") != 0 {
				t.Fatalf("an escalation to the backup set replaced the block set for a client on br-guest, got %v", seen)
			}
		})
	}
}

func TestAFailedAnswerMovesNoEscalationThroughASetTheClientDoesNotGet(t *testing.T) {
	idx := scopeIfaces(t)
	escalating := func(id, to string) *config.SetConfig {
		set := config.NewSetConfig()
		set.Id, set.Name, set.Enabled = id, id, true
		set.Targets.DomainsToMatch = []string{"ads.example"}
		set.Escalate.To, set.Escalate.DNSThreshold = to, 1
		return &set
	}
	for _, c := range []struct {
		name   string
		client string
		sets   func() []*config.SetConfig
		holds  string
	}{
		{"a client the block set covers", scopeGuest, func() []*config.SetConfig {
			block := scopedDNSBlock(config.BlockActionDrop)
			return []*config.SetConfig{block, escalating("dpi", "backup"), escalating("backup", "spare"), escalating("spare", "")}
		}, "backup"},
		{"a client the escalated block set does not cover", scopeLAN, func() []*config.SetConfig {
			block := scopedDNSBlock(config.BlockActionDrop)
			block.Targets.DomainsToMatch = []string{"unused.example"}
			block.Escalate.To, block.Escalate.DNSThreshold = "spare", 1
			return []*config.SetConfig{escalating("dpi", "block"), block, escalating("spare", "")}
		}, "block"},
	} {
		t.Run(c.name, func(t *testing.T) {
			thisHost(t, scopeRouter)
			routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"], scopeLAN: idx["br0"]})
			cfg := config.NewConfig()
			cfg.Sets = c.sets()
			w := passiveDNSWorker(t, &cfg)
			w.destState.SetEscalation("www.ads.example", c.holds, escalateReasonStall, time.Hour)
			raw := makeV4UDPPacket(dns.BuildBlockResponse(dns.BuildQuery("www.ads.example", 0x9c1, dnsTypeA)), net.ParseIP(rigUpstream), net.ParseIP(c.client), 53, 40520)
			pkt, ok := w.parseIPHeaders(raw)
			if !ok {
				t.Fatal("the answer did not parse")
			}
			w.processDnsPacket(&verdictCtx{verdict: engine.VerdictAccept}, pkt, 53, 40520, raw[28:])
			if setID, _, _ := w.destState.GetEscalation("www.ads.example"); setID != c.holds {
				t.Fatalf("the failed answer moved the escalation to %q, want it kept at %q", setID, c.holds)
			}
		})
	}
}

func TestAnEscalationNeverReplacesABlockSet(t *testing.T) {
	idx := scopeIfaces(t)
	type protocol struct {
		name    string
		host    string
		worker  func(t *testing.T, block func(*config.SetConfig) *config.SetConfig) *Worker
		blocked func(t *testing.T, w *Worker, client string, in uint32, sport uint16) bool
	}
	plainUDP := func(build func() config.SetConfig, id string, escalates bool) *config.SetConfig {
		set := build()
		set.Id, set.Name = id, id
		set.UDP.Mode = config.ConfigOff
		if escalates {
			set.Escalate.To = "backup"
		}
		return &set
	}
	quicInitial := func() config.SetConfig { return newDomainUDPSet(config.QUICFilterSNI) }
	quicFlow := func() config.SetConfig { return newDomainOnlyQUICSet(config.ConfigOff) }
	dnsSet := func(id string, escalates bool) *config.SetConfig {
		set := config.NewSetConfig()
		set.Id, set.Name, set.Enabled = id, id, true
		set.Targets.DomainsToMatch = []string{"ads.example"}
		if escalates {
			set.Escalate.To = "backup"
		}
		return &set
	}
	for _, p := range []protocol{
		{"a ClientHello", "i.ytimg.com", func(t *testing.T, block func(*config.SetConfig) *config.SetConfig) *Worker {
			blockSet := newFragmentingYTImagesSet()
			blockSet.Id, blockSet.Name = "block", "Block"
			dpi := newFragmentingYTImagesSet()
			dpi.Id, dpi.Name, dpi.Escalate.To = "dpi", "DPI", "backup"
			backup := escalationBackupSet()
			w := newInjectingWorker(t, block(&blockSet), &dpi, &backup)
			captureInjected(t, w)
			return w
		}, func(t *testing.T, w *Worker, client string, in uint32, sport uint16) bool {
			lines := connectionLines(t)
			queuedVerdict(t, w, helloFrom(client, sport), nil, 0, in)
			return blockRows(drainLines(lines)) == 1
		}},
		{"a QUIC Initial", "rr1.googlevideo.com", func(t *testing.T, block func(*config.SetConfig) *config.SetConfig) *Worker {
			return newQUICFlowWorker(t, block(plainUDP(quicInitial, "block", false)), plainUDP(quicInitial, "dpi", true), plainUDP(quicInitial, "backup", false))
		}, func(t *testing.T, w *Worker, client string, in uint32, sport uint16) bool {
			initial := buildQUICInitialWithSNI(t, []byte{7, 7, 7, 7, 3, 3, 3, byte(sport)}, "rr1.googlevideo.com")
			return queuedVerdict(t, w, makeV4UDPPacket(initial, net.ParseIP(client), quicServer, sport, 443), nil, 0, in) == nfqueue.NfDrop
		}},
		{"a QUIC flow", "www.blocked.example", func(t *testing.T, block func(*config.SetConfig) *config.SetConfig) *Worker {
			return newQUICFlowWorker(t, block(plainUDP(quicFlow, "block", false)), plainUDP(quicFlow, "dpi", true), plainUDP(quicFlow, "backup", false))
		}, func(t *testing.T, w *Worker, client string, in uint32, sport uint16) bool {
			flow := newSplitQUICHello(t, []byte{byte(sport), 1, 2, 3, 4, 5, 6, 7}, []byte{byte(sport) + 1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
			queuedVerdict(t, w, makeV4UDPPacket(flow.first, net.ParseIP(client), quicServer, sport, 443), nil, 0, in)
			dropped := 0
			for _, p := range [][]byte{flow.completing, flow.resend, flow.oneRTT} {
				if queuedVerdict(t, w, makeV4UDPPacket(p, net.ParseIP(client), quicServer, sport, 443), nil, 0, in) == nfqueue.NfDrop {
					dropped++
				}
			}
			return dropped == 3
		}},
		{"a DNS query", "www.ads.example", func(t *testing.T, block func(*config.SetConfig) *config.SetConfig) *Worker {
			blockSet := copyBlockSet(config.BlockActionDrop)
			return dnsBlockWorker(t, block(blockSet), dnsSet("dpi", true), dnsSet("backup", false))
		}, func(t *testing.T, w *Worker, client string, in uint32, sport uint16) bool {
			return queuedVerdict(t, w, dnsQueryFrom(client, scopeRouter, sport, sport), nil, 0, in) == nfqueue.NfDrop
		}},
		{"a DNS query over TCP", "www.ads.example", func(t *testing.T, block func(*config.SetConfig) *config.SetConfig) *Worker {
			w := redirectWorker(t, false, block(copyBlockSet(config.BlockActionReject)), dnsSet("dpi", true), dnsSet("backup", false))
			tcpDNSUpstream(t)
			return w
		}, func(t *testing.T, w *Worker, client string, _ uint32, sport uint16) bool {
			rcode, _ := dns.ResponseRcode(tcpQueryFrom(t, w, client, dns.BuildQuery("www.ads.example", sport, dnsTypeA)))
			return rcode == dns.RcodeNXDomain
		}},
	} {
		for _, scoped := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/scoped=%v", p.name, scoped), func(t *testing.T) {
				thisHost(t, scopeRouter)
				routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
				captureSends(t)
				w := p.worker(t, func(set *config.SetConfig) *config.SetConfig {
					scopeToGuest(set, set.Routing.BlockAction)
					if !scoped {
						set.Routing.SourceInterfaces = nil
					}
					return set
				})
				w.destState.SetEscalation(p.host, "backup", escalateReasonStall, time.Hour)
				if !p.blocked(t, w, scopeGuest, idx["br-guest"], 41000) {
					t.Fatalf("an escalation of %s to the backup set replaced the block set for a client on br-guest", p.host)
				}
				if blocked := p.blocked(t, w, scopeLAN, idx["br0"], 41001); blocked == scoped {
					t.Fatalf("a client on br0 was blocked: %v, want %v", blocked, !scoped)
				}
			})
		}
	}
}
