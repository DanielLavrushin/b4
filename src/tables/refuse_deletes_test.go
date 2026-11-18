package tables

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

var errDeleteRefused = errors.New("exit status 1")

func isDeleteArgv(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-D", "del", "delete", "destroy":
			return true
		}
	}
	return false
}

func refusedDeleteOutput(bin string) string {
	switch {
	case bin == "ip":
		return "RTNETLINK answers: No such file or directory"
	case bin == "nft":
		return "Error: Could not process rule: No such file or directory"
	case bin == "ipset":
		return "ipset v7.19: The set with the given name does not exist"
	case isIPTablesBinary(bin):
		return "iptables: Bad rule (does a matching rule exist in that chain?)."
	}
	return "No such file or directory"
}

func refuseDeletes(inner func(args ...string) (string, error)) func(args ...string) (string, error) {
	return func(args ...string) (string, error) {
		out, err := inner(args...)
		if len(args) == 0 || !isDeleteArgv(args[1:]) {
			return out, err
		}
		return refusedDeleteOutput(args[0]), errDeleteRefused
	}
}

func TestRefuseDeletesFailsEveryDeleteAndStillRecordsIt(t *testing.T) {
	var calls []string
	stub := refuseDeletes(func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "listed", nil
	})

	deletes := [][]string{
		{"iptables", "-w", "-t", "mangle", "-D", "PREROUTING", "-j", "B4_PREROUTING"},
		{"ip6tables", "-w", "-t", "mangle", "-D", "OUTPUT", "1"},
		{"ip", "rule", "del", "fwmark", "0x10000/0x10000", "lookup", "100"},
		{"ip", "-6", "rule", "del", "fwmark", "0x10000/0x10000", "lookup", "100"},
		{"nft", "delete", "table", "inet", "b4_route"},
		{"ipset", "del", "b4r_set_v4", "203.0.113.7"},
		{"ipset", "destroy", "b4r_set_v4"},
	}
	passes := [][]string{
		{"iptables", "-w", "-t", "mangle", "-A", "PREROUTING", "-d", "203.0.113.7", "-j", "B4_PREROUTING"},
		{"ip", "rule", "add", "fwmark", "0x10000/0x10000", "lookup", "100"},
		{"nft", "list", "table", "inet", "b4_route"},
		{"ipset", "add", "b4r_set_v4", "203.0.113.7", "-exist"},
	}

	var want []string
	for _, argv := range deletes {
		out, err := stub(argv...)
		if !errors.Is(err, errDeleteRefused) {
			t.Errorf("%q returned err %v, want the delete refused", strings.Join(argv, " "), err)
		}
		if out == "" || out == "listed" {
			t.Errorf("%q returned output %q, want the tool's answer for a missing entry", strings.Join(argv, " "), out)
		}
		want = append(want, strings.Join(argv, " "))
	}
	for _, argv := range passes {
		out, err := stub(argv...)
		if err != nil || out != "listed" {
			t.Errorf("%q returned (%q, %v), want the stub's own answer", strings.Join(argv, " "), out, err)
		}
		want = append(want, strings.Join(argv, " "))
	}
	if !slices.Equal(calls, want) {
		t.Errorf("the stub recorded %q, want every call once in order %q", calls, want)
	}
}

func TestRefuseDeletesEndsADeleteUntilErrorLoop(t *testing.T) {
	calls := 0
	orig := run
	t.Cleanup(func() { run = orig })
	run = refuseDeletes(func(args ...string) (string, error) {
		calls++
		return "", nil
	})

	routeDelRuleLoopExec(false, "0x10000/0x10000", "100")

	if calls != 1 {
		t.Fatalf("the ip rule delete loop ran %d deletes, want it to stop at the first refused one", calls)
	}
}
