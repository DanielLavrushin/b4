package nfq

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/warmtuple"
)

type collectCountRig struct {
	w        *Worker
	p        *Pool
	sent     func() [][]byte
	installs int
	flows    map[ctnl.Tuple]*ctnl.Flow
	start    time.Time
}

func newCollectCountRig(t *testing.T, extra ...*config.SetConfig) *collectCountRig {
	t.Helper()
	r := &collectCountRig{flows: make(map[ctnl.Tuple]*ctnl.Flow), start: time.Now()}
	r.w, r.p, r.sent = unseededCollectedRig(t, func(context.Context, *config.Config) error {
		r.installs++
		return nil
	}, extra...)
	r.w.bans = newTestIPBans(t, nil)
	lookupFlow = func(tuple ctnl.Tuple, _ ctnl.Direction) (ctnl.Flow, bool, error) {
		return *r.flowOf(tuple), true, nil
	}
	return r
}

func (r *collectCountRig) flowOf(tuple ctnl.Tuple) *ctnl.Flow {
	f := r.flows[tuple]
	if f == nil {
		flow := silentFlow().flow
		flow.ID = uint32(100 + len(r.flows))
		f = &flow
		r.flows[tuple] = f
	}
	return f
}

func (r *collectCountRig) flow(t *testing.T, sport uint16) *ctnl.Flow {
	t.Helper()
	tuple, err := ctnl.NewTuple(6, netip.AddrPortFrom(netip.MustParseAddr("10.0.0.1"), sport), netip.MustParseAddrPort("1.2.3.4:443"))
	if err != nil {
		t.Fatal(err)
	}
	return r.flowOf(tuple)
}

func (r *collectCountRig) watched(sport uint16) bool {
	r.w.destState.watchMu.RLock()
	defer r.w.destState.watchMu.RUnlock()
	for _, h := range r.w.destState.watches {
		if h.cport == sport {
			return true
		}
	}
	return false
}

func (r *collectCountRig) hello(t *testing.T, sport uint16) {
	t.Helper()
	if v := r.w.ProcessPacket(silentHelloFrom(sport)); v != engine.VerdictAccept {
		t.Fatalf("Collect changed the verdict of the ClientHello from port %d: %v", sport, v)
	}
	if !r.watched(sport) {
		t.Fatalf("the ClientHello from port %d is not watched, so this test proves nothing", sport)
	}
}

func (r *collectCountRig) serverSends(t *testing.T, sport uint16, flags byte, payload []byte) {
	t.Helper()
	raw := collectedReply(sport, afterHello(), flags, payload)
	f := r.flow(t, sport)
	f.ReplyPackets++
	f.ReplyBytes += uint64(len(raw))
	r.w.ProcessPacket(raw)
}

func (r *collectCountRig) silent(t *testing.T, sport uint16) {
	t.Helper()
	r.hello(t, sport)
	r.serverSends(t, sport, 0x10, nil)
}

func (r *collectCountRig) sweep(after time.Duration) {
	r.p.collectedPass(r.start.Add(after))
}

func (r *collectCountRig) collected() int {
	r.p.configMu.Lock()
	defer r.p.configMu.Unlock()
	return len(r.p.collected)
}

func TestOneSilentConnectionDoesNotCollectTheAddress(t *testing.T) {
	r := newCollectCountRig(t)
	r.silent(t, 42000)
	r.sweep(4 * time.Second)

	if r.collected() != 0 || r.installs != 0 {
		t.Fatalf("one silent connection must not collect the address: candidates=%d installs=%d", r.collected(), r.installs)
	}
	if ports := resetsTo(r.sent()); len(ports) != 0 {
		t.Fatalf("one silent connection must not be reset, got resets to %v", ports)
	}
	if !r.watched(42000) {
		t.Fatal("the counted connection must stay watched, so it can be reset with the connection that completes the count")
	}
}

func TestTwoSilentConnectionsInARowCollectAndResetBoth(t *testing.T) {
	r := newCollectCountRig(t)
	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)

	if r.collected() != 1 || r.installs != 1 {
		t.Fatalf("the second silent connection in a row must collect the address once: candidates=%d installs=%d", r.collected(), r.installs)
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{42000, 42001}) {
		t.Fatalf("both silent connections must be reset so their retries reach the consumer, got resets to %v", ports)
	}
	if r.w.destState.Watching() {
		t.Error("reset connections must not stay watched")
	}
}

