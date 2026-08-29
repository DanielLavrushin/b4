package mtproto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

const (
	defaultMaxConnections = 2048
	relayBufSize          = 65536
	defaultIdleTimeout    = 300 * time.Second
)

func mtprotoMaxConnections(cfg *config.Config) int {
	if n := cfg.System.MTProto.MaxConnections; n > 0 {
		return n
	}
	return defaultMaxConnections
}

func mtprotoTCPUserTimeout(cfg *config.Config) time.Duration {
	switch n := cfg.System.MTProto.TCPUserTimeoutSec; {
	case n < 0:
		return 0
	case n == 0:
		return defaultUserTimeout
	default:
		return time.Duration(n) * time.Second
	}
}

func mtprotoIdleTimeout(cfg *config.Config) time.Duration {
	switch n := cfg.System.MTProto.IdleTimeoutSec; {
	case n < 0:
		return 0
	case n == 0:
		return defaultIdleTimeout
	default:
		return time.Duration(n) * time.Second
	}
}

type Server struct {
	bufPool sync.Pool
	active  atomic.Int64

	cfg        atomic.Pointer[config.Config]
	secrets    atomic.Pointer[[]*Secret]
	wsPool     atomic.Pointer[wsPool]
	workerPool atomic.Pointer[cfWorkerPool]

	statsMu sync.Mutex
	stats   map[string]*secretStat

	connsMu  sync.Mutex
	conns    map[string]*secretConnSet
	refusals map[string]*refusalState

	webTickets webTicketStore

	mu       sync.Mutex
	running  bool
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
}

type secretStat struct {
	active atomic.Int64
	total  atomic.Int64
	up     atomic.Int64
	down   atomic.Int64
}

type SecretStat struct {
	Name         string
	Active       int64
	Total        int64
	BytesUp      int64
	BytesDown    int64
	Networks     int
	NetworkAddrs []string
}

type Stats struct {
	Enabled           bool
	Port              int
	Networks          int
	ActiveConnections int64
	TotalConnections  int64
	BytesUp           int64
	BytesDown         int64
	Secrets           []SecretStat
}

func (s *Server) secretStat(sec *Secret) *secretStat {
	key := sec.ID
	if key == "" {
		key = sec.Label()
	}
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	if s.stats == nil {
		s.stats = make(map[string]*secretStat)
	}
	st := s.stats[key]
	if st == nil {
		st = &secretStat{}
		s.stats[key] = st
	}
	return st
}

type connInfo struct {
	secretID    string
	secretName  string
	clientIP    string
	clientPort  int
	network     string
	connectedAt time.Time
	dest        atomic.Pointer[string]
	lastActive  atomic.Int64
}

type secretConnSet struct {
	label    string
	conns    map[net.Conn]*connInfo
	networks map[string]int
}

type refusalState struct {
	total int64
	last  time.Time
}

type secretPolicy struct {
	active      bool
	maxNetworks int
}

type denyInfo struct {
	limit int
	total int64
	log   bool
}

const refusalLogInterval = 60 * time.Second

func (s *Server) refuseLocked(id string, now time.Time, limit int) denyInfo {
	if s.refusals == nil {
		s.refusals = make(map[string]*refusalState)
	}
	r := s.refusals[id]
	if r == nil {
		r = &refusalState{}
		s.refusals[id] = r
	}
	r.total++
	d := denyInfo{limit: limit, total: r.total}
	if now.Sub(r.last) >= refusalLogInterval {
		r.last = now
		d.log = true
	}
	return d
}

type SessionInfo struct {
	ID          string
	Name        string
	ClientIP    string
	ClientPort  int
	Destination string
	ConnectedAt time.Time
	LastSeen    time.Time
}

func secretIdentity(sec *Secret) string {
	key := sec.ID
	if key == "" {
		key = sec.Label()
	}
	return key + "|" + sec.Hex()
}

