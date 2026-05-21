package socks5

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
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
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sni"
)

// SOCKS5 protocol constants (RFC 1928, RFC 1929)
const (
	socks5Version = 0x05

	// Auth methods
	authNone       = 0x00
	authUserPass   = 0x02
	authNoAccept   = 0xFF
	authSubVersion = 0x01

	// Commands
	cmdConnect      = 0x01
	cmdUDPAssociate = 0x03

	// Address types
	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	// Reply codes
	repSuccess          = 0x00
	repServerFailure    = 0x01
	repHostUnreachable  = 0x04
	repCmdNotSupported  = 0x07
	repAddrNotSupported = 0x08

	// Limits
	maxConnections = 1024
	handshakeTime  = 30 * time.Second
	dialTimeout    = 10 * time.Second
	bufferSize     = 32 * 1024
)

type IPBlockCache interface {
	IsBlocked(dstIPPort string) bool
	AddBlocked(dstIPPort string)
}

// Server is a SOCKS5 proxy server.
type Server struct {
	cfg      *config.Config
	listener net.Listener

	ctx    context.Context
	cancel context.CancelFunc

	activeConns atomic.Int64
	connSem     chan struct{} // semaphore for connection limiting

	bufferPool   sync.Pool
	matcher      atomic.Value // stores *sni.SuffixSet
	ipBlockCache IPBlockCache
}

func (s *Server) SetIPBlockCache(cache IPBlockCache) {
	s.ipBlockCache = cache
}

// NewServer creates a new SOCKS5 server.
func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:     cfg,
		connSem: make(chan struct{}, maxConnections),
		bufferPool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, bufferSize)
				return &buf
			},
		},
	}
}

// Start begins listening for SOCKS5 connections. Returns nil immediately if disabled.
func (s *Server) Start() error {
	if !s.cfg.System.Socks5.Enabled {
		log.Infof("SOCKS5 server disabled")
		return nil
	}

	addr := net.JoinHostPort(s.cfg.System.Socks5.BindAddress, strconv.Itoa(s.cfg.System.Socks5.Port))
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// Build initial matcher from current config
	if m := buildMatcher(s.cfg); m != nil {
		s.matcher.Store(m)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("SOCKS5 TCP listen: %w", err)
	}
	s.listener = ln

	log.Infof("SOCKS5 server listening on %s", addr)

	go s.acceptLoop()

	return nil
}

// Stop gracefully shuts down the SOCKS5 server.
func (s *Server) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}

	if s.listener != nil {
		return s.listener.Close()
	}

	return nil
}

// --- TCP accept loop ---

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Errorf("SOCKS5 accept: %v", err)
			continue
		}

		// Enforce connection limit via semaphore
		select {
		case s.connSem <- struct{}{}:
		default:
			log.Tracef("SOCKS5 connection limit reached, rejecting %s", conn.RemoteAddr())
			conn.Close()
			continue
		}

		s.activeConns.Add(1)
		go func() {
			defer func() {
				conn.Close()
				<-s.connSem
				s.activeConns.Add(-1)
			}()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) handleConn(conn net.Conn) {
	clientAddr := conn.RemoteAddr().String()
	log.Debugf("SOCKS5 new connection from %s", clientAddr)

	// Set deadline for handshake only
	if err := conn.SetDeadline(time.Now().Add(handshakeTime)); err != nil {
		log.Tracef("SOCKS5 failed to set deadline: %v", err)
		return
	}

	if err := s.authenticate(conn); err != nil {
		log.Tracef("SOCKS5 auth failed from %s: %v", clientAddr, err)
		return
	}

	if err := s.handleRequest(conn); err != nil {
		log.Tracef("SOCKS5 request failed from %s: %v", clientAddr, err)
	}
}

// --- Authentication (RFC 1928 + RFC 1929) ---

func (s *Server) authenticate(conn net.Conn) error {
	// Read version + method count
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}
	if hdr[0] != socks5Version {
		return fmt.Errorf("unsupported version %d", hdr[0])
	}

	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("read methods: %w", err)
	}

	log.Debugf("SOCKS5 auth from %s: methods=%v", conn.RemoteAddr(), methods)

	socksCfg := &s.cfg.System.Socks5
	needAuth := socksCfg.Username != "" && socksCfg.Password != ""
	var chosen byte = authNoAccept

	if needAuth {
		for _, m := range methods {
			if m == authUserPass {
				chosen = authUserPass
				break
			}
		}
	} else {
		for _, m := range methods {
			if m == authNone {
				chosen = authNone
				break
			}
		}
	}

	if _, err := conn.Write([]byte{socks5Version, chosen}); err != nil {
		return fmt.Errorf("write method selection: %w", err)
	}
	if chosen == authNoAccept {
		return fmt.Errorf("no acceptable auth method")
	}
	if chosen == authUserPass {
		return s.subnegotiateUserPass(conn)
	}

	log.Debugf("SOCKS5 auth successful from %s (method: %d)", conn.RemoteAddr(), chosen)
	return nil
}

