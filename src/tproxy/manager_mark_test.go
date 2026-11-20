package tproxy

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheListenerDropsAPinEqualToTheQueueMarksSetBits(t *testing.T) {
	set := config.NewSetConfig()
	set.Id = "listener-pin"
	set.Routing.FWMark = 0x100

	cfg := config.NewConfig()
	cfg.Queue.Mark = 0x8100
	if got, want := effectiveMark(&cfg, &set), MarkForSet(set.Id, 0); got != want {
		t.Errorf("queue mark 0x8100 with pin 0x100: the listener uses mark 0x%x, the firewall rules 0x%x", got, want)
	}
	cfg.Queue.Mark = 0x8000
	if got := effectiveMark(&cfg, &set); got != 0x100 {
		t.Errorf("a pin clear of the queue mark must be kept, got 0x%x", got)
	}
}

func TestAProxyMarkStepsOffTheDiscoveryMarksSetBits(t *testing.T) {
	set := config.NewSetConfig()
	set.Id = "discovery-pin"
	cfg := config.NewConfig()

	for _, pin := range []uint32{0x1, 0x2} {
		set.Routing.FWMark = pin
		if got, want := effectiveMark(&cfg, &set), MarkForSet(set.Id, 0); got != want {
			t.Errorf("pin 0x%x equals Discovery mark 0x%x or 0x%x under 0x%x, so Discovery's probes or the packets it sends would match the set's priority 2 rule; want the automatic mark 0x%x, got 0x%x", pin, cfg.DiscoveryFlowMark(), cfg.DiscoveryInjectedMark(), config.PerSetRouteMarkBits, want, got)
		}
	}

	set.Routing.FWMark = 0
	natural := MarkForSet(set.Id, 0)
	cfg.System.Checker.DiscoveryInjectedMark = uint(natural) | 0x100000
	got := MarkForConfig(&cfg, &set)
	if got == natural || !InMarkRange(got) || got == config.TelegramBridgeMark {
		t.Errorf("Discovery injected mark 0x%x has the set's automatic mark 0x%x under 0x%x; the set must move to another mark in the proxy range, got 0x%x", cfg.System.Checker.DiscoveryInjectedMark, natural, config.PerSetRouteMarkBits, got)
	}
}

func TestAnAutomaticMarkStepsOffTheQueueMarksSetBits(t *testing.T) {
	set := config.NewSetConfig()
	set.Id = "agree-set"
	natural := MarkForSet(set.Id, 0)

	cfg := config.NewConfig()
	cfg.Queue.Mark = uint(0x80000000 | natural)
	got := MarkForConfig(&cfg, &set)
	if got == natural {
		t.Errorf("queue mark 0x%x has 0x%x under 0x%x, the set's automatic mark, so every packet b4 injects follows the set's priority 3 rule into the local table", cfg.Queue.Mark, natural, config.PerSetRouteMarkBits)
	}
	if !InMarkRange(got) || got == config.TelegramBridgeMark {
		t.Errorf("the replacement mark 0x%x must stay in the proxy range and off the bridge mark", got)
	}

	cfg.Queue.Mark = 0x8000
	if got := MarkForConfig(&cfg, &set); got != natural {
		t.Errorf("with a queue mark clear of the set's mark the automatic mark must stay 0x%x, got 0x%x", natural, got)
	}
}
