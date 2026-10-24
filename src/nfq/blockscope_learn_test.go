package nfq

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/sock"
)

type learnClient struct {
	name string
	ip   string
	in   uint32
	hook *uint8
}

type learnRig struct {
	t      *testing.T
	w      *Worker
	client learnClient
	route  *routeHookLog
	dscp   *dscpHookLog
	tls12  bool
}

const unreachableDoH = "http://127.0.0.1:1/dns-query"

func nxDoHServer(t *testing.T, w *Worker) string {
	t.Helper()
	return dohServerReplying(t, w, dns.BuildBlockResponse)
}

func dohServerReplying(t *testing.T, w *Worker, reply func(query []byte) []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		query, err := io.ReadAll(r.Body)
		if err != nil || len(query) == 0 {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		rw.Header().Set("Content-Type", dns.DoHContentType)
		_, _ = rw.Write(reply(query))
	}))
	client := srv.Client()
	cfg := w.getConfig()
	dohClientMu.Lock()
	prevClient, prevMark, prevTimeout := dohClient, dohClientMark, dohClientTimeout
	dohClient, dohClientMark, dohClientTimeout = client, int(cfg.MainInjectedMark()), cfg.DNSQueryTimeout()
	dohClientMu.Unlock()
	t.Cleanup(func() {
		dohClientMu.Lock()
		dohClient, dohClientMark, dohClientTimeout = prevClient, prevMark, prevTimeout
		dohClientMu.Unlock()
		client.CloseIdleConnections()
		srv.Close()
	})
	return srv.URL + "/dns-query"
}

func scopedOutLearningSets() []*config.SetConfig {
	block := scopeToGuest(dscpHookSet("block", "example.com", false, false), config.BlockActionDrop)
	proxy := dscpHookSet("proxy", "example.com", true, true)
	proxy.Targets.DomainsToMatch = append(proxy.Targets.DomainsToMatch, "only.example")
	spare := dscpHookSet("spare", "unused.example", true, true)
	return []*config.SetConfig{block, proxy, spare}
}

func newLearnRig(t *testing.T, client learnClient) *learnRig {
	t.Helper()
	resetDNSAnswerCache()
	t.Cleanup(resetDNSAnswerCache)
	t.Cleanup(stopDNSRouteCleanup)
	captureSends(t)
	cfg := config.NewConfig()
	cfg.Sets = scopedOutLearningSets()
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	w.goodIPs = iphealth.NewKnownGood()
	w.ipHealth = iphealth.NewTracker(func(string, uint16, int) bool { return false })
	t.Cleanup(w.ipHealth.Stop)
	t.Cleanup(w.wg.Wait)
	return &learnRig{t: t, w: w, client: client, route: stubRouteHooks(t), dscp: stubDSCPHooks(t)}
}

func (r *learnRig) set(id string) *config.SetConfig {
	return r.w.getConfig().GetSetById(id)
}

func (r *learnRig) pin(id string) {
	r.set(id).DNS.Pins = map[string][]string{"www.example.com": {"203.0.113.73"}, "www.only.example": {"203.0.113.74"}}
}

func (r *learnRig) redirect(id, answer string) {
	set := r.set(id)
	set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(r.t, r.w, answer)
}

func (r *learnRig) failingRedirect() {
	proxy := r.set("proxy")
	proxy.DNS.Enabled, proxy.DNS.DoHURL = true, nxDoHServer(r.t, r.w)
	proxy.Escalate.To, proxy.Escalate.DNSThreshold = "spare", 1
	r.pin("spare")
}

func (r *learnRig) heal(domain string) {
	proxy := r.set("proxy")
	proxy.TCP.IPBlockDetect.Enabled, proxy.TCP.IPBlockDetect.HealDNS = true, true
	markDead(r.t, r.w, healDeadIP)
	r.w.goodIPs.Remember(domain, net.ParseIP(healGoodIP))
}

func (r *learnRig) queue(raw []byte) int {
	r.t.Helper()
	return queuedFrom(r.t, r.w, raw, r.client.in, r.client.hook, nil)
}

func (r *learnRig) query(domain string, sport, txid uint16) {
	r.t.Helper()
	r.queue(makeV4UDPPacket(dns.BuildQuery(domain, txid, dnsTypeA), net.ParseIP(r.client.ip), net.ParseIP(rigUpstream), sport, 53))
}

