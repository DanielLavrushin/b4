package main

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
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
				refused:   func(*config.Config) error { step("refused"); return nil },
				discovery: func() bool { step("discovery"); return false },
				refresh:   func(*config.Config) error { step("refresh"); return nil },
				capture:   func(*config.Config) error { step("capture"); return nil },
				routing:   func(*config.Config) { step("routing") },
				dscp:      func(*config.Config) { step("dscp") },
				ready:     func(*config.Config) error { step("ready"); return nil },
			})
			if err := install(context.Background(), view, func() bool { return true }); err != nil {
				t.Fatalf("clearing the last collected address of a collected-only consumer was refused: %v", err)
			}
			if got := strings.Join(ran, ","); got != "refused,discovery,refresh,capture,routing,dscp,ready" {
				t.Fatalf("installer steps ran as %s", got)
			}
			if status := mgr.ListenerStatus("consumer", 0); status.V4 || status.V6 {
				t.Fatalf("a consumer with no target left kept a listener: %+v", status)
			}
		})
	}
}

func TestCollectedInstallerRefusesWhileAConsumerListenerIsDown(t *testing.T) {
	for _, mode := range []string{config.RoutingModeWSReuse, config.RoutingModeProxy} {
		t.Run(mode, func(t *testing.T) {
			view := collectedOnlyConsumer(t, mode).WithCollectedIPs([]string{"198.51.100.7"})
			port := tproxy.PortFor(tproxy.MarkForConfig(view, view.Sets[0]))
			if taken, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port))); err == nil {
				t.Cleanup(func() { taken.Close() })
			}
			mgr := tproxy.NewManager(nil)
			t.Cleanup(mgr.Stop)
			install := collectedTargetsInstaller(mgr, collectedInstallSteps{
				refused:   func(*config.Config) error { return nil },
				discovery: func() bool { return false },
				refresh:   func(*config.Config) error { return nil },
				capture:   func(*config.Config) error { return nil },
				routing:   func(*config.Config) {},
				dscp:      func(*config.Config) {},
				ready:     func(*config.Config) error { return nil },
			})
			err := install(context.Background(), view, func() bool { return true })
			if status := mgr.ListenerStatus("consumer", 0); status.V4 {
				t.Skipf("the consumer listener started on port %d, so this host cannot show a listener that is down", port)
			}
			if err == nil || !strings.Contains(err.Error(), "listener") {
				t.Fatalf("the install of a collected IPv4 address was accepted while the consumer had no IPv4 listener to take its connections (err %v); the firewall then diverts them to a port nothing listens on", err)
			}
		})
	}
}

func TestCollectedInstallerFailsWhileTheCaptureSetsLagThePool(t *testing.T) {
	view := collectedOnlyConsumer(t, config.RoutingModeProxy).WithCollectedIPs(nil)
	mgr := tproxy.NewManager(nil)
	t.Cleanup(mgr.Stop)
	var ran []string
	step := func(name string) { ran = append(ran, name) }
	refused := errors.New("collected capture sets: b4_mss_0_v4c kept 198.51.100.7")
	install := collectedTargetsInstaller(mgr, collectedInstallSteps{
		refused:   func(*config.Config) error { step("refused"); return nil },
		discovery: func() bool { step("discovery"); return false },
		refresh:   func(*config.Config) error { step("refresh"); return nil },
		capture:   func(*config.Config) error { step("capture"); return refused },
		routing:   func(*config.Config) { step("routing") },
		dscp:      func(*config.Config) { step("dscp") },
		ready:     func(*config.Config) error { step("ready"); return nil },
	})
	if err := install(context.Background(), view, func() bool { return true }); !errors.Is(err, refused) {
		t.Fatalf("a capture-set sync the kernel refused must fail the install so that it is retried, got %v", err)
	}
	if got := strings.Join(ran, ","); got != "refused,discovery,refresh,capture" {
		t.Fatalf("installer steps ran as %s", got)
	}
}