func (s *Server) subnegotiateUserPass(conn net.Conn) error {
	// RFC 1929: VER(1) ULEN(1) UNAME(1-255) PLEN(1) PASSWD(1-255)
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return fmt.Errorf("read auth header: %w", err)
	}
	if hdr[0] != authSubVersion {
		return fmt.Errorf("unsupported auth sub-version %d", hdr[0])
	}

	uname := make([]byte, hdr[1])
	if _, err := io.ReadFull(conn, uname); err != nil {
		return fmt.Errorf("read username: %w", err)
	}

	plenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, plenBuf); err != nil {
		return fmt.Errorf("read password length: %w", err)
	}

	passwd := make([]byte, plenBuf[0])
	if _, err := io.ReadFull(conn, passwd); err != nil {
		return fmt.Errorf("read password: %w", err)
	}

	socksCfg := &s.cfg.System.Socks5
	// Constant-time comparison to prevent timing attacks
	userOK := subtle.ConstantTimeCompare(uname, []byte(socksCfg.Username)) == 1
	passOK := subtle.ConstantTimeCompare(passwd, []byte(socksCfg.Password)) == 1
	ok := userOK && passOK

	status := byte(0x00)
	if !ok {
		status = 0x01
	}
	if _, err := conn.Write([]byte{authSubVersion, status}); err != nil {
		return fmt.Errorf("write auth result: %w", err)
	}
	if !ok {
		return fmt.Errorf("invalid credentials")
	}
	return nil
}

// --- Request handling (RFC 1928 section 4) ---

func (s *Server) handleRequest(conn net.Conn) error {
	// VER(1) CMD(1) RSV(1) ATYP(1)
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if hdr[0] != socks5Version {
		sendReply(conn, repServerFailure, nil)
		return fmt.Errorf("unsupported version %d", hdr[0])
	}

	dest, err := readAddress(conn, hdr[3])
	if err != nil {
		sendReply(conn, repAddrNotSupported, nil)
		return fmt.Errorf("read address: %w", err)
	}

	log.Tracef("SOCKS5 request from %s: cmd=%d, dest=%s", conn.RemoteAddr(), hdr[1], dest)

	switch hdr[1] {
	case cmdConnect:
		return s.handleConnect(conn, dest)
	case cmdUDPAssociate:
		return s.handleUDPAssociate(conn, dest)
	default:
		sendReply(conn, repCmdNotSupported, nil)
		return fmt.Errorf("unsupported command %d", hdr[1])
	}
}

// --- TCP CONNECT ---

func (s *Server) handleConnect(conn net.Conn, dest string) error {
	if s.ipBlockCache != nil && s.ipBlockCache.IsBlocked(dest) {
		log.Tracef("SOCKS5 blocked cached IP: %s", dest)
		sendReply(conn, repHostUnreachable, nil)
		return fmt.Errorf("destination %s is cached as blocked", dest)
	}

	remote, err := net.DialTimeout("tcp", dest, dialTimeout)
	if err != nil {
		log.Tracef("SOCKS5 connect to %s failed: %v", dest, err)
		sendReply(conn, repHostUnreachable, nil)
		return err
	}
	defer remote.Close()

	if err := sendReply(conn, repSuccess, remote.LocalAddr()); err != nil {
		return fmt.Errorf("send reply: %w", err)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear deadline: %w", err)
	}

	s.logAndRecordConnection("TCP", conn.RemoteAddr().String(), dest, "socks5")

	return s.relay(conn, remote)
}

// relay copies data bidirectionally until one side closes.
func (s *Server) relay(a, b net.Conn) error {
	errCh := make(chan error, 2)

	cp := func(dst, src net.Conn) {
		bufPtr := s.bufferPool.Get().(*[]byte)
		buf := *bufPtr
		defer s.bufferPool.Put(bufPtr)

		_, err := io.CopyBuffer(dst, src, buf)

		// Signal the other direction to stop by closing the write half
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		errCh <- err
	}

	go cp(b, a)
	go cp(a, b)

	// Wait for both directions
	err1 := <-errCh
	err2 := <-errCh

	// Return first non-EOF error
	if err1 != nil && !errors.Is(err1, io.EOF) {
		return err1
	}
	if err2 != nil && !errors.Is(err2, io.EOF) {
		return err2
	}
	return nil
}

// --- Set matching ---

func (s *Server) getMatcher() *sni.SuffixSet {
	if v := s.matcher.Load(); v != nil {
		return v.(*sni.SuffixSet)
	}
	return nil
}

