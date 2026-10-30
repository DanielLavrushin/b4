package nfq

import (
	"bufio"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	conntrackPath     = "/proc/net/nf_conntrack"
	tunSrcCacheTTL    = 5 * time.Minute
	tunSrcNegativeTTL = 2 * time.Second
	tunSrcCacheMax    = 4096
)

var openConntrack = func(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

type tunSrcEntry struct {
	ip      string
	addr    net.IP
	origin  tunSrcOrigin
	expires time.Time
}

type tunSrcResolver struct {
	wanIP atomic.Value
	path  string
	own   localSockets

	mu    sync.Mutex
	cache map[string]tunSrcEntry

	scanMu     sync.Mutex
	scanQueued *conntrackScan
	scanActive *conntrackScan
}

type tunSrcPacket struct {
	ttl    uint8
	answer bool
}

type conntrackKey struct {
	proto string
	wan   string
	sport string
	dst   string
	dport string
}

type conntrackScan struct {
	want map[conntrackKey]string
	done chan struct{}
}

type conntrackRow struct {
	proto      string
	origSrc    string
	origDst    string
	origDport  string
	replyDst   string
	replyDport string
}

func dnsAnswerDatagram(udp []byte) bool {
	return len(udp) >= 20 && udp[10]&0x80 != 0
}

func newTunSrcResolver(wanIP string) *tunSrcResolver {
	r := &tunSrcResolver{
		path:  conntrackPath,
		cache: make(map[string]tunSrcEntry),
	}
	r.wanIP.Store(wanIP)
	return r
}

func (r *tunSrcResolver) setWAN(wanIP string) {
	if cur, _ := r.wanIP.Load().(string); cur == wanIP {
		return
	}
	r.wanIP.Store(wanIP)
	r.mu.Lock()
	r.cache = make(map[string]tunSrcEntry)
	r.mu.Unlock()
}

func cacheKey(proto uint8, src string, sport, dport uint16, dst string) string {
	var b strings.Builder
	b.WriteByte(proto)
	b.WriteString(src)
	b.WriteByte('#')
	b.WriteString(strconv.Itoa(int(sport)))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(int(dport)))
	b.WriteByte('@')
	b.WriteString(dst)
	return b.String()
}

type tunSrcOrigin uint8

const (
	tunSrcWire tunSrcOrigin = iota
	tunSrcLAN
	tunSrcRouter
	tunSrcUnknown
)

func (r *tunSrcResolver) resolve(proto uint8, src net.IP, sport uint16, dst net.IP, dport uint16) (net.IP, bool) {
	ip, origin := r.attribute(proto, src, sport, dst, dport)
	return ip, origin == tunSrcLAN
}

func originOf(lan, self string) (net.IP, tunSrcOrigin) {
	switch lan {
	case "":
		return nil, tunSrcUnknown
	case self:
		return nil, tunSrcRouter
	}
	ip := net.ParseIP(lan)
	switch {
	case ip == nil:
		return nil, tunSrcUnknown
	case hostAddress(ip):
		return ip, tunSrcRouter
	}
	return ip, tunSrcLAN
}

func (r *tunSrcResolver) attribute(proto uint8, src net.IP, sport uint16, dst net.IP, dport uint16) (net.IP, tunSrcOrigin) {
	return r.attributePacket(tunSrcPacket{}, proto, src, sport, dst, dport)
}

func (r *tunSrcResolver) attributePacket(seen tunSrcPacket, proto uint8, src net.IP, sport uint16, dst net.IP, dport uint16) (net.IP, tunSrcOrigin) {
	wan, _ := r.wanIP.Load().(string)
	self := src.String()
	if self != wan && !hostAddress(src) {
		return nil, tunSrcWire
	}
	if seen.answer {
		return nil, tunSrcUnknown
	}

	key := cacheKey(proto, self, sport, dport, dst.String())
	now := time.Now()

	r.mu.Lock()
	if e, ok := r.cache[key]; ok && now.Before(e.expires) {
		r.mu.Unlock()
		return e.addr, e.origin
	}
	r.mu.Unlock()

	dstStr := dst.String()
	lan := self
	if !r.own.holds(seen.ttl, proto, src, sport, dst, dport) {
		lan = r.lookupConntrack(proto, self, sport, dstStr, dport)
	}
	if lan == dstStr {
		log.Tracef("TUN source: conntrack named the destination %s as the source of %s:%d, ignoring it", dstStr, dstStr, dport)
		lan = ""
	}

	ip, origin := originOf(lan, self)
	ttl := tunSrcCacheTTL
	if origin != tunSrcLAN {
		ttl = tunSrcNegativeTTL
	}
	r.mu.Lock()
	r.pruneLocked(now)
	r.cache[key] = tunSrcEntry{ip: lan, addr: ip, origin: origin, expires: now.Add(ttl)}
	r.mu.Unlock()

	return ip, origin
}

