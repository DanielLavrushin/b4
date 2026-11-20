package nfq

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

var v4ConnKey = fmt.Sprintf(connKeyFormat, "10.0.0.1", 12345, "1.2.3.4", 443)

func occupyLane(t *testing.T, w *Worker, key string) func() {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(free)
	if !injectAcquire() {
		t.Fatal("no inject slot free")
	}
	w.injector.submit(key, true, w.flowJob(func() func() {
		<-gate
		return nil
	}))
	return free
}

func TestAHeldHelloIsReleasedWhenTheConnectionsLaneIsBusy(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	free := occupyLane(t, w, v4ConnKey)
	q := &verdictLog{}
	_, seg1, seg2 := splitHello("www.example.org")

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must be held", got)
	}
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+splitCut))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	free()
	if pkts := sent(); len(pkts) != 0 {
		t.Fatalf("%d packets sent, want both segments accepted in place", len(pkts))
	}
}

func TestANewConnectionOnTheSameTupleDoesNotWaitForTheOldOne(t *testing.T) {
	set := newFakingSet(false)
	w, sent := orderedWorker(t, &set)
	free := occupyLane(t, w, v4ConnKey)
	q := &verdictLog{}

	queueTo(w, q, 1, makeV4TCPPacketFlags(nil, 90000, 0x02))
	queueTo(w, q, 2, makeV4TCPPacket(buildClientHello("www.example.org", 16, 0xAB), 90001))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))
	free()
	if pkts := sent(); len(pkts) != 0 {
		t.Fatalf("%d packets sent, want the new connection's packets accepted in place", len(pkts))
	}
}

func TestTheInjectorKeepsEachConnectionsOrderUnderLoad(t *testing.T) {
	for _, detaching := range []bool{false, true} {
		t.Run(fmt.Sprintf("detaching=%v", detaching), func(t *testing.T) {
			cfg := config.NewConfig()
			w := newTestWorker(t, &cfg)
			before := InjectInflight()
			const (
				keys       = 6
				submitters = 12
				perEach    = 150
			)
			type mark struct{ from, n int }
			laned := func(n int) bool { return n%3 != 0 }
			var mu sync.Mutex
			got := make([][]mark, keys)
			var wg sync.WaitGroup
			for s := 0; s < submitters; s++ {
				wg.Add(1)
				go func(s int) {
					defer wg.Done()
					for n := 0; n < perEach; n++ {
						k := (s + n) % keys
						key := fmt.Sprintf("key-%d", k)
						for !injectAcquire() {
							time.Sleep(time.Microsecond)
						}
						job := w.flowJob(func() func() {
							mu.Lock()
							got[k] = append(got[k], mark{s, n})
							mu.Unlock()
							if n%7 == 0 {
								return func() {}
							}
							return nil
						})
						if !w.injector.submit(key, laned(n), job) {
							go func() { job.finish(job.run()) }()
						}
						if detaching && n%11 == 0 {
							w.injector.detach(key)
						}
					}
				}(s)
			}
			wg.Wait()
			w.wg.Wait()

			total := 0
			for k, marks := range got {
				total += len(marks)
				if detaching {
					continue
				}
				last := make(map[int]int)
				for _, m := range marks {
					if !laned(m.n) {
						continue
					}
					if prev, ok := last[m.from]; ok && m.n < prev {
						t.Fatalf("key %d: submitter %d's job %d ran after its job %d", k, m.from, m.n, prev)
					}
					last[m.from] = m.n
				}
			}
			if total != submitters*perEach {
				t.Fatalf("%d jobs ran, want %d", total, submitters*perEach)
			}
			if n := w.injector.pending(); n != 0 {
				t.Fatalf("%d connections left in the injector, want none", n)
			}
			if n := w.injector.live.Load(); n != 0 {
				t.Fatalf("the injector counts %d live connections, want none", n)
			}
			if n := InjectInflight(); n != before {
				t.Fatalf("%d inject slots taken after every job finished, want %d", n, before)
			}
		})
	}
}

func TestInsideHelloCoversTheWholeRecordFromItsFirstByte(t *testing.T) {
	c := newRuntimeState().tlsCache
	const key = "k"
	at := uint32(0xFFFFFF00)
	c.Store(key, "i.ytimg.com", 0x0303, at, 1, 1200)
	for _, tc := range []struct {
		seq  uint32
		want bool
	}{
		{at - 1, false},
		{at, true},
		{at + 600, true},
		{at + 1199, true},
		{at + 1200, false},
	} {
		if got := c.InsideHello(key, tc.seq); got != tc.want {
			t.Errorf("InsideHello at %+d: %v, want %v", int64(tc.seq)-int64(at), got, tc.want)
		}
	}
	if c.InsideHello("other", at) {
		t.Error("a connection with no stored hello has no hello range")
	}
	if c.LanedContinuation(key, at+600) {
		t.Error("a hello whose first segment did not go through the injector has no laned continuation")
	}
	c.MarkLaned(key, at)
	if !c.LanedContinuation(key, at+600) || c.LanedContinuation(key, at) || c.LanedContinuation(key, at+1200) {
		t.Error("once its first segment went through the injector, only the rest of the hello follows it")
	}
	c.MoveHello(key, at+1200, 500)
	if c.LanedContinuation(key, at+1300) {
		t.Error("a new hello on the connection starts without the previous hello's injector mark")
	}
	c.Store(key, "i.ytimg.com", 0x0303, at, 1, 0)
	if c.InsideHello(key, at+1) {
		t.Error("a hello of unknown length has no hello range")
	}
}
