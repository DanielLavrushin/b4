package ctnl

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"testing"

	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nltest"
	"golang.org/x/sys/unix"
)

func resetLookup(t *testing.T) {
	t.Helper()
	closeLookup := func() {
		lookupMu.Lock()
		if lookupConn != nil {
			_ = lookupConn.Close()
			lookupConn = nil
		}
		lookupMu.Unlock()
	}
	closeLookup()
	t.Cleanup(closeLookup)
}

func encodeFlow(t *testing.T, f Flow, withTuple Tuple) []byte {
	t.Helper()
	ae := netlink.NewAttributeEncoder()
	ae.ByteOrder = binary.BigEndian
	encodeTuple(ae, attrTupleOrig, withTuple)
	if f.HasState {
		ae.Nested(attrProtoInfo, func(info *netlink.AttributeEncoder) error {
			info.Nested(attrProtoInfoTCP, func(tcp *netlink.AttributeEncoder) error {
				tcp.Uint8(attrProtoInfoTCPState, f.TCPState)
				return nil
			})
			return nil
		})
	}
	if f.HasCounters {
		for _, c := range []struct {
			typ            uint16
			packets, bytes uint64
		}{{attrCountersOrig, f.OrigPackets, f.OrigBytes}, {attrCountersReply, f.ReplyPackets, f.ReplyBytes}} {
			ae.Nested(c.typ, func(n *netlink.AttributeEncoder) error {
				n.ByteOrder = binary.BigEndian
				n.Uint64(attrCountersPackets, c.packets)
				n.Uint64(attrCountersBytes, c.bytes)
				return nil
			})
		}
	}
	b, err := ae.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func lookupTuple(t *testing.T) Tuple {
	return mustTuple(t, unix.IPPROTO_TCP, netip.MustParseAddrPort("192.168.1.205:37842"), netip.MustParseAddrPort("173.194.187.103:443"))
}

func TestParseFlowReadsTheCountersAndTheTCPState(t *testing.T) {
	want := Flow{TCPState: TCPEstablished, HasState: true, OrigPackets: 7, OrigBytes: 1200, ReplyPackets: 2, ReplyBytes: 112, HasCounters: true}
	got, err := parseFlow(encodeFlow(t, want, lookupTuple(t)))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("parseFlow = %+v, want %+v", got, want)
	}
}

func TestParseFlowWithoutAccountingHasNoCounters(t *testing.T) {
	got, err := parseFlow(encodeFlow(t, Flow{TCPState: TCPEstablished, HasState: true}, lookupTuple(t)))
	if err != nil {
		t.Fatal(err)
	}
	if got.HasCounters || got.ReplyBytes != 0 || !got.HasState {
		t.Fatalf("an entry without counter attributes must report no counters, got %+v", got)
	}
}

func useLookupKernel(t *testing.T, reply func(m netlink.Message) ([]netlink.Message, error)) *fakeKernel {
	t.Helper()
	k := &fakeKernel{}
	resetState()
	resetLookup(t)
	probeState.Store(stateAvailable)
	orig := dial
	dial = func() (*netlink.Conn, error) {
		k.mu.Lock()
		k.dials++
		k.mu.Unlock()
		return nltest.Dial(func(reqs []netlink.Message) ([]netlink.Message, error) {
			k.mu.Lock()
			k.sent = append(k.sent, reqs...)
			k.mu.Unlock()
			return reply(reqs[0])
		}), nil
	}
	t.Cleanup(func() {
		dial = orig
		resetState()
	})
	return k
}

