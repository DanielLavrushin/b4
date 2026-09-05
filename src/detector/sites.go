package detector

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
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

const maxAddresses = 3

func (s *Suite) resolveSystem(ctx context.Context, domain, family string) ([]string, error) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(rctx, familyNet(family), domain)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ip := range ips {
		if len(out) == maxAddresses {
			break
		}
		out = append(out, ip.String())
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no address")
	}
	return out, nil
}

func (s *Suite) resolveHonest(ctx context.Context, domain, family string) []string {
	rctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r := &netprobe.Resolver{Mark: int(s.directMark), Timeout: 4 * time.Second}
	out, err := r.ResolveResilient(rctx, domain, familyRecord(family))
	if err != nil {
		return nil
	}
	var ips []string
	for _, ip := range out.IPs {
		if isFakeRange(ip) || len(ips) == maxAddresses {
			continue
		}
		ips = append(ips, ip)
	}
	return ips
}

func first(ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	return ips[0]
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
					site.SetId, site.SetName, site.SetEnabled = m.Id, m.Name, m.Enabled
					site.SetDNS = m.DNS.Enabled || len(m.DNS.PinnedAddresses(domain)) > 0
					if s.setDNS == nil {
						s.setDNS = make(map[string]config.DNSConfig)
					}
					s.setDNS[m.Id] = m.DNS
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
		sys, honest, b4 []string
		b4Source        string
		sysErr          error
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
			b4, source := s.resolveThroughB4(s.ctx, result.Sites[idx], fam)
			res[idx] = resolved{sys: sys, honest: honest, b4: b4, b4Source: source, sysErr: err}
		}(i)
	}
	wg.Wait()

	byIP := make(map[string]map[string]bool)
	for i, r := range res {
		sys, honest := first(r.sys), first(r.honest)
		if sys == "" || honest == "" || overlaps(r.sys, r.honest) {
			continue
		}
		if byIP[sys] == nil {
			byIP[sys] = make(map[string]bool)
		}
		byIP[sys][result.Sites[i].Domain] = true
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
		site.IPs = r.sys
		site.IP = first(r.sys)
		site.HonestIPs = r.honest
		site.HonestIP = first(r.honest)
		site.B4IPs = r.b4
		site.B4Source = r.b4Source
		switch {
		case site.IP == "" && site.HonestIP != "":
			site.FakeDNS = true
		case site.IP != "" && (isFakeRange(site.IP) || stubs[site.IP]):
			site.FakeDNS = true
		}
	}
	for ip := range stubs {
		result.StubIPs = append(result.StubIPs, ip)
	}
	sort.Strings(result.StubIPs)
	s.mu.Unlock()
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y || sameNet(x, y) {
				return true
			}
		}
	}
	return false
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
	if direct.IP != "" {
		result.Sites[idx].IP = direct.IP
	}
	if s.Options.FetchMode == FetchBoth {
		result.Sites[idx].ThroughB4 = &Fetch{Status: FetchChecking}
	}
	s.mu.Unlock()
	s.step(1)

	var through *Fetch
	if s.Options.FetchMode == FetchBoth && !s.canceled() {
		var t Fetch
		if site.SetId != "" && site.SetEnabled {
			t = s.fetchMode(site, markThroughB4, false)
		} else {
			t = Fetch{Status: direct.Status, IP: direct.IP, Source: "none", Tried: direct.Tried, Blocked: direct.Blocked, LatencyMs: direct.LatencyMs, Bytes: direct.Bytes, StatusCode: direct.StatusCode, Detail: direct.Detail}
		}
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

	ips := site.IPs
	source := "system"
	if !direct && site.SetEnabled && len(site.B4IPs) > 0 {
		ips = site.B4IPs
		source = site.B4Source
	}
	if site.FakeDNS {
		if direct || source == "system" {
			f := Fetch{Status: netprobe.DomainDNSFake}
			switch {
			case site.IP == "":
				f.Detail = "the resolver returns no address, DoH answers " + site.HonestIP
			default:
				f.Detail = "the resolver answers " + site.IP + ", DoH answers " + site.HonestIP
			}
			if !direct && site.SetName != "" {
				f.Detail += "; the set has no DNS redirect or pin for it"
			}
			if direct && site.HonestIP != "" {
				real := s.fetchAny(ctx, site, site.HonestIPs, mark)
				f.Detail += "; on the real address: " + strings.ToLower(string(real.Status))
				if real.Detail != "" && real.Status != FetchOk {
					f.Detail += " (" + real.Detail + ")"
				}
			}
			return f
		}
	}

	f := s.fetchAny(ctx, site, ips, mark)
	f.Source = source
	if !direct || s.canceled() {
		return f
	}

	blocked := isBlockedStatus(f.Status)
	var wg sync.WaitGroup
	var alt Fetch
	tryAlt := blocked && len(site.HonestIPs) > 0 && !overlaps(site.HonestIPs, ips)
	if tryAlt {
		wg.Add(1)
		go func() {
			defer wg.Done()
			alt = s.fetchAny(ctx, site, site.HonestIPs, mark)
		}()
	}
	var t12 Fetch
	tryTLS12 := blocked && !s.Options.SkipTLS12
	if tryTLS12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t12 = s.fetchSite(ctx, site.Domain, site.URL, f.IP, mark, tls.VersionTLS12)
		}()
	}
	var httpStatus FetchStatus
	var httpDetail string
	wg.Add(1)
	go func() {
		defer wg.Done()
		httpStatus, httpDetail = s.probePlainHTTP(ctx, site.Domain, f.IP, mark)
	}()
	wg.Wait()

	if tryAlt && alt.Status == FetchOk {
		f.Detail += "; the address DoH returns (" + alt.IP + ") loads"
		f.AltWorks = true
	}
	if tryTLS12 {
		f.TLS12 = t12.Status
	}
	f.HTTP, f.HTTPDetail = httpStatus, httpDetail
	return f
}

