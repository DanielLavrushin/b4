package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestKeepUnreadableConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	if notice := keepUnreadableConfig(path, nil); notice != "" {
		t.Fatalf("a clean load must not produce a notice, got %q", notice)
	}
	if notice := keepUnreadableConfig(path, errors.New("stat failed")); notice != "" {
		t.Fatalf("a missing file has nothing to keep, got %q", notice)
	}

	broken := `{"sets": [{"id": "a"`
	if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.NewConfig()
	_, loadErr := c.LoadWithMigration(path)
	if loadErr == nil {
		t.Fatal("a truncated config must fail to load")
	}
	notice := keepUnreadableConfig(path, loadErr)
	if !strings.Contains(notice, path+".corrupt") {
		t.Fatalf("the notice must name the kept copy, got %q", notice)
	}
	kept, err := os.ReadFile(path + ".corrupt")
	if err != nil || string(kept) != broken {
		t.Fatalf("copy = %q, %v", kept, err)
	}
	original, _ := os.ReadFile(path)
	if string(original) != broken {
		t.Fatal("the original must stay in place until the next save")
	}
}
