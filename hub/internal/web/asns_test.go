package web

import (
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func TestTargetsDescribeASNs(t *testing.T) {
	set := testkit.SampleSet("Telegram")
	set.Targets.ASNs = []string{"62041", "44907", "211157", "59930", "15169"}
	env := testkit.BuildEnvelope(t, &set)
	imp, err := hubwire.Open(env, hubwire.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	projection, _, err := hubwire.Scrub(&imp.Set)
	if err != nil {
		t.Fatal(err)
	}
	targets := TargetsOf(projection)
	if targets.Empty() {
		t.Fatalf("a set that targets ASNs is not empty")
	}
	if len(targets.FilterTerms()) != 0 {
		t.Errorf("an ASN set has no target filters, got %v", targets.FilterTerms())
	}
	view := targetsView(targets)
	if !reflect.DeepEqual(view.ASNs, []string{"62041", "44907", "211157", "59930", "15169"}) {
		t.Errorf("the view must list the ASNs, got %v", view.ASNs)
	}

	plain := targetsView(TargetsOf(map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"a.example"}}}))
	if plain.ASNs == nil || len(plain.ASNs) != 0 {
		t.Errorf("a set without ASNs must report an empty list, not null: %#v", plain.ASNs)
	}
}
