package discovery

import (
	"net/http"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

func (dr *DomainDiscoveryResult) untestable() bool {
	return dr.Unresolved || dr.LinkStatus != 0
}

func (ds *DiscoverySuite) badLink(domain string) bool {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()
	dr := ds.domainResults[domain]
	return dr != nil && dr.LinkStatus != 0
}

func (ds *DiscoverySuite) untestable(domain string) bool {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()
	dr := ds.domainResults[domain]
	return dr != nil && dr.untestable()
}

func (ds *DiscoverySuite) anyUntestable() bool {
	for _, di := range ds.Domains {
		if ds.untestable(di.Domain) {
			return true
		}
	}
	return false
}

func (ds *DiscoverySuite) refusedLink(di DomainInput, r CheckResult) bool {
	if r.untried || !r.finalHTTPS || r.Status != CheckStatusFailed || r.StatusCode != http.StatusBadRequest || !strings.HasPrefix(di.CheckURL, "https://") {
		return false
	}
	dnsResult := ds.dnsResults[di.Domain]
	if dnsResult == nil || !dnsResult.IsPoisoned {
		return true
	}
	return slices.Contains(dnsResult.ExpectedIPs, r.UsedIP) || slices.Contains(dnsResult.AlternativeIPs, r.UsedIP)
}

func (ds *DiscoverySuite) markBadLinks(baseline map[string]CheckResult) {
	if ds.canceled() {
		return
	}

	type refused struct {
		input  DomainInput
		status int
	}
	var marked []refused
	ds.CheckSuite.mu.Lock()
	for _, di := range ds.Domains {
		dr := ds.domainResults[di.Domain]
		r, tested := baseline[di.Domain]
		if dr == nil || !tested || dr.untestable() || !ds.refusedLink(di, r) {
			continue
		}
		dr.LinkStatus = r.StatusCode
		marked = append(marked, refused{input: di, status: r.StatusCode})
	}
	if len(marked) > 0 {
		ds.moveOffUntestablePrimaryLocked()
		ds.refreshOutcomes(false)
	}
	ds.CheckSuite.mu.Unlock()

	for _, m := range marked {
		log.DiscoveryLogf("  ⊘ [%s] %s answers HTTP %d without any strategy, so it cannot show whether one works; give the link of a page or file the site serves", m.input.Domain, m.input.CheckURL, m.status)
	}
}

func (ds *DiscoverySuite) testableOf(domains []string) []string {
	var out []string
	for _, domain := range domains {
		if !ds.untestable(domain) {
			out = append(out, domain)
		}
	}
	return out
}

func (ds *DiscoverySuite) badLinksOf(domains []string) []string {
	var out []string
	for _, domain := range domains {
		if dr := ds.domainResults[domain]; dr != nil && dr.LinkStatus != 0 {
			out = append(out, domain)
		}
	}
	return out
}
