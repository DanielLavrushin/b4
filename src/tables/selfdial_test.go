package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

// The self-dial mark exists only because it must NOT be the queue mark: the
// mangle chains accept the queue mark so the engine's reinjected packets are not
// queued twice, so a connection b4 opens itself carrying it went out with none
// of b4's own DPI bypass applied.
func TestSelfDialMark_DistinctFromQueueMark(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
	}{
		{"default", &config.Config{}},
		{"explicit default", func() *config.Config {
			c := &config.Config{}
			c.Queue.Mark = 0x8000
			return c
		}()},
		{"custom", func() *config.Config {
			c := &config.Config{}
			c.Queue.Mark = 0x1234
			return c
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := routeQueueBypassMark(tc.cfg)
			if q == SelfDialMark {
				t.Fatalf("queue mark and self-dial mark must differ, both are 0x%x", q)
			}
			if SelfDialMark&q == q {
				t.Errorf("self-dial mark 0x%x contains every bit of queue mark 0x%x, so the mangle accept rule would match it and skip the DPI bypass", SelfDialMark, q)
			}
		})
	}
}

func TestSelfDialMark_ClearOfPerSetMarks(t *testing.T) {
	const perSetReachableBits = uint32(0x27FFF)
	if SelfDialMark&perSetReachableBits != 0 {
		t.Errorf("self-dial mark 0x%x overlaps the per-set mark bits 0x%x; a set's TPROXY divert would catch b4's own dials", SelfDialMark, perSetReachableBits)
	}
}

func TestRouteSelfDialBypass_EmitsBothMarks(t *testing.T) {
	be := &mockRouteBackend{}
	cfg := &config.Config{}
	routeSelfDialBypass(be, cfg, "B4R_TEST")

	got := be.bypass["B4R_TEST"]
	if len(got) != 2 {
		t.Fatalf("expected a bypass for the queue mark and one for the self-dial mark, got %d", len(got))
	}
	want := map[uint32]bool{routeQueueBypassMark(cfg): false, SelfDialMark: false}
	for _, m := range got {
		if _, ok := want[m]; !ok {
			t.Errorf("unexpected bypass mark 0x%x", m)
			continue
		}
		want[m] = true
	}
	for m, seen := range want {
		if !seen {
			t.Errorf("missing bypass rule for mark 0x%x", m)
		}
	}
}

// Real `nft list table inet b4_route` output from a box running the bridge. The
// element list of a set closes with a brace on a content line, and the set block
// closes with one of its own, so a scanner that treats every brace as the end of
// a chain loses the rules that come after the sets.
const nftRouteTableSample = `table inet b4_route {
	set b4r_3a97e38161af453_bf3d_v4 {
		type ipv4_addr
		flags interval,timeout
		auto-merge
		elements = { 91.105.192.0/23, 91.108.4.0-91.108.23.255,
			     91.108.56.0/22, 95.161.64.0/20,
			     149.154.160.0/20, 185.76.151.0/24 }
	}

	chain output {
		type route hook output priority mangle - 1; policy accept;
		meta mark & 0x00040000 == 0x00040000 return
		meta mark & 0x00008000 == 0x00008000 return
		ip protocol tcp ip daddr @b4r_3a97e38161af453_bf3d_v4 meta mark set 0x00024c9e
	}

	chain b4r_3a97e38161af453_bf3d_pre {
		meta mark & 0x00008000 == 0x00008000 return
		meta mark & 0x00040000 == 0x00040000 return
		ip protocol tcp ip daddr @b4r_3a97e38161af453_bf3d_v4 meta mark set 0x00024c9e tproxy ip to :13686 accept
	}

	chain b4r_deadbeefdeadbee_0001_pre {
		ip protocol tcp ip daddr @b4r_deadbeefdeadbee_0001_v4 drop
	}
}`

func TestParseNftRouteChains(t *testing.T) {
	present, bypass := parseNftRouteChains(nftRouteTableSample)

	for _, c := range []string{"output", "b4r_3a97e38161af453_bf3d_pre", "b4r_deadbeefdeadbee_0001_pre"} {
		if !present[c] {
			t.Errorf("chain %s not found; the set block above it likely swallowed the scan", c)
		}
	}
	for _, c := range []string{"output", "b4r_3a97e38161af453_bf3d_pre"} {
		for _, m := range []uint32{0x8000, SelfDialMark} {
			if !bypass[c][m] {
				t.Errorf("chain %s: bypass on mark 0x%x not seen", c, m)
			}
		}
	}
	if len(bypass["b4r_deadbeefdeadbee_0001_pre"]) != 0 {
		t.Error("a chain with no bypass rules must report none")
	}
}

func TestParseNftRouteChains_MissingSelfDialBypass(t *testing.T) {
	stripped := strings.ReplaceAll(nftRouteTableSample, "\t\tmeta mark & 0x00040000 == 0x00040000 return\n", "")
	_, bypass := parseNftRouteChains(stripped)
	if bypass["b4r_3a97e38161af453_bf3d_pre"][SelfDialMark] {
		t.Fatal("a chain that lost its self-dial bypass must not report it as present")
	}
	if !bypass["b4r_3a97e38161af453_bf3d_pre"][0x8000] {
		t.Error("the queue-mark bypass should still be seen")
	}
}

func TestRouteStateChains_OnlyDivertingChainsWantBypass(t *testing.T) {
	st := routeState{
		mode:       config.RoutingModeMTProtoWS,
		chainPre:   "pre",
		chainOut:   "out",
		chainSNAT:  "snat",
		chainQUIC:  "quic",
		quicReject: true,
	}
	for _, c := range routeStateChains(st) {
		if !c.wantBypass {
			t.Errorf("tproxy mode chain %s diverts traffic and must be checked for its bypass rules", c.chain)
		}
	}

	block := routeState{mode: config.RoutingModeBlock, chainPre: "pre"}
	for _, c := range routeStateChains(block) {
		if c.wantBypass {
			t.Errorf("block chain %s carries no bypass rules; requiring them would make the monitor resync forever", c.chain)
		}
	}

	iface := routeState{mode: config.RoutingModeInterface, chainPre: "pre", chainOut: "out", chainSNAT: "snat"}
	want := map[string]bool{"pre": true, "out": true, "snat": false}
	for _, c := range routeStateChains(iface) {
		if c.wantBypass != want[c.chain] {
			t.Errorf("interface chain %s: wantBypass=%v, expected %v", c.chain, c.wantBypass, want[c.chain])
		}
	}
}

func TestRouteBypassMarks_Distinct(t *testing.T) {
	marks := routeBypassMarks(&config.Config{})
	if len(marks) != 2 {
		t.Fatalf("expected the queue mark and the self-dial mark, got %d", len(marks))
	}
	if marks[0] == marks[1] {
		t.Errorf("both bypass marks are 0x%x", marks[0])
	}
}
