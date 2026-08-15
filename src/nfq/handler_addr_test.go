package nfq

import (
	"net"
	"os"
	"regexp"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestParseIPHeadersDoesNotAliasBufferV4(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)

	pkt := makeV4UDPPacket([]byte("payload"), net.ParseIP("192.168.1.100"), net.ParseIP("8.8.8.8"), 45001, 53)

	p, ok := w.parseIPHeaders(pkt)
	if !ok {
		t.Fatal("parse failed")
	}
	src, dst := p.src.String(), p.dst.String()

	for i := 12; i < 20; i++ {
		pkt[i] = 0xff
	}

	if p.src.String() != src || p.dst.String() != dst {
		t.Fatalf("addresses followed the buffer: src %s -> %s, dst %s -> %s", src, p.src, dst, p.dst)
	}
}

func TestParseIPHeadersDoesNotAliasBufferV6(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)

	pkt := makeV6TCPPacket([]byte("payload"), 1)

	p, ok := w.parseIPHeaders(pkt)
	if !ok {
		t.Fatal("parse failed")
	}
	src, dst := p.src.String(), p.dst.String()

	for i := 8; i < 40; i++ {
		pkt[i] = 0xff
	}

	if p.src.String() != src || p.dst.String() != dst {
		t.Fatalf("addresses followed the buffer: src %s -> %s, dst %s -> %s", src, p.src, dst, p.dst)
	}
}

func TestParseIPHeadersKeepsWireSourceForReinjection(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	w.srcResolver = newResolverWith(t, tunTestWAN,
		"ipv4 2 udp 17 29 src=192.168.1.100 dst=8.8.8.8 sport=45001 dport=53 src=8.8.8.8 dst="+tunTestWAN+" sport=53 dport=45001 use=1\n")

	pkt := makeV4UDPPacket([]byte("payload"), net.ParseIP(tunTestWAN), net.ParseIP("8.8.8.8"), 45001, 53)

	p, ok := w.parseIPHeaders(pkt)
	if !ok {
		t.Fatal("parse failed")
	}

	if p.src.String() != "192.168.1.100" {
		t.Fatalf("handler should see the LAN client, got %s", p.src)
	}
	if got := net.IP(pkt[12:16]).String(); got != tunTestWAN {
		t.Fatalf("the wire source must stay SNAT'd for re-injection, got %s", got)
	}
}

func TestDNSPathReadsNoAddressesFromWire(t *testing.T) {
	src, err := os.ReadFile("dns.go")
	if err != nil {
		t.Fatal(err)
	}
	bad := regexp.MustCompile(`raw\[(12:16|16:20|8:24|24:40)\]`)
	if m := bad.FindAll(src, -1); m != nil {
		t.Fatalf("dns.go must take addresses from pktInfo, found %d wire read(s): %s", len(m), m[0])
	}
}
