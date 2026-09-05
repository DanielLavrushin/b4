package discovery

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	presetAltAddress  = "alt-address"
	presetDNSRedirect = "dns-redirect"

	altScanBudget          = 90 * time.Second
	altScanQueryTimeout    = 3 * time.Second
	altScanWorkers         = 16
	altScanTCPTimeout      = 2 * time.Second
	altScanTCPTries        = 2
	altScanTLSChecks       = 8
	altScanMaxAnswers      = 3
	altScanFallbackTargets = 2
	altScanGridStep        = 10
	altScanProbeSubnet     = "8.8.8.0/24"
)

var ecsResolvers = []string{
	"https://dns.google/dns-query",
	"https://8.8.8.8/dns-query",
	"udp://8.8.8.8:53",
}

type AltScanSummary struct {
	Resolver  string `json:"resolver"`
	Regions   int    `json:"regions"`
	Answered  int    `json:"answered"`
	Addresses int    `json:"addresses"`
	Reachable int    `json:"reachable"`
	Clean     int    `json:"clean"`
}

type altCandidate struct {
	ip      string
	latency time.Duration
}

func plainFixPreset(name string, family StrategyFamily) ConfigPreset {
	return ConfigPreset{
		Name:        name,
		Description: "No packet changes, only the DNS answer is corrected",
		Family:      family,
		Phase:       PhaseBaseline,
		Priority:    0,
		Config: config.SetConfig{
			TCP: config.TCPConfig{ConnBytesLimit: 19},
			UDP: config.UDPConfig{Mode: config.ConfigOff},
			Fragmentation: config.FragmentationConfig{
				Strategy: config.ConfigNone,
			},
			Faking: config.FakingConfig{SNI: false},
		},
	}
}

func publicIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return false
	}
	switch {
	case v4[0] == 0:
		return false
	case v4[0] == 100 && v4[1]&0xc0 == 64:
		return false
	case v4[0] == 192 && v4[1] == 0 && (v4[2] == 0 || v4[2] == 2):
		return false
	case v4[0] == 198 && v4[1]&0xfe == 18:
		return false
	case v4[0] == 198 && v4[1] == 51 && v4[2] == 100:
		return false
	case v4[0] == 203 && v4[1] == 0 && v4[2] == 113:
		return false
	case v4[0] >= 224:
		return false
	}
	return true
}

func ecsScanSubnets() []net.IPNet {
	var out []net.IPNet
	for i := 0; i <= 255; i += altScanGridStep {
		for j := 0; j <= 255; j += altScanGridStep {
			ip := net.IPv4(byte(i), byte(j), 0, 0)
			if !publicIPv4(ip) {
				continue
			}
			out = append(out, net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(16, 32)})
		}
	}
	return out
}

type ecsScanner struct {
	mark    int
	timeout time.Duration
	client  *http.Client
	dialer  *net.Dialer
	txid    uint32
	txidMu  sync.Mutex
}

func newECSScanner(mark int, timeout time.Duration) *ecsScanner {
	return &ecsScanner{
		mark:    mark,
		timeout: timeout,
		client:  dns.MarkedDoHClient(mark, timeout),
		dialer:  netprobe.Dialer(mark, timeout, 0),
	}
}

func (s *ecsScanner) nextTxid() uint16 {
	s.txidMu.Lock()
	defer s.txidMu.Unlock()
	s.txid++
	return uint16(s.txid&0x7fff) + 1
}

func (s *ecsScanner) query(ctx context.Context, resolver, domain string, subnet net.IPNet) ([]string, error) {
	q := dns.BuildQueryWithECS(domain, s.nextTxid(), 1, subnet)

	var resp []byte
	var err error
	if len(resolver) > 6 && resolver[:6] == "udp://" {
		resp, err = s.queryUDP(ctx, resolver[6:], q)
	} else {
		resp, err = dns.ResolveDoH(ctx, s.client, resolver, q)
	}
	if err != nil {
		return nil, err
	}

	var out []string
	for _, ip := range dns.ParseResponseIPs(resp) {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	return out, nil
}

func (s *ecsScanner) queryUDP(ctx context.Context, addr string, q []byte) ([]byte, error) {
	udpCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	conn, err := s.dialer.DialContext(udpCtx, "udp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := udpCtx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	if _, err := conn.Write(q); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (s *ecsScanner) pickResolver(ctx context.Context, domain string) string {
	_, probe, err := net.ParseCIDR(altScanProbeSubnet)
	if err != nil {
		return ""
	}
	for _, resolver := range ecsResolvers {
		if ctx.Err() != nil {
			return ""
		}
		if ips, err := s.query(ctx, resolver, domain, *probe); err == nil && len(ips) > 0 {
			return resolver
		}
	}
	return ""
}

func (s *ecsScanner) scan(ctx context.Context, resolver, domain string, subnets []net.IPNet) (map[string]int, int) {
	var mu sync.Mutex
	seen := map[string]int{}
	answered := 0

	jobs := make(chan net.IPNet)
	var wg sync.WaitGroup
	for w := 0; w < altScanWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for subnet := range jobs {
				ips, err := s.query(ctx, resolver, domain, subnet)
				if err != nil {
					continue
				}
				mu.Lock()
				answered++
				for _, ip := range ips {
					seen[ip]++
				}
				mu.Unlock()
			}
		}()
	}

feed:
	for _, subnet := range subnets {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- subnet:
		}
	}
	close(jobs)
	wg.Wait()
	return seen, answered
}

