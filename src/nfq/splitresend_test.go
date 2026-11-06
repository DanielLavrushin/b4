package nfq

import (
	"bytes"
	"encoding/binary"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
)

const splitResendHost = "i.ytimg.com"

type helloSegment struct {
	seq  uint32
	data []byte
}

func helloSegments(full []byte, seq uint32, cuts ...int) []helloSegment {
	var segs []helloSegment
	from := 0
	for _, cut := range append(cuts, len(full)) {
		segs = append(segs, helloSegment{seq: seq + uint32(from), data: full[from:cut]})
		from = cut
	}
	return segs
}

func (s helloSegment) from(sport uint16) []byte {
	return clientPacket(sport, s.seq, 0x18, s.data)
}

func nameInSegmentTwo() []helloSegment {
	return helloSegments(buildClientHello(splitResendHost, 1400, 0xAB), splitSeq, splitCut)
}

func sendSegments(w *Worker, sport uint16, segs []helloSegment) {
	for _, s := range segs {
		w.ProcessPacket(s.from(sport))
	}
}

func resend(t *testing.T, w *Worker, sport uint16, seg helloSegment, want engine.PacketVerdict) {
	t.Helper()
	if v := w.ProcessPacket(seg.from(sport)); v != want {
		t.Fatalf("the resent segment at %d from port %d: verdict %v, want %v", seg.seq, sport, v, want)
	}
}

func resendFloor(t *testing.T, d time.Duration) {
	t.Helper()
	prev := helloResendFloor
	helloResendFloor = d
	t.Cleanup(func() { helloResendFloor = prev })
}

func decoyHello(host string, pad int) []byte {
	decoy := buildClientHello(host, pad, 0x00)
	copy(decoy[5+4+2:], bytes.Repeat([]byte{0x77}, 32))
	return decoy
}

func decidedSet(t *testing.T, w *Worker) string {
	t.Helper()
	decision, ok := w.tlsCache.Decision(reusedTuple)
	if !ok {
		t.Fatal("the connection has no decision")
	}
	if decision.set == nil {
		return ""
	}
	return decision.set.Name
}

func TestResendsOfASplitHelloEscalateWhereverItsNameLies(t *testing.T) {
	resendFloor(t, 0)
	full := buildClientHello(splitResendHost, 1400, 0xAB)
	nameAt := bytes.Index(full, []byte(splitResendHost))
	for _, layout := range []struct {
		name string
		cuts []int
	}{
		{"the name in segment 2", []int{splitCut}},
		{"the name across the cut", []int{nameAt + 4}},
		{"the name in segment 3", []int{600, splitCut}},
	} {
		t.Run(layout.name, func(t *testing.T) {
			r := escalatingSilenceRig(t, plainBackupSet())
			segs := helloSegments(full, splitSeq, layout.cuts...)

			sendSegments(r.w, 12345, segs)
			if n := helloCount(r.w, reusedTuple); n != 1 {
				t.Fatalf("the first pass counts one ClientHello, got %d", n)
			}
			resend(t, r.w, 12345, segs[0], engine.VerdictAccept)
			if n := helloCount(r.w, reusedTuple); n != 2 {
				t.Fatalf("a resent segment 1 is a copy of the ClientHello, counted %d", n)
			}
			if reason := r.w.destState.GetEscalationReason(splitResendHost); reason != "" {
				t.Fatalf("two copies must not escalate at a threshold of three, got %q", reason)
			}
			resend(t, r.w, 12345, segs[0], engine.VerdictDrop)
			if r.w.destState.GetEscalationReason(splitResendHost) == "" {
				t.Fatal("the third copy of the ClientHello must escalate the host")
			}
			if n := helloCount(r.w, reusedTuple); n != 0 {
				t.Fatalf("an escalation starts the count over, got %d", n)
			}
		})
	}
}