func (s *Server) trackConn(sec *Secret, c net.Conn) (*connInfo, func(), denyInfo) {
	id := secretIdentity(sec)
	info := &connInfo{
		secretID:    sec.ID,
		secretName:  sec.Label(),
		connectedAt: time.Now(),
	}
	if host, port, err := net.SplitHostPort(c.RemoteAddr().String()); err == nil {
		info.clientIP = host
		if p, err := strconv.Atoi(port); err == nil {
			info.clientPort = p
		}
	}
	info.network = networkKeyOf(info.clientIP)
	info.lastActive.Store(info.connectedAt.UnixNano())

	s.connsMu.Lock()
	limit := s.secretPolicyOf(sec).maxNetworks
	if s.conns == nil {
		s.conns = make(map[string]*secretConnSet)
	}
	set := s.conns[id]
	if set == nil {
		set = &secretConnSet{
			label:    sec.Label(),
			conns:    make(map[net.Conn]*connInfo),
			networks: make(map[string]int),
		}
		s.conns[id] = set
	} else if info.network != "" && limit > 0 &&
		set.networks[info.network] == 0 && len(set.networks) >= limit {
		d := s.refuseLocked(id, info.connectedAt, limit)
		s.connsMu.Unlock()
		return nil, func() {}, d
	}
	set.conns[c] = info
	if info.network != "" {
		set.networks[info.network]++
	}
	s.connsMu.Unlock()
	return info, func() {
		s.connsMu.Lock()
		if set := s.conns[id]; set != nil {
			if _, tracked := set.conns[c]; tracked {
				delete(set.conns, c)
				if n := set.networks[info.network]; n > 1 {
					set.networks[info.network] = n - 1
				} else if n == 1 {
					delete(set.networks, info.network)
				}
			}
			if len(set.conns) == 0 {
				delete(s.conns, id)
			}
		}
		s.connsMu.Unlock()
	}, denyInfo{}
}

func (s *Server) Sessions() []SessionInfo {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	out := make([]SessionInfo, 0, len(s.conns))
	for _, set := range s.conns {
		for _, info := range set.conns {
			si := SessionInfo{
				ID:          info.secretID,
				Name:        info.secretName,
				ClientIP:    info.clientIP,
				ClientPort:  info.clientPort,
				ConnectedAt: info.connectedAt,
				LastSeen:    time.Unix(0, info.lastActive.Load()),
			}
			if d := info.dest.Load(); d != nil {
				si.Destination = *d
			}
			out = append(out, si)
		}
	}
	return out
}

const maxNetworkAddrsPerSecret = 32

