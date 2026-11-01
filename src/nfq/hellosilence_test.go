package nfq

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
)

const (
	silentServerAck = 0x0badf00d
	silentHelloSeq  = 1000
)

func silenceSet() config.SetConfig {
	set := newPassiveSet()
	set.TCP.RSTProtection.Enabled = false
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	return set
}

type flowAnswer struct {
	flow  ctnl.Flow
	found bool
	err   error
}

func silentFlow() flowAnswer {
	return flowAnswer{found: true, flow: ctnl.Flow{HasState: true, TCPState: ctnl.TCPEstablished, HasCounters: true, OrigPackets: 6, OrigBytes: 900, ReplyPackets: 2, ReplyBytes: 112}}
}

type askedFlow struct {
	tuple ctnl.Tuple
	dir   ctnl.Direction
}

type silenceRig struct {
	w       *Worker
	pool    *Pool
	probes  chan string
	sent    func() [][]byte
	lookups *atomic.Int32
	tuples  chan askedFlow
}

func (r *silenceRig) answerLookups(t *testing.T, answer func(ctnl.Tuple) flowAnswer) {
	t.Helper()
	orig := lookupFlow
	lookupFlow = func(tuple ctnl.Tuple, dir ctnl.Direction) (ctnl.Flow, bool, error) {
		r.lookups.Add(1)
		select {
		case r.tuples <- askedFlow{tuple, dir}:
		default:
		}
		a := answer(tuple)
		return a.flow, a.found, a.err
	}
	t.Cleanup(func() { lookupFlow = orig })
}

func newSilenceRig(t *testing.T, w *Worker, probe banProber) *silenceRig {
	t.Helper()
	r := &silenceRig{w: w, probes: make(chan string, 64), lookups: &atomic.Int32{}, tuples: make(chan askedFlow, 64)}
	w.bans = newTestIPBans(t, func(ctx context.Context, ip net.IP, port, mark int) iphealth.TLSResult {
		r.probes <- iphealth.BanKey(ip, port)
		return probe(ctx, ip, port, mark)
	})
	r.pool = &Pool{Workers: []*Worker{w}, state: &runtimeState{bans: w.bans, destState: w.destState}}
	r.pool.wireBans()
	r.sent = captureInjected(t, w)
	r.answerLookups(t, func(ctnl.Tuple) flowAnswer { return silentFlow() })
	return r
}

func answering(res iphealth.TLSResult) banProber {
	return func(context.Context, net.IP, int, int) iphealth.TLSResult { return res }
}

func newSilenceWorker(t *testing.T, set *config.SetConfig, res iphealth.TLSResult) *silenceRig {
	t.Helper()
	return newSilenceRig(t, newInjectingWorker(t, set), answering(res))
}

func (r *silenceRig) sweep(at time.Time) {
	r.pool.sweepSilentHellos(at)
	r.w.bans.wg.Wait()
}

func (r *silenceRig) sweepLater() {
	r.sweep(time.Now().Add(time.Minute))
}

func (r *silenceRig) probed() []string {
	var keys []string
	for {
		select {
		case key := <-r.probes:
			keys = append(keys, key)
		default:
			return keys
		}
	}
}

func clientPacketAcking(sport uint16, seq, ack uint32, flags byte, payload []byte) []byte {
	pkt := makeV4TCPPacketFromPort(payload, seq, sport, flags)
	binary.BigEndian.PutUint32(pkt[28:32], ack)
	return pkt
}

func clientPacket(sport uint16, seq uint32, flags byte, payload []byte) []byte {
	return clientPacketAcking(sport, seq, silentServerAck, flags, payload)
}

func silentHello() []byte {
	return buildClientHello("i.ytimg.com", 16, 0xAB)
}

func silentHelloFrom(sport uint16) []byte {
	return clientPacket(sport, silentHelloSeq, 0x18, silentHello())
}

func afterHello() uint32 {
	return silentHelloSeq + uint32(len(silentHello()))
}

func clientFinishedFrom(sport uint16) []byte {
	finished := []byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01, 0x17, 0x03, 0x03, 0x00, 0x02, 0xAA, 0xBB}
	return clientPacketAcking(sport, afterHello(), silentServerAck+2900, 0x18, finished)
}

