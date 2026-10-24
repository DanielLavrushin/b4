package ctnl

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	netnsMsgNew      = unix.NFNL_SUBSYS_CTNETLINK << 8
	netnsAttrTimeout = 7
)

func netnsRequire(t *testing.T) {
	t.Helper()
	if os.Getenv("B4_NETNS_TEST") != "1" {
		t.Skip("set B4_NETNS_TEST=1 and run inside a network namespace (make test-netns)")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root inside the namespace")
	}
	self, err1 := os.Readlink("/proc/self/ns/net")
	init, err2 := os.Readlink("/proc/1/ns/net")
	switch {
	case err1 == nil && err2 == nil:
		if self == init {
			t.Fatal("refusing to run: this is the host network namespace, and the test deletes and flushes conntrack entries")
		}
	default:
		if up := netnsInterfacesUp(t); len(up) > 0 {
			t.Fatalf("refusing to run: the network namespace could not be compared with the host's (%v; %v) and has the interfaces %v up, so it may be the host's, and the test deletes and flushes conntrack entries; run it in a fresh namespace (make test-netns)", err1, err2, up)
		}
	}
	resetState()
	t.Cleanup(resetState)
}

func netnsInterfacesUp(t *testing.T) []string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("refusing to run: the network interfaces could not be listed to check the namespace: %v", err)
	}
	var up []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback == 0 && ifc.Flags&net.FlagUp != 0 {
			up = append(up, ifc.Name)
		}
	}
	return up
}

func netnsCreate(t *testing.T, orig, reply Tuple) {
	t.Helper()
	ae := netlink.NewAttributeEncoder()
	encodeTuple(ae, attrTupleOrig, orig)
	encodeTuple(ae, attrTupleReply, reply)
	ae.Bytes(netnsAttrTimeout, binary.BigEndian.AppendUint32(nil, 300))
	attrs, err := ae.Encode()
	if err != nil {
		t.Fatal(err)
	}
	m := netlink.Message{
		Header: netlink.Header{Type: netnsMsgNew, Flags: netlink.Request | netlink.Acknowledge | netlink.Create | netlink.Excl},
		Data:   append([]byte{orig.family(), unix.NFNETLINK_V0, 0, 0}, attrs...),
	}
	if err := exchange(m); err != nil {
		t.Fatalf("creating the conntrack entry %s: %v", orig, err)
	}
}

func netnsExists(t *testing.T, tuple Tuple) bool {
	t.Helper()
	m, err := request(msgGet, tuple, Original)
	if err != nil {
		t.Fatal(err)
	}
	err = exchange(m)
	switch {
	case err == nil:
		return true
	case errors.Is(err, unix.ENOENT):
		return false
	}
	t.Fatalf("looking up %s: %v", tuple, err)
	return false
}

func netnsCount(t *testing.T) int {
	t.Helper()
	c, err := dial()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	msgs, err := c.Execute(netlink.Message{
		Header: netlink.Header{Type: msgGet, Flags: netlink.Request | netlink.Dump},
		Data:   []byte{unix.AF_UNSPEC, unix.NFNETLINK_V0, 0, 0},
	})
	if err != nil {
		t.Fatalf("dumping the conntrack table: %v", err)
	}
	return len(msgs)
}

type netnsEntry struct {
	name  string
	orig  Tuple
	reply Tuple
}

func netnsEntries(t *testing.T) (deleteByOrig, deleteByReply, deleteV6ByReply netnsEntry, unrelated []netnsEntry) {
	udp := func(src, dst string) Tuple {
		return mustTuple(t, unix.IPPROTO_UDP, netip.MustParseAddrPort(src), netip.MustParseAddrPort(dst))
	}
	deleteByOrig = netnsEntry{"masqueraded flow deleted by its original tuple", udp("10.9.0.2:40000", "203.0.113.10:443"), udp("203.0.113.10:443", "198.51.100.1:40000")}
	deleteByReply = netnsEntry{"port-remapped flow deleted by its reply tuple", udp("10.9.0.2:40001", "203.0.113.10:443"), udp("203.0.113.10:443", "198.51.100.1:50042")}
	deleteV6ByReply = netnsEntry{"IPv6 flow deleted by its reply tuple", udp("[2001:db8::2]:40003", "[2001:db8:ffff::10]:443"), udp("[2001:db8:ffff::10]:443", "[2001:db8::2]:40003")}
	unrelated = []netnsEntry{
		{"same client, other server", udp("10.9.0.2:40002", "203.0.113.11:443"), udp("203.0.113.11:443", "198.51.100.1:40002")},
		{"same server, other client", udp("10.9.0.3:40000", "203.0.113.10:443"), udp("203.0.113.10:443", "198.51.100.1:40004")},
		{"IPv6 neighbour", udp("[2001:db8::3]:40003", "[2001:db8:ffff::10]:443"), udp("[2001:db8:ffff::10]:443", "[2001:db8::3]:40003")},
	}
	return
}

