package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

type wsVerdict struct {
	id      uint32
	verdict int
	packet  []byte
}

type wsQueue struct {
	mu  sync.Mutex
	got []wsVerdict
}

func (q *wsQueue) SetVerdict(id uint32, verdict int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.got = append(q.got, wsVerdict{id: id, verdict: verdict})
	return nil
}

func (q *wsQueue) SetVerdictModPacket(id uint32, verdict int, packet []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.got = append(q.got, wsVerdict{id: id, verdict: verdict, packet: append([]byte(nil), packet...)})
	return nil
}

func (q *wsQueue) take() []wsVerdict {
	q.mu.Lock()
	defer q.mu.Unlock()
	got := q.got
	q.got = nil
	return got
}

type wsNopQueue struct{}

func (wsNopQueue) SetVerdict(uint32, int) error                  { return nil }
func (wsNopQueue) SetVerdictModPacket(uint32, int, []byte) error { return nil }

func wsWorker(t *testing.T, tune func(*config.SetConfig)) (*Worker, *config.Config, *config.SetConfig) {
	t.Helper()
	cfg, set := wsFixture()
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.RSTProtection.Enabled = false
	if tune != nil {
		tune(set)
	}
	return newTestWorker(t, cfg), cfg, set
}

func wsDispatch(t *testing.T, w *Worker, q *wsQueue, id uint32, raw []byte) wsVerdict {
	t.Helper()
	w.dispatch(&verdictCtx{q: q, id: id}, raw)
	got := q.take()
	if len(got) != 1 {
		t.Fatalf("packet %d got %d verdicts, want one: %+v", id, len(got), got)
	}
	return got[0]
}

func wsReply(seq uint32, flags byte) []byte {
	pkt := wsPacket(IPv4, 443, seq, flags, nil, nil)
	copy(pkt[12:16], []byte{1, 2, 3, 4})
	copy(pkt[16:20], []byte{10, 0, 0, 1})
	binary.BigEndian.PutUint16(pkt[22:24], 12345)
	sock.FixIPv4Checksum(pkt[:20])
	sock.FixTCPChecksum(pkt)
	return pkt
}

func wsOnlyWindowAndChecksumDiffer(got, was []byte) bool {
	at := wsTCPAt(was)
	return len(got) == len(was) && bytes.Equal(got[:at+14], was[:at+14]) && bytes.Equal(got[at+18:], was[at+18:])
}

func TestWSSizeHeldSegmentLeavesBeforeTheReplacedPacket(t *testing.T) {
	q := &wsQueue{}
	held := &helloHold{id: 1, q: q, raw: []byte{9, 9}, key: "k", at: time.Now()}
	vc := &verdictCtx{id: 2, q: q, held: held, modified: []byte{1, 2, 3}}
	vc.accept()
	got := q.take()
	if len(got) != 2 || got[0].id != 1 || got[0].packet != nil || got[1].id != 2 || got[1].packet == nil {
		t.Fatalf("want the held segment accepted first and the current packet replaced second, got %+v", got)
	}
}

func TestWSSizeHoldCandidateKeepsTheReplacement(t *testing.T) {
	cfg, _ := wsFixture()
	names := newPassiveSet()
	names.Id = "names"
	names.Fragmentation.Strategy = "tcp"
	cfg.Sets = append(cfg.Sets, &names)
	w := newTestWorker(t, cfg)
	hello := buildClientHello("i.ytimg.com", 600, 0xab)
	raw := wsPacket(IPv4, 12345, 101, 0x18, nil, hello[:len(hello)*2/3])
	pkt, ok := w.parseIPHeaders(raw)
	if !ok {
		t.Fatal("packet not parsed")
	}
	tcp := raw[20:]
	q := &wsQueue{}
	h := w.helloHoldCandidate(&verdictCtx{q: q, id: 7, modified: raw}, pkt, w.getMatcher(), "k", tcp, tcp[20:], 443, false)
	if h == nil {
		t.Fatal("the first segment of a split ClientHello was not a hold candidate")
	}
	h.verdict(nfqueue.NfAccept)
	got := q.take()
	if len(got) != 1 || !bytes.Equal(got[0].packet, raw) {
		t.Fatalf("a held segment of a rewritten connection must be released with its replacement, got %+v", got)
	}
}

