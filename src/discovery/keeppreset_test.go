package discovery

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheRunsPresetFilterAppliesToCachedAndCommunityStrategies(t *testing.T) {
	const community = "community-brought-v1"
	for _, keep := range []bool{false, true} {
		name := "dropped"
		if keep {
			name = "kept"
		}
		t.Run(name, func(t *testing.T) {
			env := newCacheRun(t, func([]byte) bool { return false })
			grouped := testedShape(t, env.cfg.ConfigPath, "combo-pastseq")
			grouped.Hub = &config.HubOrigin{ID: "brought", Version: 1}
			storedCache(t, env.cfg.ConfigPath, CacheEntry{Config: grouped, Family: FamilyCombo, SuccessCount: 3, Speed: 1000})

			ds := env.suite()
			ds.hubPresetsFn = func() []ConfigPreset {
				return []ConfigPreset{{Name: community, Family: FamilyCommunity, Phase: PhaseCached, Config: grouped}}
			}
			ds.keepPreset = func(p ConfigPreset) bool {
				return keep || p.Config.Hub == nil || p.Config.Hub.ID != "brought"
			}
			env.running.Store(ds)
			RegisterSuite(ds.CheckSuite)
			ds.RunDiscovery()

			for _, preset := range []string{"cached-1-combo", community} {
				sent := len(env.fakes.of(preset))
				result := ds.DomainDiscoveryResults[cacheRunDomain].Results[preset]
				if keep && (sent == 0 || result == nil) {
					t.Errorf("%s passes the run's filter, so it must be tested: %d fetches, result %+v", preset, sent, result)
				}
				if !keep && (sent != 0 || result != nil) {
					t.Errorf("%s fails the run's filter, so it must not be tested: %d fetches, result %+v", preset, sent, result)
				}
			}
		})
	}
}
