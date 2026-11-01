package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
)

const (
	setOrderClient       = "192.168.188.220"
	setOrderOtherClient  = "192.168.188.221"
	setOrderClientMAC    = "aa:bb:cc:00:01:dc"
	setOrderOtherMAC     = "aa:bb:cc:00:01:dd"
	setOrderGGCAddr      = "37.18.153.140"
	setOrderGGCNeighbour = "37.18.153.141"
	setOrderGoogleAddr   = "173.194.0.10"
	setOrderGGCName      = "rr1---sn-nxu-nufl.googlevideo.com"
	setOrderSiblingName  = "rr2---sn-nxu-nufl.googlevideo.com"
	setOrderGWName       = "rr1---sn-abc-xyz1.googlevideo.com"
	setOrderYouTubeName  = "www.youtube.com"
	setOrderOtherName    = "cdn.other.example"
	setOrderUnlisted     = "example.org"
	setOrderGGCSet       = "YT-GGC-RU"
	setOrderGWSet        = "YT-GW-AS15169"
	setOrderRTSet        = "YT-RT-V9"
	setOrderGWBackup     = "GW-backup"
	setOrderGWBackup2    = "GW-backup-2"
	setOrderGGCBackup    = "GGC-backup"
	setOrderGGCBackup2   = "GGC-backup-2"
	setOrderDeviceSet    = "GW-device"
	setOrderBlockGW      = "Block-GW"
	setOrderBlockGGC     = "Block-GGC"
	setOrderCatchAll     = "Catch-all"
	setOrderOtherSet     = "Other"
	setOrderOtherBackup  = "O-backup"
	setOrderDeviceSite   = "Device-site"
	setOrderDeviceBackup = "Device-backup"
	setOrderEscalated    = "Escalated"
	setOrderNoSet        = ""
	setOrderQUICPort     = 51820
)

var (
	setOrderGGCRanges  = []string{"79.133.77.32/27", "188.128.43.192/27", "213.59.213.96/24", "37.18.153.140/29", "188.43.61.140/27"}
	setOrderQUICDCID   = []byte{0x37, 0x18, 0x15, 0x3c, 0x01, 0x02, 0x03, 0x04}
	setOrderQUICServer = []byte{0x5e, 0x7e, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	setOrderBothMACs   = map[string]string{setOrderClient: setOrderClientMAC, setOrderOtherClient: setOrderOtherMAC}
)

func newSetOrderStrategySet(id, name string, splitAt int, fakeTTL uint8) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = name
	set.Enabled = true
	set.Fragmentation.Strategy = "tcp"
	set.Fragmentation.StrategyPool = nil
	set.Fragmentation.SNIPosition = splitAt
	set.Fragmentation.SNIPositionMax = 0
	set.Fragmentation.MiddleSNI = false
	set.Fragmentation.ReverseOrder = false
	set.TCP.Seg2Delay = 0
	set.TCP.Seg2DelayMax = 0
	set.TCP.Desync.Mode = config.ConfigOff
	set.TCP.Win.Mode = config.ConfigOff
	set.Faking.SNI = false
	set.Faking.SNIMutation.Mode = config.ConfigOff
	set.Faking.TTL = fakeTTL
	set.UDP.Mode = config.UDPModeFake
	set.UDP.FakeSeqLength = 1
	set.UDP.Seg2Delay = 0
	set.UDP.Seg2DelayMax = 0
	return &set
}

func newSetOrderGGCSet() *config.SetConfig {
	set := newSetOrderStrategySet("yt-ggc-ru", setOrderGGCSet, 2, 3)
	set.Targets.IPs = append([]string(nil), setOrderGGCRanges...)
	set.DSCP.Enabled = true
	set.DSCP.Value = 10
	return set
}

func newSetOrderGGCSetWith(on bool) *config.SetConfig {
	set := newSetOrderGGCSet()
	set.Targets.IPPriority = on
	return set
}

func newSetOrderGWSet() *config.SetConfig {
	return newSetOrderNameSet("yt-gw-as15169", setOrderGWSet, 3, 5, "googlevideo.com")
}

func newSetOrderRTSet() *config.SetConfig {
	return newSetOrderNameSet("yt-rt-v9", setOrderRTSet, 4, 7, "googlevideo.com", "youtube.com", "ytimg.com", "ggpht.com", "youtu.be")
}

func newSetOrderNameSet(id, name string, splitAt int, fakeTTL uint8, domains ...string) *config.SetConfig {
	set := newSetOrderStrategySet(id, name, splitAt, fakeTTL)
	set.Targets.SNIDomains = domains
	return set
}

func newSetOrderBackupSet(id, name string, splitAt int, fakeTTL uint8) *config.SetConfig {
	return newSetOrderNameSet(id, name, splitAt, fakeTTL, id+".invalid")
}

func newSetOrderBlockSet(id, name string, splitAt int, fakeTTL uint8) *config.SetConfig {
	set := newSetOrderStrategySet(id, name, splitAt, fakeTTL)
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = config.BlockActionDrop
	return set
}

func setOrderReporterSets(on bool) []*config.SetConfig {
	return []*config.SetConfig{newSetOrderGGCSetWith(on), newSetOrderGWSet()}
}

func setOrderReportersThreeSets(on bool) []*config.SetConfig {
	var sets []*config.SetConfig
	for i := 0; i < 5; i++ {
		filler := newSetOrderNameSet(fmt.Sprintf("filler-%d", i), fmt.Sprintf("Filler %d", i), 20+i, uint8(30+i), fmt.Sprintf("site%d.example", i))
		filler.Targets.IPs = []string{fmt.Sprintf("198.51.100.%d/32", 10+i)}
		sets = append(sets, filler)
	}
	return append(sets, newSetOrderGGCSetWith(on), newSetOrderGWSet(), newSetOrderRTSet())
}

type setOrderRow struct {
	proto, sniSet, domain, src, ipSet, dst, tls, meta string
}

type setOrderObs struct {
	what     string
	verdict  engine.PacketVerdict
	rows     []setOrderRow
	strategy string
	injected int
}

func (o setOrderObs) picked() string {
	if len(o.rows) == 0 {
		return setOrderNoSet
	}
	if o.rows[0].sniSet != "" {
		return o.rows[0].sniSet
	}
	return o.rows[0].ipSet
}

func (o setOrderObs) blocked() bool {
	for _, r := range o.rows {
		if r.meta == "block" {
			return true
		}
	}
	return false
}

func setOrderVerdict(v engine.PacketVerdict) string {
	switch v {
	case engine.VerdictAccept:
		return "accept"
	case engine.VerdictDrop:
		return "drop"
	case engine.VerdictDropFlow:
		return "drop-flow"
	}
	return strconv.Itoa(int(v))
}

func (o setOrderObs) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: picked=%q", o.what, o.picked())
	if len(o.rows) == 0 {
		b.WriteString(" (no Traffic line)")
	}
	for _, r := range o.rows {
		fmt.Fprintf(&b, " [Traffic line %s %s -> %s domain=%q tls=%q sni-set=%q ip-set=%q meta=%q]", r.proto, r.src, r.dst, r.domain, r.tls, r.sniSet, r.ipSet, r.meta)
	}
	fmt.Fprintf(&b, " verdict=%s strategy-ran=%q injected=%d", setOrderVerdict(o.verdict), o.strategy, o.injected)
	return b.String()
}

type setOrderHarness struct {
	w      *Worker
	ch     chan string
	sent   func() [][]byte
	splits map[int]string
	ttls   map[uint8]string
}

func newSetOrderHarness(t *testing.T, macs map[string]string, sets []*config.SetConfig) *setOrderHarness {
	t.Helper()
	cfg := newUDPGateConfig(t, sets...)
	w := newTestWorker(t, &cfg)
	if len(macs) > 0 {
		w.ipToMac.Store(macs)
	}
	h := &setOrderHarness{w: w, sent: captureInjected(t, w), splits: map[int]string{}, ttls: map[uint8]string{}}
	h.ch, _ = log.GetConnectionHub().Subscribe()
	t.Cleanup(func() { log.GetConnectionHub().Unsubscribe(h.ch) })
	h.fingerprints(sets)
	return h
}

func (h *setOrderHarness) fingerprints(sets []*config.SetConfig) {
	for _, s := range sets {
		h.splits[s.Fragmentation.SNIPosition] = s.Name
		h.ttls[s.Faking.TTL] = s.Name
	}
}

func (h *setOrderHarness) reload(t *testing.T, sets []*config.SetConfig) {
	t.Helper()
	cfg := newUDPGateConfig(t, sets...)
	cfg.ConfigPath = h.w.getConfig().ConfigPath
	matcher := buildMatcher(&cfg)
	matcher.TransferLearnedIPs(h.w.getMatcher())
	h.w.cfg.Store(&cfg)
	h.w.matcher.Store(matcher)
	h.w.tlsCache.ForgetDecisions(matcher)
	h.fingerprints(sets)
}

func (h *setOrderHarness) drain() []string {
	var lines []string
	for {
		select {
		case line := <-h.ch:
			lines = append(lines, line)
		default:
			return lines
		}
	}
}

func (h *setOrderHarness) fingerprint(sent [][]byte) string {
	ran := map[string]bool{}
	split := 0
	for _, p := range sent {
		if len(p) < 20 || p[0]>>4 != 4 {
			continue
		}
		ihl := int(p[0]&0x0f) * 4
		switch p[9] {
		case 17:
			if name, ok := h.ttls[p[8]]; ok {
				ran[name] = true
			}
		case 6:
			if len(p) < ihl+20 {
				continue
			}
			if n := len(p) - ihl - int(p[ihl+12]>>4)*4; n > 0 && (split == 0 || n < split) {
				split = n
			}
		}
	}
	if split > 0 {
		if name, ok := h.splits[split]; ok {
			ran[name] = true
		} else {
			ran[fmt.Sprintf("unknown split at %d", split)] = true
		}
	}
	names := make([]string, 0, len(ran))
	for name := range ran {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "+")
}

func (h *setOrderHarness) send(t *testing.T, what string, raw []byte, client, dst string, sport uint16) setOrderObs {
	t.Helper()
	h.drain()
	h.sent()
	obs := setOrderObs{what: what, verdict: h.w.ProcessPacket(raw)}
	injected := h.sent()
	obs.injected = len(injected)
	obs.strategy = h.fingerprint(injected)
	from := net.JoinHostPort(client, strconv.Itoa(int(sport)))
	to := net.JoinHostPort(dst, "443")
	for _, line := range h.drain() {
		f := strings.Split(line, ",")
		if len(f) < 9 {
			continue
		}
		row := setOrderRow{proto: f[1], sniSet: f[2], domain: f[3], src: f[4], ipSet: f[5], dst: f[6], tls: f[8]}
		if len(f) > 9 {
			row.meta = f[9]
		}
		if row.src == from && row.dst == to {
			obs.rows = append(obs.rows, row)
		}
	}
	t.Logf("observed %s", obs)
	return obs
}

type setOrderConn struct {
	client   string
	dst      string
	sport    uint16
	isn      uint32
	next     uint32
	helloSeq uint32
	hello    []byte
}

func newSetOrderConn(dst string, sport uint16, isn uint32) *setOrderConn {
	return &setOrderConn{client: setOrderClient, dst: dst, sport: sport, isn: isn, next: isn + 1}
}

func (c *setOrderConn) from(client string) *setOrderConn {
	c.client = client
	return c
}

func (c *setOrderConn) key() string {
	return fmt.Sprintf(connKeyFormat, c.client, c.sport, c.dst, 443)
}

func setOrderTCP(c *setOrderConn, seq uint32, flags byte, payload []byte) []byte {
	pkt := makeV4TCPPacketFlags(payload, seq, flags)
	copy(pkt[12:16], net.ParseIP(c.client).To4())
	copy(pkt[16:20], net.ParseIP(c.dst).To4())
	binary.BigEndian.PutUint16(pkt[20:22], c.sport)
	return pkt
}

func (h *setOrderHarness) tcp(t *testing.T, c *setOrderConn, what string, seq uint32, flags byte, payload []byte) setOrderObs {
	t.Helper()
	return h.send(t, fmt.Sprintf("%s %s:%d -> %s", what, c.client, c.sport, c.dst), setOrderTCP(c, seq, flags, payload), c.client, c.dst, c.sport)
}

func (h *setOrderHarness) syn(t *testing.T, c *setOrderConn) setOrderObs {
	t.Helper()
	return h.tcp(t, c, "TCP SYN", c.isn, 0x02, nil)
}

