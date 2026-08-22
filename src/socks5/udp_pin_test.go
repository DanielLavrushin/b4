package socks5

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func associate(t *testing.T, addr string, dstIP [4]byte, dstPort uint16) (net.Conn, *net.UDPAddr) {
	t.Helper()
	c, reply, err := greet(t, addr, authNone)
	if err != nil {
		t.Fatalf("greeting failed: %v", err)
	}
	if reply[1] != authNone {
		t.Fatalf("expected method 0x00, got % x", reply)
	}

	req := []byte{socks5Version, cmdUDPAssociate, 0x00, atypIPv4, dstIP[0], dstIP[1], dstIP[2], dstIP[3]}
	req = binary.BigEndian.AppendUint16(req, dstPort)
	if _, err := c.Write(req); err != nil {
		t.Fatalf("associate request: %v", err)
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatalf("associate reply: %v", err)
	}
	if head[1] != repSuccess {
		t.Fatalf("associate refused with 0x%02x", head[1])
	}
	if head[3] != atypIPv4 {
		t.Fatalf("expected an IPv4 bind address, got atyp 0x%02x", head[3])
	}
	bind := make([]byte, 6)
	if _, err := io.ReadFull(c, bind); err != nil {
		t.Fatalf("associate bind address: %v", err)
	}
	return c, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(binary.BigEndian.Uint16(bind[4:]))}
}

func udpDatagram(port int, payload []byte) []byte {
	d := []byte{0x00, 0x00, 0x00, atypIPv4, 127, 0, 0, 1}
	d = binary.BigEndian.AppendUint16(d, uint16(port))
	return append(d, payload...)
}

func TestUDPAssociateIgnoresClientNominatedSourceIP(t *testing.T) {
	echoPort := startUDPEcho(t)
	_, addr := startTestServer(t, config.Socks5Config{UDPReadTimeout: 5})

	c, relay := associate(t, addr, [4]byte{203, 0, 113, 5}, 0)
	defer c.Close()

	u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()

	payload := []byte("pinned to the control connection")
	if _, err := u.WriteToUDP(udpDatagram(echoPort, payload), relay); err != nil {
		t.Fatalf("send through relay: %v", err)
	}

	_ = u.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := u.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("the relay must serve the control connection peer, not the address named in ASSOCIATE: %v", err)
	}
	if n < 10 || string(buf[10:n]) != string(payload) {
		t.Fatalf("echo mismatch: got % x", buf[:n])
	}
}

func TestUDPAssociateDropsPacketsFromAnotherSource(t *testing.T) {
	echoPort := startUDPEcho(t)
	_, addr := startTestServer(t, config.Socks5Config{UDPReadTimeout: 5})

	c, relay := associate(t, addr, [4]byte{0, 0, 0, 0}, 55555)
	defer c.Close()

	u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()

	if _, err := u.WriteToUDP(udpDatagram(echoPort, []byte("wrong port")), relay); err != nil {
		t.Fatalf("send through relay: %v", err)
	}

	_ = u.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
	buf := make([]byte, 2048)
	if _, _, err := u.ReadFromUDP(buf); err == nil {
		t.Fatal("a datagram from a port the association did not claim must be dropped")
	}
}
