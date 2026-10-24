package watchdog

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"youtube.com", "youtube.com"},
		{"https://youtube.com", "youtube.com"},
		{"https://youtube.com/watch?v=123", "youtube.com"},
		{"http://example.com:8080/path", "example.com"},
		{"example.com/path", "example.com"},
		{"example.com:443", "example.com"},
		{"example.com?query=1", "example.com"},
		{"https://www.roblox.com/", "www.roblox.com"},
		{"  https://discord.com  ", "discord.com"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ExtractDomain(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractDomain(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSyncDomainStates(t *testing.T) {
	w := &Watchdog{
		domainStates: map[string]*DomainStatus{
			"old.com":  {Domain: "old.com", Status: StatusHealthy},
			"keep.com": {Domain: "keep.com", Status: StatusDegraded, ConsecutiveFailures: 2},
		},
	}

	wdCfg := config.WatchdogConfig{
		Domains:     []string{"keep.com", "new.com"},
		IntervalSec: 300,
	}

	w.syncDomainStates(wdCfg)

	if _, ok := w.domainStates["old.com"]; ok {
		t.Error("old.com should have been removed")
	}

	if st := w.domainStates["keep.com"]; st == nil {
		t.Fatal("keep.com should still exist")
	} else if st.ConsecutiveFailures != 2 {
		t.Error("keep.com state should be preserved")
	}

	if st := w.domainStates["new.com"]; st == nil {
		t.Fatal("new.com should have been created")
	} else if st.Status != StatusQueued {
		t.Errorf("new.com should be queued, got %s", st.Status)
	} else if st.Interval != 300 {
		t.Errorf("new.com interval should be 300, got %d", st.Interval)
	}
}

func TestGroupBySetKeepsDifferentWinnersApart(t *testing.T) {
	setA := &config.SetConfig{}
	setA.Fragmentation.Strategy = "combo"
	setA.Faking.Strategy = "ttl"
	setA.Faking.TTL = 3

	setB := &config.SetConfig{}
	setB.Fragmentation.Strategy = "combo"
	setB.Faking.Strategy = "ttl"
	setB.Faking.TTL = 3
	setB.Fragmentation.SNIPosition = 7

	groups := groupBySet([]domainWithSet{
		{domain: "youtube.com", set: setA},
		{domain: "meduza.io", set: setB},
		{domain: "googlevideo.com", set: setA},
	})

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups[0]) != 2 || groups[0][1].domain != "googlevideo.com" {
		t.Errorf("domains that share one winner set must share a group, got %+v", groups[0])
	}
	if len(groups[1]) != 1 || groups[1][0].domain != "meduza.io" {
		t.Errorf("a different winner must stay in its own group, got %+v", groups[1])
	}
}

func healSuite(results map[string]*discovery.DomainDiscoveryResult, groups ...discovery.StrategyGroup) *discovery.CheckSuite {
	return &discovery.CheckSuite{DomainDiscoveryResults: results, StrategyGroups: groups}
}

func TestApplyBatchResultsUsesConfirmedGroupWinner(t *testing.T) {
	existing := config.NewSetConfig()
	existing.Name = "YouTube"
	existing.Enabled = true
	existing.Targets.SNIDomains = []string{"youtube.com"}
	existing.Targets.DomainsToMatch = []string{"youtube.com"}
	existing.TCP.DPortFilter = "443,2053"
	existing.TCP.RSTProtection.Enabled = true
	existing.UDP.Mode = "drop"
	cfg := &config.Config{Sets: []*config.SetConfig{&existing}}

	fastest := config.NewSetConfig()
	fastest.Fragmentation.Strategy = "disorder"
	group := config.NewSetConfig()
	group.Fragmentation.Strategy = "combo"
	group.UDP.Mode = "fake"

	suite := healSuite(map[string]*discovery.DomainDiscoveryResult{
		"youtube.com": {
			Domain:      "youtube.com",
			BestPreset:  "fast",
			BestSuccess: true,
			Results: map[string]*discovery.DomainPresetResult{
				"fast":    {Status: discovery.CheckStatusComplete, Set: &fastest},
				"grouped": {Status: discovery.CheckStatusComplete, Set: &group, Confirmed: 3, ConfirmTries: 3},
			},
		},
		"meduza.io": {
			Domain:      "meduza.io",
			BestPreset:  "flaky",
			BestSuccess: true,
			Unconfirmed: true,
			Results:     map[string]*discovery.DomainPresetResult{"flaky": {Status: discovery.CheckStatusComplete, Set: &fastest}},
		},
		"example.com": {Domain: "example.com", BestSuccess: true, BaselineWorks: true},
	}, discovery.StrategyGroup{WinnerPreset: "grouped", Domains: []string{"youtube.com"}, Set: &group})

	saved := 0
	errs := applyBatchResults(cfg, []string{"youtube.com", "meduza.io", "example.com"}, suite, func(*config.Config) error {
		saved++
		return nil
	})

	if saved != 1 {
		t.Fatalf("expected one save, got %d", saved)
	}
	if err := errs["youtube.com"]; err != nil {
		t.Errorf("youtube.com: unexpected error %v", err)
	}
	if !errors.Is(errs["meduza.io"], errUnconfirmed) {
		t.Errorf("meduza.io: an unconfirmed winner must not be applied, got %v", errs["meduza.io"])
	}
	if !errors.Is(errs["example.com"], ErrBaselineWorks) {
		t.Errorf("example.com: got %v, want ErrBaselineWorks", errs["example.com"])
	}
	got := cfg.Sets[0]
	if len(cfg.Sets) != 1 {
		t.Fatalf("the existing set must be healed in place, got %d sets", len(cfg.Sets))
	}
	if got.Fragmentation.Strategy != "combo" {
		t.Errorf("the group winner must be applied, got %q", got.Fragmentation.Strategy)
	}
	if got.TCP.DPortFilter != "443,2053" || !got.TCP.RSTProtection.Enabled {
		t.Errorf("the set's port filter and RST protection must be kept, got %q / %v", got.TCP.DPortFilter, got.TCP.RSTProtection.Enabled)
	}
	if got.UDP.Mode != "drop" {
		t.Errorf("UDP is never probed and must be kept, got %q", got.UDP.Mode)
	}
}

func TestApplyBatchResultsDefersOnlyTheDomainsWhoseSetMovedToAnotherDSCPPath(t *testing.T) {
	stubDSCPStampNow(t)
	moved := config.NewSetConfig()
	moved.Id, moved.Name, moved.Enabled = "yt", "YouTube", true
	moved.Targets.SNIDomains = []string{"youtube.com"}
	moved.Targets.DomainsToMatch = []string{"youtube.com"}
	moved.Fragmentation.Strategy = "tcp"
	moved.DSCP = config.SetDSCPConfig{Enabled: true, Value: 7}
	cfg := &config.Config{Sets: []*config.SetConfig{&moved}}

	tested := func() *config.SetConfig {
		set := config.NewSetConfig()
		set.Fragmentation.Strategy = "disorder"
		set.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
		return &set
	}
	found := func(domain string) *discovery.DomainDiscoveryResult {
		return &discovery.DomainDiscoveryResult{
			Domain: domain, BestPreset: "p", BestSuccess: true,
			Results: map[string]*discovery.DomainPresetResult{"p": {Status: discovery.CheckStatusComplete, Set: tested()}},
		}
	}
	suite := healSuite(map[string]*discovery.DomainDiscoveryResult{"youtube.com": found("youtube.com"), "meduza.io": found("meduza.io")})

	saved := 0
	errs := applyBatchResults(cfg, []string{"youtube.com", "meduza.io"}, suite, func(*config.Config) error {
		saved++
		return nil
	})

	var change *discovery.DSCPPathChange
	if !errors.As(errs["youtube.com"], &change) || change.Tested == nil || *change.Tested != 25 || change.Now == nil || *change.Now != 7 {
		t.Errorf("youtube.com was tested on DSCP 25 and its set now carries 7, so its result must wait for a run on that path, got %v", errs["youtube.com"])
	}
	if got := cfg.GetSetById("yt").Fragmentation.Strategy; got != "tcp" {
		t.Errorf("nothing may be adopted into the set that moved, its strategy became %q", got)
	}
	if err := errs["meduza.io"]; err != nil || saved != 1 || len(cfg.Sets) != 2 || cfg.Sets[0].DSCP.Value != 25 {
		t.Errorf("meduza.io gets a new set on the path it was tested on and is saved, got err %v, %d saves, %d sets", err, saved, len(cfg.Sets))
	}
}

func TestApplyBatchResultsDefersOnlyTheGroupMembersWhoseOwnSetMoved(t *testing.T) {
	stubDSCPStampNow(t)
	for _, movedFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("moved set first %t", movedFirst), func(t *testing.T) {
			owner := func(id, domain string, dscp int) *config.SetConfig {
				set := config.NewSetConfig()
				set.Id, set.Name, set.Enabled = id, id, true
				set.Targets.SNIDomains = []string{domain}
				set.Targets.DomainsToMatch = []string{domain}
				set.Fragmentation.Strategy = "tcp"
				set.DSCP = config.SetDSCPConfig{Enabled: true, Value: dscp}
				return &set
			}
			moved, kept := owner("md", "meduza.io", 7), owner("yt", "youtube.com", 25)
			cfg := &config.Config{Sets: []*config.SetConfig{kept, moved}}
			if movedFirst {
				cfg.Sets = []*config.SetConfig{moved, kept}
			}

			winner := config.NewSetConfig()
			winner.Fragmentation.Strategy = "disorder"
			winner.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
			found := func(domain string) *discovery.DomainDiscoveryResult {
				return &discovery.DomainDiscoveryResult{
					Domain: domain, BestPreset: "p", BestSuccess: true,
					Results: map[string]*discovery.DomainPresetResult{"p": {Status: discovery.CheckStatusComplete, Set: &winner, Confirmed: 3, ConfirmTries: 3}},
				}
			}
			suite := healSuite(map[string]*discovery.DomainDiscoveryResult{"youtube.com": found("youtube.com"), "meduza.io": found("meduza.io")},
				discovery.StrategyGroup{WinnerPreset: "p", Domains: []string{"youtube.com", "meduza.io"}, Set: &winner})

			errs := applyBatchResults(cfg, []string{"youtube.com", "meduza.io"}, suite, func(*config.Config) error { return nil })

			var change *discovery.DSCPPathChange
			if !errors.As(errs["meduza.io"], &change) || cfg.GetSetById("md").Fragmentation.Strategy != "tcp" {
				t.Errorf("meduza.io's own set moved to DSCP 7, so its result waits for a run on that path: %v", errs["meduza.io"])
			}
			if err := errs["youtube.com"]; err != nil || cfg.GetSetById("yt").Fragmentation.Strategy != "disorder" {
				t.Errorf("youtube.com's set still carries 25, so it is healed whatever the set order: %v", err)
			}
			if domainInSNIList(cfg.GetSetById("yt").Targets.SNIDomains, "meduza.io") {
				t.Error("meduza.io may not be moved into the set on DSCP 25, away from the path its own set takes now")
			}
		})
	}
}

