package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func TestAnInactiveReuseConsumerOwnsCollectionOnlyAsAQueueConsumerWould(t *testing.T) {
	published := func(string) (tables.CollectedRoute, bool) {
		return tables.CollectedRoute{Port: 443, IPv4: true, IPv6: true}, true
	}
	none := func(string) (tables.CollectedRoute, bool) { return tables.CollectedRoute{}, false }
	for _, c := range []struct {
		name   string
		reuse  bool
		mark   uint
		routes func(string) (tables.CollectedRoute, bool)
		want   bool
	}{
		{"a queue consumer whose address a rival names", false, 0, none, false},
		{"an active reuse consumer with no rules yet", true, 0, none, true},
		{"a reuse consumer the queue mark makes inactive", true, 0x48000, none, false},
		{"a reuse consumer the queue mark makes inactive, with a stale published route", true, 0x48000, published, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			consumer := readyConsumer(false)
			consumer.Id = "consumer"
			consumer.TCP.WSSizeReuse.Enabled = c.reuse
			cfg := config.NewConfig()
			if c.mark != 0 {
				cfg.Queue.Mark = c.mark
			}
			cfg.Sets = []*config.SetConfig{readyProducer(), consumer}
			if c.reuse && cfg.WSReuseActive(consumer) != (c.mark == 0) {
				t.Fatalf("setup: queue mark 0x%x leaves reuse active=%v", c.mark, cfg.WSReuseActive(consumer))
			}
			view := cfg.WithCollectedIPs([]string{"1.2.3.4"})
			stubCollectedRoutes(t, c.routes)
			h := helloWatch{dst: net.ParseIP("1.2.3.4").To4(), port: 443, host: "i.ytimg.com", tls: 0x0303, ver: IPv4}
			if _, owned := collectedOwner(view, buildMatcher(view), h); owned != c.want {
				t.Errorf("%s: collectedOwner says owned=%v, want %v; while reuse does nothing the set's connections get its usual strategy, so it owns collection as any queue consumer does, through the address match and the rival-name test", c.name, owned, c.want)
			}
		})
	}
}
