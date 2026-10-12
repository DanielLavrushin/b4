package nfq

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/florianl/go-nfqueue"
)

var resolverIPs = []net.IP{net.ParseIP("198.51.100.7")}

func TestCopiedPinRewritesTheRouterResolversReply(t *testing.T) {
	injectAfter(t, time.Minute)
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())
	replies := watchReplies(t)

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x101, dnsTypeA)))
	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x101, "pin.example.com", resolverIPs), rigRouter, rigDevice, 40000)
	r.reply(q, raw)

	onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.12")
	noReply(t, replies, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the resolver's reply")
	if got := r.l.stats.repliesReplaced.Load(); got != 1 {
		t.Fatalf("replies_replaced = %d, want 1", got)
	}
}

func TestCopiedPinIsInjectedWhenTheRouterResolverIsSilent(t *testing.T) {
	injectAfter(t, 5*time.Millisecond)
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())
	sent := captureSends(t)

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x102, dnsTypeA)))
	pkt := sent.one(t)
	if !bytes.Equal(pkt[12:16], net.ParseIP(rigRouter).To4()) || !bytes.Equal(pkt[16:20], net.ParseIP(rigDevice).To4()) ||
		binary.BigEndian.Uint16(pkt[20:22]) != 53 || binary.BigEndian.Uint16(pkt[22:24]) != 40000 {
		t.Fatalf("injected %x, want an answer from the router's port 53 to the device's port 40000", pkt[:28])
	}
	injected := pkt[28:]
	onlyAddress(t, injected, "203.0.113.12")

	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x102, "pin.example.com", resolverIPs), rigRouter, rigDevice, 40000)
	r.reply(q, raw)
	if got := rewritten(t, q.now(t), raw); !bytes.Equal(got, injected) {
		t.Fatalf("the late reply carries %x, want the injected answer %x", got, injected)
	}
	sent.none(t, 30*time.Millisecond, "the pinned answer was injected twice")
	if got := r.l.stats.injections.Load(); got != 1 {
		t.Fatalf("injections = %d, want 1", got)
	}
}

func TestCopiedPinWaitsForItsRoutes(t *testing.T) {
	t.Run("the rewrite waits for the set", func(t *testing.T) {
		injectAfter(t, time.Minute)
		gate := make(chan struct{})
		stubRouteAwait(t, gate)
		r := newCopyRig(t, nil, copyPinSet())
		r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x103, dnsTypeA)))
		q := newFakeModQueue()
		raw := replyRaw(buildDNSResponse(0x103, "pin.example.com", resolverIPs), rigRouter, rigDevice, 40000)
		r.reply(q, raw)
		q.none(t, 40*time.Millisecond)
		close(gate)
		onlyAddress(t, rewritten(t, q.within(t, 5*time.Second), raw), "203.0.113.12")
		holdsDone(t, r.w)
	})
	t.Run("the injection waits for the set", func(t *testing.T) {
		injectAfter(t, 5*time.Millisecond)
		gate := make(chan struct{})
		stubRouteAwait(t, gate)
		r := newCopyRig(t, nil, copyPinSet())
		sent := captureSends(t)
		r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x104, dnsTypeA)))
		sent.none(t, 40*time.Millisecond, "the pinned answer was injected before its addresses were in the set")
		close(gate)
		onlyAddress(t, sent.one(t)[28:], "203.0.113.12")
	})
	t.Run("the rewrite waits no longer than the hold limit", func(t *testing.T) {
		injectAfter(t, time.Minute)
		holdLimit(t, 40*time.Millisecond)
		stubRouteAwait(t, make(chan struct{}))
		r := newCopyRig(t, nil, copyPinSet())
		r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x105, dnsTypeA)))
		q := newFakeModQueue()
		raw := replyRaw(buildDNSResponse(0x105, "pin.example.com", resolverIPs), rigRouter, rigDevice, 40000)
		start := time.Now()
		r.reply(q, raw)
		onlyAddress(t, rewritten(t, q.within(t, 5*time.Second), raw), "203.0.113.12")
		if held := time.Since(start); held < 40*time.Millisecond {
			t.Fatalf("released after %s, before the 40ms limit", held)
		}
		holdsDone(t, r.w)
	})
	t.Run("the injection waits no longer than the hold limit", func(t *testing.T) {
		injectAfter(t, 5*time.Millisecond)
		holdLimit(t, 40*time.Millisecond)
		stubRouteAwait(t, make(chan struct{}))
		r := newCopyRig(t, nil, copyPinSet())
		sent := captureSends(t)
		start := time.Now()
		r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x106, dnsTypeA)))
		onlyAddress(t, sent.one(t)[28:], "203.0.113.12")
		if held := time.Since(start); held < 45*time.Millisecond {
			t.Fatalf("injected after %s, before the 5ms delay plus the 40ms limit", held)
		}
	})
}

