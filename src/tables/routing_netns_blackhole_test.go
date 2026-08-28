package tables

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const (
	bhMark   = 0x5179
	bhTable  = 2293
	bhEgress = "b4bh0"
	bhDest   = "198.51.100.7"
)

func bhRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func bhTry(args ...string) { _ = exec.Command(args[0], args[1:]...).Run() }

func bhLookup(t *testing.T, mark uint32) (string, bool) {
	t.Helper()
	out, err := exec.Command("ip", "route", "get", bhDest,
		"mark", strconv.FormatUint(uint64(mark), 10)).CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

func bhSetup(t *testing.T) {
	t.Helper()
	netnsRequire(t)

	bhRun(t, "ip", "link", "add", bhEgress, "type", "dummy")
	bhRun(t, "ip", "link", "set", bhEgress, "up")
	bhRun(t, "ip", "addr", "add", "10.7.7.1/24", "dev", bhEgress)
	bhRun(t, "ip", "route", "add", "default", "via", "10.7.7.2", "dev", bhEgress)
	bhRun(t, "ip", "rule", "add", "fwmark",
		strconv.FormatUint(uint64(bhMark), 10)+"/"+strconv.FormatUint(uint64(routeSetMarkMask), 10),
		"lookup", strconv.Itoa(bhTable), "priority", "10293")

	t.Cleanup(func() {
		bhTry("ip", "rule", "del", "fwmark",
			strconv.FormatUint(uint64(bhMark), 10)+"/"+strconv.FormatUint(uint64(routeSetMarkMask), 10),
			"lookup", strconv.Itoa(bhTable))
		bhTry("ip", "route", "flush", "table", strconv.Itoa(bhTable))
		bhTry("ip", "link", "del", bhEgress)
	})
}

func TestNetnsEmptySetTableFallsThroughButABlackholeDoesNot(t *testing.T) {
	bhSetup(t)

	if out, ok := bhLookup(t, bhMark); !ok {
		t.Fatalf("an empty set table must fall through to the main table, so a set whose route is not "+
			"installed yet costs nothing: %s", out)
	}

	bhRun(t, "ip", "route", "replace", "blackhole", "default",
		"metric", routeKillSwitchMetric, "table", strconv.Itoa(bhTable))

	out, ok := bhLookup(t, bhMark)
	if ok {
		t.Fatalf("expected the blackhole to end the lookup, got %q", out)
	}
	t.Logf("blackhole ends the route lookup rather than falling through: %s", out)
}

func TestNetnsAnUnmarkedPacketIsUntouchedByTheKillSwitch(t *testing.T) {
	bhSetup(t)
	bhRun(t, "ip", "route", "replace", "blackhole", "default",
		"metric", routeKillSwitchMetric, "table", strconv.Itoa(bhTable))

	if out, ok := bhLookup(t, 0); !ok {
		t.Fatalf("a packet the set does not claim must route normally while the kill switch is armed: %s", out)
	}
	if out, ok := bhLookup(t, SelfDialMark); !ok {
		t.Fatalf("a connection b4 opened for itself carries 0x%x, which is outside the routing mask, so it "+
			"must not be caught by the set's rule: %s", SelfDialMark, out)
	}
}
