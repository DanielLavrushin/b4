package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

func TestDiscoveryPoolLeavesEscalationsAlone(t *testing.T) {
	m := metrics.GetMetricsCollector()
	previous := m.GetSnapshot().Escalations
	t.Cleanup(func() { m.UpdateEscalations(previous) })
	m.UpdateEscalations([]metrics.EscalationEntry{{Host: "main.example", ToSet: "main"}})

	cfg := config.NewConfig()
	cfg.Queue.IsDiscovery = true
	p := &Pool{Workers: []*Worker{NewWorkerWithQueue(&cfg, 0)}}
	p.publishEscalations()

	if got := m.GetSnapshot().Escalations; len(got) != 1 || got[0].Host != "main.example" {
		t.Fatalf("a discovery pool must not replace the main pool's escalations, got %+v", got)
	}

	main := config.NewConfig()
	p = &Pool{Workers: []*Worker{NewWorkerWithQueue(&main, 0)}}
	p.publishEscalations()
	if got := m.GetSnapshot().Escalations; len(got) != 0 {
		t.Fatalf("the main pool publishes its own escalations, got %+v", got)
	}
}
