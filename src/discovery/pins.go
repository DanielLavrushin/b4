package discovery

import (
	"net/netip"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns/endpoint"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

func (ds *DiscoverySuite) collectPins() map[string][]string {
	var pins map[string][]string
	add := func(src map[string][]string) {
		for rawDomain, ips := range src {
			domain := config.NormalizePinDomain(rawDomain)
			if domain == "" {
				continue
			}
			for _, raw := range ips {
				addr, err := netip.ParseAddr(raw)
				if err != nil {
					continue
				}
				if pins == nil {
					pins = map[string][]string{}
				}
				pins[domain] = appendUnique(pins[domain], addr.Unmap().String())
			}
		}
	}
	if ds.setStrategy != nil {
		add(ds.setStrategy.DNS.Pins)
	}
	add(ds.runPins)
	return pins
}

func (ds *DiscoverySuite) pinnedFor(domain string) []string {
	if len(ds.pins) == 0 {
		return nil
	}
	network := ds.dialNetwork()
	var out []string
	for _, raw := range (&config.DNSConfig{Pins: ds.pins}).PinnedAddresses(domain) {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			continue
		}
		addr = addr.Unmap()
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
	name := "example.com"
	for _, di := range ds.Domains {
		if asciiName(di.Domain) && len(ds.pinnedFor(di.Domain)) == 0 {
			name = di.Domain
			break
		}
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
	if err == nil || netprobe.NoAddressAnswer(err) {
		return nil
	}
	return err
}