func sendHello(t *testing.T, w *Worker, sport uint16, want engine.PacketVerdict) {
	t.Helper()
	if v := w.ProcessPacket(silentHelloFrom(sport)); v != want {
		t.Fatalf("the ClientHello from port %d: verdict %v, want %v", sport, v, want)
	}
}

func resetsTo(sent [][]byte) []uint16 {
	var ports []uint16
	for _, p := range sent {
		l3 := IPv6HeaderLen
		if p[0]>>4 == IPv4 {
			l3 = int(p[0]&0x0f) * 4
		}
		if len(p) > l3+13 && p[l3+13]&0x04 != 0 {
			ports = append(ports, binary.BigEndian.Uint16(p[l3+2:l3+4]))
		}
	}
	slices.Sort(ports)
	return ports
}

func TestAHelloTheClientGetsNoAnswerToBansTheAddress(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.sweep(time.Now())
	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("nothing is checked before timeout_ms has passed, probed %v", keys)
	}

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)
	r.sweepLater()

	if keys := r.probed(); !slices.Equal(keys, []string{"1.2.3.4:443"}) {
		t.Fatalf("a ClientHello the client never got past must be checked with neutral names once, probed %v", keys)
	}
	want, err := ctnl.NewTuple(6, netip.MustParseAddrPort("10.0.0.1:12345"), netip.MustParseAddrPort("1.2.3.4:443"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-r.tuples:
		if got.tuple != want || got.dir != ctnl.Original {
			t.Errorf("conntrack must be asked for the connection as the client opened it, before NAT: got %s (%s), want %s (original)", got.tuple, got.dir, want)
		}
	default:
		t.Error("an open connection must be confirmed with conntrack before the check")
	}
	if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
		t.Fatal("a silent neutral-name check must ban the address, although the client never resent its ClientHello")
	}
	if ban := r.w.bans.reg.List()[0]; ban.Host != "i.ytimg.com" || ban.Set != set.Name {
		t.Errorf("ban record = %+v, want the waiting connection's host and set", ban)
	}

	got := r.sent()
	if len(got) != 1 || !slices.Equal(resetsTo(got), []uint16{12345}) {
		t.Fatalf("the waiting connection must get one reset once the ban is in place, got %d packets", len(got))
	}
	rst := got[0]
	if sport := binary.BigEndian.Uint16(rst[20:22]); sport != 443 {
		t.Errorf("the reset must come from the server's port, got %d", sport)
	}
	if seq := binary.BigEndian.Uint32(rst[24:28]); seq != silentServerAck {
		t.Errorf("the reset must carry the sequence number the client expects from the server, got %#x want %#x", seq, silentServerAck)
	}
	if reasons := connectionReasons(ch); !slices.Contains(reasons, "ipblock-ban") {
		t.Errorf("the reset connection must be logged as ipblock-ban, got %v", reasons)
	}
	if r.w.destState.Watching() {
		t.Error("a settled connection must not stay watched")
	}
}

func TestTheSilentHelloTriggerWorksOnASetThatInjects(t *testing.T) {
	set := newFragmentingYTImagesSet()
	set.TCP.RSTProtection.Enabled = false
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictDrop)
	if n := len(resetsTo(r.sent())); n != 0 {
		t.Fatalf("the ClientHello is fragmented, not reset, got %d resets", n)
	}
	r.sweepLater()

	if keys := r.probed(); len(keys) != 1 {
		t.Fatalf("server replies of a connection b4 injects into skip the queue, so the check must not depend on them, probed %v", keys)
	}
	if !slices.Equal(resetsTo(r.sent()), []uint16{12345}) {
		t.Fatal("the waiting connection must be reset once the ban is in place")
	}
}

func TestOneNeutralCheckSettlesEveryWaitingConnectionToTheAddress(t *testing.T) {
	set := silenceSet()
	release := make(chan struct{})
	r := newSilenceRig(t, newInjectingWorker(t, &set), func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return iphealth.TLSSilent
	})

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	sendHello(t, r.w, 23456, engine.VerdictAccept)
	r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
	close(release)
	r.w.bans.wg.Wait()

	if keys := r.probed(); len(keys) != 1 {
		t.Fatalf("two waiting connections to one address share one check, probed %v", keys)
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345, 23456}) {
		t.Fatalf("both waiting connections must be reset when the ban lands, got resets to %v", ports)
	}
}