func (h *setOrderHarness) hello(t *testing.T, c *setOrderConn, name string) setOrderObs {
	t.Helper()
	c.hello, c.helloSeq = buildClientHello(name, 16, 0xAB), c.next
	obs := h.tcp(t, c, "TCP ClientHello "+name, c.next, 0x18, c.hello)
	c.next += uint32(len(c.hello))
	return obs
}

func (h *setOrderHarness) retransmittedHello(t *testing.T, c *setOrderConn) setOrderObs {
	t.Helper()
	return h.tcp(t, c, "retransmitted TCP ClientHello", c.helloSeq, 0x18, c.hello)
}

func (h *setOrderHarness) splitHello(t *testing.T, c *setOrderConn, name string) (setOrderObs, setOrderObs) {
	t.Helper()
	full := buildClientHello(name, 1400, 0xAB)
	const cut = 1396
	first := h.tcp(t, c, "first segment of a split ClientHello "+name, c.next, 0x18, full[:cut])
	second := h.tcp(t, c, "second segment of a split ClientHello "+name, c.next+cut, 0x18, full[cut:])
	c.next += uint32(len(full))
	return first, second
}

func setOrderClientHello(exts ...[]byte) []byte {
	var all []byte
	for _, e := range exts {
		all = append(all, e...)
	}
	body := []byte{0x03, 0x03}
	body = append(body, bytes.Repeat([]byte{0x11}, 32)...)
	body = append(body, 32)
	body = append(body, bytes.Repeat([]byte{0x22}, 32)...)
	body = append(body, 0x00, 0x02, 0x13, 0x01, 0x01, 0x00)
	body = binary.BigEndian.AppendUint16(body, uint16(len(all)))
	body = append(body, all...)
	handshake := append([]byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	record := binary.BigEndian.AppendUint16([]byte{0x16, 0x03, 0x01}, uint16(len(handshake)))
	return append(record, handshake...)
}

func setOrderTLS12Hello(name string) []byte {
	return setOrderClientHello(buildSNIExtension(name))
}

func setOrderNameFirstHello(name string) []byte {
	return setOrderClientHello(buildSNIExtension(name), buildExtension(21, make([]byte, 1400)), buildExtension(43, []byte{0x02, 0x03, 0x04}))
}

func (h *setOrderHarness) helloWith(t *testing.T, c *setOrderConn, what string, hello []byte) setOrderObs {
	t.Helper()
	c.hello, c.helloSeq = hello, c.next
	obs := h.tcp(t, c, what, c.next, 0x18, hello)
	c.next += uint32(len(hello))
	return obs
}

func (h *setOrderHarness) segments(t *testing.T, c *setOrderConn, what string, hello []byte, cut int) (setOrderObs, setOrderObs) {
	t.Helper()
	first := h.tcp(t, c, "first segment of "+what, c.next, 0x18, hello[:cut])
	second := h.tcp(t, c, "second segment of "+what, c.next+uint32(cut), 0x18, hello[cut:])
	c.next += uint32(len(hello))
	return first, second
}

func (h *setOrderHarness) evict(t *testing.T, c *setOrderConn) {
	t.Helper()
	h.w.tlsCache.mu.Lock()
	info, ok := h.w.tlsCache.conns[c.key()]
	if ok {
		info.lastSeen = time.Now().Add(-121 * time.Second)
	}
	h.w.tlsCache.mu.Unlock()
	if !ok {
		t.Fatalf("no TLS decision for %s to evict", c.key())
	}
	h.w.tlsCache.Cleanup()
	if _, ok := h.w.tlsCache.Decision(c.key()); ok {
		t.Fatalf("the TLS decision for %s outlived the cleanup", c.key())
	}
}

func (h *setOrderHarness) data(t *testing.T, c *setOrderConn) setOrderObs {
	t.Helper()
	payload := tlsAppData(64)
	obs := h.tcp(t, c, "TCP application data", c.next, 0x18, payload)
	c.next += uint32(len(payload))
	return obs
}

func (h *setOrderHarness) quic(t *testing.T, dst, what string, payload []byte) setOrderObs {
	t.Helper()
	raw := makeV4UDPPacket(payload, net.ParseIP(setOrderClient), net.ParseIP(dst), setOrderQUICPort, 443)
	return h.send(t, what+" -> "+dst, raw, setOrderClient, dst, setOrderQUICPort)
}

func (h *setOrderHarness) quicInitial(t *testing.T, name string) setOrderObs {
	t.Helper()
	return h.quic(t, setOrderGGCAddr, "QUIC Initial "+name, buildQUICInitialWithSNI(t, setOrderQUICDCID, name))
}

func (h *setOrderHarness) quicShortHeader(t *testing.T) setOrderObs {
	t.Helper()
	return h.quic(t, setOrderGGCAddr, "QUIC short header", append(append([]byte{0x41}, setOrderQUICServer...), make([]byte, 40)...))
}

func setOrderFirstConnection(t *testing.T, h *setOrderHarness) *setOrderConn {
	t.Helper()
	c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	h.syn(t, c)
	h.hello(t, c, setOrderGGCName)
	return c
}

type setOrderCase struct {
	id      string
	payload bool
	run     func(t *testing.T, h *setOrderHarness) setOrderObs
}

var setOrderCases = []setOrderCase{
	{"a", false, func(t *testing.T, h *setOrderHarness) setOrderObs {
		return h.syn(t, newSetOrderConn(setOrderGGCAddr, 38854, 1000))
	}},
	{"b", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
		h.syn(t, c)
		return h.hello(t, c, setOrderGGCName)
	}},
	{"c", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		return h.data(t, setOrderFirstConnection(t, h))
	}},
	{"d-syn", false, func(t *testing.T, h *setOrderHarness) setOrderObs {
		setOrderFirstConnection(t, h)
		return h.syn(t, newSetOrderConn(setOrderGGCAddr, 38856, 20000))
	}},
	{"d-hello", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		setOrderFirstConnection(t, h)
		c := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
		h.syn(t, c)
		return h.hello(t, c, setOrderGGCName)
	}},
	{"d-unlisted", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		setOrderFirstConnection(t, h)
		c := newSetOrderConn(setOrderGGCAddr, 38858, 30000)
		h.syn(t, c)
		return h.hello(t, c, setOrderUnlisted)
	}},
	{"d-sibling", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		setOrderFirstConnection(t, h)
		c := newSetOrderConn(setOrderGGCAddr, 38864, 50000)
		h.syn(t, c)
		return h.hello(t, c, setOrderSiblingName)
	}},
	{"d-after-sibling-syn", false, func(t *testing.T, h *setOrderHarness) setOrderObs {
		setOrderFirstConnection(t, h)
		c := newSetOrderConn(setOrderGGCAddr, 38864, 50000)
		h.syn(t, c)
		h.hello(t, c, setOrderSiblingName)
		return h.syn(t, newSetOrderConn(setOrderGGCAddr, 38868, 80000))
	}},
	{"e-initial", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		return h.quicInitial(t, setOrderGGCName)
	}},
	{"e-short", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		h.quicInitial(t, setOrderGGCName)
		return h.quicShortHeader(t)
	}},
	{"f1-hello", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		c := newSetOrderConn(setOrderGoogleAddr, 38860, 40000)
		h.syn(t, c)
		return h.hello(t, c, setOrderGWName)
	}},
	{"f1-data", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		c := newSetOrderConn(setOrderGoogleAddr, 38860, 40000)
		h.syn(t, c)
		h.hello(t, c, setOrderGWName)
		return h.data(t, c)
	}},
	{"f2-data", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		c := newSetOrderConn(setOrderGGCAddr, 38866, 60000)
		h.syn(t, c)
		return h.data(t, c)
	}},
	{"f3-data", true, func(t *testing.T, h *setOrderHarness) setOrderObs {
		setOrderFirstConnection(t, h)
		c := newSetOrderConn(setOrderGGCNeighbour, 38862, 70000)
		h.syn(t, c)
		return h.data(t, c)
	}},
}

func expectSetOrder(t *testing.T, id string, obs setOrderObs, want string, payload bool) {
	t.Helper()
	if got := obs.picked(); got != want {
		t.Errorf("case %s, %s: want set %q, b4 picked %q; %s", id, obs.what, want, got, obs)
	}
	if payload && obs.strategy != want {
		t.Errorf("case %s, %s: want the strategy of %q to run, ran %q; %s", id, obs.what, want, obs.strategy, obs)
	}
}

func expectBlocked(t *testing.T, obs setOrderObs, by string) {
	t.Helper()
	if got := obs.picked(); got != by || !obs.blocked() {
		t.Errorf("%s: want it blocked by %q, picked %q blocked=%v; %s", obs.what, by, got, obs.blocked(), obs)
	}
}

func expectNotBlocked(t *testing.T, obs setOrderObs, want string) {
	t.Helper()
	if obs.blocked() {
		t.Errorf("%s: blocked, want %q to handle it; %s", obs.what, want, obs)
	}
	expectSetOrder(t, "-", obs, want, true)
}

func setOrderExpect(rest, other string, otherCases ...string) map[string]string {
	want := map[string]string{}
	for _, c := range setOrderCases {
		want[c.id] = rest
	}
	for _, id := range otherCases {
		want[id] = other
	}
	return want
}

func runSetOrderMatrix(t *testing.T, macs map[string]string, build func() []*config.SetConfig, want map[string]string) {
	t.Helper()
	for _, c := range setOrderCases {
		expected, ok := want[c.id]
		if !ok {
			t.Errorf("no expectation for case %s", c.id)
			continue
		}
		t.Run(c.id, func(t *testing.T) {
			h := newSetOrderHarness(t, macs, build())
			expectSetOrder(t, c.id, c.run(t, h), expected, c.payload)
		})
	}
}

func TestSetOrderSwitchOffKeepsTheNameSetFirst(t *testing.T) {
	runSetOrderMatrix(t, nil, func() []*config.SetConfig { return setOrderReporterSets(false) },
		setOrderExpect(setOrderGWSet, setOrderGGCSet, "a", "e-short", "f2-data", "f3-data"))
}

func TestIPPriorityKeepsEveryConnectionToTheAddressSet(t *testing.T) {
	runSetOrderMatrix(t, nil, func() []*config.SetConfig { return setOrderReporterSets(true) },
		setOrderExpect(setOrderGGCSet, setOrderGWSet, "f1-hello", "f1-data"))
}

func TestIPPriorityOnTheReportersThreeSets(t *testing.T) {
	t.Run("switch on", func(t *testing.T) {
		runSetOrderMatrix(t, nil, func() []*config.SetConfig { return setOrderReportersThreeSets(true) },
			setOrderExpect(setOrderGGCSet, setOrderGWSet, "f1-hello", "f1-data"))
	})
	t.Run("switch off", func(t *testing.T) {
		runSetOrderMatrix(t, nil, func() []*config.SetConfig { return setOrderReportersThreeSets(false) },
			setOrderExpect(setOrderGWSet, setOrderGGCSet, "a", "e-short", "f2-data", "f3-data"))
	})
	for _, on := range []bool{true, false} {
		t.Run(fmt.Sprintf("a YouTube name, switch on=%v", on), func(t *testing.T) {
			h := newSetOrderHarness(t, nil, setOrderReportersThreeSets(on))
			onGGC := newSetOrderConn(setOrderGGCAddr, 38870, 90000)
			h.syn(t, onGGC)
			want := setOrderRTSet
			if on {
				want = setOrderGGCSet
			}
			expectSetOrder(t, "youtube on the GGC address", h.hello(t, onGGC, setOrderYouTubeName), want, true)
			onGoogle := newSetOrderConn(setOrderGoogleAddr, 38872, 91000)
			h.syn(t, onGoogle)
			expectSetOrder(t, "youtube on a Google address", h.hello(t, onGoogle, setOrderYouTubeName), setOrderRTSet, true)
		})
	}
}

func TestIPPriorityNameSetAboveTheAddressSetStillWins(t *testing.T) {
	runSetOrderMatrix(t, nil, func() []*config.SetConfig {
		return []*config.SetConfig{newSetOrderGWSet(), newSetOrderGGCSetWith(true)}
	}, setOrderExpect(setOrderGWSet, setOrderGGCSet, "a", "e-short", "f2-data", "f3-data"))
}

