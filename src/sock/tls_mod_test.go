package sock

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func grownHello(t *testing.T, host string, pad int) []byte {
	t.Helper()
	hello := realHello(t, host)
	out := append(cloneBytes(hello), make([]byte, 4+pad)...)
	ext := out[len(hello):]
	binary.BigEndian.PutUint16(ext[0:2], 0x0015)
	binary.BigEndian.PutUint16(ext[2:4], uint16(pad))
	binary.BigEndian.PutUint16(out[3:5], uint16(len(out)-5))
	hsLen := len(out) - 9
	out[6], out[7], out[8] = byte(hsLen>>16), byte(hsLen>>8), byte(hsLen)
	p := 9 + 2 + 32
	p += 1 + int(out[p])
	p += 2 + int(binary.BigEndian.Uint16(out[p:]))
	p += 1 + int(out[p])
	binary.BigEndian.PutUint16(out[p:], uint16(len(out)-p-2))
	return out
}

func helloDeclaringMoreThanItHolds(t *testing.T, host string, more int) []byte {
	t.Helper()
	seg := realHello(t, host)
	recLen := int(binary.BigEndian.Uint16(seg[3:5])) + more
	binary.BigEndian.PutUint16(seg[3:5], uint16(recLen))
	hsLen := (int(seg[6])<<16 | int(seg[7])<<8 | int(seg[8])) + more
	seg[6], seg[7], seg[8] = byte(hsLen>>16), byte(hsLen>>8), byte(hsLen)
	return seg
}

func helloRandom(hello []byte) []byte { return hello[11:43] }

func helloSessionID(hello []byte) []byte { return hello[44 : 44+int(hello[43])] }

func fillSessionID(hello []byte, b byte) {
	for i := range helloSessionID(hello) {
		hello[44+i] = b
	}
}

func fakePayloadFor(t *testing.T, segment []byte, v6 bool, cfg *config.SetConfig) []byte {
	t.Helper()
	if v6 {
		pkt := buildMinimalIPv6TCPPacket(len(segment))
		copy(pkt[60:], segment)
		FixTCPChecksumV6(pkt)
		fake := BuildFakeSNIPacketV6(pkt, cfg)
		if fake == nil {
			t.Fatal("IPv6 fake not built")
		}
		return fake[60:]
	}
	pkt := buildMinimalIPv4TCPPacket(len(segment))
	copy(pkt[40:], segment)
	FixTCPChecksum(pkt)
	fake := BuildFakeSNIPacketV4(pkt, cfg)
	if fake == nil {
		t.Fatal("IPv4 fake not built")
	}
	return fake[40:]
}

func fakeSetWithMods(mode string, mods ...string) *config.SetConfig {
	cfg := &config.SetConfig{}
	cfg.Faking.SNIType = config.FakePayloadDefault1
	cfg.Faking.TLSMod = mods
	cfg.Faking.FakeLenMode = mode
	return cfg
}

func familyName(v6 bool) string {
	if v6 {
		return "IPv6"
	}
	return "IPv4"
}

func TestDupSIDCopiesTheSessionIDFromTheFirstSegmentOfASplitHello(t *testing.T) {
	full := grownHello(t, "www.example.com", 1200)
	fillSessionID(full, 0xab)
	declaring := helloDeclaringMoreThanItHolds(t, "www.example.com", 1200)
	fillSessionID(declaring, 0xcd)
	cases := []struct {
		name    string
		segment []byte
	}{
		{"the whole hello", full},
		{"the first 1400 bytes of the hello", full[:1400]},
		{"a segment whose record declares 1200 bytes more than it holds", declaring},
	}
	for _, c := range cases {
		for _, v6 := range []bool{false, true} {
			t.Run(c.name+"/"+familyName(v6), func(t *testing.T) {
				if _, _, ok := sni.LocateServerName(c.segment); !ok {
					t.Fatal("the real segment carries no locatable host name")
				}
				fake := fakePayloadFor(t, c.segment, v6, fakeSetWithMods("", "dupsid"))
				got, want := helloSessionID(fake), helloSessionID(c.segment)
				if !bytes.Equal(got, want) {
					t.Fatalf("dupsid: the fake's session ID is % x..., the real one % x... (%d of %d record bytes in the segment)",
						got[:4], want[:4], len(c.segment)-5, binary.BigEndian.Uint16(c.segment[3:5]))
				}
			})
		}
	}
}

