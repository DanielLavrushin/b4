package tables

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddSetElementsFeedsNftOverStdin(t *testing.T) {
	dir := t.TempDir()
	captured := filepath.Join(dir, "script")
	fake := filepath.Join(dir, "nft")
	body := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = -f ] && [ \"$2\" = - ] || exit 2\ncat > %s\n", captured)
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	elements := make([]string, 0, 20000)
	for i := 0; i < 20000; i++ {
		elements = append(elements, fmt.Sprintf("%d.%d.%d.0/24", 10+i/65536, (i/256)%256, i%256))
	}

	n := NewNFTablesManager(nil)
	if err := n.addSetElements("b4_dup_v4", elements); err != nil {
		t.Fatalf("a set of 20000 elements must reach nft: %v", err)
	}

	raw, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if !strings.HasPrefix(script, "add element inet b4_mangle b4_dup_v4 { ") || !strings.HasSuffix(script, " }\n") {
		t.Fatalf("unexpected script shape: %.80q ... %.20q", script, script[len(script)-20:])
	}
	if got := strings.Count(script, ", ") + 1; got != len(elements) {
		t.Fatalf("script carries %d elements, want %d", got, len(elements))
	}
	if !strings.Contains(script, elements[len(elements)-1]) {
		t.Fatalf("last element missing from the script")
	}
}

func TestAddSetElementsReportsNftFailure(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "nft")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'Error: syntax error' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	n := NewNFTablesManager(nil)
	err := n.addSetElements("b4_dup_v4", []string{"10.0.0.0/8"})
	if err == nil || !strings.Contains(err.Error(), "syntax error") || !strings.Contains(err.Error(), "b4_dup_v4") {
		t.Fatalf("nft's own message must survive in the error, got: %v", err)
	}
}
