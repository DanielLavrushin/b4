package ctnl

import (
	"bytes"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func TestSelfTestSendsAGetForATupleThatCannotExist(t *testing.T) {
	k := useFakeKernel(t, answer(unix.ENOENT, 0))
	if !Available() {
		t.Fatalf("ENOENT for the impossible tuple means the kernel parsed it: want available, got %+v", CurrentStatus())
	}
	reqs := k.requests()
	if len(reqs) != 1 {
		t.Fatalf("the self-test sent %d requests, want 1", len(reqs))
	}
	m := reqs[0]
	if m.Header.Type != msgGet || m.Header.Flags&netlink.Dump != 0 {
		t.Fatalf("self-test header %+v, want a single IPCTNL_MSG_CT_GET", m.Header)
	}
	d := decodeRequest(t, m)
	if d.which != attrTupleOrig || d.family != unix.AF_INET || d.proto != unix.IPPROTO_UDP {
		t.Fatalf("self-test tuple %+v, want an IPv4 UDP CTA_TUPLE_ORIG", d)
	}
	if !bytes.Equal(d.src, []byte{127, 255, 255, 254}) || !bytes.Equal(d.dst, []byte{127, 255, 255, 253}) || d.sport != 1 || d.dport != 1 {
		t.Fatalf("self-test tuple %x:%d -> %x:%d, want 127.255.255.254:1 -> 127.255.255.253:1", d.src, d.sport, d.dst, d.dport)
	}
	if d.mark != flushGuardMark || d.markMask != flushGuardMask {
		t.Fatal("the self-test must run the same encoder as the delete, flush guard included")
	}
	want := Status{Tested: true, Available: true}
	if got := Probe(); got != want {
		t.Fatalf("Probe() = %+v, want %+v", got, want)
	}
}

func TestSelfTestIsDecidedOncePerProcess(t *testing.T) {
	k := useFakeKernel(t, answer(unix.ENOENT, 0))
	for i := 0; i < 3; i++ {
		if !Available() {
			t.Fatal("want available")
		}
	}
	Probe()
	if n := k.dialCount(); n != 1 {
		t.Fatalf("the self-test ran %d times, want once", n)
	}
}

func TestSelfTestAnythingButENOENTMeansUnavailable(t *testing.T) {
	for _, c := range []struct {
		name   string
		reply  unix.Errno
		reason string
	}{
		{"no conntrack netlink subsystem", unix.EINVAL, "invalid argument"},
		{"no CAP_NET_ADMIN", unix.EPERM, "operation not permitted"},
		{"operation not supported", unix.EOPNOTSUPP, "not supported"},
		{"an entry for the impossible tuple", 0, "cannot exist"},
	} {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLog(t)
			k := useFakeKernel(t, answer(c.reply, 0))
			if Available() {
				t.Fatal("want unavailable")
			}
			st := CurrentStatus()
			if !st.Tested || st.Available || !strings.Contains(st.Reason, c.reason) {
				t.Fatalf("status %+v, want tested, unavailable, reason naming %q", st, c.reason)
			}
			Available()
			Probe()
			if n := k.dialCount(); n != 1 {
				t.Fatalf("the failed self-test ran %d times, want once", n)
			}
			if n := strings.Count(logs.String(), "Conntrack netlink is unavailable"); n != 1 {
				t.Fatalf("logged the failure %d times, want one warning:\n%s", n, logs.String())
			}
		})
	}
}

func TestSelfTestWithoutANetlinkSocketMeansUnavailable(t *testing.T) {
	useFakeKernel(t, answer(unix.ENOENT, 0))
	dial = func() (*netlink.Conn, error) {
		return nil, errors.New("protocol not supported")
	}
	if Available() {
		t.Fatal("want unavailable when NETLINK_NETFILTER cannot be opened")
	}
	if st := CurrentStatus(); !strings.Contains(st.Reason, "protocol not supported") {
		t.Fatalf("reason %q, want the dial error", st.Reason)
	}
}

func TestRetestRunsTheSelfTestAgain(t *testing.T) {
	logs := captureLog(t)
	k := useFakeKernel(t, answer(unix.EINVAL, 0))
	if Available() {
		t.Fatal("want unavailable before the module is loaded")
	}
	Retest()
	if st := CurrentStatus(); st.Tested || st.Reason != "" {
		t.Fatalf("status after Retest %+v, want untested with no stale reason", st)
	}
	if Available() {
		t.Fatal("still failing: want unavailable")
	}
	if n := strings.Count(logs.String(), "Conntrack netlink is unavailable"); n != 1 {
		t.Fatalf("the same failure after a retest was warned %d times, want once", n)
	}
	k.setReply(answer(unix.ENOENT, 0))
	Retest()
	if !Available() {
		t.Fatal("after the module came back the self-test must pass")
	}
	if n := k.dialCount(); n != 3 {
		t.Fatalf("self-test ran %d times, want 3 (start and two retests)", n)
	}
	if !strings.Contains(logs.String(), "Conntrack netlink is available") {
		t.Fatalf("recovery after a warning must be logged:\n%s", logs.String())
	}
}

func TestSelfTestTupleIsComplete(t *testing.T) {
	if err := selfTestTuple.check(); err != nil {
		t.Fatal(err)
	}
	src := netip.MustParseAddrPort("127.255.255.254:1")
	dst := netip.MustParseAddrPort("127.255.255.253:1")
	if want := mustTuple(t, unix.IPPROTO_UDP, src, dst); selfTestTuple != want {
		t.Fatalf("self-test tuple %s, want %s", selfTestTuple, want)
	}
}
