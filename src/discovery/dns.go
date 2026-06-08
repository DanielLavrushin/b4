package discovery

import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
)

//go:embed dns.json
var cdnJSON []byte

type dohResponse struct {
	Answer []struct {
		Data string `json:"data"`
		Type int    `json:"type"`
	} `json:"Answer"`
}

type CDNEntry struct {
	Match   []string `json:"match"`
	GeoIP   []string `json:"geoip"`
	GeoSite []string `json:"geosite"`
}

type DNSProber struct {
	domain    string
	timeout   time.Duration
	pool      *nfq.Pool
	cfg       *config.Config
	flowMark  uint
	ipVersion string
}

var (
	cdnEntries []CDNEntry
	cdnOnce    sync.Once
)

func loadCDNEntries() {
	cdnOnce.Do(func() {
		if err := json.Unmarshal(cdnJSON, &cdnEntries); err != nil {
			cdnEntries = []CDNEntry{}
		}
	})
}

func GetCDNCategories(domain string) (geoip, geosite []string) {
	loadCDNEntries()

	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	for _, entry := range cdnEntries {
		for _, pattern := range entry.Match {
			if strings.HasSuffix(pattern, ".*") {
				prefix := strings.TrimSuffix(pattern, ".*")
				if strings.HasPrefix(domain, prefix+".") || strings.Contains(domain, "."+prefix+".") {
					return entry.GeoIP, entry.GeoSite
				}
				continue
			}

			if domain == pattern || strings.HasSuffix(domain, "."+pattern) {
				return entry.GeoIP, entry.GeoSite
			}
		}
	}
	return nil, nil
}

func (ds *DiscoverySuite) runDNSDiscoveryForDomain(domain string) *DNSDiscoveryResult {
	log.DiscoveryLogf("  DNS: Checking DNS poisoning for %s", domain)

	prober := NewDNSProber(
		domain,
		time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec)*time.Second,
		ds.pool,
		ds.cfg,
		ds.flowMark,
		ds.ipVersion,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return prober.Probe(ctx)
}

// applyBestDNSConfig applies the best DNS bypass config found across all domains.
func (ds *DiscoverySuite) applyBestDNSConfig() {
	var bestServer string
	needsFragment := false

	for _, dnsResult := range ds.dnsResults {
		if dnsResult == nil || !dnsResult.IsPoisoned {
			continue
		}
		if dnsResult.BestServer != "" {
			bestServer = dnsResult.BestServer
			needsFragment = dnsResult.NeedsFragment
			break
		}
		if dnsResult.NeedsFragment {
			needsFragment = true
		}
	}

	if bestServer != "" || needsFragment {
		ds.discoveredDNS = config.DNSConfig{
			Enabled:       true,
			TargetDNS:     bestServer,
			FragmentQuery: needsFragment,
		}
		log.DiscoveryLogf("  Applied DNS bypass: server=%s, fragment=%v", bestServer, needsFragment)
	}
}

func (r *DNSDiscoveryResult) hasWorkingConfig() bool {
	if r == nil {
		return true
	}
	return !r.IsPoisoned || r.BestServer != "" || r.NeedsFragment
}

func NewDNSProber(domain string, timeout time.Duration, pool *nfq.Pool, cfg *config.Config, flowMark uint, ipVersion string) *DNSProber {
	return &DNSProber{
		domain:    domain,
		timeout:   timeout,
		pool:      pool,
		cfg:       cfg,
		flowMark:  flowMark,
		ipVersion: ipVersion,
	}
}

// ipNetwork returns the resolver network ("ip4"/"ip6") for this probe run.
// An explicit per-run ipVersion wins; "auto" preserves the queue-config default.
func (p *DNSProber) ipNetwork() string {
	switch p.ipVersion {
	case "ipv4":
		return "ip4"
	case "ipv6":
		return "ip6"
	}
	if p.cfg.Queue.IPv6Enabled && !p.cfg.Queue.IPv4Enabled {
		return "ip6"
	}
	return "ip4"
}

// dnsRecordType returns the DoH record type ("A"/"AAAA") matching ipNetwork.
func (p *DNSProber) dnsRecordType() string {
	if p.ipNetwork() == "ip6" {
		return "AAAA"
	}
	return "A"
}

