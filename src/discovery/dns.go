package discovery

import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/dns/endpoint"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/nfq"
)

//go:embed dns.json
var cdnJSON []byte

type CDNEntry struct {
	Match   []string `json:"match"`
	GeoIP   []string `json:"geoip"`
	GeoSite []string `json:"geosite"`
}

type DNSProber struct {
	domain    string
	tlsPort   int
	timeout   time.Duration
	pool      *nfq.Pool
	cfg       *config.Config
	flowMark  uint
	ipVersion string
	trusted   endpoint.Endpoint
	ref       referenceAnswer

	serves      func(context.Context, string) bool
	connectable func(context.Context, []string) bool
	gateway     func(context.Context, string) bool
}

type referenceAnswer struct {
	ips      []string
	serves   bool
	source   string
	trusted  bool
	overUDP  bool
	nxdomain bool
	nodata   bool
	failure  string
}

var ownAddress = func(addr netip.Addr) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if own, ok := netip.AddrFromSlice(ipnet.IP); ok && own.Unmap() == addr.Unmap() {
				return true
			}
		}
	}
	return false
}

const (
	gatewayProbeTimeout = 2 * time.Second
	connectableTimeout  = 5 * time.Second
)

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

func KnownServiceHosts(geosite []string) []string {
	loadCDNEntries()

	wanted := make(map[string]bool, len(geosite))
	for _, category := range geosite {
		if c := strings.ToLower(strings.TrimSpace(category)); c != "" {
			wanted[c] = true
		}
	}
	hosts := []string{}
	if len(wanted) == 0 {
		return hosts
	}

	seen := make(map[string]bool)
	for _, entry := range cdnEntries {
		matched := false
		for _, category := range entry.GeoSite {
			if wanted[strings.ToLower(category)] {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, pattern := range entry.Match {
			host := strings.ToLower(strings.TrimSpace(pattern))
			if host == "" || strings.Contains(host, "*") {
				continue
			}
			if !seen[host] {
				seen[host] = true
				hosts = append(hosts, host)
			}
			break
		}
	}
	return hosts
}

func (ds *DiscoverySuite) installedGeoCategories(geoip, geosite []string) ([]string, []string) {
	if ds.cfg == nil || ds.cfg.System.Geo.GeoIpPath == "" {
		if len(geoip) > 0 {
			log.DiscoveryLogf("  Geo: dropping geoip categories %v, no geoip database is installed", geoip)
		}
		geoip = nil
	}
	if ds.cfg == nil || ds.cfg.System.Geo.GeoSitePath == "" {
		if len(geosite) > 0 {
			log.DiscoveryLogf("  Geo: dropping geosite categories %v, no geosite database is installed", geosite)
		}
		geosite = nil
	}
	return geoip, geosite
}

func (ds *DiscoverySuite) runDNSDiscoveryForDomain(di DomainInput) *DNSDiscoveryResult {
	if pins := ds.pinnedFor(di.Domain); len(pins) > 0 {
		return ds.pinnedResult(di, pins)
	}

	log.DiscoveryLogf("  DNS: Checking DNS poisoning for %s", di.Domain)

	port := checkURLPort(di.CheckURL)
	tlsPort := checkURLTLSPort(di.CheckURL)
	prober := NewDNSProber(
		di.Domain,
		port,
		tlsPort,
		time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec)*time.Second,
		ds.pool,
		ds.cfg,
		ds.flowMark,
		ds.ipVersion,
	)
	prober.trusted = ds.trusted

	ctx, cancel := ds.fetchContext(30 * time.Second)
	defer cancel()

	result := prober.Probe(ctx)
	if prober.ipNetwork() == "ip4" && asciiName(di.Domain) && shouldScanAlternatives(result) {
		ds.findAlternativeAddresses(di.Domain, port, tlsPort, result)
	}
	pinReferenceWhenNoFix(di.Domain, result)
	return result
}

func (ds *DiscoverySuite) pinnedResult(di DomainInput, pins []string) *DNSDiscoveryResult {
	log.DiscoveryLogf("  DNS: %s is pinned to %v, its DNS is not checked", di.Domain, pins)
	result := &DNSDiscoveryResult{
		ProbeResults:   []DNSProbeResult{},
		ExpectedIPs:    append([]string(nil), pins...),
		AlternativeIPs: append([]string(nil), pins...),
		Pinned:         true,
	}
	if ds.cfg == nil {
		return result
	}
	prober := NewDNSProber(
		di.Domain,
		checkURLPort(di.CheckURL),
		checkURLTLSPort(di.CheckURL),
		time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec)*time.Second,
		ds.pool,
		ds.cfg,
		ds.flowMark,
		ds.ipVersion,
	)
	ctx, cancel := ds.fetchContext(connectableTimeout + time.Second)
	defer cancel()
	if !prober.connectable(ctx, pins) {
		result.TransportBlocked = true
		log.DiscoveryLogf("  ✗ DNS: none of the pinned addresses of %s accepts a TCP connection", di.Domain)
	}
	return result
}