func TestApplyBatchResultsSkipsUnconfirmedGroupWinner(t *testing.T) {
	existing := config.NewSetConfig()
	existing.Id = "yt"
	existing.Name = "YouTube"
	existing.Targets.SNIDomains = []string{"youtube.com"}
	existing.Targets.DomainsToMatch = []string{"youtube.com"}
	cfg := &config.Config{Sets: []*config.SetConfig{&existing}}

	best := config.NewSetConfig()
	best.Fragmentation.Strategy = "disorder"
	grouped := config.NewSetConfig()
	grouped.Fragmentation.Strategy = "combo"

	suite := healSuite(map[string]*discovery.DomainDiscoveryResult{
		"youtube.com": {
			Domain:       "youtube.com",
			BestPreset:   "best",
			BestSuccess:  true,
			Confirmed:    3,
			ConfirmTries: 3,
			Results: map[string]*discovery.DomainPresetResult{
				"best":    {Status: discovery.CheckStatusComplete, Set: &best, Confirmed: 3, ConfirmTries: 3},
				"grouped": {Status: discovery.CheckStatusComplete, Set: &grouped},
			},
		},
		"meduza.io": {
			Domain:      "meduza.io",
			BestPreset:  "flaky",
			BestSuccess: true,
			Unconfirmed: true,
			Results: map[string]*discovery.DomainPresetResult{
				"flaky":   {Status: discovery.CheckStatusComplete, Set: &best},
				"grouped": {Status: discovery.CheckStatusComplete, Set: &grouped},
			},
		},
	}, discovery.StrategyGroup{WinnerPreset: "grouped", Domains: []string{"youtube.com", "meduza.io"}, Set: &grouped})

	before := cfg.Sets[0].Fragmentation.Strategy
	errs := applyBatchResults(cfg, []string{"youtube.com", "meduza.io"}, suite, func(*config.Config) error { return nil })
	if !errors.Is(errs["youtube.com"], errUnconfirmed) {
		t.Errorf("youtube.com: its group's winner is unconfirmed, and falling back to its own preset would split the group on one set, got %v", errs["youtube.com"])
	}
	if !errors.Is(errs["meduza.io"], errUnconfirmed) {
		t.Errorf("meduza.io: the group winner did not pass confirmation, got %v", errs["meduza.io"])
	}
	if got := cfg.Sets[0].Fragmentation.Strategy; got != before {
		t.Errorf("nothing unconfirmed is written, got %q", got)
	}
}

