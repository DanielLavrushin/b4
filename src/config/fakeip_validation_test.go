package config

import "testing"

func fakeDnsSet(name, cidr string) *SetConfig {
	set := NewSetConfig()
	set.Id = "11111111-2222-3333-4444-55555555555" + name[len(name)-1:]
	set.Name = name
	set.Enabled = true
	set.DNS.Enabled = true
	set.DNS.FakeIP = FakeIPConfig{Enabled: true, CIDR: cidr}
	set.Routing.Enabled = true
	set.Routing.Mode = RoutingModeProxy
	set.Routing.Upstream.Host = "127.0.0.1"
	set.Routing.Upstream.Port = 1080
	set.Routing.Upstream.UseDomain = true
	set.Targets.SNIDomains = []string{"example.com"}
	return &set
}

func TestValidateRejectsFakeDnsWithAnUpstreamThatCarriesUDPAddresses(t *testing.T) {
	cfg := NewConfig()
	set := fakeDnsSet("fake1", "198.18.0.0/16")
	set.Routing.Upstream.UDP = true
	cfg.Sets = []*SetConfig{set}

	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[0].dns.fakeip", "fakeip_needs_tcp_upstream") == nil {
		t.Fatalf("validation = %+v, want fakeip_needs_tcp_upstream", ve.Fields)
	}
}

func TestValidateRejectsOverlappingFakeDnsBlocks(t *testing.T) {
	cfg := NewConfig()
	left := fakeDnsSet("fake1", "198.18.0.0/16")
	right := fakeDnsSet("fake2", "198.18.0.0/15")
	cfg.Sets = []*SetConfig{left, right}

	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[1].dns.fakeip.cidr", "fakeip_blocks_overlap") == nil {
		t.Fatalf("validation = %+v, want fakeip_blocks_overlap", ve.Fields)
	}
}

func TestValidateAcceptsDisjointFakeDnsBlocks(t *testing.T) {
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{
		fakeDnsSet("fake1", "198.18.0.0/16"),
		fakeDnsSet("fake2", "198.19.0.0/16"),
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestDNSInterceptionIncludesResolverListsAndFakeDns(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "11111111-2222-3333-4444-555555555555"
	set.Name = "list"
	set.Enabled = true
	set.DNS.Enabled = true
	set.DNS.Upstreams = []string{"9.9.9.9"}
	cfg.Sets = []*SetConfig{&set}
	if !cfg.DNSTCPInterceptEnabled() {
		t.Fatal("a set with only dns.upstreams still needs the TCP DNS listener")
	}

	set.DNS.Upstreams = nil
	set.DNS.FakeIP = FakeIPConfig{Enabled: true, CIDR: "198.18.0.0/16"}
	if !cfg.DNSTCPInterceptEnabled() {
		t.Fatal("fake DNS answers over TCP too, so the TCP DNS listener must be on")
	}
}
