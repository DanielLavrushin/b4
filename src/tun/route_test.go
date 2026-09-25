package tun

import (
	"testing"
	"time"
)

func TestExtractField(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		keyword string
		want    string
	}{
		{"gateway present", "default via 94.189.76.193 dev eth0", "via", "94.189.76.193"},
		{"dev present", "default via 1.2.3.4 dev eth0 src 10.0.0.1", "dev", "eth0"},
		{"src present", "default via 1.2.3.4 dev eth0 src 10.0.0.1", "src", "10.0.0.1"},
		{"keyword missing", "default dev ppp0", "via", ""},
		{"src missing", "default via 1.2.3.4 dev eth0", "src", ""},
		{"keyword at end", "default via", "via", ""},
		{"empty line", "", "via", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractField(c.line, c.keyword); got != c.want {
				t.Errorf("extractField(%q, %q) = %q, want %q", c.line, c.keyword, got, c.want)
			}
		})
	}
}

func TestExtractGateway(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"via present", "default via 94.189.76.193 dev eth0", "94.189.76.193"},
		{"via with src", "default via 1.2.3.4 dev eth0 src 10.0.0.1", "1.2.3.4"},
		{"point-to-point no gateway", "default dev ppp0", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractGateway(c.line); got != c.want {
				t.Errorf("extractGateway(%q) = %q, want %q", c.line, got, c.want)
			}
		})
	}
}

func TestRecheckBeforeStartDoesNotBlock(t *testing.T) {
	done := make(chan struct{})
	go func() {
		(&Engine{}).Recheck()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Recheck blocked on an engine that never started")
	}
}
