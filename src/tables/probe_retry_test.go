package tables

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func writeFlushingIptables(t *testing.T, chainVanishes bool) (bin string, calls string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "iptables")
	calls = filepath.Join(dir, "calls")
	state := filepath.Join(dir, "state")
	vanish := "exit 0"
	if chainVanishes {
		vanish = `if [ "$n" = 1 ]; then echo 2 >"` + state + `"; exit 1; fi; exit 0`
	}
	script := `#!/bin/sh
n=$(cat "` + state + `" 2>/dev/null || echo 0)
for a in "$@"; do
  if [ "$a" = "--version" ]; then echo "iptables v1.8.7"; exit 0; fi
done
case " $* " in
*" -A B4_MODULE_TEST "*)
  echo A >>"` + calls + `"
  if [ "$n" = 0 ]; then echo 1 >"` + state + `"; echo "iptables: No chain/target/match by that name." >&2; exit 1; fi
  ` + func() string {
		if chainVanishes {
			return "exit 0"
		}
		return `echo "iptables: No chain/target/match by that name." >&2; exit 1`
	}() + `
  ;;
*" -S B4_MODULE_TEST "*)
  ` + vanish + `
  ;;
esac
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake iptables: %v", err)
	}
	return bin, calls
}

func countProbeAppends(t *testing.T, calls string) int {
	t.Helper()
	data, err := os.ReadFile(calls)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(data)))
}

func TestProbeRetriesWhenItsChainWasFlushed(t *testing.T) {
	orig := probeSleep
	probeSleep = func(time.Duration) {}
	t.Cleanup(func() { probeSleep = orig })

	bin, calls := writeFlushingIptables(t, true)
	im := NewIPTablesManager(&config.Config{}, false)
	if err := im.checkConnbytesSupport(bin); err != nil {
		t.Fatalf("probe should pass on the retry after the table was rewritten under it, got: %v", err)
	}
	if got := countProbeAppends(t, calls); got != 2 {
		t.Fatalf("probe appended %d times, want 2 (one failure, one retry)", got)
	}
}

func TestProbeDoesNotRetryARealRejection(t *testing.T) {
	orig := probeSleep
	probeSleep = func(time.Duration) {}
	t.Cleanup(func() { probeSleep = orig })

	bin, calls := writeFlushingIptables(t, false)
	im := NewIPTablesManager(&config.Config{}, false)
	if err := im.checkConnbytesSupport(bin); err == nil {
		t.Fatalf("probe should fail when the match is rejected and the chain is still there")
	}
	if got := countProbeAppends(t, calls); got != 1 {
		t.Fatalf("probe appended %d times, want 1 (a real rejection is not retried)", got)
	}
}
