package nfq

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/leaktest"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nflog"
	"github.com/florianl/go-nfqueue"
	"go.uber.org/goleak"
)

const (
	copyHookInput  = 1
	copyHookOutput = 3

	rigRouter   = "192.168.1.1"
	rigWAN      = "203.0.113.200"
	rigDevice   = "192.168.1.100"
	rigUpstream = "9.9.9.9"
	rigOutside  = "8.8.8.8"
)

type fakeCopySource struct {
	mu     sync.Mutex
	queue  []nflog.Packet
	closed chan struct{}
	once   sync.Once
	fail   chan error
	lost   atomic.Uint64
}

func newFakeCopySource() *fakeCopySource {
	return &fakeCopySource{closed: make(chan struct{}), fail: make(chan error, 1)}
}

func (s *fakeCopySource) Run(func(nflog.Packet)) error {
	select {
	case <-s.closed:
		return nil
	case err := <-s.fail:
		return err
	}
}

func (s *fakeCopySource) Drain(fn func(nflog.Packet)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) > 0 {
		p := s.queue[0]
		s.queue = s.queue[1:]
		fn(p)
	}
	return nil
}

func (s *fakeCopySource) Lost() uint64 { return s.lost.Load() }

func (s *fakeCopySource) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *fakeCopySource) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *fakeCopySource) hold(p nflog.Packet) {
	s.mu.Lock()
	s.queue = append(s.queue, p)
	s.mu.Unlock()
}

func stubCopySource(t *testing.T, sources ...*fakeCopySource) {
	t.Helper()
	prev := openDNSCopySource
	var mu sync.Mutex
	openDNSCopySource = func(uint16) (dnsCopySource, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(sources) == 0 {
			return nil, errors.New("no NFLOG group left in the test")
		}
		s := sources[0]
		sources = sources[1:]
		return s, nil
	}
	t.Cleanup(func() { openDNSCopySource = prev })
}

type modVerdict struct {
	verdict int
	packet  []byte
}

type fakeModQueue struct {
	verdicts chan modVerdict
}

func newFakeModQueue() *fakeModQueue { return &fakeModQueue{verdicts: make(chan modVerdict, 8)} }

func (q *fakeModQueue) SetVerdict(id uint32, verdict int) error {
	q.verdicts <- modVerdict{verdict: verdict}
	return nil
}

func (q *fakeModQueue) SetVerdictModPacket(id uint32, verdict int, packet []byte) error {
	q.verdicts <- modVerdict{verdict: verdict, packet: append([]byte(nil), packet...)}
	return nil
}

func (q *fakeModQueue) now(t *testing.T) modVerdict {
	t.Helper()
	select {
	case v := <-q.verdicts:
		return v
	default:
		t.Fatal("no verdict was given while the reply was being handled")
		return modVerdict{}
	}
}

func (q *fakeModQueue) within(t *testing.T, d time.Duration) modVerdict {
	t.Helper()
	select {
	case v := <-q.verdicts:
		return v
	case <-time.After(d):
		t.Fatalf("no verdict within %s", d)
		return modVerdict{}
	}
}

func (q *fakeModQueue) none(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case v := <-q.verdicts:
		t.Fatalf("verdict %d was given while b4's answer was still on its way", v.verdict)
	case <-time.After(d):
	}
}

type sentPackets chan []byte

func (s sentPackets) SendIPv4(p []byte, _ net.IP) error {
	s <- append([]byte(nil), p...)
	return nil
}

func (s sentPackets) SendIPv6(p []byte, _ net.IP) error {
	s <- append([]byte(nil), p...)
	return nil
}

func captureSends(t *testing.T) sentPackets {
	t.Helper()
	s := make(sentPackets, 8)
	prev := dnsAnswerSenderFor
	dnsAnswerSenderFor = func(*Worker) dnsAnswerSender { return s }
	t.Cleanup(func() { dnsAnswerSenderFor = prev })
	return s
}

