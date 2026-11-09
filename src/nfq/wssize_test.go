package nfq

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

func wsFixture() (*config.Config, *config.SetConfig) {
	cfg := config.NewConfig()
	set := newPassiveSet()
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	set.Targets.IpsToMatch = []string{"93.184.216.34", "2001:db8::2"}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	cfg.Sets = []*config.SetConfig{&set}
	return &cfg, &set
}

func TestWSSizePackets(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		at := 20
		if ver == IPv6 {
			at = 40
		}
		for _, scale := range []int{0, 1, 14} {
			for _, window := range []int{0, 1, 65535} {
				cfg, set := wsFixture()
				set.TCP.WSSize.Scale, set.TCP.WSSize.Window = scale, window
				c := newWSSizeCache()
				p := synWithOptions(ver, []byte{3, 3, 7, 0}, nil, 100)
				original := append([]byte(nil), p...)
				if !c.rewrite(p, at, set, cfg, time.Now()) {
					t.Fatal("SYN was not rewritten")
				}
				if got := int(binary.BigEndian.Uint16(p[at+14:])); got != window {
					t.Fatalf("window %d", got)
				}
				wantScale := min(scale, 7)
				if int(p[at+22]) != wantScale {
					t.Fatal("scale was increased or zero lost")
				}
				if len(p) != len(original) || !bytes.Equal(p[at:at+14], original[at:at+14]) {
					t.Fatal("unrelated TCP fields changed")
				}
				if ver == IPv4 && (!sock.TCPChecksumValid(p) || !ipv4HeaderChecksumValid(p[:at])) {
					t.Fatal("IPv4 checksum")
				}
				if ver == IPv6 && !sock.TCPChecksumValidV6(p) {
					t.Fatal("IPv6 checksum")
				}
			}
		}
		cfg, set := wsFixture()
		p := synWithOptions(ver, nil, nil, 100)
		n := len(p)
		if !newWSSizeCache().rewrite(p, at, set, cfg, time.Now()) || len(p) != n {
			t.Fatal("missing WS must remain absent")
		}
	}
}

func TestWSSizeLifecycle(t *testing.T) {
	cfg, set := wsFixture()
	c := newWSSizeCache()
	now := time.Now()
	syn := func(isn uint32) []byte { return synWithOptions(IPv4, []byte{3, 3, 7, 0}, nil, isn) }
	run := func(p []byte, selected *config.SetConfig, want bool) {
		t.Helper()
		if got := c.rewrite(p, 20, selected, cfg, now); got != want {
			t.Fatalf("rewrite=%v want %v flags=%x", got, want, p[33])
		}
	}
	run(makeV4TCPPacket([]byte{1}, 101), set, false)
	run(syn(100), set, true)
	run(syn(100), set, true)
	run(makeV4TCPPacket(nil, 101), nil, true)
	run(makeV4TCPPacket([]byte{1}, 101), nil, true)
	run(makeV4TCPPacket([]byte{1}, 101), set, false)
	run(makeV4TCPPacket(nil, 102), set, false)
	run(syn(200), set, true)
	fin := makeV4TCPPacketFlags(nil, 201, 0x11)
	run(fin, nil, true)
	if len(c.flows) != 0 {
		t.Fatal("FIN state retained")
	}
	run(syn(300), set, true)
	rst := makeV4TCPPacketFlags(nil, 301, 0x14)
	run(rst, nil, true)
	if len(c.flows) != 0 {
		t.Fatal("RST state retained")
	}
	run(syn(400), set, true)
	reply := makeV4TCPPacketFlags(nil, 10, 0x14)
	copy(reply[12:16], syn(400)[16:20])
	copy(reply[16:20], syn(400)[12:16])
	copy(reply[20:22], syn(400)[22:24])
	copy(reply[22:24], syn(400)[20:22])
	c.closeIncoming(reply, 20)
	if len(c.flows) != 0 {
		t.Fatal("incoming RST state retained")
	}
	run(syn(500), set, true)
	c.cleanup(now.Add(wsSizeTTL))
	if len(c.flows) != 0 {
		t.Fatal("expired state retained")
	}
	set.TCP.WSSize.Enabled = false
	run(syn(600), set, false)
	set.TCP.WSSize.Enabled = true
	run(syn(700), set, true)
	other := *cfg
	cfg = &other
	run(makeV4TCPPacket(nil, 701), set, false)
}