func TestAnAnsweredConnectionStartsTheCollectCountOver(t *testing.T) {
	for name, answer := range map[string]func(*testing.T, *collectCountRig){
		"the server sent data the queue saw": func(t *testing.T, r *collectCountRig) {
			r.hello(t, 42001)
			r.serverSends(t, 42001, 0x18, []byte{0x16, 3, 3, 0, 1, 2})
		},
		"the client acknowledged server bytes": func(t *testing.T, r *collectCountRig) {
			r.silent(t, 42001)
			r.w.ProcessPacket(clientPacketAcking(42001, afterHello(), silentServerAck+1400, 0x10, nil))
		},
		"conntrack shows server bytes the queue did not see": func(t *testing.T, r *collectCountRig) {
			r.silent(t, 42001)
			f := r.flow(t, 42001)
			f.ReplyPackets++
			f.ReplyBytes += 1400
			r.sweep(5 * time.Second)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newCollectCountRig(t)
			r.silent(t, 42000)
			r.sweep(4 * time.Second)
			answer(t, r)
			if r.watched(42001) {
				t.Fatal("an answered connection must not stay watched")
			}
			r.silent(t, 42002)
			r.sweep(6 * time.Second)

			if r.collected() != 0 || r.installs != 0 || len(resetsTo(r.sent())) != 0 {
				t.Fatalf("an answered connection between two silent ones must start the count over: candidates=%d installs=%d resets=%v", r.collected(), r.installs, resetsTo(r.sent()))
			}
			if !r.watched(42002) {
				t.Fatal("the silent connection after the answer is the first of a new count and waits")
			}
		})
	}
}

func TestAnAnswerInTheSameSweepLeavesSilentConnectionsUncounted(t *testing.T) {
	r := newCollectCountRig(t)
	r.silent(t, 42000)
	r.silent(t, 42001)
	r.silent(t, 42002)
	f := r.flow(t, 42002)
	f.ReplyPackets++
	f.ReplyBytes += 1400
	r.sweep(4 * time.Second)

	if r.collected() != 0 || r.installs != 0 || len(resetsTo(r.sent())) != 0 {
		t.Fatalf("silent connections that come due with an answered one must not be counted: candidates=%d installs=%d resets=%v", r.collected(), r.installs, resetsTo(r.sent()))
	}
	r.silent(t, 42003)
	r.sweep(5 * time.Second)
	if r.collected() != 0 {
		t.Fatal("a silent connection after the sweep with the answer is the first of a new count")
	}
}

func TestAnAnswerOnABlockWatchClearsTheCollectCount(t *testing.T) {
	blockHello := buildClientHello("www.block.example", 16, 0xAB)
	blockSet := func(consumer bool) *config.SetConfig {
		set := silenceSet()
		set.Id, set.Name = "block", "block"
		set.Targets.DomainsToMatch = []string{"block.example"}
		set.Targets.UseCollectedIPs = consumer
		return &set
	}
	for name, tc := range map[string]struct {
		consumer bool
		answer   func(*testing.T, *collectCountRig)
	}{
		"the client acknowledged server bytes": {answer: func(t *testing.T, r *collectCountRig) {
			r.w.ProcessPacket(clientPacketAcking(43000, silentHelloSeq+uint32(len(blockHello)), silentServerAck+1400, 0x10, nil))
		}},
		"conntrack shows server bytes at the deadline": {answer: func(t *testing.T, r *collectCountRig) {
			f := r.flow(t, 43000)
			f.ReplyPackets, f.ReplyBytes = 4, 4200
			r.w.sweepSilentHellos(r.w.getConfig(), r.start.Add(5*time.Second))
		}},
		"Block's answer check before a ban": {answer: func(t *testing.T, r *collectCountRig) {
			f := r.flow(t, 43000)
			f.ReplyPackets, f.ReplyBytes = 4, 4200
			r.w.bans.gotAnswer = r.w.silentHelloGotAnswer
			dst := net.ParseIP("1.2.3.4")
			if r.w.bans.stillSilent(iphealth.BanKey(dst, 443), dst, 443) {
				t.Fatal("Block's answer check must find the server bytes conntrack counted")
			}
		}},
		"a set that uses collected IPs": {consumer: true, answer: func(t *testing.T, r *collectCountRig) {
			r.w.ProcessPacket(clientPacketAcking(43000, silentHelloSeq+uint32(len(blockHello)), silentServerAck+1400, 0x10, nil))
		}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newCollectCountRig(t, blockSet(tc.consumer))
			r.silent(t, 42000)
			r.sweep(4 * time.Second)
			r.w.ProcessPacket(clientPacket(43000, silentHelloSeq, 0x18, blockHello))
			if !r.watched(43000) {
				t.Fatal("the Block set does not watch its connection, so this test proves nothing")
			}
			tc.answer(t, r)
			if r.watched(43000) {
				t.Fatal("the answered Block connection must not stay watched")
			}
			r.silent(t, 42001)
			r.sweep(6 * time.Second)

			if want := map[bool]int{false: 0, true: 1}[tc.consumer]; r.collected() != want {
				t.Fatalf("candidates=%d, want %d: an answer on a watch of a set that does not use collected IPs starts the count over, one on a consumer's watch does not", r.collected(), want)
			}
		})
	}
}

