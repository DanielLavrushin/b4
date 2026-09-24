package discovery

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

// collectTargetIPs returns deduplicated IPs from DNS discovery results, limited to maxIPs.
func (ds *DiscoverySuite) collectTargetIPs(domain string, maxIPs int) []string {
	dnsResult := ds.dnsResults[domain]
	if dnsResult == nil {
		return nil
	}
	if len(dnsResult.AlternativeIPs) > 0 && dnsResult.TransportBlocked {
		return append([]string(nil), dnsResult.AlternativeIPs...)
	}

	seen := make(map[string]bool)
	for _, ip := range dnsResult.GatewayIPs {
		seen[ip] = true
	}
	var ips []string
	for _, ip := range dnsResult.AlternativeIPs {
		if !seen[ip] {
			seen[ip] = true
			ips = append(ips, ip)
		}
	}
	for _, ip := range dnsResult.ExpectedIPs {
		if !seen[ip] {
			seen[ip] = true
			ips = append(ips, ip)
		}
	}
	for _, probe := range dnsResult.ProbeResults {
		if probe.ResolvedIP != "" && !seen[probe.ResolvedIP] {
			seen[probe.ResolvedIP] = true
			ips = append(ips, probe.ResolvedIP)
		}
	}
	if maxIPs > 0 && len(ips) > maxIPs+len(dnsResult.AlternativeIPs) {
		ips = ips[:maxIPs+len(dnsResult.AlternativeIPs)]
	}
	return ips
}

func (ds *DiscoverySuite) fetchForDomain(di DomainInput, timeout time.Duration) CheckResult {
	if dnsResult := ds.dnsResults[di.Domain]; dnsResult.gatewayIntercepted() {
		return CheckResult{
			Domain: di.Domain,
			Status: CheckStatusFailed,
			Error:  "TCP to every known address is answered by the first hop, not tried",
		}
	}
	// Use IPs already collected during DNS discovery — no fresh DNS lookups.
	// Fresh lookups are slow (poisoned DNS can timeout) and redundant since
	// DNS discovery already gathered all valid IPs from DoH + system resolver.
	// Limit to 2 IPs to avoid slow sequential fallback.
	allIPs := ds.collectTargetIPs(di.Domain, 2)

	geoip, geosite := GetCDNCategories(di.Domain)
	if len(geoip) > 0 || len(geosite) > 0 {
		// CDN domains are matched by geoip/geosite in a real config, but the
		// validation fetch still needs a resolvable IP. Pin the IP discovered
		// during the DNS phase; only fall back to system DNS when none exist
		// (otherwise a poisoned system resolver fails every preset).
		ip := ""
		if len(allIPs) > 0 {
			ip = allIPs[0]
		}
		return ds.fetchUsingIPForDomain(di, timeout, ip)
	}

	var last CheckResult
	for _, ip := range allIPs {
		last = ds.fetchUsingIPForDomain(di, timeout, ip)
		if last.Status == CheckStatusComplete {
			log.Tracef("Success with IP %s for %s", ip, di.Domain)
			return last
		}
		log.Tracef("IP %s failed for %s, trying next", ip, di.Domain)
	}

	if len(allIPs) > 0 {
		last.Status = CheckStatusFailed
		last.Error = fmt.Sprintf("all %d IPs failed: %s", len(allIPs), last.Error)
		return last
	}

	return ds.fetchUsingIPForDomain(di, timeout, "")
}

// dialNetwork forces the probe address family ("tcp4"/"tcp6") so validation
// runs over the same family b4 actually queues. An explicit IP version wins;
// otherwise it mirrors DNSProber.ipNetwork and follows the enabled queue
// families, leaving the choice to the resolver/OS only when both are processed.
func (ds *DiscoverySuite) dialNetwork() string {
	switch ds.ipVersion {
	case "ipv4":
		return "tcp4"
	case "ipv6":
		return "tcp6"
	}
	if ds.cfg == nil {
		return ""
	}
	switch {
	case ds.cfg.Queue.IPv4Enabled && ds.cfg.Queue.IPv6Enabled:
		return ""
	case ds.cfg.Queue.IPv4Enabled:
		return "tcp4"
	case ds.cfg.Queue.IPv6Enabled:
		return "tcp6"
	}
	return ""
}

