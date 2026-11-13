package nfq

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
)

func silentOnceReleased(release <-chan struct{}) banProber {
	return func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return iphealth.TLSSilent
	}
}

func (r *silenceRig) awaitCheck(t *testing.T) {
	t.Helper()
	select {
	case <-r.probes:
	case <-time.After(5 * time.Second):
		t.Fatal("the neutral-name check never started, so this test proves nothing")
	}
}

func answeredFlow() flowAnswer {
	return flowAnswer{found: true, flow: ctnl.Flow{HasState: true, TCPState: ctnl.TCPEstablished, HasCounters: true, ReplyPackets: 4, ReplyBytes: 4200}}
}

func (r *silenceRig) answerOnlyTo(t *testing.T, sport uint16) {
	t.Helper()
	answered, err := ctnl.NewTuple(6, netip.AddrPortFrom(netip.MustParseAddr("10.0.0.1"), sport), netip.MustParseAddrPort("1.2.3.4:443"))
	if err != nil {
		t.Fatal(err)
	}
	r.answerLookups(t, func(tuple ctnl.Tuple) flowAnswer {
		if tuple == answered {
			return answeredFlow()
		}
		return silentFlow()
	})
}

func TestOneUnansweredConnectionDoesNotGetTheAddressChecked(t *testing.T) {
	set := silenceSet()
	r := newFreshSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.sweepLater()

	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("one connection whose ClientHello went unanswered must not get the address checked, probed %v", keys)
	}
	if r.w.bans.reg.Len() != 0 || len(resetsTo(r.sent())) != 0 {
		t.Fatal("the address is not banned and the waiting connection is left alone")
	}
	if !r.w.destState.Watching() {
		t.Fatal("the connection must stay watched, so a ban that lands on its address later still resets it")
	}

	before := r.lookups.Load()
	r.sweepLater()
	if keys := r.probed(); len(keys) != 0 || r.lookups.Load() != before {
		t.Fatalf("a connection that was counted waits for a verdict on its address, it is not looked up or counted again: probed %v, %d more lookups", keys, r.lookups.Load()-before)
	}
}

func TestTwoUnansweredConnectionsInARowBanTheAddress(t *testing.T) {
	set := silenceSet()
	r := newFreshSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.sweepLater()
	sendHello(t, r.w, 23456, engine.VerdictAccept)
	r.sweepLater()

	if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
		t.Fatalf("the second connection in a row whose ClientHello went unanswered must get the address checked once, probed %v", keys)
	}
	if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
		t.Fatal("a silent neutral-name check after two unanswered connections must ban the address")
	}
	if ban := r.w.bans.reg.List()[0]; ban.Host != "i.ytimg.com" || ban.Set != set.Name {
		t.Errorf("ban record = %+v, want the waiting connection's host and set", ban)
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345, 23456}) {
		t.Fatalf("both waiting connections must be reset once the ban is in place, the first one too, got resets to %v", ports)
	}
	if r.w.destState.Watching() {
		t.Error("settled connections must not stay watched")
	}
}

func TestAnAnsweredConnectionStartsTheCountOver(t *testing.T) {
	for name, answer := range map[string]func(*testing.T, *silenceRig){
		"the client acknowledged server bytes": func(t *testing.T, r *silenceRig) {
			sendHello(t, r.w, 23456, engine.VerdictAccept)
			r.w.ProcessPacket(clientPacketAcking(23456, afterHello(), silentServerAck+1400, 0x10, nil))
		},
		"conntrack shows server bytes at the deadline": func(t *testing.T, r *silenceRig) {
			r.answerOnlyTo(t, 23456)
			sendHello(t, r.w, 23456, engine.VerdictAccept)
			r.sweepLater()
		},
	} {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			r := newFreshSilenceWorker(t, &set, iphealth.TLSSilent)

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			r.sweepLater()
			answer(t, r)
			sendHello(t, r.w, 34567, engine.VerdictAccept)
			r.sweepLater()

			if keys := r.probed(); len(keys) != 0 {
				t.Fatalf("an answered connection between two unanswered ones means they were not in a row, probed %v", keys)
			}
			if r.w.bans.reg.Len() != 0 {
				t.Fatal("the address must not be banned")
			}

			sendHello(t, r.w, 45678, engine.VerdictAccept)
			r.sweepLater()
			if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
				t.Fatalf("two unanswered connections in a row after the answered one must get the address checked, probed %v", keys)
			}
		})
	}
}

