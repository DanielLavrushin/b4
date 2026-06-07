package tables

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func writeFakeIptables(t *testing.T, version string, rejectWait bool, rejectConnbytes bool) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "iptables")
	reject := ""
	if rejectWait {
		reject = `  if [ "$a" = "-w" ]; then echo "iptables ` + version + `: unknown option \"-w\"" >&2; exit 2; fi`
	}
	cb := ""
	if rejectConnbytes {
		cb = `  if [ "$a" = "connbytes" ]; then echo "iptables: No chain/target/match by that name." >&2; exit 2; fi`
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = \"--version\" ]; then echo \"iptables " + version + "\"; exit 0; fi\n" +
		reject + "\n" +
		cb + "\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake iptables: %v", err)
	}
	return bin
}

func TestConnbytesProbe_OldIptablesWithoutWaitFlag(t *testing.T) {
	bin := writeFakeIptables(t, "v1.4.15", true, false)

	if iptablesSupportsWait(bin) {
		t.Fatalf("expected v1.4.15 to be detected as not supporting -w")
	}

	im := NewIPTablesManager(&config.Config{}, false)
	if err := im.checkConnbytesSupport(bin); err != nil {
		t.Fatalf("connbytes probe should pass once -w is stripped, got: %v", err)
	}
}

func TestConnbytesProbe_ModernIptablesKeepsWaitFlag(t *testing.T) {
	bin := writeFakeIptables(t, "v1.8.7 (nf_tables)", false, false)

	if !iptablesSupportsWait(bin) {
		t.Fatalf("expected v1.8.7 to be detected as supporting -w")
	}

	im := NewIPTablesManager(&config.Config{}, false)
	if err := im.checkConnbytesSupport(bin); err != nil {
		t.Fatalf("connbytes probe should pass on modern iptables, got: %v", err)
	}
}

func TestConnbytesProbe_ReportsRealError(t *testing.T) {
	bin := writeFakeIptables(t, "v1.8.7", false, true)

	im := NewIPTablesManager(&config.Config{}, false)
	err := im.checkConnbytesSupport(bin)
	if err == nil {
		t.Fatalf("expected connbytes probe to fail when the match is rejected")
	}
	if !filepath.IsAbs(bin) {
		t.Fatalf("test setup error: bin not absolute")
	}
}

func TestDropWaitFlag(t *testing.T) {
	in := []string{"iptables", "-w", "-t", "filter", "--wait", "-S"}
	got := dropWaitFlag(in)
	want := []string{"iptables", "-t", "filter", "-S"}
	if len(got) != len(want) {
		t.Fatalf("dropWaitFlag len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dropWaitFlag[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
