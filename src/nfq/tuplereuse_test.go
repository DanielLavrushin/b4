package nfq

import (
	"net"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
)

const reusedTuple = "10.0.0.1:12345->1.2.3.4:443"

func watchedAck(w *Worker, key string) (uint32, bool) {
	w.destState.watchMu.RLock()
	defer w.destState.watchMu.RUnlock()
	h, ok := w.destState.watches[key]
	if !ok {
		return 0, false
	}
	return h.helloAck, true
}

func helloCount(w *Worker, key string) int {
	w.destState.mu.RLock()
	defer w.destState.mu.RUnlock()
	if e, ok := w.destState.conns[key]; ok {
		return e.retransmits
	}
	return 0
}

func synWithISN(isn uint32) []byte {
	return clientPacketAcking(12345, isn, 0, 0x02, nil)
}

func TestANewConnectionOnTheSameTupleIsWatchedWithItsOwnServerSequence(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func() config.SetConfig
	}{
		{"a set that leaves the hello alone", silenceSet},
		{"a set that injects", injectingSilenceSet},
	} {
		for _, server := range []struct {
			name string
			ack  uint32
		}{
			{"its server starts ahead of the old one", silentServerAck + 0x10000},
			{"its server starts behind the old one", silentServerAck - 0x10000},
		} {
			t.Run(tc.name+", "+server.name, func(t *testing.T) {
				set := tc.set()
				r := newSilenceWorker(t, &set, iphealth.TLSSilent)

				r.w.ProcessPacket(silentHelloFrom(12345))
				if _, ok := watchedAck(r.w, reusedTuple); !ok {
					t.Fatal("the first connection's ClientHello must be watched")
				}
				r.w.ProcessPacket(synWithISN(89999))
				if r.w.destState.Watching() {
					t.Error("a SYN that opens a new connection on the tuple must end the old connection's watch")
				}
				r.w.ProcessPacket(clientPacketAcking(12345, 90000, server.ack, 0x18, silentHello()))
				if ack, ok := watchedAck(r.w, reusedTuple); !ok || ack != server.ack {
					t.Fatalf("the new connection must be watched with the sequence number its own server sent: watched %v, ack %#x, want %#x", ok, ack, server.ack)
				}
				if n := helloCount(r.w, reusedTuple); n != 1 {
					t.Fatalf("the new connection's ClientHello is its first, counted %d", n)
				}

				r.sweepLater()
				if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
					t.Fatalf("the address the new connection got no answer from must be checked once, probed %v", keys)
				}
				if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
					t.Fatal("a silent neutral-name check must ban the address")
				}
				sent := r.sent()
				if ports := resetsTo(sent); !slices.Equal(ports, []uint16{12345}) {
					t.Fatalf("the waiting connection must get one reset, got resets to %v", ports)
				}
				if seqs := resetSeqs(sent); !slices.Equal(seqs, []uint32{server.ack}) {
					t.Fatalf("the reset must carry the sequence number the new connection expects, got %#x want %#x", seqs, server.ack)
				}
			})
		}
	}
}

func TestANewConnectionOnTheSameTupleStartsItsOwnResendCount(t *testing.T) {
	for name, mutate := range map[string]func(*config.SetConfig){
		"at the resend threshold": func(s *config.SetConfig) { s.TCP.IPBlockDetect.RetransmitThreshold = 2 },
		"past timeout_ms":         func(s *config.SetConfig) { s.TCP.IPBlockDetect.TimeoutMs = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			mutate(&set)
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			time.Sleep(5 * time.Millisecond)
			r.w.ProcessPacket(synWithISN(89999))
			if v := r.w.ProcessPacket(clientPacketAcking(12345, 90000, silentServerAck+0x10000, 0x18, silentHello())); v != engine.VerdictAccept {
				t.Errorf("the first ClientHello of a new connection is not a resend of the old connection's, got %v", v)
			}
			r.w.bans.wg.Wait()
			if ports := resetsTo(r.sent()); len(ports) != 0 {
				t.Errorf("the new connection must not be reset for the old connection's ClientHello, got resets to %v", ports)
			}
			if r.w.destState.IsBlocked("1.2.3.4:443") {
				t.Error("one ClientHello per connection must not mark the address blocked")
			}
			if keys := r.probed(); len(keys) != 0 {
				t.Errorf("one ClientHello per connection must not get the address checked, probed %v", keys)
			}
		})
	}
}

func TestASynWithoutPayloadEndsTheOldConnectionEvenWithoutItsNameEntry(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.w.tlsCache.mu.Lock()
	delete(r.w.tlsCache.conns, reusedTuple)
	r.w.tlsCache.mu.Unlock()

	r.w.ProcessPacket(synWithISN(89999))
	if r.w.destState.Watching() {
		t.Error("a SYN without payload opens a connection that has sent no ClientHello, so the old connection's watch must end even after its name entry was evicted")
	}
	if n := helloCount(r.w, reusedTuple); n != 0 {
		t.Fatalf("the old connection's ClientHello count must go with it, got %d", n)
	}
	r.w.ProcessPacket(clientPacketAcking(12345, 90000, silentServerAck+0x10000, 0x18, silentHello()))
	if ack, ok := watchedAck(r.w, reusedTuple); !ok || ack != silentServerAck+0x10000 {
		t.Fatalf("the new connection must be watched with its own server sequence number: watched %v, ack %#x", ok, ack)
	}
	if n := helloCount(r.w, reusedTuple); n != 1 {
		t.Fatalf("the new connection's ClientHello is its first, counted %d", n)
	}
}