func TestAHelloTheClientMovesPastIsNeverChecked(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.w.ProcessPacket(clientFinishedFrom(12345))
	r.sweepLater()

	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("a client that sent data after its ClientHello got the server's answer, so nothing is checked, probed %v", keys)
	}
	if len(resetsTo(r.sent())) != 0 || r.w.bans.reg.Len() != 0 || r.w.destState.Watching() {
		t.Fatal("an answered connection is left alone and no longer watched")
	}
}

func TestAClientAckOfServerDataEndsTheWatch(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.w.ProcessPacket(clientPacketAcking(12345, afterHello(), silentServerAck+1400, 0x10, nil))
	r.sweepLater()

	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("a client that acknowledged server bytes got an answer, so nothing is checked, probed %v", keys)
	}
	if r.w.destState.Watching() {
		t.Error("the watch must end with the client's acknowledgement")
	}
}

func TestEarlyDataAfterTheHelloIsNotAnAnswer(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.w.ProcessPacket(clientPacket(12345, afterHello(), 0x18, []byte{0x17, 0x03, 0x03, 0x00, 0x02, 0xAA, 0xBB}))
	r.sweepLater()

	if keys := r.probed(); len(keys) != 1 {
		t.Fatalf("data the client sends before any server byte arrives says nothing about the server, so the address is checked, probed %v", keys)
	}
}

func TestTheRestOfAHelloInTwoSegmentsIsNotAnAnswer(t *testing.T) {
	hello := silentHello()
	cut := len(hello) - 7
	for _, finished := range []bool{false, true} {
		t.Run("finished="+strconv.FormatBool(finished), func(t *testing.T) {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)

			r.w.ProcessPacket(clientPacket(12345, silentHelloSeq, 0x10, hello[:cut]))
			r.w.ProcessPacket(clientPacket(12345, silentHelloSeq+uint32(cut), 0x18, hello[cut:]))
			if !r.w.destState.Watching() {
				t.Fatal("a ClientHello in two segments must be watched")
			}
			if finished {
				r.w.ProcessPacket(clientFinishedFrom(12345))
			}
			r.sweepLater()

			if got := len(r.probed()); got != map[bool]int{false: 1, true: 0}[finished] {
				t.Fatalf("finished=%v: the second segment of the ClientHello must not read as the client moving on, probed %d times", finished, got)
			}
		})
	}
}

func TestAClientThatClosesEndsTheWatch(t *testing.T) {
	for name, flags := range map[string]byte{"FIN": 0x11, "RST": 0x04} {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			r.w.ProcessPacket(clientPacket(12345, afterHello(), flags, nil))
			r.sweepLater()

			if keys := r.probed(); len(keys) != 0 {
				t.Fatalf("a connection the client closed does not hang, so nothing is checked, probed %v", keys)
			}
		})
	}
}

func TestAnAddressThatAnsweredTheNeutralNamesIsNotCheckedAgain(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSReplied)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.sweepLater()
	if keys := r.probed(); len(keys) != 1 {
		t.Fatalf("want one neutral-name check, probed %v", keys)
	}
	if r.w.bans.reg.Len() != 0 || len(resetsTo(r.sent())) != 0 {
		t.Fatal("an address that answers neutral names is neither banned nor reset")
	}

	sendHello(t, r.w, 23456, engine.VerdictAccept)
	r.sweepLater()
	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("an address that answered a neutral-name check is not checked again within the retest interval, probed %v", keys)
	}
	if r.w.destState.Watching() {
		t.Error("the second connection's watch must be dropped")
	}
}

func TestASilentHelloToAnAlreadyBannedAddressIsResetWithoutACheck(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	banAddress(t, r.w.bans, "1.2.3.4", 443)
	r.sweepLater()

	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("a banned address needs no new check, probed %v", keys)
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("the connection still waiting on a banned address is reset once, got resets to %v", ports)
	}
}

func TestAWaitingConnectionIsResetOnlyByWhoeverTakesItsWatch(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	due := r.w.destState.DueSilent(time.Now().Add(time.Minute))
	if len(due) != 1 {
		t.Fatalf("want one due watch, got %d", len(due))
	}
	r.w.settleSilentHellos(net.ParseIP("1.2.3.4"), 443, true)
	if r.w.destState.Unwatch(due[0].key) {
		t.Fatal("a watch the verdict already took must not be claimed again by the sweep")
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("the connection is reset once, got resets to %v", ports)
	}
}

