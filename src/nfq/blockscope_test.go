package nfq

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

const (
	scopeRouter = "192.168.1.1"
	scopeGuest  = "192.168.1.50"
	scopeLAN    = "192.168.1.60"
)

func scopeIfaces(t *testing.T) map[string]uint32 {
	t.Helper()
	idx := map[string]uint32{"br-guest": 65101, "br0": 65102, "lo": 65103, "eth9": 65104}
	names := make(map[int]string, len(idx))
	for name, i := range idx {
		ifaceCache.Store(i, name)
		scopeIfaceNames.Delete(i)
		names[int(i)] = name
	}
	stubScopeIfaceNames(t, names)
	t.Cleanup(func() {
		for _, i := range idx {
			ifaceCache.Delete(i)
			scopeIfaceNames.Delete(i)
		}
		ResetIfaceTraffic()
	})
	return idx
}

func tunScopeWorker(t *testing.T, resolver *tunSrcResolver, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"
	cfg.Sets = sets
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	w.srcResolver = resolver
	t.Cleanup(w.wg.Wait)
	return w
}

func scopeToGuest(set *config.SetConfig, action string) *config.SetConfig {
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = action
	set.Routing.SourceInterfaces = []string{"br-guest"}
	return set
}

func blockYTImages(action string, scoped bool) *config.SetConfig {
	set := newPassiveSet()
	set.Id, set.Name = "block", "Block"
	set.TCP.RSTProtection.Enabled = false
	scopeToGuest(&set, action)
	if !scoped {
		set.Routing.SourceInterfaces = nil
	}
	return &set
}

func queuedFrom(t *testing.T, w *Worker, raw []byte, in uint32, hook *uint8, uid *uint32) int {
	t.Helper()
	id := uint32(1)
	a := nfqueue.Attribute{PacketID: &id, Payload: &raw, Hook: hook, UID: uid}
	if in != 0 {
		a.InDev = &in
	}
	q := newFakeQueue()
	w.handleQueued(&verdictCtx{id: id, q: q}, a, uint(w.getConfig().Queue.Mark))
	return q.now(t)
}

func clientResets(sent [][]byte) int {
	n := 0
	for _, p := range sent {
		if len(p) >= 40 && p[0]>>4 == 4 && p[9] == 6 && p[33]&0x04 != 0 {
			n++
		}
	}
	return n
}

func helloFrom(src string, sport uint16) []byte {
	raw := makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, sport, 0x18)
	copy(raw[12:16], net.ParseIP(src).To4())
	return raw
}

func routeByClient(t *testing.T, routes map[string]uint32) *injectRouteStub {
	t.Helper()
	stub := stubInjectRoute(t, 0, false)
	stub.answerBy(func(dst net.IP, _ uint32) (uint32, bool) {
		oif, ok := routes[dst.String()]
		return oif, ok
	})
	return stub
}

func blockRows(lines []string) int {
	return countAction(lines, "block")
}

func TestATLSBlockSetScopedToAnInterfaceBlocksOnlyThatInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	for _, action := range []string{config.BlockActionDrop, config.BlockActionReject} {
		t.Run(action, func(t *testing.T) {
			w := newInjectingWorker(t, blockYTImages(action, true))
			sent := captureInjected(t, w)

			if got := queuedVerdict(t, w, ytimgHello(), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
				t.Fatalf("verdict %d: a ClientHello arriving on the set's own interface is blocked", got)
			}
			resets := clientResets(sent())
			if action == config.BlockActionReject && resets != 1 {
				t.Fatalf("%d resets sent, want the client on br-guest reset once", resets)
			}
			if action == config.BlockActionDrop && resets != 0 {
				t.Fatalf("%d resets sent for a set that drops", resets)
			}

			if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12346), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
				t.Fatalf("verdict %d: the same ClientHello arriving on br0 is outside the set's source interfaces", got)
			}
			if n := clientResets(sent()); n != 0 {
				t.Fatalf("%d resets sent to a client the set does not cover", n)
			}
		})
	}
}

func TestARegexBlockSetScopedToAnInterfaceBlocksOnlyThatInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	set := blockYTImages(config.BlockActionDrop, true)
	set.Targets.DomainsToMatch = []string{`regexp:^i\.ytimg\.com$`}
	w := newInjectingWorker(t, set)
	if got := queuedVerdict(t, w, helloFrom("10.0.0.2", 12352), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: a ClientHello from br-guest naming the set's regex is blocked", got)
	}
	if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12353), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the same ClientHello from br0 is outside the set's source interfaces", got)
	}
}

func TestAScopedTLSBlockSetLeavesOtherInterfacesToTheNextSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	dpi := newFragmentingYTImagesSet()
	dpi.Id, dpi.Name = "dpi", "DPI"
	w := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, true), &dpi)
	captureInjected(t, w)
	lines := connectionLines(t)

	if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12350), nil, 0, idx["br0"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the DPI set for the same domain takes the ClientHello from br0 and re-sends it split", got)
	}
	seen := drainLines(lines)
	if n := blockRows(seen); n != 0 {
		t.Fatalf("%d block rows for a ClientHello from br0: %v", n, seen)
	}
	if len(seen) == 0 || !strings.Contains(seen[0], ",DPI,") {
		t.Fatalf("the connection log must name the DPI set for br0, got %v", seen)
	}

	if got := queuedVerdict(t, w, helloFrom("10.0.0.2", 12351), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the block set takes the ClientHello from br-guest", got)
	}
	seen = drainLines(lines)
	if n := blockRows(seen); n != 1 || !strings.Contains(strings.Join(seen, "\n"), ",Block,") {
		t.Fatalf("want one block row naming the block set for br-guest, got %v", seen)
	}
}