func TestAnAnswerDuringTheNeutralCheckKeepsTheAddressFromABan(t *testing.T) {
	for name, answer := range map[string]func(*testing.T, *silenceRig){
		"the client moved on, seen in the queue": func(_ *testing.T, r *silenceRig) {
			r.w.ProcessPacket(clientFinishedFrom(23456))
		},
		"server bytes only conntrack shows": func(t *testing.T, r *silenceRig) {
			r.answerOnlyTo(t, 23456)
		},
	} {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			release := make(chan struct{})
			r := newFreshSilenceRig(t, newInjectingWorker(t, &set), silentOnceReleased(release))

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			sendHello(t, r.w, 23456, engine.VerdictAccept)
			r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
			r.awaitCheck(t)
			answer(t, r)
			close(release)
			r.w.bans.wg.Wait()

			if r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
				t.Fatal("a server that answered a waiting connection before the neutral-name check ended must not be banned")
			}
			if ports := resetsTo(r.sent()); len(ports) != 0 {
				t.Fatalf("without a ban no waiting connection is reset, got resets to %v", ports)
			}
			if r.w.destState.Watching() {
				t.Error("the verdict still settles the watches")
			}

			sendHello(t, r.w, 34567, engine.VerdictAccept)
			r.sweepLater()
			if keys := r.probed(); len(keys) != 0 {
				t.Fatalf("the answer starts the count over, so the next unanswered connection is the first again, probed %v", keys)
			}
		})
	}
}

func TestAConnectionTheClientGivesUpOnIsCountedOnce(t *testing.T) {
	set := silenceSet()
	set.TCP.IPBlockDetect.TimeoutMs = 1
	r := newFreshSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	time.Sleep(5 * time.Millisecond)
	r.sweepLater()
	r.w.ProcessPacket(clientPacket(12345, afterHello(), 0x11, nil))
	r.sweepLater()

	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("a connection counted while it waited must not count again when its client gives up, probed %v", keys)
	}
	if r.w.destState.Watching() {
		t.Error("a counted connection the client closed has nothing left to wait for")
	}
}

func TestTheCountStartsOverAfterTheRetestInterval(t *testing.T) {
	set := silenceSet()
	r := newFreshSilenceWorker(t, &set, iphealth.TLSSilent)
	r.w.bans.answerTTL.Store(int64(30 * time.Second))
	start := time.Now()

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.sweep(start.Add(10 * time.Second))
	sendHello(t, r.w, 23456, engine.VerdictAccept)
	r.sweep(start.Add(50 * time.Second))
	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("an unanswered connection older than the retest interval no longer counts, probed %v", keys)
	}

	sendHello(t, r.w, 34567, engine.VerdictAccept)
	r.sweep(start.Add(60 * time.Second))
	if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
		t.Fatalf("two unanswered connections within the retest interval must get the address checked, probed %v", keys)
	}
}

func TestAResendCheckStillBansOnTheNeutralNamesAlone(t *testing.T) {
	set := silenceSet()
	set.TCP.IPBlockDetect.RetransmitThreshold = 2
	release := make(chan struct{})
	r := newFreshSilenceRig(t, newInjectingWorker(t, &set), silentOnceReleased(release))

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	sendHello(t, r.w, 23456, engine.VerdictAccept)
	r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
	r.awaitCheck(t)
	sendHello(t, r.w, 34567, engine.VerdictAccept)
	sendHello(t, r.w, 34567, engine.VerdictDrop)
	r.w.ProcessPacket(clientFinishedFrom(23456))
	close(release)
	r.w.bans.wg.Wait()

	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("the resend trigger joins the check that is running, probed again: %v", keys)
	}
	if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
		t.Fatal("a ClientHello resent past the retransmit threshold needs no second connection: its check bans on the neutral names alone")
	}
}

func TestSilentCountsAreBounded(t *testing.T) {
	b := newTestIPBans(t, nil)
	now := time.Now()
	for i := range banSilentMax {
		if got := b.countSilentHello(fmt.Sprintf("10.%d.%d.1:443", i/256, i%256), now); got != 1 {
			t.Fatalf("address %d: counted %d, want 1", i, got)
		}
	}
	if got := b.countSilentHello("9.9.9.9:443", now); got != 0 {
		t.Fatalf("with every slot holding a live count a new address is not counted, got %d", got)
	}
	if got := b.countSilentHello("9.9.9.9:443", now.Add(config.DefaultIPHealthRetestSec*time.Second)); got != 1 {
		t.Fatalf("counts older than the retest interval make room, got %d", got)
	}
}

func TestACountBeingCheckedLastsUntilTheCheckEnds(t *testing.T) {
	b := newTestIPBans(t, nil)
	b.answerTTL.Store(int64(time.Millisecond))
	ip, key := net.ParseIP("1.2.3.4"), "1.2.3.4:443"
	now := time.Now()
	b.countSilentHello(key, now)
	b.countSilentHello(key, now)
	b.mu.Lock()
	b.inflight[key] = true
	b.mu.Unlock()

	time.Sleep(5 * time.Millisecond)
	b.tick(nil)
	if !b.stillSilent(key, ip, 443) {
		t.Fatal("a count whose neutral-name check is running must not expire under it")
	}

	b.release(key)
	b.tick(nil)
	if b.stillSilent(key, ip, 443) {
		t.Fatal("once the check is over an expired count is dropped")
	}
}
