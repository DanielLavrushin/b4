package detector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

func (s *DetectorSuite) runDNSCheck(ctx context.Context) *DNSResult {
	log.DiscoveryLogf("[Detector] Starting DNS integrity check for %d domains", len(DNSCheckDomains))

	result := &DNSResult{
		Status: DNSOk,
	}

	// Step 1: Find working DoH server
	var dohURL string
	for _, srv := range DoHServers {
		ip, err := resolveDoH(ctx, s.mark, srv.URL, "example.com")
		if err == nil && ip != "" {
			dohURL = srv.URL
			result.DoHServer = srv.Name
			log.DiscoveryLogf("[Detector] Using DoH server: %s", srv.Name)
			break
		}
	}

	// Step 2: Find working UDP DNS server
	var udpServer string
	for _, srv := range UDPDNSServers {
		ans, err := resolveUDP(ctx, s.mark, srv, "example.com")
		if err == nil && ans.IP != "" {
			udpServer = srv
			result.UDPServer = srv
			log.DiscoveryLogf("[Detector] Using UDP DNS server: %s", srv)
			break
		}
	}

	switch {
	case dohURL == "" && udpServer == "":
		result.DoHBlocked = true
		result.UDPBlocked = true
		result.Status = DNSBothUnavail
		result.Summary = "Neither DoH nor UDP DNS resolution is available"
		log.DiscoveryLogf("[Detector] Both DoH and UDP DNS unavailable")
		return result
	case dohURL == "":
		result.DoHBlocked = true
		result.Status = DNSDoHBlocked
		result.Summary = "All DoH servers are blocked"
		log.DiscoveryLogf("[Detector] All DoH servers blocked")
		return result
	case udpServer == "":
		result.UDPBlocked = true
		result.Status = DNSInterception
		result.Summary = "All UDP DNS servers (port 53) are blocked"
		log.DiscoveryLogf("[Detector] All UDP DNS servers blocked")
		return result
	}

	// Step 3: Compare DNS results for each domain
	ipCount := make(map[string]int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	domainResults := make([]DNSDomainResult, len(DNSCheckDomains))

	sem := make(chan struct{}, 10) // limit concurrency

	for i, domain := range DNSCheckDomains {
		if s.isCanceled() {
			break
		}
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			dr := DNSDomainResult{Domain: dom}

			dohIP, dohErr := resolveDoH(ctx, s.mark, dohURL, dom)
			trustedDoH := dohErr == nil && dohIP != ""
			if dohErr != nil {
				dr.DoHIP = "error"
			} else {
				dr.DoHIP = dohIP
			}

			udp, udpErr := resolveUDP(ctx, s.mark, udpServer, dom)
			var udpIP string
			switch {
			case udpErr != nil:
				dr.UDPIP = "timeout"
				dr.Status = DNSInterception
			case udp.NXDomain:
				dr.UDPIP = "NXDOMAIN"
				if trustedDoH {
					dr.Status = DNSFakeNXDomain
				} else {
					dr.Status = DNSInterception
				}
			case udp.Empty:
				dr.UDPIP = "empty"
				if trustedDoH {
					dr.Status = DNSFakeEmpty
				} else {
					dr.Status = DNSInterception
				}
			default:
				udpIP = udp.IP
				dr.UDPIP = udpIP
				switch {
				case isFakeIP(udpIP):
					dr.Status = DNSFakeIP
				case trustedDoH && dohIP != udpIP:
					dr.Status = DNSSpoofing
				default:
					dr.Status = DNSOk
				}
			}

			// Track stub IPs
			if udpIP != "" {
				mu.Lock()
				ipCount[udpIP]++
				mu.Unlock()
			}

			domainResults[idx] = dr

			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()
		}(i, domain)
	}

	wg.Wait()

	// Detect stub IPs (IPs that appear for 2+ different domains)
	stubIPs := make(map[string]bool)
	for ip, count := range ipCount {
		if count >= 2 {
			stubIPs[ip] = true
			result.StubIPs = append(result.StubIPs, ip)
		}
	}

	// Mark stub IPs and count results
	for i := range domainResults {
		if stubIPs[domainResults[i].UDPIP] {
			domainResults[i].IsStubIP = true
			if domainResults[i].Status == DNSOk {
				domainResults[i].Status = DNSSpoofing
			}
		}
		switch domainResults[i].Status {
		case DNSOk:
			result.OkCount++
		case DNSSpoofing:
			result.SpoofCount++
		case DNSFakeIP:
			result.FakeIPCount++
		case DNSInterception, DNSFakeNXDomain, DNSFakeEmpty:
			result.InterceptCount++
		}
	}

	result.Domains = domainResults

	switch {
	case result.SpoofCount > 0:
		result.Status = DNSSpoofing
	case result.InterceptCount > 0:
		result.Status = DNSInterception
	case result.FakeIPCount > 0:
		result.Status = DNSFakeIP
	}

	total := len(domainResults)
	result.Summary = fmt.Sprintf("%d/%d OK, %d spoofed, %d intercepted",
		result.OkCount, total, result.SpoofCount, result.InterceptCount)
	if result.FakeIPCount > 0 {
		result.Summary += fmt.Sprintf(", %d fake-IP", result.FakeIPCount)
	}
	if len(result.StubIPs) > 0 {
		result.Summary += fmt.Sprintf(", %d stub IPs detected", len(result.StubIPs))
	}

	log.DiscoveryLogf("[Detector] DNS check complete: %s", result.Summary)
	return result
}

func resolveDoH(ctx context.Context, mark uint, serverURL, domain string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r := &netprobe.Resolver{Mark: int(mark), Timeout: 5 * time.Second}
	ips, err := r.ResolveDoHOnce(ctx, netprobe.DoHServer{URL: serverURL, Format: netprobe.DoHJSON}, domain, "A")
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no A record found")
	}
	return ips[0], nil
}

type udpResult struct {
	IP       string
	NXDomain bool
	Empty    bool
}

func resolveUDP(ctx context.Context, mark uint, server, domain string) (udpResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r := &netprobe.Resolver{Mark: int(mark), Timeout: 5 * time.Second}
	ans, err := r.ResolveUDPOnce(ctx, server, domain, "A")
	if err != nil {
		return udpResult{}, err
	}
	if ans.NXDomain {
		return udpResult{NXDomain: true}, nil
	}
	if len(ans.IPs) == 0 {
		return udpResult{Empty: true}, nil
	}
	return udpResult{IP: ans.IPs[0]}, nil
}