func pinReferenceWhenNoFix(domain string, result *DNSDiscoveryResult) {
	if result == nil || !result.IsPoisoned || result.hasWorkingConfig() || len(result.AlternativeIPs) > 0 || !result.referenceTrusted {
		return
	}
	var pins []string
	for _, ip := range result.referenceIPs {
		if !refusedProbeIP(ip) && !result.isGateway(ip) {
			pins = appendUnique(pins, ip)
		}
	}
	if len(pins) == 0 {
		return
	}
	result.AlternativeIPs = pins
	log.DiscoveryLogf("  DNS: no DNS server tested answers %s honestly; the addresses %s gave, %v, will be pinned in the set", domain, result.Reference, pins)
}

func (ds *DiscoverySuite) applyBestDNSConfig() {
	for _, wantDoH := range []bool{true, false} {
		for _, di := range ds.Domains {
			r := ds.dnsResults[di.Domain]
			if r == nil || !r.IsPoisoned {
				continue
			}
			switch {
			case wantDoH && r.BestDoHURL != "":
				ds.discoveredDNS = config.DNSConfig{Enabled: true, DoHURL: r.BestDoHURL}
				log.DiscoveryLogf("  Applied DNS bypass: DoH=%s", r.BestDoHURL)
				return
			case !wantDoH && r.BestServer != "":
				ds.discoveredDNS = config.DNSConfig{Enabled: true, TargetDNS: r.BestServer, FragmentQuery: r.NeedsFragment}
				log.DiscoveryLogf("  Applied DNS bypass: server=%s, fragment=%v", r.BestServer, r.NeedsFragment)
				return
			}
		}
	}
}

func (r *DNSDiscoveryResult) hasWorkingConfig() bool {
	if r == nil {
		return true
	}
	return !r.IsPoisoned || r.BestDoHURL != "" || r.BestServer != "" || r.NeedsFragment
}

func NewDNSProber(domain string, port, tlsPort int, timeout time.Duration, pool *nfq.Pool, cfg *config.Config, flowMark uint, ipVersion string) *DNSProber {
	p := &DNSProber{
		domain:    domain,
		tlsPort:   tlsPort,
		timeout:   timeout,
		pool:      pool,
		cfg:       cfg,
		flowMark:  flowMark,
		ipVersion: ipVersion,
	}
	p.serves = p.testIPServesDomain
	p.connectable = p.anyIPConnectable
	p.gateway = func(ctx context.Context, ip string) bool {
		return netprobe.GatewayProbe(ctx, ip, port, int(flowMark), gatewayProbeTimeout)
	}
	return p
}

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

func (p *DNSProber) dnsRecordType() string {
	if p.ipNetwork() == "ip6" {
		return "AAAA"
	}
	return "A"
}

func (p *DNSProber) family() string {
	if p.ipNetwork() == "ip6" {
		return "ipv6"
	}
	return "ipv4"
}

func (p *DNSProber) Probe(ctx context.Context) *DNSDiscoveryResult {
	var systemIPs []string
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
		refCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		p.ref = p.reference(refCtx)
	}()
	wg.Wait()

	result := p.evaluate(ctx, systemIPs, p.ref.ips, p.ref.serves)
	result.Reference = p.ref.source
	result.ReferenceError = p.ref.failure
	result.referenceIPs = p.ref.ips
	result.referenceTrusted = p.ref.trusted
	p.noteMissingAddress(result)
	return result
}