func TestTheRoutersOwnTrafficIsOutsideAnInterfaceScopedBlockSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter, "10.0.0.9")
	stub := routeByClient(t, map[string]uint32{"10.0.0.1": idx["br-guest"], "10.0.0.9": idx["br-guest"]})
	uid := uint32(0)
	cases := []struct {
		name string
		raw  []byte
		in   uint32
		hook *uint8
		uid  *uint32
	}{
		{"queued at local out", helloFrom("10.0.0.1", 12360), 0, hookAt(nfHookLocalOut), nil},
		{"from a local socket", helloFrom("10.0.0.1", 12361), 0, hookAt(nfHookPostRouting), &uid},
		{"from a router address", helloFrom("10.0.0.9", 12362), 0, hookAt(nfHookPostRouting), nil},
	}
	for _, scoped := range []bool{true, false} {
		w := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, scoped))
		for _, c := range cases {
			want, why := nfqueue.NfAccept, "the router's own traffic is outside a set scoped to source interfaces"
			if !scoped {
				want, why = nfqueue.NfDrop, "a block set without source interfaces blocks the router's own traffic too"
			}
			if got := queuedFrom(t, w, c.raw, c.in, c.hook, c.uid); got != want {
				t.Errorf("%s, scoped=%v: verdict %d, want %d: %s", c.name, scoped, got, want, why)
			}
		}
	}
	if calls := stub.got(); len(calls) != 0 {
		t.Fatalf("route lookups %+v for packets already known to be the router's own", calls)
	}
}

func TestWithoutAnArrivalInterfaceTheRouteBackToTheSourceDecides(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	for _, c := range []struct {
		name string
		oif  uint32
		ok   bool
		want int
		why  string
	}{
		{"routed back through br-guest", idx["br-guest"], true, nfqueue.NfDrop, "a client reached through br-guest is in the set's scope"},
		{"routed back through br0", idx["br0"], true, nfqueue.NfAccept, "a client reached through br0 is outside the set's scope"},
		{"routed back through loopback", idx["lo"], true, nfqueue.NfAccept, "an address on loopback is the router itself"},
		{"lookup failed", 0, false, nfqueue.NfDrop, "with nothing known the set keeps applying, as before"},
		{"no route", 0, true, nfqueue.NfDrop, "with nothing known the set keeps applying, as before"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub := stubInjectRoute(t, c.oif, c.ok)
			w := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, true))
			if got := queuedVerdict(t, w, ytimgHello(), nil, 0, 0); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
			calls := stub.got()
			if len(calls) != 1 || calls[0].dst != "10.0.0.1" || calls[0].mark != 0 {
				t.Fatalf("route lookups %+v, want one for the client 10.0.0.1 with mark 0", calls)
			}
		})
	}
}

func TestTheRouteBackToASourceIsLookedUpOnce(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	stub := stubInjectRoute(t, idx["br0"], true)
	w := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, true))
	for i := range 3 {
		if got := queuedVerdict(t, w, helloFrom("10.0.0.1", uint16(12370+i)), nil, 0, 0); got != nfqueue.NfAccept {
			t.Fatalf("connection %d: verdict %d, want accepted from br0", i+1, got)
		}
	}
	if calls := stub.got(); len(calls) != 1 {
		t.Fatalf("%d route lookups for one client, want one remembered for the next connections", len(calls))
	}
}

func TestAConfigWithoutAnInterfaceScopedBlockSetNeverLooksUpARoute(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	stub := stubInjectRoute(t, idx["br0"], true)

	routed := config.NewSetConfig()
	routed.Id, routed.Name, routed.Enabled = "routed", "Routed", true
	routed.Targets.DomainsToMatch = []string{"example.org"}
	routed.Routing.Enabled = true
	routed.Routing.Mode = config.RoutingModeInterface
	routed.Routing.EgressInterface = "wg9"
	routed.Routing.SourceInterfaces = []string{"br-guest"}

	w := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, false), &routed)
	if w.getMatcher().ScopesBlockSets() {
		t.Fatal("only a block set scoped to source interfaces turns the scope on")
	}
	if got := queuedVerdict(t, w, ytimgHello(), nil, 0, 0); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: an unscoped block set blocks by destination alone", got)
	}
	if calls := stub.got(); len(calls) != 0 {
		t.Fatalf("route lookups %+v in a config without an interface scoped block set", calls)
	}

	scoped := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, true))
	other := makeV4TCPPacketFromPort(buildClientHello("www.example.org", 16, 0xAB), 1000, 12380, 0x18)
	if got := queuedVerdict(t, scoped, other, nil, 0, 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d for a domain no set lists", got)
	}
	if calls := stub.got(); len(calls) != 0 {
		t.Fatalf("route lookups %+v for a packet no scoped block set is a candidate for", calls)
	}
}