func (r *learnRig) redirectedQuery(domain string, sport, txid uint16) {
	r.t.Helper()
	replies := watchReplies(r.t)
	r.query(domain, sport, txid)
	oneReply(r.t, replies)
	holdsDone(r.t, r.w)
}

func (r *learnRig) answer(payload []byte, dport uint16) {
	r.t.Helper()
	raw := makeV4UDPPacket(payload, net.ParseIP(rigUpstream), net.ParseIP(r.client.ip), 53, dport)
	pkt, ok := r.w.parseIPHeaders(raw)
	if !ok {
		r.t.Fatal("the answer did not parse")
	}
	r.w.processDnsPacket(&verdictCtx{id: 1, q: newFakeQueue(), verdict: engine.VerdictAccept}, pkt, 53, dport, payload)
	holdsDone(r.t, r.w)
}

func (r *learnRig) hello(domain string, sport uint16) {
	r.t.Helper()
	raw := helloNaming(domain, r.client.ip, sport)
	if r.tls12 {
		raw = bytes.Replace(raw, []byte{0x00, 0x2b, 0x00, 0x03, 0x02, 0x03, 0x04}, []byte{0x00, 0x2b, 0x00, 0x03, 0x02, 0x03, 0x03}, 1)
	}
	r.queue(raw)
}

func (r *learnRig) quic(domain string, sport uint16) {
	r.t.Helper()
	initial := buildQUICInitialWithSNI(r.t, []byte{9, 8, 7, 6, 5, 4, 3, byte(sport)}, domain)
	r.queue(makeV4UDPPacket(initial, net.ParseIP(r.client.ip), quicServer, sport, 443))
}

func (r *learnRig) syn(sport uint16) {
	r.t.Helper()
	r.queue(synFrom(r.client.ip, sport))
}

func (r *learnRig) data(domain string, sport uint16) {
	r.t.Helper()
	raw := makeV4TCPPacketFromPort(tlsAppData(32), 1000+uint32(len(buildClientHello(domain, 16, 0xAB))), sport, 0x18)
	copy(raw[12:16], net.ParseIP(r.client.ip).To4())
	r.queue(raw)
}

func (r *learnRig) rst(domain string, sport uint16) {
	r.t.Helper()
	proxy := r.set("proxy")
	proxy.Routing.Mode, proxy.Routing.EgressInterface = config.RoutingModeInterface, "wg9"
	proxy.TCP.RSTProtection.Enabled = true
	proxy.Escalate.To, proxy.Escalate.RstThreshold = "spare", 1
	r.hello(domain, sport)
	raw := makeV4TCPPacketFromPort(nil, 9000, 443, 0x04)
	copy(raw[12:16], net.ParseIP("1.2.3.4").To4())
	copy(raw[16:20], net.ParseIP(r.client.ip).To4())
	raw[22], raw[23] = byte(sport>>8), byte(sport)
	r.w.ProcessPacket(raw)
}

func (r *learnRig) resolvesTo(domain string, txid, dport uint16) {
	r.t.Helper()
	r.answer(buildDNSResponse(txid, domain, []net.IP{quicServer}), dport)
}

func (r *learnRig) plainDPI() *config.SetConfig {
	dpi := r.set("proxy")
	dpi.Routing.Enabled = false
	dpi.DSCP.Enabled = false
	return dpi
}

type learnSite struct {
	name  string
	learn string
	run   func(r *learnRig, domain string)
}

func scopedOutLearnSites() []learnSite {
	return append(answerLearnSites(), connectionLearnSites()...)
}

