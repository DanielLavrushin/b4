package web

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

const wssizeTerm = "wssize"

func setWithoutTechniques() config.SetConfig {
	set := config.NewSetConfig()
	set.Targets.IPs = []string{"203.0.113.0/24"}
	set.Faking.SNI = false
	set.Fragmentation.Strategy = config.ConfigNone
	set.UDP.Mode = config.ConfigNone
	return set
}

func isPassthrough(terms []Term) bool {
	return len(terms) == 1 && terms[0].Code == TechPassthrough
}

func TestWSSizeIsDescribedAsATechnique(t *testing.T) {
	bare := setWithoutTechniques()
	if terms := Techniques(&bare, nil); !isPassthrough(terms) {
		t.Fatalf("a set without techniques must be described as passthrough: %+v", terms)
	}

	only := setWithoutTechniques()
	only.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 6, Cutoff: "first_data"}
	terms := Techniques(&only, nil)
	if isPassthrough(terms) {
		t.Errorf("a set that rewrites the real TCP window must not be described as passthrough: %+v", terms)
	}
	want := map[string]interface{}{"window": 1, "scale": 6, "cutoff": "first_data"}
	if ws := termOf(terms, wssizeTerm); ws == nil || len(terms) != 1 || !reflect.DeepEqual(ws.Params, want) {
		t.Errorf("a set whose only technique is wssize must be described by it alone, with its window, scale and cutoff: %+v", terms)
	}

	mixed := testkit.SampleSet("Mixed", "example.org")
	before := Techniques(&mixed, nil)
	mixed.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 6}
	after := Techniques(&mixed, nil)
	ws := termOf(after, wssizeTerm)
	if ws == nil || len(after) != len(before)+1 || termOf(after, TechFake) == nil || termOf(after, TechFrag) == nil {
		t.Fatalf("wssize must be listed next to the other techniques of a set: %+v", after)
	}
	if !reflect.DeepEqual(ws.Params, map[string]interface{}{"window": 1, "scale": 6}) {
		t.Errorf("the default cutoff must not be a parameter: %+v", ws.Params)
	}

	scaleOnly := setWithoutTechniques()
	scaleOnly.TCP.WSSize = config.WSSizeConfig{Enabled: true, Scale: 6}
	if ws := termOf(Techniques(&scaleOnly, nil), wssizeTerm); ws == nil || !reflect.DeepEqual(ws.Params, map[string]interface{}{"scale": 6}) {
		t.Errorf("a window left unchanged must not be described as window 0: %+v", Techniques(&scaleOnly, nil))
	}

	scaleOnly.TCP.WSSize.Cutoff = "first_data"
	if ws := termOf(Techniques(&scaleOnly, nil), wssizeTerm); ws == nil || !reflect.DeepEqual(ws.Params, map[string]interface{}{"scale": 6}) {
		t.Errorf("with no window to write the cutoff changes no packet and must not be a parameter: %+v", Techniques(&scaleOnly, nil))
	}

	stale := setWithoutTechniques()
	stale.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Cutoff: "client_hello"}
	if ws := termOf(Techniques(&stale, nil), wssizeTerm); ws == nil || !reflect.DeepEqual(ws.Params, map[string]interface{}{"window": 1}) {
		t.Errorf("a cutoff value b4 reads as the default must not be a parameter: %+v", Techniques(&stale, nil))
	}

	for name, ws := range map[string]config.WSSizeConfig{
		"switched off with leftover values":  {Window: 1, Scale: 6, Cutoff: "first_data"},
		"switched on with nothing to change": {Enabled: true},
	} {
		idle := setWithoutTechniques()
		idle.TCP.WSSize = ws
		if terms := Techniques(&idle, nil); !isPassthrough(terms) {
			t.Errorf("wssize %s changes no packet and must not be listed: %+v", name, terms)
		}
	}
}

func TestSharedWSSizeSetShowsTheTechniqueInTheConsole(t *testing.T) {
	f := newFixture(t, password)
	set := testkit.SampleSet("Small window", "example.org")
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 6, Cutoff: "first_data"}
	id, _ := f.shareAs(testkit.Identity(t), set, authorAddress)
	plain, _ := f.share("Plain", otherAddress, "plain.example")

	if v := f.versionOf(id, 1); v.B4Min != "1.91.0" {
		t.Errorf("a shared wssize set must be marked as needing b4 1.91.0, got %q", v.B4Min)
	}
	entry := entryOf(f.setsView().Pending, id, 1)
	if entry == nil {
		t.Fatal("the shared set is not in the queue")
	}
	ws := termOf(entry.Strategy, wssizeTerm)
	if ws == nil || ws.Params["window"] != float64(1) || ws.Params["scale"] != float64(6) || ws.Params["cutoff"] != "first_data" {
		t.Errorf("the queue entry must list wssize with its window, scale and cutoff: %+v", entry.Strategy)
	}
	if other := entryOf(f.setsView().Pending, plain, 1); other == nil || termOf(other.Strategy, wssizeTerm) != nil {
		t.Errorf("a set without wssize must not list it: %+v", other)
	}

	var rows SetRowsView
	f.admin(http.MethodGet, PathAPI+"/sets/rows?group=pending&q="+wssizeTerm, nil).decode(t, &rows)
	if len(rows.Rows) != 1 || rows.Rows[0].SetID != id || termOf(rows.Rows[0].Techniques, wssizeTerm) == nil {
		t.Errorf("a search for wssize must find the set that uses it and no other: %+v", rows.Rows)
	}
}
