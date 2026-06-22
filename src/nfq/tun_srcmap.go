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
)

const (
	conntrackPath     = "/proc/net/nf_conntrack"
	tunSrcCacheTTL    = 5 * time.Minute
	tunSrcNegativeTTL = 2 * time.Second
)

type tunSrcEntry struct {
	ip      string
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

func cacheKey(proto uint8, sport, dport uint16, dst string) string {
	var b strings.Builder
	b.WriteByte(proto)
	b.WriteString(strconv.Itoa(int(sport)))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(int(dport)))
	b.WriteByte('@')
	b.WriteString(dst)
	return b.String()
}

func (r *tunSrcResolver) resolve(proto uint8, src net.IP, sport uint16, dst net.IP, dport uint16) (net.IP, bool) {
	wan, _ := r.wanIP.Load().(string)
	if wan == "" {
		return nil, false
	}
	srcStr := src.String()
	if srcStr != wan {
		return nil, false
	}

	key := cacheKey(proto, sport, dport, dst.String())
	now := time.Now()

	r.mu.Lock()
	if e, ok := r.cache[key]; ok && now.Before(e.expires) {
		r.mu.Unlock()
		if e.ip == "" {
			return nil, false
		}
		if ip := net.ParseIP(e.ip); ip != nil {
			return ip, true
		}
		return nil, false
	}
	r.mu.Unlock()

	lan := r.lookupConntrack(proto, wan, sport, dst.String(), dport)

	r.mu.Lock()
	if lan == "" {
		r.cache[key] = tunSrcEntry{expires: now.Add(tunSrcNegativeTTL)}
	} else {
		r.cache[key] = tunSrcEntry{ip: lan, expires: now.Add(tunSrcCacheTTL)}
	}
	r.mu.Unlock()

	if lan == "" {
		return nil, false
	}
	if ip := net.ParseIP(lan); ip != nil {
		return ip, true
	}
	return nil, false
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
	if origSrc == "" || origSrc == wan {
		return ""
	}
	return origSrc
}
