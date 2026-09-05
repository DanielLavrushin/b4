package detector

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	dnsQueryTimeout = 4 * time.Second
	dnsParallel     = 8
	egressName      = "whoami.akamai.net"
)

type dnsAnswer struct {
	ips     []string
	nx      bool
	empty   bool
	timeout bool
	err     bool
}

type serverRun struct {
	server  DNSServer
	router  bool
	probe   *DNSProbe
	answers map[string]dnsAnswer
	egress  string
}

type dnsQuerier interface {
	query(ctx context.Context, domain string, qtype uint16) ([]byte, error)
	close()
}

type udpQuerier struct {
	mark uint
	addr string
}

func (q *udpQuerier) query(ctx context.Context, domain string, qtype uint16) ([]byte, error) {
	qctx, cancel := context.WithTimeout(ctx, dnsQueryTimeout)
	defer cancel()
	conn, err := netprobe.Dialer(int(q.mark), dnsQueryTimeout, 0).DialContext(qctx, "udp", q.addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(dnsQueryTimeout))
	if _, err := conn.Write(dns.BuildQuery(domain, uint16(time.Now().UnixNano()), qtype)); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (q *udpQuerier) close() {}

type dohQuerier struct {
	client *http.Client
	url    string
}

func (q *dohQuerier) query(ctx context.Context, domain string, qtype uint16) ([]byte, error) {
	qctx, cancel := context.WithTimeout(ctx, dnsQueryTimeout)
	defer cancel()
	return dns.ResolveDoH(qctx, q.client, q.url, dns.BuildQuery(domain, 0, qtype))
}

func (q *dohQuerier) close() { q.client.CloseIdleConnections() }

type dotQuerier struct {
	mark uint
	host string
	port int
	conn *dotConn
}

func (q *dotQuerier) query(ctx context.Context, domain string, qtype uint16) ([]byte, error) {
	if q.conn == nil {
		c, err := dialDoT(ctx, q.mark, q.host, q.port, dnsQueryTimeout)
		if err != nil {
			return nil, err
		}
		q.conn = c
	}
	body, err := q.conn.query(dns.BuildQuery(domain, uint16(time.Now().UnixNano()), qtype), dnsQueryTimeout)
	if err != nil {
		q.conn.close()
		q.conn = nil
	}
	return body, err
}

func (q *dotQuerier) close() { q.conn.close() }

func (s *Suite) querierFor(srv DNSServer) dnsQuerier {
	switch srv.Kind {
	case "doh":
		u := srv.Address
		if srv.Port != 0 && srv.Port != 443 {
			if parsed, err := url.Parse(u); err == nil && parsed.Port() == "" {
				parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(srv.Port))
				u = parsed.String()
			}
		}
		return &dohQuerier{client: netprobe.HTTPClient(int(s.directMark), dnsQueryTimeout+time.Second), url: u}
	case "dot":
		return &dotQuerier{mark: s.directMark, host: srv.Address, port: srv.Port}
	default:
		port := srv.Port
		if port == 0 {
			port = 53
		}
		return &udpQuerier{mark: s.directMark, addr: net.JoinHostPort(srv.Address, strconv.Itoa(port))}
	}
}

func parseAnswer(body []byte, err error, recordType string) dnsAnswer {
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") || strings.Contains(msg, "i/o timeout") {
			return dnsAnswer{timeout: true}
		}
		return dnsAnswer{err: true}
	}
	if len(body) < 12 {
		return dnsAnswer{err: true}
	}
	if body[3]&0x0F == 3 {
		return dnsAnswer{nx: true}
	}
	var ips []string
	for _, ip := range dns.ParseResponseIPs(body) {
		if recordType == "AAAA" && ip.To4() != nil {
			continue
		}
		if recordType != "AAAA" && ip.To4() == nil {
			continue
		}
		ips = append(ips, ip.String())
	}
	if len(ips) == 0 {
		return dnsAnswer{empty: true}
	}
	return dnsAnswer{ips: ips}
}

