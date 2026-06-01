package detector

import (
	"context"
	"fmt"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

const sniBatchSize = 5

// asnCandidate represents the best DETECTED IP for an ASN.
type asnCandidate struct {
	ASN      string
	Provider string
	IP       string
	RTT      float64
}

func (s *DetectorSuite) runSNICheck(ctx context.Context) *SNIResult {
	log.DiscoveryLogf("[Detector] Starting SNI whitelist brute-force test")

	candidates := s.identifyBlockedASNs(ctx)

	if len(candidates) == 0 {
		log.DiscoveryLogf("[Detector] No blocked ASNs found, skipping SNI brute-force")
		return &SNIResult{
			Summary: "No blocked ASNs detected — SNI brute-force not needed",
		}
	}

	log.DiscoveryLogf("[Detector] Found %d blocked ASNs, starting SNI brute-force with %d candidates",
		len(candidates), len(WhitelistSNI))

	asnResults := s.bruteForceASNs(ctx, candidates)

	result := &SNIResult{
		ASNResults:  asnResults,
		TestedCount: len(candidates),
	}
	for _, r := range asnResults {
		if r.Status == SNIFound {
			result.FoundCount++
		}
	}

	result.Summary = fmt.Sprintf("Tested %d blocked ASNs, found white SNI for %d",
		result.TestedCount, result.FoundCount)

	log.DiscoveryLogf("[Detector] SNI check complete: %s", result.Summary)
	return result
}

// identifyBlockedASNs finds ASNs with TSPU drops, picking the best IP per ASN.
// Reuses existing TCP results if available.
func (s *DetectorSuite) identifyBlockedASNs(ctx context.Context) []asnCandidate {
	s.mu.RLock()
	tcpResult := s.TCPResult
	s.mu.RUnlock()

	var targetResults []TCPTargetResult

	if tcpResult != nil && len(tcpResult.Targets) > 0 {
		targetResults = tcpResult.Targets
	} else {
		targetResults = s.runBaseProbe(ctx)
	}

	return pickCandidates(targetResults)
}

// runBaseProbe runs a quick fat probe against all port-443 targets.
func (s *DetectorSuite) runBaseProbe(ctx context.Context) []TCPTargetResult {
	var port443Targets []TCPTarget
	for _, t := range TCPTargets {
		if t.Port == 443 {
			port443Targets = append(port443Targets, t)
		}
	}

	log.DiscoveryLogf("[Detector] SNI Phase 1: probing %d port-443 targets", len(port443Targets))

	results := make([]TCPTargetResult, len(port443Targets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 15)

	for i, target := range port443Targets {
		if s.isCanceled() {
			break
		}
		wg.Add(1)
		go func(idx int, tgt TCPTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fp := FatProbe(ctx, s.mark, tgt.IP, tgt.Port, tgt.SNI, 0)
			results[idx] = TCPTargetResult{
				Target:   tgt,
				Alive:    fp.Alive,
				RTT:      fp.RTT,
				DropAtKB: fp.DropAtKB,
				Detail:   fp.Detail,
				Status:   fatProbeResultToStatus(fp),
			}

			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()
		}(i, target)
	}

	wg.Wait()
	return results
}

// pickCandidates groups results by ASN and picks the DETECTED IP with lowest RTT per ASN.
func pickCandidates(results []TCPTargetResult) []asnCandidate {
	best := make(map[string]*asnCandidate)

	for _, tr := range results {
		if tr.Status != TCPDetected {
			continue
		}
		asn := tr.Target.ASN
		existing, ok := best[asn]
		if !ok || (tr.RTT > 0 && (existing.RTT <= 0 || tr.RTT < existing.RTT)) {
			best[asn] = &asnCandidate{
				ASN:      asn,
				Provider: tr.Target.Provider,
				IP:       tr.Target.IP,
				RTT:      tr.RTT,
			}
		}
	}

	candidates := make([]asnCandidate, 0, len(best))
	for _, c := range best {
		candidates = append(candidates, *c)
	}
	return candidates
}

// bruteForceASNs tries whitelist SNI values against each blocked ASN.
func (s *DetectorSuite) bruteForceASNs(ctx context.Context, candidates []asnCandidate) []SNIASNResult {
	results := make([]SNIASNResult, len(candidates))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 15)

	for i, cand := range candidates {
		if s.isCanceled() {
			break
		}
		wg.Add(1)
		go func(idx int, c asnCandidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			foundSNI := s.probeSNIForASN(ctx, c)

			r := SNIASNResult{
				ASN:      c.ASN,
				Provider: c.Provider,
				IP:       c.IP,
			}
			if foundSNI != "" {
				r.Status = SNIFound
				r.FoundSNI = foundSNI
			} else {
				r.Status = SNINotFound
			}
			results[idx] = r

			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()
		}(i, cand)
	}

	wg.Wait()
	return results
}

// probeSNIForASN tries all whitelist SNI values against a single IP in batches.
// Returns the first working SNI, or empty string if none found.
func (s *DetectorSuite) probeSNIForASN(ctx context.Context, cand asnCandidate) string {
	for batchStart := 0; batchStart < len(WhitelistSNI); batchStart += sniBatchSize {
		if s.isCanceled() {
			return ""
		}

		batchEnd := batchStart + sniBatchSize
		if batchEnd > len(WhitelistSNI) {
			batchEnd = len(WhitelistSNI)
		}
		batch := WhitelistSNI[batchStart:batchEnd]

		found := s.probeSNIBatch(ctx, cand, batch)
		if found != "" {
			return found
		}
	}
	return ""
}

// probeSNIBatch tests a batch of SNI values concurrently, returning the first that passes.
func (s *DetectorSuite) probeSNIBatch(ctx context.Context, cand asnCandidate, batch []string) string {
	type sniResult struct {
		sni string
		ok  bool
	}

	results := make(chan sniResult, len(batch))
	batchCtx, batchCancel := context.WithCancel(ctx)
	defer batchCancel()

	var wg sync.WaitGroup
	for _, sniVal := range batch {
		wg.Add(1)
		go func(sni string) {
			defer wg.Done()
			fp := FatProbe(batchCtx, s.mark, cand.IP, 443, sni, cand.RTT)
			results <- sniResult{sni: sni, ok: !fp.Detected && fp.Alive}
		}(sniVal)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.ok {
			batchCancel() // cancel remaining probes in this batch
			label := r.sni
			if label == "" {
				label = "(no SNI)"
			}
			log.DiscoveryLogf("[Detector] Found white SNI for AS%s: %s", cand.ASN, label)
			return r.sni
		}
	}

	return ""
}
