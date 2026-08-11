package tables

import (
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
