package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

const md5FitFullSegment = 1500

func nameFirstClientHello(host string, padAfter int) []byte {
	var exts []byte
	exts = append(exts, buildSNIExtension(host)...)
	exts = append(exts, buildExtension(21, bytes.Repeat([]byte{0xAB}, padAfter))...)

	body := []byte{0x03, 0x03}
	body = append(body, bytes.Repeat([]byte{0x11}, 32)...)
	body = append(body, 32)
	body = append(body, bytes.Repeat([]byte{0x22}, 32)...)
	body = append(body, 0x00, 0x02, 0x13, 0x01, 0x01, 0x00)
	body = binary.BigEndian.AppendUint16(body, uint16(len(exts)))
	body = append(body, exts...)

	record := []byte{0x16, 0x03, 0x01}
	record = binary.BigEndian.AppendUint16(record, uint16(4+len(body)))
	record = append(record, 0x01, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	return append(record, body...)
}

type md5FitHello struct {
	name   string
	hello  []byte
	mirror bool
}

func md5FitHellos() []md5FitHello {
	return []md5FitHello{
		{"name past the cut", buildClientHello("i.ytimg.com", 1600, 0xAB), false},
		{"name in the segment", nameFirstClientHello("i.ytimg.com", 1600), true},
	}
}

func md5FitFullSegmentOriginal(ver byte, src, dst net.IP, hello []byte) []byte {
	l3 := 20
	if ver == IPv6 {
		l3 = IPv6HeaderLen
	}
	carried := md5FitFullSegment - l3 - 20 - len(fakeSNITestTimestampOptions)
	pkt := synWithOptions(ver, fakeSNITestTimestampOptions, hello[:carried], 1000)
	pkt[l3+13] = 0x18
	binary.BigEndian.PutUint32(pkt[l3+8:l3+12], fakeSNITestAck)
	if ver == IPv6 {
		if src != nil {
			copy(pkt[8:24], src.To16())
			copy(pkt[24:40], dst.To16())
		}
		sock.FixTCPChecksumV6(pkt)
		return pkt
	}
	if src != nil {
		copy(pkt[12:16], src.To4())
		copy(pkt[16:20], dst.To4())
	}
	sock.FixIPv4Checksum(pkt[:20])
	sock.FixTCPChecksum(pkt)
	return pkt
}

func md5FitSets() []config.SetConfig {
	var sets []config.SetConfig
	for _, c := range []struct {
		strategy  string
		md5OnFake bool
		sniType   int
		lenMode   string
	}{
		{"tcp_check", true, config.FakePayloadDefault1, "match"},
		{"md5sum", false, config.FakePayloadDefault1, "match"},
		{"tcp_check", true, config.FakePayloadInverted, ""},
	} {
		set := fakeSNITestSet(fmt.Sprintf("%s/md5_on_fake=%v/sni_type=%d/%s", c.strategy, c.md5OnFake, c.sniType, c.lenMode), c.strategy)
		set.Faking.MD5OnFake = c.md5OnFake
		set.Faking.SNIType = c.sniType
		set.Faking.FakeLenMode = c.lenMode
		sets = append(sets, set)
	}
	return sets
}

func fakeSNIPayload(pkt []byte) []byte {
	l3 := int(pkt[0]&0x0F) * 4
	if pkt[0]>>4 == IPv6 {
		l3 = IPv6HeaderLen
	}
	return pkt[l3+int(pkt[l3+12]>>4)*4:]
}

func md5FitHeadersConsistent(t *testing.T, fake []byte) {
	t.Helper()
	if fake[0]>>4 == IPv6 {
		if n := int(binary.BigEndian.Uint16(fake[4:6])); n != len(fake)-IPv6HeaderLen {
			t.Fatalf("the %d-byte fake declares an IPv6 payload of %d bytes", len(fake), n)
		}
		if !sock.TCPChecksumValidV6(fake) {
			t.Fatal("the fake's TCP checksum is wrong")
		}
		return
	}
	if n := int(binary.BigEndian.Uint16(fake[2:4])); n != len(fake) {
		t.Fatalf("the %d-byte fake declares an IPv4 total length of %d", len(fake), n)
	}
	if !ipv4HeaderChecksumValid(fake[:int(fake[0]&0x0F)*4]) {
		t.Fatal("the fake's IPv4 header checksum is wrong")
	}
	if !sock.TCPChecksumValid(fake) {
		t.Fatal("the fake's TCP checksum is wrong")
	}
}

func TestAnMD5FakeSizedToAFullRealSegmentStillFitsIt(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		for _, h := range md5FitHellos() {
			original := md5FitFullSegmentOriginal(ver, nil, nil, h.hello)
			realPayload := fakeSNIPayload(original)
			if mirrored := sock.MirrorClientHello(realPayload, config.FakeSNI1) != nil; mirrored != h.mirror {
				t.Fatalf("%s: Match Fake Size mirrors the real segment: %v, want %v", h.name, mirrored, h.mirror)
			}
			for _, set := range md5FitSets() {
				t.Run(fmt.Sprintf("v%d/%s/%s", ver, h.name, set.Name), func(t *testing.T) {
					w := newDualStackInjectingWorker(t, &set)
					sent := captureInjected(t, w)
					sendFakeSNIFor(w, &set, original)
					got := sent()
					if len(got) != 1 {
						t.Fatalf("want one fake, sent %d packets", len(got))
					}
					fake := got[0]
					if len(fake) > len(original) {
						t.Fatalf("a %d-byte fake for a %d-byte real segment that fills the path MTU: the interface cannot carry it", len(fake), len(original))
					}
					payload := fakeSNIPayload(fake)
					if set.Faking.Strategy == "md5sum" {
						if lengths := md5OptionLengths(fake); len(lengths) != 1 || lengths[0] != 18 {
							t.Fatalf("an MD5 Sum fake must still carry its option, got lengths %v", lengths)
						}
						if rec := int(binary.BigEndian.Uint16(payload[3:5])); rec != len(payload)-5 {
							t.Fatalf("the trimmed MD5 Sum fake carries %d bytes but declares a %d-byte record, want %d", len(payload), rec, len(payload)-5)
						}
						md5FitHeadersConsistent(t, fake)
						return
					}
					if len(payload) != len(realPayload) {
						t.Fatalf("md5_on_fake: fake payload %d bytes, want the real %d", len(payload), len(realPayload))
					}
					if lengths := md5OptionLengths(fake); len(lengths) != 0 {
						t.Fatalf("md5_on_fake: the option has no room in the packet size, yet the fake carries it (%v)", lengths)
					}
				})
			}
		}
	}
}

