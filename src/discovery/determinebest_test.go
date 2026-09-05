package discovery

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func suiteWithResults(results map[string]map[string]*DomainPresetResult) *DiscoverySuite {
	ds := &DiscoverySuite{
		CheckSuite:    NewCheckSuite(nil),
		domainResults: map[string]*DomainDiscoveryResult{},
	}
	for domain, presets := range results {
		ds.domainResults[domain] = &DomainDiscoveryResult{Domain: domain, Results: presets}
	}
	return ds
}

func presetResult(status CheckStatus, speed float64) *DomainPresetResult {
	return &DomainPresetResult{Status: status, Speed: speed, Set: &config.SetConfig{}}
}

func TestDetermineBestReportsNoBypassWhenTheBaselineWorks(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"open.example": {
			presetNoBypass: presetResult(CheckStatusComplete, 11000),
			"combo-random": presetResult(CheckStatusComplete, 13000),
		},
	})

	ds.determineBest()

	dr := ds.domainResults["open.example"]
	if !dr.BaselineWorks {
		t.Fatalf("BaselineWorks = false, want true")
	}
	if dr.BestPreset != presetNoBypass {
		t.Fatalf("BestPreset = %q, want %q", dr.BestPreset, presetNoBypass)
	}
	if dr.BestSpeed != 11000 || dr.BaselineSpeed != 11000 {
		t.Fatalf("BestSpeed = %v, BaselineSpeed = %v, want 11000 for both", dr.BestSpeed, dr.BaselineSpeed)
	}
	if !dr.BestSuccess {
		t.Fatalf("BestSuccess = false, want true")
	}
}

func TestDetermineBestPicksTheFastestPresetWhenTheBaselineFails(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"blocked.example": {
			presetNoBypass:  presetResult(CheckStatusFailed, 0),
			"combo-random":  presetResult(CheckStatusComplete, 4000),
			"fake-ttl8":     presetResult(CheckStatusComplete, 9000),
			"disorder-full": presetResult(CheckStatusFailed, 0),
		},
	})

	ds.determineBest()

	dr := ds.domainResults["blocked.example"]
	if dr.BaselineWorks {
		t.Fatalf("BaselineWorks = true, want false")
	}
	if dr.BestPreset != "fake-ttl8" {
		t.Fatalf("BestPreset = %q, want fake-ttl8", dr.BestPreset)
	}
	if dr.BaselineSpeed != 0 {
		t.Fatalf("BaselineSpeed = %v, want 0 when the baseline failed", dr.BaselineSpeed)
	}
}

func TestDetermineBestWithoutABaselineResultStillPicksAWinner(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"nobaseline.example": {
			"combo-random": presetResult(CheckStatusComplete, 2500),
		},
	})

	ds.determineBest()

	dr := ds.domainResults["nobaseline.example"]
	if dr.BaselineWorks || dr.BestPreset != "combo-random" || !dr.BestSuccess {
		t.Fatalf("got BaselineWorks=%v BestPreset=%q BestSuccess=%v", dr.BaselineWorks, dr.BestPreset, dr.BestSuccess)
	}
}

func TestDetermineBestIsIdempotent(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"blocked.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
			"fake-ttl8":    presetResult(CheckStatusComplete, 9000),
		},
		"open.example": {
			presetNoBypass: presetResult(CheckStatusComplete, 7000),
		},
	})

	ds.determineBest()
	first := *ds.domainResults["blocked.example"]
	firstOpen := *ds.domainResults["open.example"]

	ds.determineBest()
	ds.determineBest()

	if got := *ds.domainResults["blocked.example"]; got.BestPreset != first.BestPreset ||
		got.BestSpeed != first.BestSpeed || got.BaselineSpeed != first.BaselineSpeed || got.BaselineWorks != first.BaselineWorks {
		t.Fatalf("blocked domain drifted: %+v then %+v", first, got)
	}
	if got := *ds.domainResults["open.example"]; got.BestPreset != firstOpen.BestPreset ||
		got.BestSpeed != firstOpen.BestSpeed || got.BaselineSpeed != firstOpen.BaselineSpeed || got.BaselineWorks != firstOpen.BaselineWorks {
		t.Fatalf("open domain drifted: %+v then %+v", firstOpen, got)
	}
}

func TestDetermineBestDemotedWinnerFallsBackToTheNextPreset(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"blocked.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
			"fake-ttl8":    presetResult(CheckStatusComplete, 9000),
			"combo-random": presetResult(CheckStatusComplete, 4000),
		},
	})

	ds.determineBest()
	if got := ds.domainResults["blocked.example"].BestPreset; got != "fake-ttl8" {
		t.Fatalf("BestPreset = %q, want fake-ttl8", got)
	}

	ds.demoteWinner("fake-ttl8", []string{"blocked.example"}, "test")
	ds.determineBest()

	if got := ds.domainResults["blocked.example"].BestPreset; got != "combo-random" {
		t.Fatalf("after demotion BestPreset = %q, want combo-random", got)
	}
}

func TestBuildStrategyGroupsSkipsDomainsThatNeedNoBypass(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"open.example": {
			presetNoBypass: presetResult(CheckStatusComplete, 7000),
			"fake-ttl8":    presetResult(CheckStatusComplete, 8000),
		},
		"blocked.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
			"fake-ttl8":    presetResult(CheckStatusComplete, 9000),
		},
	})

	ds.determineBest()
	ds.buildStrategyGroups()

	if len(ds.StrategyGroups) != 1 {
		t.Fatalf("got %d groups, want 1", len(ds.StrategyGroups))
	}
	if got := ds.StrategyGroups[0].Domains; len(got) != 1 || got[0] != "blocked.example" {
		t.Fatalf("group domains = %v, want [blocked.example]", got)
	}
}