func TestCopiedSinkholeRewritesToNXDOMAIN(t *testing.T) {
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil, copyBlockSet(config.BlockActionReject))
	replies := watchReplies(t)
	lines := connectionLines(t)

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("www.ads.example", 0x111, dnsTypeA)))
	if n := countAction(drainLines(lines), dnsActionSinkhole); n != 1 {
		t.Fatalf("%d %s rows, want one from the copy", n, dnsActionSinkhole)
	}
	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x111, "www.ads.example", resolverIPs), rigRouter, rigDevice, 40000)
	r.reply(q, raw)
	answer := rewritten(t, q.now(t), raw)
	if rcode, _ := dns.ResponseRcode(answer); rcode != dns.RcodeNXDomain || len(dns.ParseResponseIPs(answer)) != 0 {
		t.Fatalf("the device got rcode %d with %v, want NXDOMAIN", rcode, dns.ParseResponseIPs(answer))
	}
	noReply(t, replies, 30*time.Millisecond, "b4 sent its own NXDOMAIN although it rewrote the resolver's reply")
}

func TestCopiedBlockDropDropsEveryReply(t *testing.T) {
	injectAfter(t, 5*time.Millisecond)
	r := newCopyRig(t, nil, copyBlockSet(config.BlockActionDrop))
	sent := captureSends(t)
	lines := connectionLines(t)

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("www.ads.example", 0x112, dnsTypeA)))
	if n := countAction(drainLines(lines), dnsActionBlock); n != 1 {
		t.Fatalf("%d %s rows, want one from the copy", n, dnsActionBlock)
	}
	for i := range 2 {
		q := newFakeModQueue()
		r.reply(q, replyRaw(buildDNSResponse(0x112, "www.ads.example", resolverIPs), rigRouter, rigDevice, 40000))
		if v := q.now(t); v.verdict != nfqueue.NfDrop || v.packet != nil {
			t.Fatalf("reply %d: verdict %d carrying %d bytes, want it dropped", i+1, v.verdict, len(v.packet))
		}
	}
	sent.none(t, 40*time.Millisecond, "b4 answered a query its set drops")
	if got := r.l.stats.repliesDropped.Load(); got != 2 {
		t.Fatalf("replies_dropped = %d, want 2", got)
	}
}