func (s sentPackets) one(t *testing.T) []byte {
	t.Helper()
	select {
	case p := <-s:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("b4 never sent its answer")
		return nil
	}
}

func (s sentPackets) none(t *testing.T, d time.Duration, why string) {
	t.Helper()
	select {
	case <-s:
		t.Fatal(why)
	case <-time.After(d):
	}
}

func injectAfter(t *testing.T, d time.Duration) {
	t.Helper()
	prev := dnsClaimInjectAfter
	dnsClaimInjectAfter = d
	t.Cleanup(func() { dnsClaimInjectAfter = prev })
}

type copyRig struct {
	t      *testing.T
	w      *Worker
	cfg    *config.Config
	l      *dnsCopyListener
	src    *fakeCopySource
	nextID uint32
}

func newCopyRig(t *testing.T, tune func(*config.Config), sets ...*config.SetConfig) *copyRig {
	t.Helper()
	resetDNSAnswerCache()
	t.Cleanup(resetDNSAnswerCache)
	thisHost(t, rigRouter, rigWAN)
	cfg := config.NewConfig()
	cfg.Sets = sets
	if tune != nil {
		tune(&cfg)
	}
	w := passiveDNSWorker(t, &cfg)
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx, w.cancel, w.holdStop = ctx, cancel, make(chan struct{})
	t.Cleanup(func() {
		w.releaseHolds()
		cancel()
		w.wg.Wait()
	})
	src := newFakeCopySource()
	stubCopySource(t, src)
	l := newDNSCopyListener(w)
	w.dnsCopy = l
	if _, err := l.open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.close)
	l.setFamilies(true, true)
	l.enable(uint32(cfg.Queue.Mark))
	return &copyRig{t: t, w: w, cfg: &cfg, l: l, src: src}
}

func copyOf(hook uint8, src, dst string, sport uint16, query []byte) nflog.Packet {
	return nflog.Packet{
		Payload: makeV4UDPPacket(query, net.ParseIP(src), net.ParseIP(dst), sport, 53),
		Prefix:  config.DNSQueryCopyPrefix,
		Hook:    hook,
	}
}

func (r *copyRig) read(ps ...nflog.Packet) {
	for _, p := range ps {
		r.src.hold(p)
	}
	_ = r.src.Drain(r.l.handle)
}

func replyRaw(payload []byte, src, dst string, dport uint16) []byte {
	raw := makeV4UDPPacket(payload, net.ParseIP(src), net.ParseIP(dst), 53, dport)
	raw[1] = 0x28
	binary.BigEndian.PutUint16(raw[4:6], 0x4242)
	raw[6] = 0x40
	raw[8] = 57
	return raw
}

func (r *copyRig) reply(q verdictSetter, raw []byte) {
	r.t.Helper()
	pkt, ok := r.w.parseIPHeaders(raw)
	if !ok {
		r.t.Fatal("the reply did not parse")
	}
	r.nextID++
	udp := pkt.raw[pkt.ihl:]
	r.w.processDnsPacket(&verdictCtx{id: r.nextID, q: q}, pkt, binary.BigEndian.Uint16(udp[0:2]), binary.BigEndian.Uint16(udp[2:4]), udp[UDPHeaderLen:])
}

func rewritten(t *testing.T, v modVerdict, orig []byte) []byte {
	t.Helper()
	if v.verdict != nfqueue.NfAccept || v.packet == nil {
		t.Fatalf("verdict %d carrying %d bytes, want the reply accepted with b4's answer in place", v.verdict, len(v.packet))
	}
	p := v.packet
	if len(p) < 28 || p[1] != orig[1] || !bytes.Equal(p[4:6], orig[4:6]) || p[8] != orig[8] || !bytes.Equal(p[12:24], orig[12:24]) {
		t.Fatalf("the rewritten reply lost its own header: %x, was %x", p[:min(len(p), 28)], orig[:28])
	}
	return p[28:]
}