func TestAScopedQUICBlockSetBlocksOnlyThatInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	set := newDomainUDPSet(config.QUICFilterSNI)
	set.UDP.Mode = config.ConfigOff
	scopeToGuest(&set, config.BlockActionDrop)
	w := newQUICFlowWorker(t, &set)
	initial := buildQUICInitialWithSNI(t, []byte{7, 7, 7, 7, 1, 1, 1, 1}, "rr1.googlevideo.com")

	guest := makeV4UDPPacket(initial, quicClient, quicServer, 51100, 443)
	if got := queuedVerdict(t, w, guest, nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: a QUIC Initial from br-guest naming a blocked domain is dropped", got)
	}
	lan := makeV4UDPPacket(initial, quicClient, quicServer, 51101, 443)
	if got := queuedVerdict(t, w, lan, nil, 0, idx["br0"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the same QUIC Initial from br0 is outside the set's source interfaces", got)
	}
}

func TestAScopedBlockSetStopsAQUICFlowOnlyOnItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	set := newDomainOnlyQUICSet(config.ConfigOff)
	scopeToGuest(&set, config.BlockActionDrop)
	w := newQUICFlowWorker(t, &set)

	send := func(payload []byte, sport uint16, in uint32) int {
		return queuedVerdict(t, w, makeV4UDPPacket(payload, quicClient, quicServer, sport, 443), nil, 0, in)
	}

	guest := newSplitQUICHello(t, []byte{0x61, 1, 2, 3, 4, 5, 6, 7}, []byte{0x62, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if got := send(guest.first, 50101, idx["br-guest"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the first datagram carries no complete ClientHello yet", got)
	}
	for i, p := range [][]byte{guest.completing, guest.resend, guest.oneRTT} {
		if got := send(p, 50101, idx["br-guest"]); got != nfqueue.NfDrop {
			t.Fatalf("datagram %d of the flow from br-guest: verdict %d, want dropped", i+2, got)
		}
	}

	lan := newSplitQUICHello(t, []byte{0x63, 1, 2, 3, 4, 5, 6, 7}, []byte{0x64, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	for i, p := range [][]byte{lan.first, lan.completing, lan.resend, lan.oneRTT} {
		if got := send(p, 50102, idx["br0"]); got != nfqueue.NfAccept {
			t.Fatalf("datagram %d of the flow from br0: verdict %d, want accepted", i+1, got)
		}
	}
}

func TestAScopedAddressBlockSetLeavesOtherInterfacesToTheNextSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	block := config.NewSetConfig()
	block.Id, block.Name, block.Enabled = "block", "Block", true
	block.Targets.IPs = []string{"1.2.3.4/32"}
	scopeToGuest(&block, config.BlockActionDrop)
	geo := config.NewSetConfig()
	geo.Id, geo.Name, geo.Enabled = "geo", "Geo", true
	geo.Targets.IPs = []string{"1.2.3.0/24"}
	geo.UDP.Mode = config.ConfigOff
	w := newQUICFlowWorker(t, &block, &geo)
	lines := connectionLines(t)

	udp := func(sport uint16) []byte {
		return makeV4UDPPacket([]byte("plain udp payload"), quicClient, quicServer, sport, 443)
	}

	if got := queuedVerdict(t, w, udp(52001), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: a packet from br0 goes to the next set for the address", got)
	}
	seen := drainLines(lines)
	if blockRows(seen) != 0 || len(seen) == 0 || !strings.Contains(seen[0], ",Geo,") {
		t.Fatalf("want the packet from br0 logged under the Geo set, got %v", seen)
	}

	if got := queuedVerdict(t, w, udp(52002), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: a packet from br-guest is blocked by address", got)
	}
	if seen = drainLines(lines); blockRows(seen) != 1 {
		t.Fatalf("want one block row for br-guest, got %v", seen)
	}

	captureInjected(t, w)
	queuedVerdict(t, w, ytimgHello(), nil, 0, idx["br0"])
	if seen = drainLines(lines); blockRows(seen) != 0 || len(seen) == 0 || !strings.Contains(seen[0], ",Geo,") {
		t.Fatalf("want a TCP connection from br0 logged under the Geo set, got %v", seen)
	}
	if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12390), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: a TCP connection from br-guest to the blocked address is blocked", got)
	}
	if seen = drainLines(lines); blockRows(seen) != 1 {
		t.Fatalf("want one block row for the TCP connection from br-guest, got %v", seen)
	}
}

func TestAnL7SetWithSourceInterfacesIsSelectedOnlyFromItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	set := newVoiceL7Set(config.UDPL7Discord)
	set.UDP.Mode = config.ConfigOff
	scopeToGuest(&set, config.BlockActionDrop)
	w := newQUICFlowWorker(t, &set)

	discovery := func(sport uint16) []byte {
		return makeV4UDPPacket(discordIPDiscovery(), quicClient, quicServer, sport, 50005)
	}
	if got := queuedVerdict(t, w, discovery(51200), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the voice set blocks the IP discovery request from br-guest", got)
	}
	if got := queuedVerdict(t, w, discovery(51201), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the IP discovery request from br0 is outside the set's source interfaces", got)
	}
	if got := queuedFrom(t, w, discovery(51202), 0, hookAt(nfHookLocalOut), nil); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the router's own packet is outside the set's source interfaces", got)
	}
}

