package tproxy

import (
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/sni"
	"golang.org/x/sys/unix"
)

const (
	sniffFirstWait  = 250 * time.Millisecond
	sniffTotalWait  = time.Second
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
	err = rc.Read(func(fd uintptr) bool {
		queued, qerr := unix.IoctlGetInt(int(fd), unix.SIOCINQ)
		if qerr != nil || queued <= 0 {
			return true
		}
		buf = make([]byte, min(queued, limit))
		if m, _, rerr := unix.Recvfrom(int(fd), buf, unix.MSG_PEEK|unix.MSG_DONTWAIT); rerr == nil && m > 0 {
			n = m
		}
		return true
	})
	if err != nil || n == 0 {
		return nil
	}
	return buf[:n]
}

type proxyNamer struct {
	once   sync.Once
	log    func(host string, tlsVersion uint16)
	piping atomic.Bool
	seen   atomic.Bool
}

func (p *proxyNamer) name(first []byte) {
	p.once.Do(func() {
		host, tlsVersion := sniffedHost(first)
		p.log(host, tlsVersion)
	})
}

func (p *proxyNamer) deadline(client net.Conn) {
	switch {
	case !p.piping.Load():
		p.name(peekClient(client, sniffMaxBytes))
	case !p.seen.Load():
		p.name(nil)
	}
}

func (p *proxyNamer) wrap(c net.Conn) net.Conn {
	p.piping.Store(true)
	return &namingConn{Conn: c, namer: p}
}

type namingConn struct {
	net.Conn
	namer *proxyNamer
	head  []byte
	done  bool
}

func (c *namingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if !c.done {
		if n > 0 {
			c.namer.seen.Store(true)
			c.head = append(c.head, b[:n]...)
		}
		if err != nil || (len(c.head) > 0 && (!needsMoreBytes(c.head) || len(c.head) >= sniffMaxBytes)) {
			c.done = true
			c.namer.name(c.head)
			c.head = nil
		}
	}
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
