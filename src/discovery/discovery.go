package discovery

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

type FailureMode string

const (
	FailureRSTImmediate FailureMode = "rst_immediate"
	FailureTimeout      FailureMode = "timeout"
	FailureTLSError     FailureMode = "tls_error"
	FailureUnknown      FailureMode = "unknown"

	validationRetryDelay = 100 * time.Millisecond

	minSuccessBytes = 1024

	presetNoBypass = "no-bypass"

	confirmTries = 3
	confirmDelay = 2 * time.Second

	maxProbeRedirects = 5
)

func NewDiscoverySuite(inputs []string, pool *nfq.Pool, skipDNS bool, skipCache bool, payloadFiles []string, validationTries int, tlsVersion string, ipVersion string, flowMark uint) *DiscoverySuite {
	domainInputs := parseDiscoveryInputs(inputs)
	if len(domainInputs) == 0 {
		suite := NewCheckSuite(domainInputs)
		ds := &DiscoverySuite{CheckSuite: suite}
		ds.initCancelContext()
		return ds
	}
	suite := NewCheckSuite(domainInputs)

	// Ensure validationTries is at least 1
	if validationTries < 1 {
		validationTries = 1
	}

	if tlsVersion == "" {
		tlsVersion = "auto"
	}

	if ipVersion == "" {
		ipVersion = "auto"
	}

	domainResults := make(map[string]*DomainDiscoveryResult)
	for _, di := range domainInputs {
		domainResults[di.Domain] = &DomainDiscoveryResult{
			Domain:  di.Domain,
			Url:     di.CheckURL,
			Results: make(map[string]*DomainPresetResult),
		}
	}

	ds := &DiscoverySuite{
		CheckSuite:      suite,
		pool:            pool,
		domainResults:   domainResults,
		dnsResults:      make(map[string]*DNSDiscoveryResult),
		workingPayloads: []PayloadTestResult{},
		bestPayload:     config.FakePayloadSTUN,
		skipDNS:         skipDNS,
		skipCache:       skipCache,
		validationTries: validationTries,
		tlsVersion:      tlsVersion,
		ipVersion:       ipVersion,
		flowMark:        flowMark,
	}

	if len(payloadFiles) > 0 {
		cfg := pool.GetFirstWorkerConfig()
		if cfg != nil {
			ds.customPayloads = loadCustomPayloads(cfg, payloadFiles)
		}
	}

	ds.initCancelContext()
	return ds
}

func parseDiscoveryInput(input string) (domain string, testURL string) {
	input = strings.TrimSpace(input)
	input = strings.Trim(input, "\"'`")
	input = strings.TrimSpace(input)

	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err == nil && u.Host != "" {
			return u.Hostname(), input
		}
	}

	return input, "https://" + input + "/"
}

func checkURLTLSPort(raw string) int {
	if u, err := url.Parse(raw); err == nil && u.Scheme == "http" {
		return 443
	}
	return checkURLPort(raw)
}

func tlsAddress(ip string, port int) string {
	if port <= 0 {
		port = 443
	}
	return net.JoinHostPort(ip, strconv.Itoa(port))
}

func checkURLPort(raw string) int {
	u, err := url.Parse(raw)
	if err != nil {
		return 443
	}
	if port, err := strconv.Atoi(u.Port()); err == nil && port > 0 {
		return port
	}
	if u.Scheme == "http" {
		return 80
	}
	return 443
}

func parseDiscoveryInputs(inputs []string) []DomainInput {
	seen := make(map[string]bool)
	var result []DomainInput
	for _, input := range inputs {
		domain, checkURL := parseDiscoveryInput(input)
		if domain == "" {
			continue
		}
		if !seen[domain] {
			seen[domain] = true
			result = append(result, DomainInput{Domain: domain, CheckURL: checkURL})
		}
	}
	return result
}

func (ds *DiscoverySuite) setCurrentDomain(domain string) {
	ds.CheckSuite.mu.Lock()
	ds.CurrentDomain = domain
	ds.CheckSuite.mu.Unlock()
}

func appendUnique(slice []string, items ...string) []string {
	for _, item := range items {
		found := false
		for _, existing := range slice {
			if existing == item {
				found = true
				break
			}
		}
		if !found {
			slice = append(slice, item)
		}
	}
	return slice
}

func (ds *DiscoverySuite) setStatus(status CheckStatus) {
	ds.CheckSuite.mu.Lock()
	ds.Status = status
	ds.CheckSuite.mu.Unlock()
}

// allDomainsTransportBlocked returns true if DNS discovery flagged all domains
// as transport-blocked (neither system nor reference IPs could connect).
func (ds *DiscoverySuite) allDomainsTransportBlocked() bool {
	if len(ds.dnsResults) == 0 {
		return false
	}
	for _, result := range ds.dnsResults {
		if !result.addressBlocked() && !result.gatewayIntercepted() {
			return false
		}
	}
	return true
}

func (ds *DiscoverySuite) allDomainsGatewayIntercepted() bool {
	if len(ds.dnsResults) == 0 {
		return false
	}
	for _, result := range ds.dnsResults {
		if !result.gatewayIntercepted() {
			return false
		}
	}
	return true
}

func (ds *DiscoverySuite) setPhase(phase DiscoveryPhase) {
	ds.CheckSuite.mu.Lock()
	ds.CurrentPhase = phase
	ds.CheckSuite.mu.Unlock()
}
