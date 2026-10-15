package nfq

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

func newFragmentingYTImagesSet() config.SetConfig {
	set := newPassiveSet()
	set.Fragmentation.Strategy = "tcp"
	set.Fragmentation.SNIPosition = 1
	set.Fragmentation.SNIPositionMax = 0
	set.Fragmentation.MiddleSNI = false
	set.TCP.Seg2Delay = 0
	set.TCP.Seg2DelayMax = 0
	return set
}

func newInjectingWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = sets
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	t.Cleanup(w.wg.Wait)
	return w
}

func makeV4TCPPacketFromPort(payload []byte, seq uint32, sport uint16, flags byte) []byte {
	pkt := makeV4TCPPacketFlags(payload, seq, flags)
	binary.BigEndian.PutUint16(pkt[20:22], sport)
	return pkt
}

func tlsAppData(n int) []byte {
	rec := make([]byte, 5+n)
	rec[0] = 0x17
	rec[1] = 0x03
	rec[2] = 0x03
	binary.BigEndian.PutUint16(rec[3:5], uint16(n))
	return rec
}

func loggedSet(t *testing.T, ch chan string) string {
	t.Helper()
	f := nextConnectionLine(t, ch)
	if len(f) < 7 {
		t.Fatalf("malformed connection line: %v", f)
	}
	if f[5] != "" {
		return f[5]
	}
	return f[2]
}

func TestLaterPacketsOfAConnectionKeepTheSetItsClientHelloMatched(t *testing.T) {
	for _, domainOnly := range []bool{false, true} {
		name := "learning"
		if domainOnly {
			name = "domain-only"
		}
		t.Run(name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			set.Targets.DomainOnly = domainOnly
			w := newInjectingWorker(t, &set)

			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			hello := buildClientHello("i.ytimg.com", 16, 0xAB)
			const seq = uint32(1000)
			if v := w.ProcessPacket(makeV4TCPPacket(hello, seq)); v != engine.VerdictDrop {
				t.Fatalf("the ClientHello names a domain of the set and must get its strategy, got %v", v)
			}
			if got := loggedSet(t, ch); got != set.Name {
				t.Fatalf("the ClientHello row: want set %q, got %q", set.Name, got)
			}

			next := seq + uint32(len(hello))
			data := tlsAppData(64)
			if v := w.ProcessPacket(makeV4TCPPacket(data, next)); v != engine.VerdictDrop {
				t.Errorf("a later data packet of the same connection must get the set's strategy, got %v", v)
			}
			if got := loggedSet(t, ch); got != set.Name {
				t.Errorf("a later data packet of the same connection: want set %q, got %q", set.Name, got)
			}

			if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, next+uint32(len(data)), 0x10)); v != engine.VerdictAccept {
				t.Errorf("a bare ACK has nothing to fragment, got %v", v)
			}
			if got := loggedSet(t, ch); got != set.Name {
				t.Errorf("a bare ACK of the same connection: want set %q, got %q", set.Name, got)
			}
		})
	}
}

func TestDomainOnlySetStillIgnoresOtherConnectionsToTheSameAddress(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	w.ProcessPacket(makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000))
	if got := loggedSet(t, ch); got != set.Name {
		t.Fatalf("the ClientHello row: want set %q, got %q", set.Name, got)
	}

	if v := w.ProcessPacket(makeV4TCPPacketFromPort(tlsAppData(64), 5000, 23456, 0x18)); v != engine.VerdictAccept {
		t.Errorf("another connection to the same address must not be matched by the address, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("another connection to the same address must not be attributed to the set, got %q", got)
	}

	other := buildClientHello("www.google.com", 16, 0xAB)
	if v := w.ProcessPacket(makeV4TCPPacketFromPort(other, 7000, 34567, 0x18)); v != engine.VerdictAccept {
		t.Errorf("a neighbouring domain on the same address must not get the set's strategy, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("a neighbouring domain on the same address must not be attributed to the set, got %q", got)
	}
	if v := w.ProcessPacket(makeV4TCPPacketFromPort(tlsAppData(64), 7000+uint32(len(other)), 34567, 0x18)); v != engine.VerdictAccept {
		t.Errorf("a later packet of the neighbouring domain's connection must not get the set's strategy, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("a later packet of the neighbouring domain's connection must not be attributed to the set, got %q", got)
	}
}

func TestAReusedPortIsNotDecidedByThePreviousConnectionsName(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	w.ProcessPacket(makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000))
	if got := loggedSet(t, ch); got != set.Name {
		t.Fatalf("the ClientHello row: want set %q, got %q", set.Name, got)
	}

	if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 90000, 0x02)); v != engine.VerdictAccept {
		t.Errorf("a SYN opens a new connection whose name is not known yet, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("a SYN on a reused port must not take the previous connection's set, got %q", got)
	}

	other := buildClientHello("www.google.com", 16, 0xAB)
	if v := w.ProcessPacket(makeV4TCPPacket(other, 90001)); v != engine.VerdictAccept {
		t.Errorf("a ClientHello is decided by its own name, not by the previous connection on the same port, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("a ClientHello for another domain on a reused port must not be attributed to the set, got %q", got)
	}
	if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 90001+uint32(len(other)))); v != engine.VerdictAccept {
		t.Errorf("a later packet of the new connection follows the new name, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("a later packet of the new connection must not be attributed to the set, got %q", got)
	}
}