func TestCopiedRedirectHoldsTheReplyUntilItsAnswer(t *testing.T) {
	query := func(txid uint16) []byte { return dns.BuildQuery("www.example.com", txid, dnsTypeA) }
	for _, source := range []struct {
		name string
		want string
		use  func(t *testing.T, r *copyRig, set *config.SetConfig)
	}{
		{"a UDP target answered from the fallback", "203.0.113.31", func(t *testing.T, r *copyRig, set *config.SetConfig) {
			set.DNS.TargetDNS = "192.0.2.53"
			dscpCoolDown(set.DNS.TargetDNS)
			rememberDNSAnswer("www.example.com", query(1), answerWith("203.0.113.31")(query(1)))
		}},
		{"a DoH stub", "203.0.113.32", func(t *testing.T, r *copyRig, set *config.SetConfig) {
			set.DNS.DoHURL = copyDoHServer(t, r.w, nil, answerWith("203.0.113.32")).url
		}},
	} {
		for _, replyFirst := range []bool{true, false} {
			order := "answer first"
			if replyFirst {
				order = "reply first"
			}
			t.Run(source.name+", "+order, func(t *testing.T) {
				injectAfter(t, time.Minute)
				holdLimit(t, time.Minute)
				gate := make(chan struct{})
				stubDSCPHooks(t, gate)
				set := copyRedirectSet()
				set.DSCP = config.SetDSCPConfig{Enabled: true, Value: 31}
				r := newCopyRig(t, nil, set)
				source.use(t, r, set)
				replies := watchReplies(t)
				if !replyFirst {
					close(gate)
				}

				r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, query(0x121)))
				if !replyFirst {
					holdsDone(t, r.w)
				}
				q := newFakeModQueue()
				raw := replyRaw(buildDNSResponse(0x121, "www.example.com", resolverIPs), rigRouter, rigDevice, 40000)
				r.reply(q, raw)
				if replyFirst {
					q.none(t, 40*time.Millisecond)
					close(gate)
					onlyAddress(t, rewritten(t, q.within(t, 5*time.Second), raw), source.want)
				} else {
					onlyAddress(t, rewritten(t, q.now(t), raw), source.want)
				}
				holdsDone(t, r.w)
				noReply(t, replies, 20*time.Millisecond, "b4 sent the redirect's answer as a packet of its own")
			})
		}
	}

	for _, tc := range []struct {
		name string
		use  func(t *testing.T, r *copyRig, set *config.SetConfig)
	}{
		{"a strict set whose DoH resolver fails", func(t *testing.T, r *copyRig, set *config.SetConfig) {
			set.DNS.Strict = true
			set.DNS.DoHURL = copyDoHServer(t, r.w, nil, func([]byte) []byte { return nil }).url
		}},
		{"a cooled-down target with nothing to fall back on", func(t *testing.T, r *copyRig, set *config.SetConfig) {
			set.DNS.TargetDNS = rigRouter
			dscpCoolDown(rigRouter)
		}},
	} {
		t.Run(tc.name+" answers SERVFAIL through the reply", func(t *testing.T) {
			injectAfter(t, time.Minute)
			set := copyRedirectSet()
			r := newCopyRig(t, nil, set)
			tc.use(t, r, set)
			replies := watchReplies(t)

			r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, query(0x122)))
			holdsDone(t, r.w)
			q := newFakeModQueue()
			raw := replyRaw(buildDNSResponse(0x122, "www.example.com", resolverIPs), rigRouter, rigDevice, 40000)
			r.reply(q, raw)
			if rcode, _ := dns.ResponseRcode(rewritten(t, q.now(t), raw)); rcode != dns.RcodeServFail {
				t.Fatalf("the device got rcode %d, want SERVFAIL", rcode)
			}
			noReply(t, replies, 20*time.Millisecond, "b4 sent its SERVFAIL as a packet of its own")
		})
	}
}

func TestMasqueradedReplyMatchesByServerAndPort(t *testing.T) {
	injectAfter(t, 5*time.Millisecond)
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())
	sent := captureSends(t)

	r.read(copyOf(nfHookForward, rigDevice, rigOutside, 40000, dns.BuildQuery("pin.example.com", 0x131, dnsTypeA)))
	sent.none(t, 40*time.Millisecond, "b4 injected an answer for a query the router forwards, which conntrack would drop")

	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x131, "pin.example.com", resolverIPs), rigOutside, rigWAN, 40000)
	r.reply(q, raw)
	onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.12")

	other := newFakeModQueue()
	r.reply(other, replyRaw(buildDNSResponse(0x131, "pin.example.com", resolverIPs), rigOutside, rigWAN, 40001))
	passedUnchanged(t, other.now(t), "a reply to another masqueraded port")
	sent.none(t, 20*time.Millisecond, "b4 sent a packet for a masqueraded query")
}

func TestOtherClientsAndB4OwnLookupsDoNotMatch(t *testing.T) {
	injectAfter(t, time.Minute)
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())

	r.read(copyOf(nfHookForward, rigDevice, rigOutside, 40000, dns.BuildQuery("pin.example.com", 0x141, dnsTypeA)))
	for _, tc := range []struct {
		name string
		dst  string
		port uint16
	}{
		{"another port of the same device", rigDevice, 40001},
		{"another device on the same port", "192.168.1.101", 40000},
		{"b4's own lookup of the same question", rigWAN, 41000},
	} {
		q := newFakeModQueue()
		r.reply(q, replyRaw(buildDNSResponse(0x141, "pin.example.com", resolverIPs), rigOutside, tc.dst, tc.port))
		passedUnchanged(t, q.now(t), tc.name)
	}
	if got := r.l.stats.repliesReplaced.Load(); got != 0 {
		t.Fatalf("replies_replaced = %d, want none", got)
	}
}

