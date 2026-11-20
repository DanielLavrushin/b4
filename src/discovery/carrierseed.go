package discovery

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

const carrierSeedLookups = 8

func (ds *DiscoverySuite) seedCarrierAddresses() {
	if !ds.carrier || ds.interrupted() {
		return
	}
	var pending []string
	for _, di := range ds.Domains {
		if ds.dnsResults[di.Domain].gatewayIntercepted() || len(ds.targetIPsFor([]string{di.Domain})) > 0 {
			continue
		}
		pending = append(pending, di.Domain)
	}
	if len(pending) == 0 {
		return
	}

	runCtx, stop := watchedContext(ds.cancel, ds.finish)
	defer stop()
	ctx, cancel := context.WithTimeout(runCtx, nameCheckTimeout)
	defer cancel()
	resolver := probeResolver(int(ds.flowMark), nameCheckTimeout)

	seeds := make([][]string, len(pending))
	reasons := make([]string, len(pending))
	slots := make(chan struct{}, carrierSeedLookups)
	var wg sync.WaitGroup
	for i, domain := range pending {
		wg.Add(1)
		go func(i int, domain string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			seeds[i], reasons[i] = lookupCarrierSeeds(ctx, resolver, domain)
		}(i, domain)
	}
	wg.Wait()
	if ds.interrupted() {
		return
	}

	var failed []string
	for i, domain := range pending {
		if len(seeds[i]) == 0 {
			failed = append(failed, domain+" ("+reasons[i]+")")
			continue
		}
		if ds.carrierSeeds == nil {
			ds.carrierSeeds = map[string][]string{}
		}
		ds.carrierSeeds[domain] = seeds[i]
		log.Tracef("Discovery: %s is matched from the first SYN by %v", domain, seeds[i])
	}
	if n := len(ds.carrierSeeds); n > 0 {
		log.DiscoveryLogf("The TUN carrier sees no DNS answers: %d of %d domains without a known address are matched from their first SYN by the IPv4 addresses the system resolver gives now", n, len(pending))
	}
	if len(failed) > 0 {
		log.DiscoveryLogf("The TUN carrier sees no DNS answers, and the system resolver gave no IPv4 address for %s: a strategy that acts on the SYN can miss the first connection to each of their addresses", strings.Join(failed, ", "))
	}
}

func lookupCarrierSeeds(ctx context.Context, resolver *net.Resolver, domain string) ([]string, string) {
	addrs, err := resolver.LookupNetIP(ctx, "ip4", domain)
	if err != nil {
		return nil, carrierLookupReason(err)
	}
	var ips []string
	for _, addr := range addrs {
		addr = addr.Unmap()
		if !addr.Is4() || refusedProbeIP(addr.String()) {
			continue
		}
		ips = appendUnique(ips, addr.String())
	}
	if len(ips) == 0 {
		return nil, "no usable IPv4 address"
	}
	return asCIDRs(ips), ""
}

func carrierLookupReason(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return "no such name"
	case errors.As(err, &dnsErr) && dnsErr.Err != "":
		return dnsErr.Err
	}
	return err.Error()
}

func (ds *DiscoverySuite) seedsFor(domains ...string) []string {
	if !ds.carrier {
		return nil
	}
	var out []string
	for _, domain := range domains {
		out = appendUnique(out, ds.carrierSeeds[domain]...)
	}
	return out
}

func (ds *DiscoverySuite) cdnCarrierIPs(domain string) []string {
	if !ds.carrier {
		return nil
	}
	return appendUnique(ds.targetIPsFor([]string{domain}), ds.seedsFor(domain)...)
}

func plusMatchIPs(matched, extra []string) []string {
	if len(extra) == 0 {
		return matched
	}
	return appendUnique(slices.Clone(matched), extra...)
}