func TestWSSizeNeedsAQueueThatCanReplaceThePacket(t *testing.T) {
	for _, mode := range []string{"", "nfqueue", "tun"} {
		t.Run(fmt.Sprintf("no queue, saved engine %q", mode), func(t *testing.T) {
			w, cfg, _ := wsWorker(t, nil)
			cfg.Queue.Mode = mode
			syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
			before := append([]byte(nil), syn...)
			if v := w.ProcessPacketDir(syn, engine.DirOriginal); v != engine.VerdictAccept {
				t.Fatalf("verdict %v", v)
			}
			if !bytes.Equal(syn, before) || len(w.wssize.flows) != 0 {
				t.Fatalf("a packet that came in without a queue was rewritten in place: %s", wsShow(syn))
			}
		})
	}
	t.Run("queue that only takes verdicts", func(t *testing.T) {
		w, _, _ := wsWorker(t, nil)
		q := &verdictLog{}
		syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
		before := append([]byte(nil), syn...)
		w.dispatch(&verdictCtx{q: q, id: 1}, syn)
		wantVerdicts(t, q.list(), acceptOf(1))
		if !bytes.Equal(syn, before) || len(w.wssize.flows) != 0 {
			t.Fatalf("a packet whose queue cannot replace it was rewritten in place: %s", wsShow(syn))
		}
	})
	t.Run("queue that can replace the packet while the saved engine is TUN", func(t *testing.T) {
		w, cfg, _ := wsWorker(t, nil)
		cfg.Queue.Mode = "tun"
		q := &wsQueue{}
		syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
		before := append([]byte(nil), syn...)
		w.dispatch(&verdictCtx{q: q, id: 1}, syn)
		got := q.take()
		if len(got) != 1 || got[0].packet != nil || !bytes.Equal(syn, before) || len(w.wssize.flows) != 0 {
			t.Fatalf("with the TUN engine saved, a queue of a Discovery run rewrote a packet the live engine leaves alone: %+v, %s", got, wsShow(syn))
		}
	})
}

func TestWSSizeOpensNoRecordForASetValidationRefuses(t *testing.T) {
	refused := map[string]func(*config.SetConfig){
		"routing":      func(set *config.SetConfig) { set.Routing.Enabled = true },
		"duplication":  func(set *config.SetConfig) { set.TCP.Duplicate.Enabled = true },
		"disabled set": func(set *config.SetConfig) { set.Enabled = false },
	}
	for name, mutate := range refused {
		cfg, set := wsFixture()
		mutate(set)
		c := newWSSizeCache()
		syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
		original := append([]byte(nil), syn...)
		if c.rewrite(syn, 20, set, cfg, time.Now()) || !bytes.Equal(syn, original) || len(c.flows) != 0 {
			t.Errorf("%s: the SYN left as %s with %d record(s) opened", name, wsShow(syn), len(c.flows))
		}
	}
}

func TestWSSizeGatesLeaveThePacketAlone(t *testing.T) {
	cases := []struct {
		name string
		vc   func(q *wsQueue) *verdictCtx
		port uint16
	}{
		{"egress handed off to a tunnel", func(q *wsQueue) *verdictCtx { return &verdictCtx{q: q, id: 1, tunnel: "wg0"} }, 443},
		{"interface filtered", func(q *wsQueue) *verdictCtx { return &verdictCtx{q: q, id: 1, ifaceFiltered: true} }, 443},
		{"port outside the sets", func(q *wsQueue) *verdictCtx { return &verdictCtx{q: q, id: 1} }, 8443},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, cfg, _ := wsWorker(t, nil)
			if cfg.IsTCPPort(8443) {
				t.Skip("8443 is a set port in this configuration")
			}
			syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
			binary.BigEndian.PutUint16(syn[22:24], tc.port)
			sock.FixTCPChecksum(syn)
			before := append([]byte(nil), syn...)
			q := &wsQueue{}
			w.dispatch(tc.vc(q), syn)
			got := q.take()
			if len(got) != 1 || got[0].packet != nil || !bytes.Equal(syn, before) {
				t.Fatalf("the SYN must pass unmodified, got %+v", got)
			}
			if n := len(w.wssize.flows); n != 0 {
				t.Fatalf("%d record(s) opened", n)
			}
		})
	}
}