func answerLearnSites() []learnSite {
	return []learnSite{
		{"a forwarded answer", "await proxy", func(r *learnRig, domain string) {
			r.answer(buildDNSResponse(0x951, domain, []net.IP{net.ParseIP("203.0.113.71")}), 40600)
		}},
		{"an answer to a query that noted its set", "await proxy", func(r *learnRig, domain string) {
			storeDNSPendingRoute(dnsRouteKeyRequest(IPv4, net.ParseIP(r.client.ip).To4(), 40601, net.ParseIP(rigUpstream).To4(), 53, 0x952, domain), "proxy")
			r.answer(buildDNSResponse(0x952, domain, []net.IP{net.ParseIP("203.0.113.72")}), 40601)
		}},
		{"a healed answer", "await proxy", func(r *learnRig, domain string) {
			r.heal(domain)
			r.answer(buildTestAResponse(domain, healDeadIP), 40602)
		}},
		{"a DNS failure escalation", "async spare", func(r *learnRig, domain string) {
			proxy := r.set("proxy")
			proxy.Escalate.To, proxy.Escalate.DNSThreshold = "spare", 1
			r.w.goodIPs.Remember(domain, net.ParseIP("203.0.113.76"))
			r.answer(dns.BuildBlockResponse(dns.BuildQuery(domain, 0x953, dnsTypeA)), 40603)
		}},
		{"a DNS failure the next set answers", "await spare", func(r *learnRig, domain string) {
			proxy := r.set("proxy")
			proxy.Escalate.To, proxy.Escalate.DNSThreshold = "spare", 1
			r.pin("spare")
			r.answer(dns.BuildBlockResponse(dns.BuildQuery(domain, 0x954, dnsTypeA)), 40604)
		}},
		{"a DNS failure the next set redirects", "sync spare", func(r *learnRig, domain string) {
			proxy := r.set("proxy")
			proxy.Escalate.To, proxy.Escalate.DNSThreshold = "spare", 1
			r.redirect("spare", "203.0.113.77")
			replies := watchReplies(r.t)
			r.answer(dns.BuildBlockResponse(dns.BuildQuery(domain, 0x95d, dnsTypeA)), 40609)
			oneReply(r.t, replies)
		}},
		{"a failed redirect the next set redirects", "sync spare", func(r *learnRig, domain string) {
			proxy := r.set("proxy")
			proxy.DNS.Enabled, proxy.DNS.DoHURL = true, nxDoHServer(r.t, r.w)
			proxy.Escalate.To, proxy.Escalate.DNSThreshold = "spare", 1
			r.redirect("spare", "203.0.113.77")
			r.redirectedQuery(domain, 40611, 0x95e)
		}},
		{"a pinned answer", "await proxy", func(r *learnRig, domain string) {
			r.pin("proxy")
			r.query(domain, 40605, 0x955)
			holdsDone(r.t, r.w)
		}},
		{"a redirected answer", "sync proxy", func(r *learnRig, domain string) {
			r.redirect("proxy", "203.0.113.75")
			r.redirectedQuery(domain, 40606, 0x956)
		}},
		{"a healed redirected answer", "sync proxy", func(r *learnRig, domain string) {
			r.heal(domain)
			r.redirect("proxy", healDeadIP)
			r.redirectedQuery(domain, 40607, 0x957)
		}},
		{"a failed redirect the next set answers", "await spare", func(r *learnRig, domain string) {
			r.failingRedirect()
			r.redirectedQuery(domain, 40608, 0x958)
		}},
		{"an unreachable redirect the next set answers", "await spare", func(r *learnRig, domain string) {
			r.failingRedirect()
			proxy := r.set("proxy")
			proxy.DNS.Strict, proxy.DNS.DoHURL = true, unreachableDoH
			r.redirectedQuery(domain, 40612, 0x95f)
		}},
		{"a TCP answer", "sync proxy", func(r *learnRig, domain string) {
			r.redirect("proxy", "203.0.113.75")
			tcpQueryFrom(r.t, r.w, r.client.ip, dns.BuildQuery(domain, 0x959, dnsTypeA))
		}},
		{"a healed TCP answer", "sync proxy", func(r *learnRig, domain string) {
			r.heal(domain)
			r.redirect("proxy", healDeadIP)
			tcpQueryFrom(r.t, r.w, r.client.ip, dns.BuildQuery(domain, 0x95a, dnsTypeA))
		}},
		{"a pinned TCP answer", "await proxy", func(r *learnRig, domain string) {
			r.pin("proxy")
			tcpQueryFrom(r.t, r.w, r.client.ip, dns.BuildQuery(domain, 0x95b, dnsTypeA))
		}},
		{"a failed TCP redirect the next set answers", "sync spare", func(r *learnRig, domain string) {
			r.failingRedirect()
			tcpQueryFrom(r.t, r.w, r.client.ip, dns.BuildQuery(domain, 0x95c, dnsTypeA))
		}},
	}
}