func (p *DNSProber) Probe(ctx context.Context) *DNSDiscoveryResult {
	result := &DNSDiscoveryResult{
		ProbeResults: []DNSProbeResult{},
	}

	// Run system resolver and DoH in parallel.
	var expectedIPs, systemIPs []string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sysCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		systemIPs = p.getSystemResolverIPs(sysCtx)
	}()
	go func() {
		defer wg.Done()
		dohCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		expectedIPs = p.getExpectedIPs(dohCtx)
	}()
	wg.Wait()

	if validIP := p.findValidIP(ctx, systemIPs); validIP != "" {
		log.DiscoveryLogf("  ✓ DNS OK: system IP %s serves %s", validIP, p.domain)
		result.ExpectedIPs = uniqueIPs(systemIPs, expectedIPs)
		result.ProbeResults = append(result.ProbeResults, DNSProbeResult{
			ResolvedIP: validIP,
			Works:      true,
		})
		return result
	}
	if len(systemIPs) > 0 {
		log.DiscoveryLogf("  DNS: system IPs %v failed TLS validation for %s", systemIPs, p.domain)
	}

	if len(expectedIPs) == 0 {
		log.DiscoveryLogf("  DNS: no reference IPs available for %s, assuming OK", p.domain)
		result.ExpectedIPs = systemIPs
		return result
	}
	log.DiscoveryLogf("  DNS: system IPs %v, reference IPs (DoH): %v", systemIPs, expectedIPs)

	if p.findValidIP(ctx, expectedIPs) == "" {
		log.DiscoveryLogf("  DNS: neither system nor reference IPs serve %s (transport issue or site down)", p.domain)
		result.TransportBlocked = true
		result.ExpectedIPs = uniqueIPs(expectedIPs, systemIPs)
		return result
	}

	if len(systemIPs) > 0 && sameSubnet(systemIPs, expectedIPs) {
		log.DiscoveryLogf("  ✓ DNS: system IPs in same subnet as reference (CDN variance, not poisoned)")
		result.ExpectedIPs = uniqueIPs(expectedIPs, systemIPs)
		return result
	}

	result.IsPoisoned = true
	result.ExpectedIPs = uniqueIPs(expectedIPs, systemIPs)
	log.DiscoveryLogf("  ✗ DNS poisoned: system IPs %v don't serve %s and differ from reference %v", systemIPs, p.domain, expectedIPs)

	sysResult := DNSProbeResult{
		Server:     "",
		ExpectedIP: expectedIPs[0],
		IsPoisoned: true,
	}
	if len(systemIPs) > 0 {
		sysResult.ResolvedIP = systemIPs[0]
	}
	result.ProbeResults = append(result.ProbeResults, sysResult)

	// Step 6: Try DNS bypass strategies.
	p.findDNSBypass(ctx, result, expectedIPs[0])
	return result
}

// findDNSBypass tries fragmentation and alternative DNS servers to bypass poisoning.
func (p *DNSProber) findDNSBypass(ctx context.Context, result *DNSDiscoveryResult, expectedIP string) {
	// Try fragmented query on system resolver first.
	fragResult := p.testDNSWithFragment(ctx, "", expectedIP)
	result.ProbeResults = append(result.ProbeResults, fragResult)
	if fragResult.Works {
		result.NeedsFragment = true
		log.DiscoveryLogf("  DNS: fragmented query bypass works for %s", p.domain)
		return
	}

	// Try alternative DNS servers (plain and fragmented).
	for _, server := range p.cfg.System.Checker.ReferenceDNS {
		plainResult := p.testDNS(ctx, server, false, expectedIP)
		result.ProbeResults = append(result.ProbeResults, plainResult)
		if plainResult.Works {
			result.BestServer = server
			log.DiscoveryLogf("  DNS: %s works with DNS %s", p.domain, server)
			return
		}

		fragAltResult := p.testDNSWithFragment(ctx, server, expectedIP)
		result.ProbeResults = append(result.ProbeResults, fragAltResult)
		if fragAltResult.Works {
			result.BestServer = server
			result.NeedsFragment = true
			log.DiscoveryLogf("  DNS: %s works with fragmented DNS to %s", p.domain, server)
			return
		}
	}

	log.DiscoveryLogf("  DNS: no working DNS config found for %s", p.domain)
}

