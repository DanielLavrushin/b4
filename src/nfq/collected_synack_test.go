package nfq

import (
	"encoding/binary"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/ctnl"
)

type synackFamily struct {
	enable func(r *collectCountRig)
	flow   func(t *testing.T, r *collectCountRig, sport uint16) *ctnl.Flow
	hello  func(t *testing.T, r *collectCountRig, sport uint16)
	synack func(sport uint16) []byte
	ack    func(sport uint16) []byte
}

func synackFamilies() map[string]synackFamily {
	v6flow := func(t *testing.T, r *collectCountRig, sport uint16) *ctnl.Flow {
		t.Helper()
		tuple, err := ctnl.NewTuple(6, netip.AddrPortFrom(netip.MustParseAddr("2001:db8::1"), sport), netip.MustParseAddrPort("[2001:db8::2]:443"))
		if err != nil {
			t.Fatal(err)
		}
		return r.flowOf(tuple)
	}
	return map[string]synackFamily{
		"IPv4": {
			enable: func(*collectCountRig) {},
			flow:   func(t *testing.T, r *collectCountRig, sport uint16) *ctnl.Flow { return r.flow(t, sport) },
			hello:  func(t *testing.T, r *collectCountRig, sport uint16) { r.hello(t, sport) },
			synack: func(sport uint16) []byte { return collectedReply(sport, silentHelloSeq, 0x12, nil) },
			ack:    func(sport uint16) []byte { return collectedReply(sport, afterHello(), 0x10, nil) },
		},
		"IPv6": {
			enable: func(r *collectCountRig) { r.w.getConfig().Queue.IPv6Enabled = true },
			flow:   v6flow,
			hello: func(t *testing.T, r *collectCountRig, sport uint16) {
				r.w.ProcessPacket(collectedV6Hello(sport))
				if !r.watched(sport) {
					t.Fatalf("the IPv6 ClientHello from port %d is not watched, so this test proves nothing", sport)
				}
			},
			synack: func(sport uint16) []byte {
				pkt := collectedV6Reply(sport)
				binary.BigEndian.PutUint32(pkt[IPv6HeaderLen+8:IPv6HeaderLen+12], silentHelloSeq)
				pkt[IPv6HeaderLen+13] = 0x12
				return pkt
			},
			ack: collectedV6Reply,
		},
	}
}

func (f synackFamily) connection(t *testing.T, r *collectCountRig, sport uint16, retransmitted, conntrackCounts bool) {
	t.Helper()
	f.hello(t, r, sport)
	if retransmitted {
		synack := f.synack(sport)
		if conntrackCounts {
			flow := f.flow(t, r, sport)
			flow.ReplyPackets++
			flow.ReplyBytes += uint64(len(synack))
		}
		r.w.ProcessPacket(synack)
	}
	ack := f.ack(sport)
	flow := f.flow(t, r, sport)
	flow.ReplyPackets++
	flow.ReplyBytes += uint64(len(ack))
	r.w.ProcessPacket(ack)
}

func TestARetransmittedSynAckThatConntrackIgnoresLeavesTheConnectionSilent(t *testing.T) {
	for name, f := range synackFamilies() {
		t.Run(name, func(t *testing.T) {
			r := newCollectCountRig(t)
			f.enable(r)
			for i, sport := range []uint16{42000, 42001} {
				f.connection(t, r, sport, true, false)
				r.sweep(time.Duration(4+i) * time.Second)
			}
			if ports := resetsTo(r.sent()); r.collected() != 1 || !slices.Equal(ports, []uint16{42000, 42001}) {
				t.Errorf("%s: two silent connections, each with a retransmitted SYN-ACK that conntrack does not count, gave candidates=%d resets=%v; the queue must leave that SYN-ACK out of the counters it compares with conntrack, or such a connection never counts", name, r.collected(), ports)
			}
		})
	}
}

func TestARetransmittedSynAckThatConntrackCountsIsNoAnswer(t *testing.T) {
	for name, f := range synackFamilies() {
		t.Run(name, func(t *testing.T) {
			r := newCollectCountRig(t)
			f.enable(r)
			f.connection(t, r, 42000, false, false)
			r.sweep(4 * time.Second)
			f.connection(t, r, 42001, true, true)
			r.sweep(5 * time.Second)
			if r.collected() == 0 {
				f.connection(t, r, 42002, false, false)
				r.sweep(6 * time.Second)
			}
			if ports := resetsTo(r.sent()); r.collected() != 1 || !slices.Contains(ports, uint16(42000)) {
				t.Errorf("%s: a retransmitted SYN-ACK that conntrack counts started the count over (candidates=%d resets=%v); its bytes are within the header allowance for replies the queue did not count, so it is no answer", name, r.collected(), ports)
			}
		})
	}
}
