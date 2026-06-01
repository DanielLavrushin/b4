package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

type dohResponse struct {
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

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
	if dohURL == "" {
		result.DoHBlocked = true
		result.Summary = "All DoH servers are blocked"
		log.DiscoveryLogf("[Detector] All DoH servers blocked")
		return result
	}

	// Step 2: Find working UDP DNS server
	var udpServer string
	for _, srv := range UDPDNSServers {
		_, err := resolveUDP(ctx, s.mark, srv, "example.com")
		if err == nil {
			udpServer = srv
			result.UDPServer = srv
			log.DiscoveryLogf("[Detector] Using UDP DNS server: %s", srv)
			break
		}
	}
	if udpServer == "" {
		result.UDPBlocked = true
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

			udpIP, udpErr := resolveUDP(ctx, s.mark, udpServer, dom)
			if udpErr != nil {
				errMsg := udpErr.Error()
				if isNXDomain(errMsg) {
					dr.UDPIP = "NXDOMAIN"
					if trustedDoH {
						dr.Status = DNSFakeNXDomain
					} else {
						dr.Status = DNSInterception
					}
				} else {
					dr.UDPIP = "timeout"
					dr.Status = DNSInterception
				}
			} else {
				dr.UDPIP = udpIP
				switch getFakeIPType(udpIP) {
				case "fakeip":
					dr.Status = DNSFakeIP
				case "isp", "local":
					dr.Status = DNSSpoofing
				default:
					if trustedDoH && udpIP != "" && dohIP != udpIP {
						dr.Status = DNSSpoofing
					} else {
						dr.Status = DNSOk
					}
				}
			}

			// Track stub IPs
			if udpIP != "" && udpIP != "timeout" && udpIP != "NXDOMAIN" && udpIP != "error" {
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

	client := markedHTTPClient(mark, 5*time.Second)

	reqURL := fmt.Sprintf("%s?name=%s&type=A", serverURL, domain)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/dns-json")

	resp, err := client.Do(req)
	if err != nil {
		return resolveDoHWire(ctx, client, serverURL, domain)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var doh dohResponse
	if err := json.Unmarshal(body, &doh); err != nil {
		return resolveDoHWire(ctx, client, serverURL, domain)
	}

	for _, ans := range doh.Answer {
		if ans.Type == 1 {
			return ans.Data, nil
		}
	}

	return "", fmt.Errorf("no A record found")
}

func resolveUDP(ctx context.Context, mark uint, server, domain string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return markedDialer(mark, 5*time.Second).DialContext(ctx, "udp", server+":53")
		},
	}

	ips, err := resolver.LookupIPAddr(ctx, domain)
	if err != nil {
		return "", err
	}

	for _, ip := range ips {
		if ip.IP.To4() != nil {
			return ip.IP.String(), nil
		}
	}

	return "", fmt.Errorf("no IPv4 address found")
}

func isNXDomain(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "no such host") || strings.Contains(lower, "nxdomain")
}