func TestTunModeScopesABlockSetByTheRouteBackToTheSource(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter, "10.0.0.9")
	routeByClient(t, map[string]uint32{"10.0.0.1": idx["br-guest"], "10.0.0.2": idx["br0"]})
	w := tunScopeWorker(t, nil, blockYTImages(config.BlockActionDrop, true))

	if v := w.ProcessPacket(helloFrom("10.0.0.1", 12400)); v != engine.VerdictDrop {
		t.Fatalf("%v: a client routed through br-guest is blocked", v)
	}
	if v := w.ProcessPacket(helloFrom("10.0.0.2", 12401)); v != engine.VerdictAccept {
		t.Fatalf("%v: a client routed through br0 is outside the set's scope", v)
	}
	if v := w.ProcessPacket(helloFrom("10.0.0.9", 12402)); v != engine.VerdictDrop {
		t.Fatalf("%v: without conntrack a router address may be the SNAT of any client, so the set keeps applying", v)
	}
}

func TestTunModeKeepsABlockSetForAnUplinkSourceItCannotAttribute(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter, tunTestWAN)
	routeByClient(t, map[string]uint32{"192.168.1.60": idx["br0"], "192.168.1.50": idx["br-guest"]})
	flow := func(src string, sport int) string {
		return fmt.Sprintf("ipv4 2 tcp 6 300 ESTABLISHED src=%s dst=1.2.3.4 sport=%d dport=443 src=1.2.3.4 dst=%s sport=443 dport=%d use=1\n", src, sport, tunTestWAN, sport)
	}
	for _, c := range []struct {
		name      string
		readable  bool
		conntrack string
		want      engine.PacketVerdict
		why       string
	}{
		{"no conntrack", false, "", engine.VerdictDrop, "with no way to attribute the uplink source the set keeps applying"},
		{"conntrack misses the flow", true, "", engine.VerdictDrop, "a flow conntrack does not list stays unattributed and the set keeps applying"},
		{"conntrack names the router itself", true, flow(tunTestWAN, 12410), engine.VerdictAccept, "the router's own flow is outside a set scoped to source interfaces"},
		{"conntrack names a client on br0", true, flow("192.168.1.60", 12410), engine.VerdictAccept, "a client reached through br0 is outside the set's scope"},
		{"conntrack names a client on br-guest", true, flow("192.168.1.50", 12410), engine.VerdictDrop, "a client reached through br-guest is blocked"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var resolver *tunSrcResolver
			if c.readable {
				resolver = newResolverWith(t, tunTestWAN, c.conntrack)
			}
			w := tunScopeWorker(t, resolver, blockYTImages(config.BlockActionDrop, true))
			if v := w.ProcessPacket(helloFrom(tunTestWAN, 12410)); v != c.want {
				t.Fatalf("verdict %v, want %v: %s", v, c.want, c.why)
			}
		})
	}
}

const tunNewWAN = "94.189.76.228"

func snatFlow(src, wan string, sport int) string {
	return fmt.Sprintf("ipv4 2 tcp 6 300 ESTABLISHED src=%s dst=1.2.3.4 sport=%d dport=443 src=1.2.3.4 dst=%s sport=443 dport=%d use=1\n", src, sport, wan, sport)
}

func (r *tunSrcResolver) age(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, e := range r.cache {
		e.expires = e.expires.Add(-d)
		r.cache[k] = e
	}
}

func TestTunModeAttributesAnyRouterAddressThroughConntrack(t *testing.T) {
	idx := scopeIfaces(t)
	for _, c := range []struct {
		name      string
		stored    string
		src       string
		conntrack string
		want      engine.PacketVerdict
		why       string
	}{
		{"a new uplink address names a client on br-guest", tunTestWAN, tunNewWAN, snatFlow(scopeGuest, tunNewWAN, 12440), engine.VerdictDrop, "the SNAT to an address the resolver has not been told about yet still hides a client on br-guest"},
		{"a new uplink address names a client on br0", tunTestWAN, tunNewWAN, snatFlow(scopeLAN, tunNewWAN, 12440), engine.VerdictAccept, "a client on br0 is outside the set's scope"},
		{"a new uplink address names the router", tunTestWAN, tunNewWAN, snatFlow(tunNewWAN, tunNewWAN, 12440), engine.VerdictAccept, "the router's own flow is outside the set's scope"},
		{"a new uplink address conntrack misses", tunTestWAN, tunNewWAN, "", engine.VerdictDrop, "an unattributed router address keeps the set"},
		{"no stored uplink address names a client on br-guest", "", tunTestWAN, snatFlow(scopeGuest, tunTestWAN, 12440), engine.VerdictDrop, "the SNAT hides a client on br-guest even before the uplink address is known"},
		{"no stored uplink address names the router", "", tunTestWAN, snatFlow(tunTestWAN, tunTestWAN, 12440), engine.VerdictAccept, "the router's own flow is outside the set's scope"},
	} {
		t.Run(c.name, func(t *testing.T) {
			thisHost(t, scopeRouter, tunTestWAN, tunNewWAN)
			routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"], scopeLAN: idx["br0"]})
			w := tunScopeWorker(t, newResolverWith(t, c.stored, c.conntrack), blockYTImages(config.BlockActionDrop, true))
			if v := w.ProcessPacket(helloFrom(c.src, 12440)); v != c.want {
				t.Fatalf("verdict %v, want %v: %s", v, c.want, c.why)
			}
		})
	}
}

