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

		hello := buildClientHello("i.ytimg.com", 16, 0xAB)
		if v := w.ProcessPacket(makeV4TCPPacket(hello, 1000)); v != engine.VerdictDrop {
			t.Fatalf("domain-only=%v: the ClientHello must get the set's techniques, got %v", domainOnly, v)
		}
		if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictAccept {
			t.Errorf("domain-only=%v: a set without fragmentation or desync leaves encrypted data alone, as it does on an address it learned; got %v", domainOnly, v)
		}
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
