package nfq

import (
	"context"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/sni"
	"github.com/florianl/go-nfqueue"
)

type dscpHookCall struct {
	set     string
	ips     string
	fromTLS bool
	async   bool
}

type dscpHookLog struct {
	mu    sync.Mutex
	calls []dscpHookCall
	waits []<-chan struct{}
}

func stubDSCPHooks(t *testing.T, waits ...<-chan struct{}) *dscpHookLog {
	t.Helper()
	prev, prevAsync := DSCPLearnFunc, DSCPLearnAsyncFunc
	t.Cleanup(func() { DSCPLearnFunc, DSCPLearnAsyncFunc = prev, prevAsync })
	l := &dscpHookLog{waits: waits}
	DSCPLearnFunc = func(_ *config.Config, set *config.SetConfig, ips []net.IP, fromTLS bool) []<-chan struct{} {
		l.add(set, ips, fromTLS, false)
		return l.waits
	}
	DSCPLearnAsyncFunc = func(_ *config.Config, set *config.SetConfig, ips []net.IP, fromTLS bool) {
		l.add(set, ips, fromTLS, true)
	}
	return l
}

func (l *dscpHookLog) add(set *config.SetConfig, ips []net.IP, fromTLS, async bool) {
	parts := make([]string, len(ips))
	for i, ip := range ips {
		parts[i] = ip.String()
	}
	l.mu.Lock()
	l.calls = append(l.calls, dscpHookCall{set: set.Id, ips: strings.Join(parts, ","), fromTLS: fromTLS, async: async})
	l.mu.Unlock()
}

func (l *dscpHookLog) list() []dscpHookCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

type routeHookLog struct {
	mu     sync.Mutex
	counts map[string]int
	waits  []<-chan struct{}
}

func stubRouteHooks(t *testing.T, waits ...<-chan struct{}) *routeHookLog {
	t.Helper()
	prevSync, prevAsync, prevAwait := RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSAwaitFunc
	prevIP, prevHost, prevIPAsync, prevHostAsync := RoutingLearnIPFunc, RoutingLearnHostFunc, RoutingLearnIPAsyncFunc, RoutingLearnHostAsyncFunc
	t.Cleanup(func() {
		RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSAwaitFunc = prevSync, prevAsync, prevAwait
		RoutingLearnIPFunc, RoutingLearnHostFunc, RoutingLearnIPAsyncFunc, RoutingLearnHostAsyncFunc = prevIP, prevHost, prevIPAsync, prevHostAsync
	})
	r := &routeHookLog{counts: map[string]int{}, waits: waits}
	RoutingHandleDNSFunc = func(_ *config.Config, set *config.SetConfig, _ []net.IP) { r.add("sync " + set.Id) }
	RoutingHandleDNSAsyncFunc = func(_ *config.Config, set *config.SetConfig, _ []net.IP) { r.add("async " + set.Id) }
	RoutingHandleDNSAwaitFunc = func(_ *config.Config, set *config.SetConfig, _ []net.IP) []<-chan struct{} {
		r.add("await " + set.Id)
		return r.waits
	}
	RoutingLearnIPFunc, RoutingLearnHostFunc = nil, nil
	RoutingLearnIPAsyncFunc = func(_ *config.Config, set *config.SetConfig, _ net.IP) { r.add("ip " + set.Id) }
	RoutingLearnHostAsyncFunc = func(_ *config.Config, set *config.SetConfig, _ string) { r.add("host " + set.Id) }
	return r
}

func (r *routeHookLog) add(call string) {
	r.mu.Lock()
	r.counts[call]++
	r.mu.Unlock()
}

func (r *routeHookLog) snapshot() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]int{}
	for k, v := range r.counts {
		out[k] = v
	}
	return out
}

func dscpHookSet(id, domain string, dscp, routing bool) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = id, id, true
	set.Targets.DomainsToMatch = []string{domain}
	set.DSCP = config.SetDSCPConfig{Enabled: dscp, Value: 31}
	if routing {
		set.Routing.Enabled = true
		set.Routing.Mode = config.RoutingModeProxy
	}
	return &set
}

