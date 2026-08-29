package mtproto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const (
	obfuscatedFrameLen    = 64
	connectionTagAbridged = 0xefefefef
	connectionTagInter    = 0xeeeeeeee
	connectionTagPadded   = 0xdddddddd

	telegramWSEdgeIP = "149.154.167.220"
	dcDefaultPort    = 443

	// A client is already committed by the time the dial starts: the fake-TLS and
	// obfuscated handshakes are answered locally, in under a millisecond, so
	// Telegram counts the session as accepted and then waits for a data center
	// that b4 has not reached yet. It does not wait long. In a capture from a
	// censored network every session whose dial ran past five seconds was found
	// already closed when the relay finally started, up=N down=0 in 0ms, while
	// every session that started relaying inside 300 ms carried traffic normally.
	// So the whole dial, across every transport it tries, has to fit inside that
	// window with room to spare, and one dead candidate must not be able to spend
	// it.
	//
	// This budget does not touch the dialog that says the proxy is misconfigured,
	// whatever it once looked like: that dialog has one trigger in both clients, a
	// four-byte -444 arriving from the data center, and silence delivers no such
	// value. See transportErrHandler.
	dialBudget    = 4500 * time.Millisecond
	wsDialTimeout = 3 * time.Second
	// tcpDialTimeout is the direct-to-DC fallback, reached only after the
	// WebSocket routes, and it is bounded by the remaining budget in any case.
	tcpDialTimeout = 3 * time.Second
)

var wsEdgeServedDCs = map[int]bool{2: true, 4: true}

func wsEdgeServesDC(absDC int) bool {
	return wsEdgeServedDCs[absDC]
}

func wsNativeDialHost(override string) string {
	if h := strings.TrimSpace(override); h != "" {
		return h
	}
	return telegramWSEdgeIP
}

func normalizeWorkerDomain(d string) string {
	d = strings.TrimSpace(d)
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	return strings.TrimSpace(d)
}

