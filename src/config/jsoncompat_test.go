package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMasqueradeConfigUnmarshal(t *testing.T) {
	cases := []struct {
		name        string
		json        string
		wantEnabled bool
		wantIfaces  []string
	}{
		{"legacy true", `true`, true, nil},
		{"legacy false", `false`, false, nil},
		{"legacy true padded", "  true ", true, nil},
		{"object with ifaces", `{"enabled":true,"interfaces":["eth0","eth1"]}`, true, []string{"eth0", "eth1"}},
		{"object disabled", `{"enabled":false}`, false, nil},
		{"null", `null`, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m MasqueradeConfig
			if err := json.Unmarshal([]byte(tc.json), &m); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if m.Enabled != tc.wantEnabled {
				t.Errorf("Enabled = %v, want %v", m.Enabled, tc.wantEnabled)
			}
			if !equalStringSlice(m.Interfaces, tc.wantIfaces) {
				t.Errorf("Interfaces = %v, want %v", m.Interfaces, tc.wantIfaces)
			}
		})
	}
}

func TestMasqueradeConfigEqual(t *testing.T) {
	base := MasqueradeConfig{Enabled: true, Interfaces: []string{"eth0", "eth1"}}

	if !base.Equal(MasqueradeConfig{Enabled: true, Interfaces: []string{"eth1", "eth0"}}) {
		t.Error("Equal should ignore interface order (reorder must not force a soft restart)")
	}
	if base.Equal(MasqueradeConfig{Enabled: true, Interfaces: []string{"eth0"}}) {
		t.Error("Equal should detect a differing interface set")
	}
	if base.Equal(MasqueradeConfig{Enabled: false, Interfaces: []string{"eth0", "eth1"}}) {
		t.Error("Equal should detect an Enabled change")
	}
}

func TestEqualStringSet(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"empty", nil, nil, true},
		{"same order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"reordered", []string{"a", "b"}, []string{"b", "a"}, true},
		{"different length", []string{"a"}, []string{"a", "b"}, false},
		{"same length different members", []string{"a", "b"}, []string{"a", "c"}, false},
		{"duplicates vs distinct same length", []string{"a", "a"}, []string{"a", "b"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := equalStringSet(tc.a, tc.b); got != tc.want {
				t.Errorf("equalStringSet(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestMasqueradeLegacyMigration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("legacy bool plus interface migrates to nested config", func(t *testing.T) {
		path := filepath.Join(tmpDir, "legacy_masq.json")
		legacy := `{
			"version": 48,
			"sets": [],
			"system": {"tables": {"masquerade": true, "masquerade_interface": "eth0"}}
		}`
		if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
			t.Fatal(err)
		}

		cfg := NewConfig()
		if _, err := cfg.LoadWithMigration(path); err != nil {
			t.Fatalf("LoadWithMigration failed: %v", err)
		}
		if !cfg.System.Tables.Masquerade.Enabled {
			t.Error("expected masquerade enabled after migrating legacy bool")
		}
		if got := cfg.System.Tables.Masquerade.Interfaces; !equalStringSlice(got, []string{"eth0"}) {
			t.Errorf("expected interfaces [eth0], got %v", got)
		}
	})

	t.Run("legacy bool false still loads", func(t *testing.T) {
		path := filepath.Join(tmpDir, "legacy_masq_off.json")
		legacy := `{
			"version": 48,
			"sets": [],
			"system": {"tables": {"masquerade": false}}
		}`
		if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
			t.Fatal(err)
		}

		cfg := NewConfig()
		if _, err := cfg.LoadWithMigration(path); err != nil {
			t.Fatalf("LoadWithMigration failed on legacy bool=false: %v", err)
		}
		if cfg.System.Tables.Masquerade.Enabled {
			t.Error("expected masquerade disabled")
		}
	})
}
