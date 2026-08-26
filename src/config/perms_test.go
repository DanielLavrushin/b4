package config

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSaveToFileTightensExistingPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	if err := os.WriteFile(path, []byte("{}"), 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0777); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig()
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != ConfigFileMode {
		t.Errorf("config mode = %#o, want %#o", info.Mode().Perm(), ConfigFileMode)
	}
}

func TestSaveToFileCreatesRestrictedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	cfg := NewConfig()
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != ConfigFileMode {
		t.Errorf("config mode = %#o, want %#o", info.Mode().Perm(), ConfigFileMode)
	}
}

func TestLoadFromFileTightensSiblingBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	writeMode(t, path, `{"version":1}`, 0777)
	writeMode(t, filepath.Join(dir, "b4.json.v51.bak"), `{}`, 0644)
	writeMode(t, filepath.Join(dir, "b4.json.pre304"), `{}`, 0666)
	writeMode(t, filepath.Join(dir, "discovery_history.json"), `[]`, 0644)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, name := range []string{"b4.json", "b4.json.v51.bak", "b4.json.pre304"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != ConfigFileMode {
			t.Errorf("%s mode = %#o, want %#o", name, info.Mode().Perm(), ConfigFileMode)
		}
	}

	info, err := os.Stat(filepath.Join(dir, "discovery_history.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("unrelated file was touched: mode = %#o", info.Mode().Perm())
	}
}

func TestMigrationBackupIsRestricted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	backupConfigBeforeMigration(path, []byte(`{"a":1}`), 7)

	info, err := os.Stat(path + ".v7.bak")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != ConfigFileMode {
		t.Errorf("backup mode = %#o, want %#o", info.Mode().Perm(), ConfigFileMode)
	}
}

func writeMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWithMigrationTightensSiblingBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	writeMode(t, path, `{"version":`+itoa(CurrentConfigVersion)+`}`, 0666)
	writeMode(t, filepath.Join(dir, "b4.json.bak.v1.74.0"), `{}`, 0666)
	writeMode(t, filepath.Join(dir, "b4.json.v51.bak"), `{}`, 0644)
	writeMode(t, filepath.Join(dir, "asn_cache.json"), `{}`, 0644)

	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, name := range []string{"b4.json", "b4.json.bak.v1.74.0", "b4.json.v51.bak"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != ConfigFileMode {
			t.Errorf("%s mode = %#o, want %#o", name, info.Mode().Perm(), ConfigFileMode)
		}
	}

	info, err := os.Stat(filepath.Join(dir, "asn_cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("unrelated file was touched: mode = %#o", info.Mode().Perm())
	}
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

func TestRestrictConfigFilesIgnoresSymlinkedSiblings(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "b4")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(root, "victim")
	writeMode(t, victim, "secret", 0644)

	path := filepath.Join(dir, "b4.json")
	writeMode(t, path, `{"version":`+itoa(CurrentConfigVersion)+`}`, 0666)
	if err := os.Symlink(victim, filepath.Join(dir, "b4.json.evil")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	info, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("symlink target was chmodded to %#o, want 0644", info.Mode().Perm())
	}

	info, err = os.Lstat(filepath.Join(dir, "b4.json.evil"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink entry was replaced")
	}
}

func TestRestrictConfigFilesTightensDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "b4")
	if err := os.MkdirAll(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "b4.json")
	writeMode(t, path, `{"version":`+itoa(CurrentConfigVersion)+`}`, 0666)

	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("config dir mode = %#o, want 0755", info.Mode().Perm())
	}
}