// sameSubnet checks if any system IP shares a /24 (IPv4) or /48 (IPv6) subnet
// with any reference IP. Same-subnet IPs are CDN edge variance, not DNS poisoning.
func sameSubnet(systemIPs, referenceIPs []string) bool {
	refSubnets := make(map[string]bool)
	for _, ipStr := range referenceIPs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			refSubnets[fmt.Sprintf("%d.%d.%d", v4[0], v4[1], v4[2])] = true
		} else if len(ip) == net.IPv6len {
			refSubnets[fmt.Sprintf("%x%x%x", ip[0:2], ip[2:4], ip[4:6])] = true
		}
	}

	for _, ipStr := range systemIPs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		var key string
		if v4 := ip.To4(); v4 != nil {
			key = fmt.Sprintf("%d.%d.%d", v4[0], v4[1], v4[2])
		} else if len(ip) == net.IPv6len {
			key = fmt.Sprintf("%x%x%x", ip[0:2], ip[2:4], ip[4:6])
		}
		if key != "" && refSubnets[key] {
			return true
		}
	}
	return false
}

// uniqueIPs merges two IP lists, deduplicating entries.
func uniqueIPs(primary, secondary []string) []string {
	seen := make(map[string]bool, len(primary))
	result := make([]string, 0, len(primary)+len(secondary))
	for _, ip := range primary {
		if !seen[ip] {
			seen[ip] = true
			result = append(result, ip)
		}
	}
	for _, ip := range secondary {
		if !seen[ip] {
			seen[ip] = true
			result = append(result, ip)
		}
	}
	return result
}