func dscpHookWorker(t *testing.T, sets ...*config.SetConfig) (*Worker, *config.Config) {
	t.Helper()
	t.Cleanup(resetDNSAnswerCache)
	resetDNSAnswerCache()
	thisHost(t)
	cfg := config.NewConfig()
	cfg.Sets = sets
	w := passiveDNSWorker(t, &cfg)
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx = ctx
	t.Cleanup(func() {
		cancel()
		w.wg.Wait()
	})
	return w, &cfg
}

func dscpDoHServer(t *testing.T, w *Worker, answer string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		query, err := io.ReadAll(r.Body)
		if err != nil || len(query) == 0 {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		rw.Header().Set("Content-Type", dns.DoHContentType)
		_, _ = rw.Write(dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP(answer)}))
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

func dscpTCPAsk(t *testing.T, w *Worker, query []byte) <-chan []byte {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 40000}})
	}()
	answered := make(chan []byte, 1)
	read := make(chan struct{})
	go func() {
		defer close(read)
		if resp, err := readDNSTCPMessage(clientEnd); err == nil {
			answered <- resp
		}
	}()
	t.Cleanup(func() {
		s.cancel()
		clientEnd.Close()
		<-handled
		<-read
	})
	if err := writeDNSTCPMessage(clientEnd, query, time.Second); err != nil {
		t.Fatal(err)
	}
	return answered
}

func dscpWithin(t *testing.T, ch <-chan []byte, why string) []byte {
	t.Helper()
	select {
	case resp := <-ch:
		return resp
	case <-time.After(5 * time.Second):
		t.Fatal(why)
		return nil
	}
}

func dscpNotYet(t *testing.T, ch <-chan []byte, why string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal(why)
	case <-time.After(40 * time.Millisecond):
	}
}

func dscpCoolDown(source string) {
	for range dnsSourceFailuresToTrip {
		noteDNSSourceFailure(source)
	}
}

func dscpEscalatingPair(doh string) (*config.SetConfig, *config.SetConfig) {
	primary := dscpHookSet("primary", "example.com", false, false)
	primary.DNS.Enabled, primary.DNS.TargetDNS = true, "192.0.2.53"
	primary.Escalate.To, primary.Escalate.DNSThreshold = "backup", 1
	backup := dscpHookSet("backup", "example.com", true, false)
	backup.DNS.Enabled, backup.DNS.DoHURL = true, doh
	return primary, backup
}

type dscpHookSite struct {
	name string
	run  func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall
}