func TestAParkedConnectionEndsAfterTheWatchLifetime(t *testing.T) {
	r := newCollectCountRig(t)
	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	if !r.watched(42000) {
		t.Fatal("the first silent connection must wait for a second one, or this test proves nothing")
	}
	r.sweep(helloWatchTTL)
	if !r.watched(42000) {
		t.Fatal("a waiting connection must stay watched for the watch lifetime past its deadline")
	}
	r.sweep(4*time.Second + helloWatchTTL)
	if r.w.destState.Watching() || len(r.w.destState.collectDeadlines) != 0 {
		t.Fatal("a connection that waited the whole watch lifetime must stop being watched")
	}
	if r.collected() != 0 || len(resetsTo(r.sent())) != 0 {
		t.Fatal("a connection that waited in vain is neither collected nor reset")
	}
}

func TestTheCollectCountStartsOverAfterTheRetestInterval(t *testing.T) {
	r := newCollectCountRig(t)
	r.w.getConfig().System.IPHealth.RetestIntervalSec = 30

	r.silent(t, 42000)
	r.sweep(10 * time.Second)
	r.silent(t, 42001)
	r.sweep(50 * time.Second)
	if r.collected() != 0 {
		t.Fatal("a silent connection counted longer than the retest interval ago no longer counts")
	}

	r.silent(t, 42002)
	r.sweep(60 * time.Second)
	if r.collected() != 1 {
		t.Fatal("two silent connections within the retest interval must collect the address")
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{42000, 42001, 42002}) {
		t.Fatalf("every connection still waiting on the address is reset once it is collected, got resets to %v", ports)
	}
}

func TestABlockBanDoesNotTakeAParkedCollectWatch(t *testing.T) {
	tracker := &destStateTracker{}
	dst := net.ParseIP("1.2.3.4")
	tracker.WatchHello(&helloWatch{key: "c", setID: "producer", collect: true, flowID: 7, replyKey: "r", dst: dst, port: 443, deadline: time.Now()})
	tracker.Park(*tracker.watches["c"])
	if taken := tracker.TakeSilent(dst, 443); len(taken) != 0 {
		t.Fatalf("a Block ban settling on %s:443 took %d parked Collect watches", dst, len(taken))
	}
	if watched := tracker.WatchesTo(dst, 443); len(watched) != 0 {
		t.Fatalf("Block's answer check sees %d Collect watches", len(watched))
	}

	r := newCollectCountRig(t)
	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.w.settleSilentHellos(dst, 443, true)
	if !r.watched(42000) || len(resetsTo(r.sent())) != 0 {
		t.Fatal("a Block ban on the address must leave the waiting Collect connection alone")
	}
	r.silent(t, 42001)
	r.sweep(5 * time.Second)
	if ports := resetsTo(r.sent()); r.collected() != 1 || !slices.Equal(ports, []uint16{42000, 42001}) {
		t.Fatalf("the waiting connection is still reset with the one that completes the count, got candidates=%d resets=%v", r.collected(), ports)
	}
}

func TestAWarmFlowIsNeverWatched(t *testing.T) {
	for _, action := range []string{"block", "collect"} {
		t.Run(action, func(t *testing.T) {
			set := silenceSet()
			set.TCP.IPBlockDetect.Action = action
			r := newFreshSilenceWorker(t, &set, iphealth.TLSSilent)
			r.answerLookups(t, func(ctnl.Tuple) flowAnswer {
				a := silentFlow()
				a.flow.ID = 71
				return a
			})
			r.w.warmFlows = warmtuple.NewFlows()
			release, err := r.w.warmFlows.Register(netip.MustParseAddrPort("10.0.0.1:42000"), netip.MustParseAddrPort("1.2.3.4:443"), r.w.getConfig(), r.w.getConfig().Sets[0], "i.ytimg.com")
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			sendHello(t, r.w, 42000, engine.VerdictAccept)
			if r.w.destState.Watching() {
				t.Fatal("a warm connection carries no device traffic, so IP Block Detection must not watch it")
			}
			sendHello(t, r.w, 42001, engine.VerdictAccept)
			if !r.w.destState.Watching() {
				t.Fatal("a device connection to the same address must still be watched, or this test proves nothing")
			}
		})
	}
}