// dialContext builds the probe dialer: it forces the address family from
// dialNetwork and pins pinnedIP when DNS discovery already resolved one.
func (ds *DiscoverySuite) dialContext(timeout time.Duration, pinnedHost, pinnedIP string) func(context.Context, string, string) (net.Conn, error) {
	baseDialer := netprobe.Dialer(int(ds.flowMark), timeout/2, timeout)
	baseDialer.Resolver = netprobe.MarkedResolver(int(ds.flowMark), timeout/2, "")
	forcedNet := ds.dialNetwork()

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if forcedNet != "" {
			network = forcedNet
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			host, port = addr, "443"
		}
		if port == "" {
			port = "443"
		}
		if pinnedIP != "" && strings.EqualFold(host, pinnedHost) {
			directAddr := net.JoinHostPort(pinnedIP, port)
			log.Tracef("DNS bypass: connecting to %s instead of %s", directAddr, addr)
			return baseDialer.DialContext(ctx, network, directAddr)
		}
		return baseDialer.DialContext(ctx, network, addr)
	}
}

func (ds *DiscoverySuite) tlsConfig() *tls.Config {
	cfg := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2", "http/1.1"},
	}
	switch ds.tlsVersion {
	case "tls12":
		cfg.MinVersion = tls.VersionTLS12
		cfg.MaxVersion = tls.VersionTLS12
	case "tls13":
		cfg.MinVersion = tls.VersionTLS13
		cfg.MaxVersion = tls.VersionTLS13
	}
	return cfg
}