func dscpHookSites() []dscpHookSite {
	return []dscpHookSite{
		{"forwarded answer", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			set := dscpHookSet("fwd", "example.com", dscp, routing)
			w, cfg := dscpHookWorker(t, set)
			tune(cfg, set)
			log := stubDSCPHooks(t)
			q := newFakeQueue()
			w.processDnsPacket(&verdictCtx{id: 1, q: q}, answerPacket(), 53, 40000, buildDNSResponse(1, "www.example.com", []net.IP{net.ParseIP("203.0.113.10")}))
			if v := q.now(t); v != nfqueue.NfAccept {
				t.Fatalf("verdict %d, want accept", v)
			}
			return log.list()
		}},
		{"redirected answer", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			set := dscpHookSet("redirect", "example.com", dscp, routing)
			w, cfg := dscpHookWorker(t, set)
			set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, "203.0.113.20")
			tune(cfg, set)
			log := stubDSCPHooks(t)
			replies := watchReplies(t)
			w.resolveDNSRedirect(IPv4, set, cfg, dns.BuildQuery("www.example.com", 2, dnsTypeA), net.ParseIP("192.168.1.100").To4(), 40000, net.ParseIP("192.168.1.1").To4(), nil, 0)
			onlyAddress(t, oneReply(t, replies), "203.0.113.20")
			return log.list()
		}},
		{"TCP answer", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			set := dscpHookSet("tcp", "example.com", dscp, routing)
			w, cfg := dscpHookWorker(t, set)
			set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, "203.0.113.30")
			tune(cfg, set)
			log := stubDSCPHooks(t)
			onlyAddress(t, dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 3, dnsTypeA)), "the TCP answer never came"), "203.0.113.30")
			return log.list()
		}},
		{"escalated TCP answer", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			primary, backup := dscpEscalatingPair("")
			backup.DSCP.Enabled = dscp
			if routing {
				backup.Routing.Enabled, backup.Routing.Mode = true, config.RoutingModeProxy
			}
			w, cfg := dscpHookWorker(t, primary, backup)
			backup.DNS.DoHURL = dscpDoHServer(t, w, "203.0.113.40")
			tune(cfg, backup)
			dscpCoolDown(primary.DNS.TargetDNS)
			log := stubDSCPHooks(t)
			onlyAddress(t, dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 4, dnsTypeA)), "the escalated TCP answer never came"), "203.0.113.40")
			return log.list()
		}},
		{"pinned answer", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			set := dscpHookSet("pin", "example.com", dscp, routing)
			set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.50"}}
			w, cfg := dscpHookWorker(t, set)
			tune(cfg, set)
			log := stubDSCPHooks(t)
			replies := watchReplies(t)
			q := newFakeQueue()
			w.processDnsPacket(&verdictCtx{id: 5, q: q}, queryPacket(), 40000, 53, dns.BuildQuery("pin.example.com", 5, dnsTypeA))
			if v := q.now(t); v != nfqueue.NfDrop {
				t.Fatalf("verdict %d for a pinned query, want drop", v)
			}
			onlyAddress(t, oneReply(t, replies), "203.0.113.50")
			return log.list()
		}},
		{"escalation", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			set := dscpHookSet("escalated", "example.com", dscp, routing)
			_, cfg := dscpHookWorker(t, set)
			tune(cfg, set)
			log := stubDSCPHooks(t)
			registerEscalatedRoute(cfg, set, []net.IP{net.ParseIP("203.0.113.60")})
			return log.list()
		}},
		{"TLS name", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			set := newHintSet()
			set.DSCP = config.SetDSCPConfig{Enabled: dscp, Value: 31}
			set.Routing.Enabled = routing
			cfg := config.NewConfig()
			cfg.Sets = []*config.SetConfig{&set}
			tune(&cfg, &set)
			w := newTestWorker(t, &cfg)
			log := stubDSCPHooks(t)
			w.ProcessPacket(makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000))
			return log.list()
		}},
		{"QUIC name", func(t *testing.T, dscp, routing bool, tune func(*config.Config, *config.SetConfig)) []dscpHookCall {
			set := newQUICHintSet()
			set.DSCP = config.SetDSCPConfig{Enabled: dscp, Value: 31}
			set.Routing.Enabled = routing
			cfg := config.NewConfig()
			cfg.Sets = []*config.SetConfig{&set}
			tune(&cfg, &set)
			w := newTestWorker(t, &cfg)
			log := stubDSCPHooks(t)
			initial := buildQUICInitialWithSNI(t, []byte{9, 9, 9, 9, 1, 1, 1, 1}, "rr1.googlevideo.com")
			w.ProcessPacket(makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443))
			return log.list()
		}},
	}
}

func dscpNoTune(*config.Config, *config.SetConfig) {}

func TestDSCPHookEachSite(t *testing.T) {
	want := map[string][]dscpHookCall{
		"forwarded answer":     {{set: "fwd", ips: "203.0.113.10"}},
		"redirected answer":    {{set: "redirect", ips: "203.0.113.20"}},
		"TCP answer":           {{set: "tcp", ips: "203.0.113.30"}},
		"escalated TCP answer": {{set: "backup", ips: "203.0.113.40"}},
		"pinned answer":        {{set: "pin", ips: "203.0.113.50"}},
		"escalation":           {{set: "escalated", ips: "203.0.113.60", async: true}},
		"TLS name":             {{set: "yt-images", ips: "1.2.3.4", fromTLS: true, async: true}},
		"QUIC name":            {{set: "yt-video", ips: "1.2.3.4", fromTLS: true, async: true}},
	}
	for _, site := range dscpHookSites() {
		t.Run(site.name, func(t *testing.T) {
			stubRouteHooks(t)
			if got := site.run(t, true, false, dscpNoTune); !slices.Equal(got, want[site.name]) {
				t.Errorf("DSCP hook calls %+v, want %+v", got, want[site.name])
			}
		})
	}
}

