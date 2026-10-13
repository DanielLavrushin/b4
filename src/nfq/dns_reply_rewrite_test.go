package nfq

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

func echCoverWorker(t *testing.T) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{echTestSet("cloudflare-ech", true, "cloudflare-ech.com")}
	return passiveDNSWorker(t, &cfg)
}

func handleReply(t *testing.T, w *Worker, vc *verdictCtx, raw []byte) {
	t.Helper()
	pkt, ok := w.parseIPHeaders(raw)
	if !ok {
		t.Fatal("the reply does not parse")
	}
	udp := pkt.raw[pkt.ihl:]
	w.processDnsPacket(vc, pkt, binary.BigEndian.Uint16(udp[0:2]), binary.BigEndian.Uint16(udp[2:4]), udp[UDPHeaderLen:])
}

func masqueradedReply(payload []byte) []byte {
	return replyRaw(payload, rigOutside, rigWAN, 40000)
}

type sendFailingModQueue struct{ verdicts chan modVerdict }

func (q *sendFailingModQueue) SetVerdict(id uint32, verdict int) error {
	q.verdicts <- modVerdict{verdict: verdict}
	return nil
}

func (q *sendFailingModQueue) SetVerdictModPacket(uint32, int, []byte) error {
	return errors.New("netlink send failed")
}

func keepsItsPacket(t *testing.T, v modVerdict, raw []byte) []byte {
	t.Helper()
	if v.verdict != nfqueue.NfAccept || v.packet == nil {
		t.Fatalf("verdict %d carrying %d bytes: b4 dropped the reply and sent a packet of its own, which passes b4's CONNMARK rule and marks the device's connection, so b4 skips its later replies on that socket", v.verdict, len(v.packet))
	}
	if !bytes.Equal(v.packet[12:20], raw[12:20]) || !bytes.Equal(v.packet[20:24], raw[20:24]) {
		t.Fatalf("the rewritten reply changed its addresses or ports: %x, was %x", v.packet[12:24], raw[12:24])
	}
	if v.packet[8] != raw[8] || !bytes.Equal(v.packet[4:6], raw[4:6]) {
		t.Fatalf("the rewritten reply lost its own header: TTL %d id %x, was TTL %d id %x", v.packet[8], v.packet[4:6], raw[8], raw[4:6])
	}
	return rewritten(t, v, raw)
}