func TestRouterUpstreamCopyOfAnActedNameIsQuiet(t *testing.T) {
	injectAfter(t, time.Minute)
	calls := stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())
	lines := connectionLines(t)

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x151, dnsTypeA)))
	r.read(copyOf(copyHookOutput, rigWAN, rigUpstream, 50000, dns.BuildQuery("pin.example.com", 0x7151, dnsTypeA)))
	if got := r.l.stats.claimsQuiet.Load(); got != 1 {
		t.Fatalf("claims_quiet = %d, want the router's upstream copy kept quiet", got)
	}
	if n := countAction(drainLines(lines), dnsActionPin); n != 1 {
		t.Fatalf("%d %s rows, want one: the router's own upstream lookup was acted on again", n, dnsActionPin)
	}
	learned := calls.Load()

	up := newFakeModQueue()
	r.reply(up, replyRaw(buildDNSResponse(0x7151, "pin.example.com", resolverIPs), rigUpstream, rigWAN, 50000))
	passedUnchanged(t, up.now(t), "the upstream reply of a quiet claim")
	if calls.Load() != learned {
		t.Fatal("the upstream reply of a quiet claim reached the set's address learning")
	}
	if got := r.l.stats.repliesQuiet.Load(); got != 1 {
		t.Fatalf("replies_quiet = %d, want 1", got)
	}

	dev := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x151, "pin.example.com", resolverIPs), rigRouter, rigDevice, 40000)
	r.reply(dev, raw)
	onlyAddress(t, rewritten(t, dev.now(t), raw), "203.0.113.12")

	r.read(copyOf(copyHookOutput, rigWAN, rigUpstream, 50001, dns.BuildQuery("pin.example.com", 0x7152, dnsTypeA)))
	if got := r.l.stats.claimsQuiet.Load(); got != 1 {
		t.Fatal("a router lookup after the device had its answer was kept quiet")
	}
	if n := countAction(drainLines(lines), dnsActionPin); n != 1 {
		t.Fatalf("%d %s rows for the router's own later lookup, want one", n, dnsActionPin)
	}
	own := newFakeModQueue()
	raw = replyRaw(buildDNSResponse(0x7152, "pin.example.com", resolverIPs), rigUpstream, rigWAN, 50001)
	r.reply(own, raw)
	onlyAddress(t, rewritten(t, own.now(t), raw), "203.0.113.12")
}

func TestRouterLookupsAreActedOnOnceTheDeviceWasAnswered(t *testing.T) {
	injectAfter(t, 5*time.Millisecond)
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())
	sent := captureSends(t)
	lines := connectionLines(t)

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x501, dnsTypeA)))
	onlyAddress(t, sent.one(t)[28:], "203.0.113.12")

	r.read(copyOf(copyHookOutput, rigWAN, rigUpstream, 50000, dns.BuildQuery("pin.example.com", 0x7501, dnsTypeA)))
	if got := r.l.stats.claimsQuiet.Load(); got != 0 {
		t.Fatalf("claims_quiet = %d: b4 had already answered the device itself, yet the router's own lookup of the name was passed with its real address", got)
	}
	if n := countAction(drainLines(lines), dnsActionPin); n != 2 {
		t.Fatalf("%d %s rows, want the device's and the router's own lookup", n, dnsActionPin)
	}
}

func heldQuery(t *testing.T, r *copyRig, q verdictSetter, query []byte, src, dst string, sport uint16) {
	t.Helper()
	pkt, ok := r.w.parseIPHeaders(makeV4UDPPacket(query, net.ParseIP(src), net.ParseIP(dst), sport, 53))
	if !ok {
		t.Fatal("the query did not parse")
	}
	udp := pkt.raw[pkt.ihl:]
	r.nextID++
	r.w.processDnsPacket(&verdictCtx{id: r.nextID, q: q}, pkt, sport, 53, udp[UDPHeaderLen:])
}

