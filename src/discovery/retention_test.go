package discovery

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func retainedSuite(t *testing.T, id, source string) *CheckSuite {
	t.Helper()
	suite := &CheckSuite{Id: id, Status: CheckStatusComplete, Source: source}
	RegisterSuite(suite)
	t.Cleanup(func() {
		suitesMu.Lock()
		if timer := retainTimers[id]; timer != nil {
			timer.Stop()
		}
		delete(retainTimers, id)
		delete(activeSuites, id)
		suitesMu.Unlock()
	})
	return suite
}

func retainedTimer(id string) *time.Timer {
	suitesMu.RLock()
	defer suitesMu.RUnlock()
	return retainTimers[id]
}

func TestAFinishedRunIsKeptUntilItsRetentionEnds(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 200 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })

	suite := retainedSuite(t, "retention-ttl", SourceWeb)
	retainFinished(suite)

	if _, ok := GetCheckSuite(suite.Id); !ok {
		t.Fatal("a run that just finished must stay readable")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := GetCheckSuite(suite.Id); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run is never released once its retention ends")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if retainedTimer(suite.Id) != nil {
		t.Error("a released run leaves no timer behind")
	}
}

func TestTheNextRunFromTheSameSourceReplacesTheRetainedOne(t *testing.T) {
	previous := suiteRetention
	suiteRetention = time.Hour
	t.Cleanup(func() { suiteRetention = previous })

	web := retainedSuite(t, "retention-web-1", SourceWeb)
	watchdog := retainedSuite(t, "retention-watchdog-1", SourceWatchdog)
	retainFinished(web)
	retainFinished(watchdog)
	evictedTimer := retainedTimer(web.Id)

	running := &CheckSuite{Id: "retention-web-running", Status: CheckStatusRunning, Source: SourceWeb}
	RegisterSuite(running)
	t.Cleanup(func() { releaseSuite(running.Id) })
	if _, ok := GetCheckSuite(web.Id); !ok {
		t.Fatal("registering a new run evicts nothing, or the watchdog loses the run it is about to read")
	}

	next := retainedSuite(t, "retention-web-2", SourceWeb)
	retainFinished(next)

	if _, ok := GetCheckSuite(web.Id); ok {
		t.Error("the older finished web run is replaced by the newer one")
	}
	if retainedTimer(web.Id) != nil || evictedTimer.Stop() {
		t.Error("an evicted run leaves no live timer that would keep it in memory")
	}
	for _, id := range []string{watchdog.Id, next.Id, running.Id} {
		if _, ok := GetCheckSuite(id); !ok {
			t.Errorf("%s must stay: only a finished run from the same source is replaced", id)
		}
	}
}

func TestAWebRunInsideTheWatchdogsIdleWaitLeavesItsRunReadable(t *testing.T) {
	previous := suiteRetention
	suiteRetention = time.Hour
	t.Cleanup(func() { suiteRetention = previous })

	watchdog := retainedSuite(t, "retention-watchdog-window", SourceWatchdog)
	watchdog.SetId = "set-a"
	watchdog.SetVerdict = &SetVerdict{Status: SetVerdictCovered, WinnerPreset: "p", Confirmed: true}
	retainFinished(watchdog)

	web := retainedSuite(t, "retention-web-window", SourceWeb)
	retainFinished(web)

	snap, ok := SnapshotCheckSuite(watchdog.Id)
	if !ok || snap.SetVerdict == nil || snap.SetVerdict.Status != SetVerdictCovered {
		t.Fatalf("a web run that starts and ends while the watchdog waits must not take the watchdog's verdict: %+v %v", snap, ok)
	}
}

func TestFinishedRunsDropTheExpandedMatchLists(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })

	shared := config.NewSetConfig()
	shared.Name = "shared"
	shared.Targets.GeoSiteCategories = []string{"youtube"}
	shared.Targets.DomainsToMatch = []string{"youtube.com", "googlevideo.com", "ytimg.com"}
	shared.Targets.IpsToMatch = []string{"142.250.0.0/15"}
	plain := config.NewSetConfig()
	plain.Name = "plain"

	ds := settleSuite(t, "www.youtube.com", "i.ytimg.com")
	for _, d := range []string{"www.youtube.com", "i.ytimg.com"} {
		ds.domainResults[d].BestPreset = "shared"
		ds.domainResults[d].BestSuccess = true
		ds.domainResults[d].Results["shared"] = &DomainPresetResult{PresetName: "shared", Status: CheckStatusComplete, Speed: 1000, Set: &shared}
		ds.domainResults[d].Results["plain"] = &DomainPresetResult{PresetName: "plain", Status: CheckStatusComplete, Speed: 500, Set: &plain}
	}

	ds.finalize()

	a := ds.domainResults["www.youtube.com"].Results["shared"].Set
	b := ds.domainResults["i.ytimg.com"].Results["shared"].Set
	if a == &shared || a != b {
		t.Fatalf("a set shared by results is replaced by one copy for all of them: %p %p (original %p)", a, b, &shared)
	}
	if a.Targets.DomainsToMatch != nil || a.Targets.IpsToMatch != nil {
		t.Errorf("the expanded lists only the search needed are released: %+v", a.Targets)
	}
	if len(a.Targets.GeoSiteCategories) != 1 || a.Name != "shared" {
		t.Errorf("everything the set says is kept: %+v", a)
	}
	if len(shared.Targets.DomainsToMatch) != 3 || len(shared.Targets.IpsToMatch) != 1 {
		t.Error("the original set may still be the pool's current config and is never changed")
	}
	if ds.domainResults["www.youtube.com"].Results["plain"].Set != &plain {
		t.Error("a set without match lists has nothing to release and stays as it is")
	}
	if len(ds.StrategyGroups) != 1 || ds.StrategyGroups[0].Set == nil || len(ds.StrategyGroups[0].Set.Targets.DomainsToMatch) == 0 {
		t.Errorf("the group set keeps its own targets: %+v", ds.StrategyGroups)
	}
}