func TestAnECHStrippedReplyIsRewrittenInPlace(t *testing.T) {
	w := echCoverWorker(t)
	sent := captureSends(t)
	q := newFakeModQueue()
	raw := masqueradedReply(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com"))

	handleReply(t, w, &verdictCtx{id: 1, q: q}, raw)

	if dns.ParseResponseECH(keepsItsPacket(t, q.now(t), raw)) != nil {
		t.Fatal("the rewritten reply still carries the ECH key")
	}
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
}

func TestAnIPv6ReplyIsRewrittenInPlace(t *testing.T) {
	w := echCoverWorker(t)
	sent := captureSends(t)
	q := newFakeModQueue()
	raw := makeV6UDPPacketWithExt(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com"), 53, 40000, 0, nil)
	raw[0], raw[1], raw[2], raw[3], raw[7] = 0x6b, 0x81, 0x23, 0x45, 57

	handleReply(t, w, &verdictCtx{id: 7, q: q}, raw)

	v := q.now(t)
	if v.verdict != nfqueue.NfAccept || v.packet == nil {
		t.Fatalf("verdict %d carrying %d bytes, want the reply accepted carrying the rewritten packet", v.verdict, len(v.packet))
	}
	p := v.packet
	if !bytes.Equal(p[0:4], raw[0:4]) || p[6] != 17 || p[7] != raw[7] || !bytes.Equal(p[8:44], raw[8:44]) {
		t.Fatalf("the rewritten reply lost its own header: %x, was %x", p[:44], raw[:44])
	}
	if int(binary.BigEndian.Uint16(p[4:6])) != len(p)-40 || int(binary.BigEndian.Uint16(p[44:46])) != len(p)-40 {
		t.Fatalf("payload length %d and UDP length %d for a %d-byte packet", binary.BigEndian.Uint16(p[4:6]), binary.BigEndian.Uint16(p[44:46]), len(p))
	}
	check := append([]byte(nil), p...)
	sock.FixUDPChecksumV6(check)
	if !bytes.Equal(check[46:48], p[46:48]) {
		t.Fatalf("UDP checksum %x, want %x", p[46:48], check[46:48])
	}
	if dns.ParseResponseECH(p[48:]) != nil {
		t.Fatal("the rewritten reply still carries the ECH key")
	}
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
}

func TestAnIPv4FallbackReplyIsRewrittenInPlace(t *testing.T) {
	w, _ := holdWorker(t, false)
	stubRouteAwait(t)
	sent := captureSends(t)
	q := newFakeModQueue()
	raw := masqueradedReply(buildTestMixedResponse("www.example.com", dnsTypeA, "203.0.113.11", "2001:db8::11"))

	handleReply(t, w, &verdictCtx{id: 2, q: q}, raw)

	onlyAddress(t, keepsItsPacket(t, q.now(t), raw), "203.0.113.11")
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
}

func TestARewrittenReplyWaitsInTheQueueForItsRoutes(t *testing.T) {
	w, _ := holdWorker(t, false)
	gate := make(chan struct{})
	open := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(func() {
		open()
		holdsDone(t, w)
	})
	stubRouteAwait(t, gate)
	sent := captureSends(t)
	q := newFakeModQueue()
	raw := masqueradedReply(buildTestMixedResponse("www.example.com", dnsTypeA, "203.0.113.12", "2001:db8::12"))

	handleReply(t, w, &verdictCtx{id: 3, q: q}, raw)
	q.none(t, 40*time.Millisecond)
	open()
	onlyAddress(t, keepsItsPacket(t, q.within(t, 5*time.Second), raw), "203.0.113.12")
	holdsDone(t, w)
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
}

func TestARewriteFallsBackToDropAndSend(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vc      func() (*verdictCtx, func(t *testing.T) int)
		dropped int
	}{
		{"a queue that cannot carry a modified packet", func() (*verdictCtx, func(t *testing.T) int) {
			q := newFakeQueue()
			return &verdictCtx{id: 4, q: q}, func(t *testing.T) int { return q.now(t) }
		}, nfqueue.NfDrop},
		{"the modified verdict fails to send", func() (*verdictCtx, func(t *testing.T) int) {
			q := &sendFailingModQueue{verdicts: make(chan modVerdict, 4)}
			return &verdictCtx{id: 5, q: q}, func(t *testing.T) int {
				t.Helper()
				select {
				case v := <-q.verdicts:
					return v.verdict
				case <-time.After(time.Second):
					t.Fatal("no verdict after the failed rewrite")
				}
				return -1
			}
		}, nfqueue.NfDrop},
		{"the TUN engine, which has no queue", func() (*verdictCtx, func(t *testing.T) int) {
			vc := &verdictCtx{verdict: engine.VerdictAccept}
			return vc, func(t *testing.T) int { return int(vc.verdict) }
		}, int(engine.VerdictDrop)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := echCoverWorker(t)
			sent := captureSends(t)
			vc, verdict := tc.vc()
			raw := masqueradedReply(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com"))

			handleReply(t, w, vc, raw)

			if got := verdict(t); got != tc.dropped {
				t.Fatalf("verdict %d, want the reply dropped", got)
			}
			pkt := sent.one(t)
			if !bytes.Equal(pkt[12:16], net.ParseIP(rigOutside).To4()) || !bytes.Equal(pkt[16:20], net.ParseIP(rigWAN).To4()) ||
				binary.BigEndian.Uint16(pkt[22:24]) != 40000 {
				t.Fatalf("sent %x, want the resolver's address and port 53 to the reply's own destination and port", pkt[:24])
			}
			if dns.ParseResponseECH(pkt[28:]) != nil {
				t.Fatal("the answer sent in place of the reply still carries the ECH key")
			}
		})
	}
}

func TestAnUnclaimedCopiedReplyIsRewrittenInPlace(t *testing.T) {
	injectAfter(t, time.Minute)
	stubRouteAwait(t)
	r := newCopyRig(t, nil, echTestSet("cloudflare-ech", true, "cloudflare-ech.com"))
	sent := captureSends(t)

	r.read(copyOf(nfHookForward, rigDevice, rigOutside, 40000, dns.BuildQuery("steamdb.info", 0x4242, dnsTypeHTTPS)))
	if live := r.l.claims.live.Load(); live != 0 {
		t.Fatalf("%d claims for a name no set acts on, want none", live)
	}
	q := newFakeModQueue()
	raw := masqueradedReply(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com"))
	r.reply(q, raw)

	if dns.ParseResponseECH(keepsItsPacket(t, q.now(t), raw)) != nil {
		t.Fatal("the rewritten reply still carries the ECH key")
	}
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
}

func TestAFilteredReplyIsNotSwallowedByAPendingClaim(t *testing.T) {
	for _, tc := range []struct {
		name    string
		queue   func() verdictSetter
		inPlace bool
	}{
		{"rewritten in place", func() verdictSetter { return newFakeModQueue() }, true},
		{"sent after the modified verdict fails to send", func() verdictSetter { return &sendFailingModQueue{verdicts: make(chan modVerdict, 4)} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			injectAfter(t, time.Minute)
			stubRouteAwait(t)
			r := newCopyRig(t, nil, echTestSet("cloudflare-ech", true, "cloudflare-ech.com"))
			sent := captureSends(t)

			key := dnsClaimKey{ver: IPv4, txid: 0x4242, qtype: dnsTypeHTTPS, name: "steamdb.info"}
			c, outcome := r.l.claims.begin(key, netip.MustParseAddr(rigDevice), netip.MustParseAddr(rigRouter), 40000, time.Now(), time.Minute)
			if outcome != claimBeginPrimary {
				t.Fatalf("begin: outcome %d", outcome)
			}
			defer r.l.finish(&dnsCopyDecision{claim: c})

			q := tc.queue()
			raw := replyRaw(httpsAnswerWithECH("steamdb.info", dnsTypeHTTPS, "cloudflare-ech.com"), rigRouter, rigDevice, 40000)
			handleReply(t, r.w, &verdictCtx{id: 6, q: q}, raw)

			if tc.inPlace {
				if dns.ParseResponseECH(keepsItsPacket(t, q.(*fakeModQueue).now(t), raw)) != nil {
					t.Fatal("the rewritten reply still carries the ECH key")
				}
				sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
				return
			}
			if dns.ParseResponseECH(sent.one(t)[28:]) != nil {
				t.Fatal("the answer sent in place of the reply still carries the ECH key")
			}
		})
	}
}
