package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/dns"
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

	client := markedHTTPClient(mark, 5*time.Second)

	reqURL := fmt.Sprintf("%s?name=%s&type=A", serverURL, domain)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/dns-json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var doh dohResponse
	if err := json.Unmarshal(body, &doh); err != nil {
		return "", err
	}

	for _, ans := range doh.Answer {
		if ans.Type == 1 {
			return ans.Data, nil
		}
	}

	return "", fmt.Errorf("no A record found")
}

type udpResult struct {
	IP       string
	NXDomain bool
	Empty    bool
}

func resolveUDP(ctx context.Context, mark uint, server, domain string) (udpResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := markedDialer(mark, 5*time.Second).DialContext(ctx, "udp", net.JoinHostPort(server, "53"))
	if err != nil {
		return udpResult{}, err
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	if _, err := conn.Write(dns.BuildAQuery(domain, 0x4242)); err != nil {
		return udpResult{}, err
	}

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return udpResult{}, err
	}
	resp := buf[:n]
	if len(resp) < 12 {
		return udpResult{}, fmt.Errorf("short DNS response")
	}

	if rcode := resp[3] & 0x0F; rcode == 3 {
		return udpResult{NXDomain: true}, nil
	}

	for _, ip := range dns.ParseResponseIPs(resp) {
		if v4 := ip.To4(); v4 != nil {
			return udpResult{IP: v4.String()}, nil
		}
	}

	return udpResult{Empty: true}, nil
}