func passedUnchanged(t *testing.T, v modVerdict, why string) {
	t.Helper()
	if v.verdict != nfqueue.NfAccept || v.packet != nil {
		t.Fatalf("%s: verdict %d carrying %d bytes, want the reply passed unchanged", why, v.verdict, len(v.packet))
	}
}

func copyPinSet() *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "pins", "pins", true
	set.Targets.DomainsToMatch = []string{"example.com"}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.DNS.Pins = map[string][]string{"pin.example.com": {"203.0.113.12"}}
	return &set
}

func copyBlockSet(action string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "block", "block", true
	set.Targets.DomainsToMatch = []string{"ads.example"}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = action
	return &set
}

func copyRedirectSet() *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "redirect", "redirect", true
	set.Targets.DomainsToMatch = []string{"example.com"}
	set.DNS.Enabled = true
	return &set
}

type copyDoH struct {
	url  string
	hits atomic.Int32
}

func answerWith(ip string) func([]byte) []byte {
	return func(query []byte) []byte {
		return dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP(ip)})
	}
}

func copyDoHServer(t *testing.T, w *Worker, gate <-chan struct{}, answer func([]byte) []byte) *copyDoH {
	t.Helper()
	d := &copyDoH{}
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		d.hits.Add(1)
		query, err := io.ReadAll(r.Body)
		if err != nil || len(query) == 0 {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		if gate != nil {
			select {
			case <-gate:
			case <-release:
				return
			case <-r.Context().Done():
				return
			}
		}
		resp := answer(query)
		if resp == nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", dns.DoHContentType)
		_, _ = rw.Write(resp)
	}))
	client := srv.Client()
	cfg := w.getConfig()
	dohClientMu.Lock()
	prevClient, prevMark, prevTimeout := dohClient, dohClientMark, dohClientTimeout
	dohClient, dohClientMark, dohClientTimeout = client, int(cfg.MainInjectedMark()), cfg.DNSQueryTimeout()
	dohClientMu.Unlock()
	t.Cleanup(func() {
		close(release)
		dohClientMu.Lock()
		dohClient, dohClientMark, dohClientTimeout = prevClient, prevMark, prevTimeout
		dohClientMu.Unlock()
		client.CloseIdleConnections()
		srv.Close()
	})
	d.url = srv.URL + "/dns-query"
	return d
}

func countAction(lines []string, action string) int {
	n := 0
	for _, l := range lines {
		if strings.HasSuffix(l, ","+action) {
			n++
		}
	}
	return n
}

func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})
	return buf
}

func TestCopyNeverRunsTheAnswerBranch(t *testing.T) {
	resolver := "192.168.1.53"
	primary := config.NewSetConfig()
	primary.Id, primary.Name, primary.Enabled = "primary", "primary", true
	primary.Targets.DomainsToMatch = []string{"youtube.com"}
	primary.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:01"}
	primary.Targets.SourceDevicesExclude = true
	primary.Escalate.To, primary.Escalate.DNSThreshold = "backup", 1
	backup := config.NewSetConfig()
	backup.Id, backup.Name, backup.Enabled = "backup", "backup", true
	backup.Targets.DomainsToMatch = []string{"other.org"}
	r := newCopyRig(t, nil, &primary, &backup)
	r.w.ipToMac.Store(map[string]string{resolver: "AA:BB:CC:DD:EE:01"})

	raw := makeV4UDPPacket(dns.BuildQuery("youtube.com", 0x181, dnsTypeA), net.ParseIP(resolver), net.ParseIP(rigUpstream), 53, 53)
	r.read(nflog.Packet{Payload: raw, Prefix: config.DNSQueryCopyPrefix, Hook: nfHookForward})

	if _, _, ok := r.w.destState.GetEscalation("youtube.com"); ok {
		t.Fatal("a copied query from port 53 ran the answer branch and counted as a failed answer")
	}
	if live := r.l.claims.live.Load(); live != 0 {
		t.Fatalf("%d claims for a query that passes, want none", live)
	}
	if got := r.l.stats.ignored.Load(); got != 1 {
		t.Fatalf("copies_ignored = %d, want the copy from port 53 ignored", got)
	}
}

