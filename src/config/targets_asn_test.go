package config

import (
	"reflect"
	"testing"
)

func TestAppendIPKeepsItsSemantics(t *testing.T) {
	t.Run("nil lists stay nil for an empty append", func(t *testing.T) {
		targets := &TargetsConfig{}
		if err := targets.AppendIP(nil); err != nil {
			t.Fatal(err)
		}
		if targets.IPs != nil || targets.IpsToMatch != nil {
			t.Fatalf("empty append allocated lists: %#v %#v", targets.IPs, targets.IpsToMatch)
		}
	})

	t.Run("order kept, duplicates within the input and against both lists dropped", func(t *testing.T) {
		targets := &TargetsConfig{
			IPs:        []string{"1.1.1.1", "2.2.2.2"},
			IpsToMatch: []string{"8.8.8.0/24", "1.1.1.1", "2.2.2.2"},
		}
		if err := targets.AppendIP([]string{"3.3.3.3", "1.1.1.1", "3.3.3.3", "8.8.8.0/24", "4.4.4.4"}); err != nil {
			t.Fatal(err)
		}
		if want := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "8.8.8.0/24", "4.4.4.4"}; !reflect.DeepEqual(targets.IPs, want) {
			t.Fatalf("IPs = %v, want %v", targets.IPs, want)
		}
		if want := []string{"8.8.8.0/24", "1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"}; !reflect.DeepEqual(targets.IpsToMatch, want) {
			t.Fatalf("IpsToMatch = %v, want %v", targets.IpsToMatch, want)
		}
	})

	t.Run("duplicates already present are left alone", func(t *testing.T) {
		targets := &TargetsConfig{IPs: []string{"1.1.1.1", "1.1.1.1"}}
		_ = targets.AppendIP([]string{"1.1.1.1", "5.5.5.5"})
		if want := []string{"1.1.1.1", "1.1.1.1", "5.5.5.5"}; !reflect.DeepEqual(targets.IPs, want) {
			t.Fatalf("IPs = %v, want %v", targets.IPs, want)
		}
	})

	t.Run("large append stays linear", func(t *testing.T) {
		targets := &TargetsConfig{}
		batch := make([]string, 0, 50000)
		for i := 0; i < 50000; i++ {
			batch = append(batch, "10."+itoa(i/65536)+"."+itoa((i/256)%256)+"."+itoa(i%256))
		}
		_ = targets.AppendIP(batch)
		_ = targets.AppendIP(batch)
		if len(targets.IPs) != 50000 || len(targets.IpsToMatch) != 50000 {
			t.Fatalf("got %d IPs, %d IpsToMatch", len(targets.IPs), len(targets.IpsToMatch))
		}
	})
}

func TestAppendASNs(t *testing.T) {
	s := useTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "15169", Prefixes: []string{"8.8.8.0/24", "2001:4860::/32"}})

	targets := &TargetsConfig{ASNs: []string{"AS13335"}, IpsToMatch: []string{"8.8.8.0/24"}, IPVersion: "4"}
	added, invalid := targets.AppendASNs([]string{"as15169", "13335", "64512", "15169", "junk", "32934"})
	if want := []string{"15169", "32934"}; !reflect.DeepEqual(added, want) {
		t.Fatalf("added = %v, want %v", added, want)
	}
	if want := []string{"64512", "junk"}; !reflect.DeepEqual(invalid, want) {
		t.Fatalf("invalid = %v, want %v", invalid, want)
	}
	if want := []string{"AS13335", "15169", "32934"}; !reflect.DeepEqual(targets.ASNs, want) {
		t.Fatalf("ASNs = %v, want %v", targets.ASNs, want)
	}
	if want := []string{"8.8.8.0/24"}; !reflect.DeepEqual(targets.IpsToMatch, want) {
		t.Fatalf("IpsToMatch = %v, want %v (v4 only, no duplicate)", targets.IpsToMatch, want)
	}

	targets.IPVersion = ""
	targets.IpsToMatch = nil
	targets.ASNs = nil
	targets.AppendASNs([]string{"15169"})
	if want := []string{"8.8.8.0/24", "2001:4860::/32"}; !reflect.DeepEqual(targets.IpsToMatch, want) {
		t.Fatalf("IpsToMatch = %v, want %v", targets.IpsToMatch, want)
	}
}