func networkKeyOf(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.WithZone("").Unmap()
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

func (s *Server) networkSnapshot() map[string][]string {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	out := make(map[string][]string, len(s.conns))
	for id, set := range s.conns {
		keys := make([]string, 0, len(set.networks))
		for key := range set.networks {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		out[id] = keys
	}
	return out
}

func (s *Server) secretPolicyOf(sec *Secret) secretPolicy {
	ptr := s.secrets.Load()
	if ptr == nil {
		return secretPolicy{}
	}
	id := secretIdentity(sec)
	for _, cur := range *ptr {
		if secretIdentity(cur) == id {
			return secretPolicy{active: true, maxNetworks: cur.MaxNetworks}
		}
	}
	return secretPolicy{}
}

func (s *Server) secretActive(sec *Secret) bool {
	return s.secretPolicyOf(sec).active
}

func (s *Server) closeRevokedConns(active []*Secret) {
	allowed := make(map[string]struct{}, len(active))
	for _, sec := range active {
		allowed[secretIdentity(sec)] = struct{}{}
	}

	type victim struct {
		label string
		conns []net.Conn
	}
	var victims []victim
	s.connsMu.Lock()
	for id, set := range s.conns {
		if _, ok := allowed[id]; ok {
			continue
		}
		v := victim{label: set.label, conns: make([]net.Conn, 0, len(set.conns))}
		for c := range set.conns {
			v.conns = append(v.conns, c)
		}
		victims = append(victims, v)
	}
	s.connsMu.Unlock()

	for _, v := range victims {
		for _, c := range v.conns {
			_ = c.Close()
		}
		log.Infof("MTProto: closed %d active connection(s) for revoked secret %q", len(v.conns), v.label)
	}
}

func (s *Server) pruneRefusals(active []*Secret) {
	allowed := make(map[string]struct{}, len(active))
	for _, sec := range active {
		allowed[secretIdentity(sec)] = struct{}{}
	}
	s.connsMu.Lock()
	for id := range s.refusals {
		if _, ok := allowed[id]; !ok {
			delete(s.refusals, id)
		}
	}
	s.connsMu.Unlock()
}

func (s *Server) enforceSecretLimits(active []*Secret) {
	limits := make(map[string]int, len(active))
	for _, sec := range active {
		if sec.MaxNetworks > 0 {
			limits[secretIdentity(sec)] = sec.MaxNetworks
		}
	}
	if len(limits) == 0 {
		return
	}

	type victim struct {
		label string
		limit int
		conns []net.Conn
	}
	var victims []victim

	s.connsMu.Lock()
	for id, set := range s.conns {
		limit, ok := limits[id]
		if !ok || len(set.networks) <= limit {
			continue
		}
		since := make(map[string]time.Time, len(set.networks))
		for _, info := range set.conns {
			if _, live := set.networks[info.network]; !live {
				continue
			}
			if t, seen := since[info.network]; !seen || info.connectedAt.Before(t) {
				since[info.network] = info.connectedAt
			}
		}
		keys := make([]string, 0, len(since))
		for k := range since {
			keys = append(keys, k)
		}
		if len(keys) <= limit {
			continue
		}
		slices.SortFunc(keys, func(a, b string) int {
			if since[a].Equal(since[b]) {
				return strings.Compare(a, b)
			}
			if since[a].After(since[b]) {
				return -1
			}
			return 1
		})
		drop := make(map[string]struct{}, len(keys)-limit)
		for _, k := range keys[:len(keys)-limit] {
			drop[k] = struct{}{}
			delete(set.networks, k)
		}
		v := victim{label: set.label, limit: limit}
		for c, info := range set.conns {
			if _, ok := drop[info.network]; ok {
				v.conns = append(v.conns, c)
			}
		}
		if len(v.conns) > 0 {
			victims = append(victims, v)
		}
	}
	s.connsMu.Unlock()

	for _, v := range victims {
		for _, c := range v.conns {
			_ = c.Close()
		}
		log.Infof("MTProto: secret %q is over its limit of %d network(s), closed %d connection(s)",
			v.label, v.limit, len(v.conns))
	}
}

func (s *Server) pruneStats(active []*Secret) {
	keep := make(map[string]struct{}, len(active))
	for _, sec := range active {
		key := sec.ID
		if key == "" {
			key = sec.Label()
		}
		keep[key] = struct{}{}
	}
	s.statsMu.Lock()
	for k := range s.stats {
		if _, ok := keep[k]; !ok {
			delete(s.stats, k)
		}
	}
	s.statsMu.Unlock()
}

func secretHosts(secrets []*Secret) string {
	if len(secrets) == 0 {
		return "none"
	}
	seen := make(map[string]struct{}, len(secrets))
	hosts := make([]string, 0, len(secrets))
	for _, sec := range secrets {
		if _, ok := seen[sec.Host]; ok {
			continue
		}
		seen[sec.Host] = struct{}{}
		hosts = append(hosts, sec.Host)
	}
	return strings.Join(hosts, ",")
}

func (s *Server) Stats() Stats {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()

	out := Stats{Enabled: running}
	if cfg := s.cfg.Load(); cfg != nil {
		out.Port = cfg.System.MTProto.Port
	}

	secsPtr := s.secrets.Load()
	if secsPtr == nil {
		return out
	}

	networks := s.networkSnapshot()
	allNetworks := make(map[string]struct{})

	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	for _, sec := range *secsPtr {
		key := sec.ID
		if key == "" {
			key = sec.Label()
		}
		ss := SecretStat{Name: sec.Label()}
		if keys, ok := networks[secretIdentity(sec)]; ok {
			ss.Networks = len(keys)
			for _, k := range keys {
				allNetworks[k] = struct{}{}
			}
			if len(keys) > maxNetworkAddrsPerSecret {
				keys = keys[:maxNetworkAddrsPerSecret]
			}
			ss.NetworkAddrs = keys
		}
		if st := s.stats[key]; st != nil {
			ss.Active = st.active.Load()
			ss.Total = st.total.Load()
			ss.BytesUp = st.up.Load()
			ss.BytesDown = st.down.Load()
		}
		out.Secrets = append(out.Secrets, ss)
		out.ActiveConnections += ss.Active
		out.TotalConnections += ss.Total
		out.BytesUp += ss.BytesUp
		out.BytesDown += ss.BytesDown
	}
	out.Networks = len(allNetworks)
	return out
}

func NewServer(cfg *config.Config) *Server {
	s := &Server{
		bufPool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, relayBufSize)
				return &buf
			},
		},
	}
	s.cfg.Store(cfg)
	return s
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked()
}

