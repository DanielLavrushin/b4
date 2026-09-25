package detector

import (
	"slices"
	"testing"
)

func TestUpstreamOverrideKeepsB4OwnedLists(t *testing.T) {
	stale := embeddedLists
	stale.KnownResolverNames = []string{"google"}
	stale.DNSCheckDomains = []string{"old.example"}
	stale.Sites = []string{"upstream.example"}
	stale.DNSServers = nil

	merged := withUpstreamLists(stale)
	if !slices.Contains(merged.KnownResolverNames, "cdnext") {
		t.Fatalf("known resolver names %v come from the saved override, want the embedded ones", merged.KnownResolverNames)
	}
	if !slices.Equal(merged.DNSCheckDomains, embeddedLists.DNSCheckDomains) {
		t.Fatalf("check domains %v, want the embedded ones", merged.DNSCheckDomains)
	}
	if !slices.Equal(merged.Sites, stale.Sites) {
		t.Fatalf("sites %v, want the upstream ones", merged.Sites)
	}
	if len(merged.DNSServers) != len(embeddedLists.DNSServers) {
		t.Fatalf("an override without resolvers keeps the embedded ones, got %d", len(merged.DNSServers))
	}
}