func TestCopiesFromPort53AreNotActedOn(t *testing.T) {
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil, copyBlockSet(config.BlockActionDrop))
	lines := connectionLines(t)

	r.read(copyOf(nfHookForward, rigDevice, rigOutside, 53, dns.BuildQuery("www.ads.example", 0x601, dnsTypeA)))
	if n := countAction(drainLines(lines), dnsActionBlock); n != 0 {
		t.Fatalf("%d %s rows for a query sent from port 53: copy mode never sees its answer, which the answer rules leave alone, so the Traffic page would claim a block that never happens", n, dnsActionBlock)
	}
	if live, drops := r.l.claims.live.Load(), r.l.stats.claimsDrop.Load(); live != 0 || drops != 0 {
		t.Fatalf("%d live claims and %d drop claims for a query whose answer is never queued", live, drops)
	}
	if got := r.l.stats.ignored.Load(); got != 1 {
		t.Fatalf("copies_ignored = %d, want 1", got)
	}
	if got := r.w.GetStats(); got != 0 {
		t.Fatalf("packets processed = %d for an ignored copy", got)
	}

	r.read(copyOf(nfHookForward, rigDevice, rigOutside, 40000, dns.BuildQuery("www.ads.example", 0x602, dnsTypeA)))
	if n := countAction(drainLines(lines), dnsActionBlock); n != 1 || r.l.stats.claimsDrop.Load() != 1 {
		t.Fatalf("%d %s rows and %d drop claims for a query from an ordinary port, want one each", n, dnsActionBlock, r.l.stats.claimsDrop.Load())
	}
}

func TestAQueryFloodedFromManySocketsKeepsFewClaimsAndLeavesRoomForOthers(t *testing.T) {
	injectAfter(t, time.Minute)
	stubRouteAwait(t)
	buf := captureLog(t)
	r := newCopyRig(t, nil, copyBlockSet(config.BlockActionDrop), copyPinSet())
	prev := dnsClaimMax
	dnsClaimMax = 512
	t.Cleanup(func() { dnsClaimMax = prev })

	const copies = 1000
	flood := dns.BuildQuery("www.ads.example", 0x4242, dnsTypeA)
	for i := range copies {
		r.l.handle(copyOf(nfHookForward, rigDevice, rigOutside, uint16(20000+i), flood))
	}
	tbl := r.l.claims
	tbl.mu.Lock()
	kept := len(tbl.byKey[claimKey("www.ads.example", 0x4242)])
	tbl.mu.Unlock()
	if kept == 0 || kept > dnsClaimKeyMax {
		t.Fatalf("%d claims for one query copied from %d sockets, want 1 to %d: each further copy, each reply and each answer of that query walks all of them under the lock the reply gate waits on", kept, copies, dnsClaimKeyMax)
	}
	if got := r.l.stats.claimsRefused.Load(); got != uint64(copies-kept) {
		t.Fatalf("claims_refused = %d, want the %d copies past the per-query limit", got, copies-kept)
	}
	log.Flush()
	if n := strings.Count(buf.String(), "the most it keeps for one query"); n != 1 {
		t.Fatalf("the per-query limit was logged %d times, want once a minute: %q", n, buf.String())
	}
	if strings.Contains(buf.String(), "the most it keeps, so") {
		t.Fatalf("the per-query limit was reported as the table's limit of %d: %q", dnsClaimMax, buf.String())
	}

	other := "192.168.1.101"
	r.read(copyOf(nfHookForward, other, rigOutside, 40000, dns.BuildQuery("pin.example.com", 0x501, dnsTypeA)))
	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x501, "pin.example.com", resolverIPs), rigOutside, other, 40000)
	r.reply(q, raw)
	onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.12")

	dropped := newFakeModQueue()
	r.reply(dropped, replyRaw(buildDNSResponse(0x4242, "www.ads.example", resolverIPs), rigOutside, rigDevice, 20000))
	if v := dropped.now(t); v.verdict != nfqueue.NfDrop || v.packet != nil {
		t.Fatalf("the reply to a claimed copy of the flooded query: verdict %d carrying %d bytes, want it dropped", v.verdict, len(v.packet))
	}
	passed := newFakeModQueue()
	r.reply(passed, replyRaw(buildDNSResponse(0x4242, "www.ads.example", resolverIPs), rigOutside, rigDevice, uint16(20000+copies-1)))
	passedUnchanged(t, passed.now(t), "the reply to a refused copy of the flooded query")
}

