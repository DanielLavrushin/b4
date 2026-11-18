package main

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestCollectedInstallRefusesWithoutTheQueueEngine(t *testing.T) {
	cfg := config.NewConfig()
	if err := collectedInstallRefusal(&cfg, false, false); err != nil {
		t.Fatalf("the NFQUEUE engine was refused: %v", err)
	}
	if collectedInstallRefusal(&cfg, false, true) == nil {
		t.Fatal("the collected installer ran under the TUN engine")
	}
	if collectedInstallRefusal(&cfg, true, false) == nil {
		t.Fatal("the collected installer ran with the packet engine down")
	}
	cfg.System.Tables.SkipSetup = true
	if collectedInstallRefusal(&cfg, false, false) == nil {
		t.Fatal("the collected installer ran under skip_setup")
	}
}