func (s *ecsScanner) tcpLatency(ctx context.Context, ip string) (time.Duration, bool) {
	best := time.Duration(0)
	ok := false
	for try := 0; try < altScanTCPTries; try++ {
		if ctx.Err() != nil {
			break
		}
		dialCtx, cancel := context.WithTimeout(ctx, altScanTCPTimeout)
		start := time.Now()
		conn, err := s.dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(ip, "443"))
		cancel()
		if err != nil {
			continue
		}
		conn.Close()
		took := time.Since(start)
		if !ok || took < best {
			best = took
		}
		ok = true
	}
	return best, ok
}

func (s *ecsScanner) reachable(ctx context.Context, ips []string) []altCandidate {
	var mu sync.Mutex
	var alive []altCandidate

	jobs := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < altScanWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range jobs {
				if latency, ok := s.tcpLatency(ctx, ip); ok {
					mu.Lock()
					alive = append(alive, altCandidate{ip: ip, latency: latency})
					mu.Unlock()
				}
			}
		}()
	}
feed:
	for _, ip := range ips {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- ip:
		}
	}
	close(jobs)
	wg.Wait()

	sort.Slice(alive, func(i, j int) bool { return alive[i].latency < alive[j].latency })
	return alive
}

func (s *ecsScanner) servesDomain(ctx context.Context, domain, ip string) bool {
	dialCtx, cancel := context.WithTimeout(ctx, altScanTCPTimeout*2)
	defer cancel()

	conn, err := s.dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return false
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{ServerName: domain, InsecureSkipVerify: true})
	if err := tlsConn.HandshakeContext(dialCtx); err != nil {
		return false
	}
	tlsConn.Close()
	return true
}

func (ds *DiscoverySuite) findAlternativeAddresses(domain string, result *DNSDiscoveryResult) {
	if result == nil || ds.cfg == nil {
		return
	}
	ctx, cancel := ds.fetchContext(altScanBudget)
	defer cancel()

	known := map[string]bool{}
	for _, ip := range result.ExpectedIPs {
		known[ip] = true
	}
	for _, probe := range result.ProbeResults {
		if probe.ResolvedIP != "" {
			known[probe.ResolvedIP] = true
		}
	}

	if result.TransportBlocked {
		log.DiscoveryLogf("  [%s] every known address is unreachable, asking DNS how other regions are answered", domain)
	} else {
		log.DiscoveryLogf("  [%s] the known addresses do not serve the site from here, asking DNS how other regions are answered", domain)
	}

	scanner := newECSScanner(int(ds.flowMark), altScanQueryTimeout)
	resolver := scanner.pickResolver(ctx, domain)
	if resolver == "" {
		log.DiscoveryLogf("  [%s] no ECS-capable resolver answered, alternative addresses unknown", domain)
		return
	}

	subnets := ecsScanSubnets()
	seen, answered := scanner.scan(ctx, resolver, domain, subnets)

	var candidates []string
	for ip := range seen {
		if known[ip] || !publicIPv4(net.ParseIP(ip)) {
			continue
		}
		candidates = append(candidates, ip)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if seen[candidates[i]] != seen[candidates[j]] {
			return seen[candidates[i]] > seen[candidates[j]]
		}
		return candidates[i] < candidates[j]
	})

	summary := &AltScanSummary{Resolver: resolver, Regions: len(subnets), Answered: answered, Addresses: len(seen)}
	result.AltScan = summary
	log.DiscoveryLogf("  [%s] ECS scan via %s: %d of %d regions answered, %d distinct addresses, %d not tried yet",
		domain, resolver, answered, len(subnets), len(seen), len(candidates))
	if len(candidates) == 0 || ctx.Err() != nil {
		return
	}

	alive := scanner.reachable(ctx, candidates)
	summary.Reachable = len(alive)
	if len(alive) == 0 {
		log.DiscoveryLogf("  ✗ [%s] none of the %d alternative addresses accepts a connection", domain, len(candidates))
		return
	}
	log.DiscoveryLogf("  [%s] %d of %d alternative addresses accept a connection, fastest %s at %s",
		domain, len(alive), len(candidates), alive[0].ip, alive[0].latency.Round(time.Millisecond))

	var serving []string
	for i, cand := range alive {
		if i >= altScanTLSChecks || len(serving) >= altScanMaxAnswers || ctx.Err() != nil {
			break
		}
		if scanner.servesDomain(ctx, domain, cand.ip) {
			serving = append(serving, cand.ip)
			log.DiscoveryLogf("  ✓ [%s] %s completes a TLS handshake for the site (%s)", domain, cand.ip, cand.latency.Round(time.Millisecond))
		} else {
			log.DiscoveryLogf("  ✗ [%s] %s accepts the connection but not the TLS handshake", domain, cand.ip)
		}
	}
	summary.Clean = len(serving)
	if len(serving) > 0 {
		result.AlternativeIPs = serving
		return
	}
	if !result.TransportBlocked {
		log.DiscoveryLogf("  ✗ [%s] the reachable addresses do not complete a TLS handshake either, the site needs a packet strategy on its own addresses", domain)
		return
	}
	for i, cand := range alive {
		if i >= altScanFallbackTargets {
			break
		}
		result.AlternativeIPs = append(result.AlternativeIPs, cand.ip)
	}
	log.DiscoveryLogf("  [%s] no reachable address completes a TLS handshake, keeping %v as targets for the packet strategies", domain, result.AlternativeIPs)
}

