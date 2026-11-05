package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestRouteCollectEntriesAddsTheFakeBlock(t *testing.T) {
	set := fakeBlockSet()

	v4, v6 := routeCollectEntries(set)
	if len(v6) != 0 {
		t.Fatalf("v6 entries = %v, want none", v6)
	}
	found := false
	for _, entry := range v4 {
		if entry == "198.18.0.0/15" {
			found = true
		}
	}
	if !found {
		t.Fatalf("v4 entries = %v, want the fake block among them", v4)
	}
}

func TestRouteCollectEntriesSkipsTheFakeBlockOutsideProxyRouting(t *testing.T) {
	set := fakeBlockSet()
	set.Routing = config.RoutingConfig{}

	v4, _ := routeCollectEntries(set)
	for _, entry := range v4 {
		if entry == "198.18.0.0/15" {
			t.Fatal("the fake block was routed into a set that does not use a proxy")
		}
	}
}

func TestRouteCollectEntriesKeepsTargetsBesideTheFakeBlock(t *testing.T) {
	set := fakeBlockSet()
	set.Targets.IpsToMatch = []string{"203.0.113.9", "198.18.0.0/15"}

	v4, _ := routeCollectEntries(set)
	count := 0
	for _, entry := range v4 {
		if entry == "198.18.0.0/15" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the fake block appears %d times in %v", count, v4)
	}
	if len(v4) != 2 {
		t.Fatalf("v4 entries = %v, want the target and the fake block", v4)
	}
}

func fakeBlockSet() *config.SetConfig {
	return &config.SetConfig{
		Name:    "fake",
		Enabled: true,
		Routing: config.RoutingConfig{Enabled: true, Mode: config.RoutingModeProxy},
		DNS:     config.DNSConfig{FakeIP: config.FakeIPConfig{Enabled: true, CIDR: "198.18.0.0/15"}},
	}
}
