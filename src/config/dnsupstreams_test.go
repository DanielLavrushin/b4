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
	for _, ip := range []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9", "208.67.222.222", "208.67.220.220", "77.88.8.8", "77.88.8.1"} {
		set.DNS.Upstreams = append(set.DNS.Upstreams, ip)
	}
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("a list over the limit must be trimmed, got %v", err)
	}
	if got := len(set.DNS.Upstreams); got != MaxDNSUpstreams {
		t.Fatalf("kept %d entries, want %d", got, MaxDNSUpstreams)
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