func TestAClientThatMovesOnDuringTheCheckIsNotReset(t *testing.T) {
	set := silenceSet()
	release := make(chan struct{})
	r := newSilenceRig(t, newInjectingWorker(t, &set), func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return iphealth.TLSSilent
	})

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
	r.w.ProcessPacket(clientFinishedFrom(12345))
	close(release)
	r.w.bans.wg.Wait()

	if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
		t.Fatal("the silent check still bans the address")
	}
	if ports := resetsTo(r.sent()); len(ports) != 0 {
		t.Fatalf("a connection that got its answer while the check ran must not be reset, got resets to %v", ports)
	}
}

func TestARefusedCheckIsRetriedOnTheNextSweep(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.w.bans.mu.Lock()
	for i := 0; i < banPendingMax; i++ {
		r.w.bans.inflight["busy-"+strconv.Itoa(i)] = struct{}{}
	}
	r.w.bans.mu.Unlock()
	r.sweepLater()
	if keys := r.probed(); len(keys) != 0 {
		t.Fatalf("with every probe slot taken nothing can start, probed %v", keys)
	}

	r.w.bans.mu.Lock()
	clear(r.w.bans.inflight)
	r.w.bans.mu.Unlock()
	r.sweepLater()
	if keys := r.probed(); len(keys) != 1 {
		t.Fatalf("a check that could not start must be tried again on the next sweep, probed %v", keys)
	}
	if !slices.Equal(resetsTo(r.sent()), []uint16{12345}) {
		t.Fatal("the retried check bans the address and resets the waiting connection")
	}
}

func TestOnlySetsThatBanWatchForSilentServers(t *testing.T) {
	cases := map[string]func(*config.SetConfig, *Worker){
		"without the blocked-address cache": func(s *config.SetConfig, _ *Worker) {
			s.TCP.IPBlockDetect.CacheBlockedIPs = false
		},
		"without IP block detection": func(s *config.SetConfig, _ *Worker) {
			s.TCP.IPBlockDetect.Enabled = false
		},
		"with bans switched off": func(_ *config.SetConfig, w *Worker) {
			w.bans.enabled.Store(false)
		},
		"in a discovery pool": func(_ *config.SetConfig, w *Worker) {
			w.getConfig().Queue.IsDiscovery = true
		},
		"routed through a proxy": func(s *config.SetConfig, _ *Worker) {
			s.Routing.Enabled = true
			s.Routing.Mode = config.RoutingModeProxy
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			mutate(&set, r.w)

			r.w.ProcessPacket(silentHelloFrom(12345))
			if r.w.destState.Watching() {
				t.Fatal("the connection must not be watched")
			}
			r.sweepLater()
			if keys := r.probed(); len(keys) != 0 {
				t.Fatalf("nothing may be checked, probed %v", keys)
			}
		})
	}
}

func TestAWatchIsDroppedWhenItsSetStopsBanning(t *testing.T) {
	cases := map[string]func(*config.SetConfig, *Worker){
		"the set drops the blocked-address cache": func(s *config.SetConfig, _ *Worker) {
			s.TCP.IPBlockDetect.CacheBlockedIPs = false
		},
		"the set is turned off": func(s *config.SetConfig, _ *Worker) {
			s.Enabled = false
		},
		"bans are switched off": func(_ *config.SetConfig, w *Worker) {
			w.bans.enabled.Store(false)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			mutate(&set, r.w)
			r.sweepLater()

			if keys := r.probed(); len(keys) != 0 {
				t.Fatalf("a watch whose set no longer bans is not checked, probed %v", keys)
			}
			if len(resetsTo(r.sent())) != 0 || r.w.destState.Watching() {
				t.Fatal("the watch is dropped without a reset")
			}
		})
	}
}

func TestATFOClientHelloIsNotWatched(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	r.w.ProcessPacket(makeV4TCPPacketFromPort(silentHello(), silentHelloSeq, 12345, 0x02))
	if r.w.destState.Watching() {
		t.Fatal("a ClientHello carried by a SYN has no server sequence number to reset with, so it must not be watched")
	}
}

