package config

import (
	"net"
	"slices"
	"strings"
)

const DefaultDNSPinTTLSec = 60

func NormalizePinDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimSuffix(d, ".")
	d = strings.TrimPrefix(d, "*.")
	return strings.TrimSuffix(d, ".")
}

func (c *DNSConfig) PinnedAddresses(domain string) []string {
	if c == nil || len(c.Pins) == 0 {
		return nil
	}
	q := NormalizePinDomain(domain)
	if q == "" {
		return nil
	}
	if ips, ok := c.Pins[q]; ok {
		return ips
	}

	best := ""
	var bestIPs []string
	for pin, ips := range c.Pins {
		if strings.HasSuffix(q, "."+pin) && len(pin) > len(best) {
			best = pin
			bestIPs = ips
		}
	}
	return bestIPs
}

func sanitizePins(pins map[string][]string, onInvalid func(domain, value string)) map[string][]string {
	if len(pins) == 0 {
		return nil
	}

	clean := make(map[string][]string, len(pins))
	for domain, ips := range pins {
		d := NormalizePinDomain(domain)
		if d == "" {
			continue
		}
		valid := make([]string, 0, len(ips))
		for _, raw := range ips {
			addr := strings.TrimSpace(raw)
			if net.ParseIP(addr) == nil {
				if onInvalid != nil {
					onInvalid(d, raw)
				}
				continue
			}
			valid = append(valid, addr)
		}
		if len(valid) > 0 {
			clean[d] = append(clean[d], valid...)
		}
	}

	if len(clean) == 0 {
		return nil
	}
	return clean
}

func PinDomains(pins map[string][]string) []string {
	domains := make([]string, 0, len(pins))
	for domain := range pins {
		domains = append(domains, domain)
	}
	return domains
}

func (s *SetConfig) ReplacePins(domains []string, pins map[string][]string) {
	applied := make(map[string]bool, len(domains))
	for _, domain := range domains {
		if normalized := NormalizePinDomain(domain); normalized != "" {
			applied[normalized] = true
		}
	}
	for pin := range s.DNS.Pins {
		if applied[NormalizePinDomain(pin)] {
			delete(s.DNS.Pins, pin)
		}
	}
	s.MergePins(pins)
}

func (s *SetConfig) MergePins(pins map[string][]string) {
	merged := false
	for rawDomain, ips := range pins {
		domain := NormalizePinDomain(rawDomain)
		if domain == "" {
			continue
		}
		for _, raw := range ips {
			ip := strings.TrimSpace(raw)
			parsed := net.ParseIP(ip)
			if parsed == nil || slices.ContainsFunc(s.DNS.Pins[domain], func(existing string) bool {
				return parsed.Equal(net.ParseIP(strings.TrimSpace(existing)))
			}) {
				continue
			}
			if s.DNS.Pins == nil {
				s.DNS.Pins = map[string][]string{}
			}
			s.DNS.Pins[domain] = append(s.DNS.Pins[domain], ip)
			merged = true
		}
	}
	if !merged {
		return
	}
	ibd := &s.TCP.IPBlockDetect
	ibd.Enabled = true
	ibd.SynDetect = true
	ibd.HealDNS = true
	ibd.CacheBlockedIPs = true
}