func TestTunModeNeverTakesAnUnattributedSourceForTheRouterFromItsRoute(t *testing.T) {
	for _, readable := range []bool{false, true} {
		t.Run(fmt.Sprintf("conntrack readable=%v", readable), func(t *testing.T) {
			idx := scopeIfaces(t)
			thisHost(t, scopeRouter)
			stub := routeByClient(t, map[string]uint32{tunTestWAN: idx["lo"]})
			var resolver *tunSrcResolver
			if readable {
				resolver = newResolverWith(t, tunTestWAN, "")
			}
			w := tunScopeWorker(t, resolver, blockYTImages(config.BlockActionDrop, true))
			if v := w.ProcessPacket(helloFrom(tunTestWAN, 12450)); v != engine.VerdictDrop {
				t.Fatalf("verdict %v: an uplink source b4 cannot attribute is not the router because its route points at loopback", v)
			}
			if calls := stub.got(); readable && len(calls) != 0 {
				t.Fatalf("route lookups %+v for a source conntrack names the uplink address of", calls)
			}
		})
	}
}

func TestTunModeKeepsTheSetForEveryPacketConntrackMisses(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter, tunTestWAN)
	routeByClient(t, map[string]uint32{scopeLAN: idx["br0"]})
	resolver := newResolverWith(t, tunTestWAN, "")
	w := tunScopeWorker(t, resolver, blockYTImages(config.BlockActionDrop, true))
	for i := range 2 {
		if v := w.ProcessPacket(helloFrom(tunTestWAN, 12460)); v != engine.VerdictDrop {
			t.Fatalf("packet %d: verdict %v, want the set kept while the miss is remembered", i+1, v)
		}
	}
	if err := os.WriteFile(resolver.path, []byte(snatFlow(scopeLAN, tunTestWAN, 12460)), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver.age(tunSrcNegativeTTL)
	if v := w.ProcessPacket(helloFrom(tunTestWAN, 12460)); v != engine.VerdictAccept {
		t.Fatalf("verdict %v: once the miss expires conntrack is read again and names a client on br0", v)
	}
}

func TestTunModeKeepsTheAttributionOfEachRouterAddressApart(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter, tunTestWAN, tunNewWAN)
	routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"]})
	conntrack := snatFlow(tunTestWAN, tunTestWAN, 12490) + snatFlow(scopeGuest, tunNewWAN, 12490)
	w := tunScopeWorker(t, newResolverWith(t, tunTestWAN, conntrack), blockYTImages(config.BlockActionDrop, true))
	if v := w.ProcessPacket(helloFrom(tunTestWAN, 12490)); v != engine.VerdictAccept {
		t.Fatalf("verdict %v: the router's own flow from the stored uplink address is outside the set's scope", v)
	}
	if v := w.ProcessPacket(helloFrom(tunNewWAN, 12490)); v != engine.VerdictDrop {
		t.Fatalf("verdict %v: the same ports from another router address belong to a client on br-guest", v)
	}
}

func TestTunModeRemembersTheRouterAsTheSourceNoLongerThanAMiss(t *testing.T) {
	idx := scopeIfaces(t)
	for _, c := range []struct {
		name   string
		origin string
		source string
	}{
		{"conntrack names the uplink address", tunTestWAN, "<nil>"},
		{"conntrack names another router address", scopeRouter, scopeRouter},
	} {
		t.Run(c.name, func(t *testing.T) {
			thisHost(t, scopeRouter, tunTestWAN)
			routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"]})
			resolver := newResolverWith(t, tunTestWAN, snatFlow(c.origin, tunTestWAN, 12470))
			w := tunScopeWorker(t, resolver, blockYTImages(config.BlockActionDrop, true))
			if v := w.ProcessPacket(helloFrom(tunTestWAN, 12470)); v != engine.VerdictAccept {
				t.Fatalf("verdict %v: the router's own flow is outside the set's scope", v)
			}
			if ip, origin := resolver.attribute(6, net.ParseIP(tunTestWAN), 12470, net.ParseIP("1.2.3.4"), 443); origin != tunSrcRouter || ip.String() != c.source {
				t.Fatalf("attribute = %v, %v: a flow conntrack names a router address for is the router's own, sourced from %s", ip, origin, c.source)
			}
			if err := os.WriteFile(resolver.path, []byte(snatFlow(scopeGuest, tunTestWAN, 12470)), 0o644); err != nil {
				t.Fatal(err)
			}
			resolver.age(tunSrcNegativeTTL)
			if v := w.ProcessPacket(helloFrom(tunTestWAN, 12470)); v != engine.VerdictDrop {
				t.Fatalf("verdict %v: a client on br-guest reusing the tuple once a miss would have expired is still taken for the router", v)
			}
		})
	}
}

func TestTunLocalOnlyModeTakesARouterAddressForTheRouter(t *testing.T) {
	scopeIfaces(t)
	thisHost(t, scopeRouter, "10.0.0.9")
	w := tunScopeWorker(t, nil, blockYTImages(config.BlockActionDrop, true))
	pool := &Pool{Workers: []*Worker{w}}
	for _, c := range []struct {
		localOnly bool
		want      engine.PacketVerdict
		why       string
	}{
		{true, engine.VerdictAccept, "with only the router's own connections captured a router address is the router"},
		{false, engine.VerdictDrop, "with tables left to the user a router address may be the SNAT of any client"},
	} {
		pool.SetTUNLocalOnly(c.localOnly)
		if v := w.ProcessPacket(helloFrom("10.0.0.9", 12480)); v != c.want {
			t.Fatalf("local only %v: verdict %v, want %v: %s", c.localOnly, v, c.want, c.why)
		}
	}
}

func scopedDNSBlock(action string) *config.SetConfig {
	return scopeToGuest(copyBlockSet(action), action)
}