func buildSecrets(cfg *config.Config) ([]*Secret, error) {
	mtCfg := &cfg.System.MTProto

	var secrets []*Secret
	invalid := 0
	for _, entry := range mtCfg.EffectiveSecrets() {
		sec, err := ParseSecret(entry.Secret)
		if err != nil {
			invalid++
			log.Warnf("MTProto: skipping invalid secret %q: %v", entry.Name, err)
			continue
		}
		sec.ID = entry.ID
		sec.Name = entry.Name
		sec.MaxNetworks = entry.MaxNetworks
		secrets = append(secrets, sec)
	}
	if len(secrets) > 0 {
		return secrets, nil
	}

	if invalid > 0 {
		return nil, fmt.Errorf("MTProto: %d configured secret(s), none valid", invalid)
	}

	if len(mtCfg.Secrets) > 0 {
		return nil, nil
	}

	if mtCfg.FakeSNI == "" {
		return nil, fmt.Errorf("MTProto: at least one secret or fake_sni must be configured")
	}
	sec, err := GenerateSecret(mtCfg.FakeSNI)
	if err != nil {
		return nil, fmt.Errorf("MTProto generate secret: %w", err)
	}
	entry := config.MTProtoSecret{ID: uuid.NewString(), Name: "default", Secret: sec.Hex(), Enabled: true}
	sec.ID = entry.ID
	sec.Name = entry.Name
	mtCfg.Secrets = append(mtCfg.Secrets, entry)
	if cfg.ConfigPath != "" {
		if err := cfg.SaveToFile(cfg.ConfigPath); err != nil {
			log.Warnf("MTProto: failed to persist generated secret: %v", err)
		} else {
			log.Infof("MTProto secret generated and saved")
		}
	} else {
		log.Infof("MTProto secret generated")
	}
	return []*Secret{sec}, nil
}

func (s *Server) startLocked() error {
	cfg := s.cfg.Load()
	mtCfg := &cfg.System.MTProto
	if !mtCfg.Enabled {
		log.Infof("MTProto proxy disabled")
		return nil
	}

	secrets, err := buildSecrets(cfg)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(mtCfg.BindAddress, strconv.Itoa(mtCfg.Port))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("MTProto listen: %w", err)
	}
	s.listener = ln
	s.secrets.Store(&secrets)
	s.closeRevokedConns(secrets)
	s.enforceSecretLimits(secrets)
	s.pruneRefusals(secrets)
	s.pruneStats(secrets)
	s.ctx, s.cancel = context.WithCancel(context.Background())

	log.Infof("MTProto proxy listening on %s (SNI: %s, secrets: %d)", addr, secretHosts(secrets), len(secrets))

	if mode := mtCfg.UpstreamMode; mode == "ws" || mode == "auto" || mode == "" {
		wsResetState()
		tcpResetState()
		pool := newWSPool(MTProtoUpstream{
			WSEndpointHost: mtCfg.WSEndpointHost,
			WSCustomDomain: mtCfg.WSCustomDomain,
			CFProxyEnabled: mtCfg.CFProxyEnabled,
		}, selfDialMark(), wsPoolDefaultSize)
		pool.warmup(wsWarmupDCs(mtCfg))
		s.wsPool.Store(pool)
		if len(workerDomains(mtCfg)) > 0 {
			s.workerPool.Store(newCFWorkerPool(selfDialMark()))
		} else {
			s.workerPool.Store(nil)
		}
	} else {
		s.wsPool.Store(nil)
		s.workerPool.Store(nil)
	}

	s.running = true
	go s.acceptLoop(ln)
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

func (s *Server) stopLocked() error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if pool := s.wsPool.Swap(nil); pool != nil {
		pool.close()
	}
	s.workerPool.Swap(nil).close()
	var err error
	if s.listener != nil {
		err = s.listener.Close()
		s.listener = nil
	}
	s.running = false
	return err
}