func TestResendsOfASplitHelloTripTheResendCheck(t *testing.T) {
	resendFloor(t, 0)
	for _, tc := range []struct {
		name string
		set  func() config.SetConfig
	}{
		{"a set that leaves the hello alone", silenceSet},
		{"a set that injects", injectingSilenceSet},
	} {
		for _, trigger := range []struct {
			name   string
			mutate func(*config.SetConfig)
			copies int
			wait   time.Duration
		}{
			{"at the resend threshold", func(*config.SetConfig) {}, 2, 0},
			{"past timeout_ms", func(s *config.SetConfig) {
				s.TCP.IPBlockDetect.RetransmitThreshold = 100
				s.TCP.IPBlockDetect.TimeoutMs = 1
			}, 1, 5 * time.Millisecond},
		} {
			t.Run(tc.name+", "+trigger.name, func(t *testing.T) {
				set := tc.set()
				trigger.mutate(&set)
				r := newSilenceWorker(t, &set, iphealth.TLSSilent)
				segs := nameInSegmentTwo()

				sendSegments(r.w, 12345, segs)
				time.Sleep(trigger.wait)
				for range trigger.copies - 1 {
					r.w.ProcessPacket(segs[0].from(12345))
					if ports := resetsTo(r.sent()); len(ports) != 0 {
						t.Fatalf("a copy below the threshold must not reset the client, got resets to %v", ports)
					}
				}
				resend(t, r.w, 12345, segs[0], engine.VerdictDrop)
				r.w.bans.wg.Wait()
				if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
					t.Fatalf("the resend check must reset the client once, got resets to %v", ports)
				}
				if !r.w.destState.IsBlocked("1.2.3.4:443") {
					t.Fatal("the resend check must cache the address as blocked")
				}
				if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
					t.Fatalf("the resend check must get the address checked once, probed %v", keys)
				}

				resend(t, r.w, 12345, segs[0], engine.VerdictDrop)
				if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
					t.Fatalf("with the address cached as blocked, a further copy is refused and reset like a whole ClientHello, got resets to %v", ports)
				}
			})
		}
	}
}

func TestARoundOfResendsOfASplitHelloCountsOnce(t *testing.T) {
	resendFloor(t, 0)
	r := escalatingSilenceRig(t, plainBackupSet())
	segs := nameInSegmentTwo()

	sendSegments(r.w, 12345, segs)
	sendSegments(r.w, 12345, segs)
	if n := helloCount(r.w, reusedTuple); n != 2 {
		t.Fatalf("segments 1 and 2 resent together are one copy of the ClientHello, counted %d", n)
	}
	if reason := r.w.destState.GetEscalationReason(splitResendHost); reason != "" {
		t.Fatalf("one round of ordinary loss must not escalate the host, got %q", reason)
	}
	if ports := resetsTo(r.sent()); len(ports) != 0 {
		t.Fatalf("one round of ordinary loss must not reset the client, got resets to %v", ports)
	}
	if n := r.w.pendingHello.Len(); n != 0 {
		t.Fatalf("a resend of a named ClientHello must not wait for its rest again, %d pending", n)
	}

	resend(t, r.w, 12345, segs[1], engine.VerdictAccept)
	if n := helloCount(r.w, reusedTuple); n != 2 {
		t.Fatalf("a resent segment 2 without segment 1 is not a copy of the ClientHello, counted %d", n)
	}
	resend(t, r.w, 12345, segs[0], engine.VerdictDrop)
	if r.w.destState.GetEscalationReason(splitResendHost) == "" {
		t.Fatal("the next round must be the third copy and escalate the host")
	}
}

func TestAResendBeforeAnyRetransmitTimerIsNotAStall(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rig    func(*testing.T) *silenceRig
		copies int
		fired  func(*silenceRig) bool
	}{
		{"the resend check", func(t *testing.T) *silenceRig {
			set := silenceSet()
			set.TCP.IPBlockDetect.RetransmitThreshold = 2
			return newSilenceWorker(t, &set, iphealth.TLSSilent)
		}, 1, func(r *silenceRig) bool { return len(resetsTo(r.sent())) > 0 }},
		{"the stall escalation", func(t *testing.T) *silenceRig {
			set := newPassiveSet()
			set.TCP.RSTProtection.Enabled = false
			set.Escalate.To = "backup"
			set.Escalate.StallThreshold = 2
			backup := plainBackupSet()
			return newSilenceRig(t, newInjectingWorker(t, &set, &backup), answering(iphealth.TLSSilent))
		}, 1, func(r *silenceRig) bool { return r.w.destState.GetEscalationReason(splitResendHost) != "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resendFloor(t, 300*time.Millisecond)
			r := tc.rig(t)
			segs := nameInSegmentTwo()

			sendSegments(r.w, 12345, segs)
			resend(t, r.w, 12345, segs[0], engine.VerdictAccept)
			if n := helloCount(r.w, reusedTuple); n != 1 {
				t.Fatalf("a segment 1 back this soon came from a fast retransmit, so the server is answering, counted %d", n)
			}
			if tc.fired(r) {
				t.Fatal("a resend this soon must not fire at a threshold of two")
			}
			if got := decidedSet(t, r.w); got != "YT images" {
				t.Fatalf("a resend this soon still keeps the connection on its set, got %q", got)
			}

			time.Sleep(320 * time.Millisecond)
			resend(t, r.w, 12345, segs[0], engine.VerdictDrop)
			if !tc.fired(r) {
				t.Fatal("a resend past the floor is a copy and must fire at a threshold of two")
			}
		})
	}
}

