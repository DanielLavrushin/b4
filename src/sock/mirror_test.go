package sock

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/tlsgen"
)

func realHello(t *testing.T, host string) []byte {
	t.Helper()
	hello, err := tlsgen.GenerateTLSClientHello(host)
	if err != nil {
		t.Fatalf("generate hello: %v", err)
	}
	return hello
}

func assertHostShape(t *testing.T, name string, n int) {
	t.Helper()
	if len(name) != n {
		t.Fatalf("%q has length %d, want %d", name, len(name), n)
	}
	if !sni.IsValidSNI([]byte(name)) {
		t.Fatalf("%q is not a valid host name", name)
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
		t.Fatalf("%q has an empty label", name)
	}
}

func TestFitHostName(t *testing.T) {
	cases := []struct {
		template string
		n        int
		suffix   string
	}{
		{"www.google.com", 14, "www.google.com"},
		{"www.google.com", 10, "google.com"},
		{"www.google.com", 11, "google.com"},
		{"www.google.com", 15, "www.google.com"},
		{"www.google.com", 25, ".www.google.com"},
		{"www.google.com", 5, ".com"},
		{"www.google.com", 4, ""},
		{"localhost", 12, ".localhost"},
	}
	for _, c := range cases {
		got := FitHostName(c.template, c.n)
		assertHostShape(t, got, c.n)
		if !strings.HasSuffix(got, c.suffix) {
			t.Errorf("FitHostName(%q, %d) = %q, want suffix %q", c.template, c.n, got, c.suffix)
		}
	}
	if FitHostName("www.google.com", 0) != "" {
		t.Fatal("zero length must give an empty name")
	}
	if FitHostName("com", 3) != "com" {
		t.Fatal("a template of exactly the wanted length must be kept")
	}
	for _, n := range []int{4, 5, 80, 130, 255} {
		got := FitHostName("www.google.com", n)
		assertHostShape(t, got, n)
		for _, label := range strings.Split(got, ".") {
			if len(label) > 63 {
				t.Fatalf("FitHostName(_, %d) = %q has a %d-byte label", n, got, len(label))
			}
		}
	}
}

func TestMatchPayloadLengthMirrorsTheRealHello(t *testing.T) {
	real := realHello(t, "europe1.discourse-cdn.com")
	s, e, ok := sni.LocateServerNameInRecord(real)
	if !ok {
		t.Fatal("real hello has no SNI")
	}

	for _, payload := range [][]byte{config.FakeSNI1, config.FakeSNI2} {
		fake := MatchPayloadLength(cloneBytes(payload), real, "match")
		if len(fake) != len(real) {
			t.Fatalf("fake is %d bytes, real is %d", len(fake), len(real))
		}
		if !bytes.Equal(fake[:s], real[:s]) || !bytes.Equal(fake[e:], real[e:]) {
			t.Fatal("bytes outside the host name changed")
		}
		if bytes.Equal(fake[s:e], real[s:e]) {
			t.Fatal("host name was not replaced")
		}
		host, _, found := sni.ParseTLSClientHelloSNI(fake)
		if !found {
			t.Fatal("mirrored fake has no parseable SNI")
		}
		template, _, _ := sni.ParseTLSClientHelloSNI(payload)
		assertHostShape(t, host, e-s)
		if !strings.HasSuffix(host, template) {
			t.Fatalf("fake host %q does not carry the payload's name %q", host, template)
		}
		fs, fe, ok := sni.LocateServerNameInRecord(fake)
		if !ok || fs != s || fe != e {
			t.Fatalf("mirrored fake is not a well-formed hello: range %d..%d ok=%v", fs, fe, ok)
		}
	}
}

func TestMatchPayloadLengthMirrorsALongerRealHello(t *testing.T) {
	real := realHello(t, "www.svoboda.org")
	fake := MatchPayloadLength(cloneBytes(config.FakeSNI1), real, "match")
	host, _, found := sni.ParseTLSClientHelloSNI(fake)
	if !found || len(host) != len("www.svoboda.org") {
		t.Fatalf("host %q, found=%v", host, found)
	}
	if len(fake) != len(real) {
		t.Fatalf("fake is %d bytes, real is %d", len(fake), len(real))
	}
}

func TestMatchPayloadLengthTilesNonTLSPayloads(t *testing.T) {
	real := realHello(t, "europe1.discourse-cdn.com")
	fake := MatchPayloadLength(cloneBytes(config.FakeSTUN), real, "match")
	if len(fake) != len(real) {
		t.Fatalf("fake is %d bytes, real is %d", len(fake), len(real))
	}
	if !bytes.Equal(fake[:len(config.FakeSTUN)], config.FakeSTUN) {
		t.Fatal("tiled payload does not start with the STUN payload")
	}
}

func TestMatchPayloadLengthOnContinuationSegment(t *testing.T) {
	real := realHello(t, "europe1.discourse-cdn.com")
	s, _, _ := sni.LocateServerNameInRecord(real)

	tail := real[s+40:]
	fake := MatchPayloadLength(cloneBytes(config.FakeSNI1), tail, "match")
	if len(fake) != len(tail) {
		t.Fatalf("fake is %d bytes, segment is %d", len(fake), len(tail))
	}
	if !bytes.Equal(fake[:3], config.FakeSNI1[:3]) || !bytes.Equal(fake[5:40], config.FakeSNI1[5:40]) {
		t.Fatal("a segment without a host name must fall back to the tiled payload")
	}

	withHost := real[s-9:]
	fake = MatchPayloadLength(cloneBytes(config.FakeSNI1), withHost, "match")
	if len(fake) != len(withHost) {
		t.Fatalf("fake is %d bytes, segment is %d", len(fake), len(withHost))
	}
	if bytes.Equal(fake, withHost) || !bytes.Equal(fake[:9], withHost[:9]) || !bytes.Equal(fake[9+25:], withHost[9+25:]) {
		t.Fatal("a segment carrying the host name must be mirrored with only the name replaced")
	}
}

