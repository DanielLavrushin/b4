package config

import (
	"strings"
	"testing"
)

func TestExtendedPATHAddsMissingStandardDirs(t *testing.T) {
	got := ExtendedPATH("/usr/sbin:/usr/bin:/sbin:/bin")
	if !strings.Contains(got, "/opt/bin") || !strings.Contains(got, "/opt/sbin") {
		t.Fatalf("expected entware dirs to be appended, got %q", got)
	}
	if !strings.HasPrefix(got, "/usr/sbin:/usr/bin:/sbin:/bin") {
		t.Fatalf("expected inherited entries to keep priority, got %q", got)
	}
}

func TestExtendedPATHDeduplicatesAndDropsEmpty(t *testing.T) {
	got := ExtendedPATH("/opt/bin::/usr/bin:/opt/bin")
	seen := make(map[string]int)
	for _, p := range strings.Split(got, ":") {
		if p == "" {
			t.Fatalf("empty entry in %q", got)
		}
		seen[p]++
	}
	for p, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate entry %q in %q", p, got)
		}
	}
	if !strings.HasPrefix(got, "/opt/bin:/usr/bin:") {
		t.Fatalf("expected first-occurrence order, got %q", got)
	}
}

func TestExtendedPATHEmptyInput(t *testing.T) {
	got := ExtendedPATH("")
	if got != strings.Join(standardBinPaths, ":") {
		t.Fatalf("expected the standard list, got %q", got)
	}
}
