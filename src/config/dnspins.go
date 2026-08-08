package config

import (
	"net"
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