func TestWSSizeCapacityAndConcurrentCleanup(t *testing.T) {
	cfg, set := wsFixture()
	c := newWSSizeCache()
	now := time.Now()
	for i := 0; i < wsSizeCapacity; i++ {
		p := synWithOptions(IPv4, nil, nil, 100)
		binary.BigEndian.PutUint16(p[20:22], uint16(i))
		if !c.rewrite(p, 20, set, cfg, now) {
			t.Fatal("capacity reached early")
		}
	}
	p := synWithOptions(IPv4, nil, nil, 100)
	binary.BigEndian.PutUint16(p[20:22], 65535)
	if c.rewrite(p, 20, set, cfg, now) {
		t.Fatal("capacity exceeded")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				c.rewrite(synWithOptions(IPv4, nil, nil, 200), 20, set, cfg, now)
				c.cleanup(now)
			}
		}()
	}
	wg.Wait()
	c.cleanup(now.Add(wsSizeTTL))
	if len(c.flows) != 0 {
		t.Fatal("cache leaked")
	}
}

func TestWSSizeDispatchAndMarks(t *testing.T) {
	cfg, set := wsFixture()
	w := newTestWorker(t, cfg)
	p := synWithOptions(IPv4, []byte{3, 3, 7, 0}, nil, 100)
	pkt, _ := w.parseIPHeaders(p)
	set.Targets.IpsToMatch = []string{pkt.dstStr}
	w.matcher.Store(buildMatcher(cfg))
	q := newFakeModQueue()
	w.dispatch(&verdictCtx{q: q}, p)
	v := q.now(t)
	if len(v.packet) == 0 || binary.BigEndian.Uint16(v.packet[34:36]) != 1 || !sock.TCPChecksumValid(v.packet) {
		t.Fatal("real SYN replacement missing")
	}
	if len(q.verdicts) != 0 {
		t.Fatal("multiple verdicts")
	}
	p = synWithOptions(IPv4, nil, nil, 200)
	copy(p[16:20], []byte{8, 8, 8, 8})
	before := append([]byte(nil), p...)
	w.dispatch(&verdictCtx{q: q}, p)
	if len(q.now(t).packet) != 0 || !bytes.Equal(before, p) {
		t.Fatal("unselected traffic changed")
	}
	p = synWithOptions(IPv4, nil, nil, 300)
	id := uint32(1)
	mark := uint32(cfg.Queue.Mark)
	w.handleQueued(&verdictCtx{q: q}, nfqueue.Attribute{PacketID: &id, Payload: &p, Mark: &mark}, uint(mark))
	if len(q.now(t).packet) != 0 {
		t.Fatal("injected packet rewritten")
	}
}

func TestWSSizeFastOpenAndTruncation(t *testing.T) {
	cfg, set := wsFixture()
	c := newWSSizeCache()
	now := time.Now()
	p := synWithOptions(IPv4, []byte{3, 3, 7, 0}, []byte{1}, 100)
	if !c.rewrite(p, 20, set, cfg, now) {
		t.Fatal("Fast Open first data missing")
	}
	if c.rewrite(synWithOptions(IPv4, []byte{3, 3, 7, 0}, []byte{1}, 100), 20, set, cfg, now) {
		t.Fatal("Fast Open retransmission passed cutoff")
	}
	for _, ver := range []byte{IPv4, IPv6} {
		p := synWithOptions(ver, nil, nil, 200)
		at := 20
		if ver == IPv6 {
			at = 40
		}
		p = p[:len(p)-1]
		before := append([]byte(nil), p...)
		if c.rewrite(p, at, set, cfg, now) || !bytes.Equal(p, before) {
			t.Fatal("truncated packet modified")
		}
	}
}

func TestWSSizeHeldPacketKeepsReplacement(t *testing.T) {
	q := newFakeModQueue()
	raw := synWithOptions(IPv4, nil, nil, 100)
	h := &helloHold{q: q, raw: raw, modified: true}
	h.verdict(nfqueue.NfAccept)
	if !bytes.Equal(q.now(t).packet, raw) {
		t.Fatal("held replacement lost")
	}
	h.verdict(nfqueue.NfDrop)
	if len(q.now(t).packet) != 0 {
		t.Fatal("a held drop was changed to accept")
	}
}
