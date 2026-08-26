package discovery

import (
	"errors"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestStartRefusesTUNMode(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"

	rt := NewRuntime()
	res, err := rt.Start(&cfg)
	if res != nil {
		t.Fatal("no discovery pool must be created in TUN mode")
	}
	if !errors.Is(err, ErrDiscoveryNeedsNFQueue) {
		t.Fatalf("expected ErrDiscoveryNeedsNFQueue, got %v", err)
	}
	if rt.IsActive() {
		t.Error("a refused start must not leave the runtime active")
	}
}

func TestStartRefusesSkipTables(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = true

	rt := NewRuntime()
	if _, err := rt.Start(&cfg); !errors.Is(err, ErrDiscoverySkipTables) {
		t.Fatalf("expected ErrDiscoverySkipTables, got %v", err)
	}
	if rt.IsActive() {
		t.Error("a refused start must not leave the runtime active")
	}
}