func TestTheResendFloorStaysBetweenAFastRetransmitAndTheShortestRetransmitTimer(t *testing.T) {
	if helloResendFloor < 100*time.Millisecond || helloResendFloor >= 200*time.Millisecond {
		t.Fatalf("the floor is %s: below 100 ms the fast retransmit a set's own reordering provokes counts as a stall, at 200 ms a real timer's first resend can go uncounted", helloResendFloor)
	}
}

func TestAWholeHelloCopyInsideTheResendFloorStillCounts(t *testing.T) {
	resendFloor(t, time.Minute)
	r := escalatingSilenceRig(t, plainBackupSet())

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	sendHello(t, r.w, 12345, engine.VerdictAccept)
	if n := helloCount(r.w, reusedTuple); n != 2 {
		t.Fatalf("the floor is for a segment 1 the kernel resent, a whole copy keeps its count, got %d", n)
	}
}

func TestAClientHelloStartElsewhereOnTheConnectionIsNotAResend(t *testing.T) {
	resendFloor(t, 0)
	segs := nameInSegmentTwo()
	full := buildClientHello(splitResendHost, 1400, 0xAB)
	for _, tc := range []struct {
		name string
		seq  uint32
	}{
		{"after the hello", splitSeq + uint32(len(full))},
		{"inside the hello", splitSeq + 100},
		{"before the hello", splitSeq - 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := escalatingSilenceRig(t, plainBackupSet())
			sendSegments(r.w, 12345, segs)
			for range 4 {
				r.w.ProcessPacket(clientPacket(12345, tc.seq, 0x18, segs[0].data))
			}
			if n := helloCount(r.w, reusedTuple); n != 1 {
				t.Fatalf("a nameless ClientHello start at another sequence number is not a copy of the first, counted %d", n)
			}
			if reason := r.w.destState.GetEscalationReason(splitResendHost); reason != "" {
				t.Fatalf("it must not escalate the host, got %q", reason)
			}
		})
	}
}

func TestOnlyAClientHelloStartAtTheHelloSequenceIsACopy(t *testing.T) {
	resendFloor(t, 0)
	segs := nameInSegmentTwo()
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"application data", append([]byte{0x17, 0x03, 0x03, 0x00, 0x20}, make([]byte, 32)...)},
		{"a whole ClientHello without a name", buildClientHelloWithoutSNI()},
	} {
		t.Run(tc.name+", stall count", func(t *testing.T) {
			r := escalatingSilenceRig(t, plainBackupSet())
			sendSegments(r.w, 12345, segs)
			for range 4 {
				r.w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, tc.payload))
			}
			if reason := r.w.destState.GetEscalationReason(splitResendHost); reason != "" {
				t.Fatalf("a payload that is not a segment of a ClientHello must not escalate the host, got %q", reason)
			}
			if n := helloCount(r.w, reusedTuple); n != 1 {
				t.Fatalf("a payload that is not a segment of a ClientHello is not a copy of it, counted %d", n)
			}
		})
		t.Run(tc.name+", resend check", func(t *testing.T) {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			sendSegments(r.w, 12345, segs)
			for range 4 {
				r.w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, tc.payload))
			}
			r.w.bans.wg.Wait()
			if ports := resetsTo(r.sent()); len(ports) != 0 {
				t.Fatalf("it must not reset the client, got resets to %v", ports)
			}
			if r.w.destState.IsBlocked("1.2.3.4:443") {
				t.Fatal("it must not cache the address as blocked")
			}
		})
	}
}

