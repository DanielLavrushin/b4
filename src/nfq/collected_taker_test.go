package nfq

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestCollectionAsksEveryConsumerTheFirewallApplies(t *testing.T) {
	plain := func(macs ...string) *config.SetConfig {
		set := readyConsumer(false)
		set.Id, set.Name = "plain", "plain"
		set.Targets.SourceDevices = macs
		return set
	}
	routed := func(macs ...string) *config.SetConfig {
		set := interfaceConsumer()
		set.Id, set.Name = "routed", "routed"
		set.Targets.SourceDevices = macs
		return set
	}
	for _, c := range []struct {
		name    string
		sets    []*config.SetConfig
		device  string
		collect bool
	}{
		{"a device-scoped plain consumer first, a routing consumer for every device later", []*config.SetConfig{plain(lifetimeOwnedMac), routed(), readyProducer()}, lifetimeOwnedMac, true},
		{"a routing consumer for every device first, a device-scoped plain consumer later", []*config.SetConfig{routed(), plain(lifetimeOwnedMac), readyProducer()}, lifetimeOwnedMac, true},
		{"a device only the routing consumer serves", []*config.SetConfig{plain(lifetimeOwnedMac), routed(), readyProducer()}, lifetimeOtherMac, true},
		{"a device no consumer serves", []*config.SetConfig{plain(lifetimeOwnedMac), routed(lifetimeOwnedMac), readyProducer()}, lifetimeOtherMac, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &lifetimeRig{collectCountRig: newCollectReadyRig(t, c.sets...)}
			r.resets = r.resetLog()
			logs := captureLog(t)
			r.device(c.device)
			r.silent(t, 42000)
			r.sweep(4 * time.Second)
			r.silent(t, 42001)
			r.sweep(5 * time.Second)
			refused := strings.Count(logs.String(), collectedRefusalLine)
			if c.collect && (r.collected() != 1 || r.installs != 1 || !slices.Equal(r.resets(), []uint16{42000, 42001}) || refused != 0) {
				t.Errorf("candidates=%d installs=%d resets=%v refusals=%d; the routing consumer takes the device's next connection in the firewall, so the address is collected and both silent connections are reset", r.collected(), r.installs, r.resets(), refused)
			}
			if !c.collect && (r.collected() != 0 || r.installs != 0 || len(r.resets()) != 0 || refused != 1) {
				t.Errorf("candidates=%d installs=%d resets=%v refusals=%d; no consumer takes this device's next connection, so the address must be refused once and nothing reset", r.collected(), r.installs, r.resets(), refused)
			}
		})
	}
}
