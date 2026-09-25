package mtproto

import (
	"net"
	"sync"
	"testing"
	"time"
)

func relayPool() *sync.Pool {
	return &sync.Pool{New: func() interface{} {
		buf := make([]byte, 32768)
		return &buf
	}}
}

func withFastStall(t *testing.T) {
	t.Helper()
	prevClose, prevDue := relayStallClose, relayAnswerDue
	relayStallClose, relayAnswerDue = 400*time.Millisecond, 150*time.Millisecond
	t.Cleanup(func() { relayStallClose, relayAnswerDue = prevClose, prevDue })
}

func TestRelayConns_ReportsMuteUpstream(t *testing.T) {
	withFastStall(t)
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer clientA.Close()
	defer dcA.Close()

	stalled := make(chan struct{}, 1)
	onStall := func() { stalled <- struct{}{} }

	go func() {
		_, _ = clientA.Write([]byte("a request nobody answers"))
		time.Sleep(50 * time.Millisecond)
		_, _ = clientA.Write([]byte("the retry nobody answers either"))
		buf := make([]byte, 1)
		_, _ = clientA.Read(buf)
		_ = clientA.Close()
	}()
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := dcA.Read(buf); err != nil {
				return
			}
		}
	}()

	relayConns(clientB, dcB, relayOpts{label: "test", bufPool: relayPool(), onStall: onStall})

	select {
	case <-stalled:
	default:
		t.Fatal("a relay whose upstream never answered must be reported as stalled")
	}
}

func TestRelayConns_ReportsUpstreamThatClosesWithoutAnswering(t *testing.T) {
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer clientA.Close()

	stalled := make(chan struct{}, 1)
	onStall := func() { stalled <- struct{}{} }

	go func() {
		_, _ = clientA.Write([]byte("a request nobody answers"))
	}()
	go func() {
		buf := make([]byte, 512)
		_, _ = dcA.Read(buf)
		_ = dcA.Close()
	}()

	relayConns(clientB, dcB, relayOpts{label: "test", bufPool: relayPool(), onStall: onStall})

	select {
	case <-stalled:
	default:
		t.Fatal("an upstream that closed without ever answering must be reported as stalled")
	}
}

// The shape the fail-open path leaves behind on every plain-HTTP request it
// relays: the client sends, closes its side long before an answer could be due,
// and the route is left holding a request nobody is waiting for any more. On a
// real Worker the relays that did answer took 269-361 ms, so a client that quit
// at 69-103 ms says nothing about the route and must not cool it down.
func TestRelayConns_ClientClosingBeforeAnAnswerIsDueIsNotReported(t *testing.T) {
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	stalled := make(chan struct{}, 1)
	onStall := func() { stalled <- struct{}{} }

	go func() {
		_, _ = clientA.Write([]byte("a request"))
		time.Sleep(50 * time.Millisecond)
		_ = clientA.Close()
	}()
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := dcA.Read(buf); err != nil {
				return
			}
		}
	}()

	relayConns(clientB, dcB, relayOpts{label: "test", bufPool: relayPool(), onStall: onStall})

	select {
	case <-stalled:
		t.Fatal("a client that closed before an answer was due must not be scored against the route")
	default:
	}
}

func TestRelayConns_ClientThatAskedNothingIsNotReported(t *testing.T) {
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	stalled := make(chan struct{}, 1)
	onStall := func() { stalled <- struct{}{} }

	go func() { _ = clientA.Close() }()

	relayConns(clientB, dcB, relayOpts{label: "test", bufPool: relayPool(), onStall: onStall})

	select {
	case <-stalled:
		t.Fatal("a client that closed without asking for anything says nothing about the route")
	default:
	}
}

func TestRelayConns_HealthyUpstreamNotReported(t *testing.T) {
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer clientA.Close()
	defer dcA.Close()

	stalled := make(chan struct{}, 1)
	onStall := func() { stalled <- struct{}{} }

	go func() {
		buf := make([]byte, 512)
		for {
			n, err := dcA.Read(buf)
			if err != nil {
				return
			}
			if _, err := dcA.Write(buf[:n]); err != nil {
				return
			}
		}
	}()
	go func() {
		_, _ = clientA.Write([]byte("ping"))
		buf := make([]byte, 512)
		_, _ = clientA.Read(buf)
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayOpts{label: "test", bufPool: relayPool(), onStall: onStall})

	select {
	case <-stalled:
		t.Fatal("an upstream that answered must not be reported as stalled")
	default:
	}
}

