package tproxy

import (
	"context"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func collectedConsumer(t *testing.T, mode, version string, v6 bool, static []string) *config.Config {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IPv6Enabled = v6
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "consumer", "consumer", true
	set.Targets.UseCollectedIPs = true
	set.Targets.IPVersion = version
	set.Targets.IPs = static
	set.Targets.IpsToMatch = static
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

func runningConsumerListener(t *testing.T, m *Manager) context.Context {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.mu.Lock()
	m.listeners["consumer"] = &Listener{SetID: "consumer", SetName: "consumer", lnV4: ln, ctx: ctx, cancel: cancel}
	m.mu.Unlock()
	return ctx
}

func TestCollectedConsumersReadyFollowsDiversion(t *testing.T) {
	cases := []struct {
		name      string
		mode      string
		version   string
		v6        bool
		pool      []string
		static    []string
		listener  bool
		wantReady bool
	}{
		{"reuse consumer, pool emptied", config.RoutingModeWSReuse, "", false, nil, nil, false, true},
		{"proxy consumer, pool emptied", config.RoutingModeProxy, "", false, nil, nil, false, true},
		{"v6-only consumer, v4 address collected", config.RoutingModeWSReuse, "6", true, []string{"198.51.100.7"}, nil, false, true},
		{"v4-only consumer, v6 address collected", config.RoutingModeProxy, "4", true, []string{"2001:db8::7"}, nil, false, true},
		{"dual-stack queue, v4 address, v4 listener only", config.RoutingModeWSReuse, "", true, []string{"198.51.100.7"}, nil, true, true},
		{"static v6 target, v4 address, v4 listener only", config.RoutingModeWSReuse, "", true, []string{"198.51.100.7"}, []string{"2001:db8::9"}, true, true},
		{"v4 address, no listener", config.RoutingModeWSReuse, "", false, []string{"198.51.100.7"}, nil, false, false},
		{"v6 address, v4 listener only", config.RoutingModeWSReuse, "", true, []string{"2001:db8::7"}, nil, true, false},
		{"static v4 target, v6 address, v4 listener only", config.RoutingModeWSReuse, "", true, []string{"2001:db8::7"}, []string{"198.51.100.9"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := collectedConsumer(t, tc.mode, tc.version, tc.v6, tc.static).WithCollectedIPs(tc.pool)
			m := NewManager(nil)
			t.Cleanup(m.Stop)
			if tc.listener {
				runningConsumerListener(t, m)
			} else if len(tc.pool) == 0 {
				m.SyncConfig(view)
			}
			err := m.CollectedConsumersReady(view)
			if (err == nil) != tc.wantReady {
				t.Fatalf("CollectedConsumersReady = %v, want ready=%v", err, tc.wantReady)
			}
		})
	}
}

func TestCollectedConsumersReadyAfterTheInstallerStopsTheListener(t *testing.T) {
	for _, mode := range []string{config.RoutingModeWSReuse, config.RoutingModeProxy} {
		t.Run(mode, func(t *testing.T) {
			base := collectedConsumer(t, mode, "", false, nil)
			with := base.WithCollectedIPs([]string{"198.51.100.7"})
			without := base.WithCollectedIPs(nil)
			m := NewManager(nil)
			t.Cleanup(m.Stop)
			ctx := runningConsumerListener(t, m)
			if err := m.CollectedConsumersReady(with); err != nil {
				t.Fatalf("a running consumer with one collected address is not ready: %v", err)
			}
			m.SyncConfig(without)
			if ctx.Err() == nil {
				t.Fatal("the installer's sync left the consumer listener running after its last address went")
			}
			if err := m.CollectedConsumersReady(without); err != nil {
				t.Fatalf("clearing the last collected address was refused: %v", err)
			}
		})
	}
}
