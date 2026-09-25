package detector

import (
	"context"
	"crypto/tls"
	"errors"
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

const (
	maxAddresses     = 3
	maxFailedRetries = 3
)

const (
	dnsErrTimeout       = "timeout"
	dnsErrNotFound      = "not_found"
	dnsErrServerFailure = "server_failure"
	dnsErrOther         = "error"
)

var (
	systemResolver      = net.DefaultResolver
	systemLookupTimeout = 5 * time.Second
	errNoAddress        = errors.New("no address")
)

func (s *Suite) resolveSystem(ctx context.Context, domain, family string) ([]string, error) {
	rctx, cancel := context.WithTimeout(ctx, systemLookupTimeout)
	defer cancel()
	ips, err := systemResolver.LookupIP(rctx, familyNet(family), domain)
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
		return nil, errNoAddress
	}
	return out, nil
}

func dnsErrorKind(err error) string {
	var dnsErr *net.DNSError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return dnsErrOther
	case errors.Is(err, context.DeadlineExceeded):
		return dnsErrTimeout
	case errors.Is(err, errNoAddress):
		return dnsErrNotFound
	case !errors.As(err, &dnsErr):
		return dnsErrOther
	case dnsErr.IsTimeout:
		return dnsErrTimeout
	case dnsErr.IsNotFound:
		return dnsErrNotFound
	}
	return dnsErrServerFailure
}

func dnsFailReason(kind string) string {
	switch kind {
	case dnsErrTimeout:
		return "no answer"
	case dnsErrNotFound:
		return "it answers that the name has no address"
	case dnsErrServerFailure:
		return "server failure or refusal"
	}
	return "the query failed"
}

var resolveHonest = func(ctx context.Context, mark uint, domain, family string) []string {
	rctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	r := &netprobe.Resolver{Mark: int(mark), Timeout: 4 * time.Second}
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
	result := &SitesResult{Resolvers: systemNameservers()}
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
	log.DiscoveryLogf("[Detector] Sites: %d ok, %d blocked by ISP, %d fixed by b4, %d still blocked, %d without an address from the resolver",
		result.Ok, result.Blocked, result.Fixed, result.StillBlocked, result.DNSFail)
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
			honest := resolveHonest(s.ctx, s.directMark, domain, fam)
			b4, source := s.resolveThroughB4(s.ctx, result.Sites[idx], fam)
			res[idx] = resolved{sys: sys, honest: honest, b4: b4, b4Source: source, sysErr: err}
		}(i)
	}
	wg.Wait()

	timeoutsInARow := 0
	for i := range res {
		if s.canceled() || timeoutsInARow == maxFailedRetries {
			break
		}
		if len(res[i].sys) > 0 || len(res[i].honest) == 0 || dnsErrorKind(res[i].sysErr) == dnsErrNotFound {
			continue
		}
		s.setProgress(ScopeSites, result.Sites[i].Domain)
		res[i].sys, res[i].sysErr = s.resolveSystem(s.ctx, result.Sites[i].Domain, result.Sites[i].Family)
		switch {
		case len(res[i].sys) > 0:
			timeoutsInARow = 0
		case dnsErrorKind(res[i].sysErr) == dnsErrTimeout:
			timeoutsInARow++
		}
	}

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
			site.DNSError = dnsErrorKind(r.sysErr)
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
	if direct.IP != "" && site.IP != "" {
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
	if site.IP == "" && site.HonestIP == "" {
		return Fetch{Status: netprobe.DomainError, Detail: "name does not resolve"}
	}

	ips := site.IPs
	source := "system"
	if !direct && site.SetEnabled && len(site.B4IPs) > 0 {
		ips = site.B4IPs
		source = site.B4Source
	}
	if source == "system" && site.IP == "" {
		if direct {
			return noAddressFetch(site, s.fetchAt(site, site.HonestIPs, "doh", mark, true))
		}
		f := Fetch{Status: FetchDNSFail, Detail: "the resolver gave no address (" + dnsFailReason(site.DNSError) + ")"}
		if site.SetName != "" && !site.SetDNS {
			f.Detail += "; the set has no DNS redirect or pin for it"
		}
		return f
	}
	if site.FakeDNS && source == "system" {
		f := Fetch{Status: netprobe.DomainDNSFake, Detail: "the resolver answers " + site.IP + ", DoH answers " + site.HonestIP}
		if !direct && site.SetName != "" {
			f.Detail += "; the set has no DNS redirect or pin for it"
		}
		if direct && site.HonestIP != "" {
			real := s.fetchAny(s.ctx, site, site.HonestIPs, mark)
			f.Detail += "; on the real address: " + strings.ToLower(string(real.Status))
			if real.Detail != "" && real.Status != FetchOk {
				f.Detail += " (" + real.Detail + ")"
			}
		}
		return f
	}
	return s.fetchAt(site, ips, source, mark, direct)
}