func (p *DNSProber) getSystemResolverIPs(ctx context.Context) []string {
	network := p.ipNetwork()

	resolver := markedResolver(p.flowMark, p.timeout/2, "")
	ips, err := resolver.LookupIP(ctx, network, p.domain)
	if err != nil {
		log.DiscoveryLogf("  DNS: system resolver error: %v", err)
		return nil
	}
	if len(ips) == 0 {
		log.DiscoveryLogf("  DNS: system resolver returned no IPs")
		return nil
	}

	seen := make(map[string]bool)
	var result []string
	for _, ip := range ips {
		s := ip.String()
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	log.DiscoveryLogf("  DNS: system resolver returned IPs: %v", result)
	return result
}

func (p *DNSProber) getExpectedIPs(ctx context.Context) []string {
	recordType := p.dnsRecordType()

	dohServers := []string{
		"https://dns.google/resolve?name=%s&type=" + recordType,
		"https://dns.quad9.net:5053/dns-query?name=%s&type=" + recordType,
		"https://cloudflare-dns.com/dns-query?name=%s&type=" + recordType,
	}

	client := &http.Client{
		Timeout: p.timeout,
		Transport: &http.Transport{
			DialContext: markedDialer(p.flowMark, p.timeout/2, p.timeout).DialContext,
		},
	}

	seenIPs := make(map[string]bool)
	var allIPs []string

	for _, endpoint := range dohServers {
		url := fmt.Sprintf(endpoint, p.domain)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/dns-json")

		resp, err := client.Do(req)
		if err != nil {
			log.Tracef("DoH %s failed: %v", endpoint, err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var doh dohResponse
		if err := json.Unmarshal(body, &doh); err != nil {
			continue
		}

		wantType := 1
		if recordType == "AAAA" {
			wantType = 28
		}

		unvalidatedIPs := []string{}
		for _, ans := range doh.Answer {
			if ans.Type == wantType {
				ip := ans.Data
				if seenIPs[ip] {
					continue
				}
				seenIPs[ip] = true
				unvalidatedIPs = append(unvalidatedIPs, ip)

				if p.testIPServesDomain(ctx, ip) {
					log.Tracef("DoH: verified %s for %s", ip, p.domain)
					allIPs = append(allIPs, ip)
				}
			}
		}

		if len(allIPs) == 0 && len(unvalidatedIPs) > 0 {
			log.Tracef("DoH: TLS validation failed, trusting unvalidated IPs: %v", unvalidatedIPs)
			allIPs = unvalidatedIPs
		}

		if len(allIPs) > 0 {
			break
		}
	}

	if len(allIPs) == 0 {
		ip := p.getExpectedIPFallback(ctx)
		if ip != "" {
			return []string{ip}
		}
		return nil
	}

	return allIPs
}

func (p *DNSProber) getExpectedIPFallback(ctx context.Context) string {
	network := p.ipNetwork()

	for _, server := range p.cfg.System.Checker.ReferenceDNS {
		resolver := markedResolver(p.flowMark, p.timeout/3, server)

		ips, err := resolver.LookupIP(ctx, network, p.domain)
		if err == nil && len(ips) > 0 {
			ip := ips[0].String()
			if p.testIPServesDomain(ctx, ip) {
				log.Tracef("DNS fallback: verified %s for %s from %s", ip, p.domain, server)
				return ip
			}
		}
	}
	return ""
}

func (p *DNSProber) testDNS(ctx context.Context, server string, fragmented bool, expectedIP string) DNSProbeResult {
	result := DNSProbeResult{
		Server:     server,
		Fragmented: fragmented,
		ExpectedIP: expectedIP,
	}

	resolver := markedResolver(p.flowMark, p.timeout, "")
	if server != "" {
		resolver = markedResolver(p.flowMark, p.timeout, server)
	}

	network := p.ipNetwork()

	start := time.Now()
	ips, err := resolver.LookupIP(ctx, network, p.domain)
	result.Latency = time.Since(start)

	if err != nil || len(ips) == 0 {
		result.IsPoisoned = true
		return result
	}

	result.ResolvedIP = ips[0].String()

	if expectedIP != "" && result.ResolvedIP == expectedIP {
		result.Works = true
	} else {
		result.Works = p.testIPServesDomain(ctx, result.ResolvedIP)
	}
	result.IsPoisoned = !result.Works

	return result
}

// findValidIP returns the first IP from the list that serves the domain (TLS handshake),
// or empty string if none work.
func (p *DNSProber) findValidIP(ctx context.Context, ips []string) string {
	valCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, ip := range ips {
		if p.testIPServesDomain(valCtx, ip) {
			return ip
		}
	}
	return ""
}

func (p *DNSProber) testIPServesDomain(ctx context.Context, ip string) bool {
	dialer := markedDialer(p.flowMark, p.timeout/2, p.timeout)
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return false
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         p.domain,
		InsecureSkipVerify: true,
	})

	err = tlsConn.HandshakeContext(ctx)
	if err != nil {
		return false
	}
	tlsConn.Close()
	return true
}

func (p *DNSProber) testDNSWithFragment(ctx context.Context, server string, expectedIP string) DNSProbeResult {
	result := DNSProbeResult{
		Server:     server,
		Fragmented: true,
		ExpectedIP: expectedIP,
	}

	// Apply DNS config to pool temporarily
	testCfg := p.buildDNSTestConfig(server, true)
	if err := p.pool.UpdateConfig(testCfg); err != nil {
		return result
	}
	defer p.pool.UpdateConfig(p.cfg) // Restore

	time.Sleep(time.Duration(p.cfg.System.Checker.ConfigPropagateMs) * time.Millisecond)

	// Use a timeout so we don't hang if fragmented DNS gets no response
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Now DNS queries should be fragmented via NFQ
	start := time.Now()
	resolver := markedResolver(p.flowMark, p.timeout/2, "")
	ips, err := resolver.LookupIPAddr(lookupCtx, p.domain)
	result.Latency = time.Since(start)

	if err != nil || len(ips) == 0 {
		return result
	}

	result.ResolvedIP = ips[0].IP.String()
	result.Works = p.testIPServesDomain(ctx, result.ResolvedIP)
	result.IsPoisoned = !result.Works

	return result
}

func (p *DNSProber) buildDNSTestConfig(targetDNS string, fragment bool) *config.Config {
	testSet := config.NewSetConfig()
	testSet.Name = "dns-test"
	testSet.Enabled = true
	testSet.Targets.SNIDomains = []string{p.domain}
	testSet.Targets.DomainsToMatch = []string{p.domain}

	testSet.DNS = config.DNSConfig{
		Enabled:       true,
		TargetDNS:     targetDNS,
		FragmentQuery: fragment,
	}

	return &config.Config{
		ConfigPath: p.cfg.ConfigPath,
		Queue:      p.cfg.Queue,
		System:     p.cfg.System,
		Sets:       []*config.SetConfig{&testSet},
	}
}
