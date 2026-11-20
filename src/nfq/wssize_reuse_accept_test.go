package nfq

import (
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
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

func TestReuseSetKeepsItsStrategyWithoutDiversion(t *testing.T) {
	for _, dport := range []uint16{443, 80} {
		if !reuseSetSYNRewritten(t, dport, nil) {
			t.Errorf("a SYN to port %d of a set that reuses WSSIZE kept window scale 7 while nothing diverts the set to the reuse relay, so the connection got no strategy at all", dport)
		}
	}
}
