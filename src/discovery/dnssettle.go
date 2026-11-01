package discovery

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	dnsSettleAddresses = 2
	dnsSettleTries     = 2
)

var dnsSettleRoots = netprobe.TLSRoots

func nonPublicAnswer(ips []string) bool {
	for _, ip := range ips {
		if refusedProbeIP(ip) || !netprobe.PublicAddress(net.ParseIP(ip)) {
			return true
		}
	}
	return false
}

func (ds *DiscoverySuite) settleUnverifiedDNS() {
	if ds.canceled() || rulesInvalidated() {
		return
	}
	var pending []string
	for _, di := range ds.Domains {
		if r := ds.dnsResults[di.Domain]; r != nil && r.Unverified {
			pending = append(pending, di.Domain)
		}
	}
	if len(pending) == 0 {
		return
	}
	ds.buildStrategyGroups()

	var presets []string
	byPreset := map[string][]string{}
	addrs := map[string][]string{}
	for _, domain := range pending {
		name, reason := ds.dnsInstrument(domain)
		if name != "" && !ds.httpsCheck(domain) {
			name, reason = "", "the check URL is plain HTTP, so no certificate shows whose server an address is"
		}
		if name != "" {
			if addrs[domain] = ds.settleAddresses(domain); len(addrs[domain]) == 0 {
				name, reason = "", "none of the system resolver's addresses is a public one to try"
			}
		}
		if name == "" {
			ds.logDNSUnsettled(domain, reason)
			continue
		}
		if _, seen := byPreset[name]; !seen {
			presets = append(presets, name)
		}
		byPreset[name] = append(byPreset[name], domain)
	}
	if len(presets) == 0 {
		return
	}

	ds.setPhase(PhaseConfirm)
	check := ds.checkSystemAddresses
	if ds.dnsSettleFn != nil {
		check = ds.dnsSettleFn
	}
	for _, name := range presets {
		if ds.canceled() {
			return
		}
		domains := byPreset[name]
		set := ds.storedSetFor(name, domains)
		if set == nil {
			for _, domain := range domains {
				ds.logDNSUnsettled(domain, fmt.Sprintf("the set '%s' was tested with is not recorded", name))
			}
			continue
		}
		wanted := make(map[string][]string, len(domains))
		for _, domain := range domains {
			wanted[domain] = addrs[domain]
		}
		served, completed := check(name, set, ds.inputsFor(domains), wanted)
		if !completed {
			return
		}
		for _, domain := range domains {
			ok, checked := served[domain]
			if !checked {
				ds.logDNSUnsettled(domain, fmt.Sprintf("'%s' could not be applied to try the system resolver's addresses", name))
				continue
			}
			ds.settleDNSResult(domain, name, addrs[domain], ok)
		}
	}
}

func (ds *DiscoverySuite) logDNSUnsettled(domain, reason string) {
	log.DiscoveryLogf("  [%s] DNS not verified: %s; the set keeps the DNS fix it was tested with", domain, reason)
}

func (ds *DiscoverySuite) dnsInstrument(domain string) (string, string) {
	const nothingFound = "no strategy this run found can be tried on the system resolver's addresses"
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()

	dr := ds.domainResults[domain]
	if dr == nil || dr.BaselineWorks || !dr.BestSuccess {
		return "", nothingFound
	}
	var candidates []string
	if ds.setMode() {
		domains := ds.runDomains()
		if ds.coverWinner != "" && ds.jointlyConfirmedLocked(ds.coverWinner, domains) {
			candidates = append(candidates, ds.coverWinner)
		}
		if completeEverywhere(presetSetCurrent, domains, ds.domainResults) {
			candidates = append(candidates, presetSetCurrent)
		}
	}
	for _, g := range ds.StrategyGroups {
		if containsString(g.Domains, domain) {
			candidates = append(candidates, g.WinnerPreset)
		}
	}
	candidates = append(candidates, dr.BestPreset)
	for _, name := range candidates {
		if plainFixName(name) {
			return "", fmt.Sprintf("the winner '%s' changes no packets, so it shows nothing about the system resolver's addresses", name)
		}
		if r := dr.Results[name]; r != nil && r.Status == CheckStatusComplete && r.Set != nil {
			return name, ""
		}
	}
	return "", nothingFound
}

func (ds *DiscoverySuite) httpsCheck(domain string) bool {
	for _, di := range ds.Domains {
		if di.Domain == domain {
			u, err := url.Parse(di.CheckURL)
			return err == nil && u.Scheme == "https"
		}
	}
	return false
}