func TestLookupReturnsTheEntryAndReportsAMissingOne(t *testing.T) {
	tuple := lookupTuple(t)
	entry := Flow{TCPState: TCPEstablished, HasState: true, OrigPackets: 5, OrigBytes: 900, ReplyPackets: 2, ReplyBytes: 112, HasCounters: true}
	missing := false
	k := useLookupKernel(t, func(m netlink.Message) ([]netlink.Message, error) {
		if missing {
			return nltest.Error(int(unix.ENOENT), []netlink.Message{m})
		}
		return []netlink.Message{{
			Header: netlink.Header{Type: netlink.HeaderType(unix.NFNL_SUBSYS_CTNETLINK << 8), Sequence: m.Header.Sequence, PID: m.Header.PID},
			Data:   append([]byte{unix.AF_INET, unix.NFNETLINK_V0, 0, 0}, encodeFlow(t, entry, tuple)...),
		}}, nil
	})

	got, found, err := Lookup(tuple, Original)
	if err != nil || !found || got != entry {
		t.Fatalf("Lookup = %+v, %v, %v; want %+v, true, nil", got, found, err, entry)
	}
	req := k.requests()[0]
	if req.Header.Type != netlink.HeaderType(msgGet) || req.Header.Flags != netlink.Request|netlink.Acknowledge {
		t.Errorf("the lookup must be a GET asking for an acknowledgement, which ends the kernel's multipart reply, got type %#x flags %v", req.Header.Type, req.Header.Flags)
	}
	tupleAttrs := func(req netlink.Message) []uint16 {
		t.Helper()
		ad, err := netlink.NewAttributeDecoder(req.Data[4:])
		if err != nil {
			t.Fatal(err)
		}
		var types []uint16
		for ad.Next() {
			types = append(types, ad.Type())
		}
		return types
	}
	if types := tupleAttrs(req); len(types) != 1 || types[0] != attrTupleOrig {
		t.Errorf("an original lookup carries only the original tuple, got attributes %v", types)
	}
	if _, found, err := Lookup(tuple.Reverse(), Reply); err != nil || !found {
		t.Fatalf("a reply lookup: found %v, err %v", found, err)
	}
	if types := tupleAttrs(k.requests()[1]); len(types) != 1 || types[0] != attrTupleReply {
		t.Errorf("a reply lookup carries only the reply tuple, got attributes %v", types)
	}
	if _, _, err := Lookup(tuple, Direction(9)); !errors.Is(err, errTupleDirection) {
		t.Errorf("an unknown direction is refused, got %v", err)
	}
	if n := len(k.requests()); n != 2 {
		t.Errorf("a refused direction never reaches the kernel, %d requests", n)
	}

	missing = true
	if _, found, err := Lookup(tuple, Original); err != nil || found {
		t.Fatalf("an entry the kernel does not have: found %v, err %v; want false, nil", found, err)
	}
	if n := k.dialCount(); n != 1 {
		t.Errorf("lookups share one connection while it works, dialled %d times", n)
	}
}

func TestLookupIsRefusedWhileConntrackIsUnavailable(t *testing.T) {
	k := useLookupKernel(t, func(m netlink.Message) ([]netlink.Message, error) {
		t.Error("no request may reach the kernel")
		return nil, nil
	})
	probeState.Store(stateUnavailable)
	if _, found, err := Lookup(lookupTuple(t), Original); !errors.Is(err, errUnavailable) || found {
		t.Fatalf("Lookup while unavailable: found %v, err %v", found, err)
	}
	if k.dialCount() != 0 {
		t.Error("an unavailable conntrack must not be dialled")
	}
}

func TestLookupRedialsAfterAFailure(t *testing.T) {
	fail := true
	k := useLookupKernel(t, func(m netlink.Message) ([]netlink.Message, error) {
		if fail {
			return nltest.Error(int(unix.EPERM), []netlink.Message{m})
		}
		return nltest.Error(int(unix.ENOENT), []netlink.Message{m})
	})
	if _, _, err := Lookup(lookupTuple(t), Original); err == nil {
		t.Fatal("a refused lookup must report its error")
	}
	fail = false
	if _, found, err := Lookup(lookupTuple(t), Original); err != nil || found {
		t.Fatalf("the next lookup: found %v, err %v", found, err)
	}
	if n := k.dialCount(); n != 2 {
		t.Errorf("a failed connection is replaced, dialled %d times, want 2", n)
	}
}
