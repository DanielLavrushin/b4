package detector

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const markThroughB4 uint = 0

func parseSiteInput(input string) (domain, fullURL string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	withScheme := input
	if !strings.Contains(input, "://") {
		withScheme = "https://" + input
	}
	u, err := url.Parse(withScheme)
	if err != nil || u.Hostname() == "" {
		return "", ""
	}
	if u.Scheme == "http" {
		u.Scheme = "https"
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.Fragment = ""
	return strings.ToLower(u.Hostname()), u.String()
}

func uniqueSites(inputs []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, in := range inputs {
		domain, full := parseSiteInput(in)
		if domain == "" || seen[full] {
			continue
		}
		seen[full] = true
		out = append(out, in)
	}
	return out
}

func familyNet(family string) string {
	if family == "ipv6" {
		return "ip6"
	}
	return "ip4"
}

func familyRecord(family string) string {
	if family == "ipv6" {
		return "AAAA"
	}
	return "A"
}

func (s *Suite) recordType() string {
	if s.Options.IPVersion == "ipv6" {
		return "AAAA"
	}
	return "A"
}

func (s *Suite) families() []string {
	switch s.Options.IPVersion {
	case "ipv6":
		return []string{"ipv6"}
	case "both":
		return []string{"ipv4", "ipv6"}
	default:
		return []string{"ipv4"}
	}
}

func (s *Suite) resolveSystem(ctx context.Context, domain, family string) (string, error) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(rctx, familyNet(family), domain)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no address")
	}
	return ips[0].String(), nil
}

func (s *Suite) resolveHonest(ctx context.Context, domain, family string) string {
	rctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r := &netprobe.Resolver{Mark: int(s.directMark), Timeout: 4 * time.Second}
	out, err := r.ResolveResilient(rctx, domain, familyRecord(family))
	if err != nil || len(out.IPs) == 0 {
		return ""
	}
	return out.IPs[0]
}

func (s *Suite) runSites() {
	inputs := uniqueSites(s.Options.Sites)
	if len(inputs) == 0 {
		return
	}
	s.setProgress(ScopeSites, "")

	families := s.families()
	result := &SitesResult{}
	for _, in := range inputs {
		domain, full := parseSiteInput(in)
		for _, fam := range families {
			site := SiteResult{Input: in, Domain: domain, URL: full, Family: fam, Outcome: OutcomePending}
			if s.setLookup != nil {
				if m := s.setLookup(domain); m != nil {
					site.SetId, site.SetName, site.SetEnabled, site.SetDNS = m.Id, m.Name, m.Enabled, m.DNS
				}
			}
			result.Sites = append(result.Sites, site)
		}
	}
	s.mu.Lock()
	s.Sites = result
	s.mu.Unlock()

	log.DiscoveryLogf("[Detector] Sites: resolving %d names", len(inputs))
	s.resolveAll(result)
	if s.canceled() {
		return
	}
	if len(families) > 1 {
		s.mu.Lock()
		kept := result.Sites[:0]
		dropped := 0
		for _, site := range result.Sites {
			if site.Family == "ipv6" && site.IP == "" && site.HonestIP == "" {
				dropped++
				continue
			}
			kept = append(kept, site)
		}
		result.Sites = kept
		s.Progress.Total -= dropped * s.modes()
		s.mu.Unlock()
	}

	sem := make(chan struct{}, s.Options.Parallel)
	var wg sync.WaitGroup
	for i := range result.Sites {
		if s.canceled() {
			break
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.checkSite(result, idx)
		}(i)
	}
	wg.Wait()

	s.mu.Lock()
	s.tallySites(result)
	s.mu.Unlock()
	log.DiscoveryLogf("[Detector] Sites: %d ok, %d blocked by ISP, %d fixed by b4, %d still blocked",
		result.Ok, result.Blocked, result.Fixed, result.StillBlocked)
}

func (s *Suite) resolveAll(result *SitesResult) {
	type resolved struct {
		sys, honest string
		sysErr      error
	}
	res := make([]resolved, len(result.Sites))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i := range result.Sites {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			domain := result.Sites[idx].Domain
			fam := result.Sites[idx].Family
			sys, err := s.resolveSystem(s.ctx, domain, fam)
			honest := s.resolveHonest(s.ctx, domain, fam)
			if isFakeRange(honest) {
				honest = ""
			}
			res[idx] = resolved{sys: sys, honest: honest, sysErr: err}
		}(i)
	}
	wg.Wait()

	byIP := make(map[string]map[string]bool)
	for i, r := range res {
		if r.sys == "" || r.honest == "" || r.sys == r.honest || sameNet(r.sys, r.honest) {
			continue
		}
		if byIP[r.sys] == nil {
			byIP[r.sys] = make(map[string]bool)
		}
		byIP[r.sys][result.Sites[i].Domain] = true
	}
	stubs := make(map[string]bool)
	for ip, domains := range byIP {
		if len(domains) >= 2 || isFakeRange(ip) {
			stubs[ip] = true
		}
	}

	s.mu.Lock()
	for i := range result.Sites {
		site := &result.Sites[i]
		r := res[i]
		site.IP = r.sys
		site.HonestIP = r.honest
		switch {
		case r.sys == "" && r.honest != "":
			site.FakeDNS = true
		case r.sys != "" && (isFakeRange(r.sys) || stubs[r.sys]):
			site.FakeDNS = true
		}
	}
	for ip := range stubs {
		result.StubIPs = append(result.StubIPs, ip)
	}
	sort.Strings(result.StubIPs)
	s.mu.Unlock()
}

