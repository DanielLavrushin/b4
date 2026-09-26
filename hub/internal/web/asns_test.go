package web

import (
	"reflect"
	"strings"
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
	summary := targets.Summary()
	if !strings.Contains(summary, "ASNs: AS62041, AS44907, AS211157, AS59930 and 1 more") {
		t.Errorf("the summary must name the ASNs, got %q", summary)
	}
	if strings.Contains(summary, "no targets") {
		t.Errorf("the summary must not call an ASN set empty, got %q", summary)
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
