package tables

import (
	"net"
	"sync/atomic"
	"testing"

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
func buildNeighMsg(t *testing.T, state uint16, ifindex int, dst, mac []byte) []byte {
	t.Helper()
	header := make([]byte, ndMsgSize)
	native.Endian.PutUint32(header[4:8], uint32(ifindex))
	native.Endian.PutUint16(header[8:10], state)
	ae := netlink.NewAttributeEncoder()
	ae.Bytes(unix.NDA_DST, dst)
	if mac != nil {
		ae.Bytes(unix.NDA_LLADDR, mac)
	}
	attrs, err := ae.Encode()
	if err != nil {
		t.Fatalf("encode attrs: %v", err)
	}
	return append(header, attrs...)
}
func TestParseNeighMsg(t *testing.T) {
	lo, err := net.InterfaceByName("lo")
	if err != nil {
		t.Skip("no loopback interface to index the message with")
	}
	mac := []byte{0x02, 0x42, 0xac, 0x11, 0x00, 0x02}
	buf := buildNeighMsg(t, unix.NUD_REACHABLE, lo.Index, net.ParseIP("192.0.2.1").To4(), mac)
	ifname, ip, got, alive, verdict := parseNeighMsg(unix.RTM_NEWNEIGH, buf)
	if verdict != neighOK || !alive || ifname != "lo" || ip != "192.0.2.1" || got != "02:42:ac:11:00:02" {
		t.Errorf("parse = %q %q %q %v %v, want lo 192.0.2.1 02:42:ac:11:00:02 true ok", ifname, ip, got, alive, verdict)
	}
	buf = buildNeighMsg(t, unix.NUD_FAILED, lo.Index, net.ParseIP("192.0.2.1").To4(), nil)
	if _, _, _, alive, verdict := parseNeighMsg(unix.RTM_NEWNEIGH, buf); verdict != neighOK || alive {
		t.Error("a FAILED entry parses as dead, not alive: it must resync only when a MAC was cached")
	}
	buf = buildNeighMsg(t, unix.NUD_STALE, lo.Index, net.ParseIP("192.0.2.1").To4(), nil)
	if _, _, got, alive, verdict := parseNeighMsg(unix.RTM_NEWNEIGH, buf); verdict != neighOK || !alive || got != "" {
		t.Errorf("a usable entry without lladdr still resyncs, mac must be empty: %q %v %v", got, alive, verdict)
	}
	buf = buildNeighMsg(t, unix.NUD_NONE, lo.Index, net.ParseIP("192.0.2.1").To4(), nil)
	if _, _, _, _, verdict := parseNeighMsg(unix.RTM_NEWNEIGH, buf); verdict != neighFilteredState {
		t.Error("an uninteresting state must report filtered, not malformed")
	}
	buf = buildNeighMsg(t, unix.NUD_REACHABLE, lo.Index, net.ParseIP("192.0.2.1").To4(), mac)
	if _, _, _, alive, verdict := parseNeighMsg(unix.RTM_DELNEIGH, buf); verdict != neighOK || alive {
		t.Error("a deleted neighbor parses as dead even when the payload still says reachable")
	}
	if _, _, _, _, verdict := parseNeighMsg(unix.RTM_NEWNEIGH, []byte{1, 2, 3}); verdict != neighMalformed {
		t.Error("a truncated message must report malformed")
	}
}
func TestHandleNeighEventResyncsOnlyOnMACChange(t *testing.T) {
	var cfg config.Config
	set := config.NewSetConfig()
	set.Id = "neigh"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.EgressInterface = "eth1"
	set.Routing.EgressGateway = "192.0.2.1"
	cfg.Sets = []*config.SetConfig{&set}
	var ptr atomic.Pointer[config.Config]
	ptr.Store(&cfg)
	w := newLinkWatcher(&ptr)
	prevCache := routeRuleCache
	prevSeen := routeIfaceSeen
	routeRuleCache = map[string]routeState{"neigh": {setID: "neigh", iface: "eth1", egressGW: "192.0.2.1", gwMAC: "aa:bb:cc:dd:ee:ff"}}
	routeIfaceSeen = map[string]bool{}
	t.Cleanup(func() { routeRuleCache, routeIfaceSeen = prevCache, prevSeen })
	w.handleNeighEvent("eth1", "192.0.2.1", "aa:bb:cc:dd:ee:ff", true)
	w.debounceMu.Lock()
	pending := len(w.pendingIfaces)
	w.debounceMu.Unlock()
	if pending != 0 {
		t.Error("the same MAC must not reschedule a reinstall")
	}
	w.handleNeighEvent("eth1", "192.0.2.1", "aa:bb:cc:dd:ee:00", true)
	w.debounceMu.Lock()
	pending = len(w.pendingIfaces)
	_, iface := w.pendingIfaces["eth1"]
	w.debounceMu.Unlock()
	if pending != 1 || !iface {
		t.Error("a changed MAC must schedule a reinstall of the egress interface")
	}
	w.Stop()
}
func TestHandleNeighEventResyncsOnNeighborDeath(t *testing.T) {
	var cfg config.Config
	set := config.NewSetConfig()
	set.Id = "neigh"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.EgressInterface = "eth1"
	set.Routing.EgressGateway = "192.0.2.1"
	cfg.Sets = []*config.SetConfig{&set}
	var ptr atomic.Pointer[config.Config]
	ptr.Store(&cfg)
	w := newLinkWatcher(&ptr)
	prevCache := routeRuleCache
	routeRuleCache = map[string]routeState{"neigh": {setID: "neigh", iface: "eth1", egressGW: "192.0.2.1", gwMAC: "aa:bb:cc:dd:ee:ff"}}
	t.Cleanup(func() { routeRuleCache = prevCache })
	w.handleNeighEvent("eth1", "192.0.2.1", "", false)
	w.debounceMu.Lock()
	_, iface := w.pendingIfaces["eth1"]
	w.debounceMu.Unlock()
	if !iface {
		t.Error("a dead neighbor with a cached MAC must schedule a reinstall back to the address guard")
	}
	w.Stop()
	w = newLinkWatcher(&ptr)
	routeRuleCache = map[string]routeState{"neigh": {setID: "neigh", iface: "eth1", egressGW: "192.0.2.1"}}
	w.handleNeighEvent("eth1", "192.0.2.1", "", false)
	w.debounceMu.Lock()
	pending := len(w.pendingIfaces)
	w.debounceMu.Unlock()
	if pending != 0 {
		t.Error("a dead neighbor with no cached MAC changes nothing, so no reinstall")
	}
	w.Stop()
}