func TestHoldingAgainAfterCopyingStillAnswersHeldQueries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		set      func() *config.SetConfig
		domain   string
		hook     uint8
		dst      string
		nxdomain bool
	}{
		{"a pin for a query the router forwards", copyPinSet, "pin.example.com", nfHookForward, rigOutside, false},
		{"a pin for a query to the router", copyPinSet, "pin.example.com", copyHookInput, rigRouter, false},
		{"a sinkhole for a query the router forwards", func() *config.SetConfig { return copyBlockSet(config.BlockActionReject) }, "www.ads.example", nfHookForward, rigOutside, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			injectAfter(t, time.Minute)
			stubRouteAwait(t)
			r := newCopyRig(t, nil, tc.set())
			sent := captureSends(t)
			query := dns.BuildQuery(tc.domain, 0x777, dnsTypeA)
			r.read(copyOf(tc.hook, rigDevice, tc.dst, 40000, query))

			r.l.setFamilies(false, false)
			q := newFakeModQueue()
			heldQuery(t, r, q, query, rigDevice, tc.dst, 40000)
			if v := q.now(t); v.verdict != nfqueue.NfDrop {
				t.Fatalf("the held retransmit got verdict %d, want it dropped and answered", v.verdict)
			}
			answer := sent.one(t)[28:]
			if tc.nxdomain {
				if rcode, _ := dns.ResponseRcode(answer); rcode != dns.RcodeNXDomain {
					t.Fatalf("the held query got rcode %d, want NXDOMAIN", rcode)
				}
			} else {
				onlyAddress(t, answer, "203.0.113.12")
			}
		})
	}

	t.Run("a reply held for an open claim is still rewritten", func(t *testing.T) {
		injectAfter(t, time.Minute)
		holdLimit(t, time.Minute)
		gate := make(chan struct{})
		set := copyRedirectSet()
		r := newCopyRig(t, nil, set)
		set.DNS.DoHURL = copyDoHServer(t, r.w, gate, answerWith("203.0.113.44")).url
		r.read(copyOf(nfHookForward, rigDevice, rigOutside, 40000, dns.BuildQuery("www.example.com", 0x902, dnsTypeA)))
		q := newFakeModQueue()
		raw := replyRaw(buildDNSResponse(0x902, "www.example.com", resolverIPs), rigOutside, rigWAN, 40000)
		r.reply(q, raw)
		q.none(t, 40*time.Millisecond)

		r.l.setFamilies(false, false)
		close(gate)
		onlyAddress(t, rewritten(t, q.within(t, 2*time.Second), raw), "203.0.113.44")
	})
}

func questionless(query []byte) []byte {
	b := make([]byte, 12)
	copy(b[0:2], query[0:2])
	binary.BigEndian.PutUint16(b[2:4], 0x8185)
	return b
}

func TestAQuestionlessUpstreamAnswerIsDeliveredThroughTheReply(t *testing.T) {
	for _, tc := range []struct {
		name             string
		hook             uint8
		server, replyDst string
		injected         bool
	}{
		{"a query the router forwards", nfHookForward, rigOutside, rigWAN, false},
		{"a query to the router", copyHookInput, rigRouter, rigDevice, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			injectAfter(t, 5*time.Millisecond)
			holdLimit(t, time.Minute)
			set := copyRedirectSet()
			r := newCopyRig(t, func(c *config.Config) { c.System.DNS.QueryTimeoutSec = 1 }, set)
			set.DNS.DoHURL = copyDoHServer(t, r.w, nil, questionless).url
			sent := captureSends(t)
			query := dns.BuildQuery("www.example.com", 0x904, dnsTypeA)
			r.read(copyOf(tc.hook, rigDevice, tc.server, 40000, query))
			holdsDone(t, r.w)
			if tc.injected {
				if got := sent.one(t)[28:]; !bytes.Equal(got, questionless(query)) {
					t.Fatalf("injected %x, want the resolver's answer without a question section", got)
				}
			} else {
				sent.none(t, 30*time.Millisecond, "b4 sent its answer for a masqueraded query as a packet of its own, which conntrack drops")
			}

			q := newFakeModQueue()
			raw := replyRaw(buildDNSResponse(0x904, "www.example.com", resolverIPs), tc.server, tc.replyDst, 40000)
			r.reply(q, raw)
			if got := rewritten(t, q.within(t, 2*time.Second), raw); !bytes.Equal(got, questionless(query)) {
				t.Fatalf("the reply carries %x, want the resolver's answer without a question section", got)
			}
		})
	}
}

