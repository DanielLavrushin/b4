package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWSSizeStripScaleAloneRewrites(t *testing.T) {
	cases := []struct {
		ws   WSSizeConfig
		want bool
	}{
		{WSSizeConfig{StripScale: true}, false},
		{WSSizeConfig{Enabled: true, StripScale: true}, true},
		{WSSizeConfig{Enabled: true, Window: 1, StripScale: true}, true},
	}
	for _, c := range cases {
		if got := c.ws.Rewrites(); got != c.want {
			t.Errorf("%+v rewrites=%v, want %v", c.ws, got, c.want)
		}
	}
}

func TestWSSizeStripScaleOverridesTheScale(t *testing.T) {
	got := WSSizeConfig{Enabled: true, Window: 1, Scale: 6, StripScale: true}.Effective()
	want := WSSizeConfig{Enabled: true, Window: 1, StripScale: true}
	if got != want {
		t.Fatalf("the effective option is %+v, want %+v", got, want)
	}
	if off := (WSSizeConfig{Window: 1, StripScale: true}).Effective(); off != (WSSizeConfig{}) {
		t.Fatalf("a switched-off option keeps %+v", off)
	}
}

func TestWSSizeStripScaleSavesNoUnusedScale(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		cfg := wssizeConfig(func(_ *Config, set *SetConfig) {
			set.TCP.WSSize = WSSizeConfig{Enabled: enabled, Window: 1, Scale: 6, StripScale: true}
		})
		if err := cfg.Validate(); err != nil {
			t.Fatalf("enabled=%v: a set with both a scale and strip_scale is refused: %v", enabled, err)
		}
		if got := cfg.Sets[0].TCP.WSSize; got.Scale != 0 || !got.StripScale || got.Window != 1 {
			t.Fatalf("enabled=%v: saved as %+v, want the unused scale back at 0 and the rest kept", enabled, got)
		}
	}
	cfg := wssizeConfig(func(_ *Config, set *SetConfig) {
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, Scale: 6}
	})
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Sets[0].TCP.WSSize.Scale; got != 6 {
		t.Fatalf("a scale without strip_scale was saved as %d, want 6", got)
	}
}

func TestWSSizeStripScaleSummary(t *testing.T) {
	cases := map[string]WSSizeConfig{
		"1:none":            {Enabled: true, Window: 1, StripScale: true},
		"-:none":            {Enabled: true, StripScale: true},
		"2:none:first_data": {Enabled: true, Window: 2, Scale: 6, StripScale: true, Cutoff: WSSizeCutoffFirstData},
	}
	for want, ws := range cases {
		if got := ws.Summary(); got != want {
			t.Errorf("%+v is summarised as %q, want %q", ws, got, want)
		}
	}
}

func TestWSSizeStripScaleIsSparseAndValid(t *testing.T) {
	cfg := wssizeConfig(func(_ *Config, set *SetConfig) {
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1, StripScale: true}
	})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a set that removes the Window Scale option is refused: %v", err)
	}
	sparse, err := MarshalSparse(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sparse), `"strip_scale": true`) {
		t.Fatalf("the saved config lost strip_scale: %s", sparse)
	}
	set := NewSetConfig()
	if err := json.Unmarshal([]byte(`{"tcp":{"wssize":{"enabled":true,"window":1}}}`), &set); err != nil {
		t.Fatal(err)
	}
	if set.TCP.WSSize.StripScale {
		t.Fatal("a set saved before the option existed removes the Window Scale option")
	}
	plain, err := MarshalSparse(wssizeConfig(func(_ *Config, set *SetConfig) {
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Window: 1}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "strip_scale") {
		t.Fatalf("the default value is written out: %s", plain)
	}
}
