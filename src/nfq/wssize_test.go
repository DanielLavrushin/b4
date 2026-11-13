package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

const (
	wsFirstData = config.WSSizeCutoffFirstData
	wsHello     = ""
)

var wsCutoffs = []string{wsFirstData, wsHello}

func wsCutoffName(cutoff string) string {
	if cutoff == wsFirstData {
		return "first data cutoff"
	}
	return "ClientHello cutoff"
}

func wsFixture() (*config.Config, *config.SetConfig) {
	cfg := config.NewConfig()
	set := newPassiveSet()
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	set.Targets.IpsToMatch = []string{"93.184.216.34", "2001:db8::2"}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1, Cutoff: wsFirstData}
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
				p := wsPacket(ver, 12345, 100, 0x02, []byte{3, 3, 7, 0}, nil)
				original := append([]byte(nil), p...)
				wantWindow, wantScale := wsKernelWindow, 7
				if window > 0 {
					wantWindow = window
				}
				if scale > 0 {
					wantScale = min(scale, 7)
				}
				changes := wantWindow != wsKernelWindow || wantScale != 7
				if got := c.rewrite(p, at, set, cfg, time.Now()); got != changes {
					t.Fatalf("v%d window %d scale %d: rewrite reported %v for a SYN with window %d and scale 7, want %v", ver, window, scale, got, wsKernelWindow, changes)
				}
				if got := wsWindow(p); got != wantWindow {
					t.Fatalf("v%d window %d scale %d: the SYN left with window %d, want %d", ver, window, scale, got, wantWindow)
				}
				if got := int(p[at+22]); got != wantScale {
					t.Fatalf("v%d window %d scale %d: the SYN left with scale %d, want %d", ver, window, scale, got, wantScale)
				}
				if len(p) != len(original) || !bytes.Equal(p[at:at+14], original[at:at+14]) {
					t.Fatal("unrelated TCP fields changed")
				}
				if !changes && !bytes.Equal(p, original) {
					t.Fatalf("v%d window %d scale %d: a SYN that needed no change left changed: %s", ver, window, scale, wsShow(p))
				}
				if ver == IPv4 && (!sock.TCPChecksumValid(p) || !ipv4HeaderChecksumValid(p[:at])) {
					t.Fatal("IPv4 checksum")
				}
				if ver == IPv6 && !sock.TCPChecksumValidV6(p) {
					t.Fatal("IPv6 checksum")
				}
				wantRecords := 1
				if window == 0 && scale == 0 {
					wantRecords = 0
				}
				if n := len(c.flows); n != wantRecords {
					t.Fatalf("v%d window %d scale %d: %d record(s) opened, want %d", ver, window, scale, n, wantRecords)
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

func TestWSSizeZeroLeavesTheFieldAsItIs(t *testing.T) {
	packets := []struct {
		name    string
		port    uint16
		seq     uint32
		flags   byte
		opts    []byte
		payload []byte
		records int
	}{
		{"the SYN with Window Scale 7", 12345, 100, 0x02, wsKernelSynOptions, nil, 1},
		{"the handshake ACK", 12345, 101, 0x10, nil, nil, 1},
		{"the first data packet", 12345, 101, 0x18, nil, []byte("first data"), 0},
		{"the SYN without a Window Scale option", 12346, 100, 0x02, []byte{2, 4, 5, 0xb4}, nil, 1},
	}
	numbers := []config.WSSizeConfig{
		{Window: 1, Scale: 1},
		{Window: 1, Scale: 0},
		{Window: 0, Scale: 6},
		{Window: 512, Scale: 0},
		{Window: 0, Scale: 0},
		{Window: wsKernelWindow, Scale: 7},
		{Window: 0, Scale: 14},
	}
	for _, ver := range []byte{IPv4, IPv6} {
		for _, ws := range numbers {
			t.Run(fmt.Sprintf("v%d window %d scale %d", ver, ws.Window, ws.Scale), func(t *testing.T) {
				cfg, set := wsFixture()
				set.TCP.WSSize.Window, set.TCP.WSSize.Scale = ws.Window, ws.Scale
				c, now := newWSSizeCache(), time.Now()
				for _, p := range packets {
					raw := wsPacket(ver, p.port, p.seq, p.flags, p.opts, p.payload)
					at := wsTCPAt(raw)
					original := append([]byte(nil), raw...)
					want := append([]byte(nil), raw...)
					if ws.Window > 0 {
						binary.BigEndian.PutUint16(want[at+14:], uint16(ws.Window))
					}
					if ws.Scale > 0 && wsScale(raw) > ws.Scale {
						want[at+20+len(p.opts)-1] = byte(ws.Scale)
					}
					changes := !bytes.Equal(want, original)
					if changes && ver == IPv4 {
						sock.FixTCPChecksum(want)
					}
					if changes && ver == IPv6 {
						sock.FixTCPChecksumV6(want)
					}
					if !changes {
						raw[at+16] ^= 0x55
						want[at+16] ^= 0x55
					}
					var selected *config.SetConfig
					if p.flags&0x02 != 0 {
						selected = set
					}
					if got := c.rewrite(raw, at, selected, cfg, now); got != changes {
						t.Fatalf("%s: rewrite reported %v, want %v, the packet left as %s", p.name, got, changes, wsShow(raw))
					}
					if ws.Window == 0 && wsWindow(raw) != wsKernelWindow {
						t.Fatalf("%s: window 0 leaves the window field alone, but it left as %d instead of %d", p.name, wsWindow(raw), wsKernelWindow)
					}
					if ws.Scale == 0 && wsScale(raw) != wsScale(original) {
						t.Fatalf("%s: scale 0 leaves the Window Scale option alone, but it left as %d instead of %d", p.name, wsScale(raw), wsScale(original))
					}
					if !changes && !bytes.Equal(raw, want) {
						t.Fatalf("%s needed no change, but its bytes or its checksum were touched: %s", p.name, wsShow(raw))
					}
					if changes && (!bytes.Equal(raw, want) || !wsChecksumsValid(raw)) {
						t.Fatalf("%s left as %s, want %s with nothing else changed", p.name, wsShow(raw), wsShow(want))
					}
					records := p.records
					if !set.TCP.WSSize.Rewrites() {
						records = 0
					}
					if n := len(c.flows); n != records {
						t.Fatalf("after %s the table holds %d record(s), want %d", p.name, n, records)
					}
				}
			})
		}
	}
}

func TestWSSizeNumbersOutsideTheRangeOpenNoRecord(t *testing.T) {
	outside := []config.WSSizeConfig{
		{Window: -1, Scale: 1},
		{Window: config.MaxWSSizeWindow + 1, Scale: 1},
		{Window: 1, Scale: -1},
		{Window: 1, Scale: config.MaxWSSizeScale + 1},
	}
	for _, ws := range outside {
		cfg, set := wsFixture()
		set.TCP.WSSize.Window, set.TCP.WSSize.Scale = ws.Window, ws.Scale
		c := newWSSizeCache()
		syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
		original := append([]byte(nil), syn...)
		if c.rewrite(syn, 20, set, cfg, time.Now()) || !bytes.Equal(syn, original) || len(c.flows) != 0 {
			t.Fatalf("window %d and scale %d never pass validation, but the SYN left as %s with %d record(s) opened", ws.Window, ws.Scale, wsShow(syn), len(c.flows))
		}
	}
}

func TestWSSizeCutoffMeansTheSameAfterValidation(t *testing.T) {
	hello := buildClientHello("validated.example", 1500, 0xab)
	const cut = 1200
	const writtenByEarlierBuilds = "client_hello"
	saved := map[string]bool{
		"":                           true,
		writtenByEarlierBuilds:       true,
		"anything":                   true,
		config.WSSizeCutoffFirstData: false,
	}
	for cutoff, followsTheHello := range saved {
		t.Run(fmt.Sprintf("saved as %q", cutoff), func(t *testing.T) {
			p := newWSConn(t, cutoff)
			if err := p.cfg.Validate(); err != nil {
				t.Fatalf("a config with one enabled set whose cutoff is saved as %q was refused: %v", cutoff, err)
			}
			if !p.syn(nil) || !p.ack() || !p.data(0, hello[:cut]) {
				t.Fatal("the opening through the first segment of the ClientHello must be rewritten")
			}
			if got := p.data(cut, hello[cut:]); got != followsTheHello {
				t.Fatalf("the second segment of the ClientHello was rewritten %v, want %v for a cutoff saved as %q and validated to %q", got, followsTheHello, cutoff, p.set.TCP.WSSize.Cutoff)
			}
		})
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
	run(rst, nil, false)
	if len(c.flows) != 1 {
		t.Fatal("an outgoing RST ended the record")
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
	again := synWithOptions(IPv4, []byte{3, 3, 7, 0}, []byte{1}, 100)
	if !c.rewrite(again, 20, set, cfg, now) || !bytes.Equal(again, p) {
		t.Fatal("Fast Open retransmission left with another window or scale than the first copy")
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
