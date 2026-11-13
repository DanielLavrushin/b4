package nfq

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

func TestTheBanRetestsOwnSynPassesTheBan(t *testing.T) {
	cases := map[string]struct {
		router, retesting bool
		probePort         uint32
		reset             bool
	}{
		"the re-test's own SYN":                               {router: true, retesting: true, probePort: 12345},
		"the router's SYN from another port during a re-test": {router: true, retesting: true, probePort: 40000, reset: true},
		"the router's SYN before the re-test bound its port":  {router: true, retesting: true, reset: true},
		"the router's SYN without a re-test":                  {router: true, reset: true},
		"a device's SYN from the same port during a re-test":  {retesting: true, probePort: 12345, reset: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			set := banningAddressSet()
			w := newDualStackInjectingWorker(t, &set)
			w.bans = newTestIPBans(t, nil)
			banAddress(t, w.bans, "1.2.3.4", 443)
			if c.router {
				thisHost(t, "10.0.0.1")
			} else {
				thisHost(t)
			}
			if c.retesting {
				flow, done := w.bans.beginHostRetest(net.ParseIP("1.2.3.4"), 443)
				defer done()
				flow.sport.Store(c.probePort)
			}
			sent := captureInjected(t, w)

			v := w.ProcessPacket(bareSyn(IPv4))
			if dropped := v == engine.VerdictDrop; dropped != c.reset {
				t.Errorf("SYN dropped = %t, want %t (verdict %v)", dropped, c.reset, v)
			}
			if n := resetsIn(sent()); (n == 1) != c.reset {
				t.Errorf("resets sent = %d, want reset %t", n, c.reset)
			}
		})
	}
}

func TestTheBanRetestsOwnHelloIsNeitherResetNorWatched(t *testing.T) {
	set := newPassiveSet()
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	set.TCP.IPBlockDetect.RetransmitThreshold = 2
	w := newInjectingWorker(t, &set)
	var confirmed atomic.Int32
	w.bans = newTestIPBans(t, func(context.Context, net.IP, int, int) iphealth.TLSResult {
		confirmed.Add(1)
		return iphealth.TLSSilent
	})
	banAddress(t, w.bans, "1.2.3.4", 443)
	thisHost(t, "10.0.0.1")
	sent := captureInjected(t, w)
	hello := buildClientHello("i.ytimg.com", 16, 0xAB)

	flow, done := w.bans.beginHostRetest(net.ParseIP("1.2.3.4"), 443)
	flow.sport.Store(12345)
	for i := 0; i < 3; i++ {
		if v := w.ProcessPacket(makeV4TCPPacket(hello, 1000)); v != engine.VerdictAccept {
			t.Fatalf("ClientHello %d of the re-test must pass, got %v", i+1, v)
		}
	}
	if !flow.seen.Load() || flow.handedOff.Load() {
		t.Errorf("the re-test's ClientHello must be noted as seen and sent directly: seen=%t handedOff=%t", flow.seen.Load(), flow.handedOff.Load())
	}
	if n := resetsIn(sent()); n != 0 {
		t.Errorf("the re-test's connection must not be reset, got %d resets", n)
	}
	w.bans.wg.Wait()
	if confirmed.Load() != 0 {
		t.Errorf("resends of the re-test's ClientHello must not start a ban check, started %d", confirmed.Load())
	}
	if ban := w.bans.reg.List()[0]; ban.Strikes != 1 {
		t.Errorf("a failed re-test must not strike the address again, strikes = %d", ban.Strikes)
	}
	if due := w.destState.DueSilent(time.Now().Add(time.Hour)); len(due) != 0 {
		t.Errorf("the re-test's ClientHello must not be watched for silence, %d watched", len(due))
	}

	done()
	if v := w.ProcessPacket(makeV4TCPPacketFromPort(hello, 5000, 23456, 0x18)); v != engine.VerdictDrop {
		t.Fatalf("once the re-test is over the router's ClientHello to the banned address is dropped again, got %v", v)
	}
	if n := resetsIn(sent()); n != 1 {
		t.Errorf("once the re-test is over the connection is reset, got %d resets", n)
	}
}

