package detector

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

func (s *DetectorSuite) runDomainsCheck(ctx context.Context, stubIPs map[string]bool) *DomainsResult {
	log.DiscoveryLogf("[Detector] Starting domain accessibility check for %d domains", len(CheckDomains))

	result := &DomainsResult{}
	results := make([]DomainCheckResult, len(CheckDomains))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 20)

	for i, domain := range CheckDomains {
		if s.isCanceled() {
			break
		}
		wg.Add(1)
		go func(idx int, dom string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			dr := DomainCheckResult{Domain: dom}

			// Resolve IP first
			ips, err := net.LookupIP(dom)
			if err != nil || len(ips) == 0 {
				dr.Overall = DomainError
				dr.TLS13 = &TLSProbeResult{Status: DomainError, Detail: "DNS resolution failed"}
				dr.TLS12 = &TLSProbeResult{Status: DomainError, Detail: "DNS resolution failed"}
				dr.HTTP = &HTTPProbeResult{Status: DomainError, Detail: "DNS resolution failed"}
				results[idx] = dr
				s.mu.Lock()
				s.CompletedChecks += 3
				s.mu.Unlock()
				return
			}

			for _, ip := range ips {
				if ip.To4() != nil {
					dr.IP = ip.String()
					break
				}
			}
			if dr.IP == "" {
				dr.IP = ips[0].String()
			}

			// Check if IP is a known stub/fake
			if stubIPs[dr.IP] {
				dr.IsFakeIP = true
				dr.Overall = DomainDNSFake
				dr.TLS13 = &TLSProbeResult{Status: DomainDNSFake, Detail: "Resolved to stub IP"}
				dr.TLS12 = &TLSProbeResult{Status: DomainDNSFake, Detail: "Resolved to stub IP"}
				dr.HTTP = &HTTPProbeResult{Status: DomainDNSFake, Detail: "Resolved to stub IP"}
				results[idx] = dr
				s.mu.Lock()
				s.CompletedChecks += 3
				s.mu.Unlock()
				return
			}

			// Phase 1: TLS 1.3
			dr.TLS13 = s.probeTLS(ctx, dom, dr.IP, tls.VersionTLS13)
			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()

			if s.isCanceled() {
				results[idx] = dr
				return
			}

			// Phase 2: TLS 1.2
			dr.TLS12 = s.probeTLS(ctx, dom, dr.IP, tls.VersionTLS12)
			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()

			if s.isCanceled() {
				results[idx] = dr
				return
			}

			// Phase 3: HTTP injection check
			dr.HTTP = s.probeHTTP(ctx, dom, dr.IP)
			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()

			// Determine overall status
			dr.Overall = deriveOverallStatus(dr)
			results[idx] = dr
		}(i, domain)
	}

	wg.Wait()

	for _, dr := range results {
		result.Domains = append(result.Domains, dr)
		switch dr.Overall {
		case DomainOk:
			result.OkCount++
		case DomainTLSDPI, DomainTLSMITM, DomainTLSSpoof, DomainTLSAlert, DomainTLSReset,
			DomainTLSDrop, DomainSYNDrop, DomainTCP16, DomainBlocked, DomainISPPage, DomainDNSFake:
			result.BlockedCount++
			if isDPIStatus(dr.Overall) {
				result.DPICount++
			}
		}
	}

	result.Summary = fmt.Sprintf("%d/%d accessible, %d blocked (%d via DPI)",
		result.OkCount, len(results), result.BlockedCount, result.DPICount)

	log.DiscoveryLogf("[Detector] Domain check complete: %s", result.Summary)
	return result
}

func (s *DetectorSuite) probeTLS(ctx context.Context, domain, ip string, version uint16) *TLSProbeResult {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	start := time.Now()

	tlsConf := &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
		MinVersion:         version,
		MaxVersion:         version,
	}

	conn, err := markedDialer(s.mark, 10*time.Second).DialContext(ctx, "tcp", ip+":443")
	if err != nil {
		status, detail := ClassifyTLSErrorStaged(err, stageConnect, 0)
		return &TLSProbeResult{
			Status:  status,
			Detail:  detail,
			Latency: time.Since(start).Milliseconds(),
		}
	}

	tlsConn := tls.Client(conn, tlsConf)
	err = tlsConn.HandshakeContext(ctx)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		conn.Close()
		status, detail := ClassifyTLSErrorStaged(err, stageHandshake, 0)
		return &TLSProbeResult{
			Status:  status,
			Detail:  detail,
			Latency: latency,
		}
	}

	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", domain)
	tlsConn.SetDeadline(time.Now().Add(8 * time.Second))
	_, err = tlsConn.Write([]byte(req))
	if err != nil {
		tlsConn.Close()
		status, detail := ClassifyTLSErrorStaged(err, stageRead, 0)
		return &TLSProbeResult{
			Status:  status,
			Detail:  detail,
			Latency: latency,
		}
	}

	buf := make([]byte, 16384)
	bytesRead := 0
	var readErr error
	for bytesRead < tcp16MaxBytes+4096 {
		n, e := tlsConn.Read(buf)
		bytesRead += n
		if e != nil {
			readErr = e
			break
		}
	}
	tlsConn.Close()

	if readErr != nil && readErr != io.EOF {
		status, detail := ClassifyTLSErrorStaged(readErr, stageRead, bytesRead)
		return &TLSProbeResult{
			Status:  status,
			Detail:  detail,
			Latency: latency,
		}
	}

	versionStr := "TLS 1.3"
	if version == tls.VersionTLS12 {
		versionStr = "TLS 1.2"
	}

	return &TLSProbeResult{
		Status:  DomainOk,
		Detail:  versionStr + " connection successful",
		Latency: latency,
	}
}

