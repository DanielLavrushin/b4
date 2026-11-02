package discovery

import (
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func recordSettles(ds *DiscoverySuite, servesSystem map[string]bool) *[]string {
	var tried []string
	ds.dnsSettleFn = func(name string, _ *config.SetConfig, inputs []DomainInput, _ map[string][]string) (map[string]bool, bool) {
		tried = append(tried, name)
		out := map[string]bool{}
		for _, di := range inputs {
			out[di.Domain] = servesSystem[name]
		}
		return out, true
	}
	return &tried
}

func finishSetRun(t *testing.T, ds *DiscoverySuite) *SetVerdict {
	t.Helper()
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	RegisterSuite(ds.CheckSuite)
	ds.finishRun()
	t.Cleanup(func() { ds.ctxCancel() })
	return ds.SetVerdict
}

func markConfirmed(ds *DiscoverySuite, domain, preset string) {
	r := ds.domainResults[domain].Results[preset]
	r.Confirmed, r.ConfirmTries = confirmTries, confirmTries
}

func wantSettledThroughWinner(t *testing.T, v *SetVerdict, tried []string) {
	t.Helper()
	if v == nil || v.Set == nil {
		t.Fatalf("no verdict set: %+v", v)
	}
	if len(tried) == 0 || tried[len(tried)-1] != v.WinnerPreset {
		t.Errorf("the DNS check must try the verdict's winner %q on the system addresses, tried %v", v.WinnerPreset, tried)
	}
	if !v.Set.DNS.Enabled {
		t.Errorf("the verdict's winner %q does not load the system addresses, so its set must keep the DNS fix", v.WinnerPreset)
	}
}

func TestAFullSearchSetRunChecksDNSThroughTheWinnerNotTheCurrentStrategy(t *testing.T) {
	ds := setRunFixture(t, settleDomain)
	ds.stopWhenCovered = false
	ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, presetSetCurrent, 900)
	ds.found(settleDomain, "cached-2-other", 1000)
	markConfirmed(ds, settleDomain, "cached-2-other")
	ds.setStrategy = &config.SetConfig{}
	withFakeConfirm(ds, map[string]map[string]int{presetSetCurrent: {settleDomain: 1}})
	tried := recordSettles(ds, map[string]bool{presetSetCurrent: true, "cached-2-other": false})

	wantSettledThroughWinner(t, finishSetRun(t, ds), *tried)
}

func TestASetRunChecksDNSThroughTheWinnerWhenTheGroupWinnerFailsJointly(t *testing.T) {
	ds := setRunFixture(t, settleDomain)
	ds.stopWhenCovered = false
	ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.found(settleDomain, "cached-2-other", 900)
	markConfirmed(ds, settleDomain, "cached-1-tcpmd5")
	ds.setStrategy = &config.SetConfig{}
	withFakeConfirm(ds, map[string]map[string]int{"cached-1-tcpmd5": {settleDomain: 2}})
	tried := recordSettles(ds, map[string]bool{"cached-1-tcpmd5": true, "cached-2-other": false})

	wantSettledThroughWinner(t, finishSetRun(t, ds), *tried)
}

func TestASetRunChecksDNSThroughTheWinnerWhenTheGroupWinnerFailsASiteThatLoadsWithoutB4(t *testing.T) {
	const fine = "fine.example"
	ds := setRunFixture(t, settleDomain, fine)
	ds.stopWhenCovered = false
	ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put(fine, presetNoBypass, CheckStatusComplete, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.put(fine, "cached-1-tcpmd5", CheckStatusFailed, PhaseCached, 0)
	ds.found(settleDomain, "cached-2-other", 900)
	ds.put(fine, "cached-2-other", CheckStatusComplete, PhaseCached, 0)
	markConfirmed(ds, settleDomain, "cached-1-tcpmd5")
	ds.setStrategy = &config.SetConfig{}
	withFakeConfirm(ds, nil)
	tried := recordSettles(ds, map[string]bool{"cached-1-tcpmd5": true, "cached-2-other": false})

	wantSettledThroughWinner(t, finishSetRun(t, ds), *tried)
}

func TestASetRunWinnerThatLoadsTheSystemAddressesDropsTheDNSFix(t *testing.T) {
	ds := setRunFixture(t, settleDomain)
	ds.stopWhenCovered = false
	r := ds.candidate(settleDomain, nil)
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	markConfirmed(ds, settleDomain, "cached-1-tcpmd5")
	ds.setStrategy = &config.SetConfig{}
	withFakeConfirm(ds, nil)
	recordSettles(ds, map[string]bool{"cached-1-tcpmd5": true})

	v := finishSetRun(t, ds)
	if v.WinnerPreset != "cached-1-tcpmd5" || v.Set.DNS.Enabled || r.IsPoisoned {
		t.Errorf("a winner that loads the system addresses must leave the set without the DNS fix: winner %s, dns %v, poisoned %v", v.WinnerPreset, v.Set.DNS.Enabled, r.IsPoisoned)
	}
}

func TestAPerAddressCardKeepsTheDNSFixWhenTheCheckTriedAnotherStrategy(t *testing.T) {
	ds := setRunFixture(t, settleDomain)
	ds.stopWhenCovered = false
	ds.candidate(settleDomain, func(r *DNSDiscoveryResult) {
		r.AlternativeIPs = append([]string(nil), settleReference...)
		r.referencePinned = true
	})
	ds.put(settleDomain, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.found(settleDomain, "cached-1-tcpmd5", 1000)
	ds.found(settleDomain, presetSetCurrent, 1000)
	ds.determineBest()
	ds.setStrategy = &config.SetConfig{}
	withFakeConfirm(ds, nil)
	tried := recordSettles(ds, map[string]bool{presetSetCurrent: true})

	ds.resolveSetVerdict()

	v := ds.setVerdict
	if v == nil || v.WinnerPreset != presetSetCurrent || v.Set.DNS.Enabled || len(v.Set.DNS.Pins) != 0 {
		t.Fatalf("the checked strategy loads the system addresses, so the verdict's set drops the DNS fix: %+v", v)
	}
	if len(*tried) != 1 || (*tried)[0] != presetSetCurrent {
		t.Fatalf("only the verdict's strategy is tried on the system addresses, tried %v", *tried)
	}
	if len(ds.StrategyGroups) != 1 || ds.StrategyGroups[0].WinnerPreset == presetSetCurrent {
		t.Fatalf("the per-address card must offer another strategy than the verdict: %+v", ds.StrategyGroups)
	}
	set := ds.StrategyGroups[0].Set
	if !set.DNS.Enabled || set.DNS.DoHURL != settleDoH || !slices.Equal(set.DNS.Pins[settleDomain], settleReference) {
		t.Errorf("'%s' was never tried on the system addresses, so its card keeps the DNS fix it was tested with: %+v", ds.StrategyGroups[0].WinnerPreset, set.DNS)
	}
	if !set.TCP.IPBlockDetect.Enabled {
		t.Error("a pinned set watches for dead addresses")
	}
}
