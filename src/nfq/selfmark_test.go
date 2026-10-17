package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/florianl/go-nfqueue"
)

func selfMarkConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Queue.Mark = 0x8000
	return cfg
}

func TestSelfInjectedMark_QueueMarkAlone(t *testing.T) {
	if !selfInjectedMark(0x8000, 0x8000, selfMarkConfig()) {
		t.Error("a packet carrying exactly the queue mark is one b4 injected")
	}
}

func TestSelfInjectedMark_QueueMarkPlusRoutingMark(t *testing.T) {
	cfg := selfMarkConfig()
	for _, m := range []uint32{0x8000 | 0x2bd8, 0x8000 | 0x100, 0x8000 | 0x24247} {
		if !selfInjectedMark(m, 0x8000, cfg) {
			t.Errorf("mark 0x%x is an injected packet a set's routing chain marked; dispatching it feeds b4 its own output", m)
		}
	}
}

func TestSelfInjectedMark_ClientTraffic(t *testing.T) {
	cfg := selfMarkConfig()
	for _, m := range []uint32{0, 0x1, 0x2bd8, 0x10000, 0x18000} {
		if selfInjectedMark(m, 0x8000, cfg) {
			t.Errorf("mark 0x%x does not carry b4's queue mark and per-set bits only; accepting it would let traffic past the engine", m)
		}
	}
}

func TestSelfInjectedMark_DiscoveryMarksStayProcessed(t *testing.T) {
	cfg := selfMarkConfig()
	cfg.System.Checker.DiscoveryFlowMark = 0x8001
	cfg.System.Checker.DiscoveryInjectedMark = 0x8002

	if selfInjectedMark(0x8001, 0x8000, cfg) {
		t.Error("the discovery flow mark shares the queue bit; treating it as injected would take discovery's traffic out of the engine")
	}
	if selfInjectedMark(0x8002, 0x8000, cfg) {
		t.Error("the discovery injected mark shares the queue bit and must keep its own handling")
	}
}

func TestSelfInjectedMark_DerivedDiscoveryMarks(t *testing.T) {
	cfg := selfMarkConfig()
	if selfInjectedMark(uint32(cfg.DiscoveryFlowMark()), 0x8000, cfg) {
		t.Error("the derived discovery flow mark must not read as injected")
	}
	if selfInjectedMark(uint32(cfg.DiscoveryInjectedMark()), 0x8000, cfg) {
		t.Error("the derived discovery injected mark must not read as injected")
	}
}

func TestSelfInjectedMark_DiscoveryWorkerSeesItsOwn(t *testing.T) {
	cfg := selfMarkConfig()
	cfg.System.Checker.DiscoveryFlowMark = 0x8001
	cfg.System.Checker.DiscoveryInjectedMark = 0x8002

	if !selfInjectedMark(0x8002, 0x8002, cfg) {
		t.Error("the discovery worker runs with the discovery injected mark as its queue mark and must skip its own packets")
	}
	if selfInjectedMark(0x8001, 0x8002, cfg) {
		t.Error("the discovery worker must still process the flow it is testing")
	}
}

func TestLocalQueueMarked(t *testing.T) {
	cfg := selfMarkConfig()
	root, out, in := uint32(0), uint32(2), uint32(3)
	mark := func(m uint32) *uint32 { return &m }
	for _, c := range []struct {
		name string
		a    nfqueue.Attribute
		want bool
	}{
		{"a local socket, the queue mark and a neighbour's bit", nfqueue.Attribute{Mark: mark(0x8000 | 0x10000), UID: &root, OutDev: &out}, true},
		{"a local socket, the KeeneticOS hotspot mark", nfqueue.Attribute{Mark: mark(0x0ffffaaa), UID: &root, OutDev: &out}, true},
		{"no socket, the queue mark and a neighbour's bit", nfqueue.Attribute{Mark: mark(0x8000 | 0x10000), OutDev: &out}, false},
		{"arriving for a local socket", nfqueue.Attribute{Mark: mark(0x8000 | 0x10000), UID: &root, InDev: &in}, false},
		{"a local socket, no queue mark", nfqueue.Attribute{Mark: mark(0x10000), UID: &root, OutDev: &out}, false},
		{"a local socket, no mark", nfqueue.Attribute{UID: &root, OutDev: &out}, false},
		{"a local socket, Discovery's flow", nfqueue.Attribute{Mark: mark(uint32(cfg.DiscoveryFlowMark())), UID: &root, OutDev: &out}, false},
		{"a local socket, Discovery's injected packets", nfqueue.Attribute{Mark: mark(uint32(cfg.DiscoveryInjectedMark())), UID: &root, OutDev: &out}, false},
	} {
		if got := localQueueMarked(c.a, 0x8000, cfg); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if localQueueMarked(nfqueue.Attribute{Mark: mark(0x8000), UID: &root, OutDev: &out}, 0, cfg) {
		t.Error("without a queue mark nothing reads as b4's own")
	}
}

func TestALocalPacketCarryingEveryQueueMarkBitIsLeftAlone(t *testing.T) {
	root := uint32(0)
	for _, c := range []struct {
		name string
		mark uint32
		uid  *uint32
		want int
		why  string
	}{
		{"a local socket, a neighbour's bit added", 0x8000 | 0x10000, &root, nfqueue.NfAccept, "a packet a local socket sent with every queue-mark bit is b4's own or the router's, as it was before the exact queue-mark match"},
		{"a local socket, the KeeneticOS hotspot mark", keeneticHotspotMark, &root, nfqueue.NfAccept, "the router's own packet keeps its old handling"},
		{"forwarded, a neighbour's bit added", 0x8000 | 0x10000, nil, nfqueue.NfDrop, "a forwarded packet has no socket, so a mark another service wrote gets the strategy"},
		{"forwarded, the KeeneticOS hotspot mark", keeneticHotspotMark, nil, nfqueue.NfDrop, "a hotspot device's connection gets the strategy"},
		{"a local socket, no queue mark", 0x10000, &root, nfqueue.NfDrop, "the router's own traffic without the queue mark gets the strategy"},
	} {
		t.Run(c.name, func(t *testing.T) {
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stub := stubInjectRoute(t, idx["eth7"], true)
			set := newFragmentingYTImagesSet()
			w := newInjectingWorker(t, &set)
			raw := ytimgHello()
			id, out, mark := uint32(1), idx["eth7"], c.mark
			a := nfqueue.Attribute{PacketID: &id, Payload: &raw, Mark: &mark, OutDev: &out, UID: c.uid}
			q := newFakeQueue()
			w.handleQueued(&verdictCtx{id: id, q: q}, a, uint(w.getConfig().Queue.Mark))
			if got := q.now(t); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
			if calls := stub.got(); c.want == nfqueue.NfAccept && len(calls) != 0 {
				t.Fatalf("route lookups %v: the packet must be released as the router's own, not handed off", calls)
			}
		})
	}
}
