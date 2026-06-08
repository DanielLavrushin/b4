package watchdog

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
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
			"old.com": {Domain: "old.com", Status: StatusHealthy},
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
	} else if st.Status != StatusHealthy {
		t.Errorf("new.com should be healthy, got %s", st.Status)
	} else if st.Interval != 300 {
		t.Errorf("new.com interval should be 300, got %d", st.Interval)
	}
}

func TestGroupByConfig(t *testing.T) {
	setA := &config.SetConfig{}
	setA.Fragmentation.Strategy = "combo"
	setA.Faking.Strategy = "ttl"
	setA.Faking.TTL = 3

	setB := &config.SetConfig{}
	setB.Fragmentation.Strategy = "combo"
	setB.Faking.Strategy = "ttl"
	setB.Faking.TTL = 3

	setC := &config.SetConfig{}
	setC.Fragmentation.Strategy = "disorder"
	setC.Faking.Strategy = "pastseq"

	items := []domainWithSet{
		{domain: "youtube.com", set: setA},
		{domain: "meduza.io", set: setC},
		{domain: "googlevideo.com", set: setB},
	}

	groups := groupByConfig(items)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	if len(groups[0]) != 2 {
		t.Errorf("first group should have 2 domains, got %d", len(groups[0]))
	}
	if len(groups[1]) != 1 {
		t.Errorf("second group should have 1 domain, got %d", len(groups[1]))
	}
}

func TestConfigsMatch(t *testing.T) {
	a := &config.SetConfig{}
	a.Fragmentation.Strategy = "combo"
	a.Faking.Strategy = "ttl"
	a.Faking.TTL = 3

	b := &config.SetConfig{}
	b.Fragmentation.Strategy = "combo"
	b.Faking.Strategy = "ttl"
	b.Faking.TTL = 3

	if !configsMatch(a, b) {
		t.Error("identical configs should match")
	}

	b.Faking.TTL = 5
	if configsMatch(a, b) {
		t.Error("different TTL should not match")
	}

	b.Faking.TTL = 3
	b.Fragmentation.Strategy = "disorder"
	if configsMatch(a, b) {
		t.Error("different strategy should not match")
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

	applyGroup(cfg, group)

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

	applyGroup(cfg, group)

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

	applyGroup(cfg, group)

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

	applyGroup(cfg, group)

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

	applyGroup(cfg, group)

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
