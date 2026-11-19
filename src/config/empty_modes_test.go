package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var emptyModeFields = []struct {
	path  string
	field func(*SetConfig) *string
}{
	{"tcp.desync.mode", func(s *SetConfig) *string { return &s.TCP.Desync.Mode }},
	{"tcp.win.mode", func(s *SetConfig) *string { return &s.TCP.Win.Mode }},
	{"tcp.incoming.mode", func(s *SetConfig) *string { return &s.TCP.Incoming.Mode }},
	{"tcp.incoming.strategy", func(s *SetConfig) *string { return &s.TCP.Incoming.Strategy }},
	{"faking.sni_mutation.mode", func(s *SetConfig) *string { return &s.Faking.SNIMutation.Mode }},
	{"fragmentation.strategy", func(s *SetConfig) *string { return &s.Fragmentation.Strategy }},
	{"udp.faking_strategy", func(s *SetConfig) *string { return &s.UDP.FakingStrategy }},
	{"routing.block_action", func(s *SetConfig) *string { return &s.Routing.BlockAction }},
}

func TestValidateGivesAnEmptySetModeItsDefault(t *testing.T) {
	defaults := NewSetConfig()
	for _, f := range emptyModeFields {
		t.Run(f.path, func(t *testing.T) {
			cfg := NewConfig()
			set := NewSetConfig()
			set.Id, set.Name = "s", "S"
			*f.field(&set) = ""
			cfg.Sets = []*SetConfig{&set}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if got, want := *f.field(cfg.Sets[0]), *f.field(&defaults); got != want {
				t.Errorf("%s \"\" became %q after Validate, want the default %q", f.path, got, want)
			}
		})
	}
}

func TestValidateKeepsAnExplicitSetMode(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id, set.Name = "s", "S"
	set.TCP.Desync.Mode = "rst"
	set.TCP.Win.Mode = "zero"
	set.TCP.Incoming.Mode = "fake"
	set.TCP.Incoming.Strategy = "md5"
	set.Faking.SNIMutation.Mode = "grease"
	set.Fragmentation.Strategy = ConfigNone
	set.UDP.FakingStrategy = "checksum"
	set.Routing.BlockAction = BlockActionDrop
	want := set
	cfg.Sets = []*SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, f := range emptyModeFields {
		if got, w := *f.field(cfg.Sets[0]), *f.field(&want); got != w {
			t.Errorf("%s = %q after Validate, want the explicit %q", f.path, got, w)
		}
	}
}

func TestAnEmptySetModeStoredInTheFileIsRepairedAndDroppedOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b4.json")
	stored := fmt.Sprintf(`{"version":%d,"sets":[{"id":"s","name":"S","enabled":true,
		"tcp":{"desync":{"mode":""},"win":{"mode":""},"incoming":{"mode":"","strategy":""}},
		"faking":{"sni_mutation":{"mode":""}},
		"fragmentation":{"strategy":""},
		"udp":{"faking_strategy":""},
		"routing":{"block_action":""}}]}`, CurrentConfigVersion)
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err != nil {
		t.Fatalf("LoadWithMigration: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	defaults := NewSetConfig()
	for _, f := range emptyModeFields {
		if got, want := *f.field(cfg.Sets[0]), *f.field(&defaults); got != want {
			t.Errorf("%s stored as \"\" loads as %q, want the default %q", f.path, got, want)
		}
	}

	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Sets []map[string]any `json:"sets"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Sets) != 1 {
		t.Fatalf("saved %d sets, want 1", len(saved.Sets))
	}
	for _, key := range []string{"tcp", "faking", "fragmentation", "udp", "routing"} {
		if v, ok := saved.Sets[0][key]; ok {
			t.Errorf("the repaired set still writes %s: %v", key, v)
		}
	}
}
