package discovery

import (
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func (ds *DiscoverySuite) RunDiscovery() {
	defer func() { ds.ctxCancel() }()
	defer ds.saveRunLog()
	log.DiscoveryLogf("═══════════════════════════════════════")
	domainNames := make([]string, len(ds.Domains))
	for i, di := range ds.Domains {
		domainNames[i] = di.Domain
	}
	tlsSet := ds.tlsVersion != "" && ds.tlsVersion != "auto"
	ipSet := ds.ipVersion != "" && ds.ipVersion != "auto"
	switch {
	case tlsSet && ipSet:
		log.DiscoveryLogf("Starting discovery for %d domains: %v (TLS: %s, IP: %s)", len(ds.Domains), domainNames, ds.tlsVersion, ds.ipVersion)
	case tlsSet:
		log.DiscoveryLogf("Starting discovery for %d domains: %v (TLS: %s)", len(ds.Domains), domainNames, ds.tlsVersion)
	case ipSet:
		log.DiscoveryLogf("Starting discovery for %d domains: %v (IP: %s)", len(ds.Domains), domainNames, ds.ipVersion)
	default:
		log.DiscoveryLogf("Starting discovery for %d domains: %v", len(ds.Domains), domainNames)
	}
	log.DiscoveryLogf("═══════════════════════════════════════")

	defer func() {
		ds.EndTime = time.Now()
	}()

	select {
	case <-ds.cancel:
		ds.setStatus(CheckStatusCanceled)
		ds.finalize()
		ds.logDiscoverySummary()
		return
	default:
	}

	ds.setStatus(CheckStatusRunning)

	phase1Count := len(GetPhase1Presets())
	ds.CheckSuite.mu.Lock()
	ds.TotalChecks = phase1Count * len(ds.Domains)
	ds.CheckSuite.mu.Unlock()

	ds.cfg = ds.pool.GetFirstWorkerConfig()

	if ds.cfg == nil {
		log.Errorf("Failed to get original configuration")
		ds.setStatus(CheckStatusFailed)
		return
	}

	probeFamily := ds.dialNetwork()
	if probeFamily == "" {
		probeFamily = "dual-stack"
	}
	log.DiscoveryLogf("Probe address family: %s (queue IPv4=%v, IPv6=%v)", probeFamily, ds.cfg.Queue.IPv4Enabled, ds.cfg.Queue.IPv6Enabled)

	ds.discoveryCache = LoadDiscoveryCache(ds.cfg.ConfigPath)
	defer ds.saveResultsToCache()

	ds.networkBaseline = ds.measureNetworkBaseline()

	// DNS phase: per-domain
	anyDNSPoisoned := false
	if ds.skipDNS {
		log.DiscoveryLogf("Skipping DNS discovery (user requested)")
	} else {
		ds.setPhase(PhaseDNS)
		for _, di := range ds.Domains {
			ds.setCurrentDomain(di.Domain)
			log.DiscoveryLogf("Running DNS discovery for %s", di.Domain)
			dnsResult := ds.runDNSDiscoveryForDomain(di)
			ds.dnsResults[di.Domain] = dnsResult
			ds.domainResults[di.Domain].DNSResult = dnsResult

			if dnsResult != nil && len(dnsResult.ExpectedIPs) > 0 {
				log.DiscoveryLogf("  [%s] Stored %d target IPs: %v", di.Domain, len(dnsResult.ExpectedIPs), dnsResult.ExpectedIPs)
			}
			if dnsResult.gatewayIntercepted() {
				log.DiscoveryLogf("  ⊘ TCP to %s is answered by the first hop in front of this host; b4 here cannot help: run b4 on that gateway or exclude this host from its redirect; if this host is the router itself, the ISP does this at its edge and only a proxy route helps", di.Domain)
			}

			if dnsResult != nil && dnsResult.IsPoisoned {
				anyDNSPoisoned = true
				if dnsResult.hasWorkingConfig() {
					log.DiscoveryLogf("  [%s] DNS poisoned - bypass config found", di.Domain)
				} else if len(dnsResult.ExpectedIPs) > 0 {
					log.DiscoveryLogf("  [%s] DNS poisoned, no bypass - using direct IPs", di.Domain)
				} else {
					log.DiscoveryLogf("  [%s] DNS poisoned but no expected IP known", di.Domain)
				}
			}
		}

		// Apply DNS config if any domain needs it
		if anyDNSPoisoned {
			ds.applyBestDNSConfig()
		}

		if ds.allDomainsGatewayIntercepted() {
			log.DiscoveryLogf("Every domain is answered by the first hop in front of this host, there is nothing a packet strategy from this host could change; search skipped")
			ds.finishRun()
			return
		}
	}

	if ds.interrupted() {
		ds.finishRun()
		return
	}

	// Phase 0: Test previously successful cached configurations
	var cachedPresets []ConfigPreset
	if ds.skipCache {
		log.DiscoveryLogf("Skipping cached strategies (user requested)")
	} else {
		cachedPresets = ds.discoveryCache.GetCachedPresets()
	}
	phase1Presets := GetPhase1Presets()
	current, hasCurrent := ds.setCurrentPreset()
	currentCount := 0
	if hasCurrent {
		currentCount = 1
	}

	ds.CheckSuite.mu.Lock()
	ds.TotalChecks = (len(phase1Presets) + len(cachedPresets) + len(ds.hubPresets) + currentCount) * len(ds.Domains)
	ds.CheckSuite.mu.Unlock()

	ds.setPhase(PhaseStrategy)
	ds.storeResultsMulti(phase1Presets[0], ds.testPresetAllDomains(phase1Presets[0]))
	ds.determineBest()

	if hasCurrent && !ds.interrupted() {
		ds.setPhase(PhaseCached)
		log.DiscoveryLogf("Testing the set's current strategy across %d domains", len(ds.Domains))
		ds.storeResultsMulti(current, ds.testPresetAllDomains(current))
		ds.determineBest()
	}

	if ds.interrupted() {
		ds.finishRun()
		return
	}

	if len(cachedPresets) > 0 {
		ds.setPhase(PhaseCached)
		log.DiscoveryLogf("Phase 0: Testing %d cached configurations across %d domains", len(cachedPresets), len(ds.Domains))

		for _, preset := range cachedPresets {
			if ds.interrupted() {
				ds.finishRun()
				return
			}

			if preset.Config.Faking.SNIType == config.FakePayloadRandom {
				ds.applyBestPayload(&preset.Config.Faking)
			}
			results := ds.testPresetAllDomains(preset)
			ds.storeResultsMulti(preset, results)
		}
	}

	if ds.hubPresetsFn != nil {
		ds.setPhase(PhaseCached)
		ds.hubPresets = ds.hubPresetsFn()
		if ds.setMode() {
			ds.hubPresets = unscopedPresets(ds.hubPresets)
		}
		checks := 0
		for _, preset := range ds.hubPresets {
			checks += len(ds.presetDomains(preset))
		}
		ds.CheckSuite.mu.Lock()
		ds.TotalChecks += checks
		ds.CheckSuite.mu.Unlock()
	}
	if len(ds.hubPresets) > 0 {
		ds.setPhase(PhaseCached)
		log.DiscoveryLogf("Community: testing %d strategies other users published for these domains", len(ds.hubPresets))

		for _, preset := range ds.hubPresets {
			if ds.interrupted() {
				ds.finishRun()
				return
			}

			if preset.Config.Faking.SNIType == config.FakePayloadRandom {
				ds.applyBestPayload(&preset.Config.Faking)
			}
			results := ds.testPresetAllDomains(preset)
			ds.storeResultsMulti(preset, results)
		}
	}

	// Phase 1: Strategy detection across all domains
	ds.setPhase(PhaseStrategy)
	workingFamilies := ds.runPhase1Multi(phase1Presets)
	ds.determineBest()

	if !ds.interrupted() {
		for _, family := range ds.upgradeDeadEndCheckURLs(phase1Presets, cachedPresets) {
			if !containsFamily(workingFamilies, family) {
				workingFamilies = append(workingFamilies, family)
			}
		}
		ds.determineBest()
	}

	if ds.interrupted() {
		ds.finishRun()
		return
	}

	if !ds.anyDomainNeedsBypass() {
		log.DiscoveryLogf("Verified: no packet strategy needed for any domain")
		ds.finishRun()
		return
	}

	if len(workingFamilies) == 0 {
		// Skip extended search if all domains have transport-level blocking.
		// When neither system nor reference IPs can even establish a TCP connection,
		// no packet manipulation strategy will help — the blocking is at IP level.
		if ds.allDomainsTransportBlocked() {
			log.Warnf("All domains have transport-level blocking (IP blocked) — extended search skipped")
			ds.finishRun()
			return
		}

		log.Warnf("Phase 1 found no working families, trying extended search")

		ds.setPhase(PhaseOptimize)
		workingFamilies = ds.runExtendedSearch()

		if len(workingFamilies) == 0 {
			log.Warnf("No working bypass strategies found")
			ds.finishRun()
			return
		}
	}

	log.Infof("Phase 1 complete: %d working families: %v", len(workingFamilies), workingFamilies)

	// Phase 2: Optimization using representative domain per family
	ds.setPhase(PhaseOptimize)
	bestParams := ds.runPhase2WithRepresentative(workingFamilies)
	ds.determineBest()

	// Phase 3: Combinations across all domains
	if len(workingFamilies) >= 2 {
		ds.setPhase(PhaseCombination)
		ds.runPhase3Multi(workingFamilies, bestParams)
	}

	ds.finishRun()
}

func (ds *DiscoverySuite) finishRun() {
	if !ds.canceled() {
		ds.CheckSuite.mu.RLock()
		stop, winner := ds.coverStop, ds.coverWinner
		ds.CheckSuite.mu.RUnlock()
		switch {
		case stop == coverStopCovered:
			log.DiscoveryLogf("Every address of the set is covered by '%s' (confirmed), stopping the search", winner)
		case stop == coverStopBaseline:
			log.DiscoveryLogf("Every address of the set loads without b4, stopping the search")
		case ds.finishing():
			log.DiscoveryLogf("Search stopped by the user, confirming what was found so far")
		}
		ds.resetFetchContext()
		ds.determineBest()
		if stop != coverStopCovered {
			ds.confirmWinners()
		}
		ds.resolveSetVerdict()
	}
	ds.finalize()
	ds.logDiscoverySummary()
}

func (ds *DiscoverySuite) finalize() {
	ds.buildStrategyGroups()

	ds.CheckSuite.mu.Lock()
	ds.DomainDiscoveryResults = ds.domainResults
	ds.EndTime = time.Now()
	ds.publishSetVerdictLocked()
	if ds.Status != CheckStatusCanceled {
		ds.Status = CheckStatusComplete
	}
	ds.refreshOutcomes(true)
	ds.CheckSuite.mu.Unlock()

	// Persist results to history
	if ds.cfg != nil {
		SaveToHistory(ds.CheckSuite, ds.cfg.ConfigPath)
	}

	go func() {
		time.Sleep(30 * time.Second)
		suitesMu.Lock()
		delete(activeSuites, ds.Id)
		suitesMu.Unlock()
	}()
}

func (ds *DiscoverySuite) logDiscoverySummary() {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()

	duration := time.Since(ds.StartTime)

	log.DiscoveryLogf("═══════════════════════════════════════")
	log.DiscoveryLogf("Discovery complete for %d domains in %v", len(ds.Domains), duration.Round(time.Second))

	for _, di := range ds.Domains {
		domainResult := ds.domainResults[di.Domain]
		dnsResult := ds.dnsResults[di.Domain]

		// DNS status line
		if dnsResult != nil {
			switch {
			case dnsResult.gatewayIntercepted():
				log.DiscoveryLogf("  ⊘ [%s] TCP to %v is answered by the first hop in front of this host; b4 here cannot help: run b4 on that gateway or exclude this host from its redirect; if this host is the router itself, the ISP does this at its edge and only a proxy route helps", di.Domain, dnsResult.GatewayIPs)
				continue
			case dnsResult.TransportBlocked && len(dnsResult.AlternativeIPs) > 0:
				log.DiscoveryLogf("  ⚡ [%s] known addresses blocked, answered with %v instead", di.Domain, dnsResult.AlternativeIPs)
			case dnsResult.TransportBlocked:
				log.DiscoveryLogf("  ⊘ [%s] IP-blocked: TCP connections fail to all known IPs, a proxy or VPN route is needed", di.Domain)
				continue
			case dnsResult.IsPoisoned && dnsResult.BestDoHURL != "":
				log.DiscoveryLogf("  ⚡ [%s] DNS poisoned, bypassed via %s", di.Domain, dnsResult.BestDoHURL)
			case dnsResult.IsPoisoned && dnsResult.BestServer != "":
				log.DiscoveryLogf("  ⚡ [%s] DNS poisoned, bypassed via %s", di.Domain, dnsResult.BestServer)
			case dnsResult.IsPoisoned && dnsResult.NeedsFragment:
				log.DiscoveryLogf("  ⚡ [%s] DNS poisoned, bypassed via fragmented queries", di.Domain)
			case dnsResult.IsPoisoned:
				log.DiscoveryLogf("  ✗ [%s] DNS poisoned, no bypass found", di.Domain)
			}
		}

		if domainResult.BestSuccess {
			log.DiscoveryLogf("  ✓ [%s] Best: %s (%.2f KB/s)", di.Domain, domainResult.BestPreset, domainResult.BestSpeed/1024)
		} else {
			log.DiscoveryLogf("  ✗ [%s] No working DPI bypass found", di.Domain)
		}
	}

	log.DiscoveryLogf("═══════════════════════════════════════")
}