func TestNetnsDeleteByOriginalAndByReplyTuple(t *testing.T) {
	netnsRequire(t)
	byOrig, byReply, v6ByReply, unrelated := netnsEntries(t)
	all := append([]netnsEntry{byOrig, byReply, v6ByReply}, unrelated...)
	for _, e := range all {
		netnsCreate(t, e.orig, e.reply)
	}
	before := netnsCount(t)
	if before < len(all) {
		t.Fatalf("the table holds %d entries after creating %d", before, len(all))
	}

	w := NewWorker()
	defer w.Stop()
	for _, job := range []struct {
		tuple Tuple
		dir   Direction
	}{{byOrig.orig, Original}, {byReply.reply, Reply}, {v6ByReply.reply, Reply}} {
		if !w.Enqueue(job.tuple, job.dir) {
			t.Fatalf("Enqueue(%s, %s) refused (status %+v)", job.tuple, job.dir, CurrentStatus())
		}
	}
	waitFor(t, "three deletes", func() bool { return Stats().Deleted == 3 })
	if !Available() {
		t.Fatalf("the self-test failed on this kernel: %+v", CurrentStatus())
	}

	for _, e := range []netnsEntry{byOrig, byReply, v6ByReply} {
		if netnsExists(t, e.orig) {
			t.Errorf("%s: still in the table", e.name)
		}
	}
	for _, e := range unrelated {
		if !netnsExists(t, e.orig) {
			t.Errorf("%s: deleted although no request named it", e.name)
		}
	}
	if after := netnsCount(t); after != before-3 {
		t.Fatalf("the table went from %d to %d entries, want exactly the three named ones removed", before, after)
	}

	if !w.Enqueue(byOrig.orig, Original) {
		t.Fatal("a second delete of the same flow was refused")
	}
	waitFor(t, "the delete of an entry already gone", func() bool { return Stats().NotFound == 1 })
}

func TestNetnsATuplelessDeleteWithTheGuardRemovesNothing(t *testing.T) {
	netnsRequire(t)
	udp := func(src, dst string) Tuple {
		return mustTuple(t, unix.IPPROTO_UDP, netip.MustParseAddrPort(src), netip.MustParseAddrPort(dst))
	}
	for _, e := range []netnsEntry{
		{"IPv4 flow", udp("10.9.1.2:41000", "203.0.113.20:443"), udp("203.0.113.20:443", "198.51.100.1:41000")},
		{"IPv6 flow", udp("[2001:db8:1::2]:41001", "[2001:db8:ffff::20]:443"), udp("[2001:db8:ffff::20]:443", "[2001:db8:1::2]:41001")},
	} {
		netnsCreate(t, e.orig, e.reply)
	}
	before := netnsCount(t)

	tupleless := func(guard bool) netlink.Message {
		ae := netlink.NewAttributeEncoder()
		if guard {
			ae.Bytes(attrMark, binary.BigEndian.AppendUint32(nil, flushGuardMark))
			ae.Bytes(attrMarkMask, binary.BigEndian.AppendUint32(nil, flushGuardMask))
		}
		attrs, err := ae.Encode()
		if err != nil {
			t.Fatal(err)
		}
		return netlink.Message{
			Header: netlink.Header{Type: msgDelete, Flags: netlink.Request | netlink.Acknowledge},
			Data:   append([]byte{unix.AF_INET, unix.NFNETLINK_V0, 0, 0}, attrs...),
		}
	}

	if err := exchange(tupleless(true)); err != nil {
		t.Fatalf("guarded tuple-less delete: %v", err)
	}
	if after := netnsCount(t); after != before {
		t.Fatalf("a tuple-less delete carrying the flush guard removed %d of %d entries", before-after, before)
	}

	if err := exchange(tupleless(false)); err != nil {
		t.Fatalf("unguarded tuple-less delete: %v", err)
	}
	if after := netnsCount(t); after != 0 {
		t.Fatalf("an unguarded tuple-less delete left %d of %d entries: the guard check above proves nothing on this kernel", after, before)
	}
}