func (s *Server) reloadSecretsLocked(cfg *config.Config) {
	secrets, err := buildSecrets(cfg)
	if err != nil {
		log.Errorf("MTProto secrets reload failed: %v (keeping previous secrets)", err)
		return
	}
	s.secrets.Store(&secrets)
	s.closeRevokedConns(secrets)
	s.enforceSecretLimits(secrets)
	s.pruneRefusals(secrets)
	s.pruneStats(secrets)
	log.Infof("MTProto secrets reloaded live (%d active) without restart", len(secrets))
}

func (s *Server) UpdateConfig(newCfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old := s.cfg.Load()
	s.cfg.Store(newCfg)

	if old != nil && !mtprotoNeedsRestart(old, newCfg) {
		if s.running && mtprotoSecretsChanged(old.System.MTProto, newCfg.System.MTProto) {
			s.reloadSecretsLocked(newCfg)
		}
		return
	}

	wasEnabled := old != nil && old.System.MTProto.Enabled
	if s.running {
		_ = s.stopLocked()
	}

	if newCfg.System.MTProto.Enabled {
		if err := s.startLocked(); err != nil {
			log.Errorf("MTProto reload failed: %v (proxy stopped; fix in Settings)", err)
			s.closeRevokedConns(nil)
		} else {
			log.Infof("MTProto reloaded with updated configuration")
		}
	} else if wasEnabled {
		log.Infof("MTProto proxy stopped (disabled in configuration)")
		s.closeRevokedConns(nil)
	}
}

func mtprotoNeedsRestart(old, newCfg *config.Config) bool {
	o := old.System.MTProto
	n := newCfg.System.MTProto
	if o.Enabled != n.Enabled ||
		o.Port != n.Port ||
		o.BindAddress != n.BindAddress ||
		o.FakeSNI != n.FakeSNI ||
		o.UpstreamMode != n.UpstreamMode ||
		o.WSEndpointHost != n.WSEndpointHost ||
		o.WSCustomDomain != n.WSCustomDomain ||
		o.CFProxyEnabled != n.CFProxyEnabled ||
		o.CFProxyURL != n.CFProxyURL {
		return true
	}
	return old.Queue.Mark != newCfg.Queue.Mark
}

func mtprotoSecretsChanged(o, n config.MTProtoConfig) bool {
	if len(o.Secrets) != len(n.Secrets) {
		return true
	}
	for i := range o.Secrets {
		if o.Secrets[i] != n.Secrets[i] {
			return true
		}
	}
	return false
}

func mtprotoConnMeta(user string) string {
	u := strings.Map(func(r rune) rune {
		if r == ',' || r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, user)
	u = strings.TrimSpace(u)
	if u == "" {
		return "mtproto"
	}
	return "mtproto:" + u
}

func (s *Server) GetSecret() string {
	if ptr := s.secrets.Load(); ptr != nil && len(*ptr) > 0 {
		return (*ptr)[0].Hex()
	}
	return ""
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Tracef("MTProto accept: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		limit := int64(mtprotoMaxConnections(s.cfg.Load()))
		if s.active.Add(1) > limit {
			s.active.Add(-1)
			log.Tracef("MTProto connection limit reached (%d)", limit)
			conn.Close()
			continue
		}

		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
			_ = tc.SetReadBuffer(256 * 1024)
			_ = tc.SetWriteBuffer(256 * 1024)
			setTCPUserTimeout(tc, mtprotoTCPUserTimeout(s.cfg.Load()))
		}

		go func(c net.Conn) {
			defer func() {
				c.Close()
				s.active.Add(-1)
			}()
			s.handleConn(c)
		}(conn)
	}
}

func (s *Server) handleConn(raw net.Conn) {
	clientAddr := raw.RemoteAddr().String()
	id := nextConnID()
	tag := tg(id)
	log.Infof("%s proxy new connection from %s", tag, clientAddr)

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("%s proxy panic from %s: %v", tag, clientAddr, r)
		}
	}()

	secretsPtr := s.secrets.Load()
	if secretsPtr == nil || len(*secretsPtr) == 0 {
		return
	}
	secrets := *secretsPtr
	cfg := s.cfg.Load()

	if err := raw.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return
	}

	tlsConn, secret, err := AcceptFakeTLSMulti(raw, secrets)
	if err != nil {
		log.Debugf("%s proxy fake-TLS failed from %s: %v", tag, clientAddr, err)
		var vErr *FakeTLSVerifyError
		if errors.As(err, &vErr) && cfg.System.MTProto.FakeSNI != "" {
			proxyToMaskingDomain(raw, vErr.Initial, cfg.System.MTProto.FakeSNI, selfDialMark())
		}
		return
	}
	log.Debugf("%s proxy fake-TLS handshake OK from %s (secret=%s)", tag, clientAddr, secret.Label())

	s.serveClient(raw, tlsConn, secret, clientAddr, id, tag, "TCP", secret.Host)
}

