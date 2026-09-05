package detector

import (
	"time"

	"github.com/daniellavrushin/b4/log"
)

func (s *Suite) Run(configPath string) {
	s.mu.Lock()
	s.Status = StatusRunning
	s.StartTime = time.Now()
	s.Progress.Total = s.estimateTotal()
	s.mu.Unlock()

	log.DiscoveryLogf("[Detector] Run %s: %d sites, scopes %v, mode %s, %s, parallel %d",
		s.Id, len(s.Options.Sites), s.Options.Scopes, s.Options.FetchMode, s.Options.IPVersion, s.Options.Parallel)

	s.runNetwork()

	for _, scope := range s.Options.Scopes {
		if s.canceled() {
			break
		}
		switch scope {
		case ScopeSites:
			s.runSites()
		case ScopeDNS:
			s.runDNS()
		case ScopeHosting:
			s.runHosting()
		case ScopeTelegram:
			s.runTelegram()
		}
		s.refreshVerdict()
	}

	s.mu.Lock()
	if s.Status == StatusRunning {
		s.Status = StatusComplete
	}
	s.EndTime = time.Now()
	s.Progress.Phase = ""
	s.Progress.Current = ""
	s.mu.Unlock()
	s.refreshVerdict()

	log.DiscoveryLogf("[Detector] Run %s %s in %v", s.Id, s.Status, s.EndTime.Sub(s.StartTime).Round(time.Second))

	SaveToHistory(s, configPath)
	go func() {
		time.Sleep(60 * time.Second)
		suitesMu.Lock()
		delete(activeSuites, s.Id)
		suitesMu.Unlock()
	}()
}

func (s *Suite) estimateTotal() int {
	lists := Lists()
	total := 0
	for _, scope := range s.Options.Scopes {
		switch scope {
		case ScopeSites:
			total += len(uniqueSites(s.Options.Sites)) * s.modes() * len(s.families())
		case ScopeDNS:
			total += len(lists.DNSServers) + len(readResolvConf())
		case ScopeHosting:
			total += len(lists.TCPTargets)
		case ScopeTelegram:
			total += 7
		}
	}
	return total
}

func (s *Suite) modes() int {
	if s.Options.FetchMode == FetchBoth {
		return 2
	}
	return 1
}

func (s *Suite) refreshVerdict() {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := Verdict{BlockKinds: map[string]int{}}
	if r := s.Sites; r != nil {
		v.Sites = len(r.Sites)
		for _, site := range r.Sites {
			if !site.Done {
				continue
			}
			switch site.Outcome {
			case OutcomeOk:
				v.NotBlocked++
			case OutcomeFixed:
				v.BlockedByISP++
				v.FixedByB4++
			case OutcomeStillBlocked, OutcomeBlocked:
				v.BlockedByISP++
				v.StillBlocked++
				v.StillBlockedAt = append(v.StillBlockedAt, site.Domain)
			case OutcomeBrokenByB4:
				v.NotBlocked++
				v.BrokenByB4++
			}
			if site.Direct != nil && isBlockedStatus(site.Direct.Status) {
				v.BlockKinds[string(site.Direct.Status)]++
			}
		}
	}
	if r := s.DNS; r != nil {
		v.DNSHijacked = r.Hijacked > 0
		v.DNSSubstituted = r.Substituting > 0
		v.DoHWorks = r.DoHOk > 0
		v.DoTWorks = r.DoTOk > 0
	}
	if r := s.Hosting; r != nil {
		for _, g := range r.Groups {
			if g.Status == HostingDropped || g.Status == HostingMixed {
				v.DroppedNets = append(v.DroppedNets, g.Provider)
			}
		}
	}
	if r := s.Telegram; r != nil {
		v.Telegram = string(r.Verdict)
	}
	if len(v.BlockKinds) == 0 {
		v.BlockKinds = nil
	}
	s.Verdict = v
}
