package tables

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const preroutingWithXrayDivert = `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    DIVERT         tcp  --  0.0.0.0/0  0.0.0.0/0            socket --transparent
2    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
3    SKIPLOG        tcp  --  0.0.0.0/0  0.0.0.0/0            tcp dpt:8443
`

func TestPreJumpGoesAboveAForeignSocketRule(t *testing.T) {
	emitted := stubIptPrerouting(t, preroutingWithXrayDivert)
	stubBinaries(t, backendIPTables)

	routeEnsureGatedPreJump(&routeIptBackend{}, "b4r_x_pre", routeDeviceGate{})

	var added string
	for _, e := range *emitted {
		if strings.Contains(e, "b4r_x_pre") && (strings.Contains(e, "-I") || strings.Contains(e, "-A")) {
			added = e
		}
	}
	if !strings.Contains(added, "-I PREROUTING 1") {
		t.Errorf("XrayUI's DIVERT accepts every packet of a local transparent socket, b4's included, and routes it away from b4's listener; the jump has to go above it, got %q", added)
	}
}

func TestPreJumpsBelowAForeignSocketRuleAreDetected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		listing string
		want    bool
		blocker string
	}{
		{"below divert", `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    DIVERT         tcp  --  0.0.0.0/0  0.0.0.0/0            socket --transparent
2    b4r_x_pre      all  --  0.0.0.0/0  0.0.0.0/0
3    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
`, true, "DIVERT"},
		{"plain socket match", `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    XRAY_DIVERT    tcp  --  0.0.0.0/0  0.0.0.0/0            socket
2    b4r_x_pre      all  --  0.0.0.0/0  0.0.0.0/0
3    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
`, true, "XRAY_DIVERT"},
		{"above divert", `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    b4r_x_pre      all  --  0.0.0.0/0  0.0.0.0/0
2    DIVERT         tcp  --  0.0.0.0/0  0.0.0.0/0            socket --transparent
3    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
`, false, ""},
		{"foreign rule without a socket match", `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    SKIPLOG        tcp  --  0.0.0.0/0  0.0.0.0/0            tcp dpt:8443
2    b4r_x_pre      all  --  0.0.0.0/0  0.0.0.0/0
3    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
`, false, ""},
		{"b4 capture chain", `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
2    b4r_x_pre      all  --  0.0.0.0/0  0.0.0.0/0
`, true, captureChainPre},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubIptPrerouting(t, tc.listing)
			stubBinaries(t, backendIPTables)
			got, by := iptPreJumpsDisplaced(backendIPTables)
			if got != tc.want || by != tc.blocker {
				t.Errorf("iptPreJumpsDisplaced = %v, %q; want %v, %q", got, by, tc.want, tc.blocker)
			}
		})
	}
}

func simulatedPrerouting(t *testing.T, initial []string) *[]string {
	t.Helper()
	origRun := run
	origLogged := runLogged
	t.Cleanup(func() {
		run = origRun
		runLogged = origLogged
	})
	v4 := append([]string(nil), initial...)
	v6 := append([]string(nil), initial...)
	chainFor := func(cmd string) *[]string {
		if strings.HasPrefix(cmd, "ip6") {
			return &v6
		}
		return &v4
	}
	render := func(chain []string) string {
		var b strings.Builder
		b.WriteString("Chain PREROUTING (policy ACCEPT)\nnum  target  prot opt source destination\n")
		for i, name := range chain {
			extra := ""
			if name == "DIVERT" {
				extra = "  socket --transparent"
			}
			b.WriteString(fmt.Sprintf("%d    %s  all  --  0.0.0.0/0  0.0.0.0/0%s\n", i+1, name, extra))
		}
		return b.String()
	}
	run = func(args ...string) (string, error) {
		chain := chainFor(args[0])
		for i, a := range args {
			if a == "-L" && i+1 < len(args) && args[i+1] == "PREROUTING" {
				return render(*chain), nil
			}
			if a == "-D" && i+2 < len(args) && args[i+1] == "PREROUTING" {
				n, err := strconv.Atoi(args[i+2])
				if err == nil && n >= 1 && n <= len(*chain) {
					*chain = append((*chain)[:n-1], (*chain)[n:]...)
				}
				return "", nil
			}
		}
		return "", nil
	}
	runLogged = func(op string, args ...string) bool {
		chain := chainFor(args[0])
		var at int
		var target string
		for i, a := range args {
			if a == "-I" && i+2 < len(args) {
				at, _ = strconv.Atoi(args[i+2])
			}
			if a == "-j" && i+1 < len(args) {
				target = args[i+1]
			}
		}
		if at <= 0 || target == "" {
			return true
		}
		*chain = append((*chain)[:at-1], append([]string{target}, (*chain)[at-1:]...)...)
		return true
	}
	return &v4
}

