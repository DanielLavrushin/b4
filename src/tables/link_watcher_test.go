package tables

import (
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