func TestDSCPHookNotCalledWithoutDSCP(t *testing.T) {
	routed := map[string]map[string]int{
		"forwarded answer":     {"await fwd": 1},
		"redirected answer":    {"sync redirect": 1},
		"TCP answer":           {"sync tcp": 1},
		"escalated TCP answer": {"sync backup": 1},
		"pinned answer":        {"await pin": 1},
		"escalation":           {"async escalated": 1},
		"TLS name":             {"ip yt-images": 1, "host yt-images": 1},
		"QUIC name":            {"ip yt-video": 1, "host yt-video": 1},
	}
	for _, site := range dscpHookSites() {
		for _, routing := range []bool{false, true} {
			name := site.name + " without routing"
			if routing {
				name = site.name + " with routing"
			}
			t.Run(name, func(t *testing.T) {
				route := stubRouteHooks(t)
				if got := site.run(t, false, routing, dscpNoTune); len(got) != 0 {
					t.Errorf("a set without a DSCP value reached the DSCP hooks: %+v", got)
				}
				want := map[string]int{}
				if routing {
					want = routed[site.name]
				}
				if got := route.snapshot(); !maps.Equal(got, want) {
					t.Errorf("routing hook calls %v, want %v as before", got, want)
				}
			})
		}
	}

	t.Run("no allocations", func(t *testing.T) {
		stubRouteHooks(t)
		stubDSCPHooks(t)
		RoutingLearnIPAsyncFunc = func(*config.Config, *config.SetConfig, net.IP) {}
		RoutingLearnHostAsyncFunc = func(*config.Config, *config.SetConfig, string) {}
		cfg := config.NewConfig()
		plain := dscpHookSet("plain", "example.com", false, false)
		routedSet := dscpHookSet("routed", "example.com", false, true)
		dst := net.ParseIP("203.0.113.70").To4()
		ips := []net.IP{dst}
		w := &Worker{}
		for name, f := range map[string]func(){
			"a TLS name for a plain set":       func() { registerLearnedRoute(&cfg, plain, dst, "") },
			"a TLS name for a routed set":      func() { registerLearnedRoute(&cfg, routedSet, dst, "") },
			"an escalation for a plain set":    func() { registerEscalatedRoute(&cfg, plain, ips) },
			"a held answer for a plain set":    func() { learnAnswerAwait(&cfg, plain, ips) },
			"an inline answer for a plain set": func() { w.learnAnswerInline(&cfg, plain, ips, nil) },
		} {
			if n := testing.AllocsPerRun(100, f); n != 0 {
				t.Errorf("%s allocates %.0f times per call without a DSCP set", name, n)
			}
		}
	})
}