func TestApplyBatchResultsNamesAnUnresolvedDomain(t *testing.T) {
	cfg := &config.Config{}
	suite := healSuite(map[string]*discovery.DomainDiscoveryResult{
		"typo.example": {
			Domain:     "typo.example",
			Unresolved: true,
			Outcome:    discovery.OutcomeUnresolved,
			Results:    map[string]*discovery.DomainPresetResult{},
		},
	})

	saved := false
	errs := applyBatchResults(cfg, []string{"typo.example"}, suite, func(*config.Config) error {
		saved = true
		return nil
	})
	if !errors.Is(errs["typo.example"], errUnresolved) {
		t.Fatalf("a domain that does not resolve must be reported as such, got %v", errs["typo.example"])
	}
	if saved || len(cfg.Sets) != 0 {
		t.Fatal("nothing is written for a domain that does not resolve")
	}
}

func TestDiscoveryInputsKeepHostKeys(t *testing.T) {
	in := []string{"youtube.com", "example.com/path", "example.org:8443", "https://meduza.io/x"}
	got := discoveryInputs(in)
	want := []string{"youtube.com", "https://example.com/path", "https://example.org:8443", "https://meduza.io/x"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("discoveryInputs(%q) = %q, want %q", in[i], got[i], want[i])
		}
	}
}