func TestMatchPayloadLengthOffLeavesThePayload(t *testing.T) {
	real := realHello(t, "europe1.discourse-cdn.com")
	fake := MatchPayloadLength(cloneBytes(config.FakeSNI1), real, "")
	if !bytes.Equal(fake, config.FakeSNI1) {
		t.Fatal("payload changed with fake_len_mode off")
	}
}

func TestBuiltInTLSPayloadsFitOneSegment(t *testing.T) {
	for name, payload := range map[string][]byte{"FakeSNI1": config.FakeSNI1, "FakeSNI2": config.FakeSNI2} {
		if len(payload) > 1400 {
			t.Errorf("%s is %d bytes and does not fit a 1500 byte MTU behind IP and TCP headers", name, len(payload))
		}
		if _, _, ok := sni.LocateServerNameInRecord(payload); !ok {
			t.Errorf("%s does not parse as a ClientHello with an SNI", name)
		}
	}
	if len(config.FakeSNI2) != 517 {
		t.Errorf("FakeSNI2 is %d bytes, want the 517 of a padded browser hello", len(config.FakeSNI2))
	}
}

func packetWithTimestamp(payloadSize int) []byte {
	ipHdrLen := 20
	tcpHdrLen := 32
	totalLen := ipHdrLen + tcpHdrLen + payloadSize
	pkt := make([]byte, totalLen)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], []byte{192, 168, 1, 1})
	copy(pkt[16:20], []byte{10, 0, 0, 1})
	binary.BigEndian.PutUint16(pkt[ipHdrLen:], 12345)
	binary.BigEndian.PutUint16(pkt[ipHdrLen+2:], 443)
	binary.BigEndian.PutUint32(pkt[ipHdrLen+4:], 1000)
	binary.BigEndian.PutUint32(pkt[ipHdrLen+8:], 2000)
	pkt[ipHdrLen+12] = 0x80
	pkt[ipHdrLen+13] = 0x18
	opts := pkt[ipHdrLen+20 : ipHdrLen+tcpHdrLen]
	opts[0], opts[1] = TCPOptionNOP, TCPOptionNOP
	opts[2], opts[3] = TCPOptionTimestamp, TCPTimestampLength
	binary.BigEndian.PutUint32(opts[4:8], 5000000)
	binary.BigEndian.PutUint32(opts[8:12], 7)
	FixIPv4Checksum(pkt[:ipHdrLen])
	FixTCPChecksum(pkt)
	return pkt
}

func tcpChecksumIsValid(pkt []byte) bool {
	ipHdrLen := int((pkt[0] & 0x0F) * 4)
	want := binary.BigEndian.Uint16(pkt[ipHdrLen+16 : ipHdrLen+18])
	probe := cloneBytes(pkt)
	FixTCPChecksum(probe)
	return want == binary.BigEndian.Uint16(probe[ipHdrLen+16:ipHdrLen+18])
}

func TestTimestampStrategyLowersTheTimestamp(t *testing.T) {
	pkt := packetWithTimestamp(100)
	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "timestamp"

	fake := BuildFakeSNIPacketV4(pkt, cfg)
	if fake == nil {
		t.Fatal("expected a fake")
	}
	tsval := binary.BigEndian.Uint32(fake[20+20+4 : 20+20+8])
	if tsval != 5000000-600000 {
		t.Fatalf("TSval %d, want %d", tsval, 5000000-600000)
	}
	if !tcpChecksumIsValid(fake) {
		t.Fatal("a fake with a lowered timestamp must keep a valid checksum")
	}
}

func TestTimestampStrategyFallsBackToABadChecksumV6(t *testing.T) {
	pkt := buildMinimalIPv6TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "timestamp"

	fake := BuildFakeSNIPacketV6(pkt, cfg)
	if fake == nil {
		t.Fatal("expected a fake")
	}
	if TCPChecksumValidV6(fake) {
		t.Fatal("without a timestamp option the IPv6 fake must go out with a bad checksum")
	}
	cfg.Faking.Strategy = "ttl"
	if !TCPChecksumValidV6(BuildFakeSNIPacketV6(pkt, cfg)) {
		t.Fatal("a ttl fake must keep a valid checksum")
	}
}

func TestBadChecksumSurvivesTheMD5Option(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "tcp_check"

	fake := BuildFakeSNIPacketV4(pkt, cfg)
	badsum := !TCPChecksumValid(fake)
	if !badsum {
		t.Fatal("tcp_check must produce a bad checksum")
	}
	signed := AddTCPMD5Option(fake, false)
	if !TCPChecksumValid(signed) {
		t.Fatal("AddTCPMD5Option is expected to recompute the checksum; the caller must corrupt it again")
	}
	CorruptTCPChecksum(signed)
	if TCPChecksumValid(signed) {
		t.Fatal("CorruptTCPChecksum left a valid checksum")
	}
}

func TestTimestampStrategyFallsBackToABadChecksum(t *testing.T) {
	pkt := buildMinimalIPv4TCPPacket(100)
	cfg := &config.SetConfig{}
	cfg.Faking.Strategy = "timestamp"

	fake := BuildFakeSNIPacketV4(pkt, cfg)
	if fake == nil {
		t.Fatal("expected a fake")
	}
	if fake[8] != pkt[8] {
		t.Fatal("TTL must not change on the fallback")
	}
	if tcpChecksumIsValid(fake) {
		t.Fatal("without a timestamp option the fake must go out with a bad checksum, or the server accepts it")
	}
}
