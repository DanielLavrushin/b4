package mtproto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
)

func mtprotoMaxConnections(cfg *config.Config) int {
	if n := cfg.System.MTProto.MaxConnections; n > 0 {
		return n
	}
	return defaultMaxConnections
}

type Server struct {
	bufPool sync.Pool
	active  atomic.Int64

	cfg     atomic.Pointer[config.Config]
	secrets atomic.Pointer[[]*Secret]
	wsPool  atomic.Pointer[wsPool]

	statsMu sync.Mutex
	stats   map[string]*secretStat

	connsMu sync.Mutex
	conns   map[string]*secretConnSet

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
	Name      string
	Active    int64
	Total     int64
	BytesUp   int64
	BytesDown int64
}

type Stats struct {
	Enabled           bool
	Port              int
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

type secretConnSet struct {
	label string
	conns map[net.Conn]struct{}
}

func secretIdentity(sec *Secret) string {
	key := sec.ID
	if key == "" {
		key = sec.Label()
	}
	return key + "|" + sec.Hex()
}

func (s *Server) trackConn(sec *Secret, c net.Conn) func() {
	id := secretIdentity(sec)
	s.connsMu.Lock()
	if s.conns == nil {
		s.conns = make(map[string]*secretConnSet)
	}
	set := s.conns[id]
	if set == nil {
		set = &secretConnSet{label: sec.Label(), conns: make(map[net.Conn]struct{})}
		s.conns[id] = set
	}
	set.conns[c] = struct{}{}
	s.connsMu.Unlock()
	return func() {
		s.connsMu.Lock()
		if set := s.conns[id]; set != nil {
			delete(set.conns, c)
			if len(set.conns) == 0 {
				delete(s.conns, id)
			}
		}
		s.connsMu.Unlock()
	}
}

func (s *Server) secretActive(sec *Secret) bool {
	ptr := s.secrets.Load()
	if ptr == nil {
		return false
	}
	id := secretIdentity(sec)
	for _, cur := range *ptr {
		if secretIdentity(cur) == id {
			return true
		}
	}
	return false
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

	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	for _, sec := range *secsPtr {
		key := sec.ID
		if key == "" {
			key = sec.Label()
		}
		ss := SecretStat{Name: sec.Label()}
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
		secrets = append(secrets, sec)
	}
	if len(secrets) > 0 {
		return secrets, nil
	}

	if invalid > 0 {
		return nil, fmt.Errorf("MTProto: %d configured secret(s), none valid", invalid)
	}

	if len(mtCfg.Secrets) > 0 || strings.TrimSpace(mtCfg.Secret) != "" {
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
	mtCfg.Secret = sec.Hex()
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
	s.pruneStats(secrets)
	s.ctx, s.cancel = context.WithCancel(context.Background())

	log.Infof("MTProto proxy listening on %s (SNI: %s, secrets: %d)", addr, secretHosts(secrets), len(secrets))

	if mode := mtCfg.UpstreamMode; mode == "ws" || mode == "auto" || mode == "" {
		wsResetState()
		tcpResetState()
		pool := newWSPool(MTProtoUpstream{
			WSEndpointHost: mtCfg.WSEndpointHost,
			WSCustomDomain: mtCfg.WSCustomDomain,
		}, cfg.Queue.Mark, wsPoolDefaultSize)
		pool.warmup([]int{2, 4})
		s.wsPool.Store(pool)
	} else {
		s.wsPool.Store(nil)
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
	if o.Secret != n.Secret || len(o.Secrets) != len(n.Secrets) {
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
			log.Errorf("MTProto accept: %v", err)
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
			proxyToMaskingDomain(raw, vErr.Initial, cfg.System.MTProto.FakeSNI, cfg.Queue.Mark)
		}
		return
	}
	user := secret.Label()
	log.Debugf("%s proxy fake-TLS handshake OK from %s (secret=%s)", tag, clientAddr, user)

	untrack := s.trackConn(secret, raw)
	defer untrack()
	if !s.secretActive(secret) {
		log.Infof("%s proxy secret %q revoked, dropping connection from %s", tag, user, clientAddr)
		return
	}

	result, err := AcceptObfuscated(tlsConn, secret)
	if err != nil {
		log.Tracef("%s proxy obfuscated2 failed from %s: %v", tag, clientAddr, err)
		return
	}
	log.Debugf("%s proxy client [%s] from %s wants DC %d proto=0x%08x", tag, user, clientAddr, result.DC, result.ProtoTag)
	_ = raw.SetDeadline(time.Time{})

	dcConn, transport, err := DialObfuscatedDCWithPool(&cfg.System.MTProto, cfg.Queue, result.DC, result.ProtoTag, s.wsPool.Load(), id)
	if err != nil {
		if shouldLogDialError(result.DC) {
			log.Errorf("%s proxy dial DC %d failed: %v", tag, result.DC, err)
		} else {
			log.Debugf("%s proxy dial DC %d failed (suppressed): %v", tag, result.DC, err)
		}
		return
	}
	defer dcConn.Close()

	log.Infof("%s proxy relay [%s] %s <-> DC%d via %s", tag, user, clientAddr, result.DC, transport)

	dcAddr := fmt.Sprintf("DC%d", result.DC)
	if ra := dcConn.RemoteAddr(); ra != nil {
		dcAddr = ra.String()
	}
	log.LogConnectionStr("TCP", "", secret.Host, clientAddr, "", dcAddr, "", "", mtprotoConnMeta(user))

	st := s.secretStat(secret)
	st.active.Add(1)
	st.total.Add(1)
	defer st.active.Add(-1)

	var splitter *msgSplitter
	if _, ok := dcConn.Conn.(*wsConn); ok {
		splitter = newMsgSplitter(result.ProtoTag)
	}
	up, down := s.relay(result.Conn, dcConn, splitter, fmt.Sprintf("%s [%s] %s<->DC%d via %s", tag, user, clientAddr, result.DC, transport))
	st.up.Add(up)
	st.down.Add(down)
}

func (s *Server) relay(client, dc io.ReadWriteCloser, splitter *msgSplitter, label string) (up, down int64) {
	return relayConns(client, dc, splitter, label, &s.bufPool)
}

func relayConns(client, dc io.ReadWriteCloser, splitter *msgSplitter, label string, bufPool *sync.Pool) (int64, int64) {
	type relayEnd struct {
		dir string
		err error
	}
	endCh := make(chan relayEnd, 2)
	start := time.Now()
	var upBytes, downBytes atomic.Int64

	cp := func(dst io.Writer, src io.Reader, dir string, counter *atomic.Int64) {
		bufPtr := bufPool.Get().(*[]byte)
		defer bufPool.Put(bufPtr)
		buf := *bufPtr
		var total int64
		var err error
		for {
			var n int
			n, err = src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					err = werr
				} else {
					total += int64(n)
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

	first := <-endCh
	_ = client.Close()
	_ = dc.Close()
	<-endCh

	up, down := upBytes.Load(), downBytes.Load()
	stale := ""
	if first.dir == "DC->client" && down == 0 {
		stale = " stale-upstream?"
	}
	log.Infof("%s closed: first=%s err=%v up=%d down=%d in %dms%s", label, first.dir, first.err, up, down, time.Since(start).Milliseconds(), stale)
	return up, down
}
