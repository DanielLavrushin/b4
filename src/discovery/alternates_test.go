package discovery

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestEveryGroupListsTheAlternativesItOffers(t *testing.T) {
	ds := settleSuite(t, "a.example", "b.example", "open.example")
	for _, d := range []string{"a.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.put(d, "cached-1-win", CheckStatusComplete, PhaseCached, 0)
		for i := 0; i < 15; i++ {
			name := fmt.Sprintf("alt-%02d", i)
			ds.put(d, name, CheckStatusComplete, PhaseStrategy, 1)
			ds.domainResults[d].Results[name].Speed = float64(100 + i)
		}
	}
	ds.domainResults["a.example"].Results["alt-00"].Speed = 9999
	ds.domainResults["b.example"].Results["alt-14"].Speed = 113
	ds.put("a.example", "only-a", CheckStatusComplete, PhaseStrategy, 1)
	ds.domainResults["a.example"].Results["only-a"].Speed = 5000
	ds.put("open.example", presetNoBypass, CheckStatusComplete, PhaseBaseline, 0)
	ds.put("open.example", "alt-x", CheckStatusComplete, PhaseStrategy, 1)
	ds.determineBest()

	ds.buildStrategyGroups()

	if len(ds.StrategyGroups) != 1 || ds.StrategyGroups[0].WinnerPreset != "cached-1-win" {
		t.Fatalf("precondition: one group won by the cached preset, got %+v", ds.StrategyGroups)
	}
	want := []string{"alt-13", "alt-14", "alt-12", "alt-11", "alt-10", "alt-09", "alt-08", "alt-07", "alt-06", "alt-05", "alt-04", "alt-03"}
	if got := ds.StrategyGroups[0].Alternates; !reflect.DeepEqual(got, want) {
		t.Fatalf("the group offers the %d slowest-site-fastest presets that work on every site, ties by name, not the winner, got %v", maxAlternateSets, got)
	}
	for _, d := range []string{"a.example", "b.example"} {
		if !reflect.DeepEqual(ds.domainResults[d].Alternates, want) {
			t.Errorf("%s carries its group's list, got %v", d, ds.domainResults[d].Alternates)
		}
	}
	if got := ds.domainResults["open.example"].Alternates; !reflect.DeepEqual(got, []string{"alt-x"}) {
		t.Errorf("a site that loads without b4 lists its own working presets, got %v", got)
	}
}

func TestHistoryKeepsTheSetsOfWhatTheCardOffered(t *testing.T) {
	ds := settleSuite(t, "meduza.io")
	ds.put("meduza.io", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("meduza.io", "cached-1-win", CheckStatusComplete, PhaseCached, 0)
	ds.domainResults["meduza.io"].Results["cached-1-win"].Speed = 100
	ds.domainResults["meduza.io"].Results["cached-1-win"].Set.Name = "cached-1-win"
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("fast-%02d", i)
		ds.put("meduza.io", name, CheckStatusComplete, PhaseStrategy, 1)
		ds.domainResults["meduza.io"].Results[name].Speed = float64(200 + i)
	}
	ds.determineBest()
	ds.buildStrategyGroups()
	ds.Status = CheckStatusComplete
	ds.EndTime = time.Now()
	ds.DomainDiscoveryResults = ds.domainResults
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	SaveToHistory(ds.CheckSuite, cfgPath)

	e := LoadDiscoveryHistory(cfgPath).Entries[0]
	listed := ds.StrategyGroups[0].Alternates
	if e.BestPreset != "fast-19" || len(listed) != maxAlternateSets || listed[0] != e.BestPreset {
		t.Fatalf("precondition: the fastest preset leads the list of a single site, best %q list %v", e.BestPreset, listed)
	}
	if !reflect.DeepEqual(e.Alternates, listed) {
		t.Errorf("history stores the list the card offered, got %v", e.Alternates)
	}
	kept := map[string]bool{}
	for name, r := range e.Results {
		if r.Set != nil {
			kept[name] = true
		}
	}
	if len(kept) != len(listed) {
		t.Errorf("history keeps the sets of exactly the listed presets and the best one, got %v", kept)
	}
	for _, name := range listed {
		if !kept[name] {
			t.Errorf("%s was offered and must stay applicable from history", name)
		}
	}
	if e.Set == nil || e.Set.Name != "cached-1-win" {
		t.Errorf("the group's own set stays the main offer: %+v", e.Set)
	}
}

func TestCachedPresetsRankInTheCachesOrder(t *testing.T) {
	if !presetRanksBefore("cached-2-desync", PhaseCached, 0, "cached-10-fake_sni", PhaseCached, 0) {
		t.Error("cached-2 has worked more often than cached-10 and must rank first")
	}
	if presetRanksBefore("cached-10-fake_sni", PhaseCached, 0, "cached-2-desync", PhaseCached, 0) {
		t.Error("the numeric order holds both ways")
	}
	if !presetRanksBefore("cached-2-desync", PhaseCached, 0, "combo-pastseq", PhaseCached, 0) {
		t.Error("a name that is not a cached preset falls back to the name order")
	}

	ds := settleSuite(t, "a.example")
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", "cached-10-fake_sni", CheckStatusComplete, PhaseCached, 0)
	ds.put("a.example", "cached-2-desync", CheckStatusComplete, PhaseCached, 0)
	ds.determineBest()
	ds.buildStrategyGroups()
	if len(ds.StrategyGroups) != 1 || ds.StrategyGroups[0].WinnerPreset != "cached-2-desync" {
		t.Fatalf("the group follows the cache's order, got %+v", ds.StrategyGroups)
	}
	if got := coveringPresets([]string{"a.example"}, ds.domainResults); !reflect.DeepEqual(got, []string{"cached-2-desync", "cached-10-fake_sni"}) {
		t.Errorf("a set run tries the cached presets in the same order, got %v", got)
	}
}
