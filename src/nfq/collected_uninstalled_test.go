package nfq

import (
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func TestAConsumerNotInstalledYetStillCollectsItsFirstAddress(t *testing.T) {
	reuse := readyConsumer(false)
	reuse.Id, reuse.Name = "reuse", "reuse"
	reuse.TCP.WSSizeReuse.Enabled = true
	proxy := readyConsumer(false)
	proxy.Id, proxy.Name = "proxy", "proxy"
	proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
	routed := interfaceConsumer()
	routed.Name = "interface"
	for _, consumer := range []*config.SetConfig{routed, proxy, reuse} {
		t.Run(consumer.Name, func(t *testing.T) {
			r := &lifetimeRig{collectCountRig: newCollectReadyRig(t, readyProducer(), consumer)}
			r.resets = r.resetLog()
			stubCollectedRoutes(t, func(string) (tables.CollectedRoute, bool) { return tables.CollectedRoute{}, false })
			r.silent(t, 42000)
			r.sweep(4 * time.Second)
			r.silent(t, 42001)
			r.sweep(5 * time.Second)
			if r.collected() != 1 || r.installs != 1 || !slices.Equal(r.resets(), []uint16{42000, 42001}) {
				t.Errorf("a %s consumer with only collected addresses has no rules before its first address, yet the install would give it them: candidates=%d installs=%d resets=%v", consumer.Name, r.collected(), r.installs, r.resets())
			}
		})
	}
}