func dnsQueryFrom(src, dst string, sport uint16, txid uint16) []byte {
	return makeV4UDPPacket(dns.BuildQuery("www.ads.example", txid, dnsTypeA), net.ParseIP(src), net.ParseIP(dst), sport, 53)
}

func dnsBlockWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = sets
	w := newTestWorker(t, &cfg)
	t.Cleanup(w.wg.Wait)
	return w
}

func TestAScopedDNSBlockSetAnswersOnlyItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter, rigWAN)

	t.Run("drop", func(t *testing.T) {
		w := dnsBlockWorker(t, scopedDNSBlock(config.BlockActionDrop))
		sent := captureSends(t)
		if got := queuedVerdict(t, w, dnsQueryFrom(scopeGuest, scopeRouter, 40000, 0x501), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
			t.Fatalf("verdict %d: a query from br-guest is dropped", got)
		}
		if got := queuedVerdict(t, w, dnsQueryFrom(scopeLAN, scopeRouter, 40001, 0x502), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
			t.Fatalf("verdict %d: a query from br0 is outside the set's source interfaces", got)
		}
		sent.none(t, 30*time.Millisecond, "b4 answered a query its set drops or does not cover")
	})

	t.Run("nxdomain", func(t *testing.T) {
		w := dnsBlockWorker(t, scopedDNSBlock(config.BlockActionReject))
		sent := captureSends(t)
		if got := queuedVerdict(t, w, dnsQueryFrom(scopeGuest, scopeRouter, 40002, 0x503), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
			t.Fatalf("verdict %d: a query from br-guest is answered by b4", got)
		}
		answer := sent.one(t)
		if rcode, _ := dns.ResponseRcode(answer[28:]); rcode != dns.RcodeNXDomain {
			t.Fatalf("the client on br-guest got rcode %d, want NXDOMAIN", rcode)
		}
		if got := queuedVerdict(t, w, dnsQueryFrom(scopeLAN, scopeRouter, 40003, 0x504), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
			t.Fatalf("verdict %d: a query from br0 goes to the resolver", got)
		}
		sent.none(t, 30*time.Millisecond, "b4 answered a query from br0 with NXDOMAIN")
	})

	t.Run("the router's own query", func(t *testing.T) {
		for _, scoped := range []bool{true, false} {
			set := scopedDNSBlock(config.BlockActionDrop)
			if !scoped {
				set.Routing.SourceInterfaces = nil
			}
			w := dnsBlockWorker(t, set)
			want := nfqueue.NfAccept
			if !scoped {
				want = nfqueue.NfDrop
			}
			got := queuedFrom(t, w, dnsQueryFrom(rigWAN, rigOutside, 40004, 0x505), 0, hookAt(nfHookLocalOut), nil)
			if got != want {
				t.Fatalf("scoped=%v: the resolver's upstream query got verdict %d, want %d", scoped, got, want)
			}
		}
	})
}

func TestACopiedQueryIsBlockedOnlyFromTheSetsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil, scopedDNSBlock(config.BlockActionDrop))
	lines := connectionLines(t)

	from := func(in uint32, hook uint8, src, dst string, sport uint16, txid uint16) {
		p := copyOf(hook, src, dst, sport, dns.BuildQuery("www.ads.example", txid, dnsTypeA))
		p.InDev = in
		r.read(p)
	}

	from(idx["br-guest"], copyHookInput, rigDevice, rigRouter, 40100, 0x611)
	if n := countAction(drainLines(lines), dnsActionBlock); n != 1 || r.l.stats.claimsDrop.Load() != 1 {
		t.Fatalf("%d block rows and %d drop claims for a copy from br-guest, want one each", n, r.l.stats.claimsDrop.Load())
	}
	from(idx["br0"], copyHookInput, "192.168.1.101", rigRouter, 40101, 0x612)
	if n := countAction(drainLines(lines), dnsActionBlock); n != 0 || r.l.stats.claimsDrop.Load() != 1 {
		t.Fatalf("%d block rows and %d drop claims after a copy from br0, the set does not cover it", n, r.l.stats.claimsDrop.Load())
	}
	from(0, copyHookOutput, rigWAN, rigOutside, 40102, 0x613)
	if n := countAction(drainLines(lines), dnsActionBlock); n != 0 || r.l.stats.claimsDrop.Load() != 1 {
		t.Fatalf("%d block rows and %d drop claims after the resolver's own upstream query", n, r.l.stats.claimsDrop.Load())
	}
}

func tcpQueryFrom(t *testing.T, w *Worker, from string, query []byte) []byte {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP(from), Port: 40200}})
	}()
	_ = clientEnd.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeDNSTCPMessage(clientEnd, query, time.Second); err != nil {
		t.Fatal(err)
	}
	resp, err := readDNSTCPMessage(clientEnd)
	if err != nil {
		t.Fatalf("no answer over TCP for %s: %v", from, err)
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the DNS over TCP connection was never closed")
	}
	return resp
}