func TestGetTargetsForSetExpandsASNsBetweenGeoIPAndManualIPs(t *testing.T) {
	s := useTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "15169", Prefixes: []string{"8.8.8.0/24", "2001:4860::/32"}})
	mustPut(t, s, &AsnInfo{ID: "13335", Prefixes: []string{"1.1.1.0/24"}})

	cfg := NewConfig()
	cfg.System.Geo.GeoIpPath = "/nonexistent/geoip.dat"
	set := NewSetConfig()
	set.Name = "order"
	set.Targets.GeoIpCategories = []string{"cat"}
	set.Targets.ASNs = []string{"15169", "13335", "174"}
	set.Targets.IPs = []string{"9.9.9.9", "2.2.2.0/24"}
	geoip := map[string][]string{"cat": {"5.5.5.0/24"}}

	for len(ASNRefreshRequests()) > 0 {
		<-ASNRefreshRequests()
	}
	_, ips, err := cfg.GetTargetsForSetWithCache(&set, nil, geoip)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"5.5.5.0/24", "8.8.8.0/24", "2001:4860::/32", "1.1.1.0/24", "9.9.9.9", "2.2.2.0/24"}
	if !reflect.DeepEqual(ips, want) || !reflect.DeepEqual(set.Targets.IpsToMatch, want) {
		t.Fatalf("ips = %v, IpsToMatch = %v, want %v", ips, set.Targets.IpsToMatch, want)
	}
	select {
	case <-ASNRefreshRequests():
	default:
		t.Fatal("an unresolved ASN must ask the refresher to resolve it")
	}

	set.Targets.IPVersion = "6"
	_, ips, _ = cfg.GetTargetsForSetWithCache(&set, nil, geoip)
	if want := []string{"5.5.5.0/24", "2001:4860::/32", "9.9.9.9", "2.2.2.0/24"}; !reflect.DeepEqual(ips, want) {
		t.Fatalf("ip_version 6 expansion = %v, want %v", ips, want)
	}
}

func TestGetTargetsForSetWithResolvedASNsDoesNotRequestARefresh(t *testing.T) {
	s := useTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "15169", Prefixes: []string{"8.8.8.0/24"}})
	cfg := NewConfig()
	set := NewSetConfig()
	set.Targets.ASNs = []string{"15169"}
	for len(ASNRefreshRequests()) > 0 {
		<-ASNRefreshRequests()
	}
	if _, _, err := cfg.GetTargetsForSet(&set); err != nil {
		t.Fatal(err)
	}
	if len(ASNRefreshRequests()) != 0 {
		t.Fatal("a fully resolved set must not wake the refresher")
	}
}

func TestDeclaresDestinationTargets(t *testing.T) {
	cases := []struct {
		name string
		edit func(s *SetConfig)
		want bool
	}{
		{"nothing declared", func(s *SetConfig) {}, false},
		{"only source devices", func(s *SetConfig) { s.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"} }, false},
		{"expanded ips from code", func(s *SetConfig) { s.Targets.IpsToMatch = []string{"149.154.160.0/20"} }, true},
		{"expanded domains", func(s *SetConfig) { s.Targets.DomainsToMatch = []string{"a.example"} }, true},
		{"sni domains", func(s *SetConfig) { s.Targets.SNIDomains = []string{"a.example"} }, true},
		{"manual ips", func(s *SetConfig) { s.Targets.IPs = []string{"1.1.1.1"} }, true},
		{"geosite with a missing file", func(s *SetConfig) { s.Targets.GeoSiteCategories = []string{"gone"} }, true},
		{"geoip with a missing category", func(s *SetConfig) { s.Targets.GeoIpCategories = []string{"gone"} }, true},
		{"unresolved asn", func(s *SetConfig) { s.Targets.ASNs = []string{"15169"} }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := NewSetConfig()
			tc.edit(&set)
			if got := set.DeclaresDestinationTargets(); got != tc.want {
				t.Fatalf("DeclaresDestinationTargets = %v, want %v", got, tc.want)
			}
		})
	}
}