func TestADecoyAtTheSameSequenceDoesNotShadowASplitHello(t *testing.T) {
	resendFloor(t, 0)
	for _, tc := range []struct {
		name  string
		decoy string
		pad   int
	}{
		{"a decoy that names no set", "www.iana.org", 16},
		{"a decoy that names another set", "www.listed.example", 16},
		{"a decoy as long as the real ClientHello", "www.iana.or", 1400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listed := newPassiveSet()
			listed.Id, listed.Name = "listed", "listed"
			listed.Targets.DomainsToMatch = []string{"www.listed.example"}
			listed.TCP.RSTProtection.Enabled = false
			real := newPassiveSet()
			real.TCP.RSTProtection.Enabled = false
			real.Escalate.To = "backup"
			backup := plainBackupSet()
			w := newInjectingWorker(t, &listed, &real, &backup)
			segs := nameInSegmentTwo()

			w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, decoyHello(tc.decoy, tc.pad)))
			sendSegments(w, 12345, segs)
			if host, _, _ := w.tlsCache.Lookup(reusedTuple); host != splitResendHost {
				t.Fatalf("the real ClientHello names %q, the connection is left on the decoy's %q", splitResendHost, host)
			}
			if got := decidedSet(t, w); got != real.Name {
				t.Fatalf("the real ClientHello must decide the set, got %q", got)
			}

			before := helloCount(w, reusedTuple)
			resend(t, w, 12345, segs[0], engine.VerdictAccept)
			if n := helloCount(w, reusedTuple); n != before+1 {
				t.Fatalf("a resend of the real ClientHello is a copy of it, count %d after %d", n, before)
			}
		})
	}
}

func TestASegmentOfARealHelloAfterItsDecoyIsNotADecoyCopy(t *testing.T) {
	resendFloor(t, 0)
	r := escalatingSilenceRig(t, plainBackupSet())
	segs := nameInSegmentTwo()

	r.w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, decoyHello(splitResendHost, 16)))
	n := helloCount(r.w, reusedTuple)
	r.w.ProcessPacket(segs[0].from(12345))
	if got := helloCount(r.w, reusedTuple); got != n {
		t.Fatalf("segment 1 of another ClientHello at the same sequence is not a copy of the decoy, count %d after %d", got, n)
	}
	if got := r.w.pendingHello.Len(); got != 1 {
		t.Fatalf("segment 1 of another ClientHello must wait for its rest, %d pending", got)
	}
}

func TestAWholeHelloTheKernelResendsInPiecesCounts(t *testing.T) {
	resendFloor(t, 0)
	r := escalatingSilenceRig(t, plainBackupSet())
	full := buildClientHello(splitResendHost, 1400, 0xAB)

	r.w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, full))
	r.w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, full[:splitCut]))
	if n := helloCount(r.w, reusedTuple); n != 2 {
		t.Fatalf("the same ClientHello resent in a shorter segment is a copy of it, counted %d", n)
	}
}

func TestASegmentOneTooShortToIdentifyFallsBackToThePendingCache(t *testing.T) {
	resendFloor(t, 0)
	r := escalatingSilenceRig(t, plainBackupSet())
	full := buildClientHello(splitResendHost, 1400, 0xAB)
	const cut = clientHelloRandomAt + 4

	r.w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, full[:cut]))
	r.w.ProcessPacket(clientPacket(12345, splitSeq+cut, 0x18, full[cut:]))
	if host, _, _ := r.w.tlsCache.Lookup(reusedTuple); host != splitResendHost {
		t.Fatalf("a short segment 1 still joins its rest, named %q", host)
	}
	r.w.ProcessPacket(clientPacket(12345, splitSeq, 0x18, full[:cut]))
	if n := helloCount(r.w, reusedTuple); n != 1 {
		t.Fatalf("a segment 1 that cannot be told from another ClientHello is not counted, got %d", n)
	}
}

func TestASegmentOneRepeatedBeforeTheNameIsKnownDoesNotCount(t *testing.T) {
	r := escalatingSilenceRig(t, plainBackupSet())
	segs := nameInSegmentTwo()

	r.w.ProcessPacket(segs[0].from(12345))
	r.w.ProcessPacket(segs[0].from(12345))
	r.w.ProcessPacket(segs[0].from(12345))
	if n := helloCount(r.w, reusedTuple); n != 0 {
		t.Fatalf("a slow client's segment 1 names nothing until segment 2 arrives, counted %d", n)
	}
	r.w.ProcessPacket(segs[1].from(12345))
	if n := helloCount(r.w, reusedTuple); n != 1 {
		t.Fatalf("segment 2 completes the first copy of the ClientHello, counted %d", n)
	}
}