func TestIPPriorityNeverHoldsThroughACatchAll(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
			runSetOrderMatrix(t, nil, func() []*config.SetConfig {
				catchAll := newSetOrderStrategySet("catch-all", setOrderCatchAll, 11, 21)
				catchAll.Targets.IPs = []string{"0.0.0.0/0"}
				catchAll.Targets.IPPriority = on
				return []*config.SetConfig{catchAll, newSetOrderGWSet()}
			}, setOrderExpect(setOrderGWSet, setOrderCatchAll, "a", "e-short", "f2-data", "f3-data"))
		})
	}
}

func TestIPPriorityKeepsBothSegmentsOfASplitClientHello(t *testing.T) {
	t.Run("switch off", func(t *testing.T) {
		h := newSetOrderHarness(t, nil, setOrderReporterSets(false))
		c := newSetOrderConn(setOrderGGCAddr, 38874, 92000)
		h.syn(t, c)
		first, second := h.splitHello(t, c, setOrderGGCName)
		expectSetOrder(t, "split, switch off", first, setOrderGGCSet, true)
		expectSetOrder(t, "split, switch off", second, setOrderGWSet, true)
	})
	t.Run("switch on", func(t *testing.T) {
		h := newSetOrderHarness(t, nil, setOrderReporterSets(true))
		c := newSetOrderConn(setOrderGGCAddr, 38874, 92000)
		h.syn(t, c)
		first, second := h.splitHello(t, c, setOrderGGCName)
		expectSetOrder(t, "split, switch on", first, setOrderGGCSet, true)
		expectSetOrder(t, "split, switch on", second, setOrderGGCSet, true)
		expectSetOrder(t, "split, switch on", h.data(t, c), setOrderGGCSet, true)
	})
	t.Run("switch on over an address learned before it", func(t *testing.T) {
		h := newSetOrderHarness(t, nil, setOrderReporterSets(false))
		setOrderFirstConnection(t, h)
		h.reload(t, setOrderReporterSets(true))
		c := newSetOrderConn(setOrderGGCAddr, 38874, 92000)
		expectSetOrder(t, "split over a stale entry", h.syn(t, c), setOrderGGCSet, false)
		first, second := h.splitHello(t, c, setOrderGGCName)
		expectSetOrder(t, "split over a stale entry", first, setOrderGGCSet, true)
		expectSetOrder(t, "split over a stale entry", second, setOrderGGCSet, true)
		expectSetOrder(t, "split over a stale entry", h.data(t, c), setOrderGGCSet, true)
	})
}

func TestIPPriorityBeatsAnAddressLearnedBeforeTheSwitch(t *testing.T) {
	h := newSetOrderHarness(t, nil, setOrderReporterSets(false))
	first := setOrderFirstConnection(t, h)
	h.reload(t, setOrderReporterSets(true))
	if ok, learned, _ := h.w.getMatcher().MatchLearnedIP(net.ParseIP(setOrderGGCAddr)); !ok || learned.Id != "yt-gw-as15169" {
		t.Fatalf("the reload must carry the address learned for %s, got %v %v", setOrderGWSet, ok, learned)
	}

	c := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
	expectSetOrder(t, "new connection", h.syn(t, c), setOrderGGCSet, false)
	expectSetOrder(t, "new connection", h.hello(t, c, setOrderGGCName), setOrderGGCSet, true)
	expectSetOrder(t, "new connection", h.data(t, c), setOrderGGCSet, true)
	expectSetOrder(t, "connection from before the switch", h.data(t, first), setOrderGGCSet, true)
	expectSetOrder(t, "QUIC", h.quicInitial(t, setOrderGGCName), setOrderGGCSet, true)
	expectSetOrder(t, "QUIC", h.quicShortHeader(t), setOrderGGCSet, true)

	google := newSetOrderConn(setOrderGoogleAddr, 38860, 40000)
	h.syn(t, google)
	expectSetOrder(t, "control", h.hello(t, google, setOrderGWName), setOrderGWSet, true)
	expectSetOrder(t, "control", h.data(t, google), setOrderGWSet, true)
}

func TestIPPriorityKeepsTheConnectionOverAnAddressLearnedForASetAbove(t *testing.T) {
	other := newSetOrderNameSet("other", setOrderOtherSet, 12, 23, "other.example")
	h := newSetOrderHarness(t, nil, []*config.SetConfig{other, newSetOrderGGCSetWith(true), newSetOrderGWSet()})

	first := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	h.syn(t, first)
	expectSetOrder(t, "a name set above", h.hello(t, first, setOrderOtherName), setOrderOtherSet, true)

	c := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
	expectSetOrder(t, "before the ClientHello", h.syn(t, c), setOrderOtherSet, false)
	expectSetOrder(t, "held", h.hello(t, c, setOrderGGCName), setOrderGGCSet, true)
	expectSetOrder(t, "held", h.data(t, c), setOrderGGCSet, true)
	expectSetOrder(t, "held", h.retransmittedHello(t, c), setOrderGGCSet, true)
	expectSetOrder(t, "held", h.data(t, c), setOrderGGCSet, true)
	expectSetOrder(t, "a name set above", h.data(t, first), setOrderOtherSet, true)
}

func TestIPPriorityAHelloThatNamesNoSetStaysHeldWhenASetAboveLearnsTheAddress(t *testing.T) {
	build := func(on bool) []*config.SetConfig {
		return []*config.SetConfig{newSetOrderNameSet("other", setOrderOtherSet, 12, 23, "other.example"), newSetOrderGGCSetWith(on), newSetOrderGWSet()}
	}
	h := newSetOrderHarness(t, nil, build(false))
	setOrderFirstConnection(t, h)
	h.reload(t, build(true))

	c := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
	expectSetOrder(t, "held", h.syn(t, c), setOrderGGCSet, false)
	expectSetOrder(t, "held", h.hello(t, c, setOrderUnlisted), setOrderGGCSet, true)
	o := newSetOrderConn(setOrderGGCAddr, 38858, 30000)
	h.syn(t, o)
	expectSetOrder(t, "a set above", h.hello(t, o, setOrderOtherName), setOrderOtherSet, true)
	expectSetOrder(t, "held", h.data(t, c), setOrderGGCSet, true)
	expectSetOrder(t, "held", h.data(t, c), setOrderGGCSet, true)
}

func TestIPPriorityAddressSetWhoseTLSFilterDoesNotFitLeavesTheConnectionToTheNameSet(t *testing.T) {
	build := func(on bool) []*config.SetConfig {
		ggc := newSetOrderGGCSetWith(on)
		ggc.Targets.TLSVersion = "1.2"
		return []*config.SetConfig{ggc, newSetOrderGWSet()}
	}
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("TCP, switch on=%v", on), func(t *testing.T) {
			h := newSetOrderHarness(t, nil, build(on))
			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			expectSetOrder(t, "no ClientHello yet", h.syn(t, c), setOrderGGCSet, false)
			expectSetOrder(t, "TLS 1.3", h.hello(t, c, setOrderGGCName), setOrderGWSet, true)
			for range 3 {
				expectSetOrder(t, "TLS 1.3", h.data(t, c), setOrderGWSet, true)
			}
			expectSetOrder(t, "TLS 1.3", h.retransmittedHello(t, c), setOrderGWSet, true)
			expectSetOrder(t, "TLS 1.3", h.data(t, c), setOrderGWSet, true)

			unlisted := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
			h.syn(t, unlisted)
			expectSetOrder(t, "unlisted name", h.hello(t, unlisted, setOrderUnlisted), setOrderGWSet, true)
			expectSetOrder(t, "unlisted name", h.data(t, unlisted), setOrderGWSet, true)
		})
		t.Run(fmt.Sprintf("QUIC, switch on=%v", on), func(t *testing.T) {
			h := newSetOrderHarness(t, nil, build(on))
			expectSetOrder(t, "QUIC Initial", h.quicInitial(t, setOrderGGCName), setOrderGWSet, true)
			for range 2 {
				expectSetOrder(t, "QUIC short header", h.quicShortHeader(t), setOrderGGCSet, true)
			}
		})
	}
}

func setOrderTLSFilteredSets(on bool, tls string) []*config.SetConfig {
	ggc := newSetOrderGGCSetWith(on)
	ggc.Targets.TLSVersion = tls
	return []*config.SetConfig{ggc, newSetOrderGWSet()}
}

func expectSameVerdicts(t *testing.T, off, on []setOrderObs, contenders ...string) {
	t.Helper()
	if len(off) == 0 || len(on) != len(off) {
		t.Fatalf("observed %d packets with the switch off and %d with it on", len(off), len(on))
	}
	if len(contenders) == 0 {
		contenders = []string{setOrderGWSet}
	}
	contested := false
	for i := range off {
		if slices.Contains(contenders, off[i].picked()) {
			contested = true
		}
		if !slices.Equal(off[i].rows, on[i].rows) || off[i].verdict != on[i].verdict || off[i].strategy != on[i].strategy || off[i].injected != on[i].injected {
			t.Errorf("packet %d differs with the switch on:\n off: %s\n on:  %s", i+1, off[i], on[i])
		}
	}
	if !contested {
		t.Errorf("no packet went to %v with the switch off, so the switch had nothing to decide", contenders)
	}
}

func TestIPPriorityChangesNothingForASetWithATLSFilter(t *testing.T) {
	scripts := []struct {
		name string
		run  func(t *testing.T, h *setOrderHarness) []setOrderObs
	}{
		{"a whole TLS 1.2 ClientHello", func(t *testing.T, h *setOrderHarness) []setOrderObs {
			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			return []setOrderObs{
				h.syn(t, c),
				h.helloWith(t, c, "TLS 1.2 ClientHello", setOrderTLS12Hello(setOrderGGCName)),
				h.data(t, c),
				h.retransmittedHello(t, c),
				h.data(t, c),
			}
		}},
		{"a whole TLS 1.3 ClientHello", func(t *testing.T, h *setOrderHarness) []setOrderObs {
			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			return []setOrderObs{h.syn(t, c), h.hello(t, c, setOrderGGCName), h.data(t, c), h.retransmittedHello(t, c), h.data(t, c)}
		}},
		{"a ClientHello split after its name", func(t *testing.T, h *setOrderHarness) []setOrderObs {
			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			syn := h.syn(t, c)
			first, second := h.segments(t, c, "a TLS 1.3 ClientHello", setOrderNameFirstHello(setOrderGGCName), 1200)
			return []setOrderObs{syn, first, second, h.data(t, c)}
		}},
		{"a ClientHello split before its name", func(t *testing.T, h *setOrderHarness) []setOrderObs {
			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			syn := h.syn(t, c)
			first, second := h.splitHello(t, c, setOrderGGCName)
			return []setOrderObs{syn, first, second, h.data(t, c)}
		}},
		{"later connections after an address was learned for the name set", func(t *testing.T, h *setOrderHarness) []setOrderObs {
			first := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			obs := []setOrderObs{h.syn(t, first), h.hello(t, first, setOrderGGCName)}
			for i, hello := range [][]byte{setOrderTLS12Hello(setOrderGGCName), buildClientHello(setOrderGGCName, 16, 0xAB), setOrderTLS12Hello(setOrderUnlisted)} {
				c := newSetOrderConn(setOrderGGCAddr, uint16(38856+2*i), uint32(20000+10000*i))
				obs = append(obs, h.syn(t, c), h.helloWith(t, c, "ClientHello", hello), h.data(t, c))
			}
			return append(obs, h.data(t, first))
		}},
		{"data after the connection's TLS decision expired", func(t *testing.T, h *setOrderHarness) []setOrderObs {
			var obs []setOrderObs
			for i, hello := range [][]byte{setOrderTLS12Hello(setOrderGGCName), buildClientHello(setOrderGGCName, 16, 0xAB)} {
				c := newSetOrderConn(setOrderGGCAddr, uint16(38854+2*i), uint32(1000+10000*i))
				obs = append(obs, h.syn(t, c), h.helloWith(t, c, "ClientHello", hello), h.data(t, c))
				h.evict(t, c)
				obs = append(obs, h.data(t, c), h.data(t, c))
			}
			return obs
		}},
		{"a QUIC Initial, the later datagrams of its flow and a TCP connection after them", func(t *testing.T, h *setOrderHarness) []setOrderObs {
			obs := []setOrderObs{h.quicInitial(t, setOrderGGCName), h.quicShortHeader(t), h.quicShortHeader(t)}
			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			return append(obs, h.syn(t, c), h.hello(t, c, setOrderGGCName), h.data(t, c))
		}},
	}
	for _, tls := range []string{"1.2", "1.3"} {
		for _, sc := range scripts {
			t.Run(fmt.Sprintf("%s, filter %s", sc.name, tls), func(t *testing.T) {
				t.Log("switch off")
				off := sc.run(t, newSetOrderHarness(t, nil, setOrderTLSFilteredSets(false, tls)))
				t.Log("switch on")
				on := sc.run(t, newSetOrderHarness(t, nil, setOrderTLSFilteredSets(true, tls)))
				expectSameVerdicts(t, off, on)
			})
		}
	}
}

