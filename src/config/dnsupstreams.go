package config

import (
	"fmt"
	"net"
	"strings"

	"github.com/daniellavrushin/b4/dns/endpoint"
	"github.com/daniellavrushin/b4/log"
)

const MaxDNSUpstreams = 8

func SplitUpstreamURL(raw string) (scheme, rest string, ok bool) {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	for _, candidate := range []string{"https://", "http://"} {
		if strings.HasPrefix(lower, candidate) {
			return raw[:len(candidate)], raw[len(candidate):], true
		}
	}
	return "", "", false
}

func IsDoHUpstream(raw string) bool {
	_, _, ok := SplitUpstreamURL(raw)
	return ok
}

func NormalizeUpstream(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if scheme, rest, ok := SplitUpstreamURL(raw); ok {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return "", false
		}
		return strings.ToLower(scheme) + lowerURLHost(rest), true
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return "", false
	}
	return ip.String(), true
}

func lowerURLHost(rest string) string {
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	start := strings.LastIndexByte(rest[:end], '@') + 1
	host := rest[start:end]
	lower := strings.ToLower(host)
	if lower == host {
		return rest
	}
	return rest[:start] + lower + rest[end:]
}

func (c *DNSConfig) UpstreamList() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, 2+len(c.Upstreams))
	seen := make(map[string]struct{}, 2+len(c.Upstreams))
	add := func(raw string) {
		norm, ok := NormalizeUpstream(raw)
		if !ok {
			return
		}
		if _, dup := seen[norm]; dup {
			return
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	add(c.DoHURL)
	if strings.TrimSpace(c.DoHURL) == "" {
		add(c.TargetDNS)
	}
	for _, raw := range c.Upstreams {
		add(raw)
	}
	if len(out) > MaxDNSUpstreams {
		out = out[:MaxDNSUpstreams]
	}
	return out
}

func UpstreamLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if scheme, rest, ok := SplitUpstreamURL(raw); ok {
		host := strings.SplitN(rest, "/", 2)[0]
		host = strings.SplitN(host, "?", 2)[0]
		host = strings.TrimSpace(host)
		if host != "" {
			if at := strings.LastIndex(host, "@"); at >= 0 {
				host = host[at+1:]
			}
			return scheme + host
		}
		return scheme
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String()
	}
	return ""
}

func (c *DNSConfig) HasUpstream() bool {
	if c == nil || !c.Enabled {
		return false
	}
	return len(c.UpstreamList()) > 0
}

func (c *DNSConfig) SameRoutingResolver(o *DNSConfig) bool {
	if c == nil || o == nil {
		return c == o
	}
	return c.Enabled == o.Enabled && c.Strict == o.Strict && sameList(c.UpstreamList(), o.UpstreamList())
}

func (c *DNSConfig) RoutingResolverKey() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%t|%t|%s", c.Enabled, c.Strict, strings.Join(c.UpstreamList(), "\n"))
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (c *DNSConfig) NormalizeUpstreamsLimit(limit int, already []string, onDrop func(value string)) {
	if c == nil || len(c.Upstreams) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(c.Upstreams)+len(already))
	for _, raw := range already {
		if norm, ok := NormalizeUpstream(raw); ok {
			seen[norm] = struct{}{}
		}
	}
	out := make([]string, 0, len(c.Upstreams))
	for _, raw := range c.Upstreams {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		norm, ok := NormalizeUpstream(raw)
		if !ok {
			if onDrop != nil {
				onDrop(raw)
			}
			continue
		}
		if _, dup := seen[norm]; dup {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	if limit < 0 {
		limit = 0
	}
	if len(out) > limit {
		if onDrop != nil {
			for _, raw := range out[limit:] {
				onDrop(raw)
			}
		}
		out = out[:limit]
	}
	c.Upstreams = out
}

func normalizeSetUpstreams(v *validator, setIdx int, set *SetConfig) bool {
	path := fmt.Sprintf("sets[%d].dns.upstreams", setIdx)
	for i, raw := range set.DNS.Upstreams {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		params := map[string]any{"set": set.Name, "index": i + 1}
		norm, ok := NormalizeUpstream(raw)
		if !ok {
			v.addf(path, "invalid_dns_upstream", params,
				"set %q: DNS upstream entry %d is neither an address nor an https:// URL", set.Name, i+1)
			return false
		}
		if scheme, _, isURL := SplitUpstreamURL(norm); isURL && !strings.EqualFold(scheme, "https://") {
			v.addf(path, "invalid_dns_upstream", params,
				"set %q: DNS upstream entry %d must use https://, a plain http:// query travels unencrypted", set.Name, i+1)
			return false
		}
		if _, err := endpoint.Parse(norm); err != nil {
			v.addf(path, "invalid_dns_upstream", params,
				"set %q: DNS upstream entry %d cannot be a resolver: %v", set.Name, i+1, err)
			return false
		}
	}
	listLimit := MaxDNSUpstreams
	var primary []string
	for _, candidate := range []string{set.DNS.DoHURL, set.DNS.TargetDNS} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if norm, ok := NormalizeUpstream(candidate); ok {
			listLimit--
			primary = append(primary, norm)
		}
		break
	}
	set.DNS.NormalizeUpstreamsLimit(listLimit, primary, func(value string) {
		label := UpstreamLabel(value)
		if label == "" {
			label = "one entry"
		}
		log.Warnf("Set '%s': dropping DNS upstream %s, the set already asks %d resolvers in total", set.Name, label, MaxDNSUpstreams)
	})
	return true
}