func TestTheBackupSetCountsASplitHellosResendsFromOne(t *testing.T) {
	resendFloor(t, 0)
	backup := silenceSet()
	backup.Id, backup.Name = "backup", "backup"
	r := escalatingSilenceRig(t, backup)
	segs := nameInSegmentTwo()

	sendSegments(r.w, 12345, segs)
	resend(t, r.w, 12345, segs[0], engine.VerdictAccept)
	resend(t, r.w, 12345, segs[0], engine.VerdictDrop)

	resend(t, r.w, 12345, segs[0], engine.VerdictAccept)
	resend(t, r.w, 12345, segs[0], engine.VerdictAccept)
	r.w.bans.wg.Wait()
	if ports := resetsTo(r.sent()); len(ports) != 0 {
		t.Fatalf("the backup set must try its own strategy on the stuck connection before it resets it, got resets to %v", ports)
	}
	if r.w.destState.IsBlocked("1.2.3.4:443") {
		t.Fatal("the backup set must not cache the address as blocked for the first set's copies")
	}
	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("the backup set must not check the address before its own threshold, probed %v", keys)
	}

	resend(t, r.w, 12345, segs[0], engine.VerdictDrop)
	r.w.bans.wg.Wait()
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("the backup set's own third copy must reset the connection, got resets to %v", ports)
	}
	if !r.w.destState.IsBlocked("1.2.3.4:443") {
		t.Fatal("the backup set's own third copy must cache the address as blocked")
	}
}

func TestTheBackupSetChecksASilentServerOnASplitHelloItTookOver(t *testing.T) {
	resendFloor(t, 0)
	backup := silenceSet()
	backup.Id, backup.Name = "backup", "backup"
	r := escalatingSilenceRig(t, backup)
	segs := nameInSegmentTwo()

	sendSegments(r.w, 12345, segs)
	resend(t, r.w, 12345, segs[0], engine.VerdictAccept)
	resend(t, r.w, 12345, segs[0], engine.VerdictDrop)
	resend(t, r.w, 12345, segs[0], engine.VerdictAccept)
	r.sweepLater()

	if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
		t.Fatalf("the backup set must check a server that stays silent on the connection it took over, probed %v", keys)
	}
	if ban := r.w.bans.reg.List(); len(ban) != 1 || ban[0].Set != "backup" {
		t.Fatalf("the ban must come from the backup set's watch on that connection, got %+v", ban)
	}
	sent := r.sent()
	if ports := resetsTo(sent); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("the waiting connection must be reset once the address is banned, got resets to %v", ports)
	}
	if seqs := resetSeqs(sent); !slices.Equal(seqs, []uint32{silentServerAck}) {
		t.Fatalf("the reset must carry the sequence number the client expects, got %#x want %#x", seqs, silentServerAck)
	}
}

func TestANewConnectionOnTheSameTupleDoesNotInheritASplitHellosResends(t *testing.T) {
	resendFloor(t, 0)
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)
	old := nameInSegmentTwo()

	sendSegments(r.w, 12345, old)
	resend(t, r.w, 12345, old[0], engine.VerdictAccept)
	if n := helloCount(r.w, reusedTuple); n != 2 {
		t.Fatalf("the old connection's resend must count, got %d", n)
	}

	r.w.ProcessPacket(synWithISN(89999))
	if n := helloCount(r.w, reusedTuple); n != 0 {
		t.Fatalf("a new connection on the tuple must forget the old connection's count, got %d", n)
	}
	fresh := helloSegments(buildClientHello(splitResendHost, 1400, 0xAB), 90000, splitCut)
	r.w.ProcessPacket(fresh[0].from(12345))
	if n := helloCount(r.w, reusedTuple); n != 0 {
		t.Fatalf("the new connection's segment 1 is at another sequence number and names nothing yet, counted %d", n)
	}
	r.w.ProcessPacket(fresh[1].from(12345))
	if n := helloCount(r.w, reusedTuple); n != 1 {
		t.Fatalf("the new connection's split ClientHello is its first, counted %d", n)
	}
	resend(t, r.w, 12345, fresh[0], engine.VerdictAccept)
	if n := helloCount(r.w, reusedTuple); n != 2 {
		t.Fatalf("the new connection's own resend must count, got %d", n)
	}
	r.w.bans.wg.Wait()
	if ports := resetsTo(r.sent()); len(ports) != 0 {
		t.Fatalf("no connection reached the threshold, got resets to %v", ports)
	}
}