func TestEveryDomainSettled(t *testing.T) {
	suite := healSuite(map[string]*discovery.DomainDiscoveryResult{
		"a.com": {BestSuccess: true},
		"b.com": {Results: map[string]*discovery.DomainPresetResult{"no-bypass": {Status: discovery.CheckStatusComplete}}},
		"c.com": {Results: map[string]*discovery.DomainPresetResult{"no-bypass": {Status: discovery.CheckStatusFailed}}},
	})
	if everyDomainSettled(suite, []string{"a.com", "b.com", "c.com"}) {
		t.Error("c.com has no result yet, the search must go on")
	}
	if !everyDomainSettled(suite, []string{"a.com", "https://b.com/"}) {
		t.Error("a.com found a strategy and b.com loads without one, both are settled")
	}
	if everyDomainSettled(suite, []string{"missing.com"}) {
		t.Error("a domain without results is not settled")
	}

	suite.DomainDiscoveryResults["typo.example"] = &discovery.DomainDiscoveryResult{
		Outcome: discovery.OutcomeUnresolved,
		Results: map[string]*discovery.DomainPresetResult{"no-bypass": {Status: discovery.CheckStatusFailed}},
	}
	if !everyDomainSettled(suite, []string{"a.com", "typo.example"}) {
		t.Error("a domain that does not resolve will never get a result, waiting for it only prolongs the search")
	}
}