func (s *Suite) fetchAny(ctx context.Context, site SiteResult, ips []string, mark uint) Fetch {
	if len(ips) == 0 {
		return Fetch{Status: netprobe.DomainError, Detail: "no address to try"}
	}
	var firstFail *Fetch
	var blocked []string
	var notes []string
	for _, ip := range ips {
		if s.canceled() {
			break
		}
		f := s.fetchSite(ctx, site.Domain, site.URL, ip, mark, 0)
		f.IP = ip
		if f.Status == FetchOk {
			f.Tried = append(append([]string{}, blocked...), ip)
			f.Blocked = blocked
			if len(blocked) > 0 {
				f.Detail = strings.Join(notes, "; ") + "; " + ip + " loads (" + f.Detail + ")"
			}
			return f
		}
		if isBlockedStatus(f.Status) {
			blocked = append(blocked, ip)
			notes = append(notes, ip+": "+f.Detail)
		}
		if firstFail == nil {
			copy := f
			firstFail = &copy
		}
	}
	f := *firstFail
	f.Tried = ips
	f.Blocked = blocked
	if len(ips) > 1 {
		f.Detail += "; " + strconv.Itoa(len(ips)) + " addresses tried"
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

func (s *Suite) resolveThroughB4(ctx context.Context, site SiteResult, family string) ([]string, string) {
	if site.SetId == "" || !site.SetEnabled {
		return nil, ""
	}
	dnsCfg, ok := s.setDNS[site.SetId]
	if !ok {
		return nil, ""
	}
	want6 := family == "ipv6"
	var pinned []string
	for _, pin := range dnsCfg.PinnedAddresses(site.Domain) {
		ip := net.ParseIP(pin)
		if ip == nil || (ip.To4() == nil) != want6 || isFakeRange(pin) {
			continue
		}
		pinned = append(pinned, pin)
		if len(pinned) == maxAddresses {
			break
		}
	}
	if len(pinned) > 0 {
		return pinned, "pins"
	}
	if !dnsCfg.Enabled {
		return nil, ""
	}
	qtype := uint16(1)
	if want6 {
		qtype = 28
	}
	var q dnsQuerier
	source := ""
	switch {
	case dnsCfg.DoHURL != "":
		q = &dohQuerier{client: netprobe.HTTPClient(int(s.directMark), dnsQueryTimeout+time.Second), url: dnsCfg.DoHURL}
		source = "doh"
	case net.ParseIP(dnsCfg.TargetDNS) != nil:
		q = &udpQuerier{mark: s.directMark, addr: net.JoinHostPort(dnsCfg.TargetDNS, "53")}
		source = "target"
	default:
		return nil, ""
	}
	defer q.close()
	body, err := q.query(ctx, site.Domain, qtype)
	ans := parseAnswer(body, err, familyRecord(family))
	var ips []string
	for _, ip := range ans.ips {
		if isFakeRange(ip) || len(ips) == maxAddresses {
			continue
		}
		ips = append(ips, ip)
	}
	if len(ips) == 0 {
		return nil, ""
	}
	return ips, source
}
