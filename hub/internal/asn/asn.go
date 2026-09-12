package asn

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

func CacheKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String() + "/24"
	}
	return ip.Mask(net.CIDRMask(48, 128)).String() + "/48"
}

func Routable(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
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

	info := r.resolve(ctx, ip)
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

func (r *Resolver) txt(ctx context.Context, name string) (string, error) {
	qctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	txts, err := r.lookup(qctx, name)
	if err != nil {
		return "", err
	}
	if len(txts) == 0 {
		return "", fmt.Errorf("no TXT answer for %s", name)
	}
	return txts[0], nil
}

func (r *Resolver) resolve(ctx context.Context, ip net.IP) Info {
	info := Info{}
	origin, err := r.txt(ctx, CymruName(ip))
	if err != nil {
		return info
	}
	info.ASN, info.Country = ParseOrigin(origin)
	if info.ASN == "" {
		return info
	}
	if desc, err := r.txt(ctx, "AS"+info.ASN+".asn.cymru.com"); err == nil {
		info.Name = ParseDescription(desc)
	}
	return info
}

func ParseOrigin(txt string) (asn, country string) {
	fields := strings.Split(txt, "|")
	if len(fields) < 3 {
		return "", ""
	}
	asnFields := strings.Fields(fields[0])
	if len(asnFields) == 0 {
		return "", ""
	}
	return asnFields[0], strings.ToUpper(strings.TrimSpace(fields[2]))
}

func ParseDescription(txt string) string {
	fields := strings.Split(txt, "|")
	if len(fields) < 5 {
		return ""
	}
	return strings.TrimSpace(fields[4])
}

func ClientIP(req *http.Request) net.IP {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	peer := net.ParseIP(strings.Trim(host, "[]"))
	if peer == nil || !peer.IsLoopback() {
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