func connectionLearnSites() []learnSite {
	return []learnSite{
		{"a ClientHello", "ip proxy", func(r *learnRig, domain string) {
			r.hello(domain, 12600)
		}},
		{"a QUIC Initial", "ip proxy", func(r *learnRig, domain string) {
			r.quic(domain, 51400)
		}},
		{"a stall escalation", "async spare", func(r *learnRig, domain string) {
			r.set("proxy").Escalate.To = "spare"
			for range 3 {
				r.hello(domain, 12601)
			}
		}},
		{"an escalation refresh", "async spare", func(r *learnRig, domain string) {
			r.w.destState.SetEscalation(domain, "spare", escalateReasonStall, time.Hour)
			r.hello(domain, 12602)
		}},
		{"a QUIC escalation refresh", "async spare", func(r *learnRig, domain string) {
			r.w.destState.SetEscalation(domain, "spare", escalateReasonStall, time.Hour)
			r.quic(domain, 51401)
		}},
		{"an incoming RST", "async spare", func(r *learnRig, domain string) {
			r.rst(domain, 12603)
		}},
	}
}

func TestAScopedOutBlockSetKeepsTheNextSetFromLearningItsNames(t *testing.T) {
	idx := scopeIfaces(t)
	clients := []learnClient{
		{name: "the router", ip: scopeRouter, hook: hookAt(nfHookLocalOut)},
		{name: "a client on br0", ip: scopeLAN, in: idx["br0"]},
	}
	for _, site := range scopedOutLearnSites() {
		for _, client := range clients {
			for _, domain := range []string{"www.example.com", "www.only.example"} {
				shared := domain == "www.example.com"
				t.Run(site.name+"/"+client.name+"/"+domain, func(t *testing.T) {
					thisHost(t, scopeRouter)
					routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
					r := newLearnRig(t, client)
					site.run(r, domain)
					route, dscp := r.route.snapshot(), r.dscp.list()
					if shared && (len(route) != 0 || len(dscp) != 0) {
						t.Fatalf("a name the block set scoped to br-guest also lists wrote the next set's kernel sets: routing %v, DSCP %+v", route, dscp)
					}
					if !shared && route[site.learn] == 0 {
						t.Fatalf("a name only the proxy set lists never reached %q: routing %v, DSCP %+v", site.learn, route, dscp)
					}
				})
			}
		}
	}
}

func TestAQueryForANameAScopedOutBlockSetListsNotesNoRouteForItsAnswer(t *testing.T) {
	idx := scopeIfaces(t)
	for _, domain := range []string{"www.example.com", "www.only.example"} {
		t.Run(domain, func(t *testing.T) {
			thisHost(t, scopeRouter)
			r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
			r.query(domain, 40610, 0x961)
			key := dnsRouteKeyRequest(IPv4, net.ParseIP(scopeLAN).To4(), 40610, net.ParseIP(rigUpstream).To4(), 53, 0x961, domain)
			_, noted := consumeDNSPendingRoute(key)
			if want := domain == "www.only.example"; noted != want {
				t.Fatalf("the query noted a route for its answer: %v, want %v", noted, want)
			}
		})
	}
}

func escalationFollowUps() []learnSite {
	return []learnSite{
		{"a hinted SYN after a stall", "async spare", func(r *learnRig, domain string) {
			r.set("proxy").Escalate.To = "spare"
			r.resolvesTo(domain, 0x9b1, 40820)
			for range 3 {
				r.hello(domain, 12801)
			}
			r.syn(12802)
		}},
		{"data after a ClientHello decided its set", "async spare", func(r *learnRig, domain string) {
			r.w.destState.SetEscalation(domain, "spare", escalateReasonStall, time.Hour)
			r.hello(domain, 12803)
			r.data(domain, 12803)
		}},
		{"a SYN to a dead address hinted to the DPI set", "async spare", func(r *learnRig, domain string) {
			r.plainDPI().Escalate.To = "spare"
			markDead(r.t, r.w, quicServer.String())
			r.resolvesTo(domain, 0x9b2, 40821)
			r.syn(12804)
		}},
		{"a SYN after a stall in the DPI set", "async spare", func(r *learnRig, domain string) {
			r.plainDPI().Escalate.To = "spare"
			r.resolvesTo(domain, 0x9b3, 40822)
			for range 3 {
				r.hello(domain, 12805)
			}
			r.syn(12806)
		}},
	}
}