func TestSetContainsAnyDomain(t *testing.T) {
	set := &config.SetConfig{}
	set.Targets.SNIDomains = []string{"youtube.com", "discord.com"}

	t.Run("exact match", func(t *testing.T) {
		if !setContainsAnyDomain(set, []string{"youtube.com"}) {
			t.Error("should match exact domain")
		}
	})

	t.Run("no match", func(t *testing.T) {
		if setContainsAnyDomain(set, []string{"twitter.com"}) {
			t.Error("should not match unrelated domain")
		}
	})

	t.Run("subdomain match", func(t *testing.T) {
		if !setContainsAnyDomain(set, []string{"www.youtube.com"}) {
			t.Error("should match subdomain")
		}
	})

	t.Run("reverse subdomain match", func(t *testing.T) {
		setWww := &config.SetConfig{}
		setWww.Targets.SNIDomains = []string{"www.discord.com"}
		if !setContainsAnyDomain(setWww, []string{"discord.com"}) {
			t.Error("should match parent domain")
		}
	})

	t.Run("partial name no match", func(t *testing.T) {
		if setContainsAnyDomain(set, []string{"cord.com"}) {
			t.Error("cord.com should not match discord.com")
		}
	})

	t.Run("uses DomainsToMatch when available", func(t *testing.T) {
		setGeo := &config.SetConfig{}
		setGeo.Targets.SNIDomains = []string{"youtube.com"}
		setGeo.Targets.DomainsToMatch = []string{"youtube.com", "googlevideo.com", "ytimg.com"}
		if !setContainsAnyDomain(setGeo, []string{"googlevideo.com"}) {
			t.Error("should match via DomainsToMatch")
		}
	})

	t.Run("case-insensitive query", func(t *testing.T) {
		if !setContainsAnyDomain(set, []string{"YouTube.com"}) {
			t.Error("should match regardless of case")
		}
	})

	t.Run("whitespace trimmed query", func(t *testing.T) {
		if !setContainsAnyDomain(set, []string{"  youtube.com  "}) {
			t.Error("should match after trimming whitespace")
		}
	})

	t.Run("case-insensitive stored domain", func(t *testing.T) {
		mixed := &config.SetConfig{}
		mixed.Targets.SNIDomains = []string{"YouTube.COM"}
		if !setContainsAnyDomain(mixed, []string{"youtube.com"}) {
			t.Error("should match a mixed-case stored domain")
		}
	})
}

func TestDomainMatchesSuffix(t *testing.T) {
	tests := []struct {
		domain, target string
		expected       bool
	}{
		{"www.youtube.com", "youtube.com", true},
		{"youtube.com", "www.youtube.com", true},
		{"youtube.com", "youtube.com", false},
		{"cord.com", "discord.com", false},
		{"evil-youtube.com", "youtube.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.domain+"_"+tt.target, func(t *testing.T) {
			if domainMatchesSuffix(tt.domain, tt.target) != tt.expected {
				t.Errorf("domainMatchesSuffix(%q, %q) = %v, want %v",
					tt.domain, tt.target, !tt.expected, tt.expected)
			}
		})
	}
}

func TestApplyGroup_NewSet(t *testing.T) {
	cfg := &config.Config{}

	refSet := &config.SetConfig{}
	refSet.Fragmentation.Strategy = "combo"
	refSet.Faking.Strategy = "ttl"
	refSet.Faking.TTL = 3

	group := []domainWithSet{
		{domain: "youtube.com", set: refSet},
		{domain: "googlevideo.com", set: refSet},
	}

	if err := applyGroup(cfg, group, nil); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sets) != 1 {
		t.Fatalf("expected 1 set, got %d", len(cfg.Sets))
	}

	newSet := cfg.Sets[0]
	if newSet.Name != "watchdog-youtube.com" {
		t.Errorf("name = %q, want %q", newSet.Name, "watchdog-youtube.com")
	}
	if len(newSet.Targets.SNIDomains) != 2 {
		t.Errorf("should have 2 SNI domains, got %d", len(newSet.Targets.SNIDomains))
	}
	if newSet.Fragmentation.Strategy != "combo" {
		t.Errorf("strategy = %q, want %q", newSet.Fragmentation.Strategy, "combo")
	}
	if !newSet.Enabled {
		t.Error("new set should be enabled")
	}
}