func (p *DNSProber) noteMissingAddress(result *DNSDiscoveryResult) {
	if !result.noAddress() {
		return
	}
	switch {
	case !asciiName(p.domain):
		log.DiscoveryLogf("  DNS: %s is an internationalized name, which the DNS check cannot query; the probes resolve it through the system resolver", p.domain)
	case p.ref.nxdomain:
		result.NXDomain = true
		log.DiscoveryLogf("  ✗ DNS: %s answers that %s does not exist (NXDOMAIN)", p.ref.source, p.domain)
	case p.ref.nodata:
		result.NoAddressFamily = p.family()
		log.DiscoveryLogf("  ✗ DNS: %s answers that %s exists but has no %s address", p.ref.source, p.domain, familyLabel(p.family()))
	case p.ref.failure != "":
	default:
		log.DiscoveryLogf("  ✗ DNS: no resolver returned an address for %s", p.domain)
	}
}

func asciiName(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func (p *DNSProber) evaluate(ctx context.Context, systemIPs, expectedIPs []string, referenceServes bool) *DNSDiscoveryResult {
	result := &DNSDiscoveryResult{
		ProbeResults:    []DNSProbeResult{},
		ReferenceServes: referenceServes,
	}

	if validIP := p.findValidIP(ctx, systemIPs); validIP != "" {
		log.DiscoveryLogf("  ✓ DNS OK: system IP %s serves %s", validIP, p.domain)
		result.SystemServes = true
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

	systemAnswered := len(systemIPs) > 0
	candidates := systemIPs
	if !referenceServes {
		candidates = uniqueIPs(systemIPs, expectedIPs)
	}
	terminated := p.probeGateways(ctx, candidates)
	result.GatewayIPs = terminated
	systemIPs = withoutIPs(systemIPs, terminated)
	expectedIPs = withoutIPs(expectedIPs, terminated)
	systemIntercepted := systemAnswered && len(systemIPs) == 0

	if len(expectedIPs) == 0 {
		if len(systemIPs) == 0 && len(result.GatewayIPs) > 0 {
			log.DiscoveryLogf("  ✗ DNS: every known address of %s is answered by the first hop in front of this host, nothing left to test", p.domain)
			return result
		}
		if len(systemIPs) > 0 {
			log.DiscoveryLogf("  DNS: no reference IPs available for %s, assuming OK", p.domain)
		}
		result.ExpectedIPs = systemIPs
		return result
	}
	log.DiscoveryLogf("  DNS: system IPs %v, reference IPs (DoH): %v", systemIPs, expectedIPs)

	if !p.connectable(ctx, expectedIPs) {
		log.DiscoveryLogf("  DNS: reference IPs for %s are unreachable at TCP level (transport issue or site down)", p.domain)
		result.TransportBlocked = true
		result.ExpectedIPs = uniqueIPs(expectedIPs, systemIPs)
		return result
	}

	if systemIntercepted {
		result.ExpectedIPs = append([]string(nil), expectedIPs...)
		if referenceServes {
			result.AlternativeIPs = append([]string(nil), expectedIPs...)
			log.DiscoveryLogf("  DNS: the system answer for %s is answered by the first hop in front of this host, the reference addresses %v serve the site from here and will be pinned", p.domain, expectedIPs)
		} else {
			log.DiscoveryLogf("  DNS: the system answer for %s is answered by the first hop in front of this host, that says nothing about the resolver; keeping the reference addresses %v as targets", p.domain, expectedIPs)
		}
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

	p.findDNSBypass(ctx, result, expectedIPs)
	return result
}

func (p *DNSProber) findDNSBypass(ctx context.Context, result *DNSDiscoveryResult, references []string) {
	if fix, ok := p.trustedSetDNS(); ok {
		result.BestDoHURL, result.BestServer = fix.DoHURL, fix.TargetDNS
		log.DiscoveryLogf("  DNS: the trusted DNS server %s answers %s honestly and becomes the set's DNS", p.trusted.String(), p.domain)
		return
	}
	if !p.trusted.IsZero() && len(p.ref.ips) > 0 {
		log.DiscoveryLogf("  DNS: the trusted DNS server %s answers %s honestly, but a set's DNS can only be an IP address on port 53 or an https:// URL; looking for a server a set can use", p.trusted.String(), p.domain)
	}

	if url := p.findDoHBypass(ctx, result, references); url != "" {
		result.BestDoHURL = url
		log.DiscoveryLogf("  DNS: DoH bypass works for %s via %s", p.domain, url)
		return
	}

	for _, server := range netprobe.FixDNSServers {
		for _, fragmented := range []bool{false, true} {
			if ctx.Err() != nil {
				log.DiscoveryLogf("  DNS: no time left to test more DNS servers for %s", p.domain)
				return
			}
			probe := p.testServer(ctx, server, fragmented, references)
			result.ProbeResults = append(result.ProbeResults, probe)
			if !probe.Works {
				continue
			}
			result.BestServer, result.NeedsFragment = server, fragmented
			if fragmented {
				log.DiscoveryLogf("  DNS: %s works with fragmented DNS to %s", p.domain, server)
			} else {
				log.DiscoveryLogf("  DNS: %s works with DNS %s", p.domain, server)
			}
			return
		}
	}

	log.DiscoveryLogf("  DNS: no working DNS config found for %s", p.domain)
}

func (p *DNSProber) trustedSetDNS() (config.DNSConfig, bool) {
	if p.trusted.IsZero() || len(p.ref.ips) == 0 {
		return config.DNSConfig{}, false
	}
	switch p.trusted.Transport {
	case endpoint.HTTPS:
		return config.DNSConfig{Enabled: true, DoHURL: p.trusted.URL}, true
	case endpoint.UDP, endpoint.TCPUDP:
		addr := p.trusted.Addr
		if !p.ref.overUDP || addr.Port() != endpoint.DefaultPort || addr.Addr().IsLoopback() || ownAddress(addr.Addr()) {
			return config.DNSConfig{}, false
		}
		return config.DNSConfig{Enabled: true, TargetDNS: addr.Addr().String()}, true
	}
	return config.DNSConfig{}, false
}

func (p *DNSProber) findDoHBypass(ctx context.Context, result *DNSDiscoveryResult, references []string) string {
	r := &netprobe.Resolver{Mark: int(p.flowMark), Timeout: p.timeout}
	recordType := p.dnsRecordType()

	for _, url := range netprobe.WireDoHServers {
		if ctx.Err() != nil {
			return ""
		}
		probe := DNSProbeResult{Server: url, ExpectedIP: firstIP(references)}
		ips, err := r.ResolveDoHOnce(ctx, netprobe.DoHServer{URL: url, Format: netprobe.DoHWire}, p.domain, recordType)
		if err == nil {
			if ip, ok := p.candidateWorks(ctx, ips, references); ok {
				probe.ResolvedIP, probe.Works = ip, true
				result.ProbeResults = append(result.ProbeResults, probe)
				return url
			}
			probe.IsPoisoned = true
		}
		result.ProbeResults = append(result.ProbeResults, probe)
	}

	return ""
}

func (p *DNSProber) testServer(ctx context.Context, server string, fragmented bool, references []string) DNSProbeResult {
	probe := DNSProbeResult{Server: server, Fragmented: fragmented, ExpectedIP: firstIP(references)}
	target := net.ParseIP(server)
	if p.pool == nil || target == nil {
		return probe
	}

	queryCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	qtype := uint16(1)
	if p.ipNetwork() == "ip6" {
		qtype = 28
	}
	id := uint16(rand.N(65535)) + 1
	start := time.Now()
	resp, err := p.pool.ForwardDNS(queryCtx, dns.BuildQuery(p.domain, id, qtype), target, fragmented)
	probe.Latency = time.Since(start)
	if err != nil || len(resp) < 12 || binary.BigEndian.Uint16(resp) != id {
		probe.IsPoisoned = true
		return probe
	}
	if ip, ok := p.candidateWorks(ctx, familyIPs(dns.ParseResponseIPs(resp), p.ipNetwork()), references); ok {
		probe.ResolvedIP, probe.Works = ip, true
		return probe
	}
	probe.IsPoisoned = true
	return probe
}

func (p *DNSProber) candidateWorks(ctx context.Context, ips, references []string) (string, bool) {
	for _, ip := range ips {
		if containsString(references, ip) {
			return ip, true
		}
	}
	if len(ips) > 0 && len(references) > 0 && sameSubnet(ips, references) {
		return ips[0], true
	}
	for _, ip := range ips {
		if p.serves(ctx, ip) {
			return ip, true
		}
	}
	return "", false
}

func familyIPs(ips []net.IP, network string) []string {
	var out []string
	for _, ip := range ips {
		if (network == "ip6") != (ip.To4() == nil) {
			continue
		}
		out = appendUnique(out, ip.String())
	}
	return out
}

func firstIP(ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	return ips[0]
}

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

	resolver := netprobe.MarkedResolver(int(p.flowMark), p.timeout/2)
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

func (p *DNSProber) reference(ctx context.Context) referenceAnswer {
	r := &netprobe.Resolver{Mark: int(p.flowMark), Timeout: p.timeout}

	if !p.trusted.IsZero() {
		ref := referenceAnswer{source: p.trusted.String(), trusted: true}
		ans, err := r.ResolveEndpoint(ctx, p.trusted, p.domain, p.dnsRecordType())
		ref.overUDP = ans.OverUDP
		var nx *netprobe.NXDomainError
		var nodata *netprobe.NoDataError
		switch {
		case errors.As(err, &nx):
			ref.nxdomain = true
		case errors.As(err, &nodata):
			ref.nodata = true
		case err != nil:
			ref.failure = err.Error()
			log.DiscoveryLogf("  ✗ DNS: the trusted DNS server %s did not answer for %s (%v), so DNS is not compared for it", ref.source, p.domain, err)
		default:
			ref.ips, ref.serves = p.validateReference(ctx, ans.IPs)
		}
		return ref
	}

	out, err := r.ResolveResilient(ctx, p.domain, p.dnsRecordType())
	var nx *netprobe.NXDomainError
	var nodata *netprobe.NoDataError
	switch {
	case errors.As(err, &nodata):
		return referenceAnswer{source: nodata.Server, trusted: true, nodata: true}
	case errors.As(err, &nx):
		return referenceAnswer{source: nx.Server, trusted: true, nxdomain: true}
	case err != nil || len(out.IPs) == 0:
		return referenceAnswer{}
	}

	ref := referenceAnswer{source: out.DoHURL, trusted: out.DoHURL != ""}
	if ref.source == "" {
		ref.source = out.UDPSrv
	}
	ref.ips, ref.serves = p.validateReference(ctx, out.IPs)
	return ref
}

func (p *DNSProber) validateReference(ctx context.Context, ips []string) ([]string, bool) {
	var validated []string
	for _, ip := range ips {
		if p.testIPServesDomain(ctx, ip) {
			log.Tracef("DNS: verified %s for %s", ip, p.domain)
			validated = append(validated, ip)
		}
	}
	if len(validated) > 0 {
		return validated, true
	}
	log.Tracef("DNS: TLS validation failed for %s, trusting the reference addresses: %v", p.domain, ips)
	return ips, false
}

func (p *DNSProber) findValidIP(ctx context.Context, ips []string) string {
	valCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, ip := range ips {
		if p.serves(valCtx, ip) {
			return ip
		}
	}
	return ""
}

func (p *DNSProber) probeGateways(ctx context.Context, ips []string) []string {
	if len(ips) == 0 || ctx.Err() != nil {
		return nil
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < gatewayProbeTimeout+connectableTimeout {
		log.DiscoveryLogf("  DNS: not enough time left to check whether the first hop answers TCP for %s, skipping", p.domain)
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, gatewayProbeTimeout)
	defer cancel()

	hits := make([]bool, len(ips))
	var wg sync.WaitGroup
	for i, ip := range ips {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			hits[i] = p.gateway(probeCtx, ip)
		}(i, ip)
	}
	wg.Wait()

	var terminated []string
	for i, ip := range ips {
		if !hits[i] {
			continue
		}
		terminated = appendUnique(terminated, ip)
		log.DiscoveryLogf("  ✗ [%s] %s: TCP is answered by the first hop in front of this host (a transparent proxy on the gateway), no packet strategy from this host can reach past it", p.domain, ip)
	}
	return terminated
}

func withoutIPs(ips, excluded []string) []string {
	if len(excluded) == 0 {
		return ips
	}
	kept := make([]string, 0, len(ips))
	for _, ip := range ips {
		if !containsString(excluded, ip) {
			kept = append(kept, ip)
		}
	}
	return kept
}

func (p *DNSProber) anyIPConnectable(ctx context.Context, ips []string) bool {
	connCtx, cancel := context.WithTimeout(ctx, connectableTimeout)
	defer cancel()
	dialer := probeDialer(int(p.flowMark), p.timeout/2, p.timeout)
	for _, ip := range ips {
		conn, err := dialer.DialContext(connCtx, "tcp", tlsAddress(ip, p.tlsPort))
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}

func (p *DNSProber) testIPServesDomain(ctx context.Context, ip string) bool {
	dialer := probeDialer(int(p.flowMark), p.timeout/2, p.timeout)
	conn, err := dialer.DialContext(ctx, "tcp", tlsAddress(ip, p.tlsPort))
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
