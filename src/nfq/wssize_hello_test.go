package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/sock"
)

func wsHelloRecords(hello []byte, cuts ...int) []byte {
	body := hello[5:]
	var wire []byte
	start := 0
	for _, end := range append(cuts, len(body)) {
		wire = append(wire, 22, 3, 3, byte((end-start)>>8), byte(end-start))
		wire = append(wire, body[start:end]...)
		start = end
	}
	return wire
}

func wsHelloPacket(ver byte, payload []byte, seq uint32) []byte {
	if ver == IPv6 {
		p := makeV6TCPPacket(payload, seq)
		binary.BigEndian.PutUint16(p[54:56], 60000)
		sock.FixTCPChecksumV6(p)
		return p
	}
	p := makeV4TCPPacket(payload, seq)
	binary.BigEndian.PutUint16(p[34:36], 60000)
	sock.FixIPv4Checksum(p[:20])
	sock.FixTCPChecksum(p)
	return p
}

func TestWSSizeHelloOriginalStream(t *testing.T) {
	for _, ver := range []byte{IPv4, IPv6} {
		for _, isn := range []uint32{100, 0xfffffff0} {
			t.Run(fmt.Sprintf("v%d-isn%d", ver, isn), func(t *testing.T) {
				cfg, set := wsFixture()
				set.TCP.WSSize.Cutoff = "client_hello"
				c, now := newWSSizeCache(), time.Now()
				at := 20
				if ver == IPv6 {
					at = 40
				}
				syn := synWithOptions(ver, []byte{3, 3, 7, 0}, nil, isn)
				if !c.rewrite(syn, at, set, cfg, now) {
					t.Fatal("opening not rewritten")
				}
				key, _ := streamKeyOf(syn, at)
				hello := wsHelloRecords(buildClientHello("original.example", 700, 0xab), 2, 40)
				run := func(offset int, data []byte, want bool) {
					t.Helper()
					p := wsHelloPacket(ver, data, isn+1+uint32(offset))
					before := append([]byte(nil), p...)
					if got := c.rewrite(p, at, nil, cfg, now); got != want {
						t.Fatalf("offset%d len%d rewritten%v want%v", offset, len(data), got, want)
					}
					hlen := int(p[at+12]>>4) * 4
					if !bytes.Equal(p[at+hlen:], data) || !bytes.Equal(p[at:at+14], before[at:at+14]) {
						t.Fatal("original payload or sequence altered")
					}
					if want && binary.BigEndian.Uint16(p[at+14:]) != 1 {
						t.Fatal("window missing")
					}
					if !want && !bytes.Equal(p, before) {
						t.Fatal("later packet modified")
					}
					if ver == IPv4 && (!sock.TCPChecksumValid(p) || !ipv4HeaderChecksumValid(p[:20])) || ver == IPv6 && !sock.TCPChecksumValidV6(p) {
						t.Fatal("invalid real-packet checksum")
					}
				}
				run(0, hello[:2], true)
				run(-2147483648, hello[:2], true)
				run(len(hello)-80, hello[len(hello)-80:], true)
				run(0, hello[:2], true)
				if c.flows[key].cutoff {
					t.Fatal("gap or duplicate completed ClientHello")
				}
				run(2, hello[2:len(hello)-40], true)
				if !c.flows[key].cutoff || c.flows[key].helloEnd != len(hello) || c.helloBytes != 0 {
					t.Fatalf("boundary or allocation: %+v bytes%d", c.flows[key], c.helloBytes)
				}
				run(len(hello), []byte("later application data"), false)
				run(len(hello), nil, false)
				run(0, hello[:100], true)
				run(len(hello)-1, append(hello[len(hello)-1:], 99), true)
				now = now.Add(wsSizeHelloTimeout)
				run(0, hello[:100], false)
			})
		}
	}
}

