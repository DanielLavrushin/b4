package asn

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	CacheTTL      = 24 * time.Hour
	lookupTimeout = 5 * time.Second
)

type Info struct {
	ASN     string
	Country string
	Name    string
}

type LookupTXT func(ctx context.Context, name string) ([]string, error)

type Names interface {
	UpsertASNName(ctx context.Context, asn, name, country string, now time.Time) error
}

type entry struct {
	info    Info
	expires time.Time
}

type Resolver struct {
	lookup LookupTXT
	names  Names
	now    func() time.Time
	mu     sync.Mutex
	cache  map[string]entry
}

func New(lookup LookupTXT, names Names, now func() time.Time) *Resolver {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupTXT
	}
	if now == nil {
		now = time.Now
	}
	return &Resolver{lookup: lookup, names: names, now: now, cache: make(map[string]entry)}
}

func CymruName(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", v4[3], v4[2], v4[1], v4[0])
	}
	hex := make([]byte, 0, 32)
	for _, b := range ip.To16() {
		hex = append(hex, "0123456789abcdef"[b>>4], "0123456789abcdef"[b&0x0f])
	}
	parts := make([]string, 0, 32)
	for i := len(hex) - 1; i >= 0; i-- {
		parts = append(parts, string(hex[i]))
	}
	return strings.Join(parts, ".") + ".origin6.asn.cymru.com"
}

func Network(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32))
	}
	return ip.Mask(net.CIDRMask(48, 128))
}

func CacheKey(ip net.IP) string {
	if ip.To4() != nil {
		return Network(ip).String() + "/24"
	}
	return Network(ip).String() + "/48"
}

var tunnelRanges = []netip.Prefix{netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2001::/32")}

func Routable(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	addr, _ := netip.AddrFromSlice(ip)
	for _, p := range tunnelRanges {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

func (r *Resolver) Lookup(ctx context.Context, ip net.IP) Info {
	if !Routable(ip) {
		return Info{}
	}
	key := CacheKey(ip)
	now := r.now()
	r.mu.Lock()
	if e, ok := r.cache[key]; ok && now.Before(e.expires) {
		r.mu.Unlock()
		return e.info
	}
	r.mu.Unlock()

	info := r.resolve(ctx, Network(ip))
	if info.ASN == "" {
		return info
	}
	r.mu.Lock()
	r.cache[key] = entry{info: info, expires: now.Add(CacheTTL)}
	r.mu.Unlock()
	if r.names != nil && info.Name != "" {
		_ = r.names.UpsertASNName(ctx, info.ASN, info.Name, info.Country, now)
	}
	return info
}

func (r *Resolver) txt(ctx context.Context, name string) ([]string, error) {
	qctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	txts, err := r.lookup(qctx, name)
	if err != nil {
		return nil, err
	}
	if len(txts) == 0 {
		return nil, fmt.Errorf("no TXT answer for %s", name)
	}
	return txts, nil
}

func (r *Resolver) resolve(ctx context.Context, network net.IP) Info {
	info := Info{}
	origins, err := r.txt(ctx, CymruName(network))
	if err != nil {
		return info
	}
	info.ASN, info.Country = PickOrigin(origins)
	if info.ASN == "" {
		return info
	}
	if desc, err := r.txt(ctx, "AS"+info.ASN+".asn.cymru.com"); err == nil {
		info.Name = ParseDescription(desc[0])
	}
	return info
}

func PickOrigin(txts []string) (asn, country string) {
	longest := 0
	for _, txt := range txts {
		candidate, cc := ParseOrigin(txt)
		if candidate == "" {
			continue
		}
		bits := originPrefix(txt)
		if asn == "" || bits > longest || (bits == longest && lowerASN(candidate, asn)) {
			asn, country, longest = candidate, cc, bits
		}
	}
	return asn, country
}

func originPrefix(txt string) int {
	fields := strings.Split(txt, "|")
	if len(fields) < 2 {
		return 0
	}
	_, n, err := net.ParseCIDR(strings.TrimSpace(fields[1]))
	if err != nil {
		return 0
	}
	bits, _ := n.Mask.Size()
	return bits
}

func lowerASN(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func ParseOrigin(txt string) (asn, country string) {
	fields := strings.Split(txt, "|")
	if len(fields) < 3 {
		return "", ""
	}
	for _, candidate := range strings.Fields(fields[0]) {
		if asn == "" || lowerASN(candidate, asn) {
			asn = candidate
		}
	}
	if asn == "" {
		return "", ""
	}
	return asn, strings.ToUpper(strings.TrimSpace(fields[2]))
}

func ParseDescription(txt string) string {
	fields := strings.Split(txt, "|")
	if len(fields) < 5 {
		return ""
	}
	return strings.TrimSpace(fields[4])
}

var trustedProxies struct {
	mu   sync.RWMutex
	nets []*net.IPNet
}

func SetTrustedProxies(list string) error {
	var nets []*net.IPNet
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			ip := net.ParseIP(item)
			if ip == nil {
				return fmt.Errorf("trusted proxy %q: not an address or a CIDR", item)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			item = fmt.Sprintf("%s/%d", ip, bits)
		}
		_, n, err := net.ParseCIDR(item)
		if err != nil {
			return fmt.Errorf("trusted proxy %q: %w", item, err)
		}
		nets = append(nets, n)
	}
	trustedProxies.mu.Lock()
	trustedProxies.nets = nets
	trustedProxies.mu.Unlock()
	return nil
}

func trustedPeer(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	trustedProxies.mu.RLock()
	defer trustedProxies.mu.RUnlock()
	for _, n := range trustedProxies.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func ClientIP(req *http.Request) net.IP {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	peer := net.ParseIP(strings.Trim(host, "[]"))
	if peer == nil || !trustedPeer(peer) {
		return peer
	}
	if forwarded := req.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
			return ip
		}
	}
	if real := net.ParseIP(strings.TrimSpace(req.Header.Get("X-Real-IP"))); real != nil {
		return real
	}
	return peer
}