func TestDSCPHookEscalationRefresh(t *testing.T) {
	for name, c := range map[string]struct {
		dscp, routing bool
		learned       []dscpHookCall
		routed        map[string]int
	}{
		"a set with DSCP and no routing": {dscp: true, learned: []dscpHookCall{{set: "backup", ips: "203.0.113.80", async: true}}, routed: map[string]int{}},
		"a set with DSCP and routing":    {dscp: true, routing: true, learned: []dscpHookCall{{set: "backup", ips: "203.0.113.80", async: true}}, routed: map[string]int{"async backup": 1}},
		"a set with routing only":        {routing: true, routed: map[string]int{"async backup": 1}},
		"a set with neither":             {routed: map[string]int{}},
	} {
		t.Run(name, func(t *testing.T) {
			w := newEscalateWorker()
			cfg := config.NewConfig()
			set := dscpHookSet("backup", "example.com", c.dscp, c.routing)
			w.destState.SetEscalation("www.example.com", set.Id, escalateReasonStall, 0)
			route := stubRouteHooks(t)
			learned := stubDSCPHooks(t)
			for range 2 {
				w.refreshEscalatedRoute(&cfg, set, "www.example.com", net.ParseIP("203.0.113.80"))
			}
			if got := learned.list(); !slices.Equal(got, c.learned) {
				t.Errorf("DSCP hook calls %+v, want %+v", got, c.learned)
			}
			if got := route.snapshot(); !maps.Equal(got, c.routed) {
				t.Errorf("routing hook calls %v, want %v", got, c.routed)
			}
		})
	}

	t.Run("routing turned on after DSCP refreshes", func(t *testing.T) {
		w := newEscalateWorker()
		cfg := config.NewConfig()
		w.destState.SetEscalation("www.example.com", "backup", escalateReasonStall, 0)
		route := stubRouteHooks(t)
		learned := stubDSCPHooks(t)
		dscpOnly := dscpHookSet("backup", "example.com", true, false)
		for range 2 {
			w.refreshEscalatedRoute(&cfg, dscpOnly, "www.example.com", net.ParseIP("203.0.113.80"))
		}
		routed := dscpHookSet("backup", "example.com", true, true)
		w.refreshEscalatedRoute(&cfg, routed, "www.example.com", net.ParseIP("203.0.113.80"))
		if got := route.snapshot(); !maps.Equal(got, map[string]int{"async backup": 1}) {
			t.Errorf("routing hook calls %v once routing was turned on, want the escalated host routed at once", got)
		}
		if got := len(learned.list()); got != 2 {
			t.Errorf("%d DSCP learns, want one from the DSCP refresh and one from the routing refresh", got)
		}
	})

	t.Run("interval", func(t *testing.T) {
		plain := dscpHookSet("plain", "example.com", true, false)
		plain.Routing.IPTTLSeconds = 86400
		if got := escalatedRefreshInterval(plain); got != 40*time.Minute {
			t.Errorf("a set without routing refreshes every %s, want two thirds of its 3600 s DSCP TTL, when the learner rewrites the entry", got)
		}
		routed := dscpHookSet("routed", "example.com", true, true)
		routed.Routing.IPTTLSeconds = 600
		if got := escalatedRefreshInterval(routed); got != routeRefreshInterval(routed) {
			t.Errorf("a routed set refreshes every %s, want routing's %s", got, routeRefreshInterval(routed))
		}
	})
}

func TestDSCPHookRequestKeyStaysRoutingOnly(t *testing.T) {
	ask := func(t *testing.T, w *Worker, txid uint16, swap func()) {
		t.Helper()
		q := newFakeQueue()
		w.processDnsPacket(&verdictCtx{id: uint32(txid), q: q}, queryPacket(), 40000, 53, dns.BuildQuery("www.example.com", txid, dnsTypeA))
		if v := q.now(t); v != nfqueue.NfAccept {
			t.Fatalf("verdict %d for the query, want accept", v)
		}
		swap()
		w.processDnsPacket(&verdictCtx{id: uint32(txid) + 1, q: q}, answerPacket(), 53, 40000, buildDNSResponse(txid, "www.example.com", []net.IP{net.ParseIP("203.0.113.21")}))
		if v := q.now(t); v != nfqueue.NfAccept {
			t.Fatalf("verdict %d for the answer, want accept", v)
		}
	}

	for name, dscp := range map[string]bool{"without DSCP": false, "with DSCP": true} {
		t.Run("an answer routed by its name is not routed again by its request "+name, func(t *testing.T) {
			t.Cleanup(stopDNSRouteCleanup)
			set := dscpHookSet("keyed", "example.com", dscp, true)
			w, _ := dscpHookWorker(t, set)
			route := stubRouteHooks(t)
			learned := stubDSCPHooks(t)
			ask(t, w, 21, func() {})
			if got := route.snapshot(); !maps.Equal(got, map[string]int{"await keyed": 1}) {
				t.Errorf("routing hook calls %v, want the answer routed once", got)
			}
			if got := len(learned.list()); got != map[bool]int{false: 0, true: 1}[dscp] {
				t.Errorf("%d DSCP hook calls with DSCP %v", got, dscp)
			}
		})
	}

	t.Run("an answer known only by its request is routed but not learned for DSCP", func(t *testing.T) {
		t.Cleanup(stopDNSRouteCleanup)
		set := dscpHookSet("keyed", "example.com", true, true)
		w, _ := dscpHookWorker(t, set)
		route := stubRouteHooks(t)
		learned := stubDSCPHooks(t)
		ask(t, w, 23, func() { w.matcher.Store(sni.NewSuffixSet(nil)) })
		if got := route.snapshot(); !maps.Equal(got, map[string]int{"await keyed": 1}) {
			t.Errorf("routing hook calls %v, want the answer routed by its request key", got)
		}
		if got := learned.list(); len(got) != 0 {
			t.Errorf("the request-key path fed DSCP: %+v", got)
		}
	})
}