func recordingInstallSteps(ran *[]string, discovery bool) collectedInstallSteps {
	step := func(name string) { *ran = append(*ran, name) }
	return collectedInstallSteps{
		refused:   func(*config.Config) error { step("refused"); return nil },
		discovery: func() bool { step("discovery"); return discovery },
		refresh:   func(*config.Config) error { step("refresh"); return nil },
		capture:   func(*config.Config) error { step("capture"); return nil },
		routing:   func(*config.Config) { step("routing") },
		dscp:      func(*config.Config) { step("dscp") },
		ready:     func(*config.Config) error { step("ready"); return nil },
	}
}

func TestTheCollectedInstallerFailsAtOnceWhileDiscoveryRuns(t *testing.T) {
	view := collectedOnlyConsumer(t, config.RoutingModeProxy).WithCollectedIPs([]string{"198.51.100.7"})
	mgr := tproxy.NewManager(nil)
	t.Cleanup(mgr.Stop)
	var ran []string
	install := collectedTargetsInstaller(mgr, recordingInstallSteps(&ran, true))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := install(ctx, view, func() bool { return true })
	if took := time.Since(start); err == nil || took > time.Second {
		t.Fatalf("a collected install while Discovery runs must fail at once, got err %v after %v", err, took)
	}
	if got := strings.Join(ran, ","); got != "refused,discovery" {
		t.Fatalf("installer steps ran as %s while Discovery runs", got)
	}
}

func TestACancelledCollectedInstallChangesNothing(t *testing.T) {
	view := collectedOnlyConsumer(t, config.RoutingModeProxy).WithCollectedIPs([]string{"198.51.100.7"})
	mgr := tproxy.NewManager(nil)
	t.Cleanup(mgr.Stop)
	var ran []string
	install := collectedTargetsInstaller(mgr, recordingInstallSteps(&ran, false))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := install(ctx, view, func() bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled install must fail with its context, got %v", err)
	}
	if got := strings.Join(ran, ","); got != "refused,discovery" {
		t.Fatalf("a cancelled install ran the steps %s", got)
	}
}

func TestTheCollectedInstallerChecksItsViewBeforeEachChange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current int
		want    string
	}{
		{name: "current throughout", current: 5, want: "refused,discovery,refresh,current,capture,current,current,routing,current,dscp,current,ready"},
		{name: "superseded after the capture sets", current: 1, want: "refused,discovery,refresh,current,capture,current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := collectedOnlyConsumer(t, config.RoutingModeProxy).WithCollectedIPs(nil)
			mgr := tproxy.NewManager(nil)
			t.Cleanup(mgr.Stop)
			var ran []string
			install := collectedTargetsInstaller(mgr, recordingInstallSteps(&ran, false))
			checks := 0
			err := install(context.Background(), view, func() bool {
				ran = append(ran, "current")
				checks++
				return checks <= tc.current
			})
			if got := strings.Join(ran, ","); got != tc.want {
				t.Fatalf("installer steps ran as %s, want %s", got, tc.want)
			}
			if superseded := checks > tc.current; superseded != errors.Is(err, errCollectedSuperseded) {
				t.Fatalf("superseded=%v but the install returned %v", superseded, err)
			}
		})
	}
}

func TestAPendingFirewallRefreshFailsTheCollectedInstallBeforeAnyChange(t *testing.T) {
	view := collectedOnlyConsumer(t, config.RoutingModeProxy).WithCollectedIPs(nil)
	mgr := tproxy.NewManager(nil)
	t.Cleanup(mgr.Stop)
	var ran []string
	steps := recordingInstallSteps(&ran, false)
	steps.refresh = func(*config.Config) error {
		ran = append(ran, "refresh")
		return tables.ErrFirewallRefreshPending
	}
	install := collectedTargetsInstaller(mgr, steps)
	err := install(context.Background(), view, func() bool {
		ran = append(ran, "current")
		return true
	})
	if !errors.Is(err, tables.ErrFirewallRefreshPending) {
		t.Fatalf("a pending refresh must fail the install, got %v", err)
	}
	if got := strings.Join(ran, ","); got != "refused,discovery,refresh" {
		t.Fatalf("a pending refresh must fail the install before its first change, so no rollback follows; ran %s", got)
	}
}