func TestTheResendTriggerEndsTheWatch(t *testing.T) {
	set := silenceSet()
	set.TCP.IPBlockDetect.RetransmitThreshold = 2
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	sendHello(t, r.w, 12345, engine.VerdictDrop)
	r.w.bans.wg.Wait()
	if r.w.destState.Watching() {
		t.Fatal("a connection the resend trigger already reset must not stay watched")
	}
	r.sweepLater()
	if keys := r.probed(); len(keys) != 1 {
		t.Fatalf("only the resend trigger checks the address, probed %v", keys)
	}
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("the connection is reset once, by the resend trigger, got resets to %v", ports)
	}
}

func TestTheSilentHelloTriggerCoversIPv6(t *testing.T) {
	set := banningAddressSet()
	r := newSilenceRig(t, newDualStackInjectingWorker(t, &set), answering(iphealth.TLSSilent))

	hello := makeV6TCPPacket(silentHello(), silentHelloSeq)
	binary.BigEndian.PutUint32(hello[IPv6HeaderLen+8:IPv6HeaderLen+12], silentServerAck)
	if v := r.w.ProcessPacket(hello); v != engine.VerdictAccept {
		t.Fatalf("the IPv6 ClientHello passes, got %v", v)
	}
	r.sweepLater()

	if keys := r.probed(); !slices.Equal(keys, []string{"[2001:db8::2]:443"}) {
		t.Fatalf("the IPv6 address must be checked, probed %v", keys)
	}
	got := r.sent()
	if len(got) != 1 || got[0][0]>>4 != IPv6 || !slices.Equal(resetsTo(got), []uint16{12345}) {
		t.Fatalf("the waiting IPv6 connection must get one IPv6 reset, got %d packets", len(got))
	}
	if seq := binary.BigEndian.Uint32(got[0][IPv6HeaderLen+4 : IPv6HeaderLen+8]); seq != silentServerAck {
		t.Errorf("the IPv6 reset must carry the server sequence number the client expects, got %#x", seq)
	}
}

func TestTheVerdictTakesOnlyTheWatchesOfItsAddressBeingChecked(t *testing.T) {
	tracker := &destStateTracker{}
	add := func(key, ip string, port uint16, probing, parked bool) {
		tracker.WatchHello(&helloWatch{key: key, dst: net.ParseIP(ip), port: port, deadline: time.Now(), probing: probing, parked: parked})
	}
	add("checked", "1.2.3.4", 443, true, false)
	add("parked", "1.2.3.4", 443, false, true)
	add("other-port", "1.2.3.4", 8443, true, false)
	add("other-address", "1.2.3.5", 443, true, false)
	add("not-due", "1.2.3.4", 443, false, false)

	taken := tracker.TakeSilent(net.ParseIP("1.2.3.4"), 443)
	keys := []string{}
	for _, h := range taken {
		keys = append(keys, h.key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"checked", "parked"}) {
		t.Fatalf("a verdict takes the watches of its address and port that are checked or parked, got %v", keys)
	}
	if n := tracker.watching.Load(); n != 3 {
		t.Fatalf("the other watches stay, got %d left", n)
	}
}

func TestWatchesAreBoundedAndExpire(t *testing.T) {
	tracker := &destStateTracker{}
	base := time.Now()
	for i := 0; i < maxHelloWatches+10; i++ {
		tracker.WatchHello(&helloWatch{key: strconv.Itoa(i), dst: net.ParseIP("1.2.3.4"), port: 443, deadline: base})
	}
	if n := int(tracker.watching.Load()); n != maxHelloWatches {
		t.Fatalf("watches must be capped at %d, got %d", maxHelloWatches, n)
	}
	if due := tracker.DueSilent(base); len(due) != maxHelloWatches {
		t.Fatalf("every watch past its deadline is due once, got %d", len(due))
	}
	if due := tracker.DueSilent(base.Add(time.Second)); len(due) != 0 {
		t.Fatalf("a watch already being checked is not handed out again, got %d", len(due))
	}
	tracker.DueSilent(base.Add(helloWatchTTL + time.Second))
	if tracker.Watching() {
		t.Fatal("a watch whose check never settled must be dropped after its lifetime")
	}
}