func TestWSSizeIdleConfigNeverTakesTheCacheLock(t *testing.T) {
	cases := map[string]func(cfg *config.Config, set *config.SetConfig){
		"no set rewrites the window": func(_ *config.Config, set *config.SetConfig) {
			set.TCP.WSSize.Enabled = false
		},
		"only a disabled set rewrites the window": func(cfg *config.Config, set *config.SetConfig) {
			off := *set
			off.Id, off.Enabled = "off", false
			set.TCP.WSSize.Enabled = false
			cfg.Sets = append(cfg.Sets, &off)
		},
		"the only set with the block on has window 0 and scale 0": func(_ *config.Config, set *config.SetConfig) {
			set.TCP.WSSize = config.WSSizeConfig{Enabled: true}
		},
	}
	for name, tune := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, set := wsFixture()
			set.Targets.IpsToMatch = []string{"1.2.3.4"}
			set.TCP.RSTProtection.Enabled = false
			tune(cfg, set)
			w := newTestWorker(t, cfg)
			q := &wsQueue{}
			done := make(chan struct{})
			w.wssize.mu.Lock()
			go func() {
				defer close(done)
				for i, raw := range [][]byte{
					wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil),
					wsPacket(IPv4, 12345, 101, 0x10, nil, nil),
					wsPacket(IPv4, 12345, 101, 0x18, nil, []byte("first data")),
				} {
					w.dispatch(&verdictCtx{q: q, id: uint32(i + 1)}, raw)
				}
			}()
			waited := false
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				waited = true
			}
			w.wssize.mu.Unlock()
			<-done
			if waited {
				t.Fatal("although no enabled set rewrites anything, an outgoing packet still waited for the cache lock")
			}
			for _, v := range q.take() {
				if v.packet != nil {
					t.Fatalf("packet %d was replaced", v.id)
				}
			}
			if n := len(w.wssize.flows); n != 0 {
				t.Fatalf("%d record(s) opened although no enabled set rewrites anything", n)
			}
		})
	}
}

func TestWSSizeEitherNumberSwitchesTheRewriteOn(t *testing.T) {
	for _, numbers := range []config.WSSizeConfig{{Window: 512, Scale: 0}, {Window: 0, Scale: 6}} {
		w, _, _ := wsWorker(t, func(s *config.SetConfig) {
			s.TCP.WSSize.Window, s.TCP.WSSize.Scale = numbers.Window, numbers.Scale
		})
		syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
		original := append([]byte(nil), syn...)
		v := wsDispatch(t, w, &wsQueue{}, 1, syn)
		if v.packet == nil {
			t.Fatalf("with window %d and scale %d the SYN left with a plain accept, so nothing was rewritten", numbers.Window, numbers.Scale)
		}
		if wsWindow(v.packet) != wsWindowWanted(numbers, original) || wsScale(v.packet) != wsScaleWanted(numbers, original) || !wsChecksumsValid(v.packet) {
			t.Fatalf("with window %d and scale %d the SYN left as %s", numbers.Window, numbers.Scale, wsShow(v.packet))
		}
	}
}

