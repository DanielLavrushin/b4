package socks5

import (
	"net"
	"net/netip"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type sourceACL struct {
	prefixes []netip.Prefix
	denyAll  bool
}

func hasSourceEntries(entries []string) bool {
	for _, e := range entries {
		if strings.TrimSpace(e) != "" {
			return true
		}
	}
	return false
}

func buildSourceACL(entries []string) (sourceACL, error) {
	if !hasSourceEntries(entries) {
		return sourceACL{}, nil
	}
	prefixes, err := config.ParseSourceACL(entries)
	if err != nil {
		return sourceACL{denyAll: true}, err
	}
	if len(prefixes) == 0 {
		return sourceACL{}, nil
	}
	return sourceACL{prefixes: prefixes}, nil
}

func (a *sourceACL) active() bool {
	return a != nil && (a.denyAll || len(a.prefixes) > 0)
}

func (a *sourceACL) allows(addr net.Addr) bool {
	if !a.active() {
		return true
	}
	if a.denyAll {
		return false
	}
	ip, ok := peerAddr(addr)
	if !ok {
		return false
	}
	for _, p := range a.prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func peerAddr(addr net.Addr) (netip.Addr, bool) {
	if addr == nil {
		return netip.Addr{}, false
	}
	var raw net.IP
	switch v := addr.(type) {
	case *net.TCPAddr:
		raw = v.IP
	case *net.UDPAddr:
		raw = v.IP
	case *net.IPAddr:
		raw = v.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			host = addr.String()
		}
		raw = net.ParseIP(host)
	}
	ip, ok := netip.AddrFromSlice(raw)
	if !ok {
		return netip.Addr{}, false
	}
	return ip.Unmap().WithZone(""), true
}

func (a *sourceACL) equal(other *sourceACL) bool {
	if a == nil || other == nil {
		return a == other
	}
	if a.denyAll != other.denyAll || len(a.prefixes) != len(other.prefixes) {
		return false
	}
	for i := range a.prefixes {
		if a.prefixes[i] != other.prefixes[i] {
			return false
		}
	}
	return true
}

func (a *sourceACL) describe() string {
	if a == nil {
		return "none"
	}
	if a.denyAll {
		return "deny all"
	}
	if len(a.prefixes) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(a.prefixes))
	for _, p := range a.prefixes {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ", ")
}

func (s *Server) refreshACL(cfg *config.Config) bool {
	next, err := buildSourceACL(cfg.System.Socks5.AllowedSources)
	if err != nil {
		log.Errorf("SOCKS5 allowed_sources is unusable (%v); every client is refused until it is corrected", err)
	}
	prev := s.acl.Load()
	if prev.equal(&next) {
		return false
	}
	s.acl.Store(&next)
	switch {
	case next.active():
		log.Infof("SOCKS5 source restriction active: %s", next.describe())
	case prev.active():
		log.Infof("SOCKS5 source restriction removed, every source may connect")
	}
	return true
}

func (s *Server) openLive() {
	s.liveMu.Lock()
	if s.live == nil {
		s.live = make(map[net.Conn]struct{})
	}
	s.liveClosed = false
	s.liveMu.Unlock()
}

func (s *Server) trackConn(conn net.Conn) bool {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if s.liveClosed {
		return false
	}
	if s.live == nil {
		s.live = make(map[net.Conn]struct{})
	}
	s.live[conn] = struct{}{}
	return true
}

func (s *Server) untrackConn(conn net.Conn) {
	s.liveMu.Lock()
	delete(s.live, conn)
	s.liveMu.Unlock()
}

func (s *Server) evictDeniedLocked() {
	acl := s.acl.Load()
	if !acl.active() {
		return
	}
	s.liveMu.Lock()
	var doomed []net.Conn
	for conn := range s.live {
		if !acl.allows(conn.RemoteAddr()) {
			doomed = append(doomed, conn)
		}
	}
	s.liveMu.Unlock()
	for _, conn := range doomed {
		log.Infof("SOCKS5 closing session from %s, its source is no longer allowed", conn.RemoteAddr())
		_ = conn.Close()
	}
}

func (s *Server) closeLiveLocked() {
	s.liveMu.Lock()
	s.liveClosed = true
	doomed := make([]net.Conn, 0, len(s.live))
	for conn := range s.live {
		doomed = append(doomed, conn)
	}
	s.live = make(map[net.Conn]struct{})
	s.liveMu.Unlock()
	for _, conn := range doomed {
		_ = conn.Close()
	}
	if len(doomed) > 0 {
		log.Infof("SOCKS5 closed %d live session(s)", len(doomed))
	}
}

func credentialsIncomplete(c *config.Socks5Config) bool {
	return (c.Username == "") != (c.Password == "")
}

func warnIncompleteCredentials(cfg *config.Config) {
	if credentialsIncomplete(&cfg.System.Socks5) {
		log.Errorf("SOCKS5 refuses every client: system.socks5.username and system.socks5.password must both be set or both be empty")
	}
}
