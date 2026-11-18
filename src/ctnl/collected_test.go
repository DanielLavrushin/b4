package ctnl

import (
	"net/netip"
	"testing"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func TestCollectedReplyDestinationKeepsTranslatedAddress(t *testing.T) {
	for _, raw := range []string{"203.0.113.1:63000", "[2001:db8::1]:63000"} {
		dst := netip.MustParseAddrPort(raw)
		src := netip.MustParseAddrPort("203.0.113.2:443")
		if dst.Addr().Is6() {
			src = netip.MustParseAddrPort("[2001:db8::2]:443")
		}
		tuple := mustTuple(t, unix.IPPROTO_TCP, src, dst)
		ae := netlink.NewAttributeEncoder()
		encodeTuple(ae, attrTupleReply, tuple)
		data, err := ae.Encode()
		if err != nil {
			t.Fatal(err)
		}
		flow, err := parseFlow(data)
		if err != nil || flow.ReplyDestination != dst {
			t.Fatalf("translated reply %s lost: %+v %v", dst, flow, err)
		}
	}
}
