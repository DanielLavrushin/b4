package config

import (
	"strings"
	"testing"
)

func TestUpstreamListKeepsTheOrderAndDropsDuplicates(t *testing.T) {
	dns := DNSConfig{
		Enabled:   true,
		DoHURL:    "https://dns.example/dns-query",
		TargetDNS: "9.9.9.9",
		Upstreams: []string{"1.1.1.1", "https://dns.example/dns-query", " 1.1.1.1 ", "not an address"},
	}
	got := dns.UpstreamList()
	want := []string{"https://dns.example/dns-query", "1.1.1.1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("UpstreamList = %v, want %v", got, want)
	}
}

func TestUpstreamListFallsBackToThePlainTarget(t *testing.T) {
	dns := DNSConfig{Enabled: true, TargetDNS: "9.9.9.9"}
	if got := dns.UpstreamList(); len(got) != 1 || got[0] != "9.9.9.9" {
		t.Fatalf("UpstreamList = %v, want the plain target alone", got)
	}
}

func TestUpstreamListCapsTheEffectiveList(t *testing.T) {
	upstreams := []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9", "149.112.112.112", "208.67.222.222", "208.67.220.220"}
	dns := DNSConfig{Enabled: true, TargetDNS: "77.88.8.8", Upstreams: upstreams}
	if got := dns.UpstreamList(); len(got) != MaxDNSUpstreams {
		t.Fatalf("UpstreamList has %d entries, want %d", len(got), MaxDNSUpstreams)
	}
}

func TestValidateRejectsAnEntryThatIsNotAnAddressOrURL(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "9.9.9.9"
	set.DNS.Upstreams = []string{"1.1.1.1", "dns.google"}
	cfg.Sets = []*SetConfig{set}

	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[0].dns.upstreams", "invalid_dns_upstream") == nil {
		t.Fatalf("validation = %+v, want invalid_dns_upstream", ve.Fields)
	}
}

func TestValidateRejectsAPlainHTTPUpstream(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "9.9.9.9"
	set.DNS.Upstreams = []string{"http://dns.example/dns-query"}
	cfg.Sets = []*SetConfig{set}

	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[0].dns.upstreams", "invalid_dns_upstream") == nil {
		t.Fatalf("validation = %+v, want invalid_dns_upstream", ve.Fields)
	}
}

func TestValidateDropsTheEntriesOverTheLimit(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "9.9.9.9"
	for _, ip := range []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "94.140.14.14", "208.67.222.222", "208.67.220.220", "77.88.8.8", "77.88.8.1"} {
		set.DNS.Upstreams = append(set.DNS.Upstreams, ip)
	}
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("a list over the limit must be trimmed, got %v", err)
	}
	if got := len(set.DNS.Upstreams); got != MaxDNSUpstreams-1 {
		t.Fatalf("kept %d entries, want %d: the primary counts toward the limit", got, MaxDNSUpstreams-1)
	}
	if got := len(set.DNS.UpstreamList()); got != MaxDNSUpstreams {
		t.Fatalf("the set asks %d resolvers in total, want %d", got, MaxDNSUpstreams)
	}
}

func TestSameResolverComparesTheListAndPins(t *testing.T) {
	base := DNSConfig{Enabled: true, TargetDNS: "9.9.9.9"}
	withList := base
	withList.Upstreams = []string{"1.1.1.1"}
	if base.SameResolver(&withList) {
		t.Fatal("a set that gained a resolver is not the same resolver")
	}
	pinned := base
	pinned.Pins = map[string][]string{"example.test": {"203.0.113.5"}}
	if base.SameResolver(&pinned) {
		t.Fatal("a set that gained a pin is not the same resolver")
	}
	if !base.SameResolver(&DNSConfig{Enabled: true, TargetDNS: "9.9.9.9"}) {
		t.Fatal("two identical configs are the same resolver")
	}
	if base.ResolverKey() == withList.ResolverKey() {
		t.Fatal("the key must change with the list")
	}
}

func TestValidateKeepsEightWhenTheListRepeatsThePrimary(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "9.9.9.9"
	set.DNS.Upstreams = []string{"9.9.9.9", "1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "94.140.14.14", "208.67.222.222", "208.67.220.220"}
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := len(set.DNS.UpstreamList()); got != MaxDNSUpstreams {
		t.Fatalf("the set asks %d resolvers, want %d: a repeated primary must not take a slot", got, MaxDNSUpstreams)
	}
}

func TestValidateAcceptsAnySchemeCasing(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.Upstreams = []string{"Https://dns.example/Dns-Query"}
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("a scheme written Https:// must be accepted, got %v", err)
	}
	if got := set.DNS.Upstreams[0]; got != "https://dns.example/Dns-Query" {
		t.Fatalf("normalisation = %q, want the scheme lowercased and the path kept", got)
	}
}

func TestSameRoutingResolverIgnoresPins(t *testing.T) {
	base := DNSConfig{Enabled: true, TargetDNS: "9.9.9.9"}
	pinned := base
	pinned.Pins = map[string][]string{"example.test": {"203.0.113.5"}}
	if !base.SameRoutingResolver(&pinned) {
		t.Fatal("tables never read pins, so a pin edit must not look like a new resolver")
	}
	withList := base
	withList.Upstreams = []string{"1.1.1.1"}
	if base.SameRoutingResolver(&withList) {
		t.Fatal("a new resolver in the list must look like a new resolver")
	}
	strict := base
	strict.Strict = true
	if base.SameRoutingResolver(&strict) {
		t.Fatal("strict changes the answer, so it must look like a new resolver")
	}
}
