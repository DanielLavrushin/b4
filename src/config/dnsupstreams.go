package config

import (
	"fmt"
	"net"
	"sort"
	"strings"
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
		if strings.TrimSpace(rest) == "" {
			return "", false
		}
		return strings.ToLower(scheme) + strings.TrimSpace(rest), true
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return "", false
	}
	return ip.String(), true
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

func pinsKey(pins map[string][]string) string {
	if len(pins) == 0 {
		return ""
	}
	domains := make([]string, 0, len(pins))
	for domain := range pins {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	var b strings.Builder
	for _, domain := range domains {
		b.WriteString(domain)
		b.WriteByte('=')
		b.WriteString(strings.Join(pins[domain], ","))
		b.WriteByte('\n')
	}
	return b.String()
}

func samePins(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for domain, aips := range a {
		bips, ok := b[domain]
		if !ok || len(aips) != len(bips) {
			return false
		}
		for i := range aips {
			if strings.TrimSpace(aips[i]) != strings.TrimSpace(bips[i]) {
				return false
			}
		}
	}
	return true
}

func (c *DNSConfig) SameResolver(o *DNSConfig) bool {
	if c == nil || o == nil {
		return c == o
	}
	if c.Enabled != o.Enabled || c.Strict != o.Strict || !samePins(c.Pins, o.Pins) {
		return false
	}
	return sameList(c.UpstreamList(), o.UpstreamList())
}

func (c *DNSConfig) ResolverKey() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%t|%t|%s|%s", c.Enabled, c.Strict, strings.Join(c.UpstreamList(), "\n"), pinsKey(c.Pins))
}

func (c *DNSConfig) NormalizeUpstreams(onDrop func(value string)) {
	c.NormalizeUpstreamsLimit(MaxDNSUpstreams, nil, onDrop)
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
