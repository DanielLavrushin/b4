package warmtuple

import (
	"github.com/daniellavrushin/b4/config"
	"net/netip"
	"testing"
)

func TestCollectedTargetsSupplementReuseEligibility(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = true
	set := config.NewSetConfig()
	set.Id = "consumer"
	set.Enabled = true
	set.Targets.UseCollectedIPs = true
	set.Targets.IpsToMatch = []string{"8.8.8.0/24"}
	cfg.Sets = []*config.SetConfig{&set}
	active := cfg.WithCollectedIPs([]string{"1.1.1.1", "2606:4700:4700::1111"}).Sets[0]
	for _, raw := range []string{"1.1.1.1", "2606:4700:4700::1111", "8.8.8.8"} {
		if !containsTarget(active, netip.MustParseAddr(raw)) {
			t.Fatalf("target rejected: %s", raw)
		}
	}
	if containsTarget(&set, netip.MustParseAddr("1.1.1.1")) || containsTarget(active, netip.MustParseAddr("9.9.9.9")) {
		t.Fatal("empty pool or unrelated IP accepted")
	}
}