func TestAnEscalationOnAnotherInterfaceWritesNothingForANameTheScopedBlockSetLists(t *testing.T) {
	idx := scopeIfaces(t)
	clients := []learnClient{
		{name: "the router", ip: scopeRouter, hook: hookAt(nfHookLocalOut)},
		{name: "a client on br0", ip: scopeLAN, in: idx["br0"]},
	}
	for _, site := range escalationFollowUps() {
		for _, client := range clients {
			for _, domain := range []string{"www.example.com", "www.only.example"} {
				shared := domain == "www.example.com"
				t.Run(site.name+"/"+client.name+"/"+domain, func(t *testing.T) {
					thisHost(t, scopeRouter)
					routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
					r := newLearnRig(t, client)
					site.run(r, domain)
					holdsDone(t, r.w)
					route, dscp := r.route.snapshot(), r.dscp.list()
					if shared && (len(route) != 0 || len(dscp) != 0) {
						t.Fatalf("a name the block set scoped to br-guest lists reached a kernel set: routing %v, DSCP %+v", route, dscp)
					}
					if !shared && route[site.learn] == 0 {
						t.Fatalf("a name only the DPI set lists never reached %q after its escalation: routing %v, DSCP %+v", site.learn, route, dscp)
					}
				})
			}
		}
	}
}

func TestABlockSetWithoutNamesLeavesTheNextSetLearningAsBefore(t *testing.T) {
	idx := scopeIfaces(t)
	clients := []learnClient{
		{name: "the router", ip: scopeRouter, hook: hookAt(nfHookLocalOut)},
		{name: "a client on br0", ip: scopeLAN, in: idx["br0"]},
	}
	for _, c := range []struct {
		name  string
		block func(set *config.SetConfig)
		send  func(r *learnRig)
	}{
		{"a UDP port", func(set *config.SetConfig) { set.UDP.DPortFilter = "443" }, func(r *learnRig) { r.quic("www.example.com", 51460) }},
		{"a TCP port", func(set *config.SetConfig) { set.TCP.DPortFilter = "443" }, func(r *learnRig) { r.hello("www.example.com", 12810) }},
		{"an address", func(set *config.SetConfig) { set.Targets.IPs = []string{quicServer.String() + "/32"} }, func(r *learnRig) { r.hello("www.example.com", 12811) }},
	} {
		for _, scoped := range []bool{false, true} {
			for _, client := range clients {
				t.Run(fmt.Sprintf("%s/scoped=%v/%s", c.name, scoped, client.name), func(t *testing.T) {
					thisHost(t, scopeRouter)
					routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
					r := newLearnRig(t, client)
					block := r.set("block")
					block.Targets.DomainsToMatch = nil
					c.block(block)
					if !scoped {
						block.Routing.SourceInterfaces = nil
					}
					cfg := r.w.getConfig()
					cfg.BuildSetPortRanges()
					r.w.matcher.Store(buildMatcher(cfg))
					c.send(r)
					holdsDone(t, r.w)
					if route := r.route.snapshot(); route["ip proxy"] == 0 {
						t.Fatalf("a name no block set lists never reached the proxy set's kernel sets: routing %v, DSCP %+v", route, r.dscp.list())
					}
				})
			}
		}
	}
}

func TestABlockSetForOneDeviceLeavesOtherDevicesLearningItsNames(t *testing.T) {
	idx := scopeIfaces(t)
	const guarded, other = "aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"
	for _, site := range scopedOutLearnSites() {
		for _, mac := range []string{guarded, other} {
			t.Run(site.name+"/"+mac, func(t *testing.T) {
				thisHost(t, scopeRouter)
				routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
				r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
				r.set("block").Targets.SourceDevices = []string{guarded}
				r.w.matcher.Store(buildMatcher(r.w.getConfig()))
				r.w.ipToMac.Store(map[string]string{scopeLAN: mac})
				site.run(r, "www.example.com")
				holdsDone(t, r.w)
				route, dscp := r.route.snapshot(), r.dscp.list()
				if mac == guarded && (len(route) != 0 || len(dscp) != 0) {
					t.Fatalf("the device the block set covers reached a kernel set for its name from br0: routing %v, DSCP %+v", route, dscp)
				}
				if mac == other && route[site.learn] == 0 {
					t.Fatalf("a device the block set does not cover never reached %q: routing %v, DSCP %+v", site.learn, route, dscp)
				}
			})
		}
	}
}