func TestPrecedenceLiftsJumpsAboveADivertInsertedLater(t *testing.T) {
	stubBinaries(t, backendIPTables)
	cfg := withOrderedCache(t,
		orderTestSet("first", config.RoutingModeProxy, nil),
		orderTestSet("second", config.RoutingModeProxy, nil),
	)
	chain := simulatedPrerouting(t, []string{"DIVERT", "b4r_first_pre", "b4r_second_pre", captureChainPre, "SKIPLOG"})

	routeEnsurePreJumpPrecedence(&routeIptBackend{}, cfg)

	want := "b4r_first_pre,b4r_second_pre,DIVERT," + captureChainPre + ",SKIPLOG"
	if got := strings.Join(*chain, ","); got != want {
		t.Errorf("mangle PREROUTING is %s, want %s", got, want)
	}

	before := strings.Join(*chain, ",")
	routeEnsurePreJumpPrecedence(&routeIptBackend{}, cfg)
	if got := strings.Join(*chain, ","); got != before {
		t.Errorf("a correct chain must be left alone, it became %s", got)
	}
}

func TestAlreadyOrderedIsFalseBelowAForeignSocketRule(t *testing.T) {
	stubBinaries(t, backendIPTables)
	cfg := withOrderedCache(t,
		orderTestSet("first", config.RoutingModeProxy, nil),
		orderTestSet("second", config.RoutingModeProxy, nil),
	)
	ordered := routeOrderedRoutingSets(cfg)

	simulatedPrerouting(t, []string{"DIVERT", "b4r_first_pre", "b4r_second_pre", captureChainPre})
	if routePreJumpsAlreadyOrdered(&routeIptBackend{}, ordered) {
		t.Error("jumps under DIVERT are in order but not in effect; the reorder pass must move them")
	}

	simulatedPrerouting(t, []string{"b4r_first_pre", "b4r_second_pre", "DIVERT", captureChainPre})
	if !routePreJumpsAlreadyOrdered(&routeIptBackend{}, ordered) {
		t.Error("jumps above DIVERT and the capture chain, in config order, are already right")
	}
}

func TestShadowedByNamesTheForeignRuleAboveTheSetsJump(t *testing.T) {
	stubBinaries(t, backendIPTables)
	withOrderedCache(t, orderTestSet("bridge", config.RoutingModeProxy, nil))
	origEngine := routeEngine
	t.Cleanup(func() { routeEngine = origEngine })
	routeEngine = &routeIptBackend{}

	simulatedPrerouting(t, []string{"DIVERT", "b4r_bridge_pre", captureChainPre})
	if got := RoutingPreJumpShadowedBy("bridge"); got != "DIVERT" {
		t.Errorf("RoutingPreJumpShadowedBy = %q, want DIVERT", got)
	}

	simulatedPrerouting(t, []string{"b4r_bridge_pre", "DIVERT", captureChainPre})
	if got := RoutingPreJumpShadowedBy("bridge"); got != "" {
		t.Errorf("a jump above DIVERT is not shadowed, got %q", got)
	}

	if got := RoutingPreJumpShadowedBy("missing"); got != "" {
		t.Errorf("an unknown set is not shadowed, got %q", got)
	}
}

func TestLiftShadowedJumpsMovesTheBridgeBackAboveDivert(t *testing.T) {
	stubBinaries(t, backendIPTables)
	cfg := withOrderedCache(t, orderTestSet("bridge", config.RoutingModeProxy, nil))
	origEngine, origSynced := routeEngine, routeSyncedCfg
	t.Cleanup(func() { routeEngine, routeSyncedCfg = origEngine, origSynced })
	routeEngine = &routeIptBackend{}
	routeSyncedCfg = cfg

	chain := simulatedPrerouting(t, []string{"DIVERT", "b4r_bridge_pre", captureChainPre})
	RoutingLiftShadowedJumps()
	if got := strings.Join(*chain, ","); got != "b4r_bridge_pre,DIVERT,"+captureChainPre {
		t.Errorf("Check again has to lift the jump even with the monitor off, got %s", got)
	}

	routeSyncedCfg = nil
	simulatedPrerouting(t, []string{"DIVERT", "b4r_bridge_pre", captureChainPre})
	RoutingLiftShadowedJumps()
}
