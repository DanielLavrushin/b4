package ctnl

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

var (
	clientV4 = netip.MustParseAddrPort("10.0.1.2:40000")
	serverV4 = netip.MustParseAddrPort("203.0.113.10:443")
	clientV6 = netip.MustParseAddrPort("[2001:db8::1]:40000")
	serverV6 = netip.MustParseAddrPort("[2001:db8:ffff::10]:443")
)

func mustTuple(t *testing.T, proto uint8, src, dst netip.AddrPort) Tuple {
	t.Helper()
	tuple, err := NewTuple(proto, src, dst)
	if err != nil {
		t.Fatalf("NewTuple(%d, %s, %s): %v", proto, src, dst, err)
	}
	return tuple
}

func TestNewTupleRefusesAnIncompleteTuple(t *testing.T) {
	cases := []struct {
		name     string
		proto    uint8
		src, dst netip.AddrPort
		want     error
	}{
		{"no protocol", 0, clientV4, serverV4, errTupleProto},
		{"a protocol without ports", unix.IPPROTO_ICMP, clientV4, serverV4, errTupleProto},
		{"no source address", unix.IPPROTO_UDP, netip.AddrPort{}, serverV4, errTupleAddr},
		{"no destination address", unix.IPPROTO_UDP, clientV4, netip.AddrPort{}, errTupleAddr},
		{"no source port", unix.IPPROTO_UDP, netip.AddrPortFrom(clientV4.Addr(), 0), serverV4, errTuplePort},
		{"no destination port", unix.IPPROTO_TCP, clientV4, netip.AddrPortFrom(serverV4.Addr(), 0), errTuplePort},
		{"an IPv4 source with an IPv6 destination", unix.IPPROTO_UDP, clientV4, serverV6, errTupleFamily},
		{"an IPv6 source with an IPv4 destination", unix.IPPROTO_UDP, clientV6, serverV4, errTupleFamily},
		{"IPv4-mapped IPv6 addresses", unix.IPPROTO_UDP, netip.MustParseAddrPort("[::ffff:10.0.1.2]:40000"), netip.MustParseAddrPort("[::ffff:203.0.113.10]:443"), errTupleMapped},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NewTuple(c.proto, c.src, c.dst)
			if !errors.Is(err, c.want) {
				t.Fatalf("NewTuple returned error %v, want %v", err, c.want)
			}
			if got != (Tuple{}) {
				t.Fatalf("NewTuple returned %v next to its error, want the zero tuple", got)
			}
		})
	}
}

func TestNewTupleTakesTCPAndUDPOfEitherFamily(t *testing.T) {
	for _, c := range []struct {
		proto    uint8
		src, dst netip.AddrPort
		family   uint8
	}{
		{unix.IPPROTO_UDP, clientV4, serverV4, unix.AF_INET},
		{unix.IPPROTO_TCP, clientV4, serverV4, unix.AF_INET},
		{unix.IPPROTO_UDP, clientV6, serverV6, unix.AF_INET6},
		{unix.IPPROTO_TCP, clientV6, serverV6, unix.AF_INET6},
	} {
		tuple := mustTuple(t, c.proto, c.src, c.dst)
		if tuple.family() != c.family {
			t.Errorf("%s: family %d, want %d", tuple, tuple.family(), c.family)
		}
	}
}

func TestNewTupleDropsTheZoneOfALinkLocalAddress(t *testing.T) {
	src := netip.AddrPortFrom(netip.MustParseAddr("fe80::1%eth0"), 40000)
	dst := netip.AddrPortFrom(netip.MustParseAddr("fe80::2%eth0"), 443)
	tuple := mustTuple(t, unix.IPPROTO_UDP, src, dst)
	if tuple.src.Addr().Zone() != "" || tuple.dst.Addr().Zone() != "" {
		t.Fatalf("the zone carries no meaning for conntrack and must not reach the encoder, got %s", tuple)
	}
}

func TestATupleCanOnlyComeFromNewTuple(t *testing.T) {
	typ := reflect.TypeOf(Tuple{})
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); f.IsExported() {
			t.Errorf("Tuple field %s is exported, so a caller could build a tuple NewTuple never checked", f.Name)
		}
	}
	if err := (Tuple{}).check(); err == nil {
		t.Fatal("the zero tuple passes the check, so it would encode without addresses")
	}
}

func TestReverseSwapsTheEndpoints(t *testing.T) {
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	r := tuple.Reverse()
	if r.src != serverV4 || r.dst != clientV4 || r.proto != unix.IPPROTO_UDP {
		t.Fatalf("Reverse() = %s, want udp %s -> %s", r, serverV4, clientV4)
	}
	if r.Reverse() != tuple {
		t.Fatalf("reversing twice gives %s, want %s", r.Reverse(), tuple)
	}
	if (Tuple{}).Reverse().check() == nil {
		t.Fatal("the reverse of the zero tuple must stay incomplete")
	}
}

func TestDirectionNamesOnlyTheTwoTupleAttributes(t *testing.T) {
	if !Original.valid() || !Reply.valid() {
		t.Fatal("Original and Reply must be valid directions")
	}
	if uint8(Original) != attrTupleOrig || uint8(Reply) != attrTupleReply {
		t.Fatalf("Original = %d, Reply = %d, want CTA_TUPLE_ORIG (1) and CTA_TUPLE_REPLY (2)", Original, Reply)
	}
	for _, d := range []Direction{0, 3, 14, 255} {
		if d.valid() {
			t.Errorf("direction %d must not be valid", d)
		}
	}
}