func sameNet(a, b string) bool {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	if ia == nil || ib == nil {
		return false
	}
	if ia.To4() != nil && ib.To4() != nil {
		return ia.Mask(net.CIDRMask(24, 32)).Equal(ib.Mask(net.CIDRMask(24, 32)))
	}
	return ia.Mask(net.CIDRMask(48, 128)).Equal(ib.Mask(net.CIDRMask(48, 128)))
}

func (s *Suite) checkSite(result *SitesResult, idx int) {
	s.mu.RLock()
	site := result.Sites[idx]
	s.mu.RUnlock()

	s.setProgress(ScopeSites, site.Domain)
	s.mu.Lock()
	result.Sites[idx].Direct = &Fetch{Status: FetchChecking}
	s.mu.Unlock()

	direct := s.fetchMode(site, s.directMark, true)
	s.mu.Lock()
	result.Sites[idx].Direct = &direct
	result.Sites[idx].AltWorks = direct.AltWorks
	if s.Options.FetchMode == FetchBoth {
		result.Sites[idx].ThroughB4 = &Fetch{Status: FetchChecking}
	}
	s.mu.Unlock()
	s.step(1)

	var through *Fetch
	if s.Options.FetchMode == FetchBoth && !s.canceled() {
		t := s.fetchMode(site, markThroughB4, false)
		through = &t
		s.step(1)
	}

	s.mu.Lock()
	result.Sites[idx].ThroughB4 = through
	result.Sites[idx].Outcome = outcomeFor(&direct, through)
	result.Sites[idx].Done = true
	s.tallySites(result)
	s.mu.Unlock()
	s.refreshVerdict()
}

func (s *Suite) fetchMode(site SiteResult, mark uint, direct bool) Fetch {
	ctx := s.ctx
	if site.IP == "" && site.HonestIP == "" {
		return Fetch{Status: netprobe.DomainError, Detail: "name does not resolve"}
	}

	ip := site.IP
	if !direct && site.SetDNS && site.SetEnabled && site.HonestIP != "" {
		ip = site.HonestIP
	}
	if site.FakeDNS {
		if direct || !site.SetDNS || site.HonestIP == "" {
			f := Fetch{Status: netprobe.DomainDNSFake}
			switch {
			case site.IP == "":
				f.Detail = "the resolver returns no address, DoH answers " + site.HonestIP
			default:
				f.Detail = "the resolver answers " + site.IP + ", DoH answers " + site.HonestIP
			}
			if !direct && !site.SetDNS && site.SetName != "" {
				f.Detail += "; the set has no DNS redirect"
			}
			if direct && site.HonestIP != "" {
				real := s.fetchSite(ctx, site.Domain, site.URL, site.HonestIP, mark, 0)
				f.Detail += "; on the real address: " + strings.ToLower(string(real.Status))
				if real.Detail != "" && real.Status != FetchOk {
					f.Detail += " (" + real.Detail + ")"
				}
			}
			return f
		}
		ip = site.HonestIP
	}

	f := s.fetchSite(ctx, site.Domain, site.URL, ip, mark, 0)
	if !direct || s.canceled() {
		return f
	}
	if isBlockedStatus(f.Status) && site.HonestIP != "" && site.HonestIP != site.IP {
		alt := s.fetchSite(ctx, site.Domain, site.URL, site.HonestIP, mark, 0)
		if alt.Status == FetchOk {
			f.Detail += "; the address DoH returns (" + site.HonestIP + ") loads"
			f.AltWorks = true
		}
	}
	if isBlockedStatus(f.Status) && !s.Options.SkipTLS12 {
		t12 := s.fetchSite(ctx, site.Domain, site.URL, ip, mark, tls.VersionTLS12)
		f.TLS12 = t12.Status
	}
	if !s.canceled() {
		f.HTTP, f.HTTPDetail = s.probePlainHTTP(ctx, site.Domain, ip, mark)
	}
	return f
}

func outcomeFor(direct, through *Fetch) SiteOutcome {
	if direct == nil {
		return OutcomePending
	}
	dBlocked := isBlockedStatus(direct.Status)
	dOk := direct.Status == FetchOk
	if through == nil {
		switch {
		case dOk:
			return OutcomeOk
		case dBlocked:
			return OutcomeBlocked
		case direct.Status == FetchServer:
			return OutcomeServer
		default:
			return OutcomeError
		}
	}
	tBlocked := isBlockedStatus(through.Status)
	tOk := through.Status == FetchOk
	switch {
	case dOk && tOk:
		return OutcomeOk
	case dBlocked && tOk:
		return OutcomeFixed
	case dBlocked && tBlocked:
		return OutcomeStillBlocked
	case dOk && tBlocked:
		return OutcomeBrokenByB4
	case direct.Status == FetchServer || through.Status == FetchServer:
		return OutcomeServer
	case dBlocked:
		return OutcomeStillBlocked
	default:
		return OutcomeError
	}
}

func (s *Suite) tallySites(r *SitesResult) {
	r.Ok, r.Blocked, r.Fixed, r.StillBlocked, r.BrokenByB4, r.Server, r.Errors = 0, 0, 0, 0, 0, 0, 0
	for _, site := range r.Sites {
		if !site.Done {
			continue
		}
		switch site.Outcome {
		case OutcomeOk:
			r.Ok++
		case OutcomeFixed:
			r.Blocked++
			r.Fixed++
		case OutcomeStillBlocked, OutcomeBlocked:
			r.Blocked++
			r.StillBlocked++
		case OutcomeBrokenByB4:
			r.Ok++
			r.BrokenByB4++
		case OutcomeServer:
			r.Server++
		default:
			r.Errors++
		}
	}
}