func TestDuplicateCopyJoinsTheFirstClaim(t *testing.T) {
	injectAfter(t, time.Minute)
	set := copyRedirectSet()
	r := newCopyRig(t, nil, set)
	doh := copyDoHServer(t, r.w, nil, answerWith("203.0.113.91"))
	set.DNS.DoHURL = doh.url
	lines := connectionLines(t)

	query := dns.BuildQuery("www.example.com", 0x161, dnsTypeA)
	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, query), copyOf(copyHookInput, rigDevice, rigRouter, 40000, query))
	holdsDone(t, r.w)
	if hits := doh.hits.Load(); hits != 1 {
		t.Fatalf("the set's resolver was asked %d times, want once", hits)
	}
	rows := 0
	for _, l := range drainLines(lines) {
		if strings.Contains(l, ","+dnsActionDoHPrefix) {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("%d redirect rows, want one", rows)
	}
	if got := r.l.stats.claimsAnswer.Load(); got != 1 {
		t.Fatalf("claims_answer = %d, want one decision", got)
	}

	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x161, "www.example.com", resolverIPs), rigRouter, rigDevice, 40000)
	r.reply(q, raw)
	onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.91")
}

func TestCopiedQueriesKeepPinsAheadOfTheResolverGuard(t *testing.T) {
	injectAfter(t, time.Minute)
	set := redirectSet("192.0.2.5", "")
	set.DNS.Pins = map[string][]string{"www.example.com": {"203.0.113.77"}}
	r := newCopyRig(t, nil, set)
	lines := connectionLines(t)
	replies := watchReplies(t)

	r.read(copyOf(nfHookForward, "192.0.2.5", "203.0.113.53", 40000, dns.BuildQuery("www.example.com", 0x171, dnsTypeA)))
	got := drainLines(lines)
	if countAction(got, dnsActionFromTarget) != 0 || countAction(got, dnsActionPin) != 1 {
		t.Fatalf("rows %q, want the pin to answer before the source is looked at", got)
	}
	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x171, "www.example.com", resolverIPs), "203.0.113.53", "192.0.2.5", 40000)
	r.reply(q, raw)
	onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.77")
	noReply(t, replies, 30*time.Millisecond, "a pinned name was answered twice")
}

func TestReplyBeforeTheReaderSawItsCopyIsClaimed(t *testing.T) {
	t.Run("a pinned name", func(t *testing.T) {
		injectAfter(t, time.Minute)
		stubRouteAwait(t)
		r := newCopyRig(t, nil, copyPinSet())
		r.src.hold(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x181, dnsTypeA)))

		q := newFakeModQueue()
		raw := replyRaw(buildDNSResponse(0x181, "pin.example.com", resolverIPs), rigRouter, rigDevice, 40000)
		r.reply(q, raw)
		onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.12")
	})
	t.Run("a passed name a proxy set wants to know", func(t *testing.T) {
		t.Cleanup(stopDNSRouteCleanup)
		names := withDNSNames(t, true)
		r := newCopyRig(t, nil, copyPinSet())
		r.src.hold(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("api.ipify.org", 0x182, dnsTypeA)))

		q := newFakeModQueue()
		r.reply(q, replyRaw(buildDNSResponse(0x182, "api.ipify.org", []net.IP{net.ParseIP("104.26.12.205")}), rigRouter, rigDevice, 40000))
		passedUnchanged(t, q.now(t), "a name no set acts on")
		if got := names.Lookup(net.ParseIP(rigDevice), net.ParseIP("104.26.12.205")).Own; len(got) != 1 || got[0] != "api.ipify.org" {
			t.Fatalf("names %v, want the copy drained before the reply recorded the name", got)
		}
	})
}

