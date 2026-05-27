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
	cfg      *config.Config
	secret   *Secret
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	active   atomic.Int64
	connSem  chan struct{}
	bufPool  sync.Pool
	wsPool   *wsPool
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:     cfg,
		connSem: make(chan struct{}, maxConnections),
		bufPool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, relayBufSize)
				return &buf
			},
		},
	}
}

func (s *Server) Start() error {
	mtCfg := &s.cfg.System.MTProto
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
		if s.cfg.ConfigPath != "" {
			if err := s.cfg.SaveToFile(s.cfg.ConfigPath); err != nil {
				log.Warnf("MTProto: failed to persist generated secret: %v", err)
			}
		}
		log.Infof("MTProto secret generated and saved")
	} else {
		return fmt.Errorf("MTProto: either secret or fake_sni must be configured")
	}
	s.secret = sec

	addr := net.JoinHostPort(mtCfg.BindAddress, strconv.Itoa(mtCfg.Port))
	s.ctx, s.cancel = context.WithCancel(context.Background())

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("MTProto listen: %w", err)
	}
	s.listener = ln

	log.Infof("MTProto proxy listening on %s (SNI: %s)", addr, sec.Host)

	// pre-warm WS pool only when WS upstream is actually selectable
	if mode := mtCfg.UpstreamMode; mode == "ws" || mode == "auto" || mode == "" {
		wsResetState()
		s.wsPool = newWSPool(MTProtoUpstream{
			WSEndpointHost: mtCfg.WSEndpointHost,
			WSCustomDomain: mtCfg.WSCustomDomain,
		}, s.cfg.Queue.Mark, wsPoolDefaultSize)
		s.wsPool.warmup([]int{1, 2, 3, 4, 5})

		// CF-proxy auto-refresh: matches tg-ws-proxy. Built-in defaults are already
		// loaded in cfBalancerInst; this just keeps the pool fresh.
		if mtCfg.CFProxyEnabled {
			go runCFProxyRefreshLoop(s.ctx, mtCfg.CFProxyURL)
		}
	}

	go s.acceptLoop()
	return nil
}

func (s *Server) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.wsPool != nil {
		s.wsPool.close()
		s.wsPool = nil
	}
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) GetSecret() string {
	if s.secret != nil {
		return s.secret.Hex()
	}
	return ""
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
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
	log.Infof("MTProto new connection from %s", clientAddr)

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("MTProto panic from %s: %v", clientAddr, r)
		}
	}()

	if err := raw.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return
	}

	tlsConn, err := AcceptFakeTLS(raw, s.secret)
	if err != nil {
		log.Debugf("MTProto fake-TLS failed from %s: %v", clientAddr, err)
		var vErr *FakeTLSVerifyError
		if errors.As(err, &vErr) && s.cfg.System.MTProto.FakeSNI != "" {
			proxyToMaskingDomain(raw, vErr.Initial, s.cfg.System.MTProto.FakeSNI, s.cfg.Queue.Mark)
		}
		return
	}
	log.Debugf("MTProto fake-TLS handshake OK from %s", clientAddr)

	result, err := AcceptObfuscated(tlsConn, s.secret)
	if err != nil {
		log.Tracef("MTProto obfuscated2 failed from %s: %v", clientAddr, err)
		return
	}
	log.Debugf("MTProto client from %s wants DC %d proto=0x%08x", clientAddr, result.DC, result.ProtoTag)
	_ = raw.SetDeadline(time.Time{})

	dcConn, transport, err := DialObfuscatedDCWithPool(&s.cfg.System.MTProto, s.cfg.Queue, result.DC, result.ProtoTag, s.wsPool)
	if err != nil {
		// throttle ERROR-level spam from a permanently-broken DC; full detail still goes to Debug
		if shouldLogDialError(result.DC) {
			log.Errorf("MTProto dial DC %d: %v", result.DC, err)
		} else {
			log.Debugf("MTProto dial DC %d (suppressed): %v", result.DC, err)
		}
		return
	}
	defer dcConn.Close()

	log.Infof("MTProto relay: %s <-> DC%d (%s)", clientAddr, result.DC, transport)

	var splitter *msgSplitter
	if _, ok := dcConn.Conn.(*wsConn); ok {
		splitter = newMsgSplitter(result.ProtoTag)
	}
	s.relay(result.Conn, dcConn, splitter, fmt.Sprintf("%s<->DC%d", clientAddr, result.DC))
}

func (s *Server) relay(client, dc io.ReadWriteCloser, splitter *msgSplitter, label string) {
	errCh := make(chan error, 2)
	start := time.Now()
	var upBytes, downBytes atomic.Int64

	cp := func(dst io.Writer, src io.Reader, dir string, counter *atomic.Int64) {
		bufPtr := s.bufPool.Get().(*[]byte)
		defer s.bufPool.Put(bufPtr)
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
		log.Debugf("MTProto relay %s %s: %d bytes, err=%v", label, dir, total, err)
		errCh <- err
	}

	cpSplit := func(dst io.Writer, src io.Reader, dir string, counter *atomic.Int64) {
		bufPtr := s.bufPool.Get().(*[]byte)
		defer s.bufPool.Put(bufPtr)
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
		log.Debugf("MTProto relay %s %s: %d bytes, err=%v", label, dir, total, err)
		errCh <- err
	}

	if splitter != nil {
		go cpSplit(dc, client, "client->DC", &upBytes)
	} else {
		go cp(dc, client, "client->DC", &upBytes)
	}
	go cp(client, dc, "DC->client", &downBytes)

	<-errCh
	_ = client.Close()
	_ = dc.Close()
	<-errCh
	log.Infof("MTProto session %s closed: up=%d down=%d in %dms", label, upBytes.Load(), downBytes.Load(), time.Since(start).Milliseconds())
}