func TestCopyWithQRBitIsIgnored(t *testing.T) {
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())

	resp := buildDNSResponse(0x191, "pin.example.com", []net.IP{net.ParseIP("198.51.100.7")})
	r.read(nflog.Packet{Payload: makeV4UDPPacket(resp, net.ParseIP(rigDevice), net.ParseIP(rigRouter), 40000, 53), Prefix: config.DNSQueryCopyPrefix, Hook: copyHookInput})

	if got := r.l.stats.ignored.Load(); got != 1 {
		t.Fatalf("copies_ignored = %d, want the answer-shaped copy ignored", got)
	}
	if live := r.l.claims.live.Load(); live != 0 || r.w.GetStats() != 0 {
		t.Fatalf("%d claims and %d packets processed for an ignored copy", live, r.w.GetStats())
	}
}

func TestMarkedCopiesAreIgnored(t *testing.T) {
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())
	cfg := r.cfg

	marks := []struct {
		name string
		mark uint32
	}{
		{"b4's queue mark", uint32(cfg.Queue.Mark)},
		{"the queue mark with a set's routing bits", uint32(cfg.Queue.Mark) | 0x2bd8},
		{"Discovery's flow mark", uint32(cfg.DiscoveryFlowMark())},
		{"Discovery's injected mark", uint32(cfg.DiscoveryInjectedMark())},
		{"the Telegram bridge mark", config.TelegramBridgeMark},
		{"a dial that skips b4's DPI bypass", config.SelfDialRelayMark},
	}
	for i, tc := range marks {
		p := copyOf(copyHookOutput, rigWAN, rigOutside, uint16(50000+i), dns.BuildQuery("pin.example.com", uint16(0x200+i), dnsTypeA))
		p.Mark = tc.mark
		r.read(p)
		if live := r.l.claims.live.Load(); live != 0 {
			t.Fatalf("%s: a copy carrying mark 0x%x was acted on", tc.name, tc.mark)
		}
	}
	if got := r.l.stats.ignored.Load(); got != uint64(len(marks)) {
		t.Fatalf("copies_ignored = %d, want %d", got, len(marks))
	}

	p := copyOf(copyHookOutput, rigWAN, rigOutside, 50100, dns.BuildQuery("pin.example.com", 0x2ff, dnsTypeA))
	p.Mark = 0x10000
	r.read(p)
	if got := r.l.stats.claimsAnswer.Load(); got != 1 {
		t.Fatalf("claims_answer = %d: a copy marked by another program must still be acted on", got)
	}
}

func TestForeignPrefixIsIgnored(t *testing.T) {
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())

	for i, prefix := range []string{"", "other", "b4dns-old"} {
		p := copyOf(copyHookInput, rigDevice, rigRouter, uint16(40000+i), dns.BuildQuery("pin.example.com", uint16(0x210+i), dnsTypeA))
		p.Prefix = prefix
		r.read(p)
	}
	if live := r.l.claims.live.Load(); live != 0 {
		t.Fatalf("%d claims from copies another NFLOG rule sent", live)
	}
	if got := r.l.stats.ignored.Load(); got != 3 {
		t.Fatalf("copies_ignored = %d, want 3", got)
	}
	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40010, dns.BuildQuery("pin.example.com", 0x21f, dnsTypeA)))
	if got := r.l.stats.claimsAnswer.Load(); got != 1 {
		t.Fatalf("claims_answer = %d after b4's own copy, want 1", got)
	}
}

