package config

import (
	"encoding/json"
	"testing"
)

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

func TestWSSizeValidation(t *testing.T) {
	for _, ws := range []WSSizeConfig{{Window: -1}, {Window: 65536}, {Scale: -1}, {Scale: 15}, {Cutoff: "unknown"}} {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "wssize-test"
		set.TCP.WSSize = ws
		cfg.Sets = []*SetConfig{&set}
		if cfg.Validate() == nil {
			t.Fatalf("accepted %+v", ws)
		}
	}
	for _, mode := range []string{"tun", "nfqueue"} {
		cfg := NewConfig()
		cfg.Queue.Mode = mode
		set := NewSetConfig()
		set.Id = "wssize-test"
		set.TCP.WSSize.Enabled = true
		cfg.Sets = []*SetConfig{&set}
		err := cfg.Validate()
		if mode == "tun" && err == nil {
			t.Fatal("TUN accepted")
		}
		if mode == "nfqueue" && err != nil {
			t.Fatal(err)
		}
	}
}

func TestWSSizeHelloCutoffRoundTrip(t *testing.T) {
	for _, cutoff := range []string{"", "client_hello"} {
		set := NewSetConfig()
		set.Id = "wssize-test"
		set.TCP.WSSize = WSSizeConfig{Enabled: true, Cutoff: cutoff, Window: 0, Scale: 0}
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
		if err := json.Unmarshal(raw, &imported); err != nil {
			t.Fatal(err)
		}
		ApplySetDefaults(&imported)
		if imported.TCP.WSSize != set.TCP.WSSize {
			t.Fatalf("cutoff or zero changed: %+v", imported.TCP.WSSize)
		}
		cfg := NewConfig()
		cfg.Sets = []*SetConfig{&imported}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	old := NewSetConfig()
	if err := json.Unmarshal([]byte(`{"tcp":{"wssize":{"enabled":true,"window":1,"scale":1}}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.TCP.WSSize.Cutoff != "" {
		t.Fatal("legacy first-data cutoff changed")
	}
}
