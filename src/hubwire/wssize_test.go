package hubwire

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func buildWSSizeSet(t *testing.T, ws config.WSSizeConfig) *Envelope {
	t.Helper()
	set := config.NewSetConfig()
	set.Name = "leftovers"
	set.Targets.SNIDomains = []string{"example.org"}
	set.TCP.WSSize = ws
	env, _, err := Build(&set, BuildOptions{B4Version: "1.91.0", Engine: "nfqueue"})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestSwitchedOffWSSizeDoesNotCross(t *testing.T) {
	plain := buildWSSizeSet(t, config.WSSizeConfig{})
	leftovers := config.WSSizeConfig{Enabled: false, Window: 1, Scale: 6, Cutoff: "first_data"}
	off := buildWSSizeSet(t, leftovers)
	if !reflect.DeepEqual(off.Set, plain.Set) {
		t.Errorf("a switched-off tcp.wssize block must not cross: tcp=%v, without the block tcp=%v", off.Set["tcp"], plain.Set["tcp"])
	}
	if off.Fingerprint != plain.Fingerprint {
		t.Errorf("a switched-off tcp.wssize block must not change the strategy fingerprint")
	}
	if off.MinB4 != plain.MinB4 {
		t.Errorf("a switched-off tcp.wssize block must not raise min_b4: %s, without the block %s", off.MinB4, plain.MinB4)
	}

	enabled := leftovers
	enabled.Enabled = true
	on := buildWSSizeSet(t, enabled)
	if _, ok := lookupPath(on.Set, "tcp.wssize"); !ok {
		t.Fatalf("an enabled tcp.wssize block must cross: tcp=%v", on.Set["tcp"])
	}
	if on.Fingerprint == plain.Fingerprint {
		t.Errorf("an enabled tcp.wssize block must change the strategy fingerprint")
	}
	if on.MinB4 != "1.91.0" || CompareVersions(on.MinB4, plain.MinB4) <= 0 {
		t.Errorf("an enabled tcp.wssize block must raise min_b4 to 1.91.0: %s, without the block %s", on.MinB4, plain.MinB4)
	}
	imported, err := Open(on, OpenOptions{B4Version: "1.91.0"})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Set.TCP.WSSize != enabled {
		t.Errorf("an enabled tcp.wssize block must arrive unchanged: %+v", imported.Set.TCP.WSSize)
	}
}

func TestWSSizeThatRewritesNothingDoesNotCross(t *testing.T) {
	plain := buildWSSizeSet(t, config.WSSizeConfig{})
	idle := map[string]config.WSSizeConfig{
		"switched on, window 0, scale 0":             {Enabled: true},
		"switched on, window 0, scale 0, first_data": {Enabled: true, Cutoff: config.WSSizeCutoffFirstData},
	}
	for name, ws := range idle {
		if ws.Rewrites() {
			t.Fatalf("%s is expected to rewrite nothing", name)
		}
		env := buildWSSizeSet(t, ws)
		if !reflect.DeepEqual(env.Set, plain.Set) {
			t.Errorf("%s changes no packet but crosses: tcp=%v, without the block tcp=%v", name, env.Set["tcp"], plain.Set["tcp"])
		}
		if env.Fingerprint != plain.Fingerprint {
			t.Errorf("%s changes no packet but changes the strategy fingerprint", name)
		}
		if env.MinB4 != plain.MinB4 {
			t.Errorf("%s changes no packet but raises min_b4 to %s, without the block %s", name, env.MinB4, plain.MinB4)
		}
	}
	for name, ws := range map[string]config.WSSizeConfig{
		"scale only":  {Enabled: true, Scale: 6},
		"window only": {Enabled: true, Window: 1},
	} {
		env := buildWSSizeSet(t, ws)
		if _, ok := lookupPath(env.Set, "tcp.wssize"); !ok || env.MinB4 != "1.91.0" || env.Fingerprint == plain.Fingerprint {
			t.Errorf("%s rewrites packets and must still cross with its own fingerprint and min_b4 1.91.0: tcp=%v min_b4=%s", name, env.Set["tcp"], env.MinB4)
		}
	}
}

func TestWSSizeCutoffWithoutAWindowDoesNotCross(t *testing.T) {
	scaled := buildWSSizeSet(t, config.WSSizeConfig{Enabled: true, Scale: 6})
	for _, cutoff := range []string{config.WSSizeCutoffFirstData, "client_hello"} {
		env := buildWSSizeSet(t, config.WSSizeConfig{Enabled: true, Scale: 6, Cutoff: cutoff})
		if !reflect.DeepEqual(env.Set, scaled.Set) || env.Fingerprint != scaled.Fingerprint {
			t.Errorf("with no window to write the cutoff %q changes no packet but crosses as another strategy: tcp=%v", cutoff, env.Set["tcp"])
		}
	}
	windowed := buildWSSizeSet(t, config.WSSizeConfig{Enabled: true, Window: 1})
	stale := buildWSSizeSet(t, config.WSSizeConfig{Enabled: true, Window: 1, Cutoff: "client_hello"})
	if !reflect.DeepEqual(stale.Set, windowed.Set) || stale.Fingerprint != windowed.Fingerprint {
		t.Errorf("a cutoff value b4 reads as the default crosses as another strategy: tcp=%v", stale.Set["tcp"])
	}
	firstData := buildWSSizeSet(t, config.WSSizeConfig{Enabled: true, Window: 1, Cutoff: config.WSSizeCutoffFirstData})
	if firstData.Fingerprint == windowed.Fingerprint {
		t.Error("with a window to write, the first_data cutoff is part of the strategy")
	}
}

func openWSSize(t *testing.T, ws map[string]interface{}) (*Imported, error) {
	t.Helper()
	env := buildWSSizeSet(t, config.WSSizeConfig{})
	env.Set["tcp"] = map[string]interface{}{"wssize": ws}
	env.Fingerprint = ""
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var wire Envelope
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	return Open(&wire, OpenOptions{B4Version: "1.91.0"})
}

func TestOpenTakesAForeignWSSizeBlockForWhatItDoes(t *testing.T) {
	plain := buildWSSizeSet(t, config.WSSizeConfig{})
	inert := map[string]map[string]interface{}{
		"switched on with nothing to rewrite":         {"enabled": true},
		"switched off with numbers left behind":       {"enabled": false, "window": 1, "scale": 6, "cutoff": "first_data"},
		"switched off with numbers outside the range": {"enabled": false, "window": 70000, "scale": 99},
	}
	for name, ws := range inert {
		imp, err := openWSSize(t, ws)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if imp.Set.TCP.WSSize != (config.WSSizeConfig{}) {
			t.Errorf("%s changes no packet and must be imported as no block: %+v", name, imp.Set.TCP.WSSize)
		}
		if imp.Fingerprint != plain.Fingerprint {
			t.Errorf("%s must have the fingerprint of the set without the block", name)
		}
		if _, ok := warningCodes(imp.Warnings)["values_changed"]; ok {
			t.Errorf("%s is dropped, not changed, and must not be reported as a changed value: %+v", name, imp.Warnings)
		}
	}
	for name, ws := range map[string]map[string]interface{}{
		"switched off":                {"enabled": false, "cutoff": "client_hello"},
		"switched on with no numbers": {"enabled": true, "cutoff": "client_hello"},
		"scale only":                  {"enabled": true, "scale": 6, "cutoff": "client_hello"},
	} {
		imp, err := openWSSize(t, ws)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok := warningCodes(imp.Warnings)["values_changed"]; ok {
			t.Errorf("%s: its cutoff changes no packet and must not be reported as a changed value: %+v", name, imp.Warnings)
		}
	}
	sent, err := openWSSize(t, map[string]interface{}{"enabled": true, "window": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent.Warnings) != 0 {
		t.Errorf("a block that arrives as it was sent must carry no warning: %+v", sent.Warnings)
	}

	imp, err := openWSSize(t, map[string]interface{}{"enabled": true, "window": 1, "cutoff": "client_hello"})
	if err != nil {
		t.Fatal(err)
	}
	if want := (config.WSSizeConfig{Enabled: true, Window: 1}); imp.Set.TCP.WSSize != want {
		t.Errorf("a cutoff value b4 does not know must be imported as the default: %+v", imp.Set.TCP.WSSize)
	}
	if _, ok := warningCodes(imp.Warnings)["values_changed"]; !ok {
		t.Errorf("an unknown cutoff of a block that writes the window is read as the default and must be reported: %+v", imp.Warnings)
	}

	for name, ws := range map[string]map[string]interface{}{
		"window above the range": {"enabled": true, "window": 70000},
		"window below the range": {"enabled": true, "window": -1, "scale": 1},
		"scale above the range":  {"enabled": true, "window": 1, "scale": 15},
	} {
		if imp, err := openWSSize(t, ws); err == nil {
			t.Errorf("%s with the block on must be refused, imported %+v", name, imp.Set.TCP.WSSize)
		}
	}
}

func TestWSSizeCutoffCrossesAndChangesFingerprint(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "first data cutoff"
	set.Targets.SNIDomains = []string{"example.org"}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	first, _, err := Build(&set, BuildOptions{B4Version: "1.91.0", Engine: "nfqueue"})
	if err != nil {
		t.Fatal(err)
	}
	set.TCP.WSSize.Cutoff = "first_data"
	cut, _, err := Build(&set, BuildOptions{B4Version: "1.91.0", Engine: "nfqueue"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == cut.Fingerprint || cut.MinB4 != "1.91.0" {
		t.Fatal("cutoff missing from strategy identity or version guard")
	}
	imported, err := Open(cut, OpenOptions{B4Version: "1.91.0"})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Set.TCP.WSSize != set.TCP.WSSize || imported.Fingerprint != cut.Fingerprint {
		t.Fatal("window, scale or cutoff changed during hub import")
	}
	older, err := Open(cut, OpenOptions{B4Version: "1.90.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(older.Warnings)["version_too_old"]; !ok {
		t.Fatal("older router was not warned about the cutoff")
	}
}
