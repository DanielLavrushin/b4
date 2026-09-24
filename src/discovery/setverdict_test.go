package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func setRunFixture(t *testing.T, domains ...string) *DiscoverySuite {
	t.Helper()
	inputs := make([]DomainInput, 0, len(domains))
	for _, d := range domains {
		inputs = append(inputs, DomainInput{Domain: d, CheckURL: "https://" + d + "/"})
	}
	suite := NewCheckSuite(inputs)
	suite.Status = CheckStatusRunning
	suite.CurrentPhase = PhaseStrategy
	suite.SetId = "set-a"
	ds := &DiscoverySuite{
		CheckSuite:      suite,
		domainResults:   map[string]*DomainDiscoveryResult{},
		dnsResults:      map[string]*DNSDiscoveryResult{},
		stopWhenCovered: true,
	}
	for _, d := range domains {
		ds.domainResults[d] = &DomainDiscoveryResult{Domain: d, Url: "https://" + d + "/", Results: map[string]*DomainPresetResult{}}
	}
	ds.initCancelContext()
	t.Cleanup(ds.ctxCancel)
	return ds
}

func strategySet(strategy string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Fragmentation.Strategy = strategy
	return &set
}

func (ds *DiscoverySuite) put(domain, preset string, status CheckStatus, phase DiscoveryPhase, priority int) {
	r := &DomainPresetResult{PresetName: preset, Status: status, Phase: phase, Priority: priority, Family: FamilyTCPFrag}
	if status == CheckStatusComplete {
		r.Speed = 1000
		r.Set = strategySet(preset)
	}
	ds.domainResults[domain].Results[preset] = r
}

func (ds *DiscoverySuite) store(preset string, phase DiscoveryPhase, statuses map[string]CheckStatus) {
	results := map[string]CheckResult{}
	for domain, status := range statuses {
		r := CheckResult{Domain: domain, Status: status}
		if status == CheckStatusComplete {
			r.Speed = 1000
			r.Set = strategySet(preset)
		}
		results[domain] = r
	}
	ds.storeResultsMulti(ConfigPreset{Name: preset, Family: FamilyTCPFrag, Phase: phase}, results)
}

type fakeJointConfirm struct {
	ds     *DiscoverySuite
	calls  []string
	passes map[string]map[string]int
	during func()
}

func (f *fakeJointConfirm) confirm(name string, domains []string) (map[string]int, bool) {
	f.calls = append(f.calls, name)
	if f.during != nil {
		f.during()
	}
	passes := map[string]int{}
	for _, d := range domains {
		passes[d] = confirmTries
		if want, ok := f.passes[name][d]; ok {
			passes[d] = want
		}
		f.ds.recordConfirmation(d, name, passes[d])
	}
	return passes, true
}

func withFakeConfirm(ds *DiscoverySuite, passes map[string]map[string]int) *fakeJointConfirm {
	f := &fakeJointConfirm{ds: ds, passes: passes}
	ds.jointConfirmFn = f.confirm
	return f
}