func buildMatcher(cfg *config.Config) *sni.SuffixSet {
	if len(cfg.Sets) > 0 {
		return sni.NewSuffixSet(cfg.Sets)
	}
	return nil
}

func (s *Server) UpdateConfig(newCfg *config.Config) {
	newMatcher := buildMatcher(newCfg)
	old := s.getMatcher()

	if newMatcher != nil {
		if old != nil {
			newMatcher.TransferLearnedIPs(old)
		}
		s.matcher.Store(newMatcher)
	} else if old != nil {
		s.matcher.Store((*sni.SuffixSet)(nil))
	}
	log.Tracef("SOCKS5 matcher refreshed from config update")
}

func (s *Server) matchDestination(dest string) (bool, string, bool, string) {
	_, sniTarget, _, ipTarget := s.matchDestinationSet(dest)
	return sniTarget != "", sniTarget, ipTarget != "", ipTarget
}

func (s *Server) matchDestinationSet(dest string) (*config.SetConfig, string, *config.SetConfig, string) {
	matcher := s.getMatcher()
	if matcher == nil {
		return nil, "", nil, ""
	}

	host, _, err := net.SplitHostPort(dest)
	if err != nil {
		return nil, "", nil, ""
	}

	var sniSet, ipSet *config.SetConfig
	var sniTarget, ipTarget string

	if host != "" {
		if matched, set := matcher.MatchSNI(host); matched && set != nil {
			sniSet = set
			sniTarget = set.Name
		}
	}

	ip := net.ParseIP(host)
	if ip != nil {
		if matched, set := matcher.MatchIP(ip); matched && set != nil {
			ipSet = set
			ipTarget = set.Name
		}
	}

	return sniSet, sniTarget, ipSet, ipTarget
}

// --- Logging and metrics ---

func (s *Server) logAndRecordConnection(protocol, clientAddr, dest, metadata string) {
	clientHost, clientPortStr, _ := net.SplitHostPort(clientAddr)

	domain := dest
	destHost, destPortStr, _ := net.SplitHostPort(dest)
	if destHost != "" {
		domain = destHost
	}

	matchedSNI, sniTarget, matchedIP, ipTarget := s.matchDestination(dest)

	source := net.JoinHostPort(clientHost, clientPortStr)
	destination := net.JoinHostPort(destHost, destPortStr)
	log.LogConnectionStr(protocol, sniTarget, domain, source, ipTarget, destination, "", "", metadata)

	setName := ""
	if matchedSNI {
		setName = sniTarget
	} else if matchedIP {
		setName = ipTarget
	}

	log.Tracef("SOCKS5 %s relay: %s <-> %s (Set: %s)", protocol, clientAddr, dest, setName)

	if m := metrics.GetMetricsCollector(); m != nil {
		matched := matchedSNI || matchedIP
		m.RecordConnection(protocol, domain, clientAddr, dest, matched, "", setName, "")
	}
}

// --- Address parsing ---

// readAddress reads a SOCKS5 address from r (ATYP already consumed, addrType provided).
func readAddress(r io.Reader, addrType byte) (string, error) {
	switch addrType {
	case atypIPv4:
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		ip := net.IP(buf[:4])
		port := binary.BigEndian.Uint16(buf[4:])
		return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), nil

	case atypIPv6:
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		ip := net.IP(buf[:16])
		port := binary.BigEndian.Uint16(buf[16:])
		return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), nil

	case atypDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return "", err
		}
		buf := make([]byte, int(lenBuf[0])+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		domain := string(buf[:len(buf)-2])
		port := binary.BigEndian.Uint16(buf[len(buf)-2:])
		return net.JoinHostPort(domain, strconv.Itoa(int(port))), nil

	default:
		return "", fmt.Errorf("unsupported address type %d", addrType)
	}
}

// sendReply sends a SOCKS5 reply. If bindAddr is nil, uses 0.0.0.0:0.
func sendReply(conn net.Conn, rep byte, bindAddr net.Addr) error {
	reply := []byte{socks5Version, rep, 0x00}

	if bindAddr == nil {
		reply = append(reply, atypIPv4, 0, 0, 0, 0, 0, 0)
	} else {
		host, portStr, err := net.SplitHostPort(bindAddr.String())
		if err != nil {
			return err
		}
		port, _ := strconv.Atoi(portStr)

		ip := net.ParseIP(host)
		if ip4 := ip.To4(); ip4 != nil {
			reply = append(reply, atypIPv4)
			reply = append(reply, ip4...)
		} else {
			reply = append(reply, atypIPv6)
			reply = append(reply, ip.To16()...)
		}

		portBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(portBuf, uint16(port))
		reply = append(reply, portBuf...)
	}

	_, err := conn.Write(reply)
	return err
}