func TestAnOpenConnectionIsCheckedOnlyWhenConntrackShowsNoServerBytes(t *testing.T) {
	cases := []struct {
		name    string
		answer  flowAnswer
		probed  bool
		watched bool
	}{
		{"no server bytes", silentFlow(), true, false},
		{"server bytes the queue never showed", flowAnswer{found: true, flow: ctnl.Flow{HasState: true, TCPState: ctnl.TCPEstablished, HasCounters: true, ReplyPackets: 4, ReplyBytes: 4200}}, false, false},
		{"the connection is gone", flowAnswer{}, false, false},
		{"no accounting", flowAnswer{found: true, flow: ctnl.Flow{HasState: true, TCPState: ctnl.TCPEstablished}}, false, true},
		{"conntrack unavailable", flowAnswer{err: errors.New("conntrack netlink is unavailable")}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			r.answerLookups(t, func(ctnl.Tuple) flowAnswer { return c.answer })

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			r.sweepLater()

			if got := len(r.probed()) == 1; got != c.probed {
				t.Fatalf("checked = %v, want %v", got, c.probed)
			}
			if got := r.w.destState.Watching(); got != c.watched {
				t.Fatalf("still watched = %v, want %v", got, c.watched)
			}
			if !c.watched {
				return
			}
			r.sweepLater()
			if n := r.lookups.Load(); n != 1 {
				t.Errorf("a connection conntrack cannot judge waits for the client to give up instead of being looked up again, %d lookups", n)
			}
			if len(r.probed()) != 0 || len(resetsTo(r.sent())) != 0 {
				t.Error("a connection conntrack cannot judge is neither checked nor reset")
			}
		})
	}
}

func TestAClientThatGivesUpAfterTheDeadlineGetsTheAddressChecked(t *testing.T) {
	for _, conntrack := range []string{"available", "unavailable"} {
		t.Run("conntrack "+conntrack, func(t *testing.T) {
			set := silenceSet()
			set.TCP.IPBlockDetect.TimeoutMs = 1
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			if conntrack == "unavailable" {
				r.answerLookups(t, func(ctnl.Tuple) flowAnswer { return flowAnswer{err: errors.New("conntrack netlink is unavailable")} })
			}

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			if conntrack == "unavailable" {
				r.sweepLater()
				if len(r.probed()) != 0 || !r.w.destState.Watching() {
					t.Fatal("without conntrack the open connection must wait for the client")
				}
			}
			time.Sleep(5 * time.Millisecond)
			r.w.ProcessPacket(clientPacket(12345, afterHello(), 0x11, nil))
			before := r.lookups.Load()
			r.sweepLater()

			if keys := r.probed(); len(keys) != 1 {
				t.Fatalf("a client that gave up after the deadline without a server byte is the silence itself, probed %v", keys)
			}
			if !r.w.bans.reg.Banned(net.ParseIP("1.2.3.4"), 443) {
				t.Fatal("the silent check bans the address")
			}
			if r.lookups.Load() != before {
				t.Error("a connection the client closed needs no conntrack lookup")
			}
			if ports := resetsTo(r.sent()); len(ports) != 0 {
				t.Fatalf("a connection the client already closed is not reset, got resets to %v", ports)
			}
		})
	}
}

func TestAConnectionConntrackNoLongerHoldsOpenIsNotChecked(t *testing.T) {
	for name, state := range map[string]uint8{"closed by a reset": 8, "half closed": 4} {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			r.answerLookups(t, func(ctnl.Tuple) flowAnswer {
				a := silentFlow()
				a.flow.TCPState = state
				return a
			})

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			r.sweepLater()

			if keys := r.probed(); len(keys) != 0 {
				t.Fatalf("a connection that a reset or a close already ended does not hang, so nothing is checked, probed %v", keys)
			}
			if len(resetsTo(r.sent())) != 0 || r.w.destState.Watching() {
				t.Fatal("the watch is dropped without a reset")
			}
		})
	}
}