func TestIPPriorityChangesNothingForASetWhosePortFilterLeavesThePortOut(t *testing.T) {
	build := func(on bool) []*config.SetConfig {
		ggc := newSetOrderGGCSetWith(on)
		ggc.TCP.DPortFilter = "8443"
		ggc.UDP.DPortFilter = "8443"
		return []*config.SetConfig{ggc, newSetOrderGWSet()}
	}
	script := func(t *testing.T, h *setOrderHarness) []setOrderObs {
		c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
		return []setOrderObs{h.syn(t, c), h.hello(t, c, setOrderGGCName), h.data(t, c), h.quicInitial(t, setOrderGGCName), h.quicShortHeader(t)}
	}
	off := script(t, newSetOrderHarness(t, nil, build(false)))
	on := script(t, newSetOrderHarness(t, nil, build(true)))
	expectSameVerdicts(t, off, on)
}

func setOrderIPv6OnlyGGCSet(on bool) *config.SetConfig {
	ggc := newSetOrderGGCSetWith(on)
	ggc.Targets.IPVersion = "6"
	return ggc
}

func TestIPPriorityChangesNothingOverTCPForASetWhoseIPVersionFilterDoesNotFit(t *testing.T) {
	script := func(t *testing.T, h *setOrderHarness) []setOrderObs {
		first := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
		obs := []setOrderObs{h.syn(t, first), h.hello(t, first, setOrderGGCName), h.data(t, first)}
		c := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
		return append(obs, h.syn(t, c), h.hello(t, c, setOrderUnlisted), h.data(t, c))
	}
	off := script(t, newSetOrderHarness(t, nil, []*config.SetConfig{setOrderIPv6OnlyGGCSet(false), newSetOrderGWSet()}))
	on := script(t, newSetOrderHarness(t, nil, []*config.SetConfig{setOrderIPv6OnlyGGCSet(true), newSetOrderGWSet()}))
	expectSameVerdicts(t, off, on)
}

func TestIPPriorityLetsAQUICFlowFollowItsInitialWhenTheAddressSetDoesNotFitItsIPVersion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		build     func(on bool) []*config.SetConfig
		escalated string
		off, on   []string
	}{
		{"the name set below", func(on bool) []*config.SetConfig {
			return []*config.SetConfig{setOrderIPv6OnlyGGCSet(on), newSetOrderGWSet()}
		}, "", []string{setOrderGWSet, setOrderGGCSet, setOrderGGCSet}, []string{setOrderGWSet, setOrderGWSet, setOrderGWSet}},
		{"the name set above", func(on bool) []*config.SetConfig {
			return []*config.SetConfig{newSetOrderGWSet(), setOrderIPv6OnlyGGCSet(on)}
		}, "", []string{setOrderGWSet, setOrderGGCSet, setOrderGGCSet}, []string{setOrderGWSet, setOrderGGCSet, setOrderGGCSet}},
		{"the name set below, the site escalated to its backup", func(on bool) []*config.SetConfig {
			gw := newSetOrderGWSet()
			gw.Escalate.To = "gw-backup"
			return []*config.SetConfig{setOrderIPv6OnlyGGCSet(on), gw, newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9)}
		}, "gw-backup", []string{setOrderGWBackup, setOrderGGCSet, setOrderGGCSet}, []string{setOrderGWBackup, setOrderGWBackup, setOrderGWBackup}},
		{"the name set below, its UDP port filter leaving the port out", func(on bool) []*config.SetConfig {
			gw := newSetOrderGWSet()
			gw.UDP.DPortFilter = "8443"
			return []*config.SetConfig{setOrderIPv6OnlyGGCSet(on), gw}
		}, "", []string{setOrderGGCSet, setOrderGGCSet, setOrderGGCSet}, []string{setOrderGGCSet, setOrderGGCSet, setOrderGGCSet}},
	} {
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, switch on=%v", tc.name, on), func(t *testing.T) {
				h := newSetOrderHarness(t, nil, tc.build(on))
				if tc.escalated != "" {
					h.w.destState.SetEscalation(setOrderGGCName, tc.escalated, escalateReasonStall, time.Hour)
				}
				want := tc.off
				if on {
					want = tc.on
				}
				expectSetOrder(t, tc.name, h.quicInitial(t, setOrderGGCName), want[0], true)
				expectSetOrder(t, tc.name, h.quicShortHeader(t), want[1], true)
				expectSetOrder(t, tc.name, h.quicShortHeader(t), want[2], true)
			})
		}
	}
}

func TestIPPriorityEscalation(t *testing.T) {
	t.Run("an escalation from the name set does not move the address set's connection", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
				gw := newSetOrderGWSet()
				gw.Escalate.To = "gw-backup"
				h := newSetOrderHarness(t, nil, []*config.SetConfig{newSetOrderGGCSetWith(on), gw, newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9)})
				h.w.destState.SetEscalation(setOrderGGCName, "gw-backup", escalateReasonStall, time.Hour)

				want := setOrderGWBackup
				if on {
					want = setOrderGGCSet
				}
				c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
				h.syn(t, c)
				expectSetOrder(t, "escalated host", h.hello(t, c, setOrderGGCName), want, true)
				expectSetOrder(t, "escalated host", h.data(t, c), want, true)
				expectSetOrder(t, "escalated host", h.quicInitial(t, setOrderGGCName), want, true)

				elsewhere := newSetOrderConn(setOrderGoogleAddr, 38860, 40000)
				h.syn(t, elsewhere)
				expectSetOrder(t, "the name set's own connection", h.hello(t, elsewhere, setOrderGGCName), setOrderGWBackup, true)
			})
		}
	})
	t.Run("an escalation along the address set's own chain applies", func(t *testing.T) {
		for _, target := range []string{setOrderGGCBackup, setOrderGGCBackup2} {
			t.Run(target, func(t *testing.T) {
				ggc := newSetOrderGGCSetWith(true)
				ggc.Escalate.To = "ggc-backup"
				gw := newSetOrderGWSet()
				gw.Escalate.To = "gw-backup"
				backup := newSetOrderBackupSet("ggc-backup", setOrderGGCBackup, 6, 11)
				backup.Escalate.To = "ggc-backup-2"
				h := newSetOrderHarness(t, nil, []*config.SetConfig{
					ggc, gw, newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9), backup, newSetOrderBackupSet("ggc-backup-2", setOrderGGCBackup2, 7, 13),
				})
				id := "ggc-backup"
				if target == setOrderGGCBackup2 {
					id = "ggc-backup-2"
				}
				h.w.destState.SetEscalation(setOrderGGCName, id, escalateReasonStall, time.Hour)

				c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
				h.syn(t, c)
				expectSetOrder(t, "own chain", h.hello(t, c, setOrderGGCName), target, true)
				expectSetOrder(t, "own chain", h.data(t, c), target, true)
			})
		}
	})
	t.Run("a held QUIC flow stays on one set", func(t *testing.T) {
		for _, tc := range []struct {
			name, escalated, want string
		}{
			{"an escalation along the address set's own chain", "ggc-backup", setOrderGGCBackup},
			{"an escalation from the name set", "gw-backup", setOrderGGCSet},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ggc := newSetOrderGGCSetWith(true)
				ggc.Escalate.To = "ggc-backup"
				gw := newSetOrderGWSet()
				gw.Escalate.To = "gw-backup"
				h := newSetOrderHarness(t, nil, []*config.SetConfig{
					ggc, gw, newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9), newSetOrderBackupSet("ggc-backup", setOrderGGCBackup, 6, 11),
				})
				h.w.destState.SetEscalation(setOrderGGCName, tc.escalated, escalateReasonStall, time.Hour)

				expectSetOrder(t, "QUIC Initial", h.quicInitial(t, setOrderGGCName), tc.want, true)
				for range 2 {
					expectSetOrder(t, "QUIC short header", h.quicShortHeader(t), tc.want, true)
				}
			})
		}
	})
	t.Run("an escalated name learned for the address set does not move a held connection", func(t *testing.T) {
		ggc := newSetOrderGGCSetWith(true)
		ggc.Targets.SNIDomains = []string{"ggc.example"}
		h := newSetOrderHarness(t, nil, []*config.SetConfig{ggc, newSetOrderGWSet(), newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9)})

		held := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
		h.syn(t, held)
		expectSetOrder(t, "held", h.hello(t, held, setOrderGGCName), setOrderGGCSet, true)
		named := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
		h.syn(t, named)
		expectSetOrder(t, "named", h.hello(t, named, "www.ggc.example"), setOrderGGCSet, true)
		h.w.destState.SetEscalation("www.ggc.example", "gw-backup", escalateReasonStall, time.Hour)

		expectSetOrder(t, "held", h.data(t, held), setOrderGGCSet, true)
		expectSetOrder(t, "named", h.data(t, named), setOrderGWBackup, true)
	})
	t.Run("an escalated name learned for the address set does not move a held connection, even to a set above it", func(t *testing.T) {
		ggc := newSetOrderGGCSetWith(true)
		ggc.Targets.SNIDomains = []string{"ggc.example"}
		h := newSetOrderHarness(t, nil, []*config.SetConfig{newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25), ggc, newSetOrderGWSet()})

		held := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
		h.syn(t, held)
		expectSetOrder(t, "held", h.hello(t, held, setOrderGGCName), setOrderGGCSet, true)
		named := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
		h.syn(t, named)
		expectSetOrder(t, "named", h.hello(t, named, "www.ggc.example"), setOrderGGCSet, true)
		h.w.destState.SetEscalation("www.ggc.example", "escalated", escalateReasonStall, time.Hour)

		expectSetOrder(t, "held", h.data(t, held), setOrderGGCSet, true)
		expectSetOrder(t, "named", h.data(t, named), setOrderEscalated, true)
	})
	t.Run("a stalled ClientHello escalates from the address set", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
				ggc := newSetOrderGGCSetWith(on)
				ggc.Escalate.To = "ggc-backup"
				gw := newSetOrderGWSet()
				gw.Escalate.To = "gw-backup"
				h := newSetOrderHarness(t, nil, []*config.SetConfig{
					ggc, gw, newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9), newSetOrderBackupSet("ggc-backup", setOrderGGCBackup, 6, 11),
				})
				c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
				h.syn(t, c)
				h.hello(t, c, setOrderGGCName)
				for range 3 {
					h.retransmittedHello(t, c)
				}
				want := "gw-backup"
				if on {
					want = "ggc-backup"
				}
				if got, _, ok := h.w.destState.GetEscalation(setOrderGGCName); !ok || got != want {
					t.Fatalf("escalation for %s: got %q (recorded %v), want %q", setOrderGGCName, got, ok, want)
				}
			})
		}
	})
	t.Run("nameless datagrams stay with the address set below an escalated name set", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			build func(on bool) []*config.SetConfig
		}{
			{"the target above both", func(on bool) []*config.SetConfig {
				gw := newSetOrderGWSet()
				gw.Escalate.To = "escalated"
				return []*config.SetConfig{newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25), gw, newSetOrderGGCSetWith(on)}
			}},
			{"the target on the address set's own chain", func(on bool) []*config.SetConfig {
				gw := newSetOrderGWSet()
				gw.Escalate.To = "escalated"
				ggc := newSetOrderGGCSetWith(on)
				ggc.Escalate.To = "escalated"
				return []*config.SetConfig{gw, ggc, newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25)}
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				script := func(t *testing.T, h *setOrderHarness) []setOrderObs {
					h.w.destState.SetEscalation(setOrderGGCName, "escalated", escalateReasonStall, time.Hour)
					return []setOrderObs{h.quicInitial(t, setOrderGGCName), h.quicShortHeader(t), h.quicShortHeader(t)}
				}
				off := script(t, newSetOrderHarness(t, nil, tc.build(false)))
				on := script(t, newSetOrderHarness(t, nil, tc.build(true)))
				expectSameVerdicts(t, off, on, setOrderEscalated)
				expectSetOrder(t, tc.name, on[1], setOrderGGCSet, true)
			})
		}
	})
}

