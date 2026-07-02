package log

import (
	"strings"
	"testing"
)

func TestSanitizeConnField(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean value untouched", "storage.googleapis.com", "storage.googleapis.com"},
		{"comma replaced", "evil,fake-set", "evil fake-set"},
		{"escape and nul replaced", "a\x1b[2Jb\x00c", "a [2Jb c"},
		{"newline tab del replaced", "x\n\ty\x7fz", "x  y z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeConnField(tc.in); got != tc.want {
				t.Fatalf("sanitizeConnField(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEmitConnectionKeepsCSVColumns(t *testing.T) {
	hub := GetConnectionHub()
	ch, _ := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	LogConnectionStr("TCP", "set,one", "bad\x1bdomain,com", "1.2.3.4:1", "", "5.6.7.8:443", "", "", "meta,data")

	select {
	case msg := <-ch:
		if strings.ContainsAny(msg, "\x1b\x00") {
			t.Fatalf("control characters leaked into connection log: %q", msg)
		}
		if got := strings.Count(msg, ","); got != 9 {
			t.Fatalf("expected 9 commas (10 columns incl. timestamp), got %d in %q", got, msg)
		}
	default:
		t.Fatalf("no connection log received")
	}
}
