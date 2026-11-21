package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/tlsgen"
)

type netnsPastSeqFamily struct {
	name    string
	network string
	loop    net.IP
	proto   uint16
	v6      bool
}

var netnsPastSeqFamilies = []netnsPastSeqFamily{
	{"IPv4", "tcp4", net.IPv4(127, 0, 0, 1), syscall.ETH_P_IP, false},
	{"IPv6", "tcp6", net.IPv6loopback, syscall.ETH_P_IPV6, true},
}

func netnsPastSeqHtons(v uint16) uint16 { return v<<8 | v>>8 }

func netnsPastSeqTap(t *testing.T, fam netnsPastSeqFamily) int {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(netnsPastSeqHtons(fam.proto)))
	if err != nil {
		t.Fatalf("packet socket: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	lo, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{Protocol: netnsPastSeqHtons(fam.proto), Ifindex: lo.Index}); err != nil {
		t.Fatalf("bind packet socket: %v", err)
	}
	return fd
}

func netnsPastSeqL3(pkt []byte, v6 bool) (int, bool) {
	if v6 {
		return 40, len(pkt) >= 60 && pkt[0]>>4 == 6 && pkt[6] == 6
	}
	if len(pkt) < 40 || pkt[0]>>4 != 4 || pkt[9] != 6 {
		return 0, false
	}
	return int(pkt[0]&0x0F) * 4, true
}

func netnsPastSeqHandshakeACK(t *testing.T, fd int, fam netnsPastSeqFamily, sport, dport uint16) []byte {
	t.Helper()
	buf := make([]byte, 65536)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		n, _, err := syscall.Recvfrom(fd, buf, syscall.MSG_DONTWAIT)
		if err != nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		pkt := buf[:n]
		l3, ok := netnsPastSeqL3(pkt, fam.v6)
		if !ok || binary.BigEndian.Uint16(pkt[l3:l3+2]) != sport || binary.BigEndian.Uint16(pkt[l3+2:l3+4]) != dport {
			continue
		}
		thl := int(pkt[l3+12]>>4) * 4
		if pkt[l3+13]&0x12 == 0x10 && n == l3+thl {
			return append([]byte(nil), pkt...)
		}
	}
	t.Fatal("the handshake ACK never showed on lo")
	return nil
}

func netnsPastSeqRealSegment(ack, hello []byte, fam netnsPastSeqFamily) []byte {
	l3, _ := netnsPastSeqL3(ack, fam.v6)
	pkt := append(append([]byte(nil), ack...), hello...)
	pkt[l3+13] = 0x18
	if fam.v6 {
		binary.BigEndian.PutUint16(pkt[4:6], uint16(len(pkt)-40))
		sock.FixTCPChecksumV6(pkt)
		return pkt
	}
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	sock.FixIPv4Checksum(pkt[:l3])
	sock.FixTCPChecksum(pkt)
	return pkt
}

func netnsPastSeqServerRead(t *testing.T, fd int, fam netnsPastSeqFamily, strategy string, seqOffset int32) (got, hello []byte) {
	t.Helper()
	ln, err := net.Listen(fam.network, net.JoinHostPort(fam.loop.String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial(fam.network, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var server net.Conn
	select {
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("the server never accepted the connection")
	}
	defer server.Close()

	sport := uint16(client.LocalAddr().(*net.TCPAddr).Port)
	dport := uint16(ln.Addr().(*net.TCPAddr).Port)
	ack := netnsPastSeqHandshakeACK(t, fd, fam, sport, dport)

	hello, err = tlsgen.GenerateTLSClientHello("www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	original := netnsPastSeqRealSegment(ack, hello, fam)

	set := config.NewSetConfig()
	set.Faking.SNIType = config.FakePayloadDefault1
	set.Faking.PayloadData = nil
	set.Faking.Strategy = strategy
	set.Faking.SeqOffset = seqOffset
	set.Faking.ApplyTTL = false
	set.Faking.FakeLenMode = ""
	set.Faking.TLSMod = nil
	var fake []byte
	if fam.v6 {
		fake = sock.BuildFakeSNIPacketV6(original, &set)
	} else {
		fake = sock.BuildFakeSNIPacketV4(original, &set)
	}
	if fake == nil {
		t.Fatal("fake not built")
	}

	sender, err := sock.NewSenderWithMark(0)
	if err != nil {
		t.Fatalf("raw sender: %v", err)
	}
	defer sender.Close()
	if fam.v6 && !sender.IPv6Ready() {
		t.Fatal("the sender has no IPv6 raw socket, so the fake would never leave")
	}
	if fam.v6 {
		err = sender.SendIPv6(fake, fam.loop)
	} else {
		err = sender.SendIPv4(fake, fam.loop)
	}
	if err != nil {
		t.Fatalf("send fake: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := client.Write(hello); err != nil {
		t.Fatal(err)
	}

	got = make([]byte, len(hello))
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(server, got); err != nil {
		t.Fatalf("server read: %v", err)
	}
	return got, hello
}

func TestNetnsTheServerReadsTheRealHelloBehindAPastSeqFake(t *testing.T) {
	netnsHoldRequire(t)
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	cases := []struct {
		strategy string
		offset   int32
	}{
		{"pastseq", 10000},
		{"pastseq", 2},
		{"pastseq", 1000},
		{"randseq", 100},
	}
	for _, fam := range netnsPastSeqFamilies {
		t.Run(fam.name, func(t *testing.T) {
			probe, err := net.Listen(fam.network, net.JoinHostPort(fam.loop.String(), "0"))
			if err != nil {
				t.Skipf("no %s loopback in this namespace: %v", fam.name, err)
			}
			_ = probe.Close()
			fd := netnsPastSeqTap(t, fam)
			for _, c := range cases {
				t.Run(fmt.Sprintf("%s seq_offset=%d", c.strategy, c.offset), func(t *testing.T) {
					got, hello := netnsPastSeqServerRead(t, fd, fam, c.strategy, c.offset)
					if !bytes.Equal(got, hello) {
						t.Fatalf("the server read %x... where the client sent %x...: the fake's bytes reached the stream (%v)",
							got[:8], hello[:8], bytes.Contains(config.FakeSNI1, got[:16]))
					}
				})
			}
		})
	}
}
