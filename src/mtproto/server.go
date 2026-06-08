package mtproto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	maxConnections = 512
	relayBufSize   = 65536
)

type Server struct {
	connSem chan struct{}
	bufPool sync.Pool
	active  atomic.Int64

	cfg    atomic.Pointer[config.Config]
	secret atomic.Pointer[Secret]
	wsPool atomic.Pointer[wsPool]

	mu       sync.Mutex
	running  bool
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewServer(cfg *config.Config) *Server {
	s := &Server{
		connSem: make(chan struct{}, maxConnections),
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

func (s *Server) startLocked() error {
	cfg := s.cfg.Load()
	mtCfg := &cfg.System.MTProto
	if !mtCfg.Enabled {
		log.Infof("MTProto proxy disabled")
		return nil
	}

	var sec *Secret
	if mtCfg.Secret != "" {
		var err error
		sec, err = ParseSecret(mtCfg.Secret)
		if err != nil {
			return fmt.Errorf("MTProto parse secret: %w", err)
		}
	} else if mtCfg.FakeSNI != "" {
		var err error
		sec, err = GenerateSecret(mtCfg.FakeSNI)
		if err != nil {
			return fmt.Errorf("MTProto generate secret: %w", err)
		}
		mtCfg.Secret = sec.Hex()
		if cfg.ConfigPath != "" {
			if err := cfg.SaveToFile(cfg.ConfigPath); err != nil {
				log.Warnf("MTProto: failed to persist generated secret: %v", err)
			}
		}
		log.Infof("MTProto secret generated and saved")
	} else {
		return fmt.Errorf("MTProto: either secret or fake_sni must be configured")
	}

	addr := net.JoinHostPort(mtCfg.BindAddress, strconv.Itoa(mtCfg.Port))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("MTProto listen: %w", err)
	}
	s.listener = ln
	s.secret.Store(sec)
	s.ctx, s.cancel = context.WithCancel(context.Background())

	log.Infof("MTProto proxy listening on %s (SNI: %s)", addr, sec.Host)

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

func (s *Server) UpdateConfig(newCfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old := s.cfg.Load()
	s.cfg.Store(newCfg)

	if old != nil && !mtprotoNeedsRestart(old, newCfg) {
		return
	}

	wasEnabled := old != nil && old.System.MTProto.Enabled
	if s.running {
		_ = s.stopLocked()
	}

	if newCfg.System.MTProto.Enabled {
		if err := s.startLocked(); err != nil {
			log.Errorf("MTProto reload failed: %v (proxy stopped; fix in Settings)", err)
		} else {
			log.Infof("MTProto reloaded with updated configuration")
		}
	} else if wasEnabled {
		log.Infof("MTProto proxy stopped (disabled in configuration)")
	}
}

func mtprotoNeedsRestart(old, newCfg *config.Config) bool {
	o := old.System.MTProto
	n := newCfg.System.MTProto
	if o.Enabled != n.Enabled ||
		o.Port != n.Port ||
		o.BindAddress != n.BindAddress ||
		o.Secret != n.Secret ||
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

func (s *Server) GetSecret() string {
	if sec := s.secret.Load(); sec != nil {
		return sec.Hex()
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

		select {
		case s.connSem <- struct{}{}:
		default:
			log.Tracef("MTProto connection limit reached")
			conn.Close()
			continue
		}

		// match tg-ws-proxy: tune accepted client socket to 256KB buffers + nodelay
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
			_ = tc.SetReadBuffer(256 * 1024)
			_ = tc.SetWriteBuffer(256 * 1024)
		}

		s.active.Add(1)
		go func(c net.Conn) {
			defer func() {
				c.Close()
				<-s.connSem
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

	secret := s.secret.Load()
	if secret == nil {
		return
	}
	cfg := s.cfg.Load()

	if err := raw.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return
	}

	tlsConn, err := AcceptFakeTLS(raw, secret)
	if err != nil {
		log.Debugf("%s proxy fake-TLS failed from %s: %v", tag, clientAddr, err)
		var vErr *FakeTLSVerifyError
		if errors.As(err, &vErr) && cfg.System.MTProto.FakeSNI != "" {
			proxyToMaskingDomain(raw, vErr.Initial, cfg.System.MTProto.FakeSNI, cfg.Queue.Mark)
		}
		return
	}
	log.Debugf("%s proxy fake-TLS handshake OK from %s", tag, clientAddr)

	result, err := AcceptObfuscated(tlsConn, secret)
	if err != nil {
		log.Tracef("%s proxy obfuscated2 failed from %s: %v", tag, clientAddr, err)
		return
	}
	log.Debugf("%s proxy client from %s wants DC %d proto=0x%08x", tag, clientAddr, result.DC, result.ProtoTag)
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

	log.Infof("%s proxy relay %s <-> DC%d via %s", tag, clientAddr, result.DC, transport)

	var splitter *msgSplitter
	if _, ok := dcConn.Conn.(*wsConn); ok {
		splitter = newMsgSplitter(result.ProtoTag)
	}
	s.relay(result.Conn, dcConn, splitter, fmt.Sprintf("%s %s<->DC%d via %s", tag, clientAddr, result.DC, transport))
}

func (s *Server) relay(client, dc io.ReadWriteCloser, splitter *msgSplitter, label string) {
	relayConns(client, dc, splitter, label, &s.bufPool)
}

func relayConns(client, dc io.ReadWriteCloser, splitter *msgSplitter, label string, bufPool *sync.Pool) {
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
}
