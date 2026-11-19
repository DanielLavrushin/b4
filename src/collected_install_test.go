package main

import (
	"context"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tproxy"
)

func TestCollectedInstallRefusesWithoutTheQueueEngine(t *testing.T) {
	cfg := config.NewConfig()
	if err := collectedInstallRefusal(&cfg, false, false); err != nil {
		t.Fatalf("the NFQUEUE engine was refused: %v", err)
	}
	if collectedInstallRefusal(&cfg, false, true) == nil {
		t.Fatal("the collected installer ran under the TUN engine")
	}
	if collectedInstallRefusal(&cfg, true, false) == nil {
		t.Fatal("the collected installer ran with the packet engine down")
	}
	cfg.System.Tables.SkipSetup = true
	if collectedInstallRefusal(&cfg, false, false) == nil {
		t.Fatal("the collected installer ran under skip_setup")
	}
}

func collectedOnlyConsumer(t *testing.T, mode string) *config.Config {
	t.Helper()
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "consumer", "consumer", true
	set.Targets.UseCollectedIPs = true
	if mode == config.RoutingModeWSReuse {
		set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
		set.TCP.WSSizeReuse.Enabled = true
	} else {
		set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
		set.Routing.Upstream.Host, set.Routing.Upstream.Port = "192.0.2.10", 1080
	}
	cfg.Sets = []*config.SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return &cfg
}

func TestCollectedInstallerClearsTheLastAddressOfACollectedOnlyConsumer(t *testing.T) {
	for _, mode := range []string{config.RoutingModeWSReuse, config.RoutingModeProxy} {
		t.Run(mode, func(t *testing.T) {
			view := collectedOnlyConsumer(t, mode).WithCollectedIPs(nil)
			mgr := tproxy.NewManager(nil)
			t.Cleanup(mgr.Stop)
			var ran []string
			step := func(name string) { ran = append(ran, name) }
			install := collectedTargetsInstaller(mgr, collectedInstallSteps{
				refused: func(*config.Config) error { step("refused"); return nil },
				wait:    func(context.Context) error { step("wait"); return nil },
				refresh: func(*config.Config) error { step("refresh"); return nil },
				routing: func(*config.Config) { step("routing") },
				dscp:    func(*config.Config) { step("dscp") },
				ready:   func(*config.Config) error { step("ready"); return nil },
			})
			if err := install(context.Background(), view); err != nil {
				t.Fatalf("clearing the last collected address of a collected-only consumer was refused: %v", err)
			}
			if got := strings.Join(ran, ","); got != "refused,wait,refresh,routing,dscp,ready" {
				t.Fatalf("installer steps ran as %s", got)
			}
			if status := mgr.ListenerStatus("consumer", 0); status.V4 || status.V6 {
				t.Fatalf("a consumer with no target left kept a listener: %+v", status)
			}
		})
	}
}