func TestASetWithoutFragmentationLeavesLaterEncryptedDataAloneInBothModes(t *testing.T) {
	for _, domainOnly := range []bool{false, true} {
		set := newPassiveSet()
		set.Targets.DomainOnly = domainOnly
		set.TCP.HTTPMethodEOL = true
		w := newInjectingWorker(t, &set)

		ch, _ := log.GetConnectionHub().Subscribe()

		hello := buildClientHello("i.ytimg.com", 16, 0xAB)
		if v := w.ProcessPacket(makeV4TCPPacket(hello, 1000)); v != engine.VerdictDrop {
			t.Fatalf("domain-only=%v: the ClientHello must get the set's techniques, got %v", domainOnly, v)
		}
		loggedSet(t, ch)
		if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictAccept {
			t.Errorf("domain-only=%v: a set without fragmentation or desync leaves encrypted data alone, as it does on an address it learned; got %v", domainOnly, v)
		}
		if got := loggedSet(t, ch); got != set.Name {
			t.Errorf("domain-only=%v: the data row still belongs to the connection's set, want %q, got %q", domainOnly, set.Name, got)
		}
		log.GetConnectionHub().Unsubscribe(ch)
	}
}

func newStaticAddressSet(fragment bool) config.SetConfig {
	set := newPassiveSet()
	set.Id = "address-set"
	set.Name = "Address set"
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	if fragment {
		set.Fragmentation.Strategy = "tcp"
		set.Fragmentation.SNIPosition = 1
		set.Fragmentation.SNIPositionMax = 0
		set.TCP.Seg2Delay = 0
		set.TCP.Seg2DelayMax = 0
	}
	return set
}

func buildClientHelloWithoutSNI() []byte {
	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00, 0x00, 0x02, 0x13, 0x01, 0x01, 0x00, 0x00, 0x00)
	handshake := append([]byte{0x01, 0x00, 0x00, byte(len(body))}, body...)
	return append([]byte{0x16, 0x03, 0x01, 0x00, byte(len(handshake))}, handshake...)
}

func TestANewConnectionOnAReusedPortForgetsThePreviousName(t *testing.T) {
	set := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	w.ProcessPacket(makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000))
	if got := loggedSet(t, ch); got != set.Name {
		t.Fatalf("the ClientHello row: want set %q, got %q", set.Name, got)
	}
	w.ProcessPacket(makeV4TCPPacketFlags(nil, 90000, 0x02))
	loggedSet(t, ch)

	if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 90001, 0x10)); v != engine.VerdictAccept {
		t.Errorf("the new connection's handshake ACK must pass, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("the new connection's handshake ACK must not carry the previous connection's set, got %q", got)
	}

	bare := buildClientHelloWithoutSNI()
	if v := w.ProcessPacket(makeV4TCPPacket(bare, 90001)); v != engine.VerdictAccept {
		t.Errorf("a ClientHello without a name matches no domain set, got %v", v)
	}
	loggedSet(t, ch)
	if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 90001+uint32(len(bare)))); v != engine.VerdictAccept {
		t.Errorf("a later packet of a connection without a name must not get the previous connection's strategy, got %v", v)
	}
	if got := loggedSet(t, ch); got != "" {
		t.Errorf("a later packet of a connection without a name must not carry the previous connection's set, got %q", got)
	}
}