func (ds *DiscoverySuite) fetchUsingIPForDomain(di DomainInput, timeout time.Duration, ip string) CheckResult {
	result := CheckResult{
		Domain:    di.Domain,
		Status:    CheckStatusRunning,
		Timestamp: time.Now(),
		UsedIP:    ip,
	}

	ctx, cancel := ds.fetchContext(timeout)
	defer cancel()

	transport := &http.Transport{
		TLSClientConfig:       ds.tlsConfig(),
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       timeout,
		ForceAttemptHTTP2:     true,
	}

	transport.DialContext = ds.dialContext(timeout, di.Domain, ip)

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxProbeRedirects {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			result.FinalHost = req.URL.Hostname()
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", di.CheckURL, nil)
	if err != nil {
		result.Status = CheckStatusFailed
		result.Error = err.Error()
		return result
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		result.Status = CheckStatusFailed
		_, detail := netprobe.ClassifyTLSError(err)
		result.Error = detail
		result.Duration = time.Since(start)
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.ContentSize = resp.ContentLength

	// A malformed request is our own doing: the strategy under test corrupted the stream
	// before the origin parsed it, so the fetch is not evidence that the strategy works.
	if resp.StatusCode == http.StatusBadRequest {
		result.Status = CheckStatusFailed
		result.Error = "server answered HTTP 400, the strategy corrupted the request"
		result.Duration = time.Since(start)
		return result
	}

	// Check for ISP block page indicators before reading body.
	if resp.StatusCode == 451 {
		result.Status = CheckStatusFailed
		result.Error = "ISP block page (HTTP 451)"
		result.Duration = time.Since(start)
		return result
	}
	if resp.StatusCode >= 500 {
		result.Status = CheckStatusFailed
		result.Error = fmt.Sprintf("server answered HTTP %d", resp.StatusCode)
		result.Duration = time.Since(start)
		return result
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		if netprobe.IsBlockPageRedirect(loc) {
			result.Status = CheckStatusFailed
			result.Error = "ISP block page (redirect to " + loc + ")"
			result.Duration = time.Since(start)
			return result
		}
	}

	buf := make([]byte, 16*1024)
	tailBuf := make([]byte, 0, 64)     // rolling tail for </body></html> detection
	headBuf := make([]byte, 0, 4*1024) // first 4KB for ISP block page detection
	var bytesRead int64
	lastProgress := time.Now()

	maxRead := int64(100 * 1024)
	if result.ContentSize > 0 && result.ContentSize < maxRead {
		maxRead = result.ContentSize
	}

	for bytesRead < maxRead {
		select {
		case <-ctx.Done():
			goto evaluate
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			bytesRead += int64(n)
			lastProgress = time.Now()
			if len(headBuf) < 4*1024 {
				headBuf = append(headBuf, buf[:n]...)
				if len(headBuf) > 4*1024 {
					headBuf = headBuf[:4*1024]
				}
			}
			tailBuf = append(tailBuf, buf[:n]...)
			if len(tailBuf) > 64 {
				tailBuf = tailBuf[len(tailBuf)-64:]
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			result.Status = CheckStatusFailed
			result.Error = fmt.Sprintf("read error after %d bytes: %v", bytesRead, err)
			result.Duration = time.Since(start)
			result.BytesRead = bytesRead
			return result
		}

		if time.Since(lastProgress) > 2*time.Second {
			result.Status = CheckStatusFailed
			result.Error = fmt.Sprintf("stalled after %d bytes", bytesRead)
			result.Duration = time.Since(start)
			result.BytesRead = bytesRead
			return result
		}
	}

evaluate:
	duration := time.Since(start)
	result.Duration = duration
	result.BytesRead = bytesRead

	if duration.Seconds() > 0 {
		result.Speed = float64(bytesRead) / duration.Seconds()
	}

	// Check for ISP block page in response body before marking as success.
	if blockErr := netprobe.DetectBlockPageBody(headBuf); blockErr != "" {
		result.Status = CheckStatusFailed
		result.Error = blockErr
		return result
	}

	if len(tailBuf) > 0 {
		tailLower := bytes.ToLower(tailBuf)
		if bytes.Contains(tailLower, []byte("</body>")) && bytes.Contains(tailLower, []byte("</html>")) {
			result.Status = CheckStatusComplete
			return result
		}
	}

	if result.ContentSize > 0 {
		expectedBytes := result.ContentSize
		if expectedBytes > 100*1024 {
			expectedBytes = 100 * 1024
		}

		if bytesRead < expectedBytes*9/10 {
			result.Status = CheckStatusFailed
			result.Error = fmt.Sprintf("truncated: %d/%d bytes (%.0f%%)",
				bytesRead, expectedBytes, float64(bytesRead)*100/float64(expectedBytes))
			return result
		}
	}

	if bytesRead < minSuccessBytes {
		result.Status = CheckStatusFailed
		result.Error = fmt.Sprintf("insufficient data: %d bytes", bytesRead)
		return result
	}

	result.Status = CheckStatusComplete
	return result
}

func (ds *DiscoverySuite) measureNetworkBaseline() float64 {
	// Test a known-good domain to establish actual network speed
	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
	referenceDomain := ds.cfg.System.Checker.ReferenceDomain
	if referenceDomain == "" {
		referenceDomain = config.DefaultConfig.System.Checker.ReferenceDomain
	}

	log.DiscoveryLogf("Measuring network baseline using %s", referenceDomain)

	testURL := fmt.Sprintf("https://%s/", referenceDomain)
	ctx, cancel := ds.fetchContext(timeout)
	defer cancel()

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:   ds.tlsConfig(),
			DialContext:       ds.dialContext(timeout, "", ""),
			ForceAttemptHTTP2: true,
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		log.DiscoveryLogf("Failed to create baseline request: %v", err)
		return 0
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.DiscoveryLogf("Baseline measurement failed: %v", err)
		return 0
	}
	defer resp.Body.Close()

	bytesRead, _ := io.CopyN(io.Discard, resp.Body, 100*1024)
	duration := time.Since(start)

	if bytesRead == 0 || duration.Seconds() == 0 {
		return 0
	}

	speed := float64(bytesRead) / duration.Seconds()
	log.DiscoveryLogf("Network baseline: %.2f KB/s (%d bytes in %v)", speed/1024, bytesRead, duration)

	return speed
}
