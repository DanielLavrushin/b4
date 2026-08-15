package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func selfMarkConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Queue.Mark = 0x8000
	return cfg
}

func TestSelfInjectedMark_QueueMarkAlone(t *testing.T) {
	if !selfInjectedMark(0x8000, 0x8000, selfMarkConfig()) {
		t.Error("a packet carrying exactly the queue mark is one b4 injected")
	}
}

func TestSelfInjectedMark_QueueMarkPlusRoutingMark(t *testing.T) {
	cfg := selfMarkConfig()
	for _, m := range []uint32{0x8000 | 0x2bd8, 0x8000 | 0x100, 0x8000 | 0x24247} {
		if !selfInjectedMark(m, 0x8000, cfg) {
			t.Errorf("mark 0x%x is an injected packet a set's routing chain marked; dispatching it feeds b4 its own output", m)
		}
	}
}

func TestSelfInjectedMark_ClientTraffic(t *testing.T) {
	cfg := selfMarkConfig()
	for _, m := range []uint32{0, 0x1, 0x2bd8, 0x10000, 0x18000} {
		if selfInjectedMark(m, 0x8000, cfg) {
			t.Errorf("mark 0x%x does not carry b4's queue mark and per-set bits only; accepting it would let traffic past the engine", m)
		}
	}
}

func TestSelfInjectedMark_DiscoveryMarksStayProcessed(t *testing.T) {
	cfg := selfMarkConfig()
	cfg.System.Checker.DiscoveryFlowMark = 0x8001
	cfg.System.Checker.DiscoveryInjectedMark = 0x8002

	if selfInjectedMark(0x8001, 0x8000, cfg) {
		t.Error("the discovery flow mark shares the queue bit; treating it as injected would take discovery's traffic out of the engine")
	}
	if selfInjectedMark(0x8002, 0x8000, cfg) {
		t.Error("the discovery injected mark shares the queue bit and must keep its own handling")
	}
}

func TestSelfInjectedMark_DerivedDiscoveryMarks(t *testing.T) {
	cfg := selfMarkConfig()
	if selfInjectedMark(uint32(cfg.DiscoveryFlowMark()), 0x8000, cfg) {
		t.Error("the derived discovery flow mark must not read as injected")
	}
	if selfInjectedMark(uint32(cfg.DiscoveryInjectedMark()), 0x8000, cfg) {
		t.Error("the derived discovery injected mark must not read as injected")
	}
}

func TestSelfInjectedMark_DiscoveryWorkerSeesItsOwn(t *testing.T) {
	cfg := selfMarkConfig()
	cfg.System.Checker.DiscoveryFlowMark = 0x8001
	cfg.System.Checker.DiscoveryInjectedMark = 0x8002

	if !selfInjectedMark(0x8002, 0x8002, cfg) {
		t.Error("the discovery worker runs with the discovery injected mark as its queue mark and must skip its own packets")
	}
	if selfInjectedMark(0x8001, 0x8002, cfg) {
		t.Error("the discovery worker must still process the flow it is testing")
	}
}
