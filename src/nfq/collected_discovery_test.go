package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

func TestADiscoveryPoolLeavesTheCollectedListAlone(t *testing.T) {
	mc := metrics.GetMetricsCollector()
	mc.UpdateCollectedIPs([]metrics.CollectedEntry{{IP: "203.0.113.9", Host: "collected.example", Set: "producer", Since: 1}})
	t.Cleanup(func() { mc.UpdateCollectedIPs(nil) })
	cfg := config.NewConfig()
	cfg.Queue.IsDiscovery = true
	cfg.ConfigPath = t.TempDir() + "/b4.json"
	pool := NewPool(&cfg)
	t.Cleanup(pool.Stop)
	fr := mc.Hello()
	if fr.Collected == nil || len(fr.Collected.Items) != 1 {
		t.Fatal("a Discovery pool emptied the dashboard's collected list")
	}
}
