package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func wssizeConfig(mutate func(*Config, *SetConfig)) *Config {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "wssize-test"
	set.Name = "window"
	set.Enabled = true
	cfg.Sets = []*SetConfig{&set}
	mutate(&cfg, &set)
	return &cfg
}

func wssizeField(t *testing.T, err error, path, code string) *ValidationField {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a validation error at %s, got %v", path, err)
	}
	f := findField(ve, path, code)
	if f == nil {
		t.Fatalf("no %s at %s in %v", code, path, err)
	}
	return f
}

func TestWSSizeSparseZeroAndCompatibility(t *testing.T) {
	set := NewSetConfig()
	set.Id = "wssize-test"
	if set.TCP.WSSize.Enabled {
		t.Fatal("enabled by default")
	}
	set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 0, Scale: 0}
	m, err := SetToMap(&set)
	if err != nil {
		t.Fatal(err)
	}
	m, err = SparsifySetMap(m)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	imported := NewSetConfig()
	if err = json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	ApplySetDefaults(&imported)
	if imported.TCP.WSSize != set.TCP.WSSize {
		t.Fatalf("zero changed: %+v", imported.TCP.WSSize)
	}
	old := NewSetConfig()
	if err = json.Unmarshal([]byte(`{"tcp":{"win":{"mode":"zero"}}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.TCP.WSSize.Enabled || old.TCP.Win.Mode != "zero" {
		t.Fatal("legacy fake-window behavior changed")
	}
}

func TestWSSizeZeroMeansUnchanged(t *testing.T) {
	cases := []struct {
		ws   WSSizeConfig
		want bool
	}{
		{WSSizeConfig{}, false},
		{WSSizeConfig{Window: 1, Scale: 1}, false},
		{WSSizeConfig{Enabled: true}, false},
		{WSSizeConfig{Enabled: true, Window: 1}, true},
		{WSSizeConfig{Enabled: true, Window: MaxWSSizeWindow}, true},
		{WSSizeConfig{Enabled: true, Scale: 1}, true},
		{WSSizeConfig{Enabled: true, Scale: 6}, true},
		{WSSizeConfig{Enabled: true, Window: 1, Scale: 1}, true},
	}
	for _, c := range cases {
		if got := c.ws.Rewrites(); got != c.want {
			t.Errorf("%+v rewrites=%v, want %v", c.ws, got, c.want)
		}
	}
	cfg := wssizeConfig(func(_ *Config, set *SetConfig) {
		set.TCP.WSSize.Enabled = true
	})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a switched-on block with no numbers is refused: %v", err)
	}
	if !cfg.Sets[0].TCP.WSSize.Enabled {
		t.Fatal("validation switched the block off")
	}
}

func TestWSSizeRangeIsCheckedOnlyWhenSwitchedOn(t *testing.T) {
	bad := []struct {
		ws         WSSizeConfig
		path       string
		value, max int
	}{
		{WSSizeConfig{Window: -1}, "sets[0].tcp.wssize.window", -1, MaxWSSizeWindow},
		{WSSizeConfig{Window: MaxWSSizeWindow + 1}, "sets[0].tcp.wssize.window", MaxWSSizeWindow + 1, MaxWSSizeWindow},
		{WSSizeConfig{Window: 1, Scale: -1}, "sets[0].tcp.wssize.scale", -1, MaxWSSizeScale},
		{WSSizeConfig{Window: 1, Scale: MaxWSSizeScale + 1}, "sets[0].tcp.wssize.scale", MaxWSSizeScale + 1, MaxWSSizeScale},
	}
	for _, c := range bad {
		off := wssizeConfig(func(_ *Config, set *SetConfig) { set.TCP.WSSize = c.ws })
		if err := off.Validate(); err != nil {
			t.Errorf("switched-off block %+v is refused: %v", c.ws, err)
		}
		for _, setEnabled := range []bool{true, false} {
			on := wssizeConfig(func(_ *Config, set *SetConfig) {
				set.Enabled = setEnabled
				set.TCP.WSSize = c.ws
				set.TCP.WSSize.Enabled = true
			})
			f := wssizeField(t, on.Validate(), c.path, "out_of_range")
			if f.Params["set"] != "window" || f.Params["value"] != c.value || f.Params["min"] != 0 || f.Params["max"] != c.max {
				t.Errorf("%s, set enabled %v: params %v, want set window, value %d, min 0, max %d", c.path, setEnabled, f.Params, c.value, c.max)
			}
		}
	}
	both := wssizeConfig(func(_ *Config, set *SetConfig) {
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: MaxWSSizeWindow + 1, Scale: MaxWSSizeScale + 1}
	})
	var ve *ValidationError
	if err := both.Validate(); !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0].Path != "sets[0].tcp.wssize.window" {
		t.Errorf("two numbers outside the range must report the window alone, got %v", err)
	}
	second := wssizeConfig(func(c *Config, _ *SetConfig) {
		other := NewSetConfig()
		other.Id = "wssize-second"
		other.Name = "second"
		other.Enabled = true
		other.TCP.WSSize = WSSizeConfig{Enabled: true, Scale: MaxWSSizeScale + 1}
		c.Sets = append(c.Sets, &other)
	})
	if f := wssizeField(t, second.Validate(), "sets[1].tcp.wssize.scale", "out_of_range"); f.Params["set"] != "second" {
		t.Errorf("a number outside the range in the second set names set %v", f.Params["set"])
	}
	edge := wssizeConfig(func(_ *Config, set *SetConfig) {
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: MaxWSSizeWindow, Scale: MaxWSSizeScale}
	})
	if err := edge.Validate(); err != nil {
		t.Fatalf("the largest values are refused: %v", err)
	}
}

func TestWSSizeCutoffIsNormalised(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"first_data":   WSSizeCutoffFirstData,
		"client_hello": "",
		"unknown":      "",
		"FIRST_DATA":   "",
		" first_data ": "",
	}
	for in, want := range cases {
		for _, enabled := range []bool{false, true} {
			for _, setEnabled := range []bool{false, true} {
				cfg := wssizeConfig(func(_ *Config, set *SetConfig) {
					set.Enabled = setEnabled
					set.TCP.WSSize = WSSizeConfig{Enabled: enabled, Window: 1, Scale: 1, Cutoff: in}
				})
				if err := cfg.Validate(); err != nil {
					t.Fatalf("cutoff %q, block on %v, set enabled %v: %v", in, enabled, setEnabled, err)
				}
				if got := cfg.Sets[0].TCP.WSSize.Cutoff; got != want {
					t.Errorf("cutoff %q, block on %v, set enabled %v became %q, want %q", in, enabled, setEnabled, got, want)
				}
			}
		}
	}
	if (WSSizeConfig{}).StopsAtFirstData() {
		t.Fatal("the empty cutoff stops at the first data packet")
	}
	if !(WSSizeConfig{Cutoff: WSSizeCutoffFirstData}).StopsAtFirstData() {
		t.Fatal("first_data does not stop at the first data packet")
	}
}

func TestWSSizeTunIsNotAnError(t *testing.T) {
	warnings := captureEgressGatewayWarnings(t)
	cfg := wssizeConfig(func(c *Config, set *SetConfig) {
		c.Queue.Mode = "tun"
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	})
	for i := 0; i < 3; i++ {
		if err := cfg.Validate(); err != nil {
			t.Fatalf("the TUN engine refuses a set with wssize: %v", err)
		}
	}
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "'window'") || !strings.Contains((*warnings)[0], "TUN") {
		t.Fatalf("want one warning that names the set and the engine, got %q", *warnings)
	}

	idle := captureEgressGatewayWarnings(t)
	if err := wssizeConfig(func(c *Config, set *SetConfig) {
		c.Queue.Mode = "tun"
		set.TCP.WSSize = WSSizeConfig{Enabled: true}
	}).Validate(); err != nil {
		t.Fatal(err)
	}
	if len(*idle) != 1 {
		t.Fatalf("a block that is on with no numbers yet is ignored by the TUN engine all the same, want one warning, got %q", *idle)
	}

	quiet := captureEgressGatewayWarnings(t)
	for _, mutate := range []func(*Config, *SetConfig){
		func(c *Config, set *SetConfig) {
			set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
		},
		func(c *Config, set *SetConfig) {
			c.Queue.Mode = "nfqueue"
			set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
		},
		func(c *Config, set *SetConfig) {
			c.Queue.Mode = "tun"
			set.TCP.WSSize = WSSizeConfig{Window: 1, Scale: 1}
		},
		func(c *Config, set *SetConfig) {
			c.Queue.Mode = "tun"
			set.Enabled = false
			set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
		},
	} {
		if err := wssizeConfig(mutate).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if len(*quiet) != 0 {
		t.Fatalf("warned without an enabled wssize set on the TUN engine: %q", *quiet)
	}
}

func TestWSSizeConflictNamesTheSet(t *testing.T) {
	conflicts := map[string]func(*SetConfig){
		"routing":     func(set *SetConfig) { set.Routing.Enabled = true },
		"duplication": func(set *SetConfig) { set.TCP.Duplicate.Enabled = true },
		"routing in block mode": func(set *SetConfig) {
			set.Routing.Enabled = true
			set.Routing.Mode = RoutingModeBlock
		},
	}
	for name, conflict := range conflicts {
		idle := wssizeConfig(func(_ *Config, set *SetConfig) {
			set.TCP.WSSize = WSSizeConfig{Enabled: true}
			conflict(set)
		})
		wssizeField(t, idle.Validate(), "sets[0].tcp.wssize", "wssize_unsupported")

		second := wssizeConfig(func(c *Config, _ *SetConfig) {
			other := NewSetConfig()
			other.Id = "wssize-second"
			other.Name = "second"
			other.Enabled = true
			other.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
			conflict(&other)
			c.Sets = append(c.Sets, &other)
		})
		if f := wssizeField(t, second.Validate(), "sets[1].tcp.wssize", "wssize_unsupported"); f.Params["set"] != "second" {
			t.Errorf("%s in the second set: the error names set %v", name, f.Params["set"])
		}

		on := wssizeConfig(func(_ *Config, set *SetConfig) {
			set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
			conflict(set)
		})
		f := wssizeField(t, on.Validate(), "sets[0].tcp.wssize", "wssize_unsupported")
		if f.Params["set"] != "window" {
			t.Errorf("%s: the error names set %v", name, f.Params["set"])
		}
		if strings.Contains(f.Message, "bypass") || !strings.Contains(f.Message, `"window"`) {
			t.Errorf("%s: message %q", name, f.Message)
		}

		blockOff := wssizeConfig(func(_ *Config, set *SetConfig) {
			set.TCP.WSSize = WSSizeConfig{Window: 1, Scale: 1}
			conflict(set)
		})
		if err := blockOff.Validate(); err != nil {
			t.Errorf("%s with a switched-off block is refused: %v", name, err)
		}

		setOff := wssizeConfig(func(_ *Config, set *SetConfig) {
			set.Enabled = false
			set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
			conflict(set)
		})
		if err := setOff.Validate(); err != nil {
			t.Errorf("%s in a disabled set is refused: %v", name, err)
		}
	}
}

func TestWSSizeEffectiveKeepsOnlyWhatChangesPackets(t *testing.T) {
	first := WSSizeCutoffFirstData
	cases := []struct {
		name     string
		in, want WSSizeConfig
	}{
		{"off", WSSizeConfig{}, WSSizeConfig{}},
		{"off with numbers left behind", WSSizeConfig{Window: 1, Scale: 6, Cutoff: first}, WSSizeConfig{}},
		{"on with no numbers", WSSizeConfig{Enabled: true}, WSSizeConfig{}},
		{"on with no numbers and a cutoff", WSSizeConfig{Enabled: true, Cutoff: first}, WSSizeConfig{}},
		{"window only", WSSizeConfig{Enabled: true, Window: 1}, WSSizeConfig{Enabled: true, Window: 1}},
		{"window with a cutoff", WSSizeConfig{Enabled: true, Window: 1, Cutoff: first}, WSSizeConfig{Enabled: true, Window: 1, Cutoff: first}},
		{"window with a cutoff b4 does not know", WSSizeConfig{Enabled: true, Window: 1, Cutoff: "client_hello"}, WSSizeConfig{Enabled: true, Window: 1}},
		{"scale only", WSSizeConfig{Enabled: true, Scale: 1}, WSSizeConfig{Enabled: true, Scale: 1}},
		{"scale with a cutoff that has no window to stop", WSSizeConfig{Enabled: true, Scale: 6, Cutoff: first}, WSSizeConfig{Enabled: true, Scale: 6}},
		{"both", WSSizeConfig{Enabled: true, Window: 1, Scale: 6, Cutoff: first}, WSSizeConfig{Enabled: true, Window: 1, Scale: 6, Cutoff: first}},
	}
	for _, c := range cases {
		if got := c.in.Effective(); got != c.want {
			t.Errorf("%s: %+v became %+v, want %+v", c.name, c.in, got, c.want)
		}
	}
}

func TestWSSizeSummaryShowsZeroAsUnchanged(t *testing.T) {
	cases := map[string]WSSizeConfig{
		"":                  {Window: 1, Scale: 6, Cutoff: WSSizeCutoffFirstData},
		"-:-":               {Enabled: true},
		"1:6":               {Enabled: true, Window: 1, Scale: 6},
		"1024:-":            {Enabled: true, Window: 1024},
		"-:6":               {Enabled: true, Scale: 6},
		"65535:14":          {Enabled: true, Window: MaxWSSizeWindow, Scale: MaxWSSizeScale},
		"1:6:first_data":    {Enabled: true, Window: 1, Scale: 6, Cutoff: WSSizeCutoffFirstData},
		"-:-:first_data":    {Enabled: true, Cutoff: WSSizeCutoffFirstData},
		"1024:-:first_data": {Enabled: true, Window: 1024, Cutoff: WSSizeCutoffFirstData},
	}
	for want, ws := range cases {
		if got := ws.Summary(); got != want {
			t.Errorf("%+v is summarised as %q, want %q", ws, got, want)
		}
	}
}

func TestWSSizeCutoffRoundTrip(t *testing.T) {
	for _, cutoff := range []string{"", WSSizeCutoffFirstData} {
		set := NewSetConfig()
		set.Id = "wssize-test"
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Cutoff: cutoff, Window: 1, Scale: 1}
		m, err := SetToMap(&set)
		if err != nil {
			t.Fatal(err)
		}
		m, err = SparsifySetMap(m)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"cutoff"`) != (cutoff != "") {
			t.Fatalf("cutoff %q stored as %s", cutoff, raw)
		}
		imported := NewSetConfig()
		if err := json.Unmarshal(raw, &imported); err != nil {
			t.Fatal(err)
		}
		ApplySetDefaults(&imported)
		if imported.TCP.WSSize != set.TCP.WSSize {
			t.Fatalf("cutoff or numbers changed: %+v", imported.TCP.WSSize)
		}
		cfg := NewConfig()
		cfg.Sets = []*SetConfig{&imported}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if imported.TCP.WSSize != set.TCP.WSSize {
			t.Fatalf("validation changed %+v", imported.TCP.WSSize)
		}
	}
}
