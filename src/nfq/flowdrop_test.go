package nfq

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
	"golang.org/x/sys/unix"
)

type flowDropJob struct {
	tuple ctnl.Tuple
	dir   ctnl.Direction
}

type flowDropRecorder struct {
	mu         sync.Mutex
	events     []string
	jobs       []flowDropJob
	bestEffort []flowDropJob
	writes     []flowDropKey
	refuse     bool
	setErr     error
}

func (r *flowDropRecorder) SetVerdict(id uint32, verdict int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := "accept"
	if verdict == nfqueue.NfDrop {
		name = "drop"
	}
	r.events = append(r.events, "verdict "+name)
	return nil
}

func (r *flowDropRecorder) Enqueue(t ctnl.Tuple, dir ctnl.Direction) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuse {
		return false
	}
	r.events = append(r.events, "conntrack "+dir.String())
	r.jobs = append(r.jobs, flowDropJob{tuple: t, dir: dir})
	return true
}

func (r *flowDropRecorder) EnqueueBestEffort(t ctnl.Tuple, dir ctnl.Direction) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuse {
		return false
	}
	r.events = append(r.events, "conntrack "+dir.String()+" behind the set")
	r.bestEffort = append(r.bestEffort, flowDropJob{tuple: t, dir: dir})
	return true
}

func (r *flowDropRecorder) behindSet() []flowDropJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]flowDropJob(nil), r.bestEffort...)
}

func (r *flowDropRecorder) write(src, dst netip.AddrPort) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "nft")
	r.writes = append(r.writes, flowDropKey{src: src, dst: dst})
	return r.setErr
}

func (r *flowDropRecorder) snapshot() (events []string, jobs []flowDropJob, writes []flowDropKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...), append([]flowDropJob(nil), r.jobs...), append([]flowDropKey(nil), r.writes...)
}

func udpTuple(t *testing.T, src, dst string) ctnl.Tuple {
	t.Helper()
	tuple, err := ctnl.NewTuple(unix.IPPROTO_UDP, netip.MustParseAddrPort(src), netip.MustParseAddrPort(dst))
	if err != nil {
		t.Fatal(err)
	}
	return tuple
}

func flowDropWorker(t *testing.T, sets ...*config.SetConfig) (*Worker, *flowDropRecorder) {
	t.Helper()
	w := newQUICFlowWorker(t, sets...)
	rec := &flowDropRecorder{}
	w.flowDrops = newFlowDropPersistence()
	w.flowDrops.setDeleter(rec)
	return w, rec
}

func queueFrom(w *Worker, rec *flowDropRecorder, raw []byte) {
	id := uint32(7)
	w.handleQueuedThenPersist(&verdictCtx{id: id, q: rec}, nfqueue.Attribute{PacketID: &id, Payload: &raw}, uint(w.getConfig().Queue.Mark))
}

func blockingQUICSet(action string) config.SetConfig {
	set := newDomainOnlyQUICSet(config.ConfigOff)
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = action
	return set
}

func TestADecisionDropOfQUICHoldsTheFlow(t *testing.T) {
	cases := map[string]config.SetConfig{
		"udp.mode drop":   newDomainOnlyQUICSet(config.UDPModeDrop),
		"udp.mode reject": newDomainOnlyQUICSet(config.UDPModeReject),
		"block, drop":     blockingQUICSet(config.BlockActionDrop),
		"block, reject":   blockingQUICSet(config.BlockActionReject),
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			hello := buildQUICInitialWithSNI(t, []byte{0xd1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

			w, _ := flowDropWorker(t, &set)
			if v := sendQUIC(w, hello, 50501); v != engine.VerdictDropFlow {
				t.Fatalf("ProcessPacketDir returned %v, want VerdictDropFlow so TUN can hold the flow", v)
			}

			w, rec := flowDropWorker(t, &set)
			sent := captureInjected(t, w)
			queueFrom(w, rec, makeV4UDPPacket(hello, quicClient, quicServer, 50502, 443))
			events, jobs, _ := rec.snapshot()
			if len(events) != 2 || events[0] != "verdict drop" || events[1] != "conntrack original" {
				t.Fatalf("events %q, want the drop verdict, then the conntrack delete by the original tuple", events)
			}
			if want := udpTuple(t, "10.0.0.1:50502", "1.2.3.4:443"); jobs[0].tuple != want {
				t.Fatalf("delete tuple %s, want the queued packet's %s", jobs[0].tuple, want)
			}
			icmp := 0
			for _, p := range sent() {
				if len(p) > 9 && p[9] == unix.IPPROTO_ICMP {
					icmp++
				}
			}
			wantICMP := 0
			if strings.HasSuffix(name, "reject") {
				wantICMP = 1
			}
			if icmp != wantICMP {
				t.Fatalf("%d ICMP rejects sent before the persistence returned, want %d", icmp, wantICMP)
			}
		})
	}
}

