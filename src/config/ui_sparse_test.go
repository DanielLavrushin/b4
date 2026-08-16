package config

import (
	"strings"
	"testing"
)

func TestDashboardLayoutOmittedAtDefaults(t *testing.T) {
	cfg := NewConfig()
	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\"ui\"") {
		t.Fatalf("empty ui block should be omitted, got: %s", data)
	}
}

func TestDashboardLayoutPersisted(t *testing.T) {
	cfg := NewConfig()
	cfg.UI.Dashboard = DashboardLayout{
		Order:  []string{"mtproto", "runtime"},
		Hidden: []string{"blackhole"},
		Spans:  map[string]int{"runtime": 4},
	}
	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"\"ui\"", "mtproto", "blackhole", "\"runtime\": 4"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in %s", want, s)
		}
	}
}

func TestDashboardLayoutSanitized(t *testing.T) {
	in := DashboardLayout{
		Order:  []string{"a", "a", "", strings.Repeat("x", 100), "b"},
		Hidden: nil,
		Spans:  map[string]int{"a": 99, "b": -3, "": 5},
	}
	out := in.Sanitized()
	if len(out.Order) != 2 || out.Order[0] != "a" || out.Order[1] != "b" {
		t.Fatalf("order not sanitized: %#v", out.Order)
	}
	if out.Hidden != nil {
		t.Fatalf("empty hidden should stay nil: %#v", out.Hidden)
	}
	if out.Spans["a"] != 12 || out.Spans["b"] != 1 {
		t.Fatalf("spans not clamped: %#v", out.Spans)
	}
	if _, ok := out.Spans[""]; ok {
		t.Fatal("empty id should be dropped")
	}
}