func workerDomains(cfg *config.MTProtoConfig) []string {
	raw := strings.TrimSpace(cfg.CFWorkerDomain)
	if raw == "" {
		return nil
	}
	var out []string
	for _, d := range strings.Split(raw, ",") {
		if d = normalizeWorkerDomain(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// shuffledWorkerDomains randomises the configured Worker order so a user with
// several workers spreads load and free-tier request quota across all of them
// instead of hammering whichever one happens to be listed first.
func shuffledWorkerDomains(cfg *config.MTProtoConfig) []string {
	out := workerDomains(cfg)
	if len(out) < 2 {
		return out
	}
	mrand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func workerDstIP(absDC int) string {
	addr, ok := dcAddressesV4[absDC]
	if !ok {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

type ObfuscatedConn struct {
	net.Conn
	reader cipher.Stream
	writer cipher.Stream
}

func (c *ObfuscatedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.reader.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (c *ObfuscatedConn) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	c.writer.XORKeyStream(buf, p)
	return c.Conn.Write(buf)
}

type ClientHandshakeResult struct {
	DC       int
	ProtoTag uint32
	Conn     *ObfuscatedConn
}

func AcceptObfuscated(conn net.Conn, secret *Secret) (*ClientHandshakeResult, error) {
	return acceptObfuscatedFrame(conn, func(raw []byte) []byte {
		return deriveKey(raw, secret.Key[:])
	})
}

func acceptObfuscatedFrame(conn net.Conn, derive func(raw []byte) []byte) (*ClientHandshakeResult, error) {
	frame := make([]byte, obfuscatedFrameLen)
	if _, err := io.ReadFull(conn, frame); err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	return decodeObfuscatedFrame(frame, conn, derive)
}

func decodeObfuscatedDirect(frame []byte, conn net.Conn) (*ClientHandshakeResult, error) {
	return decodeObfuscatedFrame(frame, conn, func(raw []byte) []byte {
		out := make([]byte, len(raw))
		copy(out, raw)
		return out
	})
}

func decodeObfuscatedFrame(frame []byte, conn net.Conn, derive func(raw []byte) []byte) (*ClientHandshakeResult, error) {
	decIV := make([]byte, 16)
	copy(decIV, frame[40:56])
	decStream, err := newAESCTR(derive(frame[8:40]), decIV)
	if err != nil {
		return nil, fmt.Errorf("init decrypt: %w", err)
	}

	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	encIV := make([]byte, 16)
	copy(encIV, reversed[32:48])
	encStream, err := newAESCTR(derive(reversed[:32]), encIV)
	if err != nil {
		return nil, fmt.Errorf("init encrypt: %w", err)
	}

	decrypted := make([]byte, obfuscatedFrameLen)
	copy(decrypted, frame)
	decStream.XORKeyStream(decrypted, decrypted)

	tag := binary.LittleEndian.Uint32(decrypted[56:60])
	switch tag {
	case connectionTagAbridged, connectionTagInter, connectionTagPadded:
	default:
		return nil, fmt.Errorf("invalid connection tag: 0x%08x", tag)
	}

	dc := int(int16(binary.LittleEndian.Uint16(decrypted[60:62])))

	return &ClientHandshakeResult{
		DC:       dc,
		ProtoTag: tag,
		Conn: &ObfuscatedConn{
			Conn:   conn,
			reader: decStream,
			writer: encStream,
		},
	}, nil
}

type transportKind int

const (
	transportTCP transportKind = iota
	transportWS
)

type transportPlan struct {
	kind     transportKind
	addr     string
	sni      string
	dialHost string
	dc       int
	cfBase   string
	wsPath   string
	isWorker bool
	native   bool
}

// selfDialMark is what b4 puts on every connection it opens to Telegram. It is
// deliberately not the queue mark: the mangle chains accept that one to keep the
// engine's own reinjected packets from being queued again, so an upstream dial
// carrying it went out with none of b4's DPI bypass applied - measured against a
// data center from a censored network, 114 ms unmarked against a timeout marked.
// It is a variable so a test can drop the mark: setting SO_MARK needs
// CAP_NET_ADMIN, and without it every dial fails on the setsockopt instead of
// on the route under test.
var selfDialMark = func() uint {
	return uint(config.SelfDialMark)
}

func workerNameOf(p transportPlan) string {
	if p.isWorker {
		return p.sni
	}
	return ""
}

// routeName is describe() for a plan that may be the zero value, which a dialInfo
// carries whenever the route was not recorded.
func (p transportPlan) routeName() string {
	if p.sni == "" && p.addr == "" {
		return ""
	}
	return p.describe()
}

func (p transportPlan) describe() string {
	switch p.kind {
	case transportWS:
		if p.isWorker {
			return "wsworker://" + p.sni
		}
		if p.dialHost != "" && p.dialHost != p.sni {
			return fmt.Sprintf("ws://%s@%s", p.sni, p.dialHost)
		}
		return "ws://" + p.sni
	default:
		return "tcp://" + p.addr
	}
}

// dialTarget is the address a transparently-intercepted client was actually
// dialling. The proxy has no such address and leaves it zero; the bridge does,
// and it beats every guess derived from the DC number - the canonical table has
// one address per DC while Telegram hands clients many, and a media address
// resolves to a DC whose canonical address is not the media one.
type dialTarget struct {
	ip   string
	port int
}

func (t dialTarget) valid() bool {
	return t.ip != "" && t.port > 0 && t.port <= 65535
}

func (t dialTarget) dcAddr() string {
	return net.JoinHostPort(t.ip, strconv.Itoa(dcDefaultPort))
}

func (t dialTarget) isV4() bool {
	ip := net.ParseIP(t.ip)
	return ip != nil && ip.To4() != nil
}

func planTransports(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int, target dialTarget) ([]transportPlan, error) {
	absDC := dc
	if absDC < 0 {
		absDC = -absDC
	}

	mode := cfg.UpstreamMode
	if mode == "" {
		mode = "tcp"
	}

	hasRelay := strings.TrimSpace(cfg.DCRelay) != ""
	relayFirst := mode == "auto" && hasRelay

	var plans []transportPlan
	var deferred []transportPlan

	appendTCP := func(ignoreCooldown bool) {
		var addrs []string
		var primary string
		if target.valid() && !hasRelay {
			primary = target.dcAddr()
			addrs = append(addrs, primary)
		}
		resolved, err := ResolveDCAll(dc, queueCfg.IPv6Enabled, strings.TrimSpace(cfg.DCRelay))
		if err == nil {
			for _, a := range resolved {
				if a != primary {
					addrs = append(addrs, a)
				}
			}
		}
		for _, a := range addrs {
			if !ignoreCooldown && tcpAddrInCooldown(a) {
				continue
			}
			plans = append(plans, transportPlan{kind: transportTCP, addr: a})
		}
	}

	if mode == "tcp" || relayFirst {
		appendTCP(false)
	}

	wsMode := mode == "ws" || mode == "auto"
	if wsMode {
		// The blacklist records Telegram's own WS edge answering every kws* dial
		// with a 302. That says nothing about a relay the user runs, so it must
		// only suppress the native plans - gating the worker and CF-proxy plans on
		// it too meant one 302 from kws2 silently switched off a working Worker.
		if wsEdgeServesDC(absDC) && !cfg.BridgeSkipNativeEdge {
			if wsIsBlacklisted(dc) {
				log.Debugf("%s DC %d native WS edge skipped (blacklisted)", tg(""), dc)
			} else {
				dh := wsNativeDialHost(cfg.WSEndpointHost)
				plans = append(plans, nativeEdgePlans(dc, absDC, dh)...)
			}
		}
		if d := strings.TrimSpace(cfg.WSCustomDomain); d != "" {
			plans = append(plans, transportPlan{
				kind:   transportWS,
				dc:     dc,
				sni:    fmt.Sprintf("kws%d.%s", absDC, d),
				cfBase: d,
			})
		}
		if cfg.CFProxyEnabled {
			for _, base := range cfBalancerInst.domainsForDC(dc) {
				plans = append(plans, transportPlan{
					kind:   transportWS,
					dc:     dc,
					sni:    fmt.Sprintf("kws%d.%s", absDC, base),
					cfBase: base,
				})
			}
		}
		// A Worker goes behind the Cloudflare-proxied domains, not ahead of them. It
		// dials in 190 ms and answers a handshake, so every health check passes it,
		// and then Cloudflare reclaims the request: measured from a censored network
		// against DC 1, a Worker carried 8 rounds and went mute at 8.7 s while a
		// pooled domain carried 100 rounds over two minutes on the same box. Ahead of
		// the pool it wins the dial and takes the session down with it, because the
		// list only guards dial failures.
		dst := workerDstIP(absDC)
		if target.valid() && target.isV4() {
			dst = target.ip
		}
		if dst != "" {
			for _, wd := range shuffledWorkerDomains(cfg) {
				p := transportPlan{
					kind:     transportWS,
					dc:       dc,
					sni:      wd,
					dialHost: wd,
					wsPath:   fmt.Sprintf("/apiws?dst=%s&dc=%d", dst, absDC),
					isWorker: true,
				}
				if workerInCooldown(wd) {
					deferred = append(deferred, p)
					continue
				}
				plans = append(plans, p)
			}
		}
	}

	if mode == "auto" && !relayFirst {
		appendTCP(false)
	}

	// Last resort: every candidate is cooling off. Erroring out here fails the
	// client in microseconds, and Telegram answers an instant failure with an
	// instant reconnect, so one cooling DC address turns into a dial storm that
	// never lets the cooldown lapse. Re-dialling the cooling address is slower
	// but bounded.
	if len(plans) == 0 && len(deferred) == 0 && mode != "ws" {
		appendTCP(true)
	}

	// A Worker that stopped relaying mid-session is still a route, just the worst
	// one, so it goes behind everything else rather than out of the list - for a
	// DC with no native edge it can be the only route there is.
	plans = append(plans, deferred...)

	if len(plans) == 0 {
		return nil, fmt.Errorf("no transports available for DC %d (mode=%s)", absDC, mode)
	}
	if log.Level(log.CurLevel.Load()) >= log.LevelTrace {
		names := make([]string, 0, len(plans))
		for _, p := range plans {
			names = append(names, p.describe())
		}
		log.Tracef("%s DC %d transport list (mode=%s): %s", tg(""), dc, mode, strings.Join(names, " -> "))
	}
	return plans, nil
}

// dialInfo describes which transport a dial landed on. isWorker matters to the
// caller because a Cloudflare Worker relays a raw TCP stream, so it must not get
// the per-packet WS framing that Telegram's own edge requires.
type dialInfo struct {
	transport string
	isWorker  bool
	worker    string
	// plan is the route the session actually rides. A pooled session used to be
	// logged as "ws-pool" and nothing else, so an upstream that rejected the
	// session could not be told apart from one that carried it, and nothing could
	// rank that route down afterwards.
	plan transportPlan
}

// dialPools bundles the warm-connection pools a dial may draw from. Both the
// struct pointer and either field may be nil.
type dialPools struct {
	ws     *wsPool
	worker *cfWorkerPool
}

func (p *dialPools) wsPool() *wsPool {
	if p == nil {
		return nil
	}
	return p.ws
}

func (p *dialPools) workerPool() *cfWorkerPool {
	if p == nil {
		return nil
	}
	return p.worker
}

func DialObfuscatedDC(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int, protoTag uint32) (*ObfuscatedConn, string, error) {
	conn, info, err := dialObfuscatedDC(cfg, queueCfg, dc, protoTag, nil, "", dialTarget{})
	return conn, info.transport, err
}

func dialObfuscatedDC(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int, protoTag uint32, pools *dialPools, logID string, target dialTarget) (*ObfuscatedConn, dialInfo, error) {
	tag := tg(logID)
	if pool := pools.wsPool(); pool != nil && !wsIsBlacklisted(dc) {
		if raw := pool.get(dc); raw != nil {
			obf, err := completeObfuscation(raw.conn, dc, protoTag)
			if err == nil && raw.conn.liveNow() {
				log.Infof("%s DC %d connected via ws-pool %s", tag, dc, raw.plan.describe())
				wsRecordSuccess(dc)
				return obf, dialInfo{transport: "ws-pool", plan: raw.plan}, nil
			}
			if err != nil {
				log.Debugf("%s DC %d pool conn on %s obf init failed: %v", tag, dc, raw.plan.describe(), err)
			} else {
				log.Debugf("%s DC %d pool conn on %s died before relay; re-dialing fresh", tag, dc, raw.plan.describe())
			}
			_ = raw.conn.Close()
		}
	}

	plans, err := planTransports(cfg, queueCfg, dc, target)
	if err != nil {
		return nil, dialInfo{}, err
	}

	nativeCooling := wsCooldownActive(dc)
	workerPool := pools.workerPool()

	// A cooling edge is one that just went unanswered, and both native plans
	// resolve to the same address, so trying them is two timeouts spent to learn
	// what the cooldown already recorded. Step over them while another transport
	// exists; the pool keeps probing the edge in the background and clears the
	// cooldown the moment it answers again.
	haveFallback := hasNonNativePlan(plans)
	skipNative := nativeCooling && haveFallback

	deadline := time.Now().Add(dialBudget)
	var attempts []string
	nativeTried := 0
	nativeRedirects := 0
	untried := 0
	for _, p := range plans {
		// The per-address record outlives the per-DC one, which any success
		// clears. Without it a flapping edge was retried from scratch on every
		// session and spent the whole budget before a Cloudflare domain was
		// reached even once.
		if p.native && haveFallback && (skipNative || wsEndpointCooling(p.dialHost, p.sni)) {
			untried++
			continue
		}
		remaining := time.Until(deadline)
		if remaining < wsDialMinAttempt {
			untried++
			continue
		}
		if p.isWorker {
			if raw := workerPool.get(p); raw != nil {
				obf, oerr := completeObfuscation(raw, dc, protoTag)
				if oerr == nil && raw.liveNow() {
					log.Infof("%s DC %d connected via %s (pooled)", tag, dc, p.describe())
					workerPool.warm(p)
					return obf, dialInfo{transport: p.describe(), isWorker: true, worker: p.sni, plan: p}, nil
				}
				if oerr != nil {
					log.Debugf("%s DC %d worker pool conn obf init failed: %v", tag, dc, oerr)
				} else {
					log.Debugf("%s DC %d worker pool conn died before relay; re-dialing fresh", tag, dc)
				}
				_ = raw.Close()
			}
		}

		log.Debugf("%s DC %d dialing %s", tag, dc, p.describe())
		start := time.Now()
		var conn net.Conn
		var derr error
		if p.kind == transportWS {
			// the shortened timeout belongs to the native edge that just failed;
			// applying it to a Worker or CF-proxy dial would cut those off early
			// for a fault that is not theirs
			timeout := wsDialTimeout
			if p.native && nativeCooling {
				timeout = wsDialTimeoutCooldown
			}
			if timeout > remaining {
				timeout = remaining
			}
			conn, derr = dialOneWS(p, selfDialMark(), timeout)
		} else {
			timeout := tcpDialTimeout
			if timeout > remaining {
				timeout = remaining
			}
			conn, derr = dialOneTCP(p, selfDialMark(), timeout)
		}
		if derr != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %s", p.describe(), shortErr(derr)))
			timedOut := isDialTimeout(derr)
			if p.kind == transportWS {
				if p.native {
					nativeTried++
					if isWSRedirect(derr) {
						nativeRedirects++
					} else if timedOut {
						// Cool the edge down here rather than after the loop. The
						// loop only reaches its end when every transport failed, so
						// an edge that timed out and was then rescued by a later
						// route was recorded as healthy, and stayed first in line
						// at full price for the next session as well. Measured on a
						// censored network, the same 8 s timeout to
						// 149.154.167.220 recurred at 18:29, 18:32 and 18:33.
						wsRecordFailure(dc, false)
						nativeCooling = true
						// The sibling name resolves to the same address, so it is
						// the same timeout again. Spend what is left of the budget
						// on a route that might differ.
						skipNative = haveFallback
					}
				}
				if p.cfBase != "" {
					switch {
					case wsRateLimited(derr):
						cfBalancerInst.penalize(p.cfBase, cfProxyDomainCooldown)
					case timedOut:
						// A domain that goes unanswered costs far more than one
						// that answers 429, and nothing used to record it, so
						// every session in flight paid the same timeout on the
						// same pinned domain.
						cfBalancerInst.penalize(p.cfBase, cfProxyTimeoutCooldown)
					}
				}
			} else if timedOut {
				tcpRecordFailure(p.addr)
			}
			log.Debugf("%s DC %d %s failed after %dms: %v", tag, dc, p.describe(), time.Since(start).Milliseconds(), derr)
			continue
		}
		obfConn, oerr := completeObfuscation(conn, dc, protoTag)
		if oerr != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %s", p.describe(), shortErr(oerr)))
			conn.Close()
			log.Debugf("%s DC %d obf init failed on %s: %v", tag, dc, p.describe(), oerr)
			continue
		}
		if p.kind == transportWS {
			if p.native {
				wsRecordSuccess(dc)
			}
			if p.isWorker {
				workerPool.warm(p)
			}
			if p.cfBase != "" {
				if cfBalancerInst.pin(dc, p.cfBase) {
					log.Infof("%s DC %d switched active CF domain to %s", tag, dc, p.cfBase)
				}
			}
		} else {
			tcpRecordSuccess(p.addr)
		}
		log.Infof("%s DC %d connected via %s in %dms", tag, dc, p.describe(), time.Since(start).Milliseconds())
		return obfConn, dialInfo{transport: p.describe(), isWorker: p.isWorker, worker: workerNameOf(p), plan: p}, nil
	}

	if nativeTried > 0 {
		wsRecordFailure(dc, nativeRedirects == nativeTried)
	}
	if untried > 0 {
		// Say so rather than letting the list look exhausted. Walking the rest is
		// not the alternative: past this point the client has stopped waiting, so
		// a connection made now is made for nobody.
		log.Debugf("%s DC %d gave up with %d transport(s) untried (dial budget %v spent)", tag, dc, untried, dialBudget)
	}
	if len(attempts) == 0 {
		return nil, dialInfo{}, fmt.Errorf("no transport available (all in cooldown or blacklisted)")
	}
	return nil, dialInfo{}, fmt.Errorf("all transports failed: %s", strings.Join(attempts, "; "))
}

func isDialTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return strings.Contains(err.Error(), "i/o timeout")
}

func shortErr(err error) string {
	s := err.Error()
	for _, p := range []string{"tcp dial ", "tls handshake ", "ws read response: ", "ws write upgrade: ", "ws handshake "} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

func isWSRedirect(err error) bool {
	var he *wsHandshakeError
	if !errors.As(err, &he) {
		return false
	}
	return he.isRedirect()
}

func wsRateLimited(err error) bool {
	var he *wsHandshakeError
	if !errors.As(err, &he) {
		return false
	}
	return he.statusCode == 429 || he.statusCode == 503
}

func dialOneWS(p transportPlan, mark uint, timeout time.Duration) (net.Conn, error) {
	host := p.dialHost
	if host == "" {
		host = p.sni
	}
	return dialWS(host, p.sni, p.wsPath, timeout, mark)
}

type TransportProbeResult struct {
	Transport string `json:"transport"`
	OK        bool   `json:"ok"`
	Stage     string `json:"stage,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	HoldMs    int64  `json:"hold_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

const probeHoldDuration = 2 * time.Second

func ProbeTransports(cfg *config.MTProtoConfig, queueCfg config.QueueConfig, dc int) ([]TransportProbeResult, error) {
	plans, err := planTransports(cfg, queueCfg, dc, dialTarget{})
	if err != nil {
		return nil, err
	}
	out := make([]TransportProbeResult, len(plans))
	for i, p := range plans {
		out[i] = probeOne(p, selfDialMark(), dc)
	}
	return out, nil
}

func probeOne(p transportPlan, mark uint, dc int) TransportProbeResult {
	res := TransportProbeResult{Transport: p.describe()}
	dialStart := time.Now()
	conn, err := dialOne(p, mark)
	if err != nil {
		res.Stage = "dial"
		res.Error = err.Error()
		return res
	}
	res.LatencyMs = time.Since(dialStart).Milliseconds()
	defer conn.Close()

	if _, err := completeObfuscation(conn, dc, connectionTagAbridged); err != nil {
		res.Stage = "handshake"
		res.Error = err.Error()
		return res
	}

	_ = conn.SetReadDeadline(time.Now().Add(probeHoldDuration))
	holdStart := time.Now()
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	res.HoldMs = time.Since(holdStart).Milliseconds()

	if readErr == nil {
		res.OK = true
		return res
	}
	if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
		res.OK = true
		return res
	}
	res.Stage = "hold"
	res.Error = "upstream closed connection: " + readErr.Error()
	return res
}

func hasNonNativePlan(plans []transportPlan) bool {
	for _, p := range plans {
		if !p.native {
			return true
		}
	}
	return false
}

func dialOne(p transportPlan, mark uint) (net.Conn, error) {
	switch p.kind {
	case transportWS:
		host := p.dialHost
		if host == "" {
			host = p.sni
		}
		return dialWS(host, p.sni, p.wsPath, wsDialTimeout, mark)
	default:
		return dialOneTCP(p, mark, tcpDialTimeout)
	}
}

func dialOneTCP(p transportPlan, mark uint, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	if mark > 0 {
		dialer.Control = func(network, address string, c syscall.RawConn) error {
			var sErr error
			if err := c.Control(func(fd uintptr) {
				sErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_MARK, int(mark))
			}); err != nil {
				return err
			}
			return sErr
		}
	}
	conn, err := dialer.Dial(dialNetwork(), p.addr)
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		setTCPUserTimeout(tc, defaultUserTimeout)
	}
	return conn, nil
}

func completeObfuscation(conn net.Conn, dc int, protoTag uint32) (*ObfuscatedConn, error) {
	frame := generateFrame(dc, protoTag)

	encKey := frame[8:40]
	encIV := make([]byte, 16)
	copy(encIV, frame[40:56])
	encStream, err := newAESCTR(encKey, encIV)
	if err != nil {
		return nil, fmt.Errorf("init encrypt: %w", err)
	}

	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	decKey := reversed[:32]
	decIV := make([]byte, 16)
	copy(decIV, reversed[32:48])
	decStream, err := newAESCTR(decKey, decIV)
	if err != nil {
		return nil, fmt.Errorf("init decrypt: %w", err)
	}

	encrypted := make([]byte, obfuscatedFrameLen)
	copy(encrypted, frame)
	encStream.XORKeyStream(encrypted, encrypted)
	copy(encrypted[0:56], frame[0:56])

	if _, err := conn.Write(encrypted); err != nil {
		return nil, fmt.Errorf("send handshake: %w", err)
	}

	return &ObfuscatedConn{
		Conn:   conn,
		reader: decStream,
		writer: encStream,
	}, nil
}

var reservedFirst4Words = []uint32{
	0x44414548,
	0x54534f50,
	0x20544547,
	0x4954504f,
	0x02010316,
	0xdddddddd,
	0xeeeeeeee,
}

func isReservedFirst4(b []byte) bool {
	if b[0] == 0xef {
		return true
	}
	first4 := binary.LittleEndian.Uint32(b[:4])
	for _, w := range reservedFirst4Words {
		if first4 == w {
			return true
		}
	}
	return false
}

func generateFrame(dc int, protoTag uint32) []byte {
	frame := make([]byte, obfuscatedFrameLen)
	for {
		if _, err := rand.Read(frame); err != nil {
			continue
		}

		if isReservedFirst4(frame[0:4]) {
			continue
		}
		if binary.LittleEndian.Uint32(frame[4:8]) == 0 {
			continue
		}
		break
	}

	binary.LittleEndian.PutUint32(frame[56:60], protoTag)
	binary.LittleEndian.PutUint16(frame[60:62], uint16(int16(dc)))
	return frame
}

func deriveKey(rawKey []byte, secret []byte) []byte {
	h := sha256.New()
	h.Write(rawKey)
	h.Write(secret)
	return h.Sum(nil)
}

func newAESCTR(key, iv []byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, iv), nil
}