func (s *Server) serveClient(raw net.Conn, plain net.Conn, secret *Secret, clientAddr, id, tag, proto, sni string) {
	cfg := s.cfg.Load()
	user := secret.Label()

	info, untrack, deny := s.trackConn(secret, raw)
	if deny.limit > 0 {
		if deny.log {
			log.Infof("%s proxy secret %q is at its limit of %d network(s), refusing %s (%d refused so far)",
				tag, user, deny.limit, clientAddr, deny.total)
		}
		return
	}
	defer untrack()
	if !s.secretActive(secret) {
		log.Infof("%s proxy secret %q revoked, dropping connection from %s", tag, user, clientAddr)
		return
	}

	result, err := AcceptObfuscated(plain, secret)
	if err != nil {
		log.Tracef("%s proxy obfuscated2 failed from %s: %v", tag, clientAddr, err)
		return
	}
	log.Debugf("%s proxy client [%s] from %s wants DC %d proto=0x%08x", tag, user, clientAddr, result.DC, result.ProtoTag)
	_ = raw.SetDeadline(time.Time{})

	dcConn, dial, err := dialObfuscatedDC(&cfg.System.MTProto, cfg.Queue, result.DC, result.ProtoTag, &dialPools{ws: s.wsPool.Load(), worker: s.workerPool.Load()}, id, dialTarget{})
	if err != nil {
		if shouldLogDialError(result.DC) {
			log.Errorf("%s proxy dial DC %d failed: %v", tag, result.DC, err)
		} else {
			log.Debugf("%s proxy dial DC %d failed (suppressed): %v", tag, result.DC, err)
		}
		return
	}
	defer dcConn.Close()

	log.Infof("%s proxy relay [%s] %s <-> DC%d via %s", tag, user, clientAddr, result.DC, dial.transport)

	dcAddr := fmt.Sprintf("DC%d", result.DC)
	if ra := dcConn.RemoteAddr(); ra != nil {
		dcAddr = ra.String()
	}
	info.dest.Store(&dcAddr)
	log.LogConnectionStr(proto, "", sni, clientAddr, "", dcAddr, "", "", mtprotoConnMeta(user))

	st := s.secretStat(secret)
	st.active.Add(1)
	st.total.Add(1)
	defer st.active.Add(-1)

	splitter := newSplitterFor(dcConn, dial, result.ProtoTag)
	route := dial.transport
	if r := dial.plan.routeName(); r != "" && r != route {
		route += " " + r
	}
	up, down := s.relay(result, dcConn, splitter, &info.lastActive, dial,
		fmt.Sprintf("%s [%s] %s<->DC%d via %s", tag, user, clientAddr, result.DC, route))
	st.up.Add(up)
	st.down.Add(down)
}

func (s *Server) relay(result *ClientHandshakeResult, dc io.ReadWriteCloser, splitter *msgSplitter, lastActive *atomic.Int64, dial dialInfo, label string) (up, down int64) {
	return relayConns(result.Conn, dc, relayOpts{
		splitter:       splitter,
		label:          label,
		bufPool:        &s.bufPool,
		idle:           mtprotoIdleTimeout(s.cfg.Load()),
		lastActive:     lastActive,
		onStall:        stallReporter(dial),
		scan:           newDCFrameScanner(result.ProtoTag),
		onTransportErr: transportErrHandler(dial, result.DC, label),
	})
}

