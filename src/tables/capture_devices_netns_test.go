package tables

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

const netnsHookForward = 2

func TestNetnsTheDeviceFilterMatchesADeviceAddedByHandByItsAddress(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		for _, exclude := range []bool{false, true} {
			name := engine + map[bool]string{false: "/selected", true: "/excluded"}[exclude]
			t.Run(name, func(t *testing.T) {
				netnsRequire(t)
				if engine == backendNFTables && !hasBinary("nft") {
					t.Skip("nft is not installed")
				}
				t.Cleanup(setDSCPKeepSysctl("net/ipv4/ip_forward"))
				if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0o644); err != nil {
					t.Skipf("cannot turn forwarding on in this namespace: %v", err)
				}
				dev := netnsStartDevice(t)
				srv := netnsStartPeer(t, netnsSrvLink, netnsSrvPeer, netnsSrvRouter, netnsSrvIP)
				netnsEchoServerIn(t, srv)

				routeEngine = nil
				defer func() { routeEngine = nil }()

				cfg := netnsConfig(engine)
				cfg.Sets = nil
				cfg.Queue.Devices.Enabled = true
				cfg.Queue.Devices.WhiteIsBlack = exclude
				cfg.Queue.Devices.Devices = []config.Device{
					{MAC: "AA:BB:CC:DD:EE:01", Selected: true},
					{MAC: "02:B4:0A:C9:03:64", IP: netnsDevIP, Selected: true, IsManual: true},
				}
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules: %v", err)
				}
				defer func() { _ = ClearRules(cfg) }()
				if !(&Monitor{}).checkRules(cfg) {
					t.Errorf("the monitor finds rules missing right after they were installed, so it rebuilds them on every check")
				}

				snapshot, stop := netnsPacketQueueListener(t, uint16(cfg.Queue.StartNum))
				var forwarded net.Conn
				netnsInside(t, dev.pid, func() error {
					var err error
					forwarded, err = netnsDialServer()
					return err
				})
				port, _ := netnsEchoRoundTrip(t, func() (net.Conn, error) { return forwarded, nil }, "b4-added-by-hand")
				time.Sleep(300 * time.Millisecond)
				stop()

				queued := 0
				for _, q := range snapshot() {
					s, ok := netnsParseIPv4(q.pkt)
					if !ok || s.proto != unix.IPPROTO_TCP || s.sport != port || s.dport != netnsSrvPort {
						continue
					}
					if q.hook != netnsHookForward {
						t.Errorf("a packet of the device's connection was queued at hook %d, want %d (forward)", q.hook, netnsHookForward)
					}
					queued++
				}
				switch {
				case !exclude && queued == 0:
					t.Errorf("the device added by hand as %s is selected, yet none of its packets reached the queue: it has no MAC address the filter knows, only its address", netnsDevIP)
				case exclude && queued > 0:
					t.Errorf("the device added by hand as %s is excluded, yet %d of its packets reached the queue", netnsDevIP, queued)
				}
				t.Logf("%d packets of the device's connection queued", queued)
			})
		}
	}
}