func TestApplyGroup_NewSetKeepsTheDSCPTheRunTestedWith(t *testing.T) {
	stubDSCPStampNow(t)
	owner := config.NewSetConfig()
	owner.Name = "video by category"
	owner.Enabled = true
	owner.Targets.GeoSiteCategories = []string{"youtube"}
	owner.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	cfg := &config.Config{Sets: []*config.SetConfig{&owner}}

	refSet := &config.SetConfig{}
	refSet.Fragmentation.Strategy = "combo"
	refSet.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
	refSet.Targets.IPVersion = "4"

	if err := applyGroup(cfg, []domainWithSet{{domain: "youtube.com", set: refSet}}, nil); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sets) != 2 {
		t.Fatalf("the owner lists the domain through a category only, so the heal creates a set, got %d sets", len(cfg.Sets))
	}
	if got := cfg.Sets[0].DSCP; got != refSet.DSCP {
		t.Errorf("the strategy was tested on the path DSCP 25 selects, and the new set takes the domain from its owner, so it must keep that value, got %+v", got)
	}
	if got := cfg.Sets[0].Targets.IPVersion; got != "4" {
		t.Errorf("the strategy was tested over IPv4 only, so the new set must not take the domain's IPv6 traffic too, got %q", got)
	}

	dscpStampNow = stampsNothing
	var change *discovery.DSCPPathChange
	if err := applyGroup(cfg, []domainWithSet{{domain: "meduza.io", set: refSet}}, nil); !errors.As(err, &change) || len(cfg.Sets) != 2 {
		t.Errorf("the firewall no longer writes DSCP 25, so a new set would leave without the value the run's probes carried, and none may be created: %v, %d sets", err, len(cfg.Sets))
	}
}

func TestLegacyHealOfASingleFamilyDSCPOwnerSearchesThatFamilyOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		dscp *config.DSCPProbe
		want string
	}{
		{"IPv4 only", &config.DSCPProbe{Value: 25, IPv4: true}, "ipv4"},
		{"IPv6 only", &config.DSCPProbe{Value: 25, IPv6: true}, "ipv6"},
		{"both families", &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true}, ""},
		{"no DSCP", nil, ""},
	} {
		h := legacyHarness(t)
		h.w.healBatch([]string{"youtube.com"}, tc.dscp)
		if len(h.driver.starts) != 1 || h.driver.starts[0].IPVersion != tc.want {
			t.Errorf("%s: the owner's traffic carries its DSCP value in these families only, so the search must test the same ones, got %+v", tc.name, h.driver.starts)
		}
	}
}

func TestApplyGroup_ExistingSet(t *testing.T) {
	existingSet := config.NewSetConfig()
	existingSet.Name = "MyYouTube"
	existingSet.Enabled = true
	existingSet.Targets.SNIDomains = []string{"youtube.com"}
	existingSet.Targets.DomainsToMatch = []string{"youtube.com"}
	existingSet.Fragmentation.Strategy = "tcp"

	cfg := &config.Config{
		Sets: []*config.SetConfig{&existingSet},
	}

	refSet := &config.SetConfig{}
	refSet.Fragmentation.Strategy = "combo"
	refSet.Faking.Strategy = "ttl"
	refSet.Faking.TTL = 3

	group := []domainWithSet{
		{domain: "youtube.com", set: refSet},
		{domain: "googlevideo.com", set: refSet},
	}

	if err := applyGroup(cfg, group, nil); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sets) != 1 {
		t.Fatalf("should reuse existing set, got %d sets", len(cfg.Sets))
	}
	if cfg.Sets[0].Fragmentation.Strategy != "combo" {
		t.Errorf("strategy should be updated to combo, got %s", cfg.Sets[0].Fragmentation.Strategy)
	}
	if len(cfg.Sets[0].Targets.SNIDomains) != 2 {
		t.Errorf("should have 2 SNI domains, got %d", len(cfg.Sets[0].Targets.SNIDomains))
	}
}