func TestWSSizeAPacketThatNeedsNoChangeLeavesWithAPlainAccept(t *testing.T) {
	plain := func(t *testing.T, w *Worker, q *wsQueue, id uint32, raw []byte, what string) {
		t.Helper()
		before := append([]byte(nil), raw...)
		if v := wsDispatch(t, w, q, id, raw); v.packet != nil || v.verdict != nfqueue.NfAccept || !bytes.Equal(raw, before) {
			t.Fatalf("%s needed no change and must leave with a plain accept, got verdict %d, replaced %v, packet %s", what, v.verdict, v.packet != nil, wsShow(raw))
		}
	}
	for _, cutoff := range wsCutoffs {
		t.Run(wsCutoffName(cutoff), func(t *testing.T) {
			w, _, _ := wsWorker(t, func(s *config.SetConfig) {
				s.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 0, Scale: 6, Cutoff: cutoff}
			})
			q := &wsQueue{}
			v := wsDispatch(t, w, q, 1, wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil))
			if v.packet == nil {
				t.Fatal("with window 0 and scale 6 the SYN left with a plain accept, so its Window Scale stayed 7")
			}
			if wsScale(v.packet) != 6 || wsWindow(v.packet) != wsKernelWindow || !wsChecksumsValid(v.packet) {
				t.Fatalf("with window 0 and scale 6 the SYN must leave with scale 6 and its own window %d, it left as %s", wsKernelWindow, wsShow(v.packet))
			}
			plain(t, w, q, 2, wsPacket(IPv4, 12345, 101, 0x10, nil, nil), "the handshake ACK")
			if n := len(w.wssize.flows); n != 1 {
				t.Fatalf("%d record(s) after the handshake ACK, want the one of the connection", n)
			}
			plain(t, w, q, 3, wsPacket(IPv4, 12345, 101, 0x18, nil, buildClientHello("example.com", 0, 0)), "the first data packet")
			plain(t, w, q, 4, wsPacket(IPv4, 12346, 100, 0x02, []byte{2, 4, 5, 0xb4}, nil), "a SYN without a Window Scale option")
		})
	}
}

func TestWSSizeGateFollowsAConfigReload(t *testing.T) {
	configWith := func(on bool) *config.Config {
		cfg, set := wsFixture()
		set.Targets.IpsToMatch = []string{"1.2.3.4"}
		set.TCP.RSTProtection.Enabled = false
		set.TCP.WSSize.Enabled = on
		return cfg
	}
	w := newTestWorker(t, configWith(false))
	q := &wsQueue{}
	for i, step := range []struct {
		reload *config.Config
		want   bool
	}{
		{nil, false},
		{configWith(true), true},
		{configWith(false), false},
		{configWith(true), true},
	} {
		if step.reload != nil {
			w.UpdateConfig(step.reload)
			w.matcher.Store(buildMatcher(step.reload))
		}
		v := wsDispatch(t, w, q, uint32(i+1), wsPacket(IPv4, uint16(2000+i), 100, 0x02, wsKernelSynOptions, nil))
		if replaced := v.packet != nil; replaced != step.want {
			t.Fatalf("step %d: the SYN was rewritten %v, want %v for the config installed then", i, replaced, step.want)
		}
	}
}

func TestWSSizeAnIncomingResetOrFinKeepsTheRecord(t *testing.T) {
	for _, protected := range []bool{false, true} {
		for _, flags := range []byte{0x04, 0x14, 0x11} {
			t.Run(fmt.Sprintf("RST protection %v, incoming flags %#02x", protected, flags), func(t *testing.T) {
				w, _, _ := wsWorker(t, func(s *config.SetConfig) { s.TCP.RSTProtection.Enabled = protected })
				q := &wsQueue{}
				if v := wsDispatch(t, w, q, 1, wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)); v.packet == nil {
					t.Fatal("SYN not rewritten")
				}
				w.dispatch(&verdictCtx{q: q, id: 2}, wsReply(9000, flags))
				q.take()
				if n := len(w.wssize.flows); n != 1 {
					t.Fatalf("an incoming packet whose sequence number nobody checked left %d record(s), want 1", n)
				}
				for i, raw := range [][]byte{
					wsPacket(IPv4, 12345, 101, 0x10, nil, nil),
					wsPacket(IPv4, 12345, 101, 0x18, nil, []byte("first data")),
				} {
					if v := wsDispatch(t, w, q, uint32(3+i), raw); v.packet == nil || wsWindow(v.packet) != 1 || !wsChecksumsValid(v.packet) {
						t.Fatalf("after the incoming packet the client's packet %d left with its own window: %+v", i+1, v)
					}
				}
			})
		}
	}
}

