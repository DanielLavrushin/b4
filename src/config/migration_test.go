package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func deepCompareFields(prefix string, expected, actual reflect.Value) []string {
	var diffs []string
	if expected.Kind() == reflect.Ptr {
		if expected.IsNil() && actual.IsNil() {
			return nil
		}
		if expected.IsNil() != actual.IsNil() {
			return []string{fmt.Sprintf("%s: nil mismatch", prefix)}
		}
		return deepCompareFields(prefix, expected.Elem(), actual.Elem())
	}
	if expected.Kind() == reflect.Struct {
		t := expected.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := prefix + "." + f.Name
			if prefix == "" {
				name = f.Name
			}
			diffs = append(diffs, deepCompareFields(name, expected.Field(i), actual.Field(i))...)
		}
		return diffs
	}
	if !reflect.DeepEqual(expected.Interface(), actual.Interface()) {
		diffs = append(diffs, fmt.Sprintf("%s: want %v, got %v", prefix, expected.Interface(), actual.Interface()))
	}
	return diffs
}

func TestLoadWithMigration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("empty path with no default config returns nil", func(t *testing.T) {
		cfg := NewConfig()
		if _, err := cfg.LoadWithMigration(""); err != nil {
			t.Errorf("expected nil for empty path with no discoverable config: %v", err)
		}
	})

	t.Run("nonexistent file signals creation", func(t *testing.T) {
		cfg := NewConfig()
		needsSave, err := cfg.LoadWithMigration(filepath.Join(tmpDir, "nope.json"))
		if err != nil {
			t.Errorf("expected no error for nonexistent file, got %v", err)
		}
		if !needsSave {
			t.Error("expected needsSave=true for nonexistent file")
		}
	})

	t.Run("directory path errors", func(t *testing.T) {
		cfg := NewConfig()
		_, err := cfg.LoadWithMigration(tmpDir)
		if err == nil {
			t.Error("expected error for directory path")
		}
	})

	t.Run("v0 migrates to current", func(t *testing.T) {
		path := filepath.Join(tmpDir, "v0.json")
		v0Json := `{
			"version": 0,
			"queue": {"start_num": 537, "threads": 4, "mark": 32768, "ipv4": true, "ipv6": false},
			"sets": [{"id": "11111111-1111-1111-1111-111111111111", "name": "default"}],
			"system": {}
		}`
		os.WriteFile(path, []byte(v0Json), 0644)

		cfg := NewConfig()
		if _, err := cfg.LoadWithMigration(path); err != nil {
			t.Fatalf("LoadWithMigration failed: %v", err)
		}

		if cfg.Version != CurrentConfigVersion {
			t.Errorf("expected version %d, got %d", CurrentConfigVersion, cfg.Version)
		}
		if len(cfg.Sets) > 0 && !cfg.Sets[0].Enabled {
			t.Error("migration should set Enabled=true")
		}
	})

	t.Run("current version skips migration", func(t *testing.T) {
		path := filepath.Join(tmpDir, "current.json")
		cfg := NewConfig()
		cfg.Version = CurrentConfigVersion
		cfg.SaveToFile(path)

		loaded := NewConfig()
		if _, err := loaded.LoadWithMigration(path); err != nil {
			t.Fatalf("LoadWithMigration failed: %v", err)
		}
		if loaded.Version != CurrentConfigVersion {
			t.Errorf("version should remain %d", CurrentConfigVersion)
		}
	})

	t.Run("sparse roundtrip preserves all set defaults", func(t *testing.T) {
		path := filepath.Join(tmpDir, "sparse_all.json")
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "all-defaults"
		set.Name = "AllDefaults"
		cfg.Sets = []*SetConfig{&set}
		cfg.SaveToFile(path)

		loaded := NewConfig()
		if _, err := loaded.LoadWithMigration(path); err != nil {
			t.Fatalf("LoadWithMigration failed: %v", err)
		}

		ls := loaded.Sets[0]
		expected := NewSetConfig()
		expected.Id = "all-defaults"
		expected.Name = "AllDefaults"

		diffs := deepCompareFields("", reflect.ValueOf(expected), reflect.ValueOf(*ls))
		for _, d := range diffs {
			t.Errorf("%s", d)
		}
	})
}

func TestDiscoverConfigPath(t *testing.T) {
	path := discoverConfigPath()
	if path == "" {
		t.Fatal("expected a non-empty path")
	}
	if !strings.HasPrefix(path, "/etc/b4/") && !strings.HasPrefix(path, "/opt/etc/b4/") {
		t.Errorf("unexpected path: %s", path)
	}
}

func TestApplyMigrations(t *testing.T) {
	t.Run("v0 to v1 sets enabled", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Enabled = false
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.applyMigrations(0, map[string]interface{}{}); err != nil {
			t.Fatalf("migration failed: %v", err)
		}
		if !cfg.Sets[0].Enabled {
			t.Error("v0->v1 should set Enabled=true")
		}
	})

	t.Run("v45 to v46 derives directory from error_file", func(t *testing.T) {
		cases := []struct {
			name string
			raw  map[string]interface{}
			want string
		}{
			{
				name: "custom path -> parent dir",
				raw:  map[string]interface{}{"system": map[string]interface{}{"logging": map[string]interface{}{"error_file": "/mnt/logs/errors.log"}}},
				want: "/mnt/logs",
			},
			{
				name: "empty error_file -> disabled",
				raw:  map[string]interface{}{"system": map[string]interface{}{"logging": map[string]interface{}{"error_file": ""}}},
				want: "",
			},
			{
				name: "absent error_file -> default kept",
				raw:  map[string]interface{}{},
				want: DefaultConfig.System.Logging.Directory,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cfg := NewConfig()
				if err := migrateV45to46(&cfg, tc.raw); err != nil {
					t.Fatalf("migration failed: %v", err)
				}
				if cfg.System.Logging.Directory != tc.want {
					t.Errorf("Directory: want %q, got %q", tc.want, cfg.System.Logging.Directory)
				}
			})
		}
	})
}