func (s *DetectorSuite) probeHTTP(ctx context.Context, domain, ip string) *HTTPProbeResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return markedDialer(s.mark, 10*time.Second).DialContext(ctx, "tcp", ip+":80")
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+domain+"/", nil)
	if err != nil {
		return &HTTPProbeResult{Status: DomainError, Detail: err.Error()}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
			return &HTTPProbeResult{Status: DomainTimeout, Detail: "HTTP connection timed out"}
		}
		if strings.Contains(err.Error(), "connection reset") {
			return &HTTPProbeResult{Status: DomainBlocked, Detail: "Connection reset on HTTP"}
		}
		return &HTTPProbeResult{Status: DomainError, Detail: err.Error()}
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")

	// Read limited body
	bodyBytes := make([]byte, 8192)
	n, _ := io.ReadFull(resp.Body, bodyBytes)
	body := string(bodyBytes[:n])

	status, detail := ClassifyHTTPResponse(resp.StatusCode, location, body)
	if status != DomainOk {
		return &HTTPProbeResult{
			Status:     status,
			Detail:     detail,
			StatusCode: resp.StatusCode,
			RedirectTo: location,
		}
	}

	// Cross-domain redirect detection: ISP may redirect to a foreign domain
	if location != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if isCrossDomainRedirect(domain, location) {
			return &HTTPProbeResult{
				Status:     DomainISPPage,
				Detail:     "Cross-domain redirect to: " + location,
				StatusCode: resp.StatusCode,
				RedirectTo: location,
			}
		}
	}

	return &HTTPProbeResult{
		Status:     DomainOk,
		Detail:     fmt.Sprintf("HTTP %d", resp.StatusCode),
		StatusCode: resp.StatusCode,
		RedirectTo: location,
	}
}

// isCrossDomainRedirect checks if a redirect Location points to a completely
// different domain (not a subdomain or known CDN/auth endpoint).
func isCrossDomainRedirect(originalDomain, locationURL string) bool {
	parsed, err := url.Parse(locationURL)
	if err != nil || parsed.Host == "" {
		return false
	}

	redirectHost := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	originalHost := strings.ToLower(strings.TrimPrefix(originalDomain, "www."))

	// Same domain or subdomain — not suspicious
	if redirectHost == originalHost ||
		strings.HasSuffix(redirectHost, "."+originalHost) ||
		strings.HasSuffix(originalHost, "."+redirectHost) {
		return false
	}

	// Check against CDN/auth patterns — these are legitimate redirects
	for _, pattern := range CDNRedirectPatterns {
		if strings.Contains(redirectHost, pattern) {
			return false
		}
	}

	return true
}

var dpiStatusPriority = []DomainStatus{
	DomainTLSSpoof,
	DomainTLSDPI,
	DomainTLSReset,
	DomainTLSDrop,
	DomainTCP16,
	DomainSYNDrop,
	DomainTLSAlert,
	DomainTLSMITM,
}

func isDPIStatus(st DomainStatus) bool {
	switch st {
	case DomainTLSDPI, DomainTLSSpoof, DomainTLSAlert, DomainTLSReset, DomainTLSDrop, DomainSYNDrop, DomainTCP16:
		return true
	}
	return false
}

func deriveOverallStatus(dr DomainCheckResult) DomainStatus {
	if dr.IsFakeIP {
		return DomainDNSFake
	}

	tlsStatuses := []DomainStatus{}
	if dr.TLS13 != nil {
		tlsStatuses = append(tlsStatuses, dr.TLS13.Status)
	}
	if dr.TLS12 != nil {
		tlsStatuses = append(tlsStatuses, dr.TLS12.Status)
	}

	for _, want := range dpiStatusPriority {
		for _, got := range tlsStatuses {
			if got == want {
				return want
			}
		}
	}

	if dr.HTTP != nil && dr.HTTP.Status == DomainISPPage {
		return DomainISPPage
	}

	allFailed := true
	if dr.TLS13 != nil && dr.TLS13.Status == DomainOk {
		allFailed = false
	}
	if dr.TLS12 != nil && dr.TLS12.Status == DomainOk {
		allFailed = false
	}
	if dr.HTTP != nil && dr.HTTP.Status == DomainOk {
		allFailed = false
	}

	if allFailed {
		if dr.TLS13 != nil && dr.TLS13.Status == DomainTimeout {
			return DomainTimeout
		}
		return DomainBlocked
	}

	return DomainOk
}