func TestIPPriorityAReloadDoesNotHandAHeldConnectionToTheNameSet(t *testing.T) {
	build := func(gwDomains ...string) []*config.SetConfig {
		gw := newSetOrderNameSet("yt-gw-as15169", setOrderGWSet, 3, 5, gwDomains...)
		gw.Escalate.To = "gw-backup"
		return []*config.SetConfig{newSetOrderGGCSetWith(true), gw, newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9)}
	}
	h := newSetOrderHarness(t, nil, build("googlevideo.com"))
	c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	h.syn(t, c)
	expectSetOrder(t, "before the reload", h.hello(t, c, setOrderGGCName), setOrderGGCSet, true)

	expectStillHeld := func(when string) {
		t.Helper()
		if d, ok := h.w.tlsCache.Decision(c.key()); !ok || d.set == nil || d.set.Id != "yt-ggc-ru" || d.heldBy != "yt-ggc-ru" || d.decidedBy != h.w.getMatcher() {
			t.Fatalf("%s: the re-matched decision must stay with %s and stay held, got %+v (found %v)", when, setOrderGGCSet, d, ok)
		}
	}
	h.reload(t, build("googlevideo.com"))
	h.w.destState.SetEscalation(setOrderGGCName, "gw-backup", escalateReasonStall, time.Hour)
	expectSetOrder(t, "after the reload", h.data(t, c), setOrderGGCSet, true)
	expectStillHeld("after the reload")
	expectSetOrder(t, "after the reload", h.data(t, c), setOrderGGCSet, true)

	h.reload(t, build("other.example"))
	expectSetOrder(t, "after a reload that takes the name off the name set", h.data(t, c), setOrderGGCSet, true)
	expectStillHeld("after a reload that takes the name off the name set")
	expectSetOrder(t, "after a reload that takes the name off the name set", h.data(t, c), setOrderGGCSet, true)
}

func TestIPPriorityAndBlockSets(t *testing.T) {
	t.Run("a block set below the address set", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
				block := newSetOrderBlockSet("block-gw", setOrderBlockGW, 9, 17)
				block.Targets.SNIDomains = []string{"googlevideo.com"}
				h := newSetOrderHarness(t, nil, []*config.SetConfig{newSetOrderGGCSetWith(on), block})

				c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
				h.syn(t, c)
				hello := h.hello(t, c, setOrderGGCName)
				initial := h.quicInitial(t, setOrderGGCName)
				if on {
					expectNotBlocked(t, hello, setOrderGGCSet)
					expectNotBlocked(t, initial, setOrderGGCSet)
				} else {
					expectBlocked(t, hello, setOrderBlockGW)
					expectBlocked(t, initial, setOrderBlockGW)
				}

				elsewhere := newSetOrderConn(setOrderGoogleAddr, 38860, 40000)
				h.syn(t, elsewhere)
				expectBlocked(t, h.hello(t, elsewhere, setOrderGWName), setOrderBlockGW)
			})
		}
	})
	t.Run("an address set in block mode above the name set", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
				block := newSetOrderBlockSet("block-ggc", setOrderBlockGGC, 10, 19)
				block.Targets.IPs = append([]string(nil), setOrderGGCRanges...)
				block.Targets.IPPriority = on
				h := newSetOrderHarness(t, nil, []*config.SetConfig{block, newSetOrderGWSet()})

				c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
				expectBlocked(t, h.syn(t, c), setOrderBlockGGC)
				hello := h.hello(t, c, setOrderGGCName)
				initial := h.quicInitial(t, setOrderGGCName)
				if on {
					expectBlocked(t, hello, setOrderBlockGGC)
					expectBlocked(t, initial, setOrderBlockGGC)
					expectBlocked(t, h.data(t, c), setOrderBlockGGC)
				} else {
					expectNotBlocked(t, hello, setOrderGWSet)
					expectNotBlocked(t, initial, setOrderGWSet)
					expectNotBlocked(t, h.data(t, c), setOrderGWSet)
				}
			})
		}
	})
	t.Run("an address set in block mode above the name set, its address learned for its own site", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
				block := newSetOrderBlockSet("block-ggc", setOrderBlockGGC, 10, 19)
				block.Targets.IPs = append([]string(nil), setOrderGGCRanges...)
				block.Targets.SNIDomains = []string{"ggc.example"}
				block.Targets.IPPriority = on
				h := newSetOrderHarness(t, nil, []*config.SetConfig{block, newSetOrderGWSet()})

				named := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
				h.syn(t, named)
				expectBlocked(t, h.hello(t, named, "www.ggc.example"), setOrderBlockGGC)
				if ok, learned, _ := h.w.getMatcher().MatchLearnedIP(net.ParseIP(setOrderGGCAddr)); !ok || learned.Id != block.Id {
					t.Fatalf("the fixture needs %s learned for %s, got %v %v", setOrderGGCAddr, setOrderBlockGGC, ok, learned)
				}

				c := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
				h.syn(t, c)
				hello := h.hello(t, c, setOrderGGCName)
				if on {
					expectBlocked(t, hello, setOrderBlockGGC)
				} else {
					expectNotBlocked(t, hello, setOrderGWSet)
				}
			})
		}
	})
}

func TestIPPriorityLetsADeviceScopedNameSetBelowTakeItsOwnDevices(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
			device := newSetOrderNameSet("gw-device", setOrderDeviceSet, 8, 15, "googlevideo.com")
			device.Targets.SourceDevices = []string{setOrderClientMAC}
			h := newSetOrderHarness(t, setOrderBothMACs, []*config.SetConfig{newSetOrderGGCSetWith(on), device, newSetOrderGWSet()})

			mine := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			h.syn(t, mine)
			expectSetOrder(t, "its device", h.hello(t, mine, setOrderGGCName), setOrderDeviceSet, true)
			expectSetOrder(t, "its device", h.data(t, mine), setOrderDeviceSet, true)

			want, wantMine := setOrderGWSet, setOrderGWSet
			if on {
				want, wantMine = setOrderGGCSet, setOrderDeviceSet
			}
			theirs := newSetOrderConn(setOrderGGCAddr, 38856, 20000).from(setOrderOtherClient)
			expectSetOrder(t, "another device", h.syn(t, theirs), want, false)
			expectSetOrder(t, "another device", h.hello(t, theirs, setOrderGGCName), want, true)
			expectSetOrder(t, "another device", h.data(t, theirs), want, true)
			expectSetOrder(t, "its device after the other device", h.data(t, mine), wantMine, true)
		})
	}
}

func TestIPPriorityPutsADeviceScopedAddressSetBeforeAGeneralNameSetAbove(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
			ggc := newSetOrderGGCSetWith(on)
			ggc.Targets.SourceDevices = []string{setOrderClientMAC}
			h := newSetOrderHarness(t, setOrderBothMACs, []*config.SetConfig{newSetOrderGWSet(), ggc})

			want := setOrderGWSet
			if on {
				want = setOrderGGCSet
			}
			mine := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			expectSetOrder(t, "its device", h.syn(t, mine), setOrderGGCSet, false)
			expectSetOrder(t, "its device", h.hello(t, mine, setOrderGGCName), want, true)
			expectSetOrder(t, "its device", h.data(t, mine), want, true)

			theirs := newSetOrderConn(setOrderGGCAddr, 38856, 20000).from(setOrderOtherClient)
			h.syn(t, theirs)
			expectSetOrder(t, "another device", h.hello(t, theirs, setOrderGGCName), setOrderGWSet, true)
			expectSetOrder(t, "another device", h.data(t, theirs), setOrderGWSet, true)
		})
	}
}

func TestIPPriorityLearnsNothingForTheNameSet(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
			dscp := stubDSCPHooks(t)
			routes := stubRouteHooks(t)
			gw := newSetOrderGWSet()
			gw.DSCP = config.SetDSCPConfig{Enabled: true, Value: 20}
			gw.Routing.Enabled = true
			gw.Routing.Mode = config.RoutingModeProxy
			h := newSetOrderHarness(t, nil, []*config.SetConfig{newSetOrderGGCSetWith(on), gw})

			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			h.syn(t, c)
			hello := h.hello(t, c, setOrderGGCName)
			initial := h.quicInitial(t, setOrderGGCName)

			learned, learnedSet, _ := h.w.getMatcher().MatchLearnedIP(net.ParseIP(setOrderGGCAddr))
			_, hinted := h.w.hostHints.LookupTLS(setOrderClient, setOrderGGCAddr)
			dscpCalls := 0
			for _, call := range dscp.list() {
				if call.set == gw.Id {
					dscpCalls++
				}
			}
			routeCalls := 0
			for call, n := range routes.snapshot() {
				if strings.HasSuffix(call, " "+gw.Id) {
					routeCalls += n
				}
			}

			if on {
				expectSetOrder(t, "held", hello, setOrderGGCSet, true)
				expectSetOrder(t, "held", initial, setOrderGGCSet, true)
				if learned {
					t.Errorf("an address held by %s was learned for %s", setOrderGGCSet, learnedSet.Name)
				}
				if hinted {
					t.Errorf("a QUIC Initial held by %s left a host hint", setOrderGGCSet)
				}
				if dscpCalls != 0 || routeCalls != 0 {
					t.Errorf("a held address reached the kernel state of %s: %d DSCP and %d route writes", setOrderGWSet, dscpCalls, routeCalls)
				}
				return
			}
			if !learned || learnedSet.Id != gw.Id || !hinted || dscpCalls == 0 || routeCalls == 0 {
				t.Errorf("with the switch off %s learns the address: learned=%v hint=%v DSCP=%d routes=%d", setOrderGWSet, learned, hinted, dscpCalls, routeCalls)
			}
		})
	}
}

type setOrderKernelLog struct {
	mu      sync.Mutex
	kept    map[string][]string
	dropped map[string][]string
}

func stubSetOrderKernel(t *testing.T, h *setOrderHarness) *setOrderKernelLog {
	t.Helper()
	return stubSetOrderKernelWith(t, func(cfg *config.Config, set *config.SetConfig, ip net.IP) bool {
		return h.w.getMatcher().KeepsLearned(cfg, set, ip)
	})
}

func stubSetOrderKernelWith(t *testing.T, keeps func(cfg *config.Config, set *config.SetConfig, ip net.IP) bool) *setOrderKernelLog {
	t.Helper()
	prevSync, prevAsync, prevAwait := RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSAwaitFunc
	prevIP, prevHost, prevIPAsync, prevHostAsync := RoutingLearnIPFunc, RoutingLearnHostFunc, RoutingLearnIPAsyncFunc, RoutingLearnHostAsyncFunc
	prevDSCP, prevDSCPAsync := DSCPLearnFunc, DSCPLearnAsyncFunc
	t.Cleanup(func() {
		RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc, RoutingHandleDNSAwaitFunc = prevSync, prevAsync, prevAwait
		RoutingLearnIPFunc, RoutingLearnHostFunc, RoutingLearnIPAsyncFunc, RoutingLearnHostAsyncFunc = prevIP, prevHost, prevIPAsync, prevHostAsync
		DSCPLearnFunc, DSCPLearnAsyncFunc = prevDSCP, prevDSCPAsync
	})
	k := &setOrderKernelLog{kept: map[string][]string{}, dropped: map[string][]string{}}
	split := func(kind string, cfg *config.Config, set *config.SetConfig, ips ...net.IP) {
		key := kind + " " + set.Id
		k.mu.Lock()
		defer k.mu.Unlock()
		for _, ip := range ips {
			if keeps(cfg, set, ip) {
				k.kept[key] = append(k.kept[key], ip.String())
			} else {
				k.dropped[key] = append(k.dropped[key], ip.String())
			}
		}
	}
	RoutingHandleDNSFunc = func(cfg *config.Config, set *config.SetConfig, ips []net.IP) { split("route", cfg, set, ips...) }
	RoutingHandleDNSAsyncFunc = RoutingHandleDNSFunc
	RoutingHandleDNSAwaitFunc = func(cfg *config.Config, set *config.SetConfig, ips []net.IP) []<-chan struct{} {
		split("route", cfg, set, ips...)
		return nil
	}
	RoutingLearnIPFunc, RoutingLearnHostFunc, RoutingLearnHostAsyncFunc = nil, nil, nil
	RoutingLearnIPAsyncFunc = func(cfg *config.Config, set *config.SetConfig, ip net.IP) { split("route", cfg, set, ip) }
	DSCPLearnFunc = func(cfg *config.Config, set *config.SetConfig, ips []net.IP, _ bool) []<-chan struct{} {
		split("dscp", cfg, set, ips...)
		return nil
	}
	DSCPLearnAsyncFunc = func(cfg *config.Config, set *config.SetConfig, ips []net.IP, _ bool) { split("dscp", cfg, set, ips...) }
	return k
}

