package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

const tsFallbackLine = "carries no TCP timestamps"

func resetTimestampFallbackLog(t *testing.T) {
	t.Helper()
	tsFallbackLog.Clear()
	t.Cleanup(tsFallbackLog.Clear)
}

func timestampFallbackLines(buf *syncBuffer) []string {
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, tsFallbackLine) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestATimestampFakeWithoutTimestampsWarnsOncePerSet(t *testing.T) {
	cases := []struct {
		ver      byte
		src      string
		from, to string
	}{
		{IPv4, "192.168.1.50", "192.168.1.50:12345", "1.2.3.4:443"},
		{IPv6, "2001:db8::50", "[2001:db8::50]:12345", "[2001:db8::2]:443"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("v%d", tc.ver), func(t *testing.T) {
			resetTimestampFallbackLog(t)
			buf := captureLog(t)
			set := fakeSNITestSet("ts-set", "timestamp")
			set.Id = "ts-set"
			w := newDualStackInjectingWorker(t, &set)
			sent := captureInjected(t, w)

			sendFakeSNIFor(w, &set, fakeSNIOriginal(tc.ver, tc.src, nil))
			sendFakeSNIFor(w, &set, fakeSNIOriginal(tc.ver, tc.src, nil))
			got := sent()

			log.Flush()
			lines := timestampFallbackLines(buf)
			if len(lines) != 1 {
				t.Fatalf("want exactly one fallback warning for two fakes, got %d:\n%s", len(lines), buf.String())
			}
			line := lines[0]
			for _, want := range []string{"[WARN]", `"ts-set"`, "from " + tc.from + " to " + tc.to, "wrong TCP checksum and a wrong acknowledgment number", "or the server refused them", "netsh interface tcp set global timestamps=enabled"} {
				if !strings.Contains(line, want) {
					t.Errorf("the warning must contain %q: %s", want, line)
				}
			}
			if strings.ContainsRune(line, '\u2014') {
				t.Errorf("the warning must not contain an em-dash: %s", line)
			}
			if len(got) != 2 {
				t.Fatalf("the warning must not stop the fakes, sent %d packets", len(got))
			}
		})
	}
}

func TestNoFallbackWarningWhenTheFakeIsFooledAsConfigured(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			resetTimestampFallbackLog(t)
			buf := captureLog(t)
			withTimestamps := fakeSNITestSet("ts-with-timestamps", "timestamp")
			withTimestamps.Id = "ts-with-timestamps"
			tcpCheck := fakeSNITestSet("tcp-check-set", "tcp_check")
			tcpCheck.Id = "tcp-check-set"
			w := newDualStackInjectingWorker(t, &withTimestamps, &tcpCheck)
			sent := captureInjected(t, w)

			sendFakeSNIFor(w, &withTimestamps, fakeSNIOriginal(ver, "", fakeSNITestTimestampOptions))
			sendFakeSNIFor(w, &tcpCheck, fakeSNIOriginal(ver, "", nil))
			got := sent()

			log.Flush()
			if lines := timestampFallbackLines(buf); len(lines) != 0 {
				t.Fatalf("a timestamp fake that found its option and a tcp_check fake must not warn, got:\n%s", strings.Join(lines, "\n"))
			}
			if len(got) != 2 {
				t.Fatalf("want both fakes sent, got %d packets", len(got))
			}
		})
	}
}

func TestDiscoveryTestSetsDoNotWarn(t *testing.T) {
	resetTimestampFallbackLog(t)
	buf := captureLog(t)
	first := fakeSNITestSet("cached-5-tcpmd5", "timestamp")
	second := fakeSNITestSet("fake-mirror-ts", "timestamp")
	w := newDualStackInjectingWorker(t, &first, &second)
	sent := captureInjected(t, w)

	sendFakeSNIFor(w, &first, fakeSNIOriginal(IPv4, "", nil))
	sendFakeSNIFor(w, &second, fakeSNIOriginal(IPv4, "", nil))

	log.Flush()
	if lines := timestampFallbackLines(buf); len(lines) != 0 {
		t.Fatalf("Discovery's test sets carry no Id and must not warn, got:\n%s", strings.Join(lines, "\n"))
	}
	if got := sent(); len(got) != 2 {
		t.Fatalf("the test sets must still send their fakes, sent %d packets", len(got))
	}
}

func TestADiscoveryRunDoesNotSpendARealSetsWarning(t *testing.T) {
	resetTimestampFallbackLog(t)
	buf := captureLog(t)
	set := fakeSNITestSet("user set", "timestamp")
	set.Id = "user-set-id"
	w := newDualStackInjectingWorker(t, &set)
	w.getConfig().Queue.IsDiscovery = true
	captureInjected(t, w)

	sendFakeSNIFor(w, &set, fakeSNIOriginal(IPv4, "", nil))

	log.Flush()
	if lines := timestampFallbackLines(buf); len(lines) != 0 {
		t.Fatalf("a Discovery probe runs from the router and must not warn, got:\n%s", strings.Join(lines, "\n"))
	}
	if _, spent := tsFallbackLog.Load("user-set-id"); spent {
		t.Fatal("a Discovery probe must leave the set's one warning for its real traffic")
	}
}

