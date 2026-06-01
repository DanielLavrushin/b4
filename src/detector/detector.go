package detector

import (
	"context"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func (s *DetectorSuite) Run(cfg *config.Config) {
	configPath := cfg.ConfigPath
	s.mu.Lock()
	s.Status = StatusRunning
	s.StartTime = time.Now()
	s.TotalChecks = s.estimateTotalChecks()
	s.mu.Unlock()

	log.DiscoveryLogf("[Detector] Starting detection suite %s with tests: %v", s.Id, s.Tests)

	ctx := context.Background()
	var stubIPs map[string]bool

	for _, test := range s.Tests {
		if s.isCanceled() {
			log.DiscoveryLogf("[Detector] Suite %s canceled", s.Id)
			s.mu.Lock()
			s.EndTime = time.Now()
			s.CurrentTest = ""
			s.mu.Unlock()
			SaveToHistory(s, configPath)
			s.scheduleCleanup()
			return
		}

		s.mu.Lock()
		s.CurrentTest = test
		s.mu.Unlock()

		switch test {
		case TestDNS:
			result := s.runDNSCheck(ctx)
			s.mu.Lock()
			s.DNSResult = result
			s.mu.Unlock()

			// Collect stub IPs for domain check
			if result != nil && len(result.StubIPs) > 0 {
				stubIPs = make(map[string]bool)
				for _, ip := range result.StubIPs {
					stubIPs[ip] = true
				}
			}

		case TestDomains:
			if stubIPs == nil {
				stubIPs = make(map[string]bool)
			}
			result := s.runDomainsCheck(ctx, stubIPs)
			s.mu.Lock()
			s.DomainsResult = result
			s.mu.Unlock()

		case TestTCP:
			result := s.runTCPCheck(ctx)
			s.mu.Lock()
			s.TCPResult = result
			s.mu.Unlock()

		case TestSNI:
			result := s.runSNICheck(ctx)
			s.mu.Lock()
			s.SNIResult = result
			s.mu.Unlock()

		case TestDNSAvail:
			result := s.runDNSAvailCheck(ctx)
			s.mu.Lock()
			s.DNSAvailResult = result
			s.mu.Unlock()

		case TestTelegram:
			result := s.runTelegramCheck(ctx)
			s.mu.Lock()
			s.TelegramResult = result
			s.mu.Unlock()
		}
	}

	s.mu.Lock()
	s.Status = StatusComplete
	s.EndTime = time.Now()
	s.CurrentTest = ""
	s.mu.Unlock()

	log.DiscoveryLogf("[Detector] Detection suite %s complete in %v", s.Id, s.EndTime.Sub(s.StartTime).Round(time.Second))

	SaveToHistory(s, configPath)
	s.scheduleCleanup()
}

func (s *DetectorSuite) scheduleCleanup() {
	go func() {
		time.Sleep(30 * time.Second)
		suitesMu.Lock()
		delete(activeSuites, s.Id)
		suitesMu.Unlock()
	}()
}

func (s *DetectorSuite) estimateTotalChecks() int {
	total := 0
	tcpRequested := false
	for _, test := range s.Tests {
		switch test {
		case TestDNS:
			total += len(DNSCheckDomains)
		case TestDomains:
			total += len(CheckDomains) * 3 // TLS1.3 + TLS1.2 + HTTP
		case TestTCP:
			total += len(TCPTargets)
			tcpRequested = true
		case TestSNI:
			total += estimateSNIChecks(tcpRequested)
		case TestDNSAvail:
			domains := DNSAvailDomains
			if len(domains) == 0 {
				domains = DNSCheckDomains
			}
			total += len(DNSAvailServers) * len(domains)
		case TestTelegram:
			total += len(telegramDCEndpoints()) + 1
		}
	}
	return total
}

func estimateSNIChecks(tcpAlreadyRequested bool) int {
	total := 0
	asnSet := make(map[string]bool)
	for _, t := range TCPTargets {
		if t.Port != 443 {
			continue
		}
		if !tcpAlreadyRequested {
			total++ // Phase 1 base probe
		}
		asnSet[t.ASN] = true
	}
	total += len(asnSet) // Phase 2 brute-force per ASN
	return total
}
