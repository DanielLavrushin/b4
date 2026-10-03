package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const nameCheckTimeout = 5 * time.Second

func (ds *DiscoverySuite) unresolved(domain string) bool {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()
	dr := ds.domainResults[domain]
	return dr != nil && dr.Unresolved
}

func (ds *DiscoverySuite) anyUnresolved() bool {
	for _, di := range ds.Domains {
		if ds.unresolved(di.Domain) {
			return true
		}
	}
	return false
}

type nameVerdict struct {
	unresolved    bool
	missingFamily string
}

type familyLookup struct {
	family string
	lookup nameLookup
}

func (ds *DiscoverySuite) nameVerdict(domain string, baseline CheckResult) nameVerdict {
	if baseline.Status == CheckStatusComplete || baseline.lookup == lookupOK || baseline.lookupHost == "" {
		return nameVerdict{}
	}
	dnsResult := ds.dnsResults[domain]
	if dnsResult != nil && !dnsResult.noAddress() {
		return nameVerdict{}
	}

	probed, other := ds.probedFamilies(ds.lookupFamilies(baseline.lookupHost))
	for _, f := range probed {
		if f.lookup == lookupOK {
			return nameVerdict{}
		}
	}
	answered := probed[0].lookup == lookupNotFound || (dnsResult.noAddress() && asciiName(domain))
	for _, f := range other {
		if f.lookup == lookupOK && answered {
			return nameVerdict{unresolved: true, missingFamily: probed[0].family}
		}
	}
	definite := true
	for _, f := range append(probed, other...) {
		if f.lookup != lookupNotFound {
			definite = false
		}
	}
	if definite || (dnsResult.noAddress() && asciiName(domain)) {
		return nameVerdict{unresolved: true}
	}
	return nameVerdict{}
}

func (ds *DiscoverySuite) probedFamilies(v4, v6 nameLookup) (probed, other []familyLookup) {
	ipv4 := familyLookup{family: "ipv4", lookup: v4}
	ipv6 := familyLookup{family: "ipv6", lookup: v6}
	switch ds.dialNetwork() {
	case "tcp4":
		return []familyLookup{ipv4}, []familyLookup{ipv6}
	case "tcp6":
		return []familyLookup{ipv6}, []familyLookup{ipv4}
	}
	return []familyLookup{ipv4, ipv6}, nil
}

func (ds *DiscoverySuite) lookupFamilies(host string) (v4, v6 nameLookup) {
	runCtx, stop := watchedContext(ds.cancel)
	defer stop()
	ctx, cancel := context.WithTimeout(runCtx, nameCheckTimeout)
	defer cancel()

	resolver := probeResolver(int(ds.flowMark), nameCheckTimeout)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		v4 = lookupFamily(ctx, resolver, "ip4", host)
	}()
	go func() {
		defer wg.Done()
		v6 = lookupFamily(ctx, resolver, "ip6", host)
	}()
	wg.Wait()
	return v4, v6
}

func lookupFamily(ctx context.Context, resolver *net.Resolver, network, host string) nameLookup {
	ips, err := resolver.LookupNetIP(ctx, network, host)
	switch {
	case err == nil && len(ips) > 0:
		return lookupOK
	case err == nil:
		return lookupNotFound
	}
	return lookupFailureOf(err)
}

func (ds *DiscoverySuite) markUnresolved(baseline map[string]CheckResult) {
	if ds.canceled() {
		return
	}

	verdicts := make([]nameVerdict, len(ds.Domains))
	var wg sync.WaitGroup
	for i, di := range ds.Domains {
		r, tested := baseline[di.Domain]
		if !tested || ds.unresolved(di.Domain) {
			continue
		}
		wg.Add(1)
		go func(i int, domain string, r CheckResult) {
			defer wg.Done()
			verdicts[i] = ds.nameVerdict(domain, r)
		}(i, di.Domain, r)
	}
	wg.Wait()

	var marked []string
	ds.CheckSuite.mu.Lock()
	for i, di := range ds.Domains {
		dr := ds.domainResults[di.Domain]
		if dr == nil || dr.Unresolved || !verdicts[i].unresolved {
			continue
		}
		dr.Unresolved = true
		dr.MissingFamily = verdicts[i].missingFamily
		marked = append(marked, di.Domain)
	}
	if len(marked) > 0 {
		ds.moveOffUnresolvedPrimaryLocked()
		ds.refreshOutcomes(false)
	}
	ds.CheckSuite.mu.Unlock()

	for _, domain := range marked {
		family := ds.missingFamily(domain)
		log.DiscoveryLogf("  ⊘ [%s] %s; %s", domain, unresolvedReason(family, ds.dnsResults[domain]), unresolvedAdvice(family))
	}
}

func (ds *DiscoverySuite) missingFamily(domain string) string {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()
	if dr := ds.domainResults[domain]; dr != nil {
		return dr.MissingFamily
	}
	return ""
}

func (ds *DiscoverySuite) moveOffUnresolvedPrimaryLocked() {
	if dr := ds.domainResults[ds.Domain]; dr == nil || !dr.Unresolved {
		return
	}
	for _, di := range ds.Domains {
		if dr := ds.domainResults[di.Domain]; dr != nil && !dr.Unresolved {
			ds.Domain, ds.CheckURL = di.Domain, di.CheckURL
			return
		}
	}
}

func familyLabel(family string) string {
	if family == "ipv6" {
		return "IPv6"
	}
	return "IPv4"
}

func otherFamily(family string) string {
	if family == "ipv6" {
		return "ipv4"
	}
	return "ipv6"
}

func unresolvedReason(missingFamily string, dnsResult *DNSDiscoveryResult) string {
	switch {
	case missingFamily != "":
		return fmt.Sprintf("the name has %s addresses only, and this run probes over %s", familyLabel(otherFamily(missingFamily)), familyLabel(missingFamily))
	case dnsResult == nil:
		return "the system resolver has no address for the name, and the DNS check is off"
	case dnsResult.NXDomain:
		return "the name does not exist, DNS over HTTPS answers NXDOMAIN"
	}
	return "no resolver returned an address for the name"
}

func unresolvedAdvice(missingFamily string) string {
	if missingFamily != "" {
		return "there is nothing to test a strategy on over " + familyLabel(missingFamily)
	}
	return "there is nothing to test a strategy on, check the spelling"
}

func (ds *DiscoverySuite) nothingLeftToTest() bool {
	if ds.interrupted() || len(ds.Domains) == 0 {
		return false
	}
	unresolved := 0
	for _, di := range ds.Domains {
		switch {
		case ds.unresolved(di.Domain):
			unresolved++
		case !ds.dnsResults[di.Domain].gatewayIntercepted():
			return false
		}
	}
	if unresolved == len(ds.Domains) {
		log.DiscoveryLogf("No domain has an address this run can probe, there is nothing to test a strategy on; search skipped")
	} else {
		log.DiscoveryLogf("Every domain either has no address this run can probe or is answered by the first hop in front of this host; search skipped")
	}
	return true
}