func (ds *DiscoverySuite) settleAddresses(domain string) []string {
	r := ds.dnsResults[domain]
	if r == nil {
		return nil
	}
	var out []string
	for _, ip := range r.systemIPs {
		if len(out) == dnsSettleAddresses {
			break
		}
		if !r.isGateway(ip) && !nonPublicAnswer([]string{ip}) {
			out = append(out, ip)
		}
	}
	return out
}

func (ds *DiscoverySuite) settleConfig(stored *config.SetConfig, domains []string) *config.Config {
	cfg := ds.confirmConfig(stored, domains)
	if cfg == nil || len(cfg.Sets) == 0 {
		return nil
	}
	set := cfg.Sets[0]
	set.DNS = config.DNSConfig{StripECH: set.DNS.StripECH}
	set.TCP.IPBlockDetect = config.DefaultSetConfig.TCP.IPBlockDetect
	return cfg
}

func (ds *DiscoverySuite) checkSystemAddresses(presetName string, stored *config.SetConfig, inputs []DomainInput, addrs map[string][]string) (map[string]bool, bool) {
	served := map[string]bool{}
	if ds.pool == nil || ds.cfg == nil {
		return served, true
	}
	domains := make([]string, 0, len(inputs))
	for _, di := range inputs {
		domains = append(domains, di.Domain)
	}
	cfg := ds.settleConfig(stored, domains)
	if cfg == nil {
		return served, true
	}
	if err := ds.pool.UpdateConfig(cfg); err != nil {
		log.DiscoveryLogf("  DNS: could not apply '%s' to try the system resolver's addresses: %v", presetName, err)
		return served, true
	}
	time.Sleep(time.Duration(ds.cfg.System.Checker.ConfigPropagateMs) * time.Millisecond)

	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
	for _, di := range inputs {
		ips := addrs[di.Domain]
		log.DiscoveryLogf("  Checking the system resolver's addresses %v of %s with '%s'", ips, di.Domain, presetName)
		ok := len(ips) > 0
		for _, ip := range ips {
			if !ds.systemAddressServes(di, timeout, ip) {
				ok = false
				break
			}
		}
		if ds.canceled() {
			return served, false
		}
		served[di.Domain] = ok
	}
	return served, true
}

func (ds *DiscoverySuite) systemAddressServes(di DomainInput, timeout time.Duration, ip string) bool {
	reason := ""
	for try := 0; try < dnsSettleTries && !ds.canceled(); try++ {
		r := ds.fetchUsingIPForDomain(di, timeout, ip)
		if r.Status != CheckStatusComplete {
			reason = r.Error
			continue
		}
		if reason = settleRefusal(r, di.Domain); reason == "" {
			return true
		}
		break
	}
	if reason != "" {
		log.DiscoveryLogf("  [%s] %s does not serve the site: %s", di.Domain, ip, reason)
	}
	return false
}

func settleRefusal(r CheckResult, domain string) string {
	switch {
	case r.StatusCode < 200 || r.StatusCode >= 400:
		return fmt.Sprintf("it answers HTTP %d", r.StatusCode)
	case !certificateFor(r.firstTLS, domain):
		return "the certificate of its first answer is not valid for " + domain
	}
	return ""
}

func certificateFor(state *tls.ConnectionState, domain string) bool {
	if state == nil || len(state.PeerCertificates) == 0 {
		return false
	}
	leaf := state.PeerCertificates[0]
	roots, verify := dnsSettleRoots()
	if !verify {
		return leaf.VerifyHostname(domain) == nil
	}
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	_, err := leaf.Verify(x509.VerifyOptions{DNSName: domain, Roots: roots, Intermediates: intermediates})
	return err == nil
}

func (ds *DiscoverySuite) settleDNSResult(domain, preset string, addrs []string, served bool) {
	ds.CheckSuite.mu.Lock()
	r := ds.dnsResults[domain]
	if r == nil || !r.Unverified {
		ds.CheckSuite.mu.Unlock()
		return
	}
	r.Unverified = false
	if served {
		r.IsPoisoned = false
		r.SystemServesWithStrategy = true
		r.BestDoHURL, r.BestServer, r.NeedsFragment = "", "", false
		if r.referencePinned {
			r.AlternativeIPs, r.referencePinned = nil, false
		}
		for i := range r.ProbeResults {
			if r.ProbeResults[i].Server == "" {
				r.ProbeResults[i].IsPoisoned, r.ProbeResults[i].Works = false, true
			}
		}
	}
	ds.CheckSuite.mu.Unlock()

	if served {
		log.DiscoveryLogf("  ✓ [%s] the system resolver's addresses %v load the site through '%s' with a certificate for the site, so its DNS is not poisoned and it needs no DNS fix", domain, addrs, preset)
		return
	}
	log.DiscoveryLogf("  ✗ [%s] the system resolver's addresses %v do not load the site even through '%s', the set keeps the DNS fix", domain, addrs, preset)
}
