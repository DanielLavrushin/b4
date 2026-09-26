package discovery

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func asnCarryingSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "telegram"
	set.Targets.SNIDomains = []string{"t.me"}
	set.Targets.ASNs = []string{"62041", "44907"}
	set.Targets.IpsToMatch = []string{"91.108.4.0/22", "149.154.160.0/20"}
	return set
}

func TestScopedSetNeverCarriesASNs(t *testing.T) {
	ds := altSuite(t, "t.me", &DNSDiscoveryResult{ExpectedIPs: []string{"149.154.167.99"}})
	base := asnCarryingSet()

	scoped := ds.scopeSetToDomains(&base, []string{"t.me"})
	if scoped.Targets.ASNs == nil || len(scoped.Targets.ASNs) != 0 {
		t.Errorf("a set scoped to discovered domains must carry an empty ASN list, got %#v", scoped.Targets.ASNs)
	}
	if !reflect.DeepEqual(scoped.Targets.IpsToMatch, []string{"149.154.167.99/32"}) {
		t.Errorf("the scoped set must match only the discovered addresses, got %v", scoped.Targets.IpsToMatch)
	}
	if !reflect.DeepEqual(base.Targets.ASNs, []string{"62041", "44907"}) {
		t.Errorf("scoping must not touch the source set, got %v", base.Targets.ASNs)
	}
	raw, err := json.Marshal(scoped)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"asns":[]`) {
		t.Errorf("a scoped set handed to the UI must carry an empty list, not null: %s", raw)
	}
}

func TestConfirmConfigNeverCarriesASNs(t *testing.T) {
	ds := altSuite(t, "t.me", &DNSDiscoveryResult{ExpectedIPs: []string{"149.154.167.99"}})
	stored := asnCarryingSet()

	cfg := ds.confirmConfig(&stored, []string{"t.me"})
	if cfg == nil || len(cfg.Sets) != 1 {
		t.Fatalf("expected one confirmation set, got %+v", cfg)
	}
	set := cfg.Sets[0]
	if len(set.Targets.ASNs) != 0 || len(set.Targets.IpsToMatch) != 0 {
		t.Errorf("the confirmation set is matched by domain alone: asns=%v ips=%v", set.Targets.ASNs, set.Targets.IpsToMatch)
	}
	if !set.Enabled || !reflect.DeepEqual(set.Targets.SNIDomains, []string{"t.me"}) {
		t.Errorf("the confirmation set must target the domains: %+v", set.Targets)
	}
	if !reflect.DeepEqual(stored.Targets.ASNs, []string{"62041", "44907"}) {
		t.Errorf("confirmation must not touch the stored set, got %v", stored.Targets.ASNs)
	}
}