func TestAnswerWithoutAClaimTakesTodaysPath(t *testing.T) {
	gate := make(chan struct{})
	calls := stubRouteAwait(t, gate)
	names := withDNSNames(t, true)
	set := copyPinSet()
	set.DNS.Pins = nil
	r := newCopyRig(t, nil, set)

	q := newFakeModQueue()
	r.reply(q, replyRaw(buildDNSResponse(0x191, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}), rigRouter, rigDevice, 40000))
	if calls.Load() != 1 {
		t.Fatalf("the unclaimed answer reached the set %d times, want once", calls.Load())
	}
	q.none(t, 40*time.Millisecond)
	close(gate)
	passedUnchanged(t, q.within(t, 5*time.Second), "an answer held for its addresses")
	holdsDone(t, r.w)

	other := newFakeModQueue()
	r.reply(other, replyRaw(buildDNSResponse(0x192, "evil.example", []net.IP{net.ParseIP("93.184.216.34")}), rigRouter, rigDevice, 40000))
	passedUnchanged(t, other.now(t), "an answer to no query")
	if got := names.Lookup(nil, net.ParseIP("93.184.216.34")); !got.Empty() {
		t.Fatalf("an answer that matches no query was recorded: %+v", got)
	}
}

func escalationPair(threshold int) (*config.SetConfig, *config.SetConfig) {
	primary := config.NewSetConfig()
	primary.Id, primary.Name, primary.Enabled = "primary", "primary", true
	primary.Targets.DomainsToMatch = []string{"youtube.com"}
	primary.Escalate.To, primary.Escalate.DNSThreshold = "backup", threshold
	backup := config.NewSetConfig()
	backup.Id, backup.Name, backup.Enabled = "backup", "backup", true
	backup.Targets.DomainsToMatch = []string{"other.org"}
	return &primary, &backup
}

func TestClaimedRepliesSkipEscalationCounting(t *testing.T) {
	nx := func(query []byte) []byte { return dns.BuildBlockResponse(query) }
	failed := func(txid uint16) []byte { return nx(dns.BuildQuery("youtube.com", txid, dnsTypeA)) }

	t.Run("a resolver excluded from the set behind masquerade", func(t *testing.T) {
		resolver := "192.168.1.53"
		escalated := func(withCopies bool) bool {
			primary, backup := escalationPair(1)
			primary.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:01"}
			primary.Targets.SourceDevicesExclude = true
			r := newCopyRig(t, nil, primary, backup)
			r.w.ipToMac.Store(map[string]string{resolver: "AA:BB:CC:DD:EE:01"})
			if withCopies {
				r.read(copyOf(nfHookForward, resolver, rigOutside, 40000, dns.BuildQuery("youtube.com", 0x1a1, dnsTypeA)))
				if live := r.l.claims.live.Load(); live != 0 {
					t.Fatalf("%d claims recorded for a resolver the set excludes", live)
				}
			} else {
				r.w.dnsCopy = nil
			}
			q := newFakeModQueue()
			r.reply(q, replyRaw(failed(0x1a1), rigOutside, rigWAN, 40000))
			_, _, ok := r.w.destState.GetEscalation("youtube.com")
			return ok
		}
		if hold, copied := escalated(false), escalated(true); hold != copied {
			t.Fatalf("escalated with copies = %v, in hold mode = %v: the excluded resolver's reply must count as it did", copied, hold)
		}
	})

	for _, replies := range []int{1, 2} {
		name := "the resolver's upstream query hijacked to the router's own resolver"
		if replies == 2 {
			name = "a duplicated upstream reply"
		}
		t.Run(name, func(t *testing.T) {
			injectAfter(t, time.Minute)
			primary, backup := escalationPair(2)
			primary.DNS.Enabled = true
			r := newCopyRig(t, nil, primary, backup)
			primary.DNS.DoHURL = copyDoHServer(t, r.w, nil, nx).url

			r.read(copyOf(copyHookInput, "192.168.1.53", rigRouter, 40000, dns.BuildQuery("youtube.com", 0x1a2, dnsTypeA)))
			holdsDone(t, r.w)
			for range replies {
				q := newFakeModQueue()
				raw := replyRaw(failed(0x1a2), rigRouter, "192.168.1.53", 40000)
				r.reply(q, raw)
				rewritten(t, q.now(t), raw)
			}
			if _, _, ok := r.w.destState.GetEscalation("youtube.com"); ok {
				t.Fatal("a claimed reply counted toward escalation next to b4's own redirect answer")
			}

			q := newFakeModQueue()
			r.reply(q, replyRaw(failed(0x1a3), rigRouter, "192.168.1.53", 40001))
			if id, _, ok := r.w.destState.GetEscalation("youtube.com"); !ok || id != backup.Id {
				t.Fatalf("escalation %q ok=%v: an unclaimed failed reply must still count", id, ok)
			}
		})
	}
}