func TestTheMD5OptionStaysWhereTheFakeHasRoomForIt(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		short := fakeSNIOriginal(ver, "", fakeSNITestTimestampOptions)
		for _, c := range []struct {
			name      string
			strategy  string
			md5OnFake bool
			sniType   int
			lenMode   string
			original  []byte
		}{
			{"md5_on_fake, the built-in payload, a one-segment hello", "tcp_check", true, config.FakePayloadDefault1, "", short},
			{"md5_on_fake, match, a one-segment hello", "tcp_check", true, config.FakePayloadDefault1, "match", short},
			{"md5sum, match, a one-segment hello", "md5sum", false, config.FakePayloadDefault1, "match", short},
			{"md5_on_fake, the built-in payload, a full segment", "tcp_check", true, config.FakePayloadDefault1, "", md5FitFullSegmentOriginal(ver, nil, nil, md5FitHellos()[0].hello)},
		} {
			t.Run(fmt.Sprintf("v%d/%s", ver, c.name), func(t *testing.T) {
				set := fakeSNITestSet(c.name, c.strategy)
				set.Faking.MD5OnFake = c.md5OnFake
				set.Faking.SNIType = c.sniType
				set.Faking.FakeLenMode = c.lenMode
				w := newDualStackInjectingWorker(t, &set)
				sent := captureInjected(t, w)
				sendFakeSNIFor(w, &set, c.original)
				got := sent()
				if len(got) != 1 {
					t.Fatalf("want one fake, sent %d packets", len(got))
				}
				if lengths := md5OptionLengths(got[0]); len(lengths) != 1 || lengths[0] != 18 {
					t.Fatalf("the fake has room for the option and must carry it, got lengths %v", lengths)
				}
				if c.lenMode == "match" {
					if n, want := len(fakeSNIPayload(got[0])), len(fakeSNIPayload(c.original)); n != want {
						t.Fatalf("a match-sized fake of a hello that is not cut by the segment is %d bytes, want the real %d", n, want)
					}
				}
			})
		}
	}
}

func TestAFakeSendWarningNeverAsksToEnableMatchFakeSizeWhenTheFakeIsAlreadySizedToTheSegment(t *testing.T) {
	for _, c := range []struct {
		name    string
		lenMode string
		sniType int
		advises bool
	}{
		{"Match Fake Size on", "match", config.FakePayloadDefault1, false},
		{"the Inverted payload", "", config.FakePayloadInverted, false},
		{"the built-in payload at its own length", "", config.FakePayloadDefault1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			buf := captureLog(t)
			set := fakeSNITestSet("emsgsize-"+strings.ReplaceAll(c.name, " ", "-"), "tcp_check")
			set.Faking.FakeLenMode = c.lenMode
			set.Faking.SNIType = c.sniType
			w := newDualStackInjectingWorker(t, &set)

			w.warnFakeSend(&set, 1520, net.ParseIP("1.2.3.4"), syscall.EMSGSIZE)

			log.Flush()
			out := buf.String()
			if !strings.Contains(out, fmt.Sprintf("%q", set.Name)) || !strings.Contains(out, "MTU") {
				t.Fatalf("want a warning naming the set and the MTU, got:\n%s", out)
			}
			if advises := strings.Contains(out, "enable fake_len_mode match"); advises != c.advises {
				t.Fatalf("hint advises enabling fake_len_mode match: %v, want %v:\n%s", advises, c.advises, out)
			}
		})
	}
}