func TestAResentSplitHelloStaysOnTheSetItsNameChose(t *testing.T) {
	resendFloor(t, 0)
	named := newPassiveSet()
	named.TCP.RSTProtection.Enabled = false
	named.Escalate.To = "backup"
	backup := plainBackupSet()
	wrong := plainBackupSet()
	wrong.Id, wrong.Name = "wrong", "wrong"
	learned := newHintSet()
	learned.Id, learned.Name = "learned", "learned"
	learned.TCP.RSTProtection.Enabled = false
	learned.Targets.DomainsToMatch = []string{"other.example"}
	learned.Escalate.To = wrong.Id
	w := newInjectingWorker(t, &named, &learned, &backup, &wrong)
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "www.other.example", &learned)
	segs := nameInSegmentTwo()

	sendSegments(w, 12345, segs)
	resend(t, w, 12345, segs[0], engine.VerdictAccept)
	resend(t, w, 12345, segs[0], engine.VerdictDrop)
	if id, _, ok := w.destState.GetEscalation(splitResendHost); !ok || id != backup.Id {
		t.Fatalf("copies of the ClientHello must escalate from the set its name chose, not the set that learned the address: on %q, recorded %v", id, ok)
	}
}

func TestAResendOfASplitHelloThatNamesNoSetDropsTheHostHint(t *testing.T) {
	resendFloor(t, 0)
	hinted := newHintSet()
	hinted.Id, hinted.Name = "hinted", "hinted"
	hinted.TCP.RSTProtection.Enabled = false
	hinted.Targets.DomainsToMatch = []string{"hinted.example"}
	hinted.Escalate.To = "backup"
	backup := plainBackupSet()
	w := newInjectingWorker(t, &hinted, &backup)
	w.hostHints.Store("10.0.0.1", "1.2.3.4", hinted.Id, "www.hinted.example")
	const unlisted = "www.unlisted.example"
	segs := helloSegments(buildClientHello(unlisted, 1400, 0xAB), splitSeq, splitCut)

	sendSegments(w, 12345, segs)
	for range 5 {
		resend(t, w, 12345, segs[0], engine.VerdictAccept)
	}
	if n := helloCount(w, reusedTuple); n != 0 {
		t.Fatalf("a set hinted for the address does not take a name it does not list, counted %d", n)
	}
	if id, _, ok := w.destState.GetEscalation(unlisted); ok {
		t.Fatalf("a name no set lists must not escalate, got %q", id)
	}
}

func TestAResentSplitHelloToABannedAddressIsResetLikeAWholeOne(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)
	segs := nameInSegmentTwo()

	sendSegments(r.w, 12345, segs)
	banAddress(t, r.w.bans, "1.2.3.4", 443)
	resend(t, r.w, 12345, segs[0], engine.VerdictDrop)

	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("a copy of the ClientHello to a banned address must reset the client once, got resets to %v", ports)
	}
	if r.w.destState.Watching() {
		t.Error("a connection refused for a ban must not stay watched")
	}
	if keys := r.probed(); len(keys) != 0 {
		t.Errorf("an address already banned is not checked again, probed %v", keys)
	}
}

func TestResendsOfASplitHelloEscalateOverIPv6(t *testing.T) {
	resendFloor(t, 0)
	set := newPassiveSet()
	set.TCP.RSTProtection.Enabled = false
	set.Escalate.To = "backup"
	backup := plainBackupSet()
	w := newDualStackInjectingWorker(t, &set, &backup)
	segs := nameInSegmentTwo()
	packet := func(s helloSegment) []byte {
		raw := makeV6TCPPacket(s.data, s.seq)
		binary.BigEndian.PutUint32(raw[IPv6HeaderLen+8:IPv6HeaderLen+12], silentServerAck)
		return raw
	}

	w.ProcessPacket(packet(segs[0]))
	w.ProcessPacket(packet(segs[1]))
	if v := w.ProcessPacket(packet(segs[0])); v != engine.VerdictAccept {
		t.Fatalf("the second copy of the ClientHello: verdict %v", v)
	}
	if v := w.ProcessPacket(packet(segs[0])); v != engine.VerdictDrop {
		t.Fatalf("the third copy of the ClientHello must escalate and be dropped, verdict %v", v)
	}
	if w.destState.GetEscalationReason(splitResendHost) == "" {
		t.Fatal("the third copy of the ClientHello must escalate the host")
	}
}

