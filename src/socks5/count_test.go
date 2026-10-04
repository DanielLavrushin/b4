package socks5

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
)

func countedSince(t *testing.T, before metrics.Totals) (conns, inSets uint64) {
	t.Helper()
	after := metrics.GetMetricsCollector().Totals()
	return after.Conns - before.Conns, after.ConnsInSets - before.ConnsInSets
}

func torConns60m(t *testing.T) uint64 {
	t.Helper()
	for _, s := range metrics.GetMetricsCollector().Hello().Sets {
		if s.ID == "tor" {
			return s.Conns60m
		}
	}
	t.Fatal("set tor is missing from the frame")
	return 0
}

func TestASessionThroughASetsUpstreamCountsOnceForThatSet(t *testing.T) {
	metrics.SetSetsProvider(func() ([]metrics.SetMeta, int) {
		return []metrics.SetMeta{{ID: "tor", Name: "TOR", Kind: metrics.SetKindProxy}}, 0
	})
	t.Cleanup(func() { metrics.SetSetsProvider(nil) })

	up := &fakeUpstreams{handled: true, target: startGreeter(t, "via-upstream")}
	port := startUpstreamTestServer(t, up, torSet(config.RoutingModeProxy))

	before, setBefore := metrics.GetMetricsCollector().Totals(), torConns60m(t)
	for i := 0; i < 2; i++ {
		got, err := fetchThrough(t, port, "www.blocked.example", 443)
		if err != nil || got != "via-upstream" {
			t.Fatalf("CONNECT through the set's upstream: %q, %v", got, err)
		}
	}

	if conns, inSets := countedSince(t, before); conns != 2 || inSets != 2 {
		t.Fatalf("two sessions the set's upstream carried are two connections in that set: conns %d, in sets %d", conns, inSets)
	}
	if got := torConns60m(t) - setBefore; got != 2 {
		t.Fatalf("the sessions belong to the set whose upstream carried them, got %d", got)
	}
}

func TestDirectSessionsAreLeftToTheQueue(t *testing.T) {
	direct := startGreeter(t, "direct")
	_, portStr, _ := net.SplitHostPort(direct)
	directPort, _ := strconv.Atoi(portStr)

	bypass := config.NewSetConfig()
	bypass.Id = "socks-bypass"
	bypass.Name = "Bypass"
	bypass.Enabled = true

	cases := []struct {
		name string
		up   *fakeUpstreams
		set  *config.SetConfig
	}{
		{"a name no set lists", &fakeUpstreams{handled: true}, torSet(config.RoutingModeProxy)},
		{"a name a bypass set lists", &fakeUpstreams{handled: true}, withDomain(&bypass, "localhost")},
		{"a proxy set whose upstream declines", &fakeUpstreams{}, withDomain(torSet(config.RoutingModeProxy), "localhost")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := startUpstreamTestServer(t, tc.up, tc.set)
			before := metrics.GetMetricsCollector().Totals()

			got, err := fetchThrough(t, port, "localhost", directPort)
			if err != nil || got != "direct" {
				t.Fatalf("CONNECT: %q, %v", got, err)
			}
			if conns, inSets := countedSince(t, before); conns != 0 || inSets != 0 {
				t.Fatalf("b4's own direct dial passes its queue, which counts it, so the session must not count again: conns %d, in sets %d", conns, inSets)
			}
		})
	}
}

func TestUDPRelayLeavesCountingToTheQueue(t *testing.T) {
	echoPort := startUDPEcho(t)
	_, addr := startTestServer(t, config.Socks5Config{UDPReadTimeout: 5})
	before := metrics.GetMetricsCollector().Totals()

	c, relay := associate(t, addr, [4]byte{0, 0, 0, 0}, 0)
	defer c.Close()
	u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()

	buf := make([]byte, 2048)
	for i := 0; i < 2; i++ {
		if _, err := u.WriteToUDP(udpDatagram(echoPort, []byte("relayed")), relay); err != nil {
			t.Fatalf("send through relay: %v", err)
		}
		_ = u.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, _, err := u.ReadFromUDP(buf); err != nil {
			t.Fatalf("echo through relay: %v", err)
		}
	}

	if conns, inSets := countedSince(t, before); conns != 0 || inSets != 0 {
		t.Fatalf("the relay dials directly through b4's queue, which counts the flow: conns %d, in sets %d", conns, inSets)
	}
}