func (k *setOrderKernelLog) of(kind, id string) (kept, dropped []string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	key := kind + " " + id
	return slices.Clone(k.kept[key]), slices.Clone(k.dropped[key])
}

func TestIPPriorityEscalationTargetOfTheHoldingSetLearnsTheHeldAddress(t *testing.T) {
	for _, tc := range []struct {
		on   bool
		name string
	}{{false, setOrderUnlisted}, {true, setOrderUnlisted}, {true, setOrderGGCName}} {
		t.Run(fmt.Sprintf("switch on=%v, %s", tc.on, tc.name), func(t *testing.T) {
			ggc := newSetOrderGGCSetWith(tc.on)
			ggc.Escalate.To = "ggc-backup"
			backup := newSetOrderBackupSet("ggc-backup", setOrderGGCBackup, 6, 11)
			backup.Routing.Enabled = true
			backup.Routing.Mode = config.RoutingModeInterface
			backup.Routing.EgressInterface = "wg0"
			backup.DSCP = config.SetDSCPConfig{Enabled: true, Value: 20}
			gw := newSetOrderGWSet()
			gw.DSCP = config.SetDSCPConfig{Enabled: true, Value: 30}
			h := newSetOrderHarness(t, nil, []*config.SetConfig{ggc, gw, backup})
			if h.w.getConfig().Queue.IsDiscovery {
				t.Fatal("the harness runs as Discovery, which learns nothing")
			}
			kernel := stubSetOrderKernel(t, h)

			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			h.syn(t, c)
			expectSetOrder(t, "before the escalation", h.hello(t, c, tc.name), setOrderGGCSet, true)
			for range 3 {
				h.retransmittedHello(t, c)
			}
			if got, _, ok := h.w.destState.GetEscalation(tc.name); !ok || got != "ggc-backup" {
				t.Fatalf("escalation for %s: got %q (recorded %v), want ggc-backup", tc.name, got, ok)
			}
			next := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
			h.syn(t, next)
			h.hello(t, next, tc.name)

			for _, kind := range []string{"route", "dscp"} {
				kept, dropped := kernel.of(kind, "ggc-backup")
				if len(dropped) != 0 || !slices.Contains(kept, setOrderGGCAddr) {
					t.Errorf("the escalation target %s must learn %s for its %s: kept %v, dropped %v", setOrderGGCBackup, setOrderGGCAddr, kind, kept, dropped)
				}
				if kept, dropped := kernel.of(kind, gw.Id); tc.on && len(kept)+len(dropped) != 0 {
					t.Errorf("%s was offered the held address for its %s: kept %v, dropped %v", setOrderGWSet, kind, kept, dropped)
				}
			}
		})
	}
}

func TestTheKernelFilterFollowsTheEscalationChainAsFarAsTheEscalation(t *testing.T) {
	sets := []*config.SetConfig{newSetOrderGGCSetWith(true), newSetOrderGWSet()}
	sets[0].Escalate.To = "hop-1"
	for i := 1; i <= MaxEscalationHops+2; i++ {
		hop := newSetOrderBackupSet(fmt.Sprintf("hop-%d", i), fmt.Sprintf("Hop %d", i), 40+i, uint8(60+i))
		hop.Escalate.To = fmt.Sprintf("hop-%d", i+1)
		sets = append(sets, hop)
	}
	h := newSetOrderHarness(t, nil, sets)
	cfg := h.w.getConfig()
	matcher := h.w.getMatcher()
	addr := net.ParseIP(setOrderGGCAddr)
	reached := 0
	for _, set := range cfg.Sets[2:] {
		reaches := escalationReaches(cfg, cfg.Sets[0], set.Id)
		if reaches {
			reached++
		}
		if keeps := matcher.KeepsLearned(cfg, set, addr); keeps != reaches {
			t.Errorf("%s: the kernel filter keeps the held address %v, the escalation reaches it %v", set.Name, keeps, reaches)
		}
	}
	if reached != MaxEscalationHops {
		t.Fatalf("the escalation reached %d sets of the chain, want %d", reached, MaxEscalationHops)
	}
	if matcher.KeepsLearned(cfg, cfg.Sets[1], addr) {
		t.Fatalf("%s is off the chain and must not learn the held address", setOrderGWSet)
	}
}

func TestTheKernelFilterAndAHeldConnectionBothStopTheEscalationChainAtADisabledSet(t *testing.T) {
	ggc := newSetOrderGGCSetWith(true)
	ggc.Escalate.To = "hop-1"
	hop1 := newSetOrderBackupSet("hop-1", "Hop 1", 41, 61)
	hop1.Escalate.To = "hop-2"
	hop1.Enabled = false
	hop2 := newSetOrderBackupSet("hop-2", "Hop 2", 42, 62)
	h := newSetOrderHarness(t, nil, []*config.SetConfig{ggc, newSetOrderGWSet(), hop1, hop2})
	cfg := h.w.getConfig()
	if reaches, keeps := escalationReaches(cfg, cfg.Sets[0], hop2.Id), h.w.getMatcher().KeepsLearned(cfg, cfg.GetSetById(hop2.Id), net.ParseIP(setOrderGGCAddr)); reaches || keeps {
		t.Fatalf("past the disabled %s: the escalation reaches %s %v, the kernel filter learns for it %v", hop1.Name, hop2.Name, reaches, keeps)
	}

	h.w.destState.SetEscalation(setOrderGGCName, hop2.Id, escalateReasonStall, time.Hour)
	c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	h.syn(t, c)
	expectSetOrder(t, "held", h.hello(t, c, setOrderGGCName), setOrderGGCSet, true)
	expectSetOrder(t, "held", h.data(t, c), setOrderGGCSet, true)
}

func TestAHeldConnectionFollowsAnEscalationExactlyWhereTheKernelFilterLearnsForTheTarget(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		above, onOwn, devices bool
		want                  string
	}{
		{"a target above the address set, on the name set's chain", true, false, false, setOrderEscalated},
		{"a target below the address set, on the name set's chain", false, false, false, setOrderGGCSet},
		{"a target above the address set, on its own chain", true, true, false, setOrderEscalated},
		{"a target below the address set, on its own chain", false, true, false, setOrderEscalated},
		{"a target below the address set limited to the device, on the name set's chain", false, false, true, setOrderEscalated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ggc := newSetOrderGGCSetWith(true)
			gw := newSetOrderGWSet()
			escalated := newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25)
			if tc.onOwn {
				ggc.Escalate.To = escalated.Id
			} else {
				gw.Escalate.To = escalated.Id
			}
			if tc.devices {
				escalated.Targets.SourceDevices = []string{setOrderClientMAC}
			}
			sets := []*config.SetConfig{ggc, gw, escalated}
			if tc.above {
				sets = []*config.SetConfig{escalated, ggc, gw}
			}
			h := newSetOrderHarness(t, setOrderBothMACs, sets)
			h.w.destState.SetEscalation(setOrderGGCName, escalated.Id, escalateReasonStall, time.Hour)
			cfg := h.w.getConfig()
			if keeps := h.w.getMatcher().KeepsLearned(cfg, cfg.GetSetById(escalated.Id), net.ParseIP(setOrderGGCAddr)); keeps != (tc.want == setOrderEscalated) {
				t.Fatalf("the kernel filter keeps %s for %s: %v, want %v", setOrderGGCAddr, setOrderEscalated, keeps, !keeps)
			}

			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			expectSetOrder(t, "no name yet", h.syn(t, c), setOrderGGCSet, false)
			expectSetOrder(t, "held", h.hello(t, c, setOrderGGCName), tc.want, true)
			expectSetOrder(t, "held", h.data(t, c), tc.want, true)
			expectSetOrder(t, "held", h.quicInitial(t, setOrderGGCName), tc.want, true)
			expectSetOrder(t, "held", h.quicShortHeader(t), tc.want, true)
			if tc.devices {
				theirs := newSetOrderConn(setOrderGGCAddr, 38856, 20000).from(setOrderOtherClient)
				h.syn(t, theirs)
				expectSetOrder(t, "another device", h.hello(t, theirs, setOrderGGCName), setOrderGGCSet, true)
				expectSetOrder(t, "another device", h.data(t, theirs), setOrderGGCSet, true)
			}
		})
	}
}

func TestIPPriorityAHeldAddressFollowsASiteEscalatedToARoutingSetAbove(t *testing.T) {
	route := newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25)
	route.Routing.Enabled = true
	route.Routing.Mode = config.RoutingModeInterface
	route.Routing.EgressInterface = "wg0"
	route.DSCP = config.SetDSCPConfig{Enabled: true, Value: 20}
	gw := newSetOrderGWSet()
	gw.Escalate.To = route.Id
	h := newSetOrderHarness(t, nil, []*config.SetConfig{route, newSetOrderGGCSetWith(true), gw})
	h.w.goodIPs = iphealth.NewKnownGood()
	h.w.goodIPs.Remember(setOrderGGCName, net.ParseIP(setOrderGGCAddr))
	kernel := stubSetOrderKernel(t, h)

	own := newSetOrderConn(setOrderGoogleAddr, 38860, 40000)
	h.syn(t, own)
	expectSetOrder(t, "the name set's own connection", h.hello(t, own, setOrderGGCName), setOrderGWSet, true)
	for range 3 {
		h.retransmittedHello(t, own)
	}
	if got, _, ok := h.w.destState.GetEscalation(setOrderGGCName); !ok || got != route.Id {
		t.Fatalf("escalation for %s: got %q (recorded %v), want %q", setOrderGGCName, got, ok, route.Id)
	}

	c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	expectSetOrder(t, "no name yet", h.syn(t, c), setOrderGGCSet, false)
	expectSetOrder(t, "the escalated site", h.hello(t, c, setOrderGGCName), setOrderEscalated, false)
	expectSetOrder(t, "the escalated site", h.data(t, c), setOrderEscalated, false)
	for _, kind := range []string{"route", "dscp"} {
		if kept, dropped := kernel.of(kind, route.Id); len(dropped) != 0 || !slices.Contains(kept, setOrderGGCAddr) {
			t.Errorf("%s must learn %s for its %s: kept %v, dropped %v", setOrderEscalated, setOrderGGCAddr, kind, kept, dropped)
		}
	}
}

func TestIPPriorityAHeldConnectionRefreshesTheHoldingSetWhenTheSiteEscalatedToIt(t *testing.T) {
	const other = "173.194.0.20"
	run := func(t *testing.T, on bool) (route, dscp []string) {
		ggc := newSetOrderGGCSetWith(on)
		ggc.Routing.Enabled = true
		ggc.Routing.Mode = config.RoutingModeInterface
		ggc.Routing.EgressInterface = "wg0"
		gw := newSetOrderGWSet()
		gw.Escalate.To = ggc.Id
		h := newSetOrderHarness(t, nil, []*config.SetConfig{ggc, gw})
		h.w.goodIPs = iphealth.NewKnownGood()
		h.w.goodIPs.Remember(setOrderGGCName, net.ParseIP(other))
		kernel := stubSetOrderKernel(t, h)

		own := newSetOrderConn(setOrderGoogleAddr, 38860, 40000)
		h.syn(t, own)
		h.hello(t, own, setOrderGGCName)
		for range 3 {
			if _, _, ok := h.w.destState.GetEscalation(setOrderGGCName); ok {
				break
			}
			h.retransmittedHello(t, own)
		}
		if got, _, ok := h.w.destState.GetEscalation(setOrderGGCName); !ok || got != ggc.Id {
			t.Fatalf("escalation for %s: got %q (recorded %v), want %q", setOrderGGCName, got, ok, ggc.Id)
		}
		kernel.mu.Lock()
		kernel.kept, kernel.dropped = map[string][]string{}, map[string][]string{}
		kernel.mu.Unlock()

		c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
		h.syn(t, c)
		expectSetOrder(t, "the escalated site on the address set's address", h.hello(t, c, setOrderGGCName), setOrderGGCSet, false)
		route, _ = kernel.of("route", ggc.Id)
		dscp, _ = kernel.of("dscp", ggc.Id)
		slices.Sort(route)
		slices.Sort(dscp)
		return route, dscp
	}
	offRoute, offDSCP := run(t, false)
	onRoute, onDSCP := run(t, true)
	if !slices.Contains(offRoute, other) || !slices.Contains(offDSCP, other) {
		t.Fatalf("with the switch off the hello must refresh the escalated route: route %v, DSCP %v", offRoute, offDSCP)
	}
	if !slices.Equal(offRoute, onRoute) || !slices.Equal(offDSCP, onDSCP) {
		t.Errorf("the refresh differs with the switch on: route off %v on %v, DSCP off %v on %v", offRoute, onRoute, offDSCP, onDSCP)
	}
}

