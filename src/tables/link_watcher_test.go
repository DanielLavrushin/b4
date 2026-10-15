package tables

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/josharian/native"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func buildIfInfoMsg(t *testing.T, flags uint32, ifname string) []byte {
	t.Helper()

	header := make([]byte, ifInfoMsgSize)
	native.Endian.PutUint32(header[8:12], flags)

	ae := netlink.NewAttributeEncoder()
	ae.String(unix.IFLA_IFNAME, ifname)
	attrs, err := ae.Encode()
	if err != nil {
		t.Fatalf("encode attrs: %v", err)
	}
	return append(header, attrs...)
}

func TestParseIfInfoMsg(t *testing.T) {
	t.Run("up interface", func(t *testing.T) {
		buf := buildIfInfoMsg(t, unix.IFF_UP, "eth0")
		name, up := parseIfInfoMsg(buf)
		if name != "eth0" {
			t.Errorf("name = %q, want %q", name, "eth0")
		}
		if !up {
			t.Errorf("up = false, want true")
		}
	})

	t.Run("down interface", func(t *testing.T) {
		buf := buildIfInfoMsg(t, 0, "wg0")
		name, up := parseIfInfoMsg(buf)
		if name != "wg0" {
			t.Errorf("name = %q, want %q", name, "wg0")
		}
		if up {
			t.Errorf("up = true, want false")
		}
	})

	t.Run("up combined with other flags", func(t *testing.T) {
		buf := buildIfInfoMsg(t, unix.IFF_UP|unix.IFF_RUNNING|unix.IFF_BROADCAST, "tun0")
		name, up := parseIfInfoMsg(buf)
		if name != "tun0" {
			t.Errorf("name = %q, want %q", name, "tun0")
		}
		if !up {
			t.Errorf("up = false, want true")
		}
	})

	t.Run("buffer shorter than header", func(t *testing.T) {
		name, up := parseIfInfoMsg(make([]byte, ifInfoMsgSize-1))
		if name != "" || up {
			t.Errorf("got (%q, %v), want empty", name, up)
		}
	})

	t.Run("header only, no attributes", func(t *testing.T) {
		header := make([]byte, ifInfoMsgSize)
		native.Endian.PutUint32(header[8:12], unix.IFF_UP)
		name, up := parseIfInfoMsg(header)
		if name != "" {
			t.Errorf("name = %q, want empty", name)
		}
		if !up {
			t.Errorf("up = false, want true")
		}
	})
}

func TestIsWatchedIface(t *testing.T) {
	cfg := &config.Config{
		Sets: []*config.SetConfig{
			{
				Enabled: true,
				Routing: config.RoutingConfig{
					Enabled:          true,
					EgressInterface:  "wg0",
					SourceInterfaces: []string{"br-lan"},
				},
			},
			{
				Enabled: true,
				Routing: config.RoutingConfig{
					Enabled:         true,
					EgressInterface: "tun0",
				},
			},
			{
				Enabled: false,
				Routing: config.RoutingConfig{
					Enabled:         true,
					EgressInterface: "disabled-set-iface",
				},
			},
			{
				Enabled: true,
				Routing: config.RoutingConfig{
					Enabled:         false,
					EgressInterface: "routing-off-iface",
				},
			},
			nil,
		},
	}

	cases := []struct {
		ifname string
		want   bool
	}{
		{"wg0", true},
		{"tun0", true},
		{"br-lan", false},
		{"eth0", false},
		{"disabled-set-iface", false},
		{"routing-off-iface", false},
		{"", false},
	}

	for _, tc := range cases {
		if got := isWatchedIface(cfg, tc.ifname); got != tc.want {
			t.Errorf("isWatchedIface(%q) = %v, want %v", tc.ifname, got, tc.want)
		}
	}
}

func watcherTestSet(t *testing.T) (*config.Config, *mockRouteBackend) {
	t.Helper()
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	gwStubCommands(t)
	stubBinaries(t, "ip")
	gwStubHost(t, map[string][]string{"lo": {"10.9.9.1/24"}})
	be := &mockRouteBackend{}
	routeEngine = be
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{gwTestSet("watched", "lo", "192.0.2.1")}
	RoutingSyncConfig(cfg)
	return cfg, be
}

func TestStopWaitsForAReinstallInFlightAndSkipsItsRebuild(t *testing.T) {
	cfg, be := watcherTestSet(t)
	before := gwCachedState(t, "watched")
	if before.gwV4 != "" {
		t.Fatalf("the gateway must start unusable, got %q", before.gwV4)
	}

	usable := gwHostIfaces(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	routeHostIfaces = func() (map[string]routeHostIface, error) {
		once.Do(func() { close(entered) })
		<-release
		return usable, nil
	}
	var rebuilds atomic.Int32
	be.ensureBaseFn = func() error {
		rebuilds.Add(1)
		return nil
	}

	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	w := newLinkWatcher(&ptr)
	w.scheduleReinstall("lo")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the scheduled reinstall never started")
	}

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		close(release)
		t.Fatal("Stop returned while the reinstall it had scheduled was still running, so that reinstall can rebuild routing after shutdown cleared it")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop never returned once the reinstall finished")
	}

	if n := rebuilds.Load(); n != 0 {
		t.Errorf("the reinstall saw its gateway become usable and rebuilt the sets %d times after Stop", n)
	}
	if after := gwCachedState(t, "watched"); after.gwV4 != "" {
		t.Errorf("the set was rebuilt after Stop with gateway %q", after.gwV4)
	}
}

func TestStopBeforeTheDebounceDropsTheReinstall(t *testing.T) {
	cfg, _ := watcherTestSet(t)
	var probes atomic.Int32
	routeHostIfaces = func() (map[string]routeHostIface, error) {
		probes.Add(1)
		return nil, nil
	}

	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	w := newLinkWatcher(&ptr)
	w.scheduleReinstall("lo")
	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(linkWatcherDebounce / 2):
		t.Fatal("Stop waited for a reinstall that had not started")
	}
	w.scheduleReinstall("lo")
	time.Sleep(linkWatcherDebounce + 100*time.Millisecond)
	if n := probes.Load(); n != 0 {
		t.Errorf("a reinstall ran %d times after Stop", n)
	}
}
