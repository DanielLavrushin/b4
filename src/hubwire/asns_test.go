package hubwire

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func warningCodes(ws []Warning) map[string]Warning {
	out := make(map[string]Warning, len(ws))
	for _, w := range ws {
		out[w.Code] = w
	}
	return out
}

func asnOnlySet(asns ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "Telegram"
	set.Targets.ASNs = asns
	set.Faking.SNI = true
	set.Faking.TTL = 5
	return set
}

func TestScrubCountsASNsAsTargets(t *testing.T) {
	set := asnOnlySet("62041", "44907")
	projection, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(report.Warnings)["no_targets"]; ok {
		t.Errorf("a set that targets only ASNs targets something: %+v", report.Warnings)
	}
	got, _ := lookupPath(projection, "targets.asns")
	if !reflect.DeepEqual(stringList(got), []string{"62041", "44907"}) {
		t.Errorf("the projection must carry the ASNs as references, got %v", got)
	}
	if v := MinVersion(projection); v != "1.84.0" {
		t.Errorf("a set with ASNs needs the release that reads them, got %s", v)
	}

	plain := config.NewSetConfig()
	plain.Name = "plain"
	plain.Targets.SNIDomains = []string{"example.com"}
	projection, _, _ = Scrub(&plain)
	if _, ok := lookupPath(projection, "targets.asns"); ok {
		t.Errorf("an empty ASN list must be left out of the projection: %v", projection["targets"])
	}
	if v := MinVersion(projection); v != BaselineVersion {
		t.Errorf("a set without ASNs keeps the baseline version, got %s", v)
	}
}

func TestScrubStillWarnsWhenNothingIsTargeted(t *testing.T) {
	set := asnOnlySet()
	_, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(report.Warnings)["no_targets"]; !ok {
		t.Errorf("an empty ASN list is not a target: %+v", report.Warnings)
	}
}

func TestScrubWarnsOnTooManyASNs(t *testing.T) {
	ids := make([]string, 0, MaxASNs+1)
	for i := 0; i < MaxASNs; i++ {
		ids = append(ids, strconv.Itoa(1000+i))
	}
	set := asnOnlySet(ids...)
	_, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(report.Warnings)["too_many_asns"]; ok {
		t.Errorf("%d ASNs is within the limit: %+v", MaxASNs, report.Warnings)
	}

	set = asnOnlySet(append(ids, "2000")...)
	_, report, err = Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	w, ok := warningCodes(report.Warnings)["too_many_asns"]
	if !ok {
		t.Fatalf("expected too_many_asns, got %+v", report.Warnings)
	}
	if w.Params["count"] != MaxASNs+1 || w.Params["max"] != MaxASNs {
		t.Errorf("unexpected params %+v", w.Params)
	}
	if _, ok := warningCodes(report.Warnings)["too_many_ips"]; ok {
		t.Errorf("ASNs must not count against the address limit: %+v", report.Warnings)
	}
}

func TestFingerprintIgnoresASNs(t *testing.T) {
	a := asnOnlySet("62041")
	b := asnOnlySet("15169", "13335")
	pa, _, _ := Scrub(&a)
	pb, _, _ := Scrub(&b)
	if Fingerprint(pa) != Fingerprint(pb) {
		t.Errorf("the strategy fingerprint must not depend on the ASNs a set targets")
	}
}

func TestBuildOpenRoundTripCarriesASNs(t *testing.T) {
	set := asnOnlySet("62041", "44907")
	env, _, err := Build(&set, BuildOptions{B4Version: "1.84.0"})
	if err != nil {
		t.Fatal(err)
	}
	if env.MinB4 != "1.84.0" {
		t.Errorf("the envelope must ask for the release that reads ASNs, got %s", env.MinB4)
	}
	imp, err := Open(env, OpenOptions{B4Version: "1.84.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(imp.Set.Targets.ASNs, []string{"62041", "44907"}) {
		t.Errorf("ASNs lost across the wire: %v", imp.Set.Targets.ASNs)
	}
	if imp.Fingerprint != env.Fingerprint {
		t.Errorf("fingerprint drifted: %s vs %s", imp.Fingerprint, env.Fingerprint)
	}
	codes := warningCodes(imp.Warnings)
	for _, code := range []string{"unknown_fields", "values_changed", "no_targets", "version_too_old"} {
		if _, ok := codes[code]; ok {
			t.Errorf("unexpected %s on a clean round trip: %+v", code, imp.Warnings)
		}
	}

	old, err := Open(env, OpenOptions{B4Version: "1.83.0"})
	if err != nil {
		t.Fatal(err)
	}
	if w, ok := warningCodes(old.Warnings)["version_too_old"]; !ok || w.Params["min"] != "1.84.0" {
		t.Errorf("a release before ASN targets must be told it is too old: %+v", old.Warnings)
	}
}

func TestOpenNormalisesASNs(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "crafted",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{"asns": []interface{}{"AS62041", "62041", " as44907 "}},
			"faking":  map[string]interface{}{"sni": true},
		},
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(imp.Set.Targets.ASNs, []string{"62041", "44907"}) {
		t.Errorf("ASNs must be stored canonical and deduplicated, got %v", imp.Set.Targets.ASNs)
	}
	if _, ok := warningCodes(imp.Warnings)["values_changed"]; !ok {
		t.Errorf("rewriting the ASN list must be reported: %+v", imp.Warnings)
	}
}

func TestOpenRefusesReservedASNs(t *testing.T) {
	for _, asn := range []string{"64512", "0", "23456", "4200000001", "google"} {
		env := &Envelope{
			Format: Format,
			Title:  "crafted",
			Set: map[string]interface{}{
				"targets": map[string]interface{}{"asns": []interface{}{"62041", asn}},
			},
		}
		if _, err := Open(env, OpenOptions{}); err == nil {
			t.Errorf("an envelope carrying %q must be refused", asn)
		}
	}
}
