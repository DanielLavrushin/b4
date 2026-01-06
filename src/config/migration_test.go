package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWithMigration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("empty path returns nil", func(t *testing.T) {
		cfg := NewConfig()
		if err := cfg.LoadWithMigration(""); err != nil {
			t.Errorf("expected nil for empty path: %v", err)
		}
	})

	t.Run("nonexistent file errors", func(t *testing.T) {
		cfg := NewConfig()
		err := cfg.LoadWithMigration(filepath.Join(tmpDir, "nope.json"))
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("directory path errors", func(t *testing.T) {
		cfg := NewConfig()
		err := cfg.LoadWithMigration(tmpDir)
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
		if err := cfg.LoadWithMigration(path); err != nil {
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
		if err := loaded.LoadWithMigration(path); err != nil {
			t.Fatalf("LoadWithMigration failed: %v", err)
		}
		if loaded.Version != CurrentConfigVersion {
			t.Errorf("version should remain %d", CurrentConfigVersion)
		}
	})
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

}
