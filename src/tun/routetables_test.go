package tun

import (
	"errors"
	"strings"
	"testing"
)

func noNames(int) (string, bool) { return "", false }

func freeUsage() tableUsage {
	return tableUsage{pinned: map[int]bool{}, name: noNames}
}

func holding(tables ...int) func(int) bool {
	set := make(map[int]bool)
	for _, t := range tables {
		set[t] = true
	}
	return func(t int) bool { return set[t] }
}

func TestCaptureTableFor(t *testing.T) {
	cases := map[int]int{97: 96, 1: 2, 252: 251, 256: 257, 1000: 999}
	for route, want := range cases {
		if got := captureTableFor(route); got != want {
			t.Errorf("captureTableFor(%d) = %d, want %d", route, got, want)
		}
	}
}

func TestPickTunTablesDefaultsBelowBusyboxCap(t *testing.T) {
	for _, usesCapture := range []bool{true, false} {
		route, capture, err := pickTunTables(0, usesCapture, freeUsage())
		if err != nil {
			t.Fatal(err)
		}
		if route != 97 || capture != 96 {
			t.Fatalf("usesCapture=%v: got %d/%d, want 97/96", usesCapture, route, capture)
		}
	}
}

func TestPickTunTablesSkipsTablesInUse(t *testing.T) {
	cases := []struct {
		name        string
		usesCapture bool
		usage       func(u *tableUsage)
		wantRoute   int
	}{
		{"rule looks up the bypass table", false, func(u *tableUsage) { u.lookups = []string{"main", "97"} }, 96},
		{"bypass table unused in ports mode", true, func(u *tableUsage) { u.lookups = []string{"97"} }, 97},
		{"capture table holds routes", true, func(u *tableUsage) { u.routes = holding(96) }, 96},
		{"table named in rt_tables", false, func(u *tableUsage) {
			u.name = func(t int) (string, bool) { return "wgc1", t == 97 }
		}, 96},
		{"set pins the capture table", true, func(u *tableUsage) { u.pinned[96] = true }, 96},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := freeUsage()
			c.usage(&u)
			route, capture, err := pickTunTables(0, c.usesCapture, u)
			if err != nil {
				t.Fatal(err)
			}
			if route != c.wantRoute || capture != c.wantRoute-1 {
				t.Fatalf("got %d/%d, want %d/%d", route, capture, c.wantRoute, c.wantRoute-1)
			}
		})
	}
}

func TestPickTunTablesAvoidsXrayUITable(t *testing.T) {
	u := freeUsage()
	busy := []int{}
	for tbl := 78; tbl <= 97; tbl++ {
		busy = append(busy, tbl)
	}
	u.routes = holding(busy...)
	route, capture, err := pickTunTables(0, true, u)
	if err != nil {
		t.Fatal(err)
	}
	if capture == xrayUITproxyTable {
		t.Fatalf("picked XrayUI's table: %d/%d", route, capture)
	}
	if capture != 76 {
		t.Fatalf("got %d/%d, want capture 76", route, capture)
	}
}

func TestPickTunTablesExhausted(t *testing.T) {
	u := freeUsage()
	u.routes = func(int) bool { return true }
	if _, _, err := pickTunTables(0, true, u); err == nil {
		t.Fatal("expected an error when every candidate table is in use")
	}
}

func TestPickTunTablesExplicit(t *testing.T) {
	route, capture, err := pickTunTables(9999, true, freeUsage())
	if err != nil || route != 9999 || capture != 9998 {
		t.Fatalf("explicit 9999: got %d/%d err=%v", route, capture, err)
	}

	u := freeUsage()
	u.name = func(t int) (string, bool) { return "wan0", t == 100 }
	if route, capture, err := pickTunTables(100, true, u); err != nil || capture != 99 {
		t.Fatalf("ports mode uses only the capture table, so a named route table must not block it: %d/%d err=%v", route, capture, err)
	}
	if _, _, err := pickTunTables(100, false, u); err == nil || !strings.Contains(err.Error(), `named "wan0"`) {
		t.Fatalf("default mode must refuse a named bypass table and say why, got %v", err)
	}

	u = freeUsage()
	u.lookups = []string{"77"}
	if _, _, err := pickTunTables(78, true, u); err == nil || !strings.Contains(err.Error(), "table 77") {
		t.Fatalf("explicit table whose capture table is looked up by a foreign rule must be refused, got %v", err)
	}
}

func joinDels(dels []staleRule) string {
	var out []string
	for _, d := range dels {
		out = append(out, strings.Join(d.args, " "))
	}
	return strings.Join(out, "\n")
}