func TestAScopedDNSBlockSetOverTCPUsesTheRouteBackToTheClient(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	stub := routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"], scopeLAN: idx["br0"]})
	w := redirectWorker(t, false, scopedDNSBlock(config.BlockActionReject))
	upstream := tcpDNSUpstream(t)

	resp := tcpQueryFrom(t, w, scopeGuest, dns.BuildQuery("www.ads.example", 0x701, dnsTypeA))
	if rcode, _ := dns.ResponseRcode(resp); rcode != dns.RcodeNXDomain {
		t.Fatalf("the client on br-guest got rcode %d over TCP, want NXDOMAIN", rcode)
	}
	select {
	case <-upstream.queries:
		t.Fatal("a query the set blocks reached the upstream")
	default:
	}

	for _, from := range []string{scopeLAN, scopeRouter} {
		resp = tcpQueryFrom(t, w, from, dns.BuildQuery("www.ads.example", 0x702, dnsTypeA))
		if rcode, _ := dns.ResponseRcode(resp); rcode != dns.RcodeNoError || len(dns.ParseResponseIPs(resp)) == 0 {
			t.Fatalf("%s got rcode %d with %v over TCP, want the upstream's own answer", from, rcode, dns.ParseResponseIPs(resp))
		}
		select {
		case <-upstream.queries:
		case <-time.After(time.Second):
			t.Fatalf("the query from %s never reached the upstream", from)
		}
	}

	for _, c := range stub.got() {
		if c.dst == scopeRouter || c.mark != 0 {
			t.Fatalf("route lookup %+v: the router's own address needs none, and the lookup carries no mark", c)
		}
	}
}

func TestADNSOverTCPQueryIsScopedByTheConfigThatMatchesIt(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	routeByClient(t, map[string]uint32{scopeLAN: idx["br0"]})
	pins := copyPinSet()
	pins.Routing.Enabled = false
	w := redirectWorker(t, false, pins)
	tcpDNSUpstream(t)

	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP(scopeLAN), Port: 40220}})
	}()
	_ = clientEnd.SetDeadline(time.Now().Add(5 * time.Second))
	ask := func(query []byte) []byte {
		t.Helper()
		if err := writeDNSTCPMessage(clientEnd, query, time.Second); err != nil {
			t.Fatal(err)
		}
		resp, err := readDNSTCPMessage(clientEnd)
		if err != nil {
			t.Fatalf("no answer over TCP: %v", err)
		}
		return resp
	}

	ask(dns.BuildQuery("pin.example.com", 0x721, dnsTypeA))
	time.Sleep(20 * time.Millisecond)
	reloaded := config.NewConfig()
	reloaded.Sets = []*config.SetConfig{pins, scopedDNSBlock(config.BlockActionReject)}
	w.UpdateConfig(&reloaded)
	w.matcher.Store(buildMatcher(&reloaded))

	resp := ask(dns.BuildQuery("www.ads.example", 0x722, dnsTypeA))
	if rcode, _ := dns.ResponseRcode(resp); rcode != dns.RcodeNoError {
		t.Fatalf("a query from br0 read after a reload that added a block set scoped to br-guest got rcode %d, want the upstream's answer", rcode)
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the DNS over TCP connection was never closed")
	}
}

func TestADNSAnswerForTheRouterIsLeftToTheNextSet(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"], scopeLAN: idx["br0"]})
	dpi := config.NewSetConfig()
	dpi.Id, dpi.Name, dpi.Enabled = "dpi", "DPI", true
	dpi.Targets.DomainsToMatch = []string{"ads.example"}
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{scopedDNSBlock(config.BlockActionDrop), &dpi}
	w := passiveDNSWorker(t, &cfg)

	answer := buildDNSResponse(0x801, "www.ads.example", []net.IP{net.ParseIP("203.0.113.44")})
	for _, c := range []struct {
		client string
		want   string
	}{
		{scopeRouter, "dpi"},
		{scopeLAN, "dpi"},
		{scopeGuest, "block"},
	} {
		raw := makeV4UDPPacket(answer, net.ParseIP(rigUpstream), net.ParseIP(c.client), 53, 40300)
		pkt, ok := w.parseIPHeaders(raw)
		if !ok {
			t.Fatal("the answer did not parse")
		}
		w.processDnsPacket(&verdictCtx{verdict: engine.VerdictAccept}, pkt, 53, 40300, raw[28:])
		if setID, _, ok := w.hostHints.Lookup(c.client, "203.0.113.44"); !ok || setID != c.want {
			t.Errorf("the answer for %s left a hint for %q, want %q", c.client, setID, c.want)
		}
	}
}

type scopeIfaceStub struct {
	mu    sync.Mutex
	names map[int]string
	calls int
}

func stubScopeIfaceNames(t *testing.T, names map[int]string) *scopeIfaceStub {
	t.Helper()
	s := &scopeIfaceStub{names: names}
	prev := scopeIfaceByIndex
	scopeIfaceByIndex = func(i int) (*net.Interface, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		name, ok := s.names[i]
		if !ok {
			return nil, errors.New("no such network interface")
		}
		return &net.Interface{Index: i, Name: name}, nil
	}
	t.Cleanup(func() { scopeIfaceByIndex = prev })
	return s
}

func (s *scopeIfaceStub) rename(i int, name string) {
	s.mu.Lock()
	s.names[i] = name
	s.mu.Unlock()
}

