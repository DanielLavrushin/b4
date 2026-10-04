package tables

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRunEnsureNamesTheToolOutputOnce(t *testing.T) {
	prev := run
	t.Cleanup(func() { run = prev })
	const out = "Error: Could not process rule: Not supported"

	run = func(args ...string) (string, error) {
		return out, fmt.Errorf("command [%s] failed: exit status 1 (%s)", strings.Join(args, " "), out)
	}
	err := runEnsure("nft", "add", "table", "inet", "b4_route")
	if err == nil || strings.Count(err.Error(), out) != 1 {
		t.Fatalf("the tool's output is not in the error exactly once: %v", err)
	}

	run = func(args ...string) (string, error) { return out, errors.New("exit status 1") }
	if err := runEnsure("nft", "add", "table", "inet", "b4_route"); err == nil || !strings.Contains(err.Error(), out) {
		t.Fatalf("an error without the tool's output lost it: %v", err)
	}

	run = func(args ...string) (string, error) { return "", errors.New("exit status 1") }
	if err := runEnsure("nft", "add", "table", "inet", "b4_route"); err == nil || strings.HasSuffix(err.Error(), ": ") {
		t.Fatalf("a failure without output ends in an empty field: %q", err)
	}

	run = func(args ...string) (string, error) { return "Error: File exists", errors.New("exit status 1") }
	if err := runEnsure("nft", "add", "table", "inet", "b4_route"); err != nil {
		t.Fatalf("an object that already exists was reported as a failure: %v", err)
	}

	const ipsetOut = "ipset v7.24: Kernel error received: Operation not supported"
	run = func(args ...string) (string, error) {
		return ipsetOut, fmt.Errorf("command [%s] failed: exit status 1 (%s)", strings.Join(args, " "), ipsetOut)
	}
	err = (&routeIptBackend{}).ensureIPSet("b4r_test_v4", false)
	if err == nil || strings.Count(err.Error(), ipsetOut) != 1 {
		t.Fatalf("the ipset error a set reports in System Info does not carry the tool's output exactly once: %v", err)
	}
}