func TestCopiesHonourCaptureInterfacesPerHook(t *testing.T) {
	ResetIfaceTraffic()
	t.Cleanup(ResetIfaceTraffic)
	ifaceCache.Store(uint32(201), "br0")
	ifaceCache.Store(uint32(202), "eth0")
	t.Cleanup(func() {
		ifaceCache.Delete(uint32(201))
		ifaceCache.Delete(uint32(202))
	})
	stubRouteAwait(t)
	r := newCopyRig(t, func(c *config.Config) { c.Queue.Interfaces = []string{"br0"} }, copyPinSet())

	for i, tc := range []struct {
		name    string
		hook    uint8
		in, out uint32
		decided bool
	}{
		{"forwarded from the LAN", nfHookForward, 201, 202, true},
		{"forwarded from the uplink", nfHookForward, 202, 201, false},
		{"sent by the router out of the LAN", copyHookOutput, 0, 201, true},
		{"sent by the router out of the uplink", copyHookOutput, 0, 202, false},
		{"asked of the router from the LAN", copyHookInput, 201, 0, true},
		{"asked of the router from the uplink", copyHookInput, 202, 0, false},
	} {
		p := copyOf(tc.hook, rigDevice, rigOutside, uint16(40000+i), dns.BuildQuery("pin.example.com", uint16(0x220+i), dnsTypeA))
		p.InDev, p.OutDev = tc.in, tc.out
		before := r.l.stats.claimsAnswer.Load()
		r.read(p)
		if decided := r.l.stats.claimsAnswer.Load() > before; decided != tc.decided {
			t.Errorf("%s: acted on = %v, want %v", tc.name, decided, tc.decided)
		}
	}
	got := IfaceTraffic()
	if got["br0"].Arriving != 2 || got["br0"].Leaving != 1 || got["eth0"].Arriving != 2 || got["eth0"].Leaving != 1 {
		t.Errorf("interface counters %v, want the copies counted where Capture Interfaces looked", got)
	}
}

func TestCopiesBeforeStartAreDiscarded(t *testing.T) {
	stubRouteAwait(t)
	r := newCopyRig(t, nil, copyPinSet())
	r.l.disable()

	p := copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x231, dnsTypeA))
	r.read(p)
	if live := r.l.claims.live.Load(); live != 0 || r.w.GetStats() != 0 {
		t.Fatalf("%d claims and %d packets processed before the pool started", live, r.w.GetStats())
	}
	if copies, ignored := r.l.stats.copies.Load(), r.l.stats.ignored.Load(); copies != 1 || ignored != 1 {
		t.Fatalf("copies %d, ignored %d: a copy read before the start must be counted and dropped", copies, ignored)
	}
	if r.l.active(IPv4) {
		t.Fatal("the answer gate is active before the pool started")
	}

	r.l.enable(uint32(r.cfg.Queue.Mark))
	r.read(p)
	if got := r.l.stats.claimsAnswer.Load(); got != 1 {
		t.Fatalf("claims_answer = %d after the start, want 1", got)
	}
}

