package tables

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestAStuckNftCannotBlockTheCallerForever(t *testing.T) {
	if !hasBinary("sh") {
		t.Skip("needs a shell")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "nft")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 30 &\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	prev := iptCommandTimeout
	iptCommandTimeout = 300 * time.Millisecond
	t.Cleanup(func() { iptCommandTimeout = prev })

	start := time.Now()
	_, err := NewNFTablesManager(&config.Config{}).runNft("list", "tables")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("an nft that never returns must be reported as a failure, not waited on")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("runNft blocked for %v; a stuck nft would hold the shutdown past its hard limit", elapsed)
	}
}
