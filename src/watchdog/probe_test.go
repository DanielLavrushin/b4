package watchdog

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestIsReservedAddrCoversTheWholeLocalSpace(t *testing.T) {
	reserved := []string{
		"127.0.0.1", "::1",
		"10.1.2.3", "192.168.1.1", "172.16.0.1",
		"169.254.1.1", "fe80::1",
		"100.64.0.1", "100.127.255.255",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"240.0.0.1",
		"fd00::1",
		"::ffff:192.168.1.1",
	}
	for _, s := range reserved {
		if !isReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s must be refused: probing it reaches the network b4 runs on", s)
		}
	}

	public := []string{"1.1.1.1", "8.8.8.8", "104.22.45.1", "2606:4700::1111", "100.63.255.255", "100.128.0.1"}
	for _, s := range public {
		if isReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s is a public address and must not be refused", s)
		}
	}
}

func TestProbeHostRefusesLiteralPrivateAddress(t *testing.T) {
	var priv *ErrPrivateDestination
	_, err := ProbeHost(context.Background(), "192.168.1.1", ProbeOptions{Timeout: time.Second})
	if !errors.As(err, &priv) {
		t.Fatalf("a private literal must be refused before any connection, got %v", err)
	}
}

func TestProbeHostRejectsNonHostnames(t *testing.T) {
	// The URL is rebuilt from the hostname, so a caller cannot choose the
	// scheme, port or path — a full URL is reduced to its host.
	for _, in := range []string{
		"http://10.0.0.1/cgi-bin/factory_reset",
		"https://127.0.0.1:8080/admin",
	} {
		var priv *ErrPrivateDestination
		_, err := ProbeHost(context.Background(), in, ProbeOptions{Timeout: time.Second})
		if !errors.As(err, &priv) {
			t.Errorf("%q reduces to a private host and must be refused, got %v", in, err)
		}
	}

	if _, err := ProbeHost(context.Background(), "   ", ProbeOptions{}); err == nil {
		t.Error("an empty host must be refused")
	}
}

func TestProbeHostHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := ProbeHost(ctx, "example.invalid", ProbeOptions{Timeout: time.Second})
	if err != nil {
		var priv *ErrPrivateDestination
		if errors.As(err, &priv) {
			t.Fatalf("unexpected private-address refusal: %v", err)
		}
		return
	}
	if res.OK {
		t.Error("a cancelled probe must not report success")
	}
}

func TestChecksAreNotExemptedFromTheEngine(t *testing.T) {
	cfg := config.NewConfig()

	if injected := cfg.MainInjectedMark(); markThroughEngine&injected != 0 {
		t.Fatalf("a check marked 0x%x is accepted before b4's queue (injected mark 0x%x), so it would report the unbypassed path",
			markThroughEngine, injected)
	}

	if flow := cfg.DiscoveryFlowMark(); markThroughEngine == flow {
		t.Fatalf("the discovery flow mark 0x%x only reaches a queue while a discovery run has installed its steering rules", flow)
	}
}