func TestAParkedConnectionIsSettledByItsAddressVerdict(t *testing.T) {
	for _, res := range []iphealth.TLSResult{iphealth.TLSSilent, iphealth.TLSReplied} {
		t.Run(res.String(), func(t *testing.T) {
			set := silenceSet()
			set.TCP.IPBlockDetect.RetransmitThreshold = 2
			r := newSilenceWorker(t, &set, res)
			r.answerLookups(t, func(ctnl.Tuple) flowAnswer { return flowAnswer{err: errors.New("conntrack netlink is unavailable")} })

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			r.sweepLater()
			if !r.w.destState.Watching() || len(r.probed()) != 0 {
				t.Fatal("without conntrack the open connection is parked")
			}

			sendHello(t, r.w, 23456, engine.VerdictAccept)
			sendHello(t, r.w, 23456, engine.VerdictDrop)
			r.w.bans.wg.Wait()

			want := []uint16{23456}
			if res == iphealth.TLSSilent {
				want = []uint16{12345, 23456}
			}
			if ports := resetsTo(r.sent()); !slices.Equal(ports, want) {
				t.Fatalf("the verdict on the address must settle the parked connection too: resets to %v, want %v", ports, want)
			}
			if r.w.destState.Watching() {
				t.Error("the parked watch must be settled by the verdict")
			}
		})
	}
}

func TestLookupsPerSweepAreCapped(t *testing.T) {
	set := silenceSet()
	release := make(chan struct{})
	r := newSilenceRig(t, newInjectingWorker(t, &set), func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return iphealth.TLSSilent
	})

	conns := maxSilenceLookups + 4
	for i := 0; i < conns; i++ {
		sendHello(t, r.w, uint16(20000+i), engine.VerdictAccept)
	}
	r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
	if n := r.lookups.Load(); n != maxSilenceLookups {
		t.Fatalf("one sweep asks conntrack at most %d times, asked %d", maxSilenceLookups, n)
	}
	r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
	if n := r.lookups.Load(); int(n) != conns {
		t.Fatalf("the rest are looked up on the next sweep, %d lookups in all, want %d", n, conns)
	}
	close(release)
	r.w.bans.wg.Wait()
	if n := len(resetsTo(r.sent())); n != conns {
		t.Fatalf("every waiting connection is reset once the ban lands, got %d resets", n)
	}
}

func TestALookupFailureParksTheRestOfTheSweep(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)
	r.answerLookups(t, func(ctnl.Tuple) flowAnswer { return flowAnswer{err: errors.New("netlink receive: i/o timeout")} })

	for i := 0; i < 3; i++ {
		sendHello(t, r.w, uint16(30000+i), engine.VerdictAccept)
	}
	r.sweepLater()
	if n := r.lookups.Load(); n != 1 {
		t.Fatalf("after a failed lookup the sweep stops asking conntrack, %d lookups", n)
	}
	r.sweepLater()
	if n := r.lookups.Load(); n != 1 || len(r.probed()) != 0 || !r.w.destState.Watching() {
		t.Fatalf("parked connections wait for their clients to give up: %d lookups", n)
	}
}

func TestACachedResetEndsTheWatch(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	r.w.destState.AddBlocked("1.2.3.4:443")
	sendHello(t, r.w, 12345, engine.VerdictDrop)
	if r.w.destState.Watching() {
		t.Fatal("a ClientHello reset from the blocked-address cache must end the connection's watch")
	}
	r.sweepLater()
	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("the connection is reset once, from the cache, got resets to %v", ports)
	}
}

func TestARetestVerdictSettlesConnectionsWaitingOnTheAddress(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	if due := r.w.destState.DueSilent(time.Now().Add(time.Minute)); len(due) != 1 {
		t.Fatalf("want one due watch, got %d", len(due))
	}
	banAddress(t, r.w.bans, "1.2.3.4", 443)
	r.w.bans.settleRetest("1.2.3.4:443", iphealth.TLSSilent)

	if ports := resetsTo(r.sent()); !slices.Equal(ports, []uint16{12345}) {
		t.Fatalf("a re-test that keeps the ban settles the connection waiting on it, got resets to %v", ports)
	}
	if r.w.destState.Watching() {
		t.Error("the settled watch must be gone")
	}
}

