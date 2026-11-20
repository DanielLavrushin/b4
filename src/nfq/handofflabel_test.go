package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
)

func TestAConnectionSeenLeavingByAnotherInterfaceIsLoggedWithThatInterfaceNotTheSetsRoute(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	hotspot := keeneticHotspotMark
	for _, c := range []struct {
		name string
		mark *uint32
		out  uint32
		want string
		why  string
	}{
		{"a KeeneticOS policy device leaving by the uplink", &hotspot, idx["eth7"], "reroute->eth7", "the connection was seen leaving by eth7, not through the set's tunnel"},
		{"a KeeneticOS policy device leaving through another tunnel", &hotspot, idx["tunt0"], "tunnel->tunt0", "the connection was seen entering tunt0, not the set's tunnel"},
		{"a KeeneticOS policy device leaving through the set's tunnel", &hotspot, idx["wgt0"], "routed->wgt0", "the connection leaves by the interface the set routes to"},
		{"a device without a mark leaving through the set's tunnel", nil, idx["wgt0"], "routed->wgt0", "the connection leaves by the interface the set routes to"},
		{"a device without a mark and no egress interface", nil, 0, "routed->wgt0", "nothing was seen that contradicts the set's route"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stubInjectRoute(t, idx["eth7"], true)
			set := routedIntoWgt0(newFragmentingYTImagesSet())
			if !set.RoutingHandsOffPackets() {
				t.Fatal("the set must hand its packets to wgt0")
			}
			w := newInjectingWorker(t, &set)
			sent := captureInjected(t, w)
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			if got := queuedVerdict(t, w, ytimgHello(), c.mark, c.out, 0); got != nfqueue.NfAccept {
				t.Fatalf("verdict %d, want the ClientHello accepted unchanged: only the label may depend on where the connection was seen leaving", got)
			}
			if n := len(sent()); n != 0 {
				t.Fatalf("%d packets injected, want none: the connection is handed off either way", n)
			}
			if reasons := connectionReasons(ch); len(reasons) != 1 || reasons[0] != c.want {
				t.Errorf("connection log flags %q, want [%s]: %s", reasons, c.want, c.why)
			}
		})
	}
}

func TestAConnectionLeavingByTheSetsOwnTunnelThatCarriesTheDefaultRouteKeepsTheRoutedLabel(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(
		"eth7\t00000000\t0101A8C0\t0003\t0\t0\t20\t00000000\t0\t0\t0",
		"wgdef0\t00000000\t00000000\t0001\t0\t0\t10\t00000000\t0\t0\t0",
	))
	hotspot := keeneticHotspotMark
	stubInjectRoute(t, idx["wgdef0"], true)
	set := routedIntoWgt0(newFragmentingYTImagesSet())
	set.Routing.EgressInterface = "wgdef0"
	other := routedIntoWgt0(newFragmentingYTImagesSet())
	other.Id = "other"
	other.Name = "Other"
	other.Targets.DomainsToMatch = []string{"example.org"}
	w := newInjectingWorker(t, &set, &other)
	sent := captureInjected(t, w)
	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	if got := queuedVerdict(t, w, ytimgHello(), &hotspot, idx["wgdef0"], 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want the ClientHello accepted unchanged", got)
	}
	if n := len(sent()); n != 0 {
		t.Fatalf("%d packets injected, want none", n)
	}
	if reasons := connectionReasons(ch); len(reasons) != 1 || reasons[0] != "routed->wgdef0" {
		t.Errorf("connection log flags %q, want [routed->wgdef0]: the connection leaves by the interface the set routes to; that interface carries the default route, so it is seen as a reroute rather than a tunnel, and only its name shows it is the set's own", reasons)
	}
}