func TestDeadCopyListenerIsReopened(t *testing.T) {
	buf := captureLog(t)
	dnsCopyStopWarned.Store(false)
	t.Cleanup(func() { dnsCopyStopWarned.Store(false) })
	r := newCopyRig(t, nil)
	first, second := r.src, newFakeCopySource()
	stubCopySource(t, second)

	pool := &Pool{dnsCopy: r.l}
	if !pool.DNSQueryCopyAlive() {
		t.Fatal("a bound listener reads as dead")
	}
	if group, err := pool.OpenDNSQueryCopy(); err != nil || group != 537 {
		t.Fatalf("open of a live listener = %d, %v, want its group 537", group, err)
	}
	first.fail <- errors.New("recvfrom: bad file descriptor")
	if !waitFor(t, func() bool { return !pool.DNSQueryCopyAlive() }) {
		t.Fatal("the listener still reads as alive after its reader stopped")
	}
	select {
	case <-r.l.source.Load().done:
	case <-time.After(5 * time.Second):
		t.Fatal("the reader of the stopped listener never returned")
	}
	log.Flush()
	if n := strings.Count(buf.String(), "the NFLOG copy of DNS queries stopped"); n != 1 {
		t.Fatalf("the stop was logged %d times, want once: %q", n, buf.String())
	}
	if group, err := pool.OpenDNSQueryCopy(); err != nil || group != 537 {
		t.Fatalf("reopen = %d, %v", group, err)
	}
	if !pool.DNSQueryCopyAlive() || !first.isClosed() || second.isClosed() {
		t.Fatalf("after the reopen: alive %v, old source closed %v, new source closed %v", pool.DNSQueryCopyAlive(), first.isClosed(), second.isClosed())
	}

	pool.SetDNSQueryCopyFamilies(false, true)
	if r.l.active(IPv4) || !r.l.active(IPv6) {
		t.Fatal("the gate must follow the families the firewall copies")
	}

	var none *Pool
	if _, err := none.OpenDNSQueryCopy(); err == nil || none.DNSQueryCopyAlive() {
		t.Fatal("a pool without a listener must refuse to copy")
	}
	discovery := &Pool{}
	if _, err := discovery.OpenDNSQueryCopy(); !errors.Is(err, errNoDNSCopyListener) {
		t.Fatalf("a Discovery pool opened a listener: %v", err)
	}
	discovery.SetDNSQueryCopyFamilies(true, true)
}

func TestClosedCopyListenerStaysClosed(t *testing.T) {
	r := newCopyRig(t, nil)
	r.l.close()
	if !r.src.isClosed() || r.l.alive() {
		t.Fatal("close left the NFLOG source open")
	}
	if _, err := r.l.open(); !errors.Is(err, errDNSCopyClosed) {
		t.Fatalf("open after close = %v, want a refusal so no reader outlives the pool", err)
	}
}

func TestClaimExpiryCapAndSweep(t *testing.T) {
	injectAfter(t, time.Minute)
	stubRouteAwait(t)
	buf := captureLog(t)
	r := newCopyRig(t, nil, copyBlockSet(config.BlockActionDrop), copyPinSet())

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("www.ads.example", 0x241, dnsTypeA)))
	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40001, dns.BuildQuery("pin.example.com", 0x242, dnsTypeA)))
	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x242, "pin.example.com", []net.IP{net.ParseIP("198.51.100.7")}), rigRouter, rigDevice, 40001)
	r.reply(q, raw)
	rewritten(t, q.now(t), raw)

	r.l.sweep(time.Now())
	if live := r.l.claims.live.Load(); live != 2 {
		t.Fatalf("%d live claims after a sweep before they expired, want 2", live)
	}
	r.l.sweep(time.Now().Add(time.Minute))
	if live := r.l.claims.live.Load(); live != 0 {
		t.Fatalf("%d live claims after they expired, want none", live)
	}
	if got := r.l.counters().ExpiredUnanswered; got != 1 {
		t.Fatalf("expired_unanswered = %d, want only the drop claim that never saw a reply", got)
	}

	prev := dnsClaimMax
	dnsClaimMax = 1
	t.Cleanup(func() { dnsClaimMax = prev })
	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40002, dns.BuildQuery("pin.example.com", 0x243, dnsTypeA)))
	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40003, dns.BuildQuery("pin.example.com", 0x244, dnsTypeA)))
	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40004, dns.BuildQuery("pin.example.com", 0x245, dnsTypeA)))
	if got := r.l.stats.claimsRefused.Load(); got != 2 {
		t.Fatalf("claims_refused = %d, want the two copies past the cap", got)
	}
	log.Flush()
	if n := strings.Count(buf.String(), "DNS answers to deliver, the most it keeps"); n != 1 {
		t.Fatalf("the full table was logged %d times, want once a minute: %q", n, buf.String())
	}

	refused := newFakeModQueue()
	r.reply(refused, replyRaw(buildDNSResponse(0x244, "pin.example.com", []net.IP{net.ParseIP("198.51.100.8")}), rigRouter, rigDevice, 40003))
	passedUnchanged(t, refused.now(t), "the reply of a refused copy")
	kept := newFakeModQueue()
	raw = replyRaw(buildDNSResponse(0x243, "pin.example.com", []net.IP{net.ParseIP("198.51.100.9")}), rigRouter, rigDevice, 40002)
	r.reply(kept, raw)
	onlyAddress(t, rewritten(t, kept.now(t), raw), "203.0.113.12")
}

