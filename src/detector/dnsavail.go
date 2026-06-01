package detector

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const dnsAvailTimeout = 5 * time.Second

func (s *DetectorSuite) runDNSAvailCheck(ctx context.Context) *DNSAvailResult {
	log.DiscoveryLogf("[Detector] Starting DNS availability check for %d servers", len(DNSAvailServers))

	result := &DNSAvailResult{}
	domains := DNSAvailDomains
	if len(domains) == 0 {
		domains = DNSCheckDomains
	}

	providers := make([]DNSAvailProviderResult, len(DNSAvailServers))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for i, srv := range DNSAvailServers {
		if s.isCanceled() {
			break
		}
		wg.Add(1)
		go func(idx int, server dnsAvailServer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			pr := DNSAvailProviderResult{
				Provider: server.Name,
				Address:  server.Address,
				Total:    len(domains),
			}
			if server.Kind == "doh" {
				pr.Kind = DNSAvailDoH
			} else {
				pr.Kind = DNSAvailUDP
			}

			var sum float64
			for _, dom := range domains {
				ms, ok := s.dnsAvailProbe(ctx, server, dom)
				if ok {
					pr.OkCount++
					sum += ms
				}
				s.mu.Lock()
				s.CompletedChecks++
				s.mu.Unlock()
			}
			if pr.OkCount > 0 {
				pr.Ok = true
				pr.AvgMs = round1(sum / float64(pr.OkCount))
			}
			providers[idx] = pr
		}(i, srv)
	}

	wg.Wait()

	for _, pr := range providers {
		result.Providers = append(result.Providers, pr)
		switch pr.Kind {
		case DNSAvailDoH:
			result.DoHTotal++
			if pr.Ok {
				result.DoHOk++
			}
		case DNSAvailUDP:
			result.UDPTotal++
			if pr.Ok {
				result.UDPOk++
			}
		}
	}

	result.Summary = fmt.Sprintf("DoH %d/%d, UDP %d/%d reachable",
		result.DoHOk, result.DoHTotal, result.UDPOk, result.UDPTotal)

	log.DiscoveryLogf("[Detector] DNS availability check complete: %s", result.Summary)
	return result
}

func (s *DetectorSuite) dnsAvailProbe(ctx context.Context, server dnsAvailServer, domain string) (float64, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, dnsAvailTimeout)
	defer cancel()

	start := time.Now()
	if server.Kind == "doh" {
		client := markedHTTPClient(s.mark, dnsAvailTimeout)
		if _, err := resolveDoHWire(probeCtx, client, server.Address, domain); err != nil {
			return 0, false
		}
		return float64(time.Since(start).Microseconds()) / 1000.0, true
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(c context.Context, network, address string) (net.Conn, error) {
			return markedDialer(s.mark, dnsAvailTimeout).DialContext(c, "udp", net.JoinHostPort(server.Address, "53"))
		},
	}
	if _, err := resolver.LookupIPAddr(probeCtx, domain); err != nil {
		return 0, false
	}
	return float64(time.Since(start).Microseconds()) / 1000.0, true
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
