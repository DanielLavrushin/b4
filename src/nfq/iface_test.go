package nfq

import (
	"testing"

	"github.com/florianl/go-nfqueue"
)

func TestIfaceTrafficCountsWhatTheQueueSaw(t *testing.T) {
	ResetIfaceTraffic()
	t.Cleanup(ResetIfaceTraffic)

	ifaceCache.Store(uint32(101), "br0")
	ifaceCache.Store(uint32(102), "xray0")
	t.Cleanup(func() {
		ifaceCache.Delete(uint32(101))
		ifaceCache.Delete(uint32(102))
	})

	for i := 0; i < 7; i++ {
		recordIfaceTraffic(101, ifaceLeaving)
	}
	recordIfaceTraffic(101, ifaceArriving)
	recordIfaceTraffic(102, ifaceLeaving)
	recordIfaceTraffic(0, ifaceLeaving)

	got := IfaceTraffic()
	if got["br0"].Leaving != 7 || got["br0"].Arriving != 1 || got["xray0"].Leaving != 1 {
		t.Errorf("counts are what tells the web interface a chosen interface carries nothing, got %v", got)
	}
	if got["br0"].Leaving == 8 {
		t.Error("an uplink sees the whole reply side, so folding both directions together hides a filter that lets no outgoing traffic through")
	}
	if len(got) != 2 {
		t.Errorf("a packet with no interface index cannot be attributed and must not invent one, got %v", got)
	}

	ResetIfaceTraffic()
	if len(IfaceTraffic()) != 0 {
		t.Error("the counters are per run, so a reset has to clear them")
	}
}

func TestPacketIfaceIndexPrefersTheDeviceThePacketLeavesBy(t *testing.T) {
	in, out := uint32(7), uint32(9)

	if got, role := packetIfaceIndex(nfqueue.Attribute{InDev: &in, OutDev: &out}); got != out || role != ifaceLeaving {
		t.Errorf("b4 captures in postrouting and output, where the routing decision is already made, so the outgoing device is the one that decides: got %d role=%d", got, role)
	}
	if got, role := packetIfaceIndex(nfqueue.Attribute{OutDev: &out}); got != out || role != ifaceLeaving {
		t.Errorf("postrouting carries no arriving device on every kernel, so the outgoing one has to stand alone: got %d role=%d", got, role)
	}
	if got, role := packetIfaceIndex(nfqueue.Attribute{InDev: &in}); got != in || role != ifaceArriving {
		t.Errorf("in prerouting there is no outgoing device yet, so the arriving one is all there is: got %d role=%d", got, role)
	}
	if got, _ := packetIfaceIndex(nfqueue.Attribute{}); got != 0 {
		t.Errorf("with neither device the packet cannot be attributed: got %d", got)
	}
}
