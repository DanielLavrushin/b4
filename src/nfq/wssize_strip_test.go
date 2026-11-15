package nfq

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func wsStripFixture(window, scale int) (*config.Config, *config.SetConfig) {
	cfg, set := wsFixture()
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: window, Scale: scale, StripScale: true}
	return cfg, set
}

func TestWSSizeStripScaleRemovesTheOptionFromTheSYN(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		at := 20
		if ver == IPv6 {
			at = 40
		}
		for _, window := range []int{0, 1, 2} {
			for _, scale := range []int{0, 5} {
				cfg, set := wsStripFixture(window, scale)
				c := newWSSizeCache()
				p := wsPacket(ver, 12345, 100, 0x02, wsKernelSynOptions, nil)
				original := append([]byte(nil), p...)
				if !c.rewrite(p, at, set, cfg, time.Now()) {
					t.Fatalf("v%d window %d scale %d: a SYN with a Window Scale option left unchanged: %s", ver, window, scale, wsShow(p))
				}
				if got := wsScale(p); got != -1 {
					t.Fatalf("v%d window %d scale %d: the SYN still carries Window Scale %d", ver, window, scale, got)
				}
				wantWindow := wsKernelWindow
				if window > 0 {
					wantWindow = window
				}
				if got := wsWindow(p); got != wantWindow {
					t.Fatalf("v%d window %d scale %d: the SYN left with window %d, want %d", ver, window, scale, got, wantWindow)
				}
				optsAt := at + 20
				wsAt := optsAt + len(wsKernelSynOptions) - 3
				if !bytes.Equal(p[wsAt:wsAt+3], []byte{1, 1, 1}) {
					t.Fatalf("v%d: the option's three bytes are % x, want three NOPs", ver, p[wsAt:wsAt+3])
				}
				if !bytes.Equal(p[optsAt:wsAt], original[optsAt:wsAt]) {
					t.Fatalf("v%d: the options ahead of Window Scale changed", ver)
				}
				if len(p) != len(original) || !bytes.Equal(p[at:at+14], original[at:at+14]) {
					t.Fatalf("v%d: the packet length or unrelated TCP fields changed", ver)
				}
				if !wsChecksumsValid(p) {
					t.Fatalf("v%d: checksums are wrong after the rewrite: %s", ver, wsShow(p))
				}
				again := append([]byte(nil), original...)
				if !c.rewrite(again, at, set, cfg, time.Now().Add(time.Second)) || !bytes.Equal(again, p) {
					t.Fatalf("v%d window %d scale %d: a retransmitted SYN left as %s, the first one as %s", ver, window, scale, wsShow(again), wsShow(p))
				}
				if n := len(c.flows); n != 1 {
					t.Fatalf("v%d window %d scale %d: %d records, want 1", ver, window, scale, n)
				}
			}
		}
	}
}

func TestWSSizeStripScaleLeavesASYNWithoutTheOptionAlone(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		at := 20
		if ver == IPv6 {
			at = 40
		}
		cfg, set := wsStripFixture(0, 0)
		p := wsPacket(ver, 12345, 100, 0x02, []byte{2, 4, 5, 0xb4}, nil)
		original := append([]byte(nil), p...)
		if newWSSizeCache().rewrite(p, at, set, cfg, time.Now()) || !bytes.Equal(p, original) {
			t.Fatalf("v%d: a SYN without a Window Scale option changed: %s", ver, wsShow(p))
		}
		cfg, set = wsStripFixture(1, 0)
		p = append([]byte(nil), original...)
		if !newWSSizeCache().rewrite(p, at, set, cfg, time.Now()) || wsWindow(p) != 1 || len(p) != len(original) {
			t.Fatalf("v%d: a SYN without a Window Scale option did not get the window alone: %s", ver, wsShow(p))
		}
		if !bytes.Equal(p[at+20:], original[at+20:]) {
			t.Fatalf("v%d: the options of a SYN without Window Scale changed", ver)
		}
	}
}

func TestWSSizeStripScaleRunsTheWholeOpening(t *testing.T) {
	for _, cutoff := range wsCutoffs {
		t.Run(wsCutoffName(cutoff), func(t *testing.T) {
			p := newWSConn(t, cutoff)
			p.set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, StripScale: true, Cutoff: cutoff}
			hello := buildClientHello("example.com", 600, 0xab)
			if !p.syn(nil) || !p.at(time.Second).syn(nil) || !p.ack() {
				t.Fatal("the SYN, its copy and the handshake ACK must be rewritten")
			}
			if !p.data(0, hello[:200]) {
				t.Fatal("the first ClientHello segment must carry the window")
			}
			if last, want := p.data(200, hello[200:]), cutoff == wsHello; last != want {
				t.Fatalf("the last ClientHello segment rewritten %v, want %v", last, want)
			}
			if p.send(0x10, len(hello), nil, nil) {
				t.Fatal("a packet after the ClientHello was rewritten")
			}
		})
	}
}

func TestWSSizeStripScaleRemovesEveryCopyOfTheOption(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		at := 20
		if ver == IPv6 {
			at = 40
		}
		cfg, set := wsStripFixture(0, 0)
		p := wsPacket(ver, 12345, 100, 0x02, []byte{3, 3, 7, 1, 3, 3, 5, 1}, nil)
		if !newWSSizeCache().rewrite(p, at, set, cfg, time.Now()) {
			t.Fatalf("v%d: a SYN with two Window Scale options left unchanged", ver)
		}
		if got := p[at+20 : at+28]; !bytes.Equal(got, bytes.Repeat([]byte{1}, 8)) {
			t.Fatalf("v%d: the options left as % x, want both copies of the option removed", ver, got)
		}
		if !wsChecksumsValid(p) {
			t.Fatalf("v%d: checksums are wrong after the rewrite: %s", ver, wsShow(p))
		}
	}
}

func TestWSSizeStripScaleTouchesTheOptionOnlyInASYN(t *testing.T) {
	lookalike := []byte{1, 3, 3, 7}
	for _, window := range []int{0, 1} {
		cfg, set := wsStripFixture(window, 0)
		c := newWSSizeCache()
		now := time.Now()
		if !c.rewrite(wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now) {
			t.Fatalf("window %d: the SYN left unchanged", window)
		}
		ack := wsPacket(IPv4, 12345, 101, 0x10, lookalike, nil)
		original := append([]byte(nil), ack...)
		rewritten := c.rewrite(ack, 20, nil, cfg, now)
		if rewritten != (window > 0) {
			t.Fatalf("window %d: the handshake ACK rewritten %v", window, rewritten)
		}
		if !bytes.Equal(ack[40:44], lookalike) {
			t.Fatalf("window %d: the options of a packet that is not a SYN changed to % x", window, ack[40:44])
		}
		if window == 0 && !bytes.Equal(ack, original) {
			t.Fatalf("window 0: a packet that is not a SYN changed: %s", wsShow(ack))
		}
	}
}

func TestWSSizeStripScaleIsTraced(t *testing.T) {
	var buf syncBuffer
	wsCaptureLog(t, log.LevelTrace, &buf)
	cfg, set := wsStripFixture(1, 0)
	p := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
	newWSSizeCache().rewrite(p, 20, set, cfg, time.Now())
	log.Flush()
	if out := buf.String(); !strings.Contains(out, "scale 7 removed") || !strings.Contains(out, "window 64240 -> 1") {
		t.Fatalf("the trace line does not say what changed: %q", out)
	}
}