// transportErrHandler decides what to do with a transport error coming back from
// the data center, and says so in the log either way.
//
// -444 means the session reached a data center that is not the one the client
// asked for. The client repeats its DC inside the RSA-encrypted p_q_inner_data,
// which b4 can neither read nor correct, so the mismatch is always b4's choice of
// route and never the user's configuration - and both Telegram clients answer a
// single -444 by telling the user the proxy is misconfigured and switching it
// off. Swallow it, rank the route down, and let the client redial onto another
// one. Every other code is between the client and Telegram, so it is passed on.
func transportErrHandler(dial dialInfo, clientDC int, label string) func(int32) bool {
	return func(code int32) bool {
		if code != tgErrInvalidDC {
			log.Warnf("%s upstream transport error %d (%s), relayed to the client", label, code, transportErrName(code))
			return false
		}
		log.Warnf("%s upstream answered -444 (invalid DC) for a DC %d session: the route does not end at the data center the client asked for, cutting it and ranking the route down",
			label, clientDC)
		demoteRejectedRoute(dial, clientDC)
		return true
	}
}

// demoteRejectedRoute records that a route landed a session on the wrong data
// center, so the next dial for that DC prefers something else. Nothing used to
// score a route on what happened after the dial unless it was a Worker, so a
// route that rejected every session kept its place at the front of the list.
func demoteRejectedRoute(dial dialInfo, clientDC int) {
	p := dial.plan
	switch {
	case p.isWorker && p.sni != "":
		workerRecordStall(p.sni)
	case p.cfBase != "":
		cfBalancerInst.penalize(p.cfBase, cfProxyTimeoutCooldown)
	case p.native && p.dialHost != "" && p.sni != "":
		wsEndpointFailed(p.dialHost, p.sni)
		wsRecordFailure(clientDC, false)
	case p.kind == transportTCP && p.addr != "":
		tcpRecordFailure(p.addr)
	}
}

// A relay whose upstream has gone mute while the client is still asking is not
// idle - it is dead, and nothing in the connection says so: the WebSocket stays
// open, so the reader blocks and the client is left to wait out its own receive
// timeout. Report it so the caller can rank that route down, and cut the relay
// rather than hold it for the full idle timeout.
//
// The other shape of the same failure is a route that carries the request and
// answers nothing at all, which matters because the transport list only guards
// dial failures: the Worker accepts the WebSocket, so nothing behind it is ever
// tried. Zero bytes back is evidence only when the upstream itself ended the
// relay. When the client ended it, the wait has to have been long enough for an
// answer to be due, and that is already what the in-flight watchdog and the
// went-quiet check below measure. Scoring every relay the client closed first
// blamed the route for an ordinary request and response: on the fail-open path,
// the relays that did answer took 269-361 ms, while ten closed by the client at
// 69-103 ms were each recorded as a stall and kept the Worker in cooldown.
const relayStallClose = 8 * time.Second

// relayOpts is everything the relay needs beyond the two ends it joins.
type relayOpts struct {
	splitter   *msgSplitter
	label      string
	bufPool    *sync.Pool
	idle       time.Duration
	lastActive *atomic.Int64
	onStall    func()
	// scan reads the transport framing coming back from the data center. Without
	// it a four-byte transport error was relayed to the client untouched, and the
	// client answered -444 by telling the user the proxy is misconfigured and
	// switching it off.
	scan *dcFrameScanner
	// onTransportErr is called with the code of every transport error seen on the
	// way back. It returns true when the error is b4's own fault and must not
	// reach the client, in which case the relay is cut instead.
	onTransportErr func(int32) bool
}