func TestASetThatStopsBanningDuringTheCheckIsNotReset(t *testing.T) {
	cases := map[string]func(*config.SetConfig, *Worker){
		"the set drops the blocked-address cache": func(s *config.SetConfig, _ *Worker) {
			s.TCP.IPBlockDetect.CacheBlockedIPs = false
		},
		"the set is turned off": func(s *config.SetConfig, _ *Worker) {
			s.Enabled = false
		},
		"the set is routed through a proxy": func(s *config.SetConfig, _ *Worker) {
			s.Routing.Enabled = true
			s.Routing.Mode = config.RoutingModeProxy
		},
		"the set is deleted": func(_ *config.SetConfig, w *Worker) {
			next := *w.getConfig()
			next.Sets = nil
			w.cfg.Store(&next)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			set := silenceSet()
			release := make(chan struct{})
			r := newSilenceRig(t, newInjectingWorker(t, &set), func(ctx context.Context, _ net.IP, _, _ int) iphealth.TLSResult {
				select {
				case <-release:
				case <-ctx.Done():
				}
				return iphealth.TLSSilent
			})

			sendHello(t, r.w, 12345, engine.VerdictAccept)
			r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
			mutate(&set, r.w)
			close(release)
			r.w.bans.wg.Wait()

			if ports := resetsTo(r.sent()); len(ports) != 0 {
				t.Fatalf("a set that stopped banning while the check ran must not have its connections reset, got resets to %v", ports)
			}
			if r.w.destState.Watching() {
				t.Error("the verdict still settles the watch")
			}
		})
	}
}

func TestARetestAfterBansAreSwitchedOffResetsNothing(t *testing.T) {
	set := silenceSet()
	r := newSilenceWorker(t, &set, iphealth.TLSSilent)

	sendHello(t, r.w, 12345, engine.VerdictAccept)
	if due := r.w.destState.DueSilent(time.Now().Add(time.Minute)); len(due) != 1 {
		t.Fatalf("want one due watch, got %d", len(due))
	}
	banAddress(t, r.w.bans, "1.2.3.4", 443)
	r.w.bans.enabled.Store(false)
	r.w.bans.settleRetest("1.2.3.4:443", iphealth.TLSSilent)

	if ports := resetsTo(r.sent()); len(ports) != 0 {
		t.Fatalf("with bans switched off a re-test must not reset anything, got resets to %v", ports)
	}
}

func tunHelloFrom(sport uint16) []byte {
	pkt := clientPacket(sport, silentHelloSeq, 0x18, silentHello())
	copy(pkt[12:16], net.ParseIP(tunTestWAN).To4())
	copy(pkt[16:20], net.ParseIP("104.18.8.37").To4())
	return pkt
}

func TestATunHelloIsLookedUpByTheReplyTupleOnTheWire(t *testing.T) {
	cases := map[string]string{
		"port kept by SNAT":     "ipv4 2 tcp 6 431999 ESTABLISHED src=192.168.1.50 dst=104.18.8.37 sport=44092 dport=443 src=104.18.8.37 dst=" + tunTestWAN + " sport=443 dport=44092 [ASSURED] use=1\n",
		"port remapped by SNAT": "ipv4 2 tcp 6 431999 ESTABLISHED src=192.168.1.50 dst=104.18.8.37 sport=49874 dport=443 src=104.18.8.37 dst=" + tunTestWAN + " sport=443 dport=44092 [ASSURED] use=1\n",
		"no source attribution": "",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			thisHost(t)
			set := silenceSet()
			w := newInjectingWorker(t, &set)
			w.getConfig().Queue.Mode = "tun"
			if line != "" {
				w.srcResolver = newResolverWith(t, tunTestWAN, line)
			}
			r := newSilenceRig(t, w, answering(iphealth.TLSSilent))

			r.w.ProcessPacket(tunHelloFrom(44092))
			if !r.w.destState.Watching() {
				t.Fatal("the TUN connection must be watched")
			}
			r.sweepLater()

			want, err := ctnl.NewTuple(6, netip.MustParseAddrPort("104.18.8.37:443"), netip.MustParseAddrPort(tunTestWAN+":44092"))
			if err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-r.tuples:
				if got.tuple != want || got.dir != ctnl.Reply {
					t.Fatalf("in TUN mode conntrack must be asked by the reply tuple on the wire, after SNAT: got %s (%s), want %s (reply)", got.tuple, got.dir, want)
				}
			default:
				t.Fatal("the open connection must be confirmed with conntrack")
			}
			if keys := r.probed(); !slices.Equal(keys, []string{"104.18.8.37:443"}) {
				t.Fatalf("the silent TUN connection is checked, probed %v", keys)
			}
		})
	}
}
