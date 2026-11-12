package nfq

import (
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/warmtuple"
)

func TestWarmTupleRegisteredSYNOnly(t *testing.T) {
	cfg, set := wsFixture()
	set.TCP.WarmTuple.Enabled = true
	set.TCP.WSSize.Cutoff = "client_hello"
	set.Targets.DomainsToMatch = []string{"googlevideo.com"}
	set.Targets.IpsToMatch = nil
	w := newTestWorker(t, cfg)
	w.warmFlows = warmtuple.NewFlows()
	w.matcher.Store(buildMatcher(cfg))
	p := synWithOptions(IPv4, []byte{3, 3, 7, 0}, nil, 100)
	local := netip.AddrPortFrom(netip.AddrFrom4([4]byte(p[12:16])), binary.BigEndian.Uint16(p[20:22]))
	remote := netip.AddrPortFrom(netip.AddrFrom4([4]byte(p[16:20])), binary.BigEndian.Uint16(p[22:24]))
	release, err := w.warmFlows.Register(local, remote, cfg, set, "r.googlevideo.com")
	if err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), p...)
	vc := &verdictCtx{}
	w.dispatch(vc, raw)
	if binary.BigEndian.Uint16(raw[34:36]) != 1 || raw[42] != 1 || !sock.TCPChecksumValid(raw) {
		t.Fatal("registered SYN not rewritten correctly")
	}
	other := append([]byte(nil), p...)
	binary.BigEndian.PutUint16(other[20:22], local.Port()+1)
	sock.FixTCPChecksum(other)
	w.dispatch(&verdictCtx{}, other)
	if other[42] != 7 || binary.BigEndian.Uint16(other[34:36]) == 1 {
		t.Fatal("outside flow was rewritten")
	}
	release()
	release()
	w.dispatch(&verdictCtx{}, p)
	if p[42] != 7 || w.warmFlows.Active() != 0 {
		t.Fatal("registry removal did not stop early rewriting")
	}
}