func TestApplyGroup_SkipsRoutingSetMatchedViaGeosite(t *testing.T) {
	adblock := config.NewSetConfig()
	adblock.Name = "adblock"
	adblock.Enabled = true
	adblock.Routing.Enabled = true
	adblock.Routing.Mode = config.RoutingModeBlock
	adblock.Targets.SNIDomains = []string{"ad.doubleclick.net"}
	adblock.Targets.DomainsToMatch = []string{"ad.doubleclick.net", "ads.youtube.com", "s2.youtube.com"}

	youtube := config.NewSetConfig()
	youtube.Name = "YouTubenew"
	youtube.Enabled = true
	youtube.Targets.SNIDomains = []string{"youtube.com"}
	youtube.Targets.DomainsToMatch = []string{"youtube.com"}
	youtube.Fragmentation.Strategy = "tcp"

	cfg := &config.Config{
		Sets: []*config.SetConfig{&adblock, &youtube},
	}

	refSet := &config.SetConfig{}
	refSet.Fragmentation.Strategy = "combo"

	group := []domainWithSet{
		{domain: "youtube.com", set: refSet},
	}

	if err := applyGroup(cfg, group, nil); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sets) != 2 {
		t.Fatalf("should reuse YouTube set, got %d sets", len(cfg.Sets))
	}
	for _, sni := range adblock.Targets.SNIDomains {
		if sni == "youtube.com" {
			t.Fatalf("youtube.com must not be added to the routing/block set")
		}
	}
	if youtube.Fragmentation.Strategy != "combo" {
		t.Errorf("youtube set should be healed to combo, got %s", youtube.Fragmentation.Strategy)
	}
}

func TestApplyGroup_SkipsDisabledSet(t *testing.T) {
	disabledSet := config.NewSetConfig()
	disabledSet.Name = "Disabled"
	disabledSet.Enabled = false
	disabledSet.Targets.SNIDomains = []string{"youtube.com"}
	disabledSet.Targets.DomainsToMatch = []string{"youtube.com"}

	cfg := &config.Config{
		Sets: []*config.SetConfig{&disabledSet},
	}

	refSet := &config.SetConfig{}
	refSet.Fragmentation.Strategy = "combo"

	group := []domainWithSet{
		{domain: "youtube.com", set: refSet},
	}

	if err := applyGroup(cfg, group, nil); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sets) != 2 {
		t.Fatalf("should create new set (not reuse disabled), got %d sets", len(cfg.Sets))
	}
}

func TestSetListsAnyDomain(t *testing.T) {
	set := &config.SetConfig{}
	set.Targets.SNIDomains = []string{"YouTube.com", " discord.com "}

	tests := []struct {
		name     string
		domains  []string
		expected bool
	}{
		{"exact after trim", []string{"discord.com"}, true},
		{"case-insensitive", []string{"youtube.com"}, true},
		{"whitespace trimmed", []string{"  youtube.com  "}, true},
		{"subdomain", []string{"www.youtube.com"}, true},
		{"unrelated", []string{"twitter.com"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := setListsAnyDomain(set, tt.domains); got != tt.expected {
				t.Errorf("setListsAnyDomain(%v) = %v, want %v", tt.domains, got, tt.expected)
			}
		})
	}
}

func TestDomainInSNIList(t *testing.T) {
	list := []string{"YouTube.com", " discord.com "}

	tests := []struct {
		name     string
		domain   string
		expected bool
	}{
		{"case-insensitive present", "youtube.com", true},
		{"whitespace trimmed present", "discord.com", true},
		{"absent", "twitter.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domainInSNIList(list, tt.domain); got != tt.expected {
				t.Errorf("domainInSNIList(%q) = %v, want %v", tt.domain, got, tt.expected)
			}
		})
	}
}