func TestWSSizeHelloFallbacks(t *testing.T) {
	hello := buildClientHello("original.example", 700, 0xab)
	for _, why := range []string{"non-tls", "bad-version", "empty-record", "oversized-record", "other-handshake", "oversized-message", "empty-message", "conflicting-overlap", "outside-limit", "packet-budget", "memory-budget"} {
		t.Run(why, func(t *testing.T) {
			cfg, set := wsFixture()
			set.TCP.WSSize.Cutoff = "client_hello"
			c, now := newWSSizeCache(), time.Now()
			syn := synWithOptions(IPv4, nil, nil, 100)
			c.rewrite(syn, 20, set, cfg, now)
			key, _ := streamKeyOf(syn, 20)
			payload, seq := append([]byte(nil), hello...), uint32(101)
			switch why {
			case "non-tls":
				payload = []byte("GET / HTTP/1.1\r\n")
			case "bad-version":
				payload[1] = 4
			case "empty-record":
				payload[3], payload[4] = 0, 0
			case "oversized-record":
				payload[3], payload[4] = 0x40, 1
			case "other-handshake":
				payload[5] = 2
			case "oversized-message":
				payload[6], payload[7], payload[8] = 1, 0, 0
			case "empty-message":
				payload[6], payload[7], payload[8] = 0, 0, 0
			case "conflicting-overlap":
				c.rewrite(makeV4TCPPacket(hello[:20], 101), 20, nil, cfg, now)
				payload[19] ^= 1
			case "outside-limit":
				seq += wsSizeHelloMax
			case "packet-budget":
				for i := 0; i < wsSizeHelloPackets; i++ {
					c.rewrite(makeV4TCPPacket(hello[:10], 101), 20, nil, cfg, now)
				}
			case "memory-budget":
				c.helloBytes = wsSizeHelloBudget
			}
			if !c.rewrite(makeV4TCPPacket(payload, seq), 20, nil, cfg, now) {
				t.Fatal("fallback triggering packet must be included")
			}
			if !c.flows[key].cutoff || c.flows[key].hello != nil || c.flows[key].helloEnd != 0 {
				t.Fatal("fallback retained tracking")
			}
			if why != "memory-budget" && c.helloBytes != 0 {
				t.Fatal("fallback allocation leaked")
			}
			if c.rewrite(makeV4TCPPacket(hello, 101), 20, nil, cfg, now) {
				t.Fatal("fallback reactivated")
			}
		})
	}
}

func TestWSSizeHelloCleanup(t *testing.T) {
	for _, why := range []string{"timeout-packet", "timeout-cleanup", "expiry", "config", "new-syn", "fin", "rst", "incoming-rst"} {
		t.Run(why, func(t *testing.T) {
			cfg, set := wsFixture()
			set.TCP.WSSize.Cutoff = "client_hello"
			c, now := newWSSizeCache(), time.Now()
			syn := synWithOptions(IPv4, nil, nil, 100)
			c.rewrite(syn, 20, set, cfg, now)
			c.rewrite(makeV4TCPPacket([]byte{22, 3}, 101), 20, nil, cfg, now)
			if c.helloBytes == 0 {
				t.Fatal("fixture did not allocate")
			}
			switch why {
			case "timeout-packet":
				if c.rewrite(makeV4TCPPacket([]byte{22}, 101), 20, nil, cfg, now.Add(wsSizeHelloTimeout)) {
					t.Fatal("deadline packet modified")
				}
			case "timeout-cleanup":
				c.cleanup(now.Add(wsSizeHelloTimeout))
			case "expiry":
				c.cleanup(now.Add(wsSizeTTL))
			case "config":
				other := *cfg
				c.rewrite(makeV4TCPPacket(nil, 101), 20, nil, &other, now)
			case "new-syn":
				c.rewrite(synWithOptions(IPv4, nil, nil, 200), 20, set, cfg, now)
			case "fin":
				c.rewrite(makeV4TCPPacketFlags(nil, 103, 0x11), 20, nil, cfg, now)
			case "rst":
				c.rewrite(makeV4TCPPacketFlags(nil, 103, 0x14), 20, nil, cfg, now)
			case "incoming-rst":
				p := makeV4TCPPacketFlags(nil, 10, 0x14)
				copy(p[12:16], syn[16:20])
				copy(p[16:20], syn[12:16])
				copy(p[20:22], syn[22:24])
				copy(p[22:24], syn[20:22])
				c.closeIncoming(p, 20)
			}
			if c.helloBytes != 0 {
				t.Fatalf("%d bytes leaked", c.helloBytes)
			}
		})
	}
}