func noAddressFetch(site SiteResult, f Fetch) Fetch {
	if f.Status != FetchOk {
		return f
	}
	detail := "the resolver gave no address (" + dnsFailReason(site.DNSError) + "), DoH answers " + site.HonestIP + "; the site loads at " + f.IP
	if f.Detail != "" {
		detail += " (" + f.Detail + ")"
	}
	f.Status = FetchDNSFail
	f.Detail = detail
	return f
}

func (s *Suite) fetchAt(site SiteResult, ips []string, source string, mark uint, direct bool) Fetch {
	ctx := s.ctx
	f := s.fetchAny(ctx, site, ips, mark)
	f.Source = source
	if !direct || s.canceled() {
		return f
	}

	blocked := isBlockedStatus(f.Status)
	gateway := f.Status == netprobe.DomainGateway
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
	tryTLS12 := blocked && !gateway && !s.Options.SkipTLS12
	if tryTLS12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t12 = fetchAddress(s, ctx, site.Domain, site.URL, f.IP, mark, tls.VersionTLS12)
		}()
	}
	var httpStatus FetchStatus
	var httpDetail string
	if !gateway {
		wg.Add(1)
		go func() {
			defer wg.Done()
			httpStatus, httpDetail = probeHTTP(s, ctx, site.Domain, f.IP, mark)
		}()
	}
	wg.Wait()

	if tryAlt && alt.Status == FetchOk {
		f.Detail += "; the address DoH returns (" + alt.IP + ") loads"
		f.AltWorks = true
	}
	if tryTLS12 {
		f.TLS12 = t12.Status
	}
	if !gateway {
		f.HTTP, f.HTTPDetail = httpStatus, httpDetail
	}
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
		f := fetchAddress(s, ctx, site.Domain, site.URL, ip, mark, 0)
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
		if firstFail == nil || failureRank(f.Status) > failureRank(firstFail.Status) {
			copy := f
			firstFail = &copy
		}
	}
	if firstFail == nil {
		return Fetch{Status: FetchSkipped, Detail: "stopped before the address was tried"}
	}
	f := *firstFail
	f.Tried = ips
	f.Blocked = blocked
	if len(ips) > 1 {
		f.Detail += "; " + strconv.Itoa(len(ips)) + " addresses tried"
	}
	return f
}

func failureRank(st FetchStatus) int {
	switch {
	case st == netprobe.DomainGateway:
		return 1
	case isBlockedStatus(st):
		return 2
	}
	return 0
}

func outcomeFor(direct, through *Fetch) SiteOutcome {
	if direct == nil {
		return OutcomePending
	}
	if direct.Status == FetchDNSFail {
		if through != nil && isBlockedStatus(through.Status) {
			return OutcomeBrokenByB4
		}
		return OutcomeDNS
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
	r.Ok, r.Blocked, r.Fixed, r.StillBlocked, r.BrokenByB4, r.Server, r.DNSFail, r.Errors = 0, 0, 0, 0, 0, 0, 0, 0
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
		case OutcomeDNS:
			r.DNSFail++
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
