package nfq

import (
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/warmtuple"
)

func reuseSetSYNRewritten(t *testing.T, dport uint16, edit func(*config.SetConfig)) bool {
	t.Helper()
	cfg, set := wsFixture()
	set.Id = "reuse-accept"
	set.TCP.WSSize.Cutoff = ""
	set.TCP.DPortFilter = "80,443"
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.WSSizeReuse.Enabled = true
	if edit != nil {
		edit(set)
	}
	cfg.BuildSetPortRanges()
	cfg.BuildTCPPortMap()
	w := newTestWorker(t, cfg)
	p := synWithOptions(IPv4, []byte{3, 3, 7, 0}, nil, 100)
	binary.BigEndian.PutUint16(p[22:24], dport)
	sock.FixTCPChecksum(p)
	w.dispatch(&verdictCtx{q: newFakeModQueue()}, p)
	return p[42] != 7
}

func TestReuseSetPassesDivertedTLSUntouched(t *testing.T) {
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	wsReuseDiverts = func(id string) bool { return id == "reuse-accept" }
	if reuseSetSYNRewritten(t, 443, nil) {
		t.Error("a SYN to port 443 of a set whose TPROXY diversion is active was rewritten; the reuse relay owns that connection and its warm carries the strategy")
	}
	if !reuseSetSYNRewritten(t, 80, nil) {
		t.Error("a SYN to port 80 of a diverted reuse set was passed untouched, though the diversion takes only port 443")
	}
	if !reuseSetSYNRewritten(t, 443, func(s *config.SetConfig) { s.TCP.WSSizeReuse.Enabled = false }) {
		t.Error("a SYN to port 443 of a set with reuse off was passed untouched because a diversion is published under its id")
	}
	if !reuseSetSYNRewritten(t, 443, func(s *config.SetConfig) { s.Id = "other" }) {
		t.Error("a SYN to port 443 of a reuse set whose own diversion is not active was passed untouched because another set's is")
	}
}

func TestARegisteredReuseFallbackGetsTheSetsStrategyWhileTheSetIsDiverted(t *testing.T) {
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	wsReuseDiverts = func(id string) bool { return id == "reuse-fallback" }
	cfg, set := wsFixture()
	set.Id = "reuse-fallback"
	set.TCP.WSSize.Cutoff = ""
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.WSSizeReuse.Enabled = true
	cfg.BuildSetPortRanges()
	cfg.BuildTCPPortMap()
	w := newTestWorker(t, cfg)
	w.warmFlows = warmtuple.NewFlows()
	syn := func(sport uint16) []byte {
		p := synWithOptions(IPv4, []byte{3, 3, 7, 0}, nil, 100)
		binary.BigEndian.PutUint16(p[20:22], sport)
		binary.BigEndian.PutUint16(p[22:24], 443)
		sock.FixTCPChecksum(p)
		return p
	}
	device := syn(41000)
	w.dispatch(&verdictCtx{q: newFakeModQueue()}, device)
	if device[42] != 7 {
		t.Fatal("a diverted connection of the reuse set was rewritten, so this test proves nothing")
	}
	fallback := syn(41001)
	local := netip.AddrPortFrom(netip.AddrFrom4([4]byte(fallback[12:16])), 41001)
	remote := netip.AddrPortFrom(netip.AddrFrom4([4]byte(fallback[16:20])), 443)
	release, err := w.warmFlows.Register(local, remote, cfg, set, "video.example.org")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	w.dispatch(&verdictCtx{q: newFakeModQueue()}, fallback)
	if binary.BigEndian.Uint16(fallback[34:36]) != 1 || fallback[42] != 1 || !sock.TCPChecksumValid(fallback) {
		t.Fatalf("a registered fallback SYN to port 443 of a diverted reuse set left with window %d and scale %d, want the set's 1 and 1", binary.BigEndian.Uint16(fallback[34:36]), fallback[42])
	}
}

func TestReuseSetKeepsItsStrategyWithoutDiversion(t *testing.T) {
	for _, dport := range []uint16{443, 80} {
		if !reuseSetSYNRewritten(t, dport, nil) {
			t.Errorf("a SYN to port %d of a set that reuses WSSIZE kept window scale 7 while nothing diverts the set to the reuse relay, so the connection got no strategy at all", dport)
		}
	}
}