func TestWSSizeARefusedOutgoingResetLeavesTheConnectionRewritten(t *testing.T) {
	w, _, _ := wsWorker(t, func(s *config.SetConfig) { s.TCP.RSTProtection.Enabled = true })
	q := &wsQueue{}
	if v := wsDispatch(t, w, q, 1, wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)); v.packet == nil {
		t.Fatal("SYN not rewritten")
	}
	rst := wsPacket(IPv4, 12345, 101, 0x04, nil, nil)
	before := append([]byte(nil), rst...)
	if v := wsDispatch(t, w, q, 2, rst); v.verdict != nfqueue.NfDrop {
		t.Fatalf("fixture: RST protection drops a RST sent before the connection is established, got %+v", v)
	}
	if !bytes.Equal(rst, before) {
		t.Fatalf("the refused RST was rewritten: %s", wsShow(rst))
	}
	for i, raw := range [][]byte{
		wsPacket(IPv4, 12345, 101, 0x10, nil, nil),
		wsPacket(IPv4, 12345, 101, 0x18, nil, []byte("first data")),
	} {
		if v := wsDispatch(t, w, q, uint32(3+i), raw); v.packet == nil || wsWindow(v.packet) != 1 || !wsChecksumsValid(v.packet) {
			t.Fatalf("the connection went on after the refused RST, but its packet %d left with its own window: %+v", i+1, v)
		}
	}
}

func TestWSSizeInjectedPacketsCarryTheWindow(t *testing.T) {
	hello := buildClientHello("example.com", 0, 0)
	for _, name := range []string{"synfake", "dropsack", "tcp fragments"} {
		t.Run(name, func(t *testing.T) {
			w, _, _ := wsWorker(t, func(s *config.SetConfig) {
				s.TCP.WSSize.Cutoff = wsHello
				switch name {
				case "synfake":
					s.TCP.SynFake = true
				case "dropsack":
					s.TCP.DropSACK = true
				default:
					s.Fragmentation = config.DefaultSetConfig.Fragmentation
					s.Fragmentation.Strategy = "tcp"
					s.Fragmentation.StrategyPool = nil
				}
			})
			sent := captureInjected(t, w)
			q := &wsQueue{}
			real := 0
			for i, raw := range [][]byte{
				wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil),
				wsPacket(IPv4, 12345, 101, 0x10, nil, nil),
				wsPacket(IPv4, 12345, 101, 0x18, nil, hello),
			} {
				w.dispatch(&verdictCtx{q: q, id: uint32(i + 1)}, raw)
				out := sent()
				for _, v := range q.take() {
					if v.packet != nil {
						out = append(out, v.packet)
					} else if v.verdict == nfqueue.NfAccept {
						t.Fatalf("packet %d left with a plain accept, so the kernel sent the original window", i+1)
					}
				}
				for _, p := range out {
					if p[8] != 64 {
						continue
					}
					real++
					if wsWindow(p) != 1 || !wsChecksumsValid(p) {
						t.Fatalf("packet %d went out as %s", i+1, wsShow(p))
					}
					if p[33]&0x02 != 0 && wsScale(p) != 1 {
						t.Fatalf("SYN went out as %s", wsShow(p))
					}
				}
			}
			if real < 3 {
				t.Fatalf("only %d real packets seen", real)
			}
		})
	}
}

func TestWSSizeAStrippedFastOpenSynKeepsTheWindow(t *testing.T) {
	hello := buildClientHello("example.com", 0, 0)
	tfo := func() []byte { return wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, hello) }
	bare := func() []byte { return wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil) }
	ack := func() []byte { return wsPacket(IPv4, 12345, 101, 0x10, nil, nil) }
	resent := func() []byte { return wsPacket(IPv4, 12345, 101, 0x18, nil, hello) }
	flows := map[string]func() [][]byte{
		"data resent right after the SYN":       func() [][]byte { return [][]byte{tfo(), resent()} },
		"SYN copies before the data resends":    func() [][]byte { return [][]byte{tfo(), tfo(), bare(), resent()} },
		"handshake ACK before the data resends": func() [][]byte { return [][]byte{tfo(), ack(), resent()} },
	}
	for _, cutoff := range wsCutoffs {
		for name, packets := range flows {
			t.Run(fmt.Sprintf("%s, %s", wsCutoffName(cutoff), name), func(t *testing.T) {
				w, _, _ := wsWorker(t, func(s *config.SetConfig) {
					s.TCP.WSSize.Cutoff = cutoff
					s.Fragmentation = config.DefaultSetConfig.Fragmentation
					s.Fragmentation.Strategy = "tcp"
					s.Fragmentation.StrategyPool = nil
				})
				wsDispatchAllWithTheWindow(t, w, packets(), 0)
			})
		}
	}
}