func shouldScanAlternatives(result *DNSDiscoveryResult) bool {
	if result == nil {
		return false
	}
	return result.TransportBlocked || (!result.SystemServes && !result.ReferenceServes)
}

func plainFixResult(dr *DomainDiscoveryResult) (string, *DomainPresetResult) {
	for _, name := range []string{presetAltAddress, presetDNSRedirect} {
		if r := dr.Results[name]; r != nil && r.Status == CheckStatusComplete {
			return name, r
		}
	}
	return "", nil
}

func (ds *DiscoverySuite) alternativeIPs(domain string) []string {
	r := ds.dnsResults[domain]
	if r == nil {
		return nil
	}
	return r.AlternativeIPs
}

func (ds *DiscoverySuite) pinsFor(domains []string) map[string][]string {
	var pins map[string][]string
	for _, domain := range domains {
		ips := ds.alternativeIPs(domain)
		if len(ips) == 0 {
			continue
		}
		if pins == nil {
			pins = map[string][]string{}
		}
		pins[config.NormalizePinDomain(domain)] = append([]string(nil), ips...)
	}
	return pins
}

func (ds *DiscoverySuite) anyAddressBlocked(domains []string) bool {
	for _, domain := range domains {
		if r := ds.dnsResults[domain]; r != nil && r.TransportBlocked {
			return true
		}
	}
	return false
}

func (ds *DiscoverySuite) plainFixSet(name string, family StrategyFamily) *config.SetConfig {
	if ds.plainSets == nil {
		ds.plainSets = map[string]*config.SetConfig{}
	}
	if set := ds.plainSets[name]; set != nil {
		return set
	}
	set := ds.buildTestConfigMulti(plainFixPreset(name, family)).Sets[0]
	ds.plainSets[name] = set
	return set
}

func (ds *DiscoverySuite) recordPlainFix(domain string, domainResult *DomainDiscoveryResult, result CheckResult) {
	if result.Status != CheckStatusComplete {
		return
	}
	dnsResult := ds.dnsResults[domain]
	if dnsResult == nil {
		return
	}

	name, family := "", StrategyFamily("")
	switch {
	case result.UsedIP != "" && containsString(dnsResult.AlternativeIPs, result.UsedIP):
		name, family = presetAltAddress, FamilyAltAddress
	case dnsResult.IsPoisoned && dnsResult.hasWorkingConfig():
		name, family = presetDNSRedirect, FamilyDNSRedirect
	default:
		return
	}
	if _, exists := domainResult.Results[name]; exists {
		return
	}
	domainResult.Results[name] = &DomainPresetResult{
		PresetName: name,
		Family:     family,
		Phase:      PhaseBaseline,
		Priority:   0,
		Status:     CheckStatusComplete,
		Duration:   result.Duration,
		Speed:      result.Speed,
		BytesRead:  result.BytesRead,
		StatusCode: result.StatusCode,
		Set:        ds.plainFixSet(name, family),
	}
	domainResult.BaselineWorks = false
	domainResult.BestPreset = name
	domainResult.BestSpeed = result.Speed
	domainResult.BestSuccess = true
	if name == presetAltAddress {
		log.DiscoveryLogf("  ★ [%s] loads through %s without any packet change", domain, result.UsedIP)
	} else {
		log.DiscoveryLogf("  ★ [%s] loads without any packet change once DNS is answered honestly", domain)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func (ds *DiscoverySuite) anyDomainNeedsBypass() bool {
	for _, di := range ds.Domains {
		if ds.needsBypass(di.Domain) {
			return true
		}
	}
	return false
}

func (ds *DiscoverySuite) needsBypass(domain string) bool {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()

	dr := ds.domainResults[domain]
	if dr == nil {
		return true
	}
	if dr.BaselineWorks {
		return false
	}
	if _, r := plainFixResult(dr); r != nil {
		return false
	}
	return true
}