func (r *tunSrcResolver) pruneLocked(now time.Time) {
	if len(r.cache) < tunSrcCacheMax {
		return
	}
	for k, e := range r.cache {
		if !now.Before(e.expires) {
			delete(r.cache, k)
		}
	}
	keep := tunSrcCacheMax * 3 / 4
	for k := range r.cache {
		if len(r.cache) <= keep {
			return
		}
		delete(r.cache, k)
	}
}

func (r *tunSrcResolver) addressable(proto uint8, src net.IP, sport uint16, dst net.IP, dport uint16) bool {
	wan, _ := r.wanIP.Load().(string)
	if wan == "" || src.String() != wan {
		return true
	}

	key := cacheKey(proto, wan, sport, dport, dst.String())

	r.mu.Lock()
	e, ok := r.cache[key]
	r.mu.Unlock()

	return ok && time.Now().Before(e.expires) && e.ip != ""
}

func protoName(proto uint8) string {
	switch proto {
	case 6:
		return "tcp"
	case 17:
		return "udp"
	default:
		return ""
	}
}

func (r *tunSrcResolver) lookupConntrack(proto uint8, wan string, sport uint16, dst string, dport uint16) string {
	pname := protoName(proto)
	if pname == "" {
		return ""
	}
	key := conntrackKey{proto: pname, wan: wan, sport: strconv.Itoa(int(sport)), dst: dst, dport: strconv.Itoa(int(dport))}

	r.scanMu.Lock()
	scan := r.scanQueued
	if scan == nil {
		scan = &conntrackScan{want: map[conntrackKey]string{}, done: make(chan struct{})}
		r.scanQueued = scan
	}
	scan.want[key] = ""
	for {
		select {
		case <-scan.done:
			lan := scan.want[key]
			r.scanMu.Unlock()
			return lan
		default:
		}
		if r.scanActive == nil {
			r.scanQueued, r.scanActive = nil, scan
			r.scanMu.Unlock()
			r.runScan(scan)
			r.scanMu.Lock()
			continue
		}
		active := r.scanActive.done
		r.scanMu.Unlock()
		<-active
		r.scanMu.Lock()
	}
}

func (r *tunSrcResolver) runScan(scan *conntrackScan) {
	defer func() {
		r.scanMu.Lock()
		r.scanActive = nil
		close(scan.done)
		r.scanMu.Unlock()
	}()
	r.readConntrack(scan.want)
}

func (r *tunSrcResolver) readConntrack(want map[conntrackKey]string) {
	f, err := openConntrack(r.path)
	if err != nil {
		return
	}
	defer f.Close()

	needles := make([]string, 0, 1)
	for k := range want {
		if needle := "dst=" + k.wan; !slices.Contains(needles, needle) {
			needles = append(needles, needle)
		}
	}
	left := len(want)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for left > 0 && sc.Scan() {
		line := sc.Text()
		if !containsAny(line, needles) {
			continue
		}
		row, ok := parseConntrackRow(line)
		if !ok {
			continue
		}
		k := conntrackKey{proto: row.proto, wan: row.replyDst, sport: row.replyDport, dst: row.origDst, dport: row.origDport}
		if lan, wanted := want[k]; wanted && lan == "" {
			want[k] = row.origSrc
			left--
		}
	}
}

func containsAny(line string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(line, n) {
			return true
		}
	}
	return false
}

func parseConntrackRow(line string) (conntrackRow, bool) {
	var row conntrackRow
	dstN, dportN := 0, 0

	for _, tok := range strings.Fields(line) {
		switch {
		case tok == "tcp" || tok == "udp":
			if row.proto == "" && row.origSrc == "" {
				row.proto = tok
			}
		case strings.HasPrefix(tok, "src="):
			if row.origSrc == "" {
				row.origSrc = tok[4:]
			}
		case strings.HasPrefix(tok, "dst="):
			switch dstN {
			case 0:
				row.origDst = tok[4:]
			case 1:
				row.replyDst = tok[4:]
			}
			dstN++
		case strings.HasPrefix(tok, "dport="):
			switch dportN {
			case 0:
				row.origDport = tok[6:]
			case 1:
				row.replyDport = tok[6:]
			}
			dportN++
		}
	}
	return row, row.proto != "" && row.origSrc != "" && dstN >= 2 && dportN >= 2
}