func TestResendsOfAHeldSplitHelloAreCountedAndNotHeldAgain(t *testing.T) {
	resendFloor(t, 0)
	holdHellosFor(t, time.Minute)
	r := escalatingSilenceRig(t, plainBackupSet())
	t.Cleanup(r.w.pendingHello.ReleaseHolds)
	q := &verdictLog{}
	segs := nameInSegmentTwo()

	queueTo(r.w, q, 1, segs[0].from(12345))
	if got := q.list(); len(got) != 0 {
		t.Fatalf("segment 1 got %v while it was handled, it must wait for the rest of the record", got)
	}
	queueTo(r.w, q, 2, segs[1].from(12345))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2))

	queueTo(r.w, q, 3, segs[0].from(12345))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2), acceptOf(3))
	queueTo(r.w, q, 4, segs[0].from(12345))
	wantVerdicts(t, q.list(), acceptOf(1), acceptOf(2), acceptOf(3), dropOf(4))
	if r.w.destState.GetEscalationReason(splitResendHost) == "" {
		t.Fatal("the third copy of a held split ClientHello must escalate the host")
	}
}

func TestASplitHelloStartingOnAFastOpenSynKeepsItsNameAndAnchor(t *testing.T) {
	resendFloor(t, 0)
	full := buildClientHello(splitResendHost, 1400, 0xAB)
	const isn = uint32(5000)
	dataStart := isn + 1

	t.Run("the server took the SYN's data and segment 2 follows", func(t *testing.T) {
		r := escalatingSilenceRig(t, plainBackupSet())
		r.w.ProcessPacket(fastOpenSyn(IPv4, full[:splitCut], isn))
		r.w.ProcessPacket(clientPacket(12345, dataStart+splitCut, 0x18, full[splitCut:]))
		if host, _, _ := r.w.tlsCache.Lookup(reusedTuple); host != splitResendHost {
			t.Fatalf("segment 2 continues the SYN's data, named %q", host)
		}
		if n := helloCount(r.w, reusedTuple); n != 1 {
			t.Fatalf("the first pass counts one ClientHello, got %d", n)
		}
	})

	t.Run("the server refused the SYN's data and the client resends it", func(t *testing.T) {
		r := escalatingSilenceRig(t, plainBackupSet())
		nameAt := bytes.Index(full, []byte(splitResendHost))
		cut := nameAt + 4
		r.w.ProcessPacket(fastOpenSyn(IPv4, full[:cut], isn))
		r.w.ProcessPacket(clientPacket(12345, dataStart, 0x18, full[:cut]))
		r.w.ProcessPacket(clientPacket(12345, dataStart+uint32(cut), 0x18, full[cut:]))
		if host, _, _ := r.w.tlsCache.Lookup(reusedTuple); host != splitResendHost {
			t.Fatalf("the join must not repeat a byte of the name, named %q", host)
		}
		if n := helloCount(r.w, reusedTuple); n != 1 {
			t.Fatalf("the first pass counts one ClientHello, got %d", n)
		}
		resend(t, r.w, 12345, helloSegment{seq: dataStart, data: full[:cut]}, engine.VerdictAccept)
		if n := helloCount(r.w, reusedTuple); n != 2 {
			t.Fatalf("a resend at the start of the data is a copy of the ClientHello, counted %d", n)
		}
	})
}

func TestATruncatedHelloOnARetransmittedFastOpenSynCountsLikeAWholeOne(t *testing.T) {
	resendFloor(t, 0)
	set := newPassiveSet()
	set.TCP.IPBlockDetect.Enabled = true
	w := newDualStackInjectingWorker(t, &set)
	full := buildClientHello(splitResendHost, 1400, 0xAB)

	w.ProcessPacket(fastOpenSyn(IPv4, full, splitSeq))
	if n := helloCount(w, reusedTuple); n != 1 {
		t.Fatalf("a fast-open SYN with a whole ClientHello counts once, got %d", n)
	}
	w.ProcessPacket(fastOpenSyn(IPv4, full[:splitCut], splitSeq))
	if n := helloCount(w, reusedTuple); n != 2 {
		t.Fatalf("a fast-open SYN resent with only the start of the ClientHello is a copy of it, counted %d", n)
	}
}