func (s *Suite) runDNS() {
	lists := Lists()
	s.setProgress(ScopeDNS, "")

	var runs []*serverRun
	for _, addr := range readResolvConf() {
		runs = append(runs, &serverRun{
			server: DNSServer{Name: "Router " + addr, Brand: "Router", Address: addr, Kind: "udp"},
			router: true,
		})
	}
	for _, srv := range lists.DNSServers {
		runs = append(runs, &serverRun{server: srv})
	}
	if len(runs) == 0 {
		return
	}

	result := &DNSResult{Providers: []DNSProvider{}, RouterServers: readResolvConf()}
	s.mu.Lock()
	s.DNS = result
	s.mu.Unlock()
	log.DiscoveryLogf("[Detector] DNS: probing %d resolvers", len(runs))

	qtype := uint16(1)
	if s.recordType() == "AAAA" {
		qtype = 28
	}

	sem := make(chan struct{}, dnsParallel)
	var wg sync.WaitGroup
	for _, run := range runs {
		if s.canceled() {
			break
		}
		wg.Add(1)
		go func(r *serverRun) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.probeResolver(r, lists, qtype)
			s.step(1)
			s.mu.Lock()
			result.Providers = buildProviders(runs, false)
			s.mu.Unlock()
		}(run)
	}
	wg.Wait()

	truth := buildTruth(runs, lists.DNSCheckDomains)
	stubs := findStubs(runs, truth)
	for _, r := range runs {
		if r.probe == nil || r.probe.Status != DNSProbeOk {
			continue
		}
		judgeHonesty(r, truth, stubs)
	}

	s.setProgress(ScopeDNS, "egress")
	s.judgeEgress(runs)

	s.mu.Lock()
	result.Providers = buildProviders(runs, true)
	result.TruthAvailable = truth.available()
	for ip := range stubs {
		result.StubIPs = append(result.StubIPs, ip)
	}
	sort.Strings(result.StubIPs)
	hijackOrg := make(map[string]int)
	for _, r := range runs {
		p := r.probe
		if p == nil {
			continue
		}
		switch r.server.Kind {
		case "udp":
			if !r.router {
				result.UDPTotal++
				if p.Status == DNSProbeOk {
					result.UDPOk++
				}
			}
			if p.Hijacked {
				result.Hijacked++
				key := p.AnsweredByOrg
				if key == "" {
					key = "AS" + p.AnsweredByASN
				}
				hijackOrg[key]++
			}
		case "doh":
			result.DoHTotal++
			if p.Status == DNSProbeOk {
				result.DoHOk++
				if p.Honesty == HonestyHonest {
					result.HonestDoH = append(result.HonestDoH, r.server.Address)
				}
			}
		case "dot":
			result.DoTTotal++
			if p.Status == DNSProbeOk {
				result.DoTOk++
			}
		}
		if p.Honesty == HonestySubstituted {
			result.Substituting++
		}
	}
	best, bestN := "", 0
	for org, n := range hijackOrg {
		if n > bestN {
			best, bestN = org, n
		}
	}
	result.HijackedBy = best
	for _, r := range runs {
		if r.probe != nil && r.probe.Hijacked && (r.probe.AnsweredByOrg == best || "AS"+r.probe.AnsweredByASN == best) {
			result.HijackedByASN = r.probe.AnsweredByASN
			break
		}
	}
	s.mu.Unlock()
	log.DiscoveryLogf("[Detector] DNS: UDP %d/%d, DoH %d/%d, DoT %d/%d, hijacked %d, substituting %d",
		result.UDPOk, result.UDPTotal, result.DoHOk, result.DoHTotal, result.DoTOk, result.DoTTotal, result.Hijacked, result.Substituting)
}

func (s *Suite) probeResolver(r *serverRun, lists TargetLists, qtype uint16) {
	s.setProgress(ScopeDNS, r.server.Name)
	probe := &DNSProbe{Address: r.server.Address, Honesty: HonestyUnknown}
	answers := make(map[string]dnsAnswer)
	egress := ""
	defer func() {
		s.mu.Lock()
		r.probe = probe
		r.answers = answers
		r.egress = egress
		s.mu.Unlock()
	}()

	q := s.querierFor(r.server)
	defer q.close()

	best := -1.0
	var lastErr dnsAnswer
	answered := 0
	for _, dom := range lists.DNSTrustedDomains {
		if s.canceled() {
			return
		}
		start := time.Now()
		body, err := q.query(s.ctx, dom, qtype)
		ans := parseAnswer(body, err, s.recordType())
		if err == nil && !ans.err {
			answered++
			lat := float64(time.Since(start).Microseconds()) / 1000.0
			if best < 0 || lat < best {
				best = lat
			}
		} else {
			lastErr = ans
			if err != nil {
				probe.Detail = err.Error()
			}
		}
	}
	if answered == 0 {
		switch {
		case lastErr.timeout:
			probe.Status = DNSProbeTimeout
		case r.server.Kind != "udp":
			probe.Status = DNSProbeBlocked
		default:
			probe.Status = DNSProbeError
		}
		return
	}
	probe.Status = DNSProbeOk
	probe.LatencyMs = round1(best)
	probe.Detail = ""

	for _, dom := range lists.DNSCheckDomains {
		if s.canceled() {
			return
		}
		body, err := q.query(s.ctx, dom, qtype)
		answers[dom] = parseAnswer(body, err, s.recordType())
	}

	if r.server.Kind == "udp" {
		for attempt := 0; attempt < 2 && egress == ""; attempt++ {
			body, err := q.query(s.ctx, egressName, 1)
			if err == nil {
				if ips := dns.ParseResponseIPs(body); len(ips) > 0 {
					egress = ips[0].String()
				}
			}
		}
	}
}

type truthTable struct {
	votes     map[string]map[string]map[*serverRun]bool
	answering map[string]map[*serverRun]bool
}

