package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func suiteWithQueue(ipVersion string, v4, v6 bool) *DiscoverySuite {
	return &DiscoverySuite{
		ipVersion: ipVersion,
		cfg: &config.Config{
			Queue: config.QueueConfig{IPv4Enabled: v4, IPv6Enabled: v6},
		},
	}
}

func TestDialNetworkFollowsQueueFamilies(t *testing.T) {
	tests := []struct {
		name      string
		ipVersion string
		v4, v6    bool
		want      string
	}{
		{"explicit ipv4 wins over queue", "ipv4", false, true, "tcp4"},
		{"explicit ipv6 wins over queue", "ipv6", true, false, "tcp6"},
		{"auto with default queue pins ipv4", "auto", true, false, "tcp4"},
		{"auto with ipv6-only queue pins ipv6", "auto", false, true, "tcp6"},
		{"auto with dual-stack queue stays unforced", "auto", true, true, ""},
		{"auto with no family enabled stays unforced", "auto", false, false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds := suiteWithQueue(tc.ipVersion, tc.v4, tc.v6)
			if got := ds.dialNetwork(); got != tc.want {
				t.Fatalf("dialNetwork() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDialNetworkWithoutConfigStaysUnforced(t *testing.T) {
	ds := &DiscoverySuite{ipVersion: "auto"}
	if got := ds.dialNetwork(); got != "" {
		t.Fatalf("dialNetwork() = %q, want %q", got, "")
	}
}

func TestDialContextOverridesRequestedFamily(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no IPv4 loopback available: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}

	ds := suiteWithQueue("auto", true, false)
	dial := ds.dialContext(2*time.Second, "blocked.example", "127.0.0.1")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := dial(ctx, "tcp6", net.JoinHostPort("blocked.example", port))
	if err != nil {
		t.Fatalf("dial with forced tcp4 failed: %v", err)
	}
	defer conn.Close()

	if _, ok := conn.RemoteAddr().(*net.TCPAddr); !ok {
		t.Fatalf("unexpected remote addr type %T", conn.RemoteAddr())
	}
	if got := conn.RemoteAddr().(*net.TCPAddr).IP.To4(); got == nil {
		t.Fatalf("expected IPv4 remote addr, got %s", conn.RemoteAddr())
	}
}
