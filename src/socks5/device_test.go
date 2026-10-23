package socks5

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

const testClientMAC = "AA:BB:CC:00:00:01"

func withClientMAC(t *testing.T) chan string {
	t.Helper()
	metrics.SetMACProvider(func(ip string) string {
		if ip == "127.0.0.1" {
			return testClientMAC
		}
		return ""
	})
	t.Cleanup(func() { metrics.SetMACProvider(nil) })
	lines, _ := log.GetConnectionHub().Subscribe()
	t.Cleanup(func() { log.GetConnectionHub().Unsubscribe(lines) })
	return lines
}

func sessionLineMAC(t *testing.T, lines chan string, proto, domain string) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw := <-lines:
			f := strings.Split(raw, ",")
			if len(f) >= 10 && f[1] == proto && f[3] == domain && f[9] == "socks5" {
				return f[7]
			}
		case <-deadline:
			t.Fatalf("no %s session line for %s", proto, domain)
			return ""
		}
	}
}

func TestASessionLineCarriesTheMACTheDashboardCountsItUnder(t *testing.T) {
	lines := withClientMAC(t)
	up := &fakeUpstreams{handled: true, target: startGreeter(t, "via-upstream")}
	port := startUpstreamTestServer(t, up, torSet(config.RoutingModeProxy))

	if got, err := fetchThrough(t, port, "www.blocked.example", 443); err != nil || got != "via-upstream" {
		t.Fatalf("CONNECT through the set's upstream: %q, %v", got, err)
	}
	if mac := sessionLineMAC(t, lines, "TCP", "www.blocked.example"); mac != testClientMAC {
		t.Fatalf("session line MAC %q, want %q: the Traffic page groups a line by it, and without device mappings it has no other way to match the dashboard's device link", mac, testClientMAC)
	}
	listed := false
	for _, d := range metrics.GetMetricsCollector().Hello().Devices.Items {
		listed = listed || d.Key == testClientMAC
	}
	if !listed {
		t.Fatal("the dashboard lists the session under the same MAC")
	}
}

func TestAUDPSessionLineCarriesTheClientMAC(t *testing.T) {
	lines := withClientMAC(t)
	echoPort := startUDPEcho(t)
	_, addr := startTestServer(t, config.Socks5Config{UDPReadTimeout: 5})

	c, relay := associate(t, addr, [4]byte{0, 0, 0, 0}, 0)
	defer c.Close()
	u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if _, err := u.WriteToUDP(udpDatagram(echoPort, []byte("relayed")), relay); err != nil {
		t.Fatalf("send through relay: %v", err)
	}

	if mac := sessionLineMAC(t, lines, "UDP", "127.0.0.1"); mac != testClientMAC {
		t.Fatalf("UDP session line MAC %q, want %q", mac, testClientMAC)
	}
}