func (s *scopeIfaceStub) lookups() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestTheScopeSeesARenamedInterfaceOnceItsNameIsRefreshed(t *testing.T) {
	scopeIfaces(t)
	thisHost(t, scopeRouter)
	const guest = 65120
	ifaceCache.Store(uint32(guest), "guest0")
	t.Cleanup(func() {
		ifaceCache.Delete(uint32(guest))
		scopeIfaceNames.Delete(uint32(guest))
	})
	names := stubScopeIfaceNames(t, map[int]string{guest: "guest0"})
	w := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, true))

	if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12420), nil, 0, guest); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: guest0 is not one of the set's source interfaces", got)
	}
	names.rename(guest, "br-guest")
	if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12421), nil, 0, guest); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: until its refresh the scope keeps the name it read", got)
	}
	if n := names.lookups(); n != 1 {
		t.Fatalf("%d interface lookups for two packets before the refresh, want one", n)
	}

	scopeIfaceNames.Store(uint32(guest), scopeIfaceEntry{name: "guest0", at: time.Now().Add(-scopeIfaceTTL)})
	if got := queuedVerdict(t, w, helloFrom("10.0.0.1", 12422), nil, 0, guest); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: once refreshed the scope sees the interface renamed to br-guest", got)
	}
	if name := getIfaceName(guest); name != "br-guest" {
		t.Fatalf("the interface cache still names index %d %q after the scope read its new name", guest, name)
	}
}

func TestASetListingLoopbackStillLeavesOutTheRoutersOwnTraffic(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	stubInjectRoute(t, idx["lo"], true)
	set := blockYTImages(config.BlockActionDrop, true)
	set.Routing.SourceInterfaces = []string{"br-guest", "lo"}
	w := newInjectingWorker(t, set)

	if got := queuedVerdict(t, w, helloFrom("10.0.0.3", 12430), nil, 0, idx["lo"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: a packet arriving on loopback is the router's own", got)
	}
	if got := queuedVerdict(t, w, helloFrom("10.0.0.4", 12431), nil, 0, 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: a source routed back through loopback is the router itself", got)
	}
	if got := queuedVerdict(t, w, helloFrom("10.0.0.5", 12432), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the set still blocks br-guest", got)
	}
}

func TestTheScopeIsBuiltOnlyForAConfigWithAnInterfaceScopedBlockSet(t *testing.T) {
	scopeIfaces(t)
	thisHost(t, scopeRouter)
	for _, scoped := range []bool{false, true} {
		w := newInjectingWorker(t, blockYTImages(config.BlockActionDrop, scoped))
		pkt, ok := w.parseIPHeaders(ytimgHello())
		if !ok {
			t.Fatal("the ClientHello did not parse")
		}
		vc := &verdictCtx{}
		keep, client := w.blockScope(vc, pkt, w.getMatcher()), w.clientScope(pkt.src)
		if !scoped && (keep != nil || vc.scope != nil || client != nil) {
			t.Fatal("a config without an interface scoped block set built a scope")
		}
		if scoped && (keep == nil || vc.scope == nil || client == nil || client.filter() == nil) {
			t.Fatal("a config with an interface scoped block set built no scope")
		}
		if w.clientScope(nil) != nil {
			t.Fatal("a scope was built for no client")
		}
	}
}

func TestACopiedQueryIsAnsweredNXDOMAINOnlyOnTheSetsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil, scopedDNSBlock(config.BlockActionReject))
	routeByClient(t, map[string]uint32{rigDevice: idx["br-guest"], "192.168.1.101": idx["br0"]})
	lines := connectionLines(t)

	for _, c := range []struct {
		in     uint32
		client string
		sport  uint16
		txid   uint16
		want   int
	}{
		{idx["br-guest"], rigDevice, 40130, 0x641, 1},
		{idx["br0"], "192.168.1.101", 40131, 0x642, 0},
	} {
		p := copyOf(copyHookInput, c.client, rigRouter, c.sport, dns.BuildQuery("www.ads.example", c.txid, dnsTypeA))
		p.InDev = c.in
		r.read(p)
		if n := countAction(drainLines(lines), dnsActionSinkhole); n != c.want {
			t.Fatalf("%s: %d sinkhole rows, want %d", c.client, n, c.want)
		}
		q := newFakeModQueue()
		raw := replyRaw(buildDNSResponse(c.txid, "www.ads.example", resolverIPs), rigRouter, c.client, c.sport)
		r.reply(q, raw)
		if c.want == 0 {
			passedUnchanged(t, q.now(t), "the resolver's answer for a client on br0")
			continue
		}
		if rcode, _ := dns.ResponseRcode(rewritten(t, q.now(t), raw)); rcode != dns.RcodeNXDomain {
			t.Fatalf("the client on br-guest got rcode %d, want NXDOMAIN", rcode)
		}
	}
}

func TestACopyOfAQueryTheRouterSendsIsOutsideTheScope(t *testing.T) {
	idx := scopeIfaces(t)
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil, scopedDNSBlock(config.BlockActionDrop))
	routeByClient(t, map[string]uint32{"192.168.1.77": idx["br-guest"]})
	lines := connectionLines(t)

	r.read(copyOf(copyHookOutput, "192.168.1.77", rigOutside, 40140, dns.BuildQuery("www.ads.example", 0x651, dnsTypeA)))
	if n := countAction(drainLines(lines), dnsActionBlock); n != 0 || r.l.stats.claimsDrop.Load() != 0 {
		t.Fatalf("%d block rows for a copy taken as the router sent it, from an address routed back through br-guest", n)
	}
	r.read(copyOf(copyHookInput, "192.168.1.77", rigRouter, 40141, dns.BuildQuery("www.ads.example", 0x652, dnsTypeA)))
	if n := countAction(drainLines(lines), dnsActionBlock); n != 1 {
		t.Fatalf("%d block rows for the same address arriving at the router, want the route back through br-guest to decide", n)
	}
}
