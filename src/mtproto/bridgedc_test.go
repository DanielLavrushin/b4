package mtproto

import (
	"fmt"
	"net"
	"testing"
	"time"
)

const androidRandomDC = 27330

func resetBridgeDCState(t *testing.T) {
	t.Helper()
	wsResetState()
	tcpResetState()
	bridgeResetRejects()
	t.Cleanup(func() {
		wsResetState()
		tcpResetState()
		bridgeResetRejects()
	})
}

func TestDCForIPKnowsTheAndroidMediaAddressOfDC4(t *testing.T) {
	dc, ok := dcForIP(net.ParseIP("149.154.167.255"))
	if !ok || dc != 4 {
		t.Fatalf("149.154.167.255 serves only DC 4 media, got dc=%d ok=%v; the /24 range sends it to DC 2, which answers -444", dc, ok)
	}
}

func TestBridgeSiblingDC(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{2, 4}, {4, 2}, {1, 3}, {3, 1}, {-2, -4}, {-4, -2}, {5, 0}, {203, 0},
	} {
		if got := bridgeSiblingDC(tc.in); got != tc.want {
			t.Errorf("bridgeSiblingDC(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestBridgeChooseDC(t *testing.T) {
	resetBridgeDCState(t)
	for _, tc := range []struct {
		name      string
		ip        string
		handshake int
		dc        int
		src       string
		guessed   bool
		ok        bool
	}{
		{"android to a range address", "149.154.167.200", androidRandomDC, 2, "ip-range", true, true},
		{"android to the DC 4 media address", "149.154.167.255", androidRandomDC, 4, "ip", true, true},
		{"desktop to its own DC address", "149.154.167.51", 2, 2, "ip", false, true},
		{"desktop media session to a DC 2 address", "149.154.167.51", -2, -2, "ip+handshake-media", false, true},
		{"desktop naming another DC than the address", "149.154.167.51", 4, 2, "ip", true, true},
		{"handshake decides where the address says nothing", "149.154.175.211", 3, 3, "handshake", false, true},
		{"nothing to go on", "149.154.175.211", androidRandomDC, 0, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := bridgeChooseDC("test", net.ParseIP(tc.ip), tc.handshake)
			if c.dc != tc.dc || c.src != tc.src || c.guessed != tc.guessed || ok != tc.ok {
				t.Errorf("bridgeChooseDC(%s, %d) = %d %q guessed=%v ok=%v, want %d %q guessed=%v ok=%v",
					tc.ip, tc.handshake, c.dc, c.src, c.guessed, ok, tc.dc, tc.src, tc.guessed, tc.ok)
			}
		})
	}
}

func TestBridgeGuessThatDrewInvalidDCMovesToTheSibling(t *testing.T) {
	resetBridgeDCState(t)
	native := transportPlan{kind: transportWS, dc: 2, sni: "kws2.web.telegram.org", dialHost: telegramWSEdgeIP, native: true}
	ip := net.ParseIP("149.154.167.200")

	first, _ := bridgeChooseDC("test", ip, androidRandomDC)
	if !first.errHandler(dialInfo{transport: "ws-pool", plan: native}, "test", ip)(tgErrInvalidDC) {
		t.Fatal("-444 must be swallowed and the session cut, so the client redials")
	}
	if wsEndpointCooling(telegramWSEdgeIP, "kws2.web.telegram.org") || wsCooldownActive(2) {
		t.Fatal("the edge carried the session to DC 2 as asked; the label was wrong, so the route must not be ranked down for every other DC 2 session")
	}

	next, ok := bridgeChooseDC("test", ip, androidRandomDC)
	if !ok || next.dc != 4 || next.src != "ip-range+learned" {
		t.Fatalf("the next session to that address has to go to DC 4, got %d %q ok=%v", next.dc, next.src, ok)
	}
	if media, _, found := bridgeLearnedDC(ip, -2); !found || media != -4 {
		t.Fatalf("a media label keeps its sign on the sibling, got %d found=%v", media, found)
	}
	if named, _ := bridgeChooseDC("test", ip, -2); named.dc != -2 || named.guessed {
		t.Fatalf("a client that names DC -2 keeps it, got %d guessed=%v", named.dc, named.guessed)
	}
	if other, _ := bridgeChooseDC("test", net.ParseIP("149.154.167.201"), androidRandomDC); other.dc != 2 {
		t.Fatalf("another address is untouched, got %d", other.dc)
	}

	if first.errHandler(dialInfo{transport: "ws-pool", plan: native}, "test", ip)(tgErrFlood) {
		t.Fatal("other codes belong to the client and must be passed on")
	}
}

func TestBridgeClientNamedDCStillRanksTheRouteDown(t *testing.T) {
	resetBridgeDCState(t)
	native := transportPlan{kind: transportWS, dc: 2, sni: "kws2.web.telegram.org", dialHost: telegramWSEdgeIP, native: true}
	ip := net.ParseIP("149.154.167.51")

	c, _ := bridgeChooseDC("test", ip, 2)
	if !c.errHandler(dialInfo{transport: "ws-pool", plan: native}, "test", ip)(tgErrInvalidDC) {
		t.Fatal("-444 must be swallowed")
	}
	if !wsEndpointCooling(telegramWSEdgeIP, "kws2.web.telegram.org") {
		t.Fatal("the client named DC 2 itself, so a -444 indicts the route")
	}
	if again, _ := bridgeChooseDC("test", ip, 2); again.dc != 2 || again.src != "ip" {
		t.Fatalf("a DC the client named is not relabelled, got %d %q", again.dc, again.src)
	}
}

func TestBridgeConcurrentSessionsToARefusedAddressLearnOnce(t *testing.T) {
	resetBridgeDCState(t)
	native := transportPlan{kind: transportWS, dc: 2, sni: "kws2.web.telegram.org", dialHost: telegramWSEdgeIP, native: true}
	ip := net.ParseIP("149.154.167.206")

	a, _ := bridgeChooseDC("test", ip, androidRandomDC)
	b, _ := bridgeChooseDC("test", ip, androidRandomDC)
	a.errHandler(dialInfo{transport: "ws-pool", plan: native}, "test", ip)(tgErrInvalidDC)
	b.errHandler(dialInfo{transport: "ws-pool", plan: native}, "test", ip)(tgErrInvalidDC)
	if wsEndpointCooling(telegramWSEdgeIP, "kws2.web.telegram.org") {
		t.Fatal("a second session dialled before the first -444 was learned is the same mislabel, not a route fault")
	}
	if next, ok := bridgeChooseDC("test", ip, androidRandomDC); !ok || next.dc != 4 {
		t.Fatalf("two -444s as DC 2 must not count as DC 4 refusing too, got %d ok=%v", next.dc, ok)
	}
}

func TestBridgeAddressThatRefusesBothSiblingsBypassesTheRoutes(t *testing.T) {
	resetBridgeDCState(t)
	native2 := transportPlan{kind: transportWS, dc: 2, sni: "kws2.web.telegram.org", dialHost: telegramWSEdgeIP, native: true}
	native4 := transportPlan{kind: transportWS, dc: 4, sni: "kws4.web.telegram.org", dialHost: telegramWSEdgeIP, native: true}
	ip := net.ParseIP("149.154.167.207")

	c2, _ := bridgeChooseDC("test", ip, androidRandomDC)
	c2.errHandler(dialInfo{transport: "ws-pool", plan: native2}, "test", ip)(tgErrInvalidDC)
	c4, _ := bridgeChooseDC("test", ip, androidRandomDC)
	if c4.dc != 4 {
		t.Fatalf("second session should try DC 4, got %d", c4.dc)
	}
	c4.errHandler(dialInfo{transport: "ws-pool", plan: native4}, "test", ip)(tgErrInvalidDC)

	if wsEndpointCooling(telegramWSEdgeIP, "kws2.web.telegram.org") || wsEndpointCooling(telegramWSEdgeIP, "kws4.web.telegram.org") {
		t.Fatal("an address that fits neither label says nothing about the routes that serve both data centers for every other session")
	}
	if _, ok := bridgeChooseDC("test", ip, androidRandomDC); ok {
		t.Fatal("an address that refused both data centers has to bypass the data center routes, the way an address b4 cannot map does")
	}
	if c, ok := bridgeChooseDC("test", ip, 4); !ok || c.dc != 4 || c.guessed {
		t.Fatalf("a client that names its data center still gets it, got %d guessed=%v ok=%v", c.dc, c.guessed, ok)
	}
}

func TestBridgeAddressOfADCWithoutSiblingBypassesTheRoutes(t *testing.T) {
	resetBridgeDCState(t)
	addr := "91.108.56.200:443"
	tcp := transportPlan{kind: transportTCP, addr: addr}
	ip := net.ParseIP("91.108.56.200")

	c, ok := bridgeChooseDC("test", ip, androidRandomDC)
	if !ok || c.dc != 5 || !c.guessed {
		t.Fatalf("the DC 5 range labels the address as a guess, got %d guessed=%v ok=%v", c.dc, c.guessed, ok)
	}
	if !c.errHandler(dialInfo{transport: "tcp://" + addr, plan: tcp}, "test", ip)(tgErrInvalidDC) {
		t.Fatal("-444 must be swallowed")
	}
	if tcpAddrInCooldown(addr) {
		t.Fatal("the label was a guess, so the route is not ranked down")
	}
	if _, ok := bridgeChooseDC("test", ip, androidRandomDC); ok {
		t.Fatal("DC 5 has no sibling, so later sessions to the address bypass the data center routes")
	}
}

func TestBridgeRejectsExpire(t *testing.T) {
	resetBridgeDCState(t)
	ip := net.ParseIP("149.154.167.202")
	bridgeRejectDC(ip, 2)
	bridgeRejectMu.Lock()
	r := bridgeRejects[ip.String()]
	r.until = time.Now().Add(-time.Second)
	bridgeRejects[ip.String()] = r
	bridgeRejectMu.Unlock()
	if c, _ := bridgeChooseDC("test", ip, androidRandomDC); c.dc != 2 {
		t.Fatalf("an expired entry must not steer, got %d", c.dc)
	}
	bridgeRejectDC(ip, 2)
	if c, _ := bridgeChooseDC("test", ip, androidRandomDC); c.dc != 4 {
		t.Fatalf("a fresh -444 after expiry learns again, got %d", c.dc)
	}

	bridgeRejectDC(ip, 4)
	bridgeRejectMu.Lock()
	r = bridgeRejects[ip.String()]
	bridgeRejectMu.Unlock()
	if !r.unresolved || time.Until(r.until) > bridgeUnresolvedTTL {
		t.Fatalf("a bypass lasts %s, not the sibling's %s: %+v", bridgeUnresolvedTTL, bridgeRejectTTL, r)
	}
}

func TestBridgeRejectsStayBounded(t *testing.T) {
	resetBridgeDCState(t)
	for i := 0; i < bridgeRejectMax+40; i++ {
		bridgeRejectDC(net.ParseIP(fmt.Sprintf("10.0.%d.%d", i/250, i%250)), 2)
	}
	bridgeRejectMu.Lock()
	n := len(bridgeRejects)
	bridgeRejectMu.Unlock()
	if n > bridgeRejectMax {
		t.Fatalf("%d entries kept, the cap is %d", n, bridgeRejectMax)
	}
}