func TestOnlyDecisionDropsOfQUICHoldTheFlow(t *testing.T) {
	stun := make([]byte, 20)
	binary.BigEndian.PutUint16(stun[0:2], 0x0001)
	binary.BigEndian.PutUint32(stun[4:8], 0x2112A442)
	copy(stun[8:], "b4-stun-txid")
	rtp := make([]byte, 40)
	rtp[0], rtp[1] = 0x80, 0x78

	learning := newDomainOnlyQUICSet(config.UDPModeDrop)
	learning.Targets.DomainOnly = false
	learning.UDP.FilterSTUN = false

	ipSet := config.NewSetConfig()
	ipSet.Id = "ip-drop"
	ipSet.Name = "IPDrop"
	ipSet.Enabled = true
	ipSet.Targets.IPs = []string{"1.2.3.4/32"}
	ipSet.UDP.Mode = config.UDPModeDrop
	ipSet.Fragmentation.Strategy = config.ConfigNone

	for _, c := range []struct {
		name    string
		set     config.SetConfig
		teach   bool
		payload []byte
		want    engine.PacketVerdict
	}{
		{"plain UDP to a dropping address set", ipSet, false, []byte("plain udp, not quic"), engine.VerdictDrop},
		{"a QUIC fake", newDomainOnlyQUICSet(config.UDPModeFake), false, nil, engine.VerdictDrop},
		{"a coalesced QUIC Initial", newDomainOnlyQUICSet(config.UDPModeCoalesce), false, nil, engine.VerdictDrop},
		{"QUIC of a set that leaves UDP alone", newDomainOnlyQUICSet(config.ConfigOff), false, nil, engine.VerdictAccept},
		{"STUN to an address learned for a dropped name", learning, true, stun, engine.VerdictDrop},
		{"Discord voice to an address learned for a dropped name", learning, true, rtp, engine.VerdictDrop},
	} {
		t.Run(c.name, func(t *testing.T) {
			payload := c.payload
			if payload == nil {
				payload = buildQUICInitialWithSNI(t, []byte{0xd2, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
			}
			w, rec := flowDropWorker(t, &c.set)
			if c.teach {
				teach := buildQUICInitialWithSNI(t, []byte{0xd3, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
				if v := sendQUIC(w, teach, 50600); v != engine.VerdictDropFlow {
					t.Fatalf("the ClientHello that teaches the address must hold its own flow, got %v", v)
				}
			}
			if v := sendQUIC(w, payload, 50601); v != c.want {
				t.Fatalf("ProcessPacket returned %v, want %v", v, c.want)
			}
			queueFrom(w, rec, makeV4UDPPacket(payload, quicClient, quicServer, 50602, 443))
			w.wg.Wait()
			if _, jobs, writes := rec.snapshot(); len(jobs) != 0 || len(writes) != 0 {
				t.Fatalf("%d conntrack deletes and %d set writes for a drop that is no decision on a QUIC flow", len(jobs), len(writes))
			}
		})
	}
}

func TestATCPStrategyNeverHoldsTheFlow(t *testing.T) {
	cfg := directionConfig("443")
	w := directionWorker(t, &cfg)
	rec := &flowDropRecorder{}
	w.flowDrops = newFlowDropPersistence()
	w.flowDrops.setDeleter(rec)

	if v := w.ProcessPacket(clientHelloFrom(40001)); v != engine.VerdictDrop {
		t.Fatalf("the fragmenting set rewrites the ClientHello: want VerdictDrop, got %v", v)
	}
	raw := clientHelloFrom(40002)
	queueFrom(w, rec, raw)
	w.wg.Wait()
	events, jobs, _ := rec.snapshot()
	if len(events) != 1 || events[0] != "verdict drop" || len(jobs) != 0 {
		t.Fatalf("events %q: a TCP rewrite must not reach the flow persistence", events)
	}
}

func TestFlowDropPrefersTheDropSetOfThePacketsFamily(t *testing.T) {
	f := newFlowDropPersistence()
	rec := &flowDropRecorder{}
	f.setWriters(rec.write, nil)
	f.setDeleter(rec)
	now := time.Unix(1000, 0)

	v4 := makeV4UDPPacket([]byte{0xc0, 0, 0, 0, 1, 0, 0}, net.ParseIP("192.168.1.10"), quicServer, 50001, 443)
	f.persist(v4, now)
	events, jobs, writes := rec.snapshot()
	if len(writes) != 1 || len(jobs) != 0 {
		t.Fatalf("%d set writes and %d counted deletes, want the IPv4 drop set to hold the flow", len(writes), len(jobs))
	}
	if want := (flowDropKey{netip.MustParseAddrPort("192.168.1.10:50001"), netip.MustParseAddrPort("1.2.3.4:443")}); writes[0] != want {
		t.Fatalf("set key %v, want %v", writes[0], want)
	}
	if want := []string{"nft", "conntrack original behind the set"}; strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events %q, want %q: once the flow is in the set its conntrack entry goes too, so the flow restarts in the queue window when the set lets it go", events, want)
	}
	if behind := rec.behindSet(); len(behind) != 1 || behind[0].tuple != udpTuple(t, "192.168.1.10:50001", "1.2.3.4:443") || behind[0].dir != ctnl.Original {
		t.Fatalf("deletes behind the set %+v, want the queued packet's original tuple", behind)
	}

	v6 := makeV6UDPPacketWithExt([]byte{0xc0, 0, 0, 0, 1, 0, 0}, 50002, 443, 0, nil)
	f.persist(v6, now)
	_, jobs, writes = rec.snapshot()
	if len(writes) != 1 || len(jobs) != 1 || len(rec.behindSet()) != 1 {
		t.Fatalf("with no IPv6 drop set the IPv6 flow must go to conntrack: %d writes, %d deletes, %d behind the set", len(writes), len(jobs), len(rec.behindSet()))
	}
	if want := udpTuple(t, "["+v6TestSrc+"]:50002", "["+v6TestDst+"]:443"); jobs[0].tuple != want || jobs[0].dir != ctnl.Original {
		t.Fatalf("delete %s by %s, want %s by the original tuple", jobs[0].tuple, jobs[0].dir, want)
	}

	s := f.snapshot()
	if !s.NftV4 || s.NftV6 || !s.Conntrack || s.NftWritten != 1 || s.ConntrackQueued != 1 || s.ConntrackBehindSet != 1 || s.Unpersisted != 0 {
		t.Fatalf("counters %+v", s)
	}

	f.setDeleter(nil)
	other := makeV4UDPPacket([]byte{0xc0, 0, 0, 0, 1, 0, 0}, net.ParseIP("192.168.1.10"), quicServer, 50009, 443)
	f.persist(other, now)
	if _, _, writes = rec.snapshot(); len(writes) != 2 || len(rec.behindSet()) != 1 {
		t.Fatalf("without conntrack the set alone holds the flow: %d writes, %d deletes behind the set", len(writes), len(rec.behindSet()))
	}
	if s := f.snapshot(); s.NftWritten != 2 || s.Unpersisted != 0 {
		t.Fatalf("a flow the set holds is persisted with or without conntrack: counters %+v", s)
	}
}

func TestFlowDropWritesAFlowOnceInAFewSeconds(t *testing.T) {
	f := newFlowDropPersistence()
	rec := &flowDropRecorder{}
	f.setWriters(rec.write, rec.write)
	f.setDeleter(rec)
	raw := makeV4UDPPacket([]byte{1}, quicClient, quicServer, 50003, 443)
	other := makeV4UDPPacket([]byte{1}, quicClient, quicServer, 50004, 443)
	now := time.Unix(2000, 0)

	f.persist(raw, now)
	f.persist(raw, now.Add(time.Second))
	f.persist(other, now.Add(time.Second))
	f.persist(raw, now.Add(flowDropRecentTTL-time.Millisecond))
	if _, jobs, writes := rec.snapshot(); len(writes) != 2 || len(jobs) != 0 {
		t.Fatalf("%d writes and %d deletes, want one write per flow inside the window and no conntrack fallback", len(writes), len(jobs))
	}
	if n := len(rec.behindSet()); n != 2 {
		t.Fatalf("%d deletes behind the set, want one per write", n)
	}
	f.persist(raw, now.Add(flowDropRecentTTL))
	if _, _, writes := rec.snapshot(); len(writes) != 3 || len(rec.behindSet()) != 3 {
		t.Fatalf("%d writes and %d deletes behind the set, want the flow written and deleted again once the window passed", len(writes), len(rec.behindSet()))
	}
	if s := f.snapshot(); s.NftRecent != 2 || s.NftWritten != 3 {
		t.Fatalf("counters %+v, want 2 recent and 3 written", s)
	}

	f.setWriters(rec.write, rec.write)
	f.persist(raw, now.Add(flowDropRecentTTL+time.Millisecond))
	if _, _, writes := rec.snapshot(); len(writes) != 4 {
		t.Fatal("publishing the drop sets again (a rebuilt table) must forget the recent writes")
	}
}

func TestFlowDropFallsBackToConntrackWhenTheSetWriteFails(t *testing.T) {
	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})

	f := newFlowDropPersistence()
	rec := &flowDropRecorder{setErr: errors.New("no such file or directory")}
	f.setWriters(rec.write, nil)
	f.setDeleter(rec)
	raw := makeV4UDPPacket([]byte{1}, quicClient, quicServer, 50005, 443)
	now := time.Unix(3000, 0)

	f.persist(raw, now)
	f.persist(raw, now.Add(time.Millisecond))
	events, jobs, _ := rec.snapshot()
	want := []string{"nft", "conntrack original", "nft", "conntrack original"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events %q, want %q: a failed write is not remembered and each drop falls back to conntrack", events, want)
	}
	if jobs[0].tuple != udpTuple(t, "10.0.0.1:50005", "1.2.3.4:443") {
		t.Fatalf("fallback tuple %s", jobs[0].tuple)
	}
	if s := f.snapshot(); s.NftFailed != 2 || s.ConntrackQueued != 2 {
		t.Fatalf("counters %+v", s)
	}
	if n := strings.Count(buf.String(), "drop set failed"); n != 1 {
		t.Fatalf("the failed write was logged %d times, want once:\n%s", n, buf.String())
	}
}

func TestFlowDropCountsWhatItCannotHold(t *testing.T) {
	quic := []byte{0xc0, 0, 0, 0, 1, 0, 0}
	udp := makeV4UDPPacket(quic, quicClient, quicServer, 50006, 443)

	f := newFlowDropPersistence()
	f.persist(udp, time.Now())
	if s := f.snapshot(); s.Unpersisted != 1 {
		t.Fatalf("with neither a drop set nor conntrack: counters %+v, want one unpersisted", s)
	}

	rec := &flowDropRecorder{refuse: true}
	f.setDeleter(rec)
	f.persist(udp, time.Now())
	if s := f.snapshot(); s.Unpersisted != 1 || s.ConntrackRefused != 1 || s.ConntrackQueued != 0 {
		t.Fatalf("with conntrack refusing the job: counters %+v, want it counted as refused, not as never tried", s)
	}

	rec.refuse = false
	tcp := makeV4TCPPacket([]byte{1}, 1)
	truncated := udp[:IPv4HeaderMinLen+4]
	mapped := makeV6UDPPacketWithExt(quic, 50007, 443, 0, nil)
	copy(mapped[8:24], net.ParseIP("::ffff:10.0.0.1").To16())
	copy(mapped[24:40], net.ParseIP("::ffff:1.2.3.4").To16())
	for _, raw := range [][]byte{tcp, truncated, mapped, nil} {
		f.persist(raw, time.Now())
	}
	if s := f.snapshot(); s.Unpersisted != 5 || s.ConntrackQueued != 0 || s.ConntrackRefused != 1 {
		t.Fatalf("a packet without a complete UDP tuple must be counted, never deleted: counters %+v", s)
	}
	f.setDeleter(nil)
	if f.snapshot().Conntrack {
		t.Fatal("clearing the deleter must show in the snapshot")
	}
}

func TestFlowDropReadsTheUDPHeaderPastIPv6ExtensionHeaders(t *testing.T) {
	ext := make([]byte, 8)
	ext[0] = unix.IPPROTO_UDP
	raw := makeV6UDPPacketWithExt([]byte{1}, 50008, 443, 0, ext)
	src, dst, ok := udpFlowEndpoints(raw)
	if !ok {
		t.Fatal("hop-by-hop header in front of UDP: endpoints not found")
	}
	if src != netip.MustParseAddrPort("["+v6TestSrc+"]:50008") || dst != netip.MustParseAddrPort("["+v6TestDst+"]:443") {
		t.Fatalf("endpoints %s -> %s", src, dst)
	}
}

func TestPoolWiresTheDropSetWriters(t *testing.T) {
	p := &Pool{state: newRuntimeState()}
	rec := &flowDropRecorder{}
	p.SetUDPDropWriters(nil, rec.write)
	if s := p.state.flowDrops.snapshot(); s.NftV4 || !s.NftV6 {
		t.Fatalf("published writers %+v, want IPv6 only", s)
	}
	p.SetUDPDropWriters(nil, nil)
	if s := p.state.flowDrops.snapshot(); s.NftV4 || s.NftV6 {
		t.Fatalf("clearing the writers left %+v", s)
	}
	var nilPool *Pool
	nilPool.SetUDPDropWriters(rec.write, rec.write)
}