func TestAStaleSweepCannotEndOrParkTheNewConnectionsWatch(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	r.w.ProcessPacket(silentHelloFrom(12345))
	r.w.destState.watchMu.RLock()
	deadline := r.w.destState.watches[reusedTuple].deadline
	r.w.destState.watchMu.RUnlock()
	due := r.w.destState.DueSilent(deadline.Add(time.Millisecond))
	if len(due) != 1 {
		t.Fatalf("the old connection's watch must be due once its deadline passed, got %d", len(due))
	}

	r.w.ProcessPacket(synWithISN(89999))
	r.w.ProcessPacket(clientPacketAcking(12345, 90000, silentServerAck+0x10000, 0x18, silentHello()))
	if r.w.destState.EndWatch(due[0]) {
		t.Fatal("a sweep result for the old connection must not end the new connection's watch")
	}
	r.w.destState.Park(due[0])
	r.w.destState.Unprobe(due[0])

	r.w.destState.watchMu.RLock()
	h, ok := r.w.destState.watches[reusedTuple]
	r.w.destState.watchMu.RUnlock()
	if !ok || h.helloAck != silentServerAck+0x10000 || h.parked || h.probing {
		t.Fatalf("the new connection's watch must stay as its own ClientHello left it: watched %v, %+v", ok, h)
	}
}

func TestARetransmittedFastOpenSynStillCountsItsHelloAgain(t *testing.T) {
	set := newPassiveSet()
	set.TCP.IPBlockDetect.Enabled = true
	w := newDualStackInjectingWorker(t, &set)
	syn := fastOpenSyn(IPv4, buildClientHello("i.ytimg.com", 16, 0xAB), 1000)

	w.ProcessPacket(syn)
	w.ProcessPacket(syn)
	if n := helloCount(w, reusedTuple); n != 2 {
		t.Fatalf("a fast-open SYN resent with its ClientHello belongs to the same connection and counts as a resend, got %d", n)
	}
}

func TestANewConnectionOnTheSameTupleHoldsAndWatchesItsSplitHello(t *testing.T) {
	holdHellosFor(t, time.Minute)
	for _, server := range []struct {
		name string
		ack  uint32
	}{
		{"its server starts ahead of the old one", silentServerAck + 0x10000},
		{"its server starts behind the old one", silentServerAck - 0x10000},
	} {
		t.Run(server.name, func(t *testing.T) {
			set := injectingSilenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			t.Cleanup(r.w.pendingHello.ReleaseHolds)
			q := &verdictLog{}
			full, seg1, seg2 := splitHello("i.ytimg.com")

			r.w.ProcessPacket(silentHelloFrom(12345))
			if _, ok := watchedAck(r.w, reusedTuple); !ok {
				t.Fatal("the first connection's ClientHello must be watched")
			}
			queueTo(r.w, q, 1, synWithISN(89999))
			if r.w.destState.Watching() {
				t.Error("a SYN that opens a new connection on the tuple must end the old connection's watch")
			}
			queueTo(r.w, q, 2, clientPacketAcking(12345, 90000, server.ack, 0x18, seg1))
			if got := q.list(); len(got) != 1 {
				t.Fatalf("the new connection's segment 1 got %v while it was handled, it must wait for the rest of the record", got[1:])
			}
			queueTo(r.w, q, 3, clientPacketAcking(12345, 90000+splitCut, server.ack, 0x18, seg2))
			wantVerdicts(t, q.list(), acceptOf(1), dropOf(3), dropOf(2))
			if ack, ok := watchedAck(r.w, reusedTuple); !ok || ack != server.ack {
				t.Fatalf("the new connection must be watched with the sequence number its own server sent: watched %v, ack %#x, want %#x", ok, ack, server.ack)
			}
			if n := helloCount(r.w, reusedTuple); n != 1 {
				t.Fatalf("the new connection's split ClientHello is its first, counted %d", n)
			}

			r.sweepLater()
			sent := r.sent()
			var replayed []sentSegment
			for _, s := range sentData(sent) {
				if s.seq >= 90000 {
					replayed = append(replayed, s)
				}
			}
			coversOnce(t, replayed, 90000, full)
			if ports := resetsTo(sent); !slices.Equal(ports, []uint16{12345}) {
				t.Fatalf("the waiting connection must get one reset, got resets to %v", ports)
			}
			if seqs := resetSeqs(sent); !slices.Equal(seqs, []uint32{server.ack}) {
				t.Fatalf("the reset must carry the sequence number the new connection expects, got %#x want %#x", seqs, server.ack)
			}
		})
	}
}