func TestWSSizeHelloFastOpenAndBytewise(t *testing.T) {
	cfg, set := wsFixture()
	set.TCP.WSSize.Cutoff = "client_hello"
	hello := buildClientHello("original.example", 0, 0)
	for _, count := range []int{1, len(hello)} {
		c, now := newWSSizeCache(), time.Now()
		if !c.rewrite(synWithOptions(IPv4, nil, hello[:count], 100), 20, set, cfg, now) {
			t.Fatal("Fast Open omitted")
		}
		for i := count; i < len(hello); i++ {
			if !c.rewrite(makeV4TCPPacket(hello[i:i+1], 101+uint32(i)), 20, nil, cfg, now) {
				t.Fatalf("byte %d omitted", i)
			}
		}
		if c.helloBytes != 0 || c.rewrite(makeV4TCPPacket([]byte{0}, 101+uint32(len(hello))), 20, nil, cfg, now) {
			t.Fatal("ClientHello did not complete")
		}
	}
}

func TestWSSizeHelloBudgetAndConcurrency(t *testing.T) {
	cfg, set := wsFixture()
	set.TCP.WSSize.Cutoff = "client_hello"
	c, now := newWSSizeCache(), time.Now()
	hello := buildClientHello("original.example", 700, 0xab)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 32; i++ {
				port := uint16(2000 + worker*32 + i)
				p := synWithOptions(IPv4, nil, nil, 100)
				binary.BigEndian.PutUint16(p[20:22], port)
				c.rewrite(p, 20, set, cfg, now)
				p = makeV4TCPPacket(hello[:20], 101+wsSizeHelloMax-20)
				binary.BigEndian.PutUint16(p[20:22], port)
				c.rewrite(p, 20, nil, cfg, now)
				c.cleanup(now)
			}
		}(worker)
	}
	wg.Wait()
	if c.helloBytes > wsSizeHelloBudget || c.helloBytes == 0 {
		t.Fatalf("buffer budget %d", c.helloBytes)
	}
	c.cleanup(now.Add(wsSizeHelloTimeout))
	if c.helloBytes != 0 {
		t.Fatal("global budget leaked")
	}
}

func TestWSSizeHelloWireLimit(t *testing.T) {
	base := buildClientHello("x", 0, 0xab)
	hello := wsHelloRecords(buildClientHello("x", wsSizeHelloMax-len(base)-15, 0xab), 16384, 32768, 49152)
	if len(hello) != wsSizeHelloMax {
		t.Fatal("fixture does not reach the wire limit")
	}
	cfg, set := wsFixture()
	set.TCP.WSSize.Cutoff = "client_hello"
	c, now := newWSSizeCache(), time.Now()
	syn := synWithOptions(IPv4, nil, nil, 100)
	c.rewrite(syn, 20, set, cfg, now)
	key, _ := streamKeyOf(syn, 20)
	for at := 0; at < len(hello); at += 4096 {
		if !c.rewrite(wsHelloPacket(IPv4, hello[at:at+4096], 101+uint32(at)), 20, nil, cfg, now) {
			t.Fatalf("wire limit rejected at offset %d", at)
		}
	}
	if c.flows[key].helloEnd != len(hello) || c.helloBytes != 0 {
		t.Fatal("maximum ClientHello not completed")
	}
}