func relayConns(client, dc io.ReadWriteCloser, o relayOpts) (int64, int64) {
	splitter, label, bufPool, idle, lastActive, onStall := o.splitter, o.label, o.bufPool, o.idle, o.lastActive, o.onStall
	type relayEnd struct {
		dir string
		err error
	}
	endCh := make(chan relayEnd, 2)
	start := time.Now()
	var upBytes, downBytes atomic.Int64
	var lastDown atomic.Int64
	var upSinceDown atomic.Int64
	var stallReported atomic.Bool
	lastDown.Store(start.UnixNano())
	reportStall := func() {
		if onStall != nil && !stallReported.Swap(true) {
			onStall()
		}
	}
	if lastActive == nil {
		lastActive = new(atomic.Int64)
	}
	lastActive.Store(start.UnixNano())

	cp := func(dst io.Writer, src io.Reader, dir string, counter *atomic.Int64) {
		bufPtr := bufPool.Get().(*[]byte)
		defer bufPool.Put(bufPtr)
		buf := *bufPtr
		var total int64
		var err error
		up := dir == "client->DC"
		for {
			var n int
			n, err = src.Read(buf)
			if n > 0 {
				lastActive.Store(time.Now().UnixNano())
				if up {
					upSinceDown.Add(int64(n))
				} else {
					lastDown.Store(time.Now().UnixNano())
					upSinceDown.Store(0)
				}
				out := buf[:n]
				var rest []byte
				var code int32
				var rejected bool
				if !up && o.scan != nil {
					out, rest, code, rejected = o.scan.feed(out)
				}
				if rejected && o.onTransportErr != nil && o.onTransportErr(code) {
					// b4 chose the route that produced this, so the client must not
					// see it: forward what came before and end the session there.
					rest = nil
				} else if rejected {
					// Between the client and Telegram: put the frame back where it
					// was and carry on.
					out = append(append(append([]byte(nil), out...), transportErrFrame(o.scan.proto, code)...), rest...)
					rest = nil
					rejected = false
				}
				if len(out) > 0 {
					if _, werr := dst.Write(out); werr != nil {
						err = werr
					} else {
						total += int64(len(out))
					}
				}
				if rejected && err == nil {
					err = errUpstreamRejected
				}
			}
			if err != nil {
				break
			}
		}
		counter.Store(total)
		log.Debugf("%s %s: %d bytes, err=%v", label, dir, total, err)
		endCh <- relayEnd{dir: dir, err: err}
	}

	cpSplit := func(dst io.Writer, src io.Reader, dir string, counter *atomic.Int64) {
		bufPtr := bufPool.Get().(*[]byte)
		defer bufPool.Put(bufPtr)
		buf := *bufPtr
		var total int64
		var err error
		for {
			var n int
			n, err = src.Read(buf)
			if n > 0 {
				lastActive.Store(time.Now().UnixNano())
				upSinceDown.Add(int64(n))
				for _, pkt := range splitter.split(buf[:n]) {
					if _, werr := dst.Write(pkt); werr != nil {
						err = werr
						break
					}
					total += int64(len(pkt))
				}
			}
			if err != nil {
				if tail := splitter.flush(); len(tail) > 0 {
					_, _ = dst.Write(tail)
				}
				break
			}
		}
		counter.Store(total)
		log.Debugf("%s %s: %d bytes, err=%v", label, dir, total, err)
		endCh <- relayEnd{dir: dir, err: err}
	}

	if splitter != nil {
		go cpSplit(dc, client, "client->DC", &upBytes)
	} else {
		go cp(dc, client, "client->DC", &upBytes)
	}
	go cp(client, dc, "DC->client", &downBytes)

	done := make(chan struct{})
	if onStall != nil {
		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if upSinceDown.Load() == 0 {
						continue
					}
					silent := time.Since(time.Unix(0, lastDown.Load()))
					if silent >= relayStallClose {
						log.Infof("%s upstream silent for %s with %d B awaiting an answer, cutting the relay",
							label, silent.Round(time.Second), upSinceDown.Load())
						reportStall()
						_ = client.Close()
						_ = dc.Close()
						return
					}
				}
			}
		}()
	}
	if idle > 0 {
		go func() {
			interval := idle / 4
			if interval < 100*time.Millisecond {
				interval = 100 * time.Millisecond
			}
			if interval > 15*time.Second {
				interval = 15 * time.Second
			}
			t := time.NewTicker(interval)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if time.Since(time.Unix(0, lastActive.Load())) >= idle {
						log.Infof("%s idle for %s, reaping", label, idle)
						_ = client.Close()
						_ = dc.Close()
						return
					}
				}
			}
		}()
	}

	first := <-endCh
	_ = client.Close()
	_ = dc.Close()
	<-endCh
	close(done)

	up, down := upBytes.Load(), downBytes.Load()
	staleUpstream := first.dir == "DC->client" && down == 0
	neverAnswered := staleUpstream && up > 0
	wentQuiet := upSinceDown.Load() > 0 &&
		time.Since(time.Unix(0, lastDown.Load())) >= relayStallClose
	if neverAnswered || wentQuiet {
		reportStall()
	}
	stale := ""
	if staleUpstream {
		stale = " stale-upstream?"
	}
	log.Infof("%s closed: first=%s err=%v up=%d down=%d in %dms%s", label, first.dir, first.err, up, down, time.Since(start).Milliseconds(), stale)
	return up, down
}
