package discovery

import (
	"github.com/daniellavrushin/b4/config"
	"testing"
)

func TestCollectedProducerIsNotAProbeStrategy(t *testing.T) {
	set := config.NewSetConfig()
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.Action = "collect"
	set.Targets.UseCollectedIPs = true
	strategy := SetRunStrategy(&set)
	if strategy.TCP.IPBlockDetect.Enabled || strategy.Targets.UseCollectedIPs || strategy.TCP.IPBlockDetect.Action != "block" {
		t.Fatal("local collection leaked into Discovery")
	}
	if !set.TCP.IPBlockDetect.Collects() {
		t.Fatal("source collection settings changed")
	}
	set.TCP.IPBlockDetect.Action = "block"
	if !SetRunStrategy(&set).TCP.IPBlockDetect.Enabled {
		t.Fatal("legacy Block probe behavior changed")
	}
}