func TestABlockedNameDoesNotResetLaterPacketsThroughAnAddressSet(t *testing.T) {
	blocked := newDropBlockSet()
	address := newStaticAddressSet(false)
	w := newInjectingWorker(t, &blocked, &address)

	if v := w.ProcessPacket(makeV4TCPPacket(buildClientHello("blocked.example", 16, 0xAB), 1000)); v != engine.VerdictDrop {
		t.Fatalf("the blocked name's ClientHello must be dropped, got %v", v)
	}
	if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 2000, 0x10)); v != engine.VerdictAccept {
		t.Errorf("a later packet is decided by the address set, as before, not blocked through it; got %v", v)
	}

	w.ProcessPacket(makeV4TCPPacketFlags(nil, 90000, 0x02))
	if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 90001, 0x10)); v != engine.VerdictAccept {
		t.Errorf("a new connection on the same port must not be blocked by the previous connection's name, got %v", v)
	}
}

func TestAnAddressSetKeepsLaterPacketsOfADomainOnlyConnection(t *testing.T) {
	images := newFragmentingYTImagesSet()
	address := newStaticAddressSet(true)
	w := newInjectingWorker(t, &images, &address)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	if got := loggedSet(t, ch); got != images.Name {
		t.Fatalf("the ClientHello row: want set %q, got %q", images.Name, got)
	}

	if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictDrop {
		t.Errorf("the address set fragments later data, as before, got %v", v)
	}
	f := nextConnectionLine(t, ch)
	if len(f) < 7 {
		t.Fatalf("malformed connection line: %v", f)
	}
	if f[2] != images.Name || f[5] != address.Name {
		t.Errorf("a later row names the connection's domain set and the address set that handled it: want %q and %q, got %q and %q", images.Name, address.Name, f[2], f[5])
	}
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != address.Id {
		t.Errorf("the address set keeps the connection's incoming handling, as before, got %v", bound)
	}
}

func TestASetThatLeavesDataAloneDoesNotTakeItFromAnotherSet(t *testing.T) {
	newImages := func() config.SetConfig {
		set := newPassiveSet()
		set.TCP.HTTPMethodEOL = true
		return set
	}

	t.Run("address set", func(t *testing.T) {
		images := newImages()
		address := newStaticAddressSet(true)
		w := newInjectingWorker(t, &images, &address)

		hello := buildClientHello("i.ytimg.com", 16, 0xAB)
		w.ProcessPacket(makeV4TCPPacket(hello, 1000))
		if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictDrop {
			t.Errorf("the address set still fragments data the domain set leaves alone, got %v", v)
		}
	})

	t.Run("dns hint", func(t *testing.T) {
		images := newImages()
		video := newFragmentingYTImagesSet()
		video.Id = "yt-video"
		video.Name = "YT video"
		video.Targets.DomainOnly = false
		video.Targets.DomainsToMatch = []string{"youtube.com"}
		w := newInjectingWorker(t, &images, &video)
		w.storeHostHint("10.0.0.1", "1.2.3.4", &video, "www.youtube.com", "dns")

		hello := buildClientHello("i.ytimg.com", 16, 0xAB)
		w.ProcessPacket(makeV4TCPPacket(hello, 1000))
		if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictDrop {
			t.Errorf("the hinted set still fragments data the domain set leaves alone, got %v", v)
		}
	})
}

func TestLaterPacketsReuseTheConnectionsDecision(t *testing.T) {
	images := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &images)

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
	if _, _, by, set, ok := w.tlsCache.Decision("10.0.0.1:12345->1.2.3.4:443"); !ok || by != w.getMatcher() || set != &images {
		t.Errorf("the connection's set must be decided once and kept, got decided=%v set=%v", by == w.getMatcher(), set)
	}

	other := buildClientHello("www.google.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacketFromPort(other, 5000, 23456, 0x18))
	w.ProcessPacket(makeV4TCPPacketFromPort(tlsAppData(64), 5000+uint32(len(other)), 23456, 0x18))
	if _, _, by, set, ok := w.tlsCache.Decision("10.0.0.1:23456->1.2.3.4:443"); !ok || by != w.getMatcher() || set != nil {
		t.Errorf("a name that matches no set must be remembered as such, got decided=%v set=%v", by == w.getMatcher(), set)
	}
}

