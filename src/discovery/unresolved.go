package discovery

import "github.com/daniellavrushin/b4/log"

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

func (ds *DiscoverySuite) nameUnresolved(domain string, baseline CheckResult) bool {
	if baseline.Status == CheckStatusComplete {
		return false
	}
	switch baseline.lookup {
	case lookupNotFound:
		return true
	case lookupFailed:
		return ds.dnsResults[domain].noAddress()
	}
	return false
}

func (ds *DiscoverySuite) markUnresolved(baseline map[string]CheckResult) {
	if ds.interrupted() {
		return
	}

	var marked []string
	ds.CheckSuite.mu.Lock()
	for _, di := range ds.Domains {
		dr := ds.domainResults[di.Domain]
		r, tested := baseline[di.Domain]
		if dr == nil || dr.Unresolved || !tested || !ds.nameUnresolved(di.Domain, r) {
			continue
		}
		dr.Unresolved = true
		marked = append(marked, di.Domain)
	}
	if len(marked) > 0 {
		ds.moveOffUnresolvedPrimaryLocked()
		ds.refreshOutcomes(false)
	}
	ds.CheckSuite.mu.Unlock()

	for _, domain := range marked {
		log.DiscoveryLogf("  ⊘ [%s] %s; there is nothing to test a strategy on, check the spelling", domain, unresolvedReason(ds.dnsResults[domain]))
	}
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

func unresolvedReason(dnsResult *DNSDiscoveryResult) string {
	switch {
	case dnsResult == nil:
		return "the system resolver has no address for the name, and the DNS check is off"
	case dnsResult.NXDomain:
		return "the name does not exist, DNS over HTTPS answers NXDOMAIN"
	}
	return "no resolver returned an address for the name"
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
		log.DiscoveryLogf("No domain resolves to an address, there is nothing to test a strategy on; search skipped")
	} else {
		log.DiscoveryLogf("Every domain either does not resolve or is answered by the first hop in front of this host; search skipped")
	}
	return true
}
