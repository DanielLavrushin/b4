package config

import (
	"reflect"
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
	if base.RoutingResolverKey() == withList.RoutingResolverKey() {
		t.Fatal("the key must change with the list")
	}
	if base.RoutingResolverKey() != pinned.RoutingResolverKey() {
		t.Fatal("the key must not change with a pin")
	}
}

func TestACaseOnlyChangeInADoHPathIsANewResolver(t *testing.T) {
	a := DNSConfig{Enabled: true, DoHURL: "https://dns.example/Dns-Query"}
	b := DNSConfig{Enabled: true, DoHURL: "https://dns.example/dns-query"}
	if a.SameRoutingResolver(&b) {
		t.Fatal("a DoH path is case sensitive, so this is a different resolver")
	}

	for _, spelling := range []string{"Https://dns.example/Dns-Query", "https://DNS.Example/Dns-Query"} {
		c := DNSConfig{Enabled: true, DoHURL: spelling}
		if !a.SameRoutingResolver(&c) {
			t.Fatalf("%s: the scheme and the host are normalised, so their casing is not a change", spelling)
		}
	}
}

func TestNormalizeUpstreamLowersTheHostAndNothingElse(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://DNS.GOOGLE/dns-query", "https://dns.google/dns-query"},
		{"HTTPS://Dns.Example:8443/Dns-Query?Name=A#Top", "https://dns.example:8443/Dns-Query?Name=A#Top"},
		{"https://DNS.Example", "https://dns.example"},
		{"https://DNS.Example?DNS=AAAA", "https://dns.example?DNS=AAAA"},
		{"https://User:Secret@DNS.Example/Path", "https://User:Secret@dns.example/Path"},
		{"https://[2001:DB8::1]/dns-query", "https://[2001:db8::1]/dns-query"},
		{"https://dns.example/A@B", "https://dns.example/A@B"},
	} {
		got, ok := NormalizeUpstream(tc.in)
		if !ok || got != tc.want {
			t.Errorf("NormalizeUpstream(%q) = %q, %v, want %q", tc.in, got, ok, tc.want)
		}
	}
}

func TestOneDoHServerInAnotherHostCaseIsOneResolver(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.DoHURL = "https://dns.google/dns-query"
	set.DNS.Upstreams = []string{"https://DNS.GOOGLE/dns-query", "https://Dns.Google/dns-query", "1.1.1.1"}
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := set.DNS.Upstreams; len(got) != 1 || got[0] != "1.1.1.1" {
		t.Fatalf("stored list = %q, want the repeats of the primary gone", got)
	}
	if got := set.DNS.UpstreamList(); len(got) != 2 {
		t.Fatalf("the set asks %q, want one DoH server and one address", got)
	}
}

func TestValidateRefusesAnEntryThatCannotBeAResolver(t *testing.T) {
	for _, entry := range []string{
		"https:///dns-query",
		"https://?x=1",
		"https://exa mple/dns-query",
		"https://user:secret@dns.example/dns-query",
		"https://dns.example:0/dns-query",
		"https://dns.example:70000/dns-query",
		"https://[fe80::1%25eth0]/dns-query",
		"0.0.0.0",
		"::",
		"::ffff:0.0.0.0",
		"fe80::1%eth0",
		"1.1.1.1:53",
	} {
		cfg := NewConfig()
		set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
		set.DNS.Enabled = true
		set.DNS.Upstreams = []string{"1.1.1.1", entry}
		cfg.Sets = []*SetConfig{set}

		err := cfg.Validate()
		if err == nil {
			t.Errorf("%q passed Validate and is stored as %q", entry, set.DNS.Upstreams)
			continue
		}
		field := findField(mustValidationErr(t, err), "sets[0].dns.upstreams", "invalid_dns_upstream")
		if field == nil {
			t.Errorf("%q: validation = %v, want invalid_dns_upstream", entry, err)
			continue
		}
		if field.Params["index"] != 2 {
			t.Errorf("%q: the error names entry %v, want 2", entry, field.Params["index"])
		}
	}
}

func TestValidateAcceptsEveryKindOfResolverEntry(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.Upstreams = []string{
		"9.9.9.9",
		"2620:fe::fe",
		"https://dns.example",
		"https://dns.example:8443/dns-query",
		"https://1.1.1.1/dns-query",
		"https://[2606:4700:4700::1111]/dns-query",
		"https://dns.nextdns.io/abc123?name=router",
	}
	want := append([]string(nil), set.DNS.Upstreams...)
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !reflect.DeepEqual(set.DNS.Upstreams, want) {
		t.Fatalf("stored list = %q, want %q", set.DNS.Upstreams, want)
	}
}

