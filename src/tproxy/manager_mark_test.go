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