func TestCoveringPresetsNeedEveryAddressAndPutTheSetFirst(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	for _, d := range []string{"a.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusComplete, PhaseBaseline, 0)
		ds.put(d, presetAltAddress, CheckStatusComplete, PhaseBaseline, 0)
		ds.put(d, presetDNSRedirect, CheckStatusComplete, PhaseBaseline, 0)
		ds.put(d, "combo-late", CheckStatusComplete, PhaseCombination, 0)
		ds.put(d, "tcp-slow", CheckStatusComplete, PhaseStrategy, 5)
		ds.put(d, "tcp-fast", CheckStatusComplete, PhaseStrategy, 1)
		ds.put(d, "cached-1-combo", CheckStatusComplete, PhaseCached, 0)
		ds.put(d, presetSetCurrent, CheckStatusComplete, PhaseCached, 0)
	}
	ds.put("a.example", "only-a", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("a.example", "fails-on-b", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("b.example", "fails-on-b", CheckStatusFailed, PhaseStrategy, 0)
	ds.put("a.example", "no-set", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("b.example", "no-set", CheckStatusComplete, PhaseStrategy, 0)
	ds.domainResults["b.example"].Results["no-set"].Set = nil

	got := coveringPresets(ds.runDomains(), ds.domainResults)
	want := []string{presetSetCurrent, "cached-1-combo", "tcp-fast", "tcp-slow", "combo-late"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coveringPresets = %v, want %v", got, want)
	}
}

func TestPresetRankingIsSharedWithTheGroups(t *testing.T) {
	if !presetRanksBefore("z", PhaseCached, 9, "a", PhaseStrategy, 0) {
		t.Error("an earlier phase outranks a later one whatever the name or priority")
	}
	if !presetRanksBefore("z", PhaseStrategy, 1, "a", PhaseStrategy, 2) {
		t.Error("within a phase the lower priority wins")
	}
	if !presetRanksBefore("a", PhaseOptimize, 0, "b", PhaseOptimize, 0) {
		t.Error("the name breaks the last tie")
	}
}

func TestSetVerdictCovered(t *testing.T) {
	ds := setRunFixture(t, "blocked.example", "open.example")
	ds.put("blocked.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("open.example", presetNoBypass, CheckStatusComplete, PhaseBaseline, 0)
	ds.put("blocked.example", "combo", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("open.example", "combo", CheckStatusComplete, PhaseStrategy, 0)
	ds.determineBest()
	ds.buildStrategyGroups()

	v := ds.buildSetVerdict(ds.runDomains(), "combo")
	if v.Status != SetVerdictCovered || v.WinnerPreset != "combo" || !v.Confirmed {
		t.Fatalf("verdict = %+v, want a confirmed cover by combo", v)
	}
	if !reflect.DeepEqual(v.Covered, []string{"blocked.example", "open.example"}) || len(v.Uncovered) != 0 {
		t.Errorf("covered = %v uncovered = %v, want every address covered", v.Covered, v.Uncovered)
	}
	if !reflect.DeepEqual(v.NoBypass, []string{"open.example"}) {
		t.Errorf("no_bypass = %v, want the address that loads without b4", v.NoBypass)
	}
	if v.Set == nil || !reflect.DeepEqual(v.Set.Targets.SNIDomains, []string{"blocked.example", "open.example"}) {
		t.Errorf("the verdict set must be scoped to every address of the run, got %+v", v.Set)
	}
}

func TestSetVerdictCurrentWorks(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	for _, d := range []string{"a.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.put(d, presetSetCurrent, CheckStatusComplete, PhaseCached, 0)
	}
	ds.determineBest()

	v := ds.buildSetVerdict(ds.runDomains(), presetSetCurrent)
	if v.Status != SetVerdictCurrentWorks || v.WinnerPreset != presetSetCurrent {
		t.Fatalf("verdict = %+v, want current_works", v)
	}
}

func TestSetVerdictPartialPicksTheLargestGroup(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example", "c.example", "d.example")
	for _, d := range []string{"a.example", "b.example", "c.example", "d.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	}
	ds.put("a.example", "solo", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("b.example", "pair", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("c.example", "pair", CheckStatusComplete, PhaseStrategy, 0)
	ds.determineBest()
	ds.buildStrategyGroups()

	v := ds.buildSetVerdict(ds.runDomains(), "")
	if v.Status != SetVerdictPartial || v.WinnerPreset != "pair" {
		t.Fatalf("verdict = %+v, want partial with the two-address group", v)
	}
	if !reflect.DeepEqual(v.Covered, []string{"b.example", "c.example"}) {
		t.Errorf("covered = %v", v.Covered)
	}
	if !reflect.DeepEqual(v.Uncovered, []string{"a.example", "d.example"}) {
		t.Errorf("uncovered = %v, want the other group and the lost address in run order", v.Uncovered)
	}
	if v.Set == nil || !reflect.DeepEqual(v.Set.Targets.SNIDomains, []string{"b.example", "c.example"}) {
		t.Errorf("the partial set is the group's own scoped set, got %+v", v.Set)
	}
}

func TestSetVerdictPartialTieGoesToTheFirstAddress(t *testing.T) {
	ds := setRunFixture(t, "z.example", "a.example")
	for _, d := range []string{"z.example", "a.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	}
	ds.put("a.example", "alpha", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("z.example", "zulu", CheckStatusComplete, PhaseStrategy, 0)
	ds.determineBest()
	ds.buildStrategyGroups()
	if len(ds.StrategyGroups) != 2 || ds.StrategyGroups[0].WinnerPreset != "alpha" {
		t.Fatalf("precondition: alpha's group comes first, got %+v", ds.StrategyGroups)
	}

	v := ds.buildSetVerdict(ds.runDomains(), "")
	if v.Status != SetVerdictPartial || v.WinnerPreset != "zulu" {
		t.Fatalf("verdict = %+v, want the group holding the run's first address", v)
	}
	if !reflect.DeepEqual(v.Uncovered, []string{"a.example"}) {
		t.Errorf("uncovered = %v", v.Uncovered)
	}

	if got := largestGroup(ds.StrategyGroups, []string{"q.example"}); got == nil || got.WinnerPreset != "alpha" {
		t.Errorf("with no group holding the first address the first group wins, got %+v", got)
	}
}

func TestSetVerdictNotNeededAndNone(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	for _, d := range []string{"a.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusComplete, PhaseBaseline, 0)
		ds.put(d, "combo", CheckStatusComplete, PhaseStrategy, 0)
	}
	ds.determineBest()
	ds.buildStrategyGroups()
	if v := ds.buildSetVerdict(ds.runDomains(), "combo"); v.Status != SetVerdictNotNeeded || len(v.NoBypass) != 2 || v.Set != nil {
		t.Fatalf("every address loads without b4, verdict = %+v", v)
	}

	ds = setRunFixture(t, "a.example", "b.example")
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", "combo", CheckStatusFailed, PhaseStrategy, 0)
	ds.put("b.example", presetNoBypass, CheckStatusComplete, PhaseBaseline, 0)
	ds.determineBest()
	ds.buildStrategyGroups()
	v := ds.buildSetVerdict(ds.runDomains(), "")
	if v.Status != SetVerdictNone || !reflect.DeepEqual(v.Uncovered, []string{"a.example"}) || !reflect.DeepEqual(v.NoBypass, []string{"b.example"}) {
		t.Fatalf("nothing works for a.example, verdict = %+v", v)
	}
}

func TestSetVerdictIncompleteWhenCanceled(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.setVerdict = &SetVerdict{Status: SetVerdictCovered}
	ds.Status = CheckStatusCanceled
	ds.publishSetVerdictLocked()
	if ds.SetVerdict == nil || ds.SetVerdict.Status != SetVerdictIncomplete {
		t.Fatalf("a canceled run has no verdict to trust, got %+v", ds.SetVerdict)
	}

	ds = setRunFixture(t, "a.example")
	ds.publishSetVerdictLocked()
	if ds.SetVerdict == nil || ds.SetVerdict.Status != SetVerdictIncomplete {
		t.Fatalf("a run that never reached a verdict is incomplete, got %+v", ds.SetVerdict)
	}

	ds = setRunFixture(t, "a.example")
	ds.setVerdict = &SetVerdict{Status: SetVerdictPartial}
	ds.publishSetVerdictLocked()
	if ds.SetVerdict.Status != SetVerdictPartial {
		t.Fatalf("the resolved verdict is published as is, got %+v", ds.SetVerdict)
	}

	plain := setRunFixture(t, "a.example")
	plain.SetId = ""
	plain.publishSetVerdictLocked()
	if plain.SetVerdict != nil {
		t.Fatal("a run without a set carries no set verdict")
	}
}

func TestResolveSetVerdictConfirmsJointlyAndDemotesFailures(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	for _, d := range []string{"a.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.put(d, "first", CheckStatusComplete, PhaseCached, 0)
		ds.put(d, "second", CheckStatusComplete, PhaseStrategy, 0)
	}
	fake := withFakeConfirm(ds, map[string]map[string]int{"first": {"b.example": 1}})
	ds.determineBest()

	ds.resolveSetVerdict()

	if !reflect.DeepEqual(fake.calls, []string{"first", "second"}) {
		t.Fatalf("joint confirmations = %v, want first then second", fake.calls)
	}
	if r := ds.domainResults["b.example"].Results["first"]; r.Status != CheckStatusFailed || !strings.Contains(r.Error, "every address of the set") {
		t.Errorf("a candidate that fails the joint confirmation is demoted where it failed, got %+v", r)
	}
	if r := ds.domainResults["a.example"].Results["first"]; r.Status != CheckStatusComplete {
		t.Errorf("the address that passed keeps its result, got %+v", r)
	}
	if ds.setVerdict == nil || ds.setVerdict.Status != SetVerdictCovered || ds.setVerdict.WinnerPreset != "second" {
		t.Fatalf("verdict = %+v, want covered by second", ds.setVerdict)
	}
}

func TestResolveSetVerdictKeepsTheEarlyStopWinner(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	for _, d := range []string{"a.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.put(d, "first", CheckStatusComplete, PhaseCached, 0)
		ds.put(d, "winner", CheckStatusComplete, PhaseStrategy, 0)
		ds.recordConfirmation(d, "winner", confirmTries)
	}
	ds.coverWinner = "winner"
	ds.jointConfirmed = map[string]bool{"winner": true}
	fake := withFakeConfirm(ds, nil)
	ds.determineBest()

	ds.resolveSetVerdict()

	if len(fake.calls) != 0 {
		t.Errorf("the early-stop winner is already confirmed jointly, nothing to re-run: %v", fake.calls)
	}
	if ds.setVerdict == nil || ds.setVerdict.WinnerPreset != "winner" || ds.setVerdict.Status != SetVerdictCovered {
		t.Fatalf("verdict = %+v, want covered by the early-stop winner", ds.setVerdict)
	}
}

func TestResolveSetVerdictTriesThreeCandidatesAndTheLargestGroup(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	fails := map[string]map[string]int{}
	for _, name := range []string{"p1", "p2", "p3", "p4"} {
		for _, d := range []string{"a.example", "b.example"} {
			ds.put(d, name, CheckStatusComplete, PhaseStrategy, 0)
		}
		fails[name] = map[string]int{"b.example": 0}
	}
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("b.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	fake := withFakeConfirm(ds, fails)
	ds.determineBest()

	ds.resolveSetVerdict()

	if !reflect.DeepEqual(fake.calls, []string{"p1", "p2", "p3", "p4"}) {
		t.Fatalf("joint confirmations = %v, want three candidates plus the largest group's winner", fake.calls)
	}
	v := ds.setVerdict
	if v == nil || v.Status != SetVerdictPartial {
		t.Fatalf("verdict = %+v, want partial", v)
	}
	if !reflect.DeepEqual(v.Uncovered, []string{"b.example"}) || !reflect.DeepEqual(v.Covered, []string{"a.example"}) {
		t.Fatalf("covered=%v uncovered=%v, want a.example covered and b.example named as uncovered", v.Covered, v.Uncovered)
	}
}

func TestEarlyStopWaitsForTheBaselineThenStopsOnAConfirmedCover(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	fake := withFakeConfirm(ds, nil)

	ds.store(presetSetCurrent, PhaseCached, map[string]CheckStatus{"a.example": CheckStatusComplete, "b.example": CheckStatusComplete})
	if len(fake.calls) != 0 || ds.finishing() {
		t.Fatal("nothing may be decided before every address has a no-bypass result")
	}

	ds.store(presetNoBypass, PhaseBaseline, map[string]CheckStatus{"a.example": CheckStatusFailed, "b.example": CheckStatusComplete})
	if !reflect.DeepEqual(fake.calls, []string{presetSetCurrent}) {
		t.Fatalf("joint confirmations = %v, want the set's own strategy", fake.calls)
	}
	if !ds.finishing() || !ds.StoppedCovered || ds.StoppedEarly {
		t.Fatalf("a confirmed cover ends the search without reading as a user stop: finishing=%v covered=%v early=%v", ds.finishing(), ds.StoppedCovered, ds.StoppedEarly)
	}
	if ds.canceled() {
		t.Fatal("the early stop must not cancel the run, confirmation and the verdict still follow")
	}
	if ds.coverWinner != presetSetCurrent || ds.coverStop != coverStopCovered {
		t.Fatalf("winner = %q stop = %q", ds.coverWinner, ds.coverStop)
	}

	ds.store("later", PhaseStrategy, map[string]CheckStatus{"a.example": CheckStatusComplete, "b.example": CheckStatusComplete})
	if len(fake.calls) != 1 {
		t.Fatalf("a finished search is not checked again: %v", fake.calls)
	}
}

func TestEarlyStopWhenEveryAddressLoadsWithoutB4(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	fake := withFakeConfirm(ds, nil)

	ds.store(presetNoBypass, PhaseBaseline, map[string]CheckStatus{"a.example": CheckStatusComplete, "b.example": CheckStatusComplete})
	if !ds.finishing() || ds.coverStop != coverStopBaseline {
		t.Fatalf("finishing=%v stop=%q, want the search stopped as not needed", ds.finishing(), ds.coverStop)
	}
	if ds.StoppedCovered || len(fake.calls) != 0 {
		t.Fatalf("no strategy was confirmed, covered=%v calls=%v", ds.StoppedCovered, fake.calls)
	}
}

func TestEarlyStopTriesThreeCandidatesAtATimeAndNeverTwice(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	fails := map[string]map[string]int{}
	for _, name := range []string{"p1", "p2", "p3", "p4", "p5"} {
		fails[name] = map[string]int{"b.example": 2}
	}
	fake := withFakeConfirm(ds, fails)
	for _, d := range []string{"a.example", "b.example"} {
		for _, name := range []string{"p1", "p2", "p3", "p4"} {
			ds.put(d, name, CheckStatusComplete, PhaseStrategy, 0)
		}
	}

	ds.store(presetNoBypass, PhaseBaseline, map[string]CheckStatus{"a.example": CheckStatusFailed, "b.example": CheckStatusFailed})
	if !reflect.DeepEqual(fake.calls, []string{"p1", "p2", "p3"}) {
		t.Fatalf("first check confirmations = %v, want the first three", fake.calls)
	}
	if ds.finishing() {
		t.Fatal("no candidate covered every address, the search goes on")
	}
	for _, name := range []string{"p1", "p2", "p3"} {
		if r := ds.domainResults["b.example"].Results[name]; r.Status != CheckStatusFailed {
			t.Errorf("%s failed jointly on b.example and must be demoted there, got %+v", name, r)
		}
	}

	ds.store("p5", PhaseStrategy, map[string]CheckStatus{"a.example": CheckStatusComplete, "b.example": CheckStatusComplete})
	if !reflect.DeepEqual(fake.calls, []string{"p1", "p2", "p3"}) {
		t.Fatalf("confirmations = %v, want no more early-stop attempts after %d failures", fake.calls, maxCoverFailures)
	}
}

func TestEarlyStopNeedsTheOptionAndStaysOutOfConfirmation(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.stopWhenCovered = false
	fake := withFakeConfirm(ds, nil)
	ds.store(presetNoBypass, PhaseBaseline, map[string]CheckStatus{"a.example": CheckStatusFailed})
	ds.store("p1", PhaseStrategy, map[string]CheckStatus{"a.example": CheckStatusComplete})
	if len(fake.calls) != 0 || ds.finishing() {
		t.Fatal("without stop_when_covered the search runs in full")
	}

	ds = setRunFixture(t, "a.example")
	ds.CurrentPhase = PhaseConfirm
	fake = withFakeConfirm(ds, nil)
	ds.store(presetNoBypass, PhaseBaseline, map[string]CheckStatus{"a.example": CheckStatusFailed})
	ds.store("p1", PhaseStrategy, map[string]CheckStatus{"a.example": CheckStatusComplete})
	if len(fake.calls) != 0 {
		t.Fatal("the confirmation phase does its own confirming")
	}

	ds = setRunFixture(t, "a.example")
	ds.SetId = ""
	fake = withFakeConfirm(ds, nil)
	ds.store(presetNoBypass, PhaseBaseline, map[string]CheckStatus{"a.example": CheckStatusFailed})
	ds.store("p1", PhaseStrategy, map[string]CheckStatus{"a.example": CheckStatusComplete})
	if len(fake.calls) != 0 {
		t.Fatal("a run without a set never stops early")
	}
}

func TestEarlyStopInterruptedByTheUserDemotesNothing(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	fake := withFakeConfirm(ds, map[string]map[string]int{"p1": {"a.example": 1}})
	fake.during = func() {
		ds.CheckSuite.mu.Lock()
		ds.closeFinishLocked()
		ds.CheckSuite.mu.Unlock()
	}
	ds.store(presetNoBypass, PhaseBaseline, map[string]CheckStatus{"a.example": CheckStatusFailed})
	ds.store("p1", PhaseStrategy, map[string]CheckStatus{"a.example": CheckStatusComplete})

	r := ds.domainResults["a.example"].Results["p1"]
	if r.Status != CheckStatusComplete {
		t.Fatalf("fetches cut short by a stop prove nothing, the result must stay, got %+v", r)
	}
	if r.ConfirmTries != 0 || r.Confirmed != 0 {
		t.Fatalf("the cut-short confirmation must not count, so the final pass confirms it again: %d/%d", r.Confirmed, r.ConfirmTries)
	}
	if ds.StoppedCovered || ds.coverWinner != "" {
		t.Fatal("nothing was confirmed")
	}
}

func TestSetCurrentPresetCarriesOnlyTheStrategy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "captures"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte{0x16, 0x03, 0x01, 0x42}
	if err := os.WriteFile(filepath.Join(dir, "captures", "tls_example.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	set := config.NewSetConfig()
	set.TCP.ConnBytesLimit = 7
	set.UDP.Mode = "drop"
	set.Fragmentation.Strategy = "tls"
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = "captures/tls_example.bin"
	set.DNS = config.DNSConfig{Enabled: true, TargetDNS: "9.9.9.9", Pins: map[string][]string{"a.example": {"1.2.3.4"}}}
	set.Targets.SNIDomains = []string{"a.example"}

	ds := setRunFixture(t, "a.example")
	ds.cfg = &config.Config{ConfigPath: filepath.Join(dir, "config.json")}
	ds.setStrategy = &set

	preset, ok := ds.setCurrentPreset()
	if !ok {
		t.Fatal("a set run with the set's strategy tests it first")
	}
	if preset.Name != presetSetCurrent || preset.Family != FamilyCurrent || preset.Phase != PhaseCached || preset.Priority != 0 {
		t.Fatalf("preset = %s %s %s %d", preset.Name, preset.Family, preset.Phase, preset.Priority)
	}
	if preset.Config.TCP.ConnBytesLimit != 7 || preset.Config.UDP.Mode != "drop" || preset.Config.Fragmentation.Strategy != "tls" {
		t.Errorf("the strategy must come from the set: %+v", preset.Config)
	}
	if preset.Config.DNS.Enabled || len(preset.Config.DNS.Pins) > 0 || len(preset.Config.Targets.SNIDomains) > 0 {
		t.Errorf("DNS and targets stay out of the tested strategy: dns=%+v targets=%+v", preset.Config.DNS, preset.Config.Targets)
	}
	if !reflect.DeepEqual(preset.Config.Faking.PayloadData, payload) {
		t.Errorf("a capture payload is loaded the way a live set loads it, got %v", preset.Config.Faking.PayloadData)
	}

	ds.setStrategy = nil
	if _, ok := ds.setCurrentPreset(); ok {
		t.Error("without the set's strategy there is nothing to test first")
	}
}

func TestSetCurrentNeverEntersTheCache(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.cfg = &config.Config{ConfigPath: filepath.Join(t.TempDir(), "config.json")}
	ds.discoveryCache = &DiscoveryCache{}
	ds.put("a.example", presetSetCurrent, CheckStatusComplete, PhaseCached, 0)
	ds.put("a.example", "combo", CheckStatusComplete, PhaseStrategy, 0)

	ds.saveResultsToCache()

	if len(ds.discoveryCache.Entries) != 1 || ds.discoveryCache.Entries[0].Config.Fragmentation.Strategy != "combo" {
		t.Fatalf("only the searched strategy is cached, got %+v", ds.discoveryCache.Entries)
	}
}

func TestSetRunsAreKeptPerSetAndClearedWithTheHistory(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	older := time.Now().Add(-time.Hour)
	first := &CheckSuite{
		Id: "run-1", Status: CheckStatusComplete, StartTime: older, EndTime: older, SetId: "set-a",
		Domains:    []DomainInput{{Domain: "a.example", CheckURL: "https://a.example/x"}},
		SetVerdict: &SetVerdict{Status: SetVerdictPartial, Covered: []string{"a.example"}},
		DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
			"a.example": {Domain: "a.example", BestPreset: "combo", BestSuccess: true},
		},
	}
	SaveToHistory(first, cfgPath)
	second := &CheckSuite{
		Id: "run-2", Status: CheckStatusComplete, StartTime: time.Now(), EndTime: time.Now(), SetId: "set-a",
		Domains:    []DomainInput{{Domain: "a.example", CheckURL: "https://a.example/y"}, {Domain: "b.example", CheckURL: "https://b.example/"}},
		SetVerdict: &SetVerdict{Status: SetVerdictCovered, WinnerPreset: "combo", Covered: []string{"a.example", "b.example"}, Confirmed: true},
	}
	SaveToHistory(second, cfgPath)
	other := &CheckSuite{
		Id: "run-3", Status: CheckStatusComplete, EndTime: older.Add(time.Minute), SetId: "set-b",
		SetVerdict: &SetVerdict{Status: SetVerdictNone},
	}
	SaveToHistory(other, cfgPath)
	SaveToHistory(&CheckSuite{Id: "run-plain", Status: CheckStatusComplete, EndTime: time.Now()}, cfgPath)

	hist := LoadDiscoveryHistory(cfgPath)
	if len(hist.SetRuns) != 2 {
		t.Fatalf("one record per set, got %+v", hist.SetRuns)
	}
	rec := hist.SetRuns["set-a"]
	if rec.SuiteId != "run-2" || rec.Verdict.Status != SetVerdictCovered || !reflect.DeepEqual(rec.URLs, []string{"https://a.example/y", "https://b.example/"}) {
		t.Fatalf("the last run of the set replaces the earlier one, got %+v", rec)
	}
	runs := hist.SetRunsNewestFirst()
	if len(runs) != 2 || runs[0].SetId != "set-a" || runs[1].SetId != "set-b" {
		t.Fatalf("records newest first, got %+v", runs)
	}
	if found, ok := hist.SetRunForSuite("run-3"); !ok || found.SetId != "set-b" {
		t.Fatalf("a record is found by its suite, got %+v %v", found, ok)
	}

	if err := UpdateHistory(cfgPath, func(h *DiscoveryHistory) bool { h.Clear(); return true }); err != nil {
		t.Fatal(err)
	}
	if hist := LoadDiscoveryHistory(cfgPath); len(hist.SetRuns) != 0 || len(hist.Entries) != 0 {
		t.Fatalf("clearing the history clears the set runs too, got %+v", hist)
	}
}

func TestUnscopedPresetsReachEveryAddress(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	scoped := []ConfigPreset{{Name: "community-x", Domains: []string{"other.example"}}}
	if got := ds.presetDomains(scoped[0]); len(got) != 0 {
		t.Fatalf("precondition: a scoped preset skips addresses it was not published for, got %v", got)
	}
	open := unscopedPresets(scoped)
	if got := ds.presetDomains(open[0]); len(got) != 2 {
		t.Fatalf("a set run tests community strategies on every address, got %v", got)
	}
	if len(scoped[0].Domains) != 1 {
		t.Error("the caller's presets must not be modified")
	}
}

func TestBestPayloadVariantFollowsThePrimaryDomain(t *testing.T) {
	ds := setRunFixture(t, "a.example", "b.example")
	presets := []ConfigPreset{{Name: "combo-pastseq", Family: FamilyCombo, Phase: PhaseStrategy}}

	if _, ok := ds.bestPayloadVariant(presets); ok {
		t.Fatal("with no working payload there is nothing to re-test")
	}

	ds.workingPayloads = []PayloadTestResult{
		{Payload: config.FakePayloadSTUN, Works: true, Speed: 100},
		{Payload: config.FakePayloadDefault1, Works: true, Speed: 900},
		{Payload: config.FakePayloadDefault2, Works: false},
	}
	variant, ok := ds.bestPayloadVariant(presets)
	if !ok || variant.Name != "combo-pastseq-p1" || variant.Config.Faking.SNIType != config.FakePayloadDefault1 {
		t.Fatalf("variant = %s sni=%d ok=%v, want combo-pastseq-p1 with the google payload", variant.Name, variant.Config.Faking.SNIType, ok)
	}

	ds.customPayloads = []CustomPayload{{Name: "ya.ru", Filepath: "captures/ya.bin", Data: []byte{1}}}
	ds.workingPayloads = []PayloadTestResult{{Payload: customPayloadID(0), Works: true, Speed: 5}}
	variant, ok = ds.bestPayloadVariant(presets)
	if !ok || variant.Name != "payload-test-ya.ru" || variant.Config.Faking.SNIType != config.FakePayloadCapture || variant.Config.Faking.PayloadFile != "captures/ya.bin" {
		t.Fatalf("custom variant = %+v ok=%v", variant, ok)
	}
}

func TestCurrentStrategyNeedingADNSFixIsCovered(t *testing.T) {
	ds := setRunFixture(t, "a.example")
	ds.put("a.example", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example", presetSetCurrent, CheckStatusComplete, PhaseCached, 0)
	ds.dnsResults["a.example"] = &DNSDiscoveryResult{AlternativeIPs: []string{"203.0.113.7"}}
	ds.coverWinner = presetSetCurrent
	ds.jointConfirmed = map[string]bool{presetSetCurrent: true}
	ds.domainResults["a.example"].Results[presetSetCurrent].Confirmed = confirmTries
	ds.domainResults["a.example"].Results[presetSetCurrent].ConfirmTries = confirmTries
	withFakeConfirm(ds, nil)
	ds.determineBest()

	ds.setStrategy = &config.SetConfig{}
	ds.resolveSetVerdict()
	if v := ds.setVerdict; v == nil || v.Status != SetVerdictCovered || len(v.Set.DNS.Pins["a.example"]) == 0 {
		t.Fatalf("verdict = %+v, want covered: the set works only with the alternative address it does not pin", v)
	}

	ds.setStrategy = &config.SetConfig{DNS: config.DNSConfig{Pins: map[string][]string{"a.example": {"203.0.113.7"}}}}
	ds.resolveSetVerdict()
	if v := ds.setVerdict; v == nil || v.Status != SetVerdictCurrentWorks {
		t.Fatalf("verdict = %+v, want current_works once the set already pins the address", v)
	}
}