func TestStallReporter_OnlyTracksWorkers(t *testing.T) {
	if stallReporter(dialInfo{transport: "ws://kws4.web.telegram.org"}) != nil {
		t.Error("the native edge fails by closing and must not be cooled down on a stall")
	}
	if stallReporter(dialInfo{transport: "tcp://149.154.167.91:443"}) != nil {
		t.Error("a direct connection fails by closing and must not be cooled down on a stall")
	}
	if stallReporter(dialInfo{isWorker: true, worker: "w.example.workers.dev"}) == nil {
		t.Error("a worker relay must report stalls")
	}
}

func TestWorkerStallCooldownExpires(t *testing.T) {
	workerResetStall()
	t.Cleanup(workerResetStall)

	const d = "expiring.workers.dev"
	if workerInCooldown(d) {
		t.Fatal("a worker starts healthy")
	}
	workerRecordStall(d)
	if !workerInCooldown(d) {
		t.Fatal("a stalled worker must be in cooldown")
	}

	workerStallMu.Lock()
	workerStallUntil[d] = time.Now().Add(-time.Second)
	workerStallMu.Unlock()

	if workerInCooldown(d) {
		t.Error("cooldown must lapse")
	}
	workerStallMu.Lock()
	_, still := workerStallUntil[d]
	workerStallMu.Unlock()
	if still {
		t.Error("a lapsed entry must be dropped rather than accumulate")
	}
}

// The proxy reaches a Worker for every data center Telegram's own edge does not
// front - 1, 3 and 5, which is the media path for foreign channels - so it meets
// the same silent Worker the bridge does and must react the same way.
func TestStallReporter_CoversProxyAndBridgeAlike(t *testing.T) {
	worker := dialInfo{transport: "wsworker://w.example.workers.dev", isWorker: true, worker: "w.example.workers.dev"}
	if stallReporter(worker) == nil {
		t.Error("a Worker relay must report stalls whichever feature opened it")
	}
	for _, d := range []dialInfo{
		{transport: "ws://kws4.web.telegram.org"},
		{transport: "ws-pool"},
		{transport: "tcp://149.154.167.91:443"},
	} {
		if stallReporter(d) != nil {
			t.Errorf("%s fails by closing and must not be cut or cooled down on a stall", d.transport)
		}
	}
}

func TestRelayConns_OneUnansweredWriteIsNotAStall(t *testing.T) {
	withFastStall(t)
	stall := relayStallClose
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	stalled := make(chan struct{}, 1)
	onStall := func() { stalled <- struct{}{} }

	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := dcA.Read(buf); err != nil {
				return
			}
		}
	}()
	go func() {
		_, _ = clientA.Write([]byte("an ack that expects no reply"))
		time.Sleep(3 * stall)
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayOpts{label: "test", bufPool: relayPool(), onStall: onStall})

	select {
	case <-stalled:
		t.Fatal("an idle session whose last write needs no answer was scored as a stall")
	default:
	}
}

func TestRelayConns_RequestAfterALongQuietIsNotCut(t *testing.T) {
	withFastStall(t)
	stall := relayStallClose
	clientA, clientB := net.Pipe()
	dcA, dcB := net.Pipe()
	defer dcA.Close()

	stalled := make(chan struct{}, 1)
	onStall := func() { stalled <- struct{}{} }
	answered := make(chan struct{})

	go func() {
		_, _ = dcA.Write([]byte("an update"))
		buf := make([]byte, 512)
		for {
			if _, err := dcA.Read(buf); err != nil {
				return
			}
			time.Sleep(stall / 2)
			_, _ = dcA.Write([]byte("the answer"))
		}
	}()
	go func() {
		buf := make([]byte, 512)
		_, _ = clientA.Read(buf)
		time.Sleep(2 * stall)
		_, _ = clientA.Write([]byte("get file part 1"))
		_, _ = clientA.Write([]byte("get file part 2"))
		if _, err := clientA.Read(buf); err == nil {
			close(answered)
		}
		_ = clientA.Close()
	}()

	relayConns(clientB, dcB, relayOpts{label: "test", bufPool: relayPool(), onStall: onStall})

	select {
	case <-answered:
	default:
		t.Fatal("a request sent after a quiet spell was cut before its answer arrived")
	}
	select {
	case <-stalled:
		t.Fatal("a request answered in time was scored as a stall")
	default:
	}
}