func TestAScopedBlockSetForAnotherVersionLeavesClientHellosLearning(t *testing.T) {
	idx := scopeIfaces(t)
	for _, limit := range []struct {
		name  string
		apply func(set *config.SetConfig)
	}{
		{"IPv6 only", func(set *config.SetConfig) { set.Targets.IPVersion = "6" }},
		{"TLS 1.2 only", func(set *config.SetConfig) { set.Targets.TLSVersion = "1.2" }},
	} {
		for _, c := range []struct {
			name  string
			learn string
			send  func(r *learnRig)
		}{
			{"a ClientHello", "ip proxy", func(r *learnRig) { r.hello("www.example.com", 12830) }},
			{"a QUIC Initial", "ip proxy", func(r *learnRig) { r.quic("www.example.com", 51470) }},
		} {
			t.Run(limit.name+"/"+c.name, func(t *testing.T) {
				thisHost(t, scopeRouter)
				routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
				r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
				limit.apply(r.set("block"))
				r.w.matcher.Store(buildMatcher(r.w.getConfig()))
				c.send(r)
				holdsDone(t, r.w)
				if route := r.route.snapshot(); route[c.learn] == 0 {
					t.Fatalf("an IPv4 TLS 1.3 hello the %s block set never applies to did not reach %q: routing %v, DSCP %+v", limit.name, c.learn, route, r.dscp.list())
				}
			})
		}
	}
}

func TestANameAScopedBlockSetListsByRegexIsLearnedOnlyWhereThatSetApplies(t *testing.T) {
	idx := scopeIfaces(t)
	for _, c := range []struct {
		domain string
		learns bool
	}{
		{"www.example.com", false},
		{"cdn.example.com", true},
	} {
		t.Run(c.domain, func(t *testing.T) {
			thisHost(t, scopeRouter)
			routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
			r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
			r.set("block").Targets.DomainsToMatch = []string{`regexp:^www\.example\.com$`}
			r.set("proxy").Targets.DomainsToMatch = []string{`regexp:\.example\.com$`}
			r.w.matcher.Store(buildMatcher(r.w.getConfig()))
			r.hello(c.domain, 12820)
			r.resolvesTo(c.domain, 0x9b4, 40830)
			holdsDone(t, r.w)
			route := r.route.snapshot()
			if learned := len(route) != 0; learned != c.learns {
				t.Fatalf("the proxy set learned %s from br0: %v, want %v (routing %v, DSCP %+v)", c.domain, learned, c.learns, route, r.dscp.list())
			}
		})
	}
}

func helloEscalationSites() []learnSite {
	return []learnSite{
		{"a stall escalation", "async spare", func(r *learnRig, domain string) {
			r.set("proxy").Escalate.To = "spare"
			for range 3 {
				r.hello(domain, 12901)
			}
		}},
		{"an escalation refresh", "async spare", func(r *learnRig, domain string) {
			r.w.destState.SetEscalation(domain, "spare", escalateReasonStall, time.Hour)
			r.hello(domain, 12902)
		}},
		{"an escalation refresh on data after the ClientHello", "async spare", func(r *learnRig, domain string) {
			r.hello(domain, 12903)
			r.w.destState.SetEscalation(domain, "spare", escalateReasonStall, time.Hour)
			r.data(domain, 12903)
		}},
		{"an incoming RST", "async spare", func(r *learnRig, domain string) {
			r.rst(domain, 12904)
		}},
	}
}