func TestPlanTunRuleSweep(t *testing.T) {
	out := strings.Join([]string{
		"0:\tfrom all lookup local",
		"3:\tfrom all fwmark 0x8000/0x27fff lookup 252",
		"5:\tfrom 192.168.50.0/24 fwmark 0x10000000 lookup 120",
		"9:\tfrom all fwmark 0x40000000/0x40000000 lookup 9998",
		"9:\tfrom all fwmark 0x80000/0x80000 lookup 9998",
		"600:\tfrom all fwmark 0x5678 lookup 9998",
		"10:\tfrom all fwmark 0x40000000/0x40000000 lookup 96 ",
		"10:\tnot from all fwmark 0x40000000/0x40000000 lookup 50",
		"88:\tfrom all fwmark 0x20000000/0x20000000 lookup main suppress_prefixlength 0",
		"89:\tfrom all fwmark 0x20000000/0x20000000 lookup 9999",
		"99:\tfrom all fwmark 0x10000000/0x10000000 lookup main suppress_prefixlength 0",
		"99:\tfrom all fwmark 0x10000000/0x10000000 lookup main",
		"100:\tfrom all fwmark 0x10000000/0x10000000 lookup 9999",
		"100:\tfrom all fwmark 0x8000 lookup 9999",
		"150:\tfrom all fwmark 0x10000000/0xf0000000 lookup 77",
		"200:\tfrom all fwmark 0x5 lookup 96",
		"300:\tfrom all fwmark 0x8000 lookup 120",
		"500:\tfrom all fwmark 0x20000000/0x20000000 iif d0 lookup 121",
		"600:\tfrom all fwmark 0x40000000/0x40000000 lookup 130",
		"32766:\tfrom all lookup main",
		"32767:\tfrom all lookup default",
	}, "\n")

	dels, flush := planTunRuleSweep(out, ownedTunTables(0), 0x8000)

	want := strings.Join([]string{
		"ip rule del priority 9 fwmark 0x40000000/0x40000000 lookup 9998",
		"ip rule del priority 9 fwmark 0x80000/0x80000 lookup 9998",
		"ip rule del priority 10 fwmark 0x40000000/0x40000000 lookup 96",
		"ip rule del priority 88 fwmark 0x20000000/0x20000000 lookup main",
		"ip rule del priority 89 fwmark 0x20000000/0x20000000 lookup 9999",
		"ip rule del priority 99 fwmark 0x10000000/0x10000000 lookup main",
		"ip rule del priority 100 fwmark 0x10000000/0x10000000 lookup 9999",
		"ip rule del priority 100 fwmark 0x8000 lookup 9999",
	}, "\n")
	if got := joinDels(dels); got != want {
		t.Fatalf("deletes:\n%s\nwant:\n%s", got, want)
	}
	if strings.Join(flush, ",") != "9999" {
		t.Fatalf("flush = %v, want [9999]: 9998 and 96 are still looked up by foreign rules and main is never flushed", flush)
	}
}

func TestPlanTunRuleSweepExplicitTable(t *testing.T) {
	out := "100:\tfrom all fwmark 0x8000 lookup 200\n10:\tfrom all fwmark 0x40000000/0x40000000 lookup 199\n500:\tfrom all fwmark 0x1234 lookup 201\n"
	dels, flush := planTunRuleSweep(out, ownedTunTables(200), 0x8000)
	if len(dels) != 2 || strings.Join(flush, ",") != "199,200" {
		t.Fatalf("the configured tables must be swept like the legacy ones: dels=%q flush=%v", joinDels(dels), flush)
	}
	foreign := "500:\tfrom all fwmark 0x1234 lookup 200\n"
	if dels, flush := planTunRuleSweep(foreign, ownedTunTables(200), 0x8000); len(dels) != 0 || len(flush) != 0 {
		t.Fatalf("a foreign mark on the configured table must be kept: dels=%q flush=%v", joinDels(dels), flush)
	}
	if owned := ownedTunTables(254); owned["254"] || owned["253"] {
		t.Fatalf("a reserved route_table must never make main or default sweepable: %v", owned)
	}
}

func TestPlanTunRuleSweepBareMarks(t *testing.T) {
	dels, flush := planTunRuleSweep("10:\tfrom all fwmark 0x40000000 lookup 96\n", ownedTunTables(0), 0x8000)
	if joinDels(dels) != "ip rule del priority 10 fwmark 0x40000000 lookup 96" {
		t.Fatalf("bare steer mark must be swept, got %q", joinDels(dels))
	}
	if strings.Join(flush, ",") != "96" {
		t.Fatalf("flush = %v, want [96]", flush)
	}
}

func TestRoutingErrorHints(t *testing.T) {
	tableErr := errors.New("ip rule add fwmark 0x40000000/0x40000000 lookup 9998 priority 10: exit status 1 (ip: invalid argument '9998' to 'table ID')")
	if got := routingError("ip rule add", 9998, tableErr).Error(); !strings.Contains(got, "queue.tun.route_table to 0") {
		t.Errorf("table rejection must point at route_table, got %q", got)
	}
	markErr := errors.New("exit status 1 (ip: invalid argument '0x40000000/0x40000000' to 'fwmark')")
	if got := routingError("ip rule add", 96, markErr).Error(); !strings.Contains(got, "1.33") {
		t.Errorf("fwmark mask rejection must name the busybox version, got %q", got)
	}
	usageErr := errors.New("exit status 1 (BusyBox v1.30.1 multi-call binary.\n\nUsage: ip [OPTIONS] address|route|link|tunnel OBJECT)")
	if got := routingError("ip rule add", 96, usageErr).Error(); !strings.Contains(got, "without 'ip rule'") {
		t.Errorf("an ip applet without rule support must be named, got %q", got)
	}
	other := errors.New("exit status 2 (RTNETLINK answers: Operation not supported)")
	if got := routingError("ip rule add", 96, other); !errors.Is(got, other) || strings.Contains(got.Error(), "busybox") {
		t.Errorf("unrelated failures must pass through without a busybox hint, got %q", got)
	}
}
