package sni

import (
	"github.com/daniellavrushin/b4/config"
	"net"
	"testing"
)

func TestCollectedUnionOrderingSourceAndEmpty(t *testing.T) {
	first := config.NewSetConfig()
	first.Id = "first"
	first.Enabled = true
	first.Targets.UseCollectedIPs = true
	first.Targets.IpsToMatch = []string{"8.8.8.8"}
	first.Targets.IPPriority = true
	second := config.NewSetConfig()
	second.Id = "second"
	second.Enabled = true
	second.Targets.UseCollectedIPs = true
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&first, &second}
	empty := NewSuffixSet(cfg.Sets)
	if yes, _ := empty.MatchIP(net.ParseIP("1.1.1.1")); yes {
		t.Fatal("empty pool matched")
	}
	view := cfg.WithCollectedIPs([]string{"1.1.1.1"})
	matcher := NewSuffixSet(view.Sets)
	for _, ip := range []string{"1.1.1.1", "8.8.8.8"} {
		yes, set := matcher.MatchIPWithSource(net.ParseIP(ip), "")
		if !yes || set.Id != "first" {
			t.Fatalf("union/order failed for %s: %v", ip, set)
		}
	}
	second.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	matcher = NewSuffixSet(cfg.WithCollectedIPs([]string{"1.1.1.1"}).Sets)
	yes, set, prefix := matcher.MatchIPWithSourceDetail(net.ParseIP("1.1.1.1"), "aa:bb:cc:dd:ee:ff")
	if !yes || set.Id != "second" || prefix != 32 {
		t.Fatal("source device or exact-address priority lost")
	}
	first.Targets.IPVersion = "6"
	matcher = NewSuffixSet(cfg.WithCollectedIPs([]string{"1.1.1.1"}).Sets)
	if yes, _ := matcher.MatchIPWithSource(net.ParseIP("1.1.1.1"), ""); yes {
		t.Fatal("address family filter lost")
	}
}