func TestWinnersToConfirmCoversBothTheDomainBestAndTheGroupWinner(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"a.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
			"broad":        presetResult(CheckStatusComplete, 3000),
			"fast":         presetResult(CheckStatusComplete, 9000),
		},
		"b.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
			"broad":        presetResult(CheckStatusComplete, 2500),
		},
	})

	ds.determineBest()
	ds.buildStrategyGroups()

	if got := ds.domainResults["a.example"].BestPreset; got != "fast" {
		t.Fatalf("a.example BestPreset = %q, want fast", got)
	}
	if len(ds.StrategyGroups) != 1 || ds.StrategyGroups[0].WinnerPreset != "broad" {
		t.Fatalf("groups = %+v, want one group won by broad", ds.StrategyGroups)
	}

	pending := ds.winnersToConfirm()
	if got := pending["fast"]; len(got) != 1 || got[0] != "a.example" {
		t.Fatalf("pending[fast] = %v, want [a.example]", got)
	}
	if got := pending["broad"]; len(got) != 2 {
		t.Fatalf("pending[broad] = %v, want both domains", got)
	}
}

func TestWinnersToConfirmSkipsAlreadyConfirmedPresets(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"a.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
			"fast":         presetResult(CheckStatusComplete, 9000),
		},
	})

	ds.determineBest()
	ds.buildStrategyGroups()

	if len(ds.winnersToConfirm()) == 0 {
		t.Fatal("the winner must start out unconfirmed")
	}

	ds.recordConfirmation("a.example", "fast", confirmTries)

	if got := ds.winnersToConfirm(); len(got) != 0 {
		t.Fatalf("a confirmed winner must not be queued again, got %v", got)
	}
	if dr := ds.domainResults["a.example"]; dr.Confirmed != confirmTries || dr.ConfirmTries != confirmTries {
		t.Fatalf("the domain mirror was not updated: %d/%d", dr.Confirmed, dr.ConfirmTries)
	}
}

func TestOutcomesFollowTheVerdict(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"open.example": {
			presetNoBypass: presetResult(CheckStatusComplete, 11000),
		},
		"blocked.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
			"combo-random": presetResult(CheckStatusComplete, 4000),
		},
		"dead.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
		},
		"lost.example": {
			presetNoBypass: presetResult(CheckStatusFailed, 0),
		},
	})
	ds.domainResults["dead.example"].DNSResult = &DNSDiscoveryResult{TransportBlocked: true}

	ds.determineBest()

	want := map[string]Outcome{
		"open.example":    OutcomeWorksWithoutBypass,
		"blocked.example": OutcomeFound,
		"dead.example":    OutcomeAddressBlocked,
		"lost.example":    "",
	}
	for domain, outcome := range want {
		if got := ds.domainResults[domain].Outcome; got != outcome {
			t.Errorf("%s: outcome %q, want %q while the run is still going", domain, got, outcome)
		}
	}
	if !ds.domainResults["blocked.example"].Unconfirmed {
		t.Error("a winner that has not been through the confirmation pass must read as unconfirmed")
	}

	ds.refreshOutcomes(true)
	if got := ds.domainResults["lost.example"].Outcome; got != OutcomeNotFound {
		t.Errorf("once the run is over a domain with no working preset is %q, got %q", OutcomeNotFound, got)
	}

	ds.domainResults["blocked.example"].Confirmed = confirmTries
	ds.domainResults["blocked.example"].ConfirmTries = confirmTries
	ds.refreshOutcomes(true)
	if ds.domainResults["blocked.example"].Unconfirmed {
		t.Error("a winner that passed every confirmation try is confirmed")
	}
}

func TestFailedResultsCarryNoSet(t *testing.T) {
	ds := suiteWithResults(map[string]map[string]*DomainPresetResult{
		"a.example": {},
	})
	set := config.NewSetConfig()

	ds.storeResultsMulti(ConfigPreset{Name: "combo-random", Family: FamilyCombo}, map[string]CheckResult{
		"a.example": {Domain: "a.example", Status: CheckStatusFailed, Set: &set},
	})
	if ds.domainResults["a.example"].Results["combo-random"].Set != nil {
		t.Error("a failed preset is never applied, so its set is dead weight in every status poll")
	}
	if got := ds.domainResults["a.example"].Outcome; got != "" {
		t.Errorf("nothing has worked yet and the run is not over, outcome should be open, got %q", got)
	}

	ds.storeResultsMulti(ConfigPreset{Name: "combo-pastseq", Family: FamilyCombo}, map[string]CheckResult{
		"a.example": {Domain: "a.example", Status: CheckStatusComplete, Speed: 1000, Set: &set},
	})
	if ds.domainResults["a.example"].Results["combo-pastseq"].Set == nil {
		t.Error("the set of a working preset is the one that gets applied")
	}
	if got := ds.domainResults["a.example"].Outcome; got != OutcomeFound {
		t.Errorf("the first success makes the domain read as found, got %q", got)
	}
	if !ds.domainResults["a.example"].Unconfirmed {
		t.Error("a first success is provisional until the confirmation pass")
	}
}