func TestWSSizeAFastOpenSynThatKeepsItsPayloadKeepsTheWindow(t *testing.T) {
	hello := buildClientHello("example.com", 0, 0)
	tfo := func() []byte { return wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, hello) }
	ack := func() []byte { return wsPacket(IPv4, 12345, 101, 0x10, nil, nil) }
	resent := func() []byte { return wsPacket(IPv4, 12345, 101, 0x18, nil, hello) }
	for _, cutoff := range wsCutoffs {
		t.Run(wsCutoffName(cutoff), func(t *testing.T) {
			w, _, _ := wsWorker(t, func(s *config.SetConfig) { s.TCP.WSSize.Cutoff = cutoff })
			wsDispatchAllWithTheWindow(t, w, [][]byte{tfo(), ack(), resent()}, len(hello))
			past := wsPacket(IPv4, 12345, 101+uint32(len(hello)), 0x10, nil, nil)
			before := append([]byte(nil), past...)
			if v := wsDispatch(t, w, &wsQueue{}, 9, past); v.packet != nil || !bytes.Equal(past, before) {
				t.Fatalf("an ACK that sits past the end of the ClientHello was rewritten: %s", wsShow(past))
			}
		})
	}
}

func wsDispatchAllWithTheWindow(t *testing.T, w *Worker, packets [][]byte, synPayload int) {
	t.Helper()
	sent := captureInjected(t, w)
	q := &wsQueue{}
	for i, raw := range packets {
		w.dispatch(&verdictCtx{q: q, id: uint32(i + 1)}, raw)
		out := sent()
		for _, v := range q.take() {
			if v.packet != nil {
				out = append(out, v.packet)
			} else if v.verdict == nfqueue.NfAccept {
				t.Fatalf("packet %d left with a plain accept, so the kernel sent the original window", i+1)
			}
		}
		if len(out) == 0 {
			t.Fatalf("packet %d: nothing went out", i+1)
		}
		for _, p := range out {
			if wsWindow(p) != 1 || !wsChecksumsValid(p) {
				t.Fatalf("packet %d went out as %s", i+1, wsShow(p))
			}
			if p[33]&0x02 != 0 && (wsScale(p) != 1 || len(p) != 20+20+len(wsKernelSynOptions)+synPayload) {
				t.Fatalf("SYN copy %d went out as %s", i+1, wsShow(p))
			}
		}
	}
}

func TestWSSizeFindsWindowScaleBehindTheKernelsOtherOptions(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			cfg, set := wsFixture()
			c, now := newWSSizeCache(), time.Now()
			syn := wsPacket(ver, 12345, 100, 0x02, wsKernelSynOptions, nil)
			at := wsTCPAt(syn)
			before := append([]byte(nil), syn...)
			if !c.rewrite(syn, at, set, cfg, now) {
				t.Fatal("SYN not rewritten")
			}
			scaleAt := at + 20 + len(wsKernelSynOptions) - 1
			if syn[scaleAt] != 1 {
				t.Fatalf("Window Scale behind MSS, SACK-permitted, timestamps and a NOP stayed %d", syn[scaleAt])
			}
			syn[scaleAt] = before[scaleAt]
			if !wsOnlyWindowAndChecksumDiffer(syn, before) {
				t.Fatal("the SYN changed outside its window, scale and checksum")
			}
			syn[scaleAt] = 1
			if wsWindow(syn) != 1 || !wsChecksumsValid(syn) {
				t.Fatalf("SYN left as %s", wsShow(syn))
			}
			for _, payload := range [][]byte{nil, []byte("first data")} {
				flags := byte(0x18)
				if payload == nil {
					flags = 0x10
				}
				p := wsPacket(ver, 12345, 101, flags, nil, payload)
				was := append([]byte(nil), p...)
				if !c.rewrite(p, at, nil, cfg, now) || wsWindow(p) != 1 || !wsChecksumsValid(p) || !wsOnlyWindowAndChecksumDiffer(p, was) {
					t.Fatalf("a packet with %d payload bytes left as %s", len(payload), wsShow(p))
				}
			}
			later := wsPacket(ver, 12345, 111, 0x18, nil, []byte("later data"))
			kept := append([]byte(nil), later...)
			if c.rewrite(later, at, nil, cfg, now) || !bytes.Equal(later, kept) {
				t.Fatal("data after the cutoff changed")
			}
		})
	}
}