func TestCaptureInterfacesDoNotHideClaimedReplies(t *testing.T) {
	ResetIfaceTraffic()
	t.Cleanup(ResetIfaceTraffic)
	ifaceCache.Store(uint32(211), "br0")
	ifaceCache.Store(uint32(212), "eth0")
	t.Cleanup(func() {
		ifaceCache.Delete(uint32(211))
		ifaceCache.Delete(uint32(212))
	})
	injectAfter(t, time.Minute)
	calls := stubRouteAwait(t)
	r := newCopyRig(t, func(c *config.Config) { c.Queue.Interfaces = []string{"br0"} }, copyPinSet())
	mark := uint(r.cfg.Queue.Mark)
	queued := func(id uint32, raw []byte, in uint32) nfqueue.Attribute {
		return nfqueue.Attribute{PacketID: &id, Payload: &raw, InDev: &in}
	}

	unclaimed := newFakeModQueue()
	r.w.handleQueued(&verdictCtx{id: 1, q: unclaimed}, queued(1, replyRaw(buildDNSResponse(0x1b0, "www.example.com", resolverIPs), rigOutside, rigWAN, 40000), 212), mark)
	passedUnchanged(t, unclaimed.now(t), "a reply on the uplink with no claim at all")

	p := copyOf(nfHookForward, rigDevice, rigOutside, 40000, dns.BuildQuery("pin.example.com", 0x1b1, dnsTypeA))
	p.InDev, p.OutDev = 211, 212
	r.read(p)
	learned := calls.Load()

	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x1b1, "pin.example.com", resolverIPs), rigOutside, rigWAN, 40000)
	r.w.handleQueued(&verdictCtx{id: 2, q: q}, queued(2, raw, 212), mark)
	onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.12")

	other := newFakeModQueue()
	r.w.handleQueued(&verdictCtx{id: 3, q: other}, queued(3, replyRaw(buildDNSResponse(0x1b2, "www.example.com", resolverIPs), rigOutside, rigWAN, 40002), 212), mark)
	passedUnchanged(t, other.now(t), "an unclaimed reply on the uplink")
	if calls.Load() != learned {
		t.Fatal("an unclaimed reply from an interface Capture Interfaces leaves out took the answer path")
	}

	lan := newFakeModQueue()
	r.w.handleQueued(&verdictCtx{id: 4, q: lan}, queued(4, replyRaw(buildDNSResponse(0x1b3, "www.example.com", resolverIPs), rigRouter, rigDevice, 40003), 211), mark)
	passedUnchanged(t, lan.now(t), "an unclaimed reply on the LAN")
	if calls.Load() != learned+1 {
		t.Fatal("an unclaimed reply on a captured interface must still take the answer path")
	}
}

func TestHeldClaimedReplyIsAcceptedWhenTheWorkerStops(t *testing.T) {
	injectAfter(t, time.Minute)
	set := copyRedirectSet()
	set.DNS.Strict = true
	r := newCopyRig(t, nil, set)
	set.DNS.DoHURL = copyDoHServer(t, r.w, make(chan struct{}), answerWith("203.0.113.99")).url
	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("www.example.com", 0x1c1, dnsTypeA)))

	q := &unbindingQueue{verdicts: make(chan int, 8)}
	cancel := r.w.cancel
	var unbound atomic.Bool
	r.w.cancel = func() {
		unbound.Store(true)
		q.unbound.Store(true)
		cancel()
	}
	r.reply(q, replyRaw(buildDNSResponse(0x1c1, "www.example.com", resolverIPs), rigRouter, rigDevice, 40000))
	select {
	case v := <-q.verdicts:
		t.Fatalf("verdict %d before b4's answer or the stop", v)
	case <-time.After(30 * time.Millisecond):
	}

	r.w.Stop()
	select {
	case v := <-q.verdicts:
		if v != nfqueue.NfAccept {
			t.Fatalf("verdict %d on stop, want accept", v)
		}
	default:
		t.Fatal("the held reply was not accepted before the worker let go of its queue")
	}
	if !unbound.Load() {
		t.Fatal("the worker never let go of its queue")
	}
	if got := r.l.stats.repliesFailedOpen.Load(); got != 1 {
		t.Fatalf("replies_failed_open = %d, want 1", got)
	}
}