func versionedEscalationSites() []learnSite {
	return append(helloEscalationSites(), []learnSite{
		{"a QUIC escalation refresh", "async spare", func(r *learnRig, domain string) {
			r.w.destState.SetEscalation(domain, "spare", escalateReasonStall, time.Hour)
			r.quic(domain, 51901)
		}},
		{"a SYN a QUIC Initial hinted once the name escalated", "async spare", func(r *learnRig, domain string) {
			r.quic(domain, 51902)
			r.w.destState.SetEscalation(domain, "spare", escalateReasonStall, time.Hour)
			r.syn(12907)
		}},
		{"a SYN to a dead address a QUIC Initial hinted", "async spare", func(r *learnRig, domain string) {
			r.plainDPI().Escalate.To = "spare"
			markDead(r.t, r.w, quicServer.String())
			r.quic(domain, 51903)
			r.syn(12908)
		}},
	}...)
}

func TestAScopedBlockSetForAnotherVersionLeavesEscalationsWritingTheirRoutes(t *testing.T) {
	idx := scopeIfaces(t)
	for _, limit := range []struct {
		name  string
		apply func(set *config.SetConfig)
	}{
		{"IPv6 only", func(set *config.SetConfig) { set.Targets.IPVersion = "6" }},
		{"TLS 1.2 only", func(set *config.SetConfig) { set.Targets.TLSVersion = "1.2" }},
	} {
		for _, site := range versionedEscalationSites() {
			for _, client := range []learnClient{
				{name: "a client on br0", ip: scopeLAN, in: idx["br0"]},
				{name: "a client on br-guest", ip: scopeGuest, in: idx["br-guest"]},
			} {
				t.Run(limit.name+"/"+site.name+"/"+client.name, func(t *testing.T) {
					thisHost(t, scopeRouter)
					routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
					r := newLearnRig(t, client)
					limit.apply(r.set("block"))
					r.w.matcher.Store(buildMatcher(r.w.getConfig()))
					site.run(r, "www.example.com")
					holdsDone(t, r.w)
					if route := r.route.snapshot(); route[site.learn] == 0 {
						t.Fatalf("an IPv4 TLS 1.3 connection the %s block set never applies to escalated without reaching %q: routing %v, DSCP %+v", limit.name, site.learn, route, r.dscp.list())
					}
				})
			}
		}
	}
}

func TestAnEarlierSetForAnotherVersionLeavesTheScopedBlockSetGuardingItsNames(t *testing.T) {
	idx := scopeIfaces(t)
	for _, limit := range []struct {
		name  string
		apply func(set *config.SetConfig)
	}{
		{"IPv6 only", func(set *config.SetConfig) { set.Targets.IPVersion = "6" }},
		{"TLS 1.2 only", func(set *config.SetConfig) { set.Targets.TLSVersion = "1.2" }},
	} {
		for _, site := range versionedEscalationSites() {
			for _, client := range []learnClient{
				{name: "the router", ip: scopeRouter, hook: hookAt(nfHookLocalOut)},
				{name: "a client on br0", ip: scopeLAN, in: idx["br0"]},
			} {
				t.Run(limit.name+"/"+site.name+"/"+client.name, func(t *testing.T) {
					thisHost(t, scopeRouter)
					routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
					r := newLearnRig(t, client)
					earlier := dscpHookSet("earlier", "example.com", true, true)
					limit.apply(earlier)
					cfg := r.w.getConfig()
					cfg.Sets = []*config.SetConfig{earlier, r.set("block"), r.set("proxy"), r.set("spare")}
					r.w.matcher.Store(buildMatcher(cfg))
					site.run(r, "www.example.com")
					holdsDone(t, r.w)
					if route, dscp := r.route.snapshot(), r.dscp.list(); len(route) != 0 || len(dscp) != 0 {
						t.Fatalf("an IPv4 TLS 1.3 connection for a name the scoped block set takes from the %s set wrote kernel sets: routing %v, DSCP %+v", limit.name, route, dscp)
					}
				})
			}
		}
	}
}