func buildTruth(runs []*serverRun, domains []string) *truthTable {
	t := &truthTable{
		votes:     make(map[string]map[string]map[*serverRun]bool),
		answering: make(map[string]map[*serverRun]bool),
	}
	for _, r := range runs {
		if r.probe == nil || r.probe.Status != DNSProbeOk || r.server.Kind == "udp" {
			continue
		}
		for _, dom := range domains {
			ans := r.answers[dom]
			if len(ans.ips) == 0 {
				continue
			}
			if t.answering[dom] == nil {
				t.answering[dom] = make(map[*serverRun]bool)
			}
			t.answering[dom][r] = true
			for _, ip := range ans.ips {
				if isFakeRange(ip) {
					continue
				}
				if t.votes[dom] == nil {
					t.votes[dom] = make(map[string]map[*serverRun]bool)
				}
				if t.votes[dom][ip] == nil {
					t.votes[dom][ip] = make(map[*serverRun]bool)
				}
				t.votes[dom][ip][r] = true
			}
		}
	}
	return t
}

func (t *truthTable) available() bool {
	return len(t.answering) > 0
}

func (t *truthTable) forDomain(dom string, self *serverRun) []string {
	others := 0
	for r := range t.answering[dom] {
		if r != self {
			others++
		}
	}
	if others == 0 {
		return nil
	}
	var out []string
	for ip, voters := range t.votes[dom] {
		n := 0
		for r := range voters {
			if r != self {
				n++
			}
		}
		if n >= 2 || (n == 1 && others == 1) {
			out = append(out, ip)
		}
	}
	return out
}

func findStubs(runs []*serverRun, truth *truthTable) map[string]bool {
	stubs := make(map[string]bool)
	for _, r := range runs {
		if r.probe == nil || r.probe.Status != DNSProbeOk {
			continue
		}
		byIP := make(map[string]map[string]bool)
		for dom, ans := range r.answers {
			for _, ip := range ans.ips {
				if inTruth(ip, truth.forDomain(dom, r)) {
					continue
				}
				if byIP[ip] == nil {
					byIP[ip] = make(map[string]bool)
				}
				byIP[ip][dom] = true
			}
		}
		for ip, doms := range byIP {
			if len(doms) >= 2 || isFakeRange(ip) {
				stubs[ip] = true
			}
		}
	}
	return stubs
}

func inTruth(ip string, truth []string) bool {
	for _, t := range truth {
		if t == ip || sameNet(t, ip) {
			return true
		}
	}
	return false
}

func judgeHonesty(r *serverRun, truth *truthTable, stubs map[string]bool) {
	p := r.probe
	match, nx, silent, fake, other := 0, 0, 0, 0, 0
	for dom, ans := range r.answers {
		t := truth.forDomain(dom, r)
		if len(t) == 0 {
			continue
		}
		p.Checked++
		switch {
		case len(ans.ips) > 0:
			hit := false
			for _, ip := range ans.ips {
				if inTruth(ip, t) {
					hit = true
					break
				}
			}
			switch {
			case hit:
				match++
			case stubs[ans.ips[0]] || isFakeRange(ans.ips[0]):
				fake++
			default:
				other++
			}
		case ans.nx:
			nx++
		default:
			silent++
		}
	}
	if p.Checked == 0 {
		p.Honesty = HonestyUnknown
		return
	}
	p.Substituted = fake + silent + nx
	switch {
	case fake > 0 || silent > 0:
		p.Honesty = HonestySubstituted
	case nx > 0:
		p.Honesty = HonestyFiltered
	case other > 0 && match == 0:
		p.Honesty = HonestyDiffers
		p.Substituted = 0
	default:
		p.Honesty = HonestyHonest
		p.Substituted = 0
	}
}

func (s *Suite) judgeEgress(runs []*serverRun) {
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, r := range runs {
		if r.server.Kind != "udp" || r.probe == nil || r.probe.Status != DNSProbeOk || r.egress == "" {
			continue
		}
		wg.Add(1)
		go func(r *serverRun) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p := r.probe
			p.AnsweredBy = r.egress
			egress := lookupASN(s.ctx, s.directMark, r.egress)
			p.AnsweredByASN = egress.ASN
			p.AnsweredByOrg = egress.Org
			if r.router || egress.ASN == "" {
				return
			}
			own := lookupASN(s.ctx, s.directMark, r.server.Address)
			if own.ASN == egress.ASN {
				return
			}
			brand := strings.ToLower(strings.Fields(r.server.Brand)[0])
			if knownResolverOrg(egress.Org) || strings.Contains(strings.ToLower(egress.Org), brand) {
				return
			}
			p.Hijacked = true
		}(r)
	}
	wg.Wait()
}

func buildProviders(runs []*serverRun, final bool) []DNSProvider {
	order := []string{}
	byBrand := make(map[string]*DNSProvider)
	for _, r := range runs {
		if r.probe == nil {
			continue
		}
		key := r.server.Brand
		if r.router {
			key = r.server.Name
		}
		p, ok := byBrand[key]
		if !ok {
			p = &DNSProvider{Name: key, Router: r.router}
			byBrand[key] = p
			order = append(order, key)
		}
		probe := *r.probe
		switch r.server.Kind {
		case "doh":
			p.DoH = &probe
		case "dot":
			p.DoT = &probe
		default:
			p.UDP = &probe
		}
	}
	out := make([]DNSProvider, 0, len(order))
	for _, key := range order {
		out = append(out, *byBrand[key])
	}
	return out
}