func TestASetChangeForgetsTheConnectionsDecision(t *testing.T) {
	images := newFragmentingYTImagesSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&images}
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	t.Cleanup(w.wg.Wait)
	pool := &Pool{Workers: []*Worker{w}, state: &runtimeState{tlsCache: w.tlsCache}}
	const key = "10.0.0.1:12345->1.2.3.4:443"

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))

	if err := pool.UpdateConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if _, _, by, _, _ := w.tlsCache.Decision(key); by == nil {
		t.Fatal("a reload that leaves the sets alone keeps the decision")
	}

	changed := cfg.Clone()
	changed.Sets[0].Name = "Renamed"
	if err := pool.UpdateConfig(changed); err != nil {
		t.Fatal(err)
	}
	if _, _, by, set, _ := w.tlsCache.Decision(key); by != nil || set != nil {
		t.Fatal("a set change must drop decisions that point at the old matcher and sets")
	}

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)
	w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 2000+uint32(len(hello))))
	if got := loggedSet(t, ch); got != "Renamed" {
		t.Errorf("the next packet decides again with the new sets, want %q, got %q", "Renamed", got)
	}
}

func TestADuplicateRowNamesTheConnectionsDomainSet(t *testing.T) {
	set := newPassiveSet()
	set.TCP.Duplicate.Enabled = true
	set.TCP.Duplicate.Count = 1
	w := newInjectingWorker(t, &set)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	loggedSet(t, ch)

	w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
	f := nextConnectionLine(t, ch)
	if len(f) < 10 || f[9] != "tcp-dup" {
		t.Fatalf("want a tcp-dup row, got %v", f)
	}
	if f[2] != set.Name || f[5] != "" {
		t.Errorf("a duplicated later packet is the domain set's: want sni %q and no address set, got %q and %q", set.Name, f[2], f[5])
	}
}

func TestLaterPacketsFollowTheirOwnConnectionOnAnAddressAnotherSetLearned(t *testing.T) {
	images := newFragmentingYTImagesSet()
	video := newFragmentingYTImagesSet()
	video.Id = "yt-video"
	video.Name = "YT video"
	video.Targets.DomainOnly = false
	video.Targets.DomainsToMatch = []string{"youtube.com"}
	w := newInjectingWorker(t, &images, &video)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	w.ProcessPacket(makeV4TCPPacketFromPort(buildClientHello("www.youtube.com", 16, 0xAB), 1000, 1111, 0x18))
	if got := loggedSet(t, ch); got != video.Name {
		t.Fatalf("the youtube.com ClientHello row: want set %q, got %q", video.Name, got)
	}

	hello := buildClientHello("i.ytimg.com", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacketFromPort(hello, 5000, 2222, 0x18))
	if got := loggedSet(t, ch); got != images.Name {
		t.Fatalf("the i.ytimg.com ClientHello row: want set %q, got %q", images.Name, got)
	}

	w.ProcessPacket(makeV4TCPPacketFromPort(tlsAppData(64), 5000+uint32(len(hello)), 2222, 0x18))
	if got := loggedSet(t, ch); got != images.Name {
		t.Errorf("a later packet of the i.ytimg.com connection belongs to %q, the set its ClientHello matched, not to %q that learned the address from another connection; got %q", images.Name, video.Name, got)
	}
}

func TestRegisterLearnedRoute_SkipsDomainOnly(t *testing.T) {
	prevIP := RoutingLearnIPFunc
	prevHost := RoutingLearnHostFunc
	defer func() {
		RoutingLearnIPFunc = prevIP
		RoutingLearnHostFunc = prevHost
	}()

	var calls int
	RoutingLearnIPFunc = func(cfg *config.Config, set *config.SetConfig, ip net.IP) {
		calls++
	}
	var hostCalls int
	var lastHost string
	RoutingLearnHostFunc = func(cfg *config.Config, set *config.SetConfig, host string) {
		hostCalls++
		lastHost = host
	}

	cfg := &config.Config{}
	dst := net.ParseIP("1.2.3.4")

	set := config.NewSetConfig()
	set.Name = "cdn"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Targets.DomainOnly = true
	registerLearnedRoute(cfg, &set, dst, "cdn.example.com")
	if calls != 0 {
		t.Errorf("domain-only set must not register a learned route, got %d calls", calls)
	}
	if hostCalls != 0 {
		t.Errorf("domain-only set must not register a learned host, got %d calls", hostCalls)
	}

	set.Targets.DomainOnly = false
	registerLearnedRoute(cfg, &set, dst, "cdn.example.com")
	if calls != 1 {
		t.Errorf("non-domain-only set must register a learned route, got %d calls", calls)
	}
	if hostCalls != 1 || lastHost != "cdn.example.com" {
		t.Errorf("non-domain-only set must register the matched host, got %d calls (host %q)", hostCalls, lastHost)
	}

	registerLearnedRoute(cfg, &set, dst, "")
	if hostCalls != 1 {
		t.Errorf("empty host must not be registered, got %d calls", hostCalls)
	}
}
