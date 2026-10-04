package discovery

import (
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/dns/endpoint"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

func (ds *DiscoverySuite) collectPins() {
	ds.givenPins = normalizedPins(ds.runPins, nil)
	if ds.setStrategy != nil {
		ds.setPins = normalizedPins(ds.setStrategy.DNS.Pins, func(domain string, addr netip.Addr) {
			log.DiscoveryLogf("The set pins %s to %s, a private or local address, which Discovery does not probe", domain, addr)
		})
	}
}

func normalizedPins(src map[string][]string, refused func(string, netip.Addr)) map[string][]string {
	var pins map[string][]string
	for rawDomain, ips := range src {
		domain := config.NormalizePinDomain(rawDomain)
		if domain == "" {
			continue
		}
		for _, raw := range ips {
			addr, err := netip.ParseAddr(strings.TrimSpace(raw))
			if err != nil {
				continue
			}
			addr = addr.Unmap()
			if refusedProbeIP(addr.String()) {
				if refused != nil {
					refused(domain, addr)
				}
				continue
			}
			if pins == nil {
				pins = map[string][]string{}
			}
			pins[domain] = appendUnique(pins[domain], addr.String())
		}
	}
	return pins
}

func (ds *DiscoverySuite) pinnedFor(domain string) ([]string, bool) {
	if !asciiName(domain) {
		return nil, false
	}
	if ips := ds.familyPins(ds.givenPins, domain); len(ips) > 0 {
		return ips, false
	}
	ips := ds.familyPins(ds.setPins, domain)
	return ips, len(ips) > 0
}

func (ds *DiscoverySuite) familyPins(pins map[string][]string, domain string) []string {
	if len(pins) == 0 {
		return nil
	}
	network := ds.dialNetwork()
	var out []string
	for _, raw := range (&config.DNSConfig{Pins: pins}).PinnedAddresses(domain) {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			continue
		}
		if (network == "tcp4" && !addr.Is4()) || (network == "tcp6" && !addr.Is6()) {
			continue
		}
		out = appendUnique(out, addr.String())
	}
	return out
}

func (ds *DiscoverySuite) trustedServer() endpoint.Endpoint {
	if ds.dnsServerOverride != "" {
		ep, err := endpoint.Parse(ds.dnsServerOverride)
		if err == nil {
			return ep
		}
		log.DiscoveryLogf("Ignoring the trusted DNS server %q given for this run: %v", ds.dnsServerOverride, err)
	}
	ep, _ := ds.cfg.TrustedDNSServer()
	return ep
}

func (ds *DiscoverySuite) checkTrustedServer() error {
	name := ""
	for _, di := range ds.Domains {
		if pins, _ := ds.pinnedFor(di.Domain); len(pins) > 0 {
			continue
		}
		if asciiName(di.Domain) {
			name = di.Domain
			break
		}
		name = "example.com"
	}
	if name == "" {
		return nil
	}
	record := "A"
	if ds.dialNetwork() == "tcp6" {
		record = "AAAA"
	}
	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
	r := &netprobe.Resolver{Mark: int(ds.flowMark), Timeout: timeout}
	ctx, cancel := ds.fetchContext(2 * timeout)
	defer cancel()
	_, err := r.ResolveEndpoint(ctx, ds.trusted, name, record)
	var rcode *netprobe.RcodeError
	switch {
	case err == nil, netprobe.NoAddressAnswer(err):
		return nil
	case errors.As(err, &rcode) && rcode.Rcode != dns.RcodeRefused:
		return nil
	}
	return err
}