func (h *setOrderHarness) tracked(c *setOrderConn) string {
	if set := h.w.connTracker.GetSetForIncoming(c.client, c.sport, c.dst, 443); set != nil {
		return set.Name
	}
	return setOrderNoSet
}

func TestIPPriorityAHeldConnectionKeepsItsSetWhenItsAddressWasLearnedForTheHoldingSetItself(t *testing.T) {
	ggc := newSetOrderGGCSetWith(true)
	ggc.Targets.SNIDomains = []string{"ggc.example"}
	ggc.Fragmentation.Strategy = config.ConfigNone
	h := newSetOrderHarness(t, nil, []*config.SetConfig{ggc, newSetOrderGWSet()})

	named := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
	h.syn(t, named)
	expectSetOrder(t, "the address set's own site", h.hello(t, named, "www.ggc.example"), setOrderGGCSet, false)
	if ok, learned, host := h.w.getMatcher().MatchLearnedIP(net.ParseIP(setOrderGGCAddr)); !ok || learned.Id != "yt-ggc-ru" || host != "www.ggc.example" {
		t.Fatalf("the fixture needs %s learned for %s under www.ggc.example, got %v %v %q", setOrderGGCAddr, setOrderGGCSet, ok, learned, host)
	}

	held := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	h.syn(t, held)
	expectSetOrder(t, "held", h.hello(t, held, setOrderGGCName), setOrderGGCSet, false)
	for i := range 2 {
		expectSetOrder(t, fmt.Sprintf("held data %d", i+1), h.data(t, held), setOrderGGCSet, false)
	}
	if got := h.tracked(held); got != setOrderGGCSet {
		t.Errorf("the held connection is tracked as %q, want %q", got, setOrderGGCSet)
	}
}

func (h *setOrderHarness) forgedRST(c *setOrderConn) {
	rst := makeV4TCPPacketFlags(nil, 777, 0x14)
	copy(rst[12:16], net.ParseIP(c.dst).To4())
	copy(rst[16:20], net.ParseIP(c.client).To4())
	binary.BigEndian.PutUint16(rst[20:22], 443)
	binary.BigEndian.PutUint16(rst[22:24], c.sport)
	h.w.ProcessPacket(rst)
}

func TestIPPriorityAHeldConnectionIsTrackedUnderTheAddressSetEvenWhenItsSYNWentElsewhere(t *testing.T) {
	other := newSetOrderNameSet("other", setOrderOtherSet, 12, 23, "other.example")
	other.Escalate.To = "o-backup"
	oBackup := newSetOrderBackupSet("o-backup", "O-backup", 15, 29)
	oBackup.Escalate.To = "o-backup-2"
	oBackup.Escalate.RstThreshold = 1
	h := newSetOrderHarness(t, nil, []*config.SetConfig{
		other, oBackup, newSetOrderBackupSet("o-backup-2", "O-backup-2", 18, 35), newSetOrderGGCSetWith(true), newSetOrderGWSet(),
	})
	first := newSetOrderConn(setOrderGGCAddr, 38850, 500)
	h.syn(t, first)
	h.hello(t, first, setOrderOtherName)
	h.w.destState.SetEscalation(setOrderOtherName, "o-backup", escalateReasonStall, time.Hour)

	held := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	expectSetOrder(t, "SYN, nameless, follows the learned site's escalation", h.syn(t, held), "O-backup", false)
	expectSetOrder(t, "held", h.hello(t, held, setOrderGGCName), setOrderGGCSet, true)
	expectSetOrder(t, "held", h.data(t, held), setOrderGGCSet, true)
	if got := h.tracked(held); got != setOrderGGCSet {
		t.Fatalf("a held connection must be tracked under the address set after its ClientHello, got %q", got)
	}
	h.forgedRST(held)
	if esc, _, ok := h.w.destState.GetEscalation(setOrderGGCName); ok {
		t.Errorf("a reset on a held connection escalated %s to %q along the chain of the set its SYN went to", setOrderGGCName, esc)
	}
}

func setOrderSiteLearnedForTheAddressSet(t *testing.T, h *setOrderHarness) {
	t.Helper()
	named := newSetOrderConn(setOrderGGCAddr, 38856, 20000)
	h.syn(t, named)
	expectSetOrder(t, "the address set's own site", h.hello(t, named, "www.ggc.example"), setOrderGGCSet, true)
	if ok, learned, host := h.w.getMatcher().MatchLearnedIP(net.ParseIP(setOrderGGCAddr)); !ok || learned.Id != "yt-ggc-ru" || host != "www.ggc.example" {
		t.Fatalf("the fixture needs %s learned for %s under www.ggc.example, got %v %v %q", setOrderGGCAddr, setOrderGGCSet, ok, learned, host)
	}
}

const setOrderNamelessKnownBehaviour = "known behaviour, a packet without a name follows the escalation of the site learned for its address"

func TestIPPriorityOnlyTheNamelessPacketsOfAHeldConnectionFollowTheEscalationOfASiteLearnedForItsAddress(t *testing.T) {
	for _, layout := range []string{"on the address set's own chain", "above the address set"} {
		t.Run("the escalation target "+layout, func(t *testing.T) {
			ggc := newSetOrderGGCSetWith(true)
			ggc.Targets.SNIDomains = []string{"ggc.example"}
			escalated := newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25)
			sets := []*config.SetConfig{escalated, ggc, newSetOrderGWSet()}
			if layout == "on the address set's own chain" {
				ggc.Escalate.To = escalated.Id
				sets = []*config.SetConfig{ggc, newSetOrderGWSet(), escalated}
			}
			h := newSetOrderHarness(t, nil, sets)
			setOrderSiteLearnedForTheAddressSet(t, h)
			h.w.destState.SetEscalation("www.ggc.example", escalated.Id, escalateReasonStall, time.Hour)
			cfg := h.w.getConfig()
			if !h.w.getMatcher().KeepsLearned(cfg, cfg.GetSetById(escalated.Id), net.ParseIP(setOrderGGCAddr)) {
				t.Fatalf("the kernel filter must keep %s for %s", setOrderGGCAddr, setOrderEscalated)
			}

			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			expectSetOrder(t, setOrderNamelessKnownBehaviour, h.syn(t, c), setOrderEscalated, false)
			expectSetOrder(t, layout, h.hello(t, c, setOrderGGCName), setOrderGGCSet, true)
			expectSetOrder(t, layout, h.data(t, c), setOrderGGCSet, true)
			expectSetOrder(t, layout, h.retransmittedHello(t, c), setOrderGGCSet, true)
			expectSetOrder(t, layout, h.data(t, c), setOrderGGCSet, true)

			split := newSetOrderConn(setOrderGGCAddr, 38858, 30000)
			expectSetOrder(t, setOrderNamelessKnownBehaviour, h.syn(t, split), setOrderEscalated, false)
			first, second := h.splitHello(t, split, setOrderGGCName)
			expectSetOrder(t, setOrderNamelessKnownBehaviour, first, setOrderEscalated, true)
			expectSetOrder(t, layout, second, setOrderGGCSet, true)
			expectSetOrder(t, layout, h.data(t, split), setOrderGGCSet, true)
		})
	}
}

func setOrderOtherSiteEscalatedAbove(holder *config.SetConfig) []*config.SetConfig {
	other := newSetOrderNameSet("other", setOrderOtherSet, 12, 23, "other.example")
	other.Escalate.To = "o-backup"
	return []*config.SetConfig{other, newSetOrderBackupSet("o-backup", setOrderOtherBackup, 15, 29), holder, newSetOrderGWSet()}
}

func setOrderDeviceSiteEscalated() []*config.SetConfig {
	site := newSetOrderNameSet("device-site", setOrderDeviceSite, 16, 31, "dev.example")
	site.Targets.SourceDevices = []string{setOrderClientMAC}
	site.Escalate.To = "device-backup"
	backup := newSetOrderBackupSet("device-backup", setOrderDeviceBackup, 17, 33)
	backup.Targets.SourceDevices = []string{setOrderClientMAC}
	return []*config.SetConfig{newSetOrderGGCSetWith(true), site, backup, newSetOrderGWSet()}
}

func TestIPPriorityAHeldConnectionStaysOnOneSetWhenAnotherSetLearnedItsAddressForAnEscalatedSite(t *testing.T) {
	blockGGC := func() *config.SetConfig {
		block := newSetOrderBlockSet("block-ggc", setOrderBlockGGC, 10, 19)
		block.Targets.IPs = append([]string(nil), setOrderGGCRanges...)
		block.Targets.IPPriority = true
		return block
	}
	for _, tc := range []struct {
		name          string
		macs          map[string]string
		sets          []*config.SetConfig
		learner, site string
		backup        string
		organic       bool
		holder        string
		blocks        bool
	}{
		{"a name set above, its site escalated to a backup above the address set", nil,
			setOrderOtherSiteEscalatedAbove(newSetOrderGGCSetWith(true)), "other", setOrderOtherName, "o-backup", false, setOrderGGCSet, false},
		{"a name set above, its site escalated after three retransmissions", nil,
			setOrderOtherSiteEscalatedAbove(newSetOrderGGCSetWith(true)), "other", setOrderOtherName, "o-backup", true, setOrderGGCSet, false},
		{"a name set above, the address set in block mode", nil,
			setOrderOtherSiteEscalatedAbove(blockGGC()), "other", setOrderOtherName, "o-backup", false, setOrderBlockGGC, true},
		{"a name set limited to the device, its site escalated to a backup limited to it", setOrderBothMACs,
			setOrderDeviceSiteEscalated(), "device-site", "www.dev.example", "device-backup", false, setOrderGGCSet, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newSetOrderHarness(t, tc.macs, tc.sets)
			first := newSetOrderConn(setOrderGGCAddr, 38850, 500)
			h.syn(t, first)
			h.hello(t, first, tc.site)
			if ok, learned, host := h.w.getMatcher().MatchLearnedIP(net.ParseIP(setOrderGGCAddr)); !ok || learned.Id != tc.learner || host != tc.site {
				t.Fatalf("the fixture needs %s learned for %s under %s, got %v %v %q", setOrderGGCAddr, tc.learner, tc.site, ok, learned, host)
			}
			if tc.organic {
				for range 3 {
					h.retransmittedHello(t, first)
				}
			} else {
				h.w.destState.SetEscalation(tc.site, tc.backup, escalateReasonStall, time.Hour)
			}
			if got, _, ok := h.w.destState.GetEscalation(tc.site); !ok || got != tc.backup {
				t.Fatalf("escalation for %s: got %q (recorded %v), want %q", tc.site, got, ok, tc.backup)
			}
			expectHeld := func(obs setOrderObs) {
				t.Helper()
				if tc.blocks {
					expectBlocked(t, obs, tc.holder)
					return
				}
				expectSetOrder(t, "held", obs, tc.holder, true)
			}

			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			h.syn(t, c)
			expectHeld(h.hello(t, c, setOrderGGCName))
			expectHeld(h.data(t, c))
			expectHeld(h.retransmittedHello(t, c))
			expectHeld(h.data(t, c))

			split := newSetOrderConn(setOrderGGCAddr, 38858, 30000)
			h.syn(t, split)
			_, second := h.splitHello(t, split, setOrderGGCName)
			expectHeld(second)
			expectHeld(h.data(t, split))
		})
	}
}

func TestIPPriorityAHeldConnectionKeepsTheAddressSetsStrategyWhateverTheTLSFilterOfTheLearnedSitesEscalationTarget(t *testing.T) {
	for _, tls := range []string{"1.2", "1.3"} {
		t.Run("the escalation target's targets.tls "+tls, func(t *testing.T) {
			ggc := newSetOrderGGCSetWith(true)
			ggc.Targets.SNIDomains = []string{"ggc.example"}
			escalated := newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25)
			escalated.Targets.TLSVersion = tls
			ggc.Escalate.To = escalated.Id
			h := newSetOrderHarness(t, nil, []*config.SetConfig{ggc, newSetOrderGWSet(), escalated})
			setOrderSiteLearnedForTheAddressSet(t, h)
			h.w.destState.SetEscalation("www.ggc.example", escalated.Id, escalateReasonStall, time.Hour)

			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			h.syn(t, c)
			expectSetOrder(t, "a TLS 1.3 ClientHello", h.hello(t, c, setOrderGGCName), setOrderGGCSet, true)
			expectSetOrder(t, "its data", h.data(t, c), setOrderGGCSet, true)
			expectSetOrder(t, "a retransmitted TLS 1.3 ClientHello", h.retransmittedHello(t, c), setOrderGGCSet, true)
			expectSetOrder(t, "its data", h.data(t, c), setOrderGGCSet, true)
		})
	}
}

