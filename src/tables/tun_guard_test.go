package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestMasqueradeSpecsExemptsTUNDevice(t *testing.T) {
	cfg := &config.Config{}
	cfg.Queue.Mode = "tun"
	cfg.Queue.TUN.DeviceName = "b4tun0"

	specs := masqueradeSpecs(cfg)
	if len(specs) != 2 {
		t.Fatalf("expected a RETURN plus a MASQUERADE, got %v", specs)
	}
	if specs[0][0] != "-o" || specs[0][1] != "b4tun0" || specs[0][3] != "RETURN" {
		t.Errorf("first spec must exempt the tun device, got %v", specs[0])
	}
	if specs[1][1] != "MASQUERADE" {
		t.Errorf("second spec must still masquerade, got %v", specs[1])
	}
}

func TestMasqueradeSpecsUnchangedForNFQueue(t *testing.T) {
	cfg := &config.Config{}
	specs := masqueradeSpecs(cfg)
	if len(specs) != 1 || specs[0][0] != "-j" || specs[0][1] != "MASQUERADE" {
		t.Fatalf("nfqueue mode must keep the bare masquerade rule, got %v", specs)
	}

	cfg.System.Tables.Masquerade.Interfaces = []string{"eth0", "ppp0"}
	specs = masqueradeSpecs(cfg)
	if len(specs) != 2 || specs[0][1] != "eth0" || specs[1][1] != "ppp0" {
		t.Fatalf("nfqueue mode with an interface list must be unchanged, got %v", specs)
	}
}

func TestRouteClaimedMarkMatch(t *testing.T) {
	if got := RouteClaimedMarkMatch(); got != "0x0/0x27fff" {
		t.Errorf("RouteClaimedMarkMatch = %s, want 0x0/0x27fff", got)
	}
	if RouteClaimedMarkMask() != routeSetMarkMask {
		t.Error("the exported mask must be the per-set routing mask")
	}
}

func TestRoutingKeeperPrimesBeforeActing(t *testing.T) {
	cfg := &config.Config{}
	k := NewRoutingKeeper()
	if k.primed {
		t.Fatal("a fresh keeper must not claim to be primed")
	}
	k.Reconcile(cfg)
	if !k.primed {
		t.Error("the first reconcile must only take a snapshot")
	}
}

func TestRoutingKeeperSkipsWhenTablesSkipped(t *testing.T) {
	cfg := &config.Config{}
	cfg.System.Tables.SkipSetup = true
	k := NewRoutingKeeper()
	k.Reconcile(cfg)
	if k.primed {
		t.Error("--skip-tables must leave the keeper inert")
	}
}
