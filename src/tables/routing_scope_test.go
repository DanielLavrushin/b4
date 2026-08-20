package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func scopedSet(devices []string, exclude bool, ifaces []string) *config.SetConfig {
	s := config.NewSetConfig()
	s.Id = "s1"
	s.Name = "S1"
	s.Targets.SourceDevices = devices
	s.Targets.SourceDevicesExclude = exclude
	s.Routing.SourceInterfaces = ifaces
	return &s
}

func TestRouteSetIsSourceScoped(t *testing.T) {
	cases := []struct {
		name string
		set  *config.SetConfig
		want bool
	}{
		{"nil set", nil, false},
		{"no scope at all", scopedSet(nil, false, nil), false},
		{"source device", scopedSet([]string{"AA:BB:CC:DD:EE:FF"}, false, nil), true},
		{"blank source device", scopedSet([]string{"  "}, false, nil), false},
		{"excluded devices are not a scope", scopedSet([]string{"AA:BB:CC:DD:EE:FF"}, true, nil), false},
		{"source interface", scopedSet(nil, false, []string{"br0"}), true},
		{"blank source interface", scopedSet(nil, false, []string{" "}), false},
		{"device and interface", scopedSet([]string{"AA:BB:CC:DD:EE:FF"}, false, []string{"br0"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeSetIsSourceScoped(tc.set); got != tc.want {
				t.Errorf("routeSetIsSourceScoped = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRouteEnsureChainJumpsOutputScoping(t *testing.T) {
	st := routeState{chainPre: "b4r_x_pre", chainOut: "b4r_x_out", chainSNAT: "b4r_x_snat"}

	t.Run("an unscoped set keeps the output jump", func(t *testing.T) {
		be := &mockRouteBackend{}
		routeEnsureChainJumps(be, st, routeDeviceGate{}, false)
		if !be.hasJump("OUTPUT", st.chainOut) {
			t.Errorf("expected an OUTPUT jump, got %+v", be.jumps)
		}
	})

	t.Run("a source-scoped set removes the output jump", func(t *testing.T) {
		be := &mockRouteBackend{}
		routeEnsureChainJumps(be, st, routeDeviceGate{}, true)
		if be.hasJump("OUTPUT", st.chainOut) {
			t.Errorf("traffic the router originates can never come from a source device, got %+v", be.jumps)
		}
		if !be.hasDeletedJump("OUTPUT", st.chainOut) {
			t.Errorf("a previously installed OUTPUT jump must be removed, got %+v", be.deletedJumps)
		}
	})
}

func TestRouteStateChainsSkipOutForScopedSets(t *testing.T) {
	base := routeState{
		mode:      config.RoutingModeInterface,
		chainPre:  "b4r_x_pre",
		chainOut:  "b4r_x_out",
		chainSNAT: "b4r_x_snat",
	}

	chainsOf := func(st routeState) map[string]bool {
		out := map[string]bool{}
		for _, c := range routeStateChains(st) {
			out[c.chain] = true
		}
		return out
	}

	t.Run("an unscoped interface set still verifies its out chain", func(t *testing.T) {
		got := chainsOf(base)
		if !got["b4r_x_out"] || !got["b4r_x_pre"] || !got["b4r_x_snat"] {
			t.Errorf("expected all three chains, got %v", got)
		}
	})

	t.Run("a source-scoped interface set must not verify a chain it never fills", func(t *testing.T) {
		st := base
		st.srcScoped = true
		got := chainsOf(st)
		if got["b4r_x_out"] {
			t.Error("the out chain is left empty for a scoped set, so demanding its bypass rules loops the monitor forever")
		}
		if !got["b4r_x_pre"] || !got["b4r_x_snat"] {
			t.Errorf("the other chains must still be verified, got %v", got)
		}
	})

	t.Run("buildRouteState records the scope", func(t *testing.T) {
		cfg := config.NewConfig()
		set := config.NewSetConfig()
		set.Id, set.Name = "s1", "S1"
		set.Routing.Mode = config.RoutingModeInterface
		if buildRouteState(&cfg, &set).srcScoped {
			t.Error("a set with no source scope must not be marked scoped")
		}
		set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		if !buildRouteState(&cfg, &set).srcScoped {
			t.Error("a set bound to a source device must be marked scoped")
		}
	})
}
