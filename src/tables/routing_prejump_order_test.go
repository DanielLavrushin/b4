package tables

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func stubIptPrerouting(t *testing.T, listing string) *[]string {
	t.Helper()
	origRun := run
	origLogged := runLogged
	t.Cleanup(func() {
		run = origRun
		runLogged = origLogged
	})

	emitted := []string{}
	run = func(args ...string) (string, error) {
		for i, a := range args {
			if a == "-L" && i+1 < len(args) && args[i+1] == "PREROUTING" {
				return listing, nil
			}
		}
		return "", nil
	}
	runLogged = func(op string, args ...string) bool {
		emitted = append(emitted, strings.Join(args, " "))
		return true
	}
	return &emitted
}

const preroutingWithCapture = `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    SKIPLOG        all  --  0.0.0.0/0  0.0.0.0/0
2    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
3    B4_DNSTCP      tcp  --  0.0.0.0/0  0.0.0.0/0
`

func TestPreJumpGoesAboveTheCaptureChain(t *testing.T) {
	emitted := stubIptPrerouting(t, preroutingWithCapture)
	stubBinaries(t, backendIPTables)

	routeEnsureGatedPreJump(&routeIptBackend{}, "b4r_x_pre", routeDeviceGate{})

	var added string
	for _, e := range *emitted {
		if strings.Contains(e, "b4r_x_pre") && (strings.Contains(e, "-I") || strings.Contains(e, "-A")) {
			added = e
		}
	}
	if added == "" {
		t.Fatalf("no jump was emitted: %v", *emitted)
	}
	if !strings.Contains(added, "-I PREROUTING 2") {
		t.Errorf("the jump has to go in at the capture chain's position so the set's divert rule sees a reply before the capture engine queues it; got %q", added)
	}
}

func TestPreJumpIsAppendedWhenThereIsNoCaptureChain(t *testing.T) {
	emitted := stubIptPrerouting(t, "Chain PREROUTING (policy ACCEPT)\nnum  target  prot opt source destination\n")
	stubBinaries(t, backendIPTables)

	routeEnsureGatedPreJump(&routeIptBackend{}, "b4r_x_pre", routeDeviceGate{})

	for _, e := range *emitted {
		if strings.Contains(e, "b4r_x_pre") && strings.Contains(e, "-A PREROUTING") {
			return
		}
	}
	t.Errorf("with no capture chain to sit above, the jump is appended as before: %v", *emitted)
}

func TestPreJumpsBelowCaptureAreDetected(t *testing.T) {
	below := `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
2    b4r_x_pre      all  --  0.0.0.0/0  0.0.0.0/0
`
	above := `Chain PREROUTING (policy ACCEPT)
num  target         prot opt source     destination
1    b4r_x_pre      all  --  0.0.0.0/0  0.0.0.0/0
2    B4_PREROUTING  all  --  0.0.0.0/0  0.0.0.0/0
`
	for _, tc := range []struct {
		name    string
		listing string
		want    bool
	}{
		{"below", below, true},
		{"above", above, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubIptPrerouting(t, tc.listing)
			stubBinaries(t, backendIPTables)
			if got := iptPreJumpsBelowCapture(backendIPTables); got != tc.want {
				t.Errorf("iptPreJumpsBelowCapture = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPreJumpOrderSurvivesSeveralSets(t *testing.T) {
	origRun := run
	origLogged := runLogged
	t.Cleanup(func() {
		run = origRun
		runLogged = origLogged
	})
	stubBinaries(t, backendIPTables)

	chain := []string{"SKIPLOG", captureChainPre, "B4_DNSTCP"}
	render := func() string {
		var b strings.Builder
		b.WriteString("Chain PREROUTING (policy ACCEPT)\nnum  target  prot opt source destination\n")
		for i, name := range chain {
			b.WriteString(fmt.Sprintf("%d    %s  all  --  0.0.0.0/0  0.0.0.0/0\n", i+1, name))
		}
		return b.String()
	}

	run = func(args ...string) (string, error) {
		for i, a := range args {
			if a == "-L" && i+1 < len(args) && args[i+1] == "PREROUTING" {
				return render(), nil
			}
		}
		return "", nil
	}
	runLogged = func(op string, args ...string) bool {
		if args[0] != backendIPTables {
			return true
		}
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
		chain = append(chain[:at-1], append([]string{target}, chain[at-1:]...)...)
		return true
	}

	for _, name := range []string{"first", "second", "third"} {
		routeEnsureGatedPreJump(&routeIptBackend{}, routeChainPrefix+name+"_pre", routeDeviceGate{})
	}

	var got []string
	captureAt := -1
	for i, name := range chain {
		if name == captureChainPre {
			captureAt = i
		}
		if routeIsPreChainName(name) {
			got = append(got, name)
		}
	}
	want := []string{"b4r_first_pre", "b4r_second_pre", "b4r_third_pre"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the sets have to keep the order the config gives them, got %v want %v (chain %v)", got, want, chain)
	}
	for i, name := range chain {
		if routeIsPreChainName(name) && i > captureAt {
			t.Errorf("%s sits below %s (index %d vs %d) in %v", name, captureChainPre, i, captureAt, chain)
		}
	}
}
