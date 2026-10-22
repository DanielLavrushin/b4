package nfq

import (
	"bufio"
	"net"
	"os"
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

type tunSrcEntry struct {
	ip      string
	addr    net.IP
	origin  tunSrcOrigin
	expires time.Time
}

type tunSrcResolver struct {
	wanIP atomic.Value
	path  string

	mu    sync.Mutex
	cache map[string]tunSrcEntry
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
	wan, _ := r.wanIP.Load().(string)
	self := src.String()
	if self != wan && !hostAddress(src) {
		return nil, tunSrcWire
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
	lan := r.lookupConntrack(proto, self, sport, dstStr, dport)
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
	f, err := os.Open(r.path)
	if err != nil {
		return ""
	}
	defer f.Close()

	wantSport := strconv.Itoa(int(sport))
	wantDport := strconv.Itoa(int(dport))

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, pname+" ") && !strings.Contains(line, " "+pname+" ") {
			continue
		}
		if !strings.Contains(line, "dst="+wan) {
			continue
		}
		if lan := matchConntrackLine(line, wan, wantSport, dst, wantDport); lan != "" {
			return lan
		}
	}
	return ""
}

func matchConntrackLine(line, wan, sport, dst, dport string) string {
	var origSrc, origDst, replyDst string
	var origDport, replyDport string
	dstN, dportN := 0, 0

	for _, tok := range strings.Fields(line) {
		switch {
		case strings.HasPrefix(tok, "src="):
			if origSrc == "" {
				origSrc = tok[4:]
			}
		case strings.HasPrefix(tok, "dst="):
			v := tok[4:]
			if dstN == 0 {
				origDst = v
			} else if dstN == 1 {
				replyDst = v
			}
			dstN++
		case strings.HasPrefix(tok, "dport="):
			v := tok[6:]
			if dportN == 0 {
				origDport = v
			} else if dportN == 1 {
				replyDport = v
			}
			dportN++
		}
	}

	if origDst != dst || origDport != dport {
		return ""
	}
	if replyDst != wan || replyDport != sport {
		return ""
	}
	if origSrc == "" {
		return ""
	}
	return origSrc
}
