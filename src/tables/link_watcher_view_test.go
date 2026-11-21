package tables

import (
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestALinkReinstallKeepsTheCollectedAddressesOfAConsumer(t *testing.T) {
	const collected = "203.0.113.7"
	for _, c := range []struct {
		name   string
		static []string
	}{
		{"collected targets only", nil},
		{"collected and static targets", []string{"198.51.100.70"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			familyResetGlobals(t)
			stubRetryState(t)
			gwIsolate(t)
			gwStubCommands(t)
			run = refuseDeletes(run)
			stubBinaries(t, "ip")
			gwStubHost(t, map[string][]string{"lo": {"10.9.9.1/24"}})
			held := routeCTMarkHeld.Load()
			routeCTMarkHeld.Store(false)
			t.Cleanup(func() { routeCTMarkHeld.Store(held) })
			var deleted []string
			be := &mockRouteBackend{delElementsFn: func(setName string, ips []string) {
				if slices.Contains(ips, collected) {
					deleted = append(deleted, setName)
				}
			}}
			routeEngine = be

			cfg := familyTestConfig(true, false)
			cfg.System.Tables.Engine = backendNFTables
			consumer := gwTestSet("consumer", "lo", "")
			consumer.Routing.FWMark = 0x7e20
			consumer.Routing.Table = 233
			consumer.Targets.IpsToMatch = c.static
			consumer.Targets.UseCollectedIPs = true
			cfg.Sets = []*config.SetConfig{gwTestSet("watched", "lo", "192.0.2.1"), consumer}
			view := func(c *config.Config) *config.Config { return c.WithCollectedIPs([]string{collected}) }

			RoutingSyncConfig(view(cfg))
			gwCachedState(t, "consumer")
			if before := gwCachedState(t, "watched"); before.gwV4 != "" {
				t.Fatalf("the gateway must start unusable, got %q", before.gwV4)
			}

			usable := gwHostIfaces(t, map[string][]string{"lo": {"192.0.2.10/24"}})
			routeHostIfaces = func() (map[string]routeHostIface, error) { return usable, nil }

			var ptr atomic.Pointer[config.Config]
			ptr.Store(cfg)
			m := NewMonitor(&ptr, view)
			m.linkWatcher.scheduleReinstall("lo")
			m.linkWatcher.reinstalls.Wait()

			if after := gwCachedState(t, "watched"); after.gwV4 == "" {
				t.Fatal("the reinstall did not rebuild the sets once the gateway became usable, so this test covers nothing")
			}
			routeMu.Lock()
			_, installed := routeRuleCache["consumer"]
			routeMu.Unlock()
			if !installed {
				t.Errorf("the link watcher's rebuild removed the collected consumer: it synced the saved config, which carries no collected addresses")
			}
			if len(deleted) > 0 {
				t.Errorf("the link watcher's rebuild deleted the collected address %s from %s", collected, strings.Join(deleted, ", "))
			}
			synced := routingSyncedConfig()
			if synced == nil || synced.GetSetById("consumer") == nil || !slices.Contains(synced.GetSetById("consumer").Targets.CollectedIPs, collected) {
				t.Errorf("after the link watcher's rebuild the synced config no longer lists %s as a collected address of the consumer, so the monitor keeps the loss", collected)
			}
		})
	}
}
