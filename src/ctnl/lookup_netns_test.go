package ctnl

import (
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const netnsAcctSysctl = "/proc/sys/net/netfilter/nf_conntrack_acct"

func netnsSetAcct(t *testing.T, on bool) {
	t.Helper()
	v := "0"
	if on {
		v = "1"
	}
	if err := os.WriteFile(netnsAcctSysctl, []byte(v), 0o644); err != nil {
		t.Skipf("cannot switch conntrack accounting in this namespace: %v", err)
	}
}

func netnsCreateTCP(t *testing.T, orig, reply Tuple, state uint8) {
	t.Helper()
	ae := netlink.NewAttributeEncoder()
	encodeTuple(ae, attrTupleOrig, orig)
	encodeTuple(ae, attrTupleReply, reply)
	ae.Bytes(netnsAttrTimeout, binary.BigEndian.AppendUint32(nil, 300))
	ae.Nested(attrProtoInfo, func(info *netlink.AttributeEncoder) error {
		info.Nested(attrProtoInfoTCP, func(tcp *netlink.AttributeEncoder) error {
			tcp.Uint8(attrProtoInfoTCPState, state)
			return nil
		})
		return nil
	})
	attrs, err := ae.Encode()
	if err != nil {
		t.Fatal(err)
	}
	m := netlink.Message{
		Header: netlink.Header{Type: netnsMsgNew, Flags: netlink.Request | netlink.Acknowledge | netlink.Create | netlink.Excl},
		Data:   append([]byte{orig.family(), unix.NFNETLINK_V0, 0, 0}, attrs...),
	}
	if err := exchange(m); err != nil {
		t.Fatalf("creating the TCP conntrack entry %s: %v", orig, err)
	}
}

func TestNetnsLookupReadsAnEntryWithAndWithoutAccounting(t *testing.T) {
	netnsRequire(t)
	resetLookup(t)
	udp := func(src, dst string) Tuple {
		return mustTuple(t, unix.IPPROTO_UDP, netip.MustParseAddrPort(src), netip.MustParseAddrPort(dst))
	}
	tcp := func(src, dst string) Tuple {
		return mustTuple(t, unix.IPPROTO_TCP, netip.MustParseAddrPort(src), netip.MustParseAddrPort(dst))
	}

	netnsSetAcct(t, true)
	counted := udp("10.9.2.2:42000", "203.0.113.30:443")
	countedReply := udp("203.0.113.30:443", "198.51.100.1:42000")
	netnsCreate(t, counted, countedReply)
	f, found, err := Lookup(counted, Original)
	if err != nil || !found {
		t.Fatalf("Lookup of an existing entry: found %v, err %v", found, err)
	}
	if byReply, found, err := Lookup(countedReply, Reply); err != nil || !found || byReply != f {
		t.Errorf("the same entry looked up by its reply tuple, the wire tuple after NAT: %+v, found %v, err %v; want %+v", byReply, found, err, f)
	}
	if !f.HasCounters || f.ReplyPackets != 0 || f.ReplyBytes != 0 {
		t.Errorf("an entry made with accounting on carries zero counters, got %+v", f)
	}

	established := tcp("10.9.2.2:42001", "203.0.113.30:443")
	netnsCreateTCP(t, established, tcp("203.0.113.30:443", "198.51.100.1:42001"), TCPEstablished)
	if f, found, err := Lookup(established, Original); err != nil || !found || !f.HasState || f.TCPState != TCPEstablished {
		t.Errorf("a TCP entry reports its state: %+v, found %v, err %v", f, found, err)
	}

	netnsSetAcct(t, false)
	uncounted := udp("10.9.2.2:42002", "203.0.113.30:443")
	netnsCreate(t, uncounted, udp("203.0.113.30:443", "198.51.100.1:42002"))
	if f, found, err := Lookup(uncounted, Original); err != nil || !found || f.HasCounters {
		t.Errorf("an entry made with accounting off has no counters: %+v, found %v, err %v", f, found, err)
	}

	if _, found, err := Lookup(udp("10.9.2.2:42003", "203.0.113.30:443"), Original); err != nil || found {
		t.Fatalf("a tuple the table does not hold: found %v, err %v; want false, nil", found, err)
	}
	if _, found, err := Lookup(counted, Original); err != nil || !found {
		t.Fatalf("the shared connection keeps working after a miss: found %v, err %v", found, err)
	}
}

func netnsRun(t *testing.T, stdin string, name string, args ...string) {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("%s %v: %v: %s", name, args, err, out)
	}
}

func TestNetnsLookupTellsAServerThatAnsweredFromOneThatDidNot(t *testing.T) {
	netnsRequire(t)
	resetLookup(t)
	netnsRun(t, "", "ip", "link", "set", "lo", "up")
	netnsRun(t, "table inet b4lookup {\n\tchain out {\n\t\ttype filter hook output priority 0;\n\t\tct state new counter\n\t}\n}\n", "nft", "-f", "-")
	netnsSetAcct(t, true)

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var held []net.Conn
		defer func() {
			for _, c := range held {
				_ = c.Close()
			}
		}()
		for _, reply := range []bool{true, false} {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
			_, _ = io.ReadFull(c, make([]byte, 517))
			if reply {
				_, _ = c.Write(make([]byte, 3000))
			}
		}
		<-release
	}()
	defer func() {
		_ = ln.Close()
		close(release)
		<-done
	}()

	flowOf := func(c net.Conn) Flow {
		t.Helper()
		local := c.LocalAddr().(*net.TCPAddr).AddrPort()
		remote := c.RemoteAddr().(*net.TCPAddr).AddrPort()
		f, found, err := Lookup(mustTuple(t, unix.IPPROTO_TCP, local, remote), Original)
		if err != nil || !found || !f.HasCounters {
			t.Fatalf("Lookup of the live connection: %+v, found %v, err %v", f, found, err)
		}
		return f
	}
	payloadless := func(f Flow) bool { return f.ReplyBytes <= f.ReplyPackets*80 }

	answered, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer answered.Close()
	_, _ = answered.Write(make([]byte, 517))
	if _, err := io.ReadFull(answered, make([]byte, 3000)); err != nil {
		t.Fatal(err)
	}
	if f := flowOf(answered); payloadless(f) {
		t.Errorf("a server that sent 3000 bytes must not look payloadless: %+v", f)
	}

	silent, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	_, _ = silent.Write(make([]byte, 517))
	time.Sleep(200 * time.Millisecond)
	if f := flowOf(silent); !payloadless(f) || f.ReplyPackets == 0 || f.TCPState != TCPEstablished {
		t.Errorf("a server that only acknowledged the client must look payloadless and established: %+v", f)
	}
}