func TestApplyGroup_ExistingSet_CaseInsensitive(t *testing.T) {
	existingSet := config.NewSetConfig()
	existingSet.Name = "MyYouTube"
	existingSet.Enabled = true
	existingSet.Targets.SNIDomains = []string{"YouTube.com"}
	existingSet.Targets.DomainsToMatch = []string{"YouTube.com"}
	existingSet.Fragmentation.Strategy = "tcp"

	cfg := &config.Config{
		Sets: []*config.SetConfig{&existingSet},
	}

	refSet := &config.SetConfig{}
	refSet.Fragmentation.Strategy = "combo"

	group := []domainWithSet{
		{domain: "youtube.com", set: refSet},
	}

	if err := applyGroup(cfg, group, nil); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sets) != 1 {
		t.Fatalf("should reuse existing set despite case difference, got %d sets", len(cfg.Sets))
	}
	if len(cfg.Sets[0].Targets.SNIDomains) != 1 {
		t.Errorf("should not append a case-variant duplicate, got %d: %v",
			len(cfg.Sets[0].Targets.SNIDomains), cfg.Sets[0].Targets.SNIDomains)
	}
	if cfg.Sets[0].Fragmentation.Strategy != "combo" {
		t.Errorf("strategy should be healed to combo, got %s", cfg.Sets[0].Fragmentation.Strategy)
	}
}

func updateFuncFixture(refresh func()) (*atomic.Pointer[config.Config], UpdateFunc) {
	ptr := &atomic.Pointer[config.Config]{}
	cfg := config.NewConfig()
	ptr.Store(&cfg)
	return ptr, NewUpdateFunc(ptr.Load, func(_, next *config.Config) error {
		ptr.Store(next)
		return nil
	}, refresh)
}

func bumpConnBytes(current *config.Config) (*config.Config, error) {
	next := current.Clone()
	next.Queue.TCPConnBytesLimit++
	return next, nil
}

func TestUpdateFuncRefreshesTheFirewallOutsideTheWriteLock(t *testing.T) {
	refreshing := make(chan struct{})
	release := make(chan struct{})
	ptr, update := updateFuncFixture(func() {
		close(refreshing)
		<-release
	})
	start := ptr.Load().Queue.TCPConnBytesLimit

	done := make(chan error, 1)
	go func() { done <- update(bumpConnBytes) }()
	select {
	case <-refreshing:
	case <-time.After(5 * time.Second):
		t.Fatal("a change to the intercepted connection bytes refreshes the firewall")
	}
	if got := ptr.Load().Queue.TCPConnBytesLimit; got != start+1 {
		t.Errorf("the config is stored before the refresh runs, got %d", got)
	}

	saved := make(chan struct{})
	go func() {
		unlock := config.LockWrites()
		unlock()
		close(saved)
	}()
	select {
	case <-saved:
	case <-time.After(5 * time.Second):
		t.Fatal("another save waits for a firewall refresh that is still running")
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the update never returned")
	}
}

func TestUpdateFuncRefreshesOnlyWhenNeeded(t *testing.T) {
	var refreshes atomic.Int32
	ptr, update := updateFuncFixture(func() { refreshes.Add(1) })
	start := ptr.Load().Queue.TCPConnBytesLimit

	if err := update(func(current *config.Config) (*config.Config, error) {
		next := current.Clone()
		next.System.Geo.AutoUpdate.LastRun = "2026-09-24T12:00:00Z"
		return next, nil
	}); err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 0 {
		t.Error("a save that leaves the firewall alone does not refresh it")
	}

	boom := errors.New("boom")
	if err := update(func(*config.Config) (*config.Config, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("a refused mutate is returned: %v", err)
	}
	if refreshes.Load() != 0 {
		t.Error("a refused mutate does not refresh")
	}

	if err := update(bumpConnBytes); err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 1 || ptr.Load().Queue.TCPConnBytesLimit != start+1 {
		t.Errorf("a firewall-relevant change is stored and refreshes once, got %d refreshes", refreshes.Load())
	}
}

func TestStopWithoutStartIsANoOp(t *testing.T) {
	cfg := config.NewConfig()
	var ptr atomic.Pointer[config.Config]
	ptr.Store(&cfg)
	w := New(&ptr, nil, nil)
	w.Stop()
}