func setOrderTwoBackupChains() []*config.SetConfig {
	ggc := newSetOrderGGCSetWith(true)
	ggc.Targets.SNIDomains = []string{"ggc.example"}
	ggc.Escalate.To = "ggc-backup"
	gw := newSetOrderGWSet()
	gw.Escalate.To = "gw-backup"
	ggcBackup := newSetOrderBackupSet("ggc-backup", setOrderGGCBackup, 6, 11)
	ggcBackup.Escalate.To = "ggc-backup-2"
	gwBackup := newSetOrderBackupSet("gw-backup", setOrderGWBackup, 5, 9)
	gwBackup.Escalate.To = "gw-backup-2"
	gwBackup.DSCP = config.SetDSCPConfig{Enabled: true, Value: 30}
	return []*config.SetConfig{
		ggc, gw, ggcBackup, gwBackup,
		newSetOrderBackupSet("ggc-backup-2", setOrderGGCBackup2, 7, 13),
		newSetOrderBackupSet("gw-backup-2", setOrderGWBackup2, 14, 27),
	}
}

func setOrderEscalateBothSites(t *testing.T, h *setOrderHarness) {
	t.Helper()
	h.w.destState.SetEscalation("www.ggc.example", "ggc-backup", escalateReasonStall, time.Hour)
	h.w.destState.SetEscalation(setOrderGGCName, "gw-backup", escalateReasonStall, time.Hour)
	cfg := h.w.getConfig()
	addr := net.ParseIP(setOrderGGCAddr)
	matcher := h.w.getMatcher()
	if !matcher.KeepsLearned(cfg, cfg.GetSetById("ggc-backup"), addr) || matcher.KeepsLearned(cfg, cfg.GetSetById("gw-backup"), addr) {
		t.Fatalf("the kernel filter must keep %s for %s and drop it for %s", setOrderGGCAddr, setOrderGGCBackup, setOrderGWBackup)
	}
}

func TestIPPriorityAHeldConnectionStaysOnTheAddressSetWhenBothItsSiteAndTheLearnedSiteAreEscalated(t *testing.T) {
	h := newSetOrderHarness(t, nil, setOrderTwoBackupChains())
	kernel := stubSetOrderKernel(t, h)
	setOrderSiteLearnedForTheAddressSet(t, h)
	setOrderEscalateBothSites(t, h)

	expectTracked := func(c *setOrderConn, obs setOrderObs) {
		t.Helper()
		if got := h.tracked(c); got != setOrderGGCSet {
			t.Errorf("%s: the connection tracker holds the connection for %q, want %q", obs.what, got, setOrderGGCSet)
		}
	}
	c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
	expectSetOrder(t, setOrderNamelessKnownBehaviour, h.syn(t, c), setOrderGGCBackup, false)
	for _, step := range []func() setOrderObs{
		func() setOrderObs { return h.hello(t, c, setOrderGGCName) },
		func() setOrderObs { return h.data(t, c) },
		func() setOrderObs { return h.data(t, c) },
		func() setOrderObs { return h.retransmittedHello(t, c) },
		func() setOrderObs { return h.data(t, c) },
	} {
		obs := step()
		expectSetOrder(t, "held", obs, setOrderGGCSet, true)
		expectTracked(c, obs)
	}

	split := newSetOrderConn(setOrderGGCAddr, 38858, 30000)
	h.syn(t, split)
	first, second := h.splitHello(t, split, setOrderGGCName)
	expectSetOrder(t, setOrderNamelessKnownBehaviour, first, setOrderGGCBackup, true)
	expectSetOrder(t, "held", second, setOrderGGCSet, true)
	expectTracked(split, second)
	data := h.data(t, split)
	expectSetOrder(t, "held", data, setOrderGGCSet, true)
	expectTracked(split, data)

	for _, kind := range []string{"route", "dscp"} {
		if kept, dropped := kernel.of(kind, "gw-backup"); len(kept)+len(dropped) != 0 {
			t.Errorf("nfq handed the held address to %s: %s kept %v, dropped %v", setOrderGWBackup, kind, kept, dropped)
		}
	}
}

func TestIPPriorityAHeldQUICFlowNeverFollowsTheNameSetsEscalation(t *testing.T) {
	h := newSetOrderHarness(t, nil, setOrderTwoBackupChains())
	kernel := stubSetOrderKernel(t, h)
	setOrderSiteLearnedForTheAddressSet(t, h)
	setOrderEscalateBothSites(t, h)

	expectSetOrder(t, "held", h.quicInitial(t, setOrderGGCName), setOrderGGCSet, true)
	for range 2 {
		expectSetOrder(t, "held", h.quicShortHeader(t), setOrderGGCSet, true)
	}
	if kept, dropped := kernel.of("dscp", "gw-backup"); len(kept)+len(dropped) != 0 {
		t.Errorf("nfq handed the held address to %s: DSCP kept %v, dropped %v", setOrderGWBackup, kept, dropped)
	}
}

func TestIPPriorityKnownLimitationAnEscalationTargetBelowTheAddressSetNeverRoutesItsAddresses(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("switch on=%v", on), func(t *testing.T) {
			gw := newSetOrderGWSet()
			gw.Escalate.To = "escalated"
			ggc := newSetOrderGGCSetWith(on)
			ggc.Targets.IPs = []string{"37.18.153.140/29"}
			escalated := newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25)
			escalated.Routing.Enabled = true
			escalated.Routing.Mode = config.RoutingModeInterface
			escalated.Routing.EgressInterface = "wg0"
			escalated.DSCP = config.SetDSCPConfig{Enabled: true, Value: 20}
			h := newSetOrderHarness(t, nil, []*config.SetConfig{gw, ggc, escalated})
			cfg := h.w.getConfig()
			kernelFilter := sni.NewSuffixSet(cfg.Sets)
			kernel := stubSetOrderKernelWith(t, kernelFilter.KeepsLearned)
			h.w.destState.SetEscalation(setOrderGGCName, escalated.Id, escalateReasonStall, time.Hour)

			expectSetOrder(t, "the escalated site", h.quicInitial(t, setOrderGGCName), setOrderEscalated, true)
			expectSetOrder(t, "a nameless datagram", h.quicShortHeader(t), setOrderGGCSet, true)
			c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
			expectSetOrder(t, "the escalated site", h.syn(t, c), setOrderEscalated, false)
			expectSetOrder(t, "the escalated site", h.hello(t, c, setOrderGGCName), setOrderEscalated, true)
			expectSetOrder(t, "the escalated site", h.data(t, c), setOrderEscalated, true)

			if keeps := kernelFilter.KeepsLearned(cfg, cfg.GetSetById(escalated.Id), net.ParseIP(setOrderGGCAddr)); keeps == on {
				t.Errorf("the kernel filter keeps %s for %s: %v, want %v", setOrderGGCAddr, setOrderEscalated, keeps, !on)
			}
			for _, kind := range []string{"route", "dscp"} {
				kept, dropped := kernel.of(kind, escalated.Id)
				if on && (slices.Contains(kept, setOrderGGCAddr) || !slices.Contains(dropped, setOrderGGCAddr)) {
					t.Errorf("switch on: the %s write of %s for %s must be dropped: kept %v, dropped %v", kind, setOrderGGCAddr, setOrderEscalated, kept, dropped)
				}
				if !on && (!slices.Contains(kept, setOrderGGCAddr) || len(dropped) != 0) {
					t.Errorf("switch off: the %s write of %s for %s must be kept: kept %v, dropped %v", kind, setOrderGGCAddr, setOrderEscalated, kept, dropped)
				}
			}
		})
	}
}

func TestIPPriorityRetransmittedHeldClientHellosNeverEscalateTheirSiteAlongAChainTheHoldRefuses(t *testing.T) {
	holder := func(on bool) *config.SetConfig {
		ggc := newSetOrderGGCSetWith(on)
		ggc.Targets.SNIDomains = []string{"ggc.example"}
		return ggc
	}
	backups := func() (*config.SetConfig, *config.SetConfig) {
		escalated := newSetOrderBackupSet("escalated", setOrderEscalated, 13, 25)
		escalated.Escalate.To = "next"
		return escalated, newSetOrderBackupSet("next", "Next", 14, 27)
	}
	for _, tc := range []struct {
		name    string
		macs    map[string]string
		sets    func(on bool) []*config.SetConfig
		learner string
		learn   func(t *testing.T, h *setOrderHarness)
	}{
		{"another device's set learns the address and its site stalls into a backup above the address set", setOrderBothMACs, func(on bool) []*config.SetConfig {
			dev := newSetOrderNameSet("dev", "Dev", 16, 31, "ggc.example")
			dev.Targets.SourceDevices = []string{setOrderOtherMAC}
			dev.Escalate.To = "escalated"
			escalated, next := backups()
			return []*config.SetConfig{dev, escalated, holder(on), newSetOrderGWSet(), next}
		}, "dev", func(t *testing.T, h *setOrderHarness) {
			theirs := newSetOrderConn(setOrderGGCAddr, 38840, 400).from(setOrderOtherClient)
			h.syn(t, theirs)
			h.hello(t, theirs, "www.ggc.example")
			for range 2 {
				h.retransmittedHello(t, theirs)
			}
		}},
		{"the address set's own site learned for the address and escalated to a backup above it", nil, func(on bool) []*config.SetConfig {
			escalated, next := backups()
			return []*config.SetConfig{escalated, holder(on), newSetOrderGWSet(), next}
		}, "yt-ggc-ru", func(t *testing.T, h *setOrderHarness) {
			named := newSetOrderConn(setOrderGGCAddr, 38850, 500)
			h.syn(t, named)
			h.hello(t, named, "www.ggc.example")
			h.w.destState.SetEscalation("www.ggc.example", "escalated", escalateReasonStall, time.Hour)
		}},
	} {
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, switch on=%v", tc.name, on), func(t *testing.T) {
				h := newSetOrderHarness(t, tc.macs, tc.sets(on))
				tc.learn(t, h)
				if ok, learned, host := h.w.getMatcher().MatchLearnedIP(net.ParseIP(setOrderGGCAddr)); !ok || learned.Id != tc.learner || host != "www.ggc.example" {
					t.Fatalf("the fixture needs %s learned for %s under www.ggc.example, got %v %v %q", setOrderGGCAddr, tc.learner, ok, learned, host)
				}
				if got, _, ok := h.w.destState.GetEscalation("www.ggc.example"); !ok || got != "escalated" {
					t.Fatalf("escalation for www.ggc.example: got %q (recorded %v), want escalated", got, ok)
				}

				want := setOrderGWSet
				if on {
					want = setOrderGGCSet
				}
				c := newSetOrderConn(setOrderGGCAddr, 38854, 1000)
				h.syn(t, c)
				hellos := []setOrderObs{h.hello(t, c, setOrderGGCName)}
				for range 10 {
					hellos = append(hellos, h.retransmittedHello(t, c))
				}
				for _, obs := range hellos {
					expectSetOrder(t, "held", obs, want, true)
					for _, row := range obs.rows {
						if strings.HasPrefix(row.meta, "escalate->") {
							t.Errorf("%s was dropped for an escalation (%s); %s", obs.what, row.meta, obs)
						}
					}
				}
				if got, hops, ok := h.w.destState.GetEscalation(setOrderGGCName); ok {
					t.Errorf("the held connection escalated its own site %s to %q (%d hops)", setOrderGGCName, got, hops)
				}

				elsewhere := newSetOrderConn(setOrderGoogleAddr, 38880, 80000)
				h.syn(t, elsewhere)
				expectSetOrder(t, "the same site outside the address set's ranges", h.hello(t, elsewhere, setOrderGGCName), setOrderGWSet, true)
				expectSetOrder(t, "the same site outside the address set's ranges", h.data(t, elsewhere), setOrderGWSet, true)
			})
		}
	}
}