func TestLostCopiesAreReported(t *testing.T) {
	buf := captureLog(t)
	r := newCopyRig(t, nil)
	start := time.Now()

	r.src.lost.Store(3)
	r.l.sweep(start)
	r.src.lost.Store(5)
	r.l.sweep(start.Add(time.Minute))
	log.Flush()
	if got := buf.String(); strings.Count(got, "copies of DNS queries were lost") != 1 || !strings.Contains(got, "DNS: 3 copies of DNS queries were lost") {
		t.Fatalf("lost copies logged as %q, want one line for the first three", got)
	}
	r.l.sweep(start.Add(6 * time.Minute))
	log.Flush()
	if got := buf.String(); !strings.Contains(got, "DNS: 2 copies of DNS queries were lost") {
		t.Fatalf("the next report after five minutes is missing: %q", got)
	}
	if got := r.l.counters().CopiesLost; got != 5 {
		t.Fatalf("copies_lost = %d, want 5", got)
	}
}

func TestPoolStopLeavesNoCopyGoroutines(t *testing.T) {
	opts := leaktest.Options(goleak.IgnoreCurrent())
	t.Cleanup(func() { goleak.VerifyNone(t, opts...) })
	injectAfter(t, time.Minute)
	gate := make(chan struct{})
	stubRouteAwait(t, gate)
	thisHost(t, rigRouter, rigWAN)
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{copyPinSet()}
	w := passiveDNSWorker(t, &cfg)
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx, w.cancel, w.holdStop = ctx, cancel, make(chan struct{})
	src := newFakeCopySource()
	stubCopySource(t, src)
	pool := &Pool{Workers: []*Worker{w}, stopCleanup: make(chan struct{}), dnsCopy: newDNSCopyListener(w)}
	w.dnsCopy = pool.dnsCopy
	if _, err := pool.OpenDNSQueryCopy(); err != nil {
		t.Fatal(err)
	}
	pool.SetDNSQueryCopyFamilies(true, false)
	pool.dnsCopy.enable(uint32(cfg.Queue.Mark))
	r := &copyRig{t: t, w: w, cfg: &cfg, l: pool.dnsCopy, src: src}

	r.read(copyOf(copyHookInput, rigDevice, rigRouter, 40000, dns.BuildQuery("pin.example.com", 0x251, dnsTypeA)))
	q := newFakeModQueue()
	raw := replyRaw(buildDNSResponse(0x251, "pin.example.com", []net.IP{net.ParseIP("198.51.100.7")}), rigRouter, rigDevice, 40000)
	r.reply(q, raw)
	q.none(t, 30*time.Millisecond)

	pool.Stop()
	onlyAddress(t, rewritten(t, q.now(t), raw), "203.0.113.12")
	if !src.isClosed() || pool.DNSQueryCopyAlive() || pool.dnsCopy.claims.live.Load() != 0 {
		t.Fatal("the pool stopped with its NFLOG source open or claims left")
	}
}