func TestTheBanRetestNotesAHelloADuplicatingSetSends(t *testing.T) {
	set := banningAddressSet()
	set.TCP.Duplicate.Enabled = true
	set.TCP.Duplicate.Count = 2
	w := newDualStackInjectingWorker(t, &set)
	w.bans = newTestIPBans(t, nil)
	banAddress(t, w.bans, "1.2.3.4", 443)
	thisHost(t, "10.0.0.1")
	sent := captureInjected(t, w)

	flow, done := w.bans.beginHostRetest(net.ParseIP("1.2.3.4"), 443)
	defer done()
	flow.sport.Store(12345)

	w.ProcessPacket(bareSyn(IPv4))
	if flow.seen.Load() {
		t.Fatal("only the ClientHello marks the re-test as seen, not its SYN")
	}
	w.ProcessPacket(makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000))
	if !flow.seen.Load() || flow.handedOff.Load() {
		t.Errorf("a ClientHello a duplicating set sends out itself must be noted as seen and direct, or a ban under such a set never lifts by its name: seen=%t handedOff=%t", flow.seen.Load(), flow.handedOff.Load())
	}
	if n := resetsIn(sent()); n != 0 {
		t.Errorf("the re-test's connection must not be reset, got %d resets", n)
	}
}

func TestTheBanRetestNotesAHelloThatLeavesThroughAProxy(t *testing.T) {
	set := newPassiveSet()
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	w := newInjectingWorker(t, &set)
	w.bans = newTestIPBans(t, nil)
	banAddress(t, w.bans, "1.2.3.4", 443)
	thisHost(t, "10.0.0.1")

	flow, done := w.bans.beginHostRetest(net.ParseIP("1.2.3.4"), 443)
	defer done()
	flow.sport.Store(12345)
	w.ProcessPacket(makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000))
	if !flow.seen.Load() || !flow.handedOff.Load() {
		t.Errorf("a ClientHello a routing set takes must be noted as handed off: seen=%t handedOff=%t", flow.seen.Load(), flow.handedOff.Load())
	}
}

func TestTheBanRetestNotesAnUnmatchedHelloThroughATunnel(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := newFragmentingYTImagesSet()
	for name, c := range map[string]struct {
		out       uint32
		handedOff bool
	}{
		"through another service's tunnel": {idx["wgt0"], true},
		"through the uplink":               {idx["eth7"], false},
	} {
		t.Run(name, func(t *testing.T) {
			w := newInjectingWorker(t, &set)
			w.bans = newTestIPBans(t, nil)
			banAddress(t, w.bans, "1.2.3.4", 443)
			thisHost(t, "10.0.0.1")

			flow, done := w.bans.beginHostRetest(net.ParseIP("1.2.3.4"), 443)
			defer done()
			flow.sport.Store(12345)
			queuedVerdict(t, w, makeV4TCPPacket(buildClientHello("unlisted.example", 16, 0xAB), 1000), nil, c.out, 0)
			if !flow.seen.Load() {
				t.Fatal("the re-test's ClientHello must be noted as seen although no set takes its name")
			}
			if flow.handedOff.Load() != c.handedOff {
				t.Errorf("handed off = %t, want %t: an answer that came back through a tunnel says nothing about the direct path, whether or not a set takes the name", flow.handedOff.Load(), c.handedOff)
			}
		})
	}
}

func nameRetestBans(t *testing.T, handled, handedOff bool) (*ipBans, *atomic.Int32) {
	t.Helper()
	b := newTestIPBans(t, func(context.Context, net.IP, int, int) iphealth.TLSResult { return iphealth.TLSSilent })
	asked := &atomic.Int32{}
	b.hostProbe = func(_ context.Context, ip net.IP, port int, host string, bound func(int)) iphealth.TLSResult {
		asked.Add(1)
		bound(40000)
		if host != bannedHost {
			t.Errorf("the name re-test asked for %q, want the name the ban was made for", host)
		}
		if flow := b.hostRetestTo(ip, uint16(port)); flow == nil {
			t.Error("the address must be marked as under re-test while its name probe runs")
		} else if flow.sport.Load() != 40000 {
			t.Errorf("the re-test must record the port its probe bound, got %d", flow.sport.Load())
		} else if handled {
			flow.note(handedOff)
		}
		return iphealth.TLSReplied
	}
	return b, asked
}

func TestRetestsLiftABanWhenTheBannedNameAnswersThroughB4(t *testing.T) {
	b, asked := nameRetestBans(t, true, false)
	ip := net.ParseIP("74.125.172.167")
	banAddress(t, b, "74.125.172.167", 443)

	for i := 0; i < 3; i++ {
		if !b.reg.Banned(ip, 443) {
			t.Fatalf("the ban was lifted after %d answered name re-tests, want 3", i)
		}
		b.retest(0, 0)
		b.wg.Wait()
	}
	if b.reg.Banned(ip, 443) {
		t.Fatal("three re-tests in a row answered for the banned name must lift the ban, although neutral names stay silent")
	}
	if asked.Load() != 3 {
		t.Errorf("name probes = %d, want one per re-test", asked.Load())
	}
	if b.hostRetestTo(ip, 443) != nil {
		t.Error("the address must not stay marked as under re-test")
	}

	banAddress(t, b, "74.125.172.167", 443)
	for i := 0; i < 3; i++ {
		b.retest(0, 0)
		b.wg.Wait()
	}
	if b.reg.Banned(ip, 443) {
		t.Fatal("an address banned again is re-tested with its name like the first time")
	}
}