func TestBlankEntriesAreDroppedWithoutAReport(t *testing.T) {
	dns := DNSConfig{Enabled: true, TargetDNS: "9.9.9.9", Upstreams: []string{"1.1.1.1", "", "   "}}
	var dropped []string
	dns.NormalizeUpstreamsLimit(MaxDNSUpstreams-1, []string{"9.9.9.9"}, func(value string) {
		dropped = append(dropped, value)
	})
	if len(dropped) != 0 {
		t.Errorf("the drop callback ran for %q: it reports an entry over the limit, and this set asks two resolvers", dropped)
	}
	if got := dns.Upstreams; len(got) != 1 || got[0] != "1.1.1.1" {
		t.Errorf("kept %q, want the one real entry", got)
	}
}

func TestValidateLeavesTheListAloneWhileTheRedirectIsOff(t *testing.T) {
	typed := []string{"dns.google", " 1.1.1.1 ", "", "HTTPS://DNS.Example/dns-query", "http://dns.example/dns-query", "0.0.0.0", "1.1.1.1"}
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.TargetDNS = "9.9.9.9"
	set.DNS.Upstreams = append([]string(nil), typed...)
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("the list is refused while the redirect is off and its field is hidden, unlike doh_url: %v", err)
	}
	if !reflect.DeepEqual(set.DNS.Upstreams, typed) {
		t.Fatalf("stored list = %q, want it as typed: %q", set.DNS.Upstreams, typed)
	}

	set.DNS.Enabled = true
	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[0].dns.upstreams", "invalid_dns_upstream") == nil {
		t.Fatalf("validation = %+v, want invalid_dns_upstream once the redirect is on", ve.Fields)
	}
}

func TestValidateTwiceChangesNothingMore(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.DoHURL = "Https://dns.google/dns-query"
	set.DNS.TargetDNS = "8.8.8.8"
	set.DNS.Upstreams = []string{" ::ffff:1.1.1.1 ", "1.1.1.1", "HTTPS://DNS.Google/dns-query", "2001:4860:4860:0:0:0:0:8888", "8.8.8.8", "https://DNS.Example/Dns-Query"}
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	first := append([]string(nil), set.DNS.Upstreams...)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	want := []string{"1.1.1.1", "2001:4860:4860::8888", "8.8.8.8", "https://dns.example/Dns-Query"}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(set.DNS.Upstreams, want) {
		t.Fatalf("got %q then %q, want %q both times", first, set.DNS.Upstreams, want)
	}
}

func TestAnInvalidPrimaryDoesNotTakeOneOfTheEight(t *testing.T) {
	cfg := NewConfig()
	set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "resolver.local"
	set.DNS.Upstreams = []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "94.140.14.14", "208.67.222.222", "208.67.220.220", "77.88.8.8"}
	cfg.Sets = []*SetConfig{set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := len(set.DNS.UpstreamList()); got != MaxDNSUpstreams {
		t.Fatalf("the set asks %d resolvers, want %d: a primary that never becomes a resolver takes no slot", got, MaxDNSUpstreams)
	}
}

func TestAURLInTheAddressFieldIsNotAResolver(t *testing.T) {
	for _, target := range []string{"https://dns.example/dns-query", "http://192.0.2.1/dns-query", "HTTPS://dns.example/dns-query"} {
		dns := DNSConfig{Enabled: true, TargetDNS: target}
		if list := dns.UpstreamList(); len(list) != 0 || dns.HasUpstream() {
			t.Errorf("target_dns %q became the resolver %v; the address field takes an address, a URL there is a bad target as before", target, list)
		}

		cfg := NewConfig()
		set := &SetConfig{Id: "dns1", Name: "dns", Enabled: true}
		set.DNS = DNSConfig{Enabled: true, TargetDNS: target, Upstreams: []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "94.140.14.14", "208.67.222.222", "208.67.220.220", "77.88.8.8"}}
		cfg.Sets = []*SetConfig{set}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("validate with target_dns %q: %v", target, err)
		}
		if got := len(set.DNS.Upstreams); got != MaxDNSUpstreams {
			t.Errorf("target_dns %q took a slot: %d of %d list entries kept", target, got, MaxDNSUpstreams)
		}
	}

	dns := DNSConfig{Enabled: true, TargetDNS: " 9.9.9.9 "}
	if list := dns.UpstreamList(); len(list) != 1 || list[0] != "9.9.9.9" {
		t.Errorf("an address with blanks around it gave %v, want the address", list)
	}
}
