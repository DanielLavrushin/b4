package config

import (
	"reflect"
	"testing"
)

func asnValidationConfig(asns ...string) (*Config, *SetConfig) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	set.Name = "asn set"
	set.Targets.ASNs = asns
	cfg.Sets = []*SetConfig{&set}
	return &cfg, cfg.Sets[0]
}

func TestValidate_ASNsNormalisedInPlace(t *testing.T) {
	cfg, set := asnValidationConfig(" AS15169 ", "asn13335", "15169", "032934")
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	if want := []string{"15169", "13335", "32934"}; !reflect.DeepEqual(set.Targets.ASNs, want) {
		t.Fatalf("ASNs = %v, want %v", set.Targets.ASNs, want)
	}
	if err := cfg.Validate(); err != nil || !reflect.DeepEqual(set.Targets.ASNs, []string{"15169", "13335", "32934"}) {
		t.Fatalf("validation must be idempotent, got %v, %v", set.Targets.ASNs, err)
	}
}

func TestValidate_ASNInvalid(t *testing.T) {
	cfg, set := asnValidationConfig("AS15169", "64512", "bogus", "0")
	ve := mustValidationErr(t, cfg.Validate())
	var bad []any
	for _, f := range ve.Fields {
		if f.Path == "sets[0].targets.asns" && f.Code == "asn_invalid" {
			bad = append(bad, f.Params["value"])
		}
	}
	if want := []any{"64512", "bogus", "0"}; !reflect.DeepEqual(bad, want) {
		t.Fatalf("asn_invalid values = %v, want %v (fields %+v)", bad, want, ve.Fields)
	}
	if want := []string{"AS15169", "64512", "bogus", "0"}; !reflect.DeepEqual(set.Targets.ASNs, want) {
		t.Fatalf("a rejected list must be left as the user wrote it, got %v", set.Targets.ASNs)
	}
}

func TestValidate_ASNsNeedNoGeoFile(t *testing.T) {
	cfg, _ := asnValidationConfig("15169")
	cfg.System.Geo.GeoIpPath = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("ASNs must not require a geoip database, got %v", err)
	}
}

func TestValidate_ASNsScopeMSSClamp(t *testing.T) {
	cfg, set := asnValidationConfig("15169")
	set.MSSClamp.Enabled = true
	set.MSSClamp.Size = 88
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an ASN must count as an IP scope for the MSS clamp, got %v", err)
	}

	cfg, set = asnValidationConfig()
	set.MSSClamp.Enabled = true
	set.MSSClamp.Size = 88
	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[0].mss_clamp", "mss_clamp_scope_required") == nil {
		t.Fatalf("missing mss_clamp_scope_required; got %+v", ve.Fields)
	}
}