func TestATLS12ClientIsGuardedAtTheVersionItsHelloCarries(t *testing.T) {
	idx := scopeIfaces(t)
	sites := append([]learnSite{{"a ClientHello", "ip proxy", func(r *learnRig, domain string) {
		r.hello(domain, 12930)
	}}}, helloEscalationSites()...)
	for _, limit := range []struct {
		version string
		guards  bool
	}{
		{"1.2", true},
		{"1.3", false},
	} {
		for _, site := range sites {
			t.Run("TLS "+limit.version+" only/"+site.name, func(t *testing.T) {
				thisHost(t, scopeRouter)
				routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
				r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
				r.tls12 = true
				r.set("block").Targets.TLSVersion = limit.version
				r.w.matcher.Store(buildMatcher(r.w.getConfig()))
				site.run(r, "www.example.com")
				holdsDone(t, r.w)
				route, dscp := r.route.snapshot(), r.dscp.list()
				if limit.guards && (len(route) != 0 || len(dscp) != 0) {
					t.Fatalf("a TLS 1.2 connection from br0 for a name the TLS 1.2 only block set takes on br-guest wrote kernel sets: routing %v, DSCP %+v", route, dscp)
				}
				if !limit.guards && route[site.learn] == 0 {
					t.Fatalf("a TLS 1.2 connection the TLS 1.3 only block set never applies to did not reach %q: routing %v, DSCP %+v", site.learn, route, dscp)
				}
			})
		}
	}
}

func TestADeadAddressEscalationIsGuardedLikeTheAnswerThatNamedTheAddress(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
	r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
	r.set("block").Targets.IPVersion = "6"
	r.w.matcher.Store(buildMatcher(r.w.getConfig()))
	r.plainDPI().Escalate.To = "spare"
	markDead(t, r.w, quicServer.String())
	r.resolvesTo("www.example.com", 0x9c1, 40840)
	r.syn(12905)
	holdsDone(t, r.w)
	if _, _, ok := r.w.destState.GetEscalation("www.example.com"); !ok {
		t.Fatal("the SYN to the dead address did not escalate")
	}
	if route, dscp := r.route.snapshot(), r.dscp.list(); len(route) != 0 || len(dscp) != 0 {
		t.Fatalf("the DNS answer the IPv6 only block set took named the address, yet its escalation wrote kernel sets: routing %v, DSCP %+v", route, dscp)
	}
}

func TestAnRSTEscalationWithoutARouteBackToTheClientWritesNothingForABlockedName(t *testing.T) {
	idx := scopeIfaces(t)
	for _, domain := range []string{"www.example.com", "www.only.example"} {
		t.Run(domain, func(t *testing.T) {
			thisHost(t, scopeRouter)
			routeByClient(t, map[string]uint32{scopeGuest: idx["br-guest"]})
			r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
			r.rst(domain, 12906)
			holdsDone(t, r.w)
			if _, _, ok := r.w.destState.GetEscalation(domain); !ok {
				t.Fatal("the forged RST did not escalate")
			}
			route, dscp := r.route.snapshot(), r.dscp.list()
			if domain == "www.example.com" && (len(route) != 0 || len(dscp) != 0) {
				t.Fatalf("a br0 connection the scoped block set was refused for wrote kernel sets for its name: routing %v, DSCP %+v", route, dscp)
			}
			if domain == "www.only.example" && route["async spare"] == 0 {
				t.Fatalf("a name only the proxy set lists never reached the escalated set: routing %v, DSCP %+v", route, dscp)
			}
		})
	}
}

func TestAScopedBlockSetForOneVersionStillTakesTheAnswersForItsNames(t *testing.T) {
	idx := scopeIfaces(t)
	for _, limit := range []struct {
		name  string
		apply func(set *config.SetConfig)
	}{
		{"IPv6 only", func(set *config.SetConfig) { set.Targets.IPVersion = "6" }},
		{"TLS 1.2 only", func(set *config.SetConfig) { set.Targets.TLSVersion = "1.2" }},
	} {
		for _, site := range answerLearnSites() {
			t.Run(limit.name+"/"+site.name, func(t *testing.T) {
				thisHost(t, scopeRouter)
				routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
				r := newLearnRig(t, learnClient{name: "br0", ip: scopeLAN, in: idx["br0"]})
				limit.apply(r.set("block"))
				r.w.matcher.Store(buildMatcher(r.w.getConfig()))
				site.run(r, "www.example.com")
				holdsDone(t, r.w)
				if route, dscp := r.route.snapshot(), r.dscp.list(); len(route) != 0 || len(dscp) != 0 {
					t.Fatalf("a DNS answer carries no IP or TLS version, so the %s block set takes it, yet the next set's kernel sets were written: routing %v, DSCP %+v", limit.name, route, dscp)
				}
			})
		}
	}
}
