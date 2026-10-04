package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func recordDNSTCPReady(t *testing.T) *[][2]bool {
	t.Helper()
	var calls [][2]bool
	orig := DNSTCPReadyFunc
	DNSTCPReadyFunc = func(v4, v6 bool) { calls = append(calls, [2]bool{v4, v6}) }
	t.Cleanup(func() { DNSTCPReadyFunc = orig })
	return &calls
}

func TestDiscoveryPoolLeavesDNSTCPReadinessAlone(t *testing.T) {
	calls := recordDNSTCPReady(t)

	cfg := config.NewConfig()
	cfg.Queue.IsDiscovery = true
	p := &Pool{Workers: []*Worker{NewWorkerWithQueue(&cfg, 0)}}

	p.publishDNSTCPReady()
	p.reconcileDNSTCP(&cfg)

	if len(*calls) != 0 {
		t.Fatalf("a discovery pool has no DNS TCP listener and must not report the main pool's listener as down, got %v", *calls)
	}
}

func TestMainPoolReportsDNSTCPReadiness(t *testing.T) {
	calls := recordDNSTCPReady(t)

	cfg := config.NewConfig()
	p := &Pool{Workers: []*Worker{NewWorkerWithQueue(&cfg, 0)}}

	p.reconcileDNSTCP(&cfg)

	if len(*calls) != 1 || (*calls)[0] != [2]bool{false, false} {
		t.Fatalf("the main pool without a listener reports both families down, got %v", *calls)
	}
}