func TestDSCPHookSkippedInDiscoveryAndDomainOnly(t *testing.T) {
	cases := map[string]func(*config.Config, *config.SetConfig){
		"Discovery":   func(cfg *config.Config, _ *config.SetConfig) { cfg.Queue.IsDiscovery = true },
		"domain-only": func(_ *config.Config, set *config.SetConfig) { set.Targets.DomainOnly = true },
	}
	for name, tune := range cases {
		for _, site := range dscpHookSites() {
			for _, routing := range []bool{false, true} {
				t.Run(name+" "+site.name, func(t *testing.T) {
					route := stubRouteHooks(t)
					if got := site.run(t, true, routing, tune); len(got) != 0 {
						t.Errorf("the DSCP hooks ran: %+v", got)
					}
					if got := route.snapshot(); len(got) != 0 {
						t.Errorf("the routing hooks ran: %v", got)
					}
				})
			}
		}
	}
}

func TestDSCPHookHoldAndInlineWaits(t *testing.T) {
	t.Run("a forwarded answer is held until both writes land", func(t *testing.T) {
		set := dscpHookSet("fwd", "example.com", true, true)
		w, _ := dscpHookWorker(t, set)
		route, dscpGate := make(chan struct{}), make(chan struct{})
		stubRouteHooks(t, route)
		stubDSCPHooks(t, dscpGate)
		q := newFakeQueue()

		w.processDnsPacket(&verdictCtx{id: 11, q: q}, answerPacket(), 53, 40000, buildDNSResponse(11, "www.example.com", []net.IP{net.ParseIP("203.0.113.11")}))
		q.none(t, 40*time.Millisecond)
		close(route)
		q.none(t, 40*time.Millisecond)
		close(dscpGate)
		if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
			t.Fatalf("verdict %d, want accept", v)
		}
		holdsDone(t, w)
	})

	t.Run("a forwarded answer of a set without routing is held for its DSCP write", func(t *testing.T) {
		set := dscpHookSet("fwd", "example.com", true, false)
		w, _ := dscpHookWorker(t, set)
		gate := make(chan struct{})
		stubRouteHooks(t)
		stubDSCPHooks(t, gate)
		q := newFakeQueue()

		w.processDnsPacket(&verdictCtx{id: 12, q: q}, answerPacket(), 53, 40000, buildDNSResponse(12, "www.example.com", []net.IP{net.ParseIP("203.0.113.12")}))
		q.none(t, 40*time.Millisecond)
		close(gate)
		if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
			t.Fatalf("verdict %d, want accept", v)
		}
		holdsDone(t, w)
	})

	t.Run("a forwarded answer keeps its DSCP wait next to the routing of its escalated request", func(t *testing.T) {
		t.Cleanup(stopDNSRouteCleanup)
		plain := dscpHookSet("plain", "example.com", true, false)
		routed := dscpHookSet("routed", "other.org", false, true)
		w, _ := dscpHookWorker(t, plain, routed)
		w.destState.SetEscalation("www.example.com", routed.Id, "test", 0)
		route, dscpGate := make(chan struct{}), make(chan struct{})
		routeLog := stubRouteHooks(t, route)
		dscpLog := stubDSCPHooks(t, dscpGate)
		q := newFakeQueue()

		w.processDnsPacket(&verdictCtx{id: 17, q: q}, queryPacket(), 40000, 53, dns.BuildQuery("www.example.com", 17, dnsTypeA))
		if v := q.now(t); v != nfqueue.NfAccept {
			t.Fatalf("verdict %d for the query, want accept", v)
		}
		w.processDnsPacket(&verdictCtx{id: 18, q: q}, answerPacket(), 53, 40000, buildDNSResponse(17, "www.example.com", []net.IP{net.ParseIP("203.0.113.17")}))
		if got := routeLog.snapshot(); !maps.Equal(got, map[string]int{"await routed": 1}) {
			t.Fatalf("routing hook calls %v, want the escalated request routed once", got)
		}
		if got := dscpLog.list(); !slices.Equal(got, []dscpHookCall{{set: "plain", ips: "203.0.113.17"}}) {
			t.Fatalf("DSCP hook calls %+v, want the answer's own set", got)
		}
		close(route)
		q.none(t, 40*time.Millisecond)
		close(dscpGate)
		if v := q.within(t, 5*time.Second); v != nfqueue.NfAccept {
			t.Fatalf("verdict %d, want accept", v)
		}
		holdsDone(t, w)
	})

	t.Run("a pinned answer waits for its DSCP write", func(t *testing.T) {
		set := dscpHookSet("pin", "example.com", true, false)
		set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.13"}}
		w, _ := dscpHookWorker(t, set)
		gate := make(chan struct{})
		stubRouteHooks(t)
		stubDSCPHooks(t, gate)
		replies := watchReplies(t)
		q := newFakeQueue()

		w.processDnsPacket(&verdictCtx{id: 13, q: q}, queryPacket(), 40000, 53, dns.BuildQuery("pin.example.com", 13, dnsTypeA))
		noReply(t, replies, 40*time.Millisecond, "the pinned answer was sent before its DSCP write landed")
		close(gate)
		onlyAddress(t, oneReply(t, replies), "203.0.113.13")
		holdsDone(t, w)
	})

	t.Run("a redirected answer waits in line for its DSCP write", func(t *testing.T) {
		set := dscpHookSet("redirect", "example.com", true, true)
		w, cfg := dscpHookWorker(t, set)
		set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, "203.0.113.14")
		gate := make(chan struct{})
		route := stubRouteHooks(t)
		stubDSCPHooks(t, gate)
		replies := watchReplies(t)

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.resolveDNSRedirect(IPv4, set, cfg, dns.BuildQuery("www.example.com", 14, dnsTypeA), net.ParseIP("192.168.1.100").To4(), 40000, net.ParseIP("192.168.1.1").To4(), nil, 0)
		}()
		noReply(t, replies, 60*time.Millisecond, "the redirected answer was sent before its DSCP write landed")
		if got := route.snapshot()["sync redirect"]; got != 1 {
			t.Errorf("the routing write ran %d times while the DSCP write was pending, want 1", got)
		}
		close(gate)
		onlyAddress(t, oneReply(t, replies), "203.0.113.14")
		<-done
	})

	t.Run("a TCP answer waits in line for its DSCP write", func(t *testing.T) {
		set := dscpHookSet("tcp", "example.com", true, false)
		w, _ := dscpHookWorker(t, set)
		set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, "203.0.113.15")
		gate := make(chan struct{})
		stubRouteHooks(t)
		stubDSCPHooks(t, gate)

		answered := dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 15, dnsTypeA))
		dscpNotYet(t, answered, "the TCP answer was written before its DSCP write landed")
		close(gate)
		onlyAddress(t, dscpWithin(t, answered, "the TCP answer never came"), "203.0.113.15")
	})

	t.Run("an inline wait ends at the hold limit", func(t *testing.T) {
		set := dscpHookSet("tcp", "example.com", true, false)
		w, _ := dscpHookWorker(t, set)
		set.DNS.Enabled, set.DNS.DoHURL = true, dscpDoHServer(t, w, "203.0.113.16")
		holdLimit(t, 40*time.Millisecond)
		stubRouteHooks(t)
		stubDSCPHooks(t, make(chan struct{}))

		start := time.Now()
		onlyAddress(t, dscpWithin(t, dscpTCPAsk(t, w, dns.BuildQuery("www.example.com", 16, dnsTypeA)), "a DSCP write that never lands kept the TCP answer"), "203.0.113.16")
		if held := time.Since(start); held < 40*time.Millisecond {
			t.Errorf("the TCP answer was written after %s, before the %s limit", held, 40*time.Millisecond)
		}
	})
}