func TestEachSetWarnsOnceWhileB4Runs(t *testing.T) {
	resetTimestampFallbackLog(t)
	buf := captureLog(t)
	first := fakeSNITestSet("first set", "timestamp")
	first.Id = "first-id"
	second := fakeSNITestSet("second set", "timestamp")
	second.Id = "second-id"
	w := newDualStackInjectingWorker(t, &first, &second)
	captureInjected(t, w)

	for range 5 {
		sendFakeSNIFor(w, &first, fakeSNIOriginal(IPv4, "", nil))
	}
	sendFakeSNIFor(w, &second, fakeSNIOriginal(IPv4, "", nil))
	renamed := first
	renamed.Name = "first set renamed"
	sendFakeSNIFor(w, &renamed, fakeSNIOriginal(IPv4, "", nil))

	log.Flush()
	lines := timestampFallbackLines(buf)
	if len(lines) != 2 {
		t.Fatalf("want one warning per set for the whole run, got %d:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"first set"`) || !strings.Contains(lines[1], `"second set"`) {
		t.Fatalf("each warning must name its own set:\n%s", strings.Join(lines, "\n"))
	}
	if _, ok := tsFallbackLog.Load("first-id"); !ok {
		t.Fatal("the warning must be remembered by the set Id")
	}
}

func TestTheTunFallbackWarningNamesTheDeviceBehindTheRouter(t *testing.T) {
	thisHost(t)
	resetTimestampFallbackLog(t)
	buf := captureLog(t)
	set := newFakingSet(false)
	set.Id = "tun-ts"
	set.Name = "tun-ts"
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	w.srcResolver = newResolverWith(t, tunTestWAN,
		"ipv4     2 tcp      6 431999 ESTABLISHED src=192.168.1.50 dst=1.2.3.4 sport=49874 dport=443 src=1.2.3.4 dst="+tunTestWAN+" sport=443 dport=44092 [ASSURED] use=1\n")

	pkt := makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000)
	copy(pkt[12:16], net.ParseIP(tunTestWAN).To4())
	binary.BigEndian.PutUint16(pkt[20:22], 44092)
	sock.FixIPv4Checksum(pkt[:20])
	sock.FixTCPChecksum(pkt)

	w.ProcessPacket(pkt)
	if len(sent()) == 0 {
		t.Fatal("the ClientHello must go out with its fake")
	}

	log.Flush()
	lines := timestampFallbackLines(buf)
	if len(lines) != 1 {
		t.Fatalf("want one fallback warning, got %d:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "from 192.168.1.50:49874 to 1.2.3.4:443") {
		t.Errorf("the warning must name the device and its own port from conntrack: %s", lines[0])
	}
	if strings.Contains(lines[0], tunTestWAN) || strings.Contains(lines[0], ":44092") {
		t.Errorf("the warning must not name the router's WAN address or its NAT port: %s", lines[0])
	}
}

func rfc1071TCPChecksumValid(pkt []byte) bool {
	var pseudo, segment []byte
	if pkt[0]>>4 == IPv4 {
		ihl := int(pkt[0]&0x0f) * 4
		segment = pkt[ihl:int(binary.BigEndian.Uint16(pkt[2:4]))]
		pseudo = append(append([]byte(nil), pkt[12:20]...), 0, 6)
		pseudo = binary.BigEndian.AppendUint16(pseudo, uint16(len(segment)))
	} else {
		segment = pkt[IPv6HeaderLen : IPv6HeaderLen+int(binary.BigEndian.Uint16(pkt[4:6]))]
		pseudo = append([]byte(nil), pkt[8:40]...)
		pseudo = binary.BigEndian.AppendUint32(pseudo, uint32(len(segment)))
		pseudo = append(pseudo, 0, 0, 0, 6)
	}
	var sum uint32
	for _, part := range [][]byte{pseudo, segment} {
		for i := 0; i+1 < len(part); i += 2 {
			sum += uint32(part[i])<<8 | uint32(part[i+1])
		}
		if len(part)%2 == 1 {
			sum += uint32(part[len(part)-1]) << 8
		}
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return sum == 0xffff
}

func TestATimestampFakeWithoutTimestampsGoesOutWithAWrongChecksumAndAck(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			resetTimestampFallbackLog(t)
			captureLog(t)
			set := fakeSNITestSet("ts-wire", "timestamp")
			set.Faking.SNISeqLength = 2
			set.Faking.MD5OnFake = true
			w := newDualStackInjectingWorker(t, &set)
			sent := captureInjected(t, w)

			original := fakeSNIOriginal(ver, "", nil)
			if !rfc1071TCPChecksumValid(original) {
				t.Fatal("the checksum helper rejects a valid original")
			}
			l3, ttlAt := 20, 8
			if ver == IPv6 {
				l3, ttlAt = IPv6HeaderLen, 7
			}
			origSeq := binary.BigEndian.Uint32(original[l3+4 : l3+8])
			origAck := binary.BigEndian.Uint32(original[l3+8 : l3+12])

			sendFakeSNIFor(w, &set, original)

			got := sent()
			if len(got) != 2 {
				t.Fatalf("want two fakes, sent %d packets", len(got))
			}
			wantSeq := origSeq
			for i, fake := range got {
				tcpHL := int(fake[l3+12]>>4) * 4
				if lengths := md5OptionLengths(fake); len(lengths) != 1 || lengths[0] != 18 {
					t.Errorf("fake %d: want one MD5 option of length 18, got %v", i, lengths)
				}
				if ack := binary.BigEndian.Uint32(fake[l3+8 : l3+12]); ack != origAck+0x60000000 {
					t.Errorf("fake %d: ack %#x, want %#x", i, ack, origAck+0x60000000)
				}
				if seq := binary.BigEndian.Uint32(fake[l3+4 : l3+8]); seq != wantSeq {
					t.Errorf("fake %d: seq %d, want %d", i, seq, wantSeq)
				}
				if fake[ttlAt] != original[ttlAt] {
					t.Errorf("fake %d: TTL %d, want the original %d", i, fake[ttlAt], original[ttlAt])
				}
				if rfc1071TCPChecksumValid(fake) {
					t.Errorf("fake %d: the TCP checksum must be wrong", i)
				}
				wantSeq += uint32(len(fake) - l3 - tcpHL)
			}
		})
	}
}
