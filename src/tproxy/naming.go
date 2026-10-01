package tproxy

import (
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/sni"
	"golang.org/x/sys/unix"
)

const (
	sniffFirstWait  = 250 * time.Millisecond
	sniffTotalWait  = time.Second
	sniffRetryWait  = 10 * time.Millisecond
	sniffRetries    = 5
	sniffMaxBytes   = 16 * 1024
	tlsRecordHeader = 5
)

type sniffResult struct {
	prefix     []byte
	host       string
	tlsVersion uint16
}

func sniffClient(c net.Conn, firstWait, total time.Duration, limit int) sniffResult {
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	start := time.Now()
	buf := make([]byte, limit)
	_ = c.SetReadDeadline(start.Add(firstWait))
	n, err := c.Read(buf)
	if n <= 0 {
		return sniffResult{}
	}
	_ = c.SetReadDeadline(start.Add(total))
	for err == nil && n < limit && needsMoreBytes(buf[:n]) {
		var m int
		m, err = c.Read(buf[n:])
		n += m
	}
	res := sniffResult{prefix: append([]byte(nil), buf[:n]...)}
	res.host, res.tlsVersion = sniffedHost(res.prefix)
	return res
}

func peekClient(c net.Conn, limit int) []byte {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return nil
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return nil
	}
	var buf []byte
	n := 0
	err = rc.Control(func(fd uintptr) {
		queued, qerr := unix.IoctlGetInt(int(fd), unix.SIOCINQ)
		if qerr != nil || queued <= 0 {
			return
		}
		buf = make([]byte, min(queued, limit))
		if m, _, rerr := unix.Recvfrom(int(fd), buf, unix.MSG_PEEK|unix.MSG_DONTWAIT); rerr == nil && m > 0 {
			n = m
		}
	})
	if err != nil || n == 0 {
		return nil
	}
	return buf[:n]
}

type proxyNamer struct {
	log     func(host string, tlsVersion uint16)
	mu      sync.Mutex
	head    []byte
	seen    bool
	reading bool
	named   bool
}

func (p *proxyNamer) deadline(client net.Conn) {
	p.settle(client, false, 0)
}

func (p *proxyNamer) expire(client net.Conn) {
	p.settle(client, true, 0)
}

func (p *proxyNamer) settle(client net.Conn, final bool, retried int) {
	p.mu.Lock()
	named, seen, read := p.named, p.seen, len(p.head)
	p.mu.Unlock()
	if named || (seen && !final) {
		return
	}
	peeked := peekClient(client, sniffMaxBytes)
	if !seen && !final && len(peeked) > 0 && len(peeked) < sniffMaxBytes && needsMoreBytes(peeked) {
		return
	}
	p.mu.Lock()
	if p.named || (p.seen && !final) {
		p.mu.Unlock()
		return
	}
	busy := p.reading || p.seen != seen || len(p.head) != read
	if busy && final && retried < sniffRetries {
		p.mu.Unlock()
		time.AfterFunc(sniffRetryWait, func() { p.settle(client, true, retried+1) })
		return
	}
	first := p.head
	switch {
	case len(peeked) == 0:
	case busy && !final:
		p.mu.Unlock()
		return
	case busy:
	case !seen:
		first = peeked
	default:
		first = withRest(p.head, peeked)
	}
	p.named, p.head = true, nil
	p.mu.Unlock()
	p.write(first)
}

func withRest(head, rest []byte) []byte {
	joined := append(append([]byte(nil), head...), rest...)
	if host, _ := sniffedHost(joined); host != "" {
		return joined
	}
	return head
}

func (p *proxyNamer) enter() {
	p.mu.Lock()
	p.reading = true
	p.mu.Unlock()
}

func (p *proxyNamer) observe(b []byte, err error) bool {
	p.mu.Lock()
	p.reading = false
	if p.named {
		p.mu.Unlock()
		return true
	}
	if len(b) > 0 {
		p.seen = true
		p.head = append(p.head, b...)
	}
	if err == nil && (len(p.head) == 0 || (len(p.head) < sniffMaxBytes && needsMoreBytes(p.head))) {
		p.mu.Unlock()
		return false
	}
	first := p.head
	p.named, p.head = true, nil
	p.mu.Unlock()
	p.write(first)
	return true
}

func (p *proxyNamer) write(first []byte) {
	host, tlsVersion := sniffedHost(first)
	p.log(host, tlsVersion)
}

func (p *proxyNamer) wrap(c net.Conn) net.Conn {
	return &namingConn{Conn: c, namer: p}
}

type namingConn struct {
	net.Conn
	namer *proxyNamer
	done  bool
}

func (c *namingConn) Read(b []byte) (int, error) {
	if c.done {
		return c.Conn.Read(b)
	}
	c.namer.enter()
	n, err := c.Conn.Read(b)
	c.done = c.namer.observe(b[:n], err)
	return n, err
}

func (c *namingConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

func isClientHelloRecord(b []byte) bool {
	return len(b) > tlsRecordHeader && b[0] == 0x16 && b[tlsRecordHeader] == 0x01
}

func needsMoreBytes(b []byte) bool {
	if len(b) > 0 && b[0] == 0x16 {
		if len(b) < tlsRecordHeader+1 {
			return true
		}
		if b[tlsRecordHeader] != 0x01 {
			return false
		}
		return len(b) < tlsRecordHeader+(int(b[3])<<8|int(b[4]))
	}
	if sni.LooksLikeHTTPRequest(b) {
		return !sni.HTTPHeadersComplete(b)
	}
	return sni.IsHTTPMethodPrefix(b)
}

func sniffedHost(b []byte) (string, uint16) {
	if isClientHelloRecord(b) {
		host, ver, _ := sni.ParseTLSClientHelloSNI(b)
		return dns.CleanHostName(host), ver
	}
	if host, ok := sni.ParseHTTPHost(b); ok {
		return dns.CleanHostName(host), 0
	}
	return "", 0
}

type targetInputs struct {
	sniffed string
	names   dns.NameMatches
	learned string
	inSet   func(string) bool
	pinned  func(string) bool
}

func needsSniff(names dns.NameMatches, setHasDomains bool) bool {
	if len(names.Own) == 1 {
		return false
	}
	return !names.Empty() || setHasDomains
}

func chooseTarget(in targetInputs) (string, string) {
	name, source := pickName(in)
	if name == "" || (in.pinned != nil && in.pinned(name)) {
		return "", ""
	}
	return name, source
}

func pickName(in targetInputs) (string, string) {
	if in.sniffed != "" {
		if in.names.Has(in.sniffed) || (in.inSet != nil && in.inSet(in.sniffed)) {
			return in.sniffed, "sni"
		}
		return "", ""
	}
	if len(in.names.Own) > 0 {
		return in.names.Own[0], "dns"
	}
	if in.learned != "" {
		return in.learned, "learned"
	}
	for _, name := range in.names.Local {
		if in.inSet != nil && in.inSet(name) {
			return name, "dns"
		}
	}
	return "", ""
}