func TestANameRetestCountsOnlyWhenB4SentItsHelloDirectly(t *testing.T) {
	cases := map[string]struct{ handled, handedOff bool }{
		"its ClientHello never reached b4": {},
		"its ClientHello left by a route":  {handled: true, handedOff: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, asked := nameRetestBans(t, c.handled, c.handedOff)
			ip := net.ParseIP("74.125.172.167")
			banAddress(t, b, "74.125.172.167", 443)
			for i := 0; i < 3; i++ {
				b.retest(0, 0)
				b.wg.Wait()
			}
			if asked.Load() != 3 {
				t.Fatalf("name probes = %d, want 3", asked.Load())
			}
			if !b.reg.Banned(ip, 443) {
				t.Error("an answer to a probe b4 did not send out itself must not lift the ban")
			}
		})
	}
}

func TestTheFirstBanCheckNeverUsesTheNameProbe(t *testing.T) {
	b, asked := nameRetestBans(t, true, false)
	cfg := config.NewConfig()
	b.applyConfig(&cfg)
	b.confirm(&cfg, net.ParseIP("74.125.172.167"), 443, bannedHost, bannedSet)
	b.wg.Wait()
	if !b.reg.Banned(net.ParseIP("74.125.172.167"), 443) {
		t.Fatal("silent neutral names ban the address")
	}
	if asked.Load() != 0 {
		t.Errorf("the check that decides a ban must use neutral names only, name probes = %d", asked.Load())
	}
}

func TestTheBanRetestHintsTheSetForItsOwnConnection(t *testing.T) {
	set := newHintSet()
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	w := newInjectingWorker(t, &set)
	prev := routeSource
	routeSource = func(net.IP, int) net.IP { return net.ParseIP("10.0.0.1") }
	t.Cleanup(func() { routeSource = prev })

	w.hintBanRetest(net.ParseIP("1.2.3.4"), 443, "i.ytimg.com")
	hinted, host, _, _ := w.lookupHostHint(w.getConfig(), "10.0.0.1", "1.2.3.4", "")
	if hinted == nil || hinted.Id != set.Id || host != "i.ytimg.com" {
		t.Fatalf("the re-test's SYN must find the name's set through a hint, got set %v host %q", hinted, host)
	}

	w.hintBanRetest(net.ParseIP("1.2.3.5"), 443, "no-set.example")
	if hinted, _, _, _ := w.lookupHostHint(w.getConfig(), "10.0.0.1", "1.2.3.5", ""); hinted != nil {
		t.Errorf("a name no set takes gets no hint, got %v", hinted)
	}
}

func TestTheBanRetestHintsASetTheRoutersOwnSynTakes(t *testing.T) {
	for _, c := range []struct {
		name                     string
		dpiVersion, blockVersion string
		blockFirst               bool
	}{
		{"an IPv4 block set for br-guest below an IPv6 set", "6", "4", false},
		{"a block set for br-guest listed first", "", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			scopeIfaces(t)
			thisHost(t, scopeRouter)
			dpi := familySet("dpi", c.dpiVersion)
			dpi.TCP.RSTProtection.Enabled = false
			dpi.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1, Cutoff: wsFirstData}
			block := scopeToGuest(familySet("block", c.blockVersion), config.BlockActionDrop)
			sets := []*config.SetConfig{dpi, block}
			if c.blockFirst {
				sets = []*config.SetConfig{block, dpi}
			}
			w := familyWorker(t, sets...)
			prev := routeSource
			routeSource = func(net.IP, int) net.IP { return net.ParseIP(scopeRouter) }
			t.Cleanup(func() { routeSource = prev })

			w.hintBanRetest(net.ParseIP(familyV4Addr), 443, familyName)

			syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
			copy(syn[12:16], net.ParseIP(scopeRouter).To4())
			sock.FixIPv4Checksum(syn[:20])
			sock.FixTCPChecksum(syn)
			q := &wsQueue{}
			id := uint32(1)
			w.handleQueued(&verdictCtx{id: id, q: q}, nfqueue.Attribute{PacketID: &id, Payload: &syn, Hook: hookAt(nfHookLocalOut)}, uint(w.getConfig().Queue.Mark))
			got := q.take()
			if len(got) != 1 || got[0].packet == nil || wsWindow(got[0].packet) != 1 {
				hinted, _, _ := w.hostHints.Lookup(scopeRouter, familyV4Addr)
				t.Fatalf("the router's SYN left without the window of set dpi: the re-test hinted set %q, verdicts %+v", hinted, got)
			}
		})
	}
}