func TestRndRedrawsTheRandomAndTheSessionIDOfEveryFake(t *testing.T) {
	template := config.FakeSNI1
	templateHost, _, ok := sni.ParseTLSClientHelloSNI(template)
	if !ok {
		t.Fatal("the built-in payload does not parse as a ClientHello")
	}
	real := realHello(t, "www.example.com")
	fillSessionID(real, 0xab)
	sidLen := len(helloSessionID(template))
	if sidLen == 0 {
		t.Fatal("the built-in payload has no session ID to redraw")
	}

	for _, v6 := range []bool{false, true} {
		t.Run(familyName(v6), func(t *testing.T) {
			first := fakePayloadFor(t, real, v6, fakeSetWithMods("", "rnd"))
			second := fakePayloadFor(t, real, v6, fakeSetWithMods("", "rnd"))
			for _, fake := range [][]byte{first, second} {
				if len(fake) != len(template) || len(helloSessionID(fake)) != sidLen {
					t.Fatalf("rnd changed the shape: %d bytes with a %d-byte session ID, the payload has %d and %d", len(fake), len(helloSessionID(fake)), len(template), sidLen)
				}
				if bytes.Equal(helloRandom(fake), helloRandom(template)) {
					t.Fatalf("rnd kept the payload's Random % x", helloRandom(fake)[:8])
				}
				if bytes.Equal(helloSessionID(fake), helloSessionID(template)) {
					t.Fatalf("rnd kept the payload's session ID % x", helloSessionID(fake)[:8])
				}
				if !bytes.Equal(fake[:11], template[:11]) || !bytes.Equal(fake[44+sidLen:], template[44+sidLen:]) {
					t.Fatal("rnd changed bytes outside the Random and the session ID")
				}
				host, _, found := sni.ParseTLSClientHelloSNI(fake)
				if !found || host != templateHost {
					t.Fatalf("the fake no longer parses with the payload's name: %q, %v", host, found)
				}
			}
			if bytes.Equal(helloRandom(first), helloRandom(second)) || bytes.Equal(helloSessionID(first), helloSessionID(second)) {
				t.Fatal("two fakes with rnd share a Random or a session ID")
			}

			both := fakePayloadFor(t, real, v6, fakeSetWithMods("", "rnd", "dupsid"))
			if !bytes.Equal(helloSessionID(both), helloSessionID(real)) {
				t.Fatalf("rnd with dupsid: session ID % x..., want the real hello's % x...", helloSessionID(both)[:4], helloSessionID(real)[:4])
			}
			if bytes.Equal(helloRandom(both), helloRandom(template)) || bytes.Equal(helloRandom(both), helloRandom(real)) {
				t.Fatal("rnd with dupsid must still redraw the Random")
			}
		})
	}
}

func TestRndLeavesAnEmptySessionIDEmpty(t *testing.T) {
	template := config.FakeSNI1
	sidLen := int(template[43])
	noSID := append(cloneBytes(template[:43]), 0)
	noSID = append(noSID, template[44+sidLen:]...)
	binary.BigEndian.PutUint16(noSID[3:5], uint16(len(noSID)-5))
	hs := len(noSID) - 9
	noSID[6], noSID[7], noSID[8] = byte(hs>>16), byte(hs>>8), byte(hs)

	fake := ApplyTLSMod(cloneBytes(noSID), nil, ParseTLSMod([]string{"rnd"}))
	if len(fake) != len(noSID) || fake[43] != 0 {
		t.Fatalf("rnd on a hello without a session ID gave %d bytes with a %d-byte session ID", len(fake), fake[43])
	}
	if bytes.Equal(helloRandom(fake), helloRandom(noSID)) {
		t.Fatal("rnd kept the Random")
	}
	if !bytes.Equal(fake[43:], noSID[43:]) {
		t.Fatal("rnd changed bytes after the Random of a hello without a session ID")
	}
}

func TestRndSurvivesMatchFakeSize(t *testing.T) {
	whole := realHello(t, "www.example.com")
	split := grownHello(t, "www.example.com", 1200)
	cases := []struct {
		name    string
		segment []byte
	}{
		{"a hello in one segment", whole},
		{"the first 1400 bytes of a split hello", split[:1400]},
	}
	for _, c := range cases {
		for _, v6 := range []bool{false, true} {
			t.Run(c.name+"/"+familyName(v6), func(t *testing.T) {
				fake := fakePayloadFor(t, c.segment, v6, fakeSetWithMods("match", "rnd"))
				if len(fake) != len(c.segment) {
					t.Fatalf("the match-mode fake is %d bytes, the segment %d", len(fake), len(c.segment))
				}
				if bytes.Equal(helloRandom(fake), helloRandom(c.segment)) {
					t.Fatalf("tls_mod rnd with fake_len_mode match: the fake carries the real hello's Random % x", helloRandom(fake)[:8])
				}
				if bytes.Equal(helloSessionID(fake), helloSessionID(c.segment)) {
					t.Fatalf("tls_mod rnd with fake_len_mode match: the fake carries the real hello's session ID % x", helloSessionID(fake)[:8])
				}

				both := fakePayloadFor(t, c.segment, v6, fakeSetWithMods("match", "rnd", "dupsid"))
				if bytes.Equal(helloRandom(both), helloRandom(c.segment)) {
					t.Fatal("rnd with dupsid and match: the fake carries the real Random")
				}
				if !bytes.Equal(helloSessionID(both), helloSessionID(c.segment)) {
					t.Fatal("rnd with dupsid and match: the fake must carry the real session ID")
				}
			})
		}
	}
}

func TestATiledOrCutTLSFakeDeclaresItsOwnLengths(t *testing.T) {
	real := realHello(t, "europe1.discourse-cdn.com")
	s, _, ok := sni.LocateServerNameInRecord(real)
	if !ok {
		t.Fatal("real hello has no SNI")
	}
	cases := []struct {
		name    string
		segment []byte
	}{
		{"a segment shorter than the payload", real[s+40:]},
		{"a segment longer than the payload", make([]byte, len(config.FakeSNI1)+700)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := MatchPayloadLength(cloneBytes(config.FakeSNI1), c.segment, "match")
			if len(fake) != len(c.segment) {
				t.Fatalf("the fake is %d bytes, the segment %d", len(fake), len(c.segment))
			}
			if fake[0] != 0x16 || fake[5] != 0x01 {
				t.Fatalf("the fake does not start with a ClientHello record: % x", fake[:6])
			}
			recLen := int(binary.BigEndian.Uint16(fake[3:5]))
			hsLen := int(fake[6])<<16 | int(fake[7])<<8 | int(fake[8])
			if recLen != len(fake)-5 || hsLen != recLen-4 {
				t.Fatalf("%d-byte fake: record length %d (want %d), handshake length %d (want %d)", len(fake), recLen, len(fake)-5, hsLen, recLen-4)
			}
		})
	}
}