func TestWSSizeLeavesBytesThatOnlyResembleAWindowScaleOption(t *testing.T) {
	layouts := []struct {
		name    string
		opts    []byte
		payload []byte
	}{
		{"kind 3 with length 4", []byte{3, 4, 7, 0}, nil},
		{"kind 3 cut off by the end of the TCP header, no payload", []byte{1, 1, 3, 3}, nil},
		{"kind 3 cut off by the end of the TCP header, payload behind it", []byte{1, 1, 3, 3}, []byte("first data")},
		{"kind 3 behind the End of Option List", []byte{2, 4, 5, 0xb4, 0, 3, 3, 7}, nil},
	}
	numbers := []config.WSSizeConfig{{Window: 1, Scale: 1}, {Window: 0, Scale: 1}}
	for _, ver := range []byte{IPv4, IPv6} {
		for _, ws := range numbers {
			for _, layout := range layouts {
				t.Run(fmt.Sprintf("v%d window %d scale %d, %s", ver, ws.Window, ws.Scale, layout.name), func(t *testing.T) {
					cfg, set := wsFixture()
					set.TCP.WSSize.Window, set.TCP.WSSize.Scale = ws.Window, ws.Scale
					c := newWSSizeCache()
					syn := wsPacket(ver, 12345, 100, 0x02, layout.opts, layout.payload)
					at := wsTCPAt(syn)
					want := append([]byte(nil), syn...)
					if ws.Window > 0 {
						binary.BigEndian.PutUint16(want[at+14:], uint16(ws.Window))
						if ver == IPv4 {
							sock.FixTCPChecksum(want)
						} else {
							sock.FixTCPChecksumV6(want)
						}
					}
					rewritten := func() bool {
						defer func() {
							if r := recover(); r != nil {
								t.Fatalf("the rewrite panicked on a SYN with the options % x and %d payload bytes: %v", layout.opts, len(layout.payload), r)
							}
						}()
						return c.rewrite(syn, at, set, cfg, time.Now())
					}()
					if opts := syn[at+20 : at+20+len(layout.opts)]; !bytes.Equal(opts, layout.opts) {
						t.Fatalf("the options % x hold no Window Scale option, but they left as % x", layout.opts, opts)
					}
					if payload := syn[at+20+len(layout.opts):]; !bytes.Equal(payload, layout.payload) {
						t.Fatalf("the payload %q behind the options % x left as %q", layout.payload, layout.opts, payload)
					}
					if rewritten != (ws.Window > 0) || !bytes.Equal(syn, want) {
						t.Fatalf("with window %d and no Window Scale option to lower, only the window field and the checksum may change: rewrite reported %v, the SYN left as % x, want % x", ws.Window, rewritten, syn[at:], want[at:])
					}
				})
			}
		}
	}
}

func BenchmarkWSSizeIdleDispatch(b *testing.B) {
	cfg := config.NewConfig()
	set := newPassiveSet()
	cfg.Sets = []*config.SetConfig{&set}
	cfg.ConfigPath = b.TempDir() + "/b4.json"
	state := newRuntimeState()
	matcher := buildMatcher(&cfg)
	workers := make([]*Worker, 32)
	for i := range workers {
		w := NewWorkerWithQueue(&cfg, uint16(i))
		w.matcher.Store(matcher)
		w.ipToMac.Store(make(map[string]string))
		w.tlsCache = state.tlsCache
		w.wssize = state.wssize
		w.streams = state.streams
		w.connTracker = state.connState
		w.destState = state.destState
		w.pendingHello = state.pendingHello
		w.hostHints = state.hostHints
		w.quicFlows = state.quicFlows
		w.voiceFlows = state.voiceFlows
		workers[i] = w
	}
	var next atomic.Int32
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		n := int(next.Add(1) - 1)
		w := workers[n%len(workers)]
		ack := wsPacket(IPv4, uint16(20000+n), 101, 0x10, nil, nil)
		for pb.Next() {
			w.dispatch(&verdictCtx{id: 1, q: wsNopQueue{}}, ack)
		}
	})
}
