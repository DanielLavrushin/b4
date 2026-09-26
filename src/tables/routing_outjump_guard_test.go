package tables

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const outQueueAccept = "ACCEPT|mark match 0x8000/0x8000"

func simulatedOutput(t *testing.T, initial []string) (*[]string, *[]string, *[]string) {
	t.Helper()
	origRun := run
	origLogged := runLogged
	t.Cleanup(func() {
		run = origRun
		runLogged = origLogged
	})
	v4 := append([]string(nil), initial...)
	v6 := append([]string(nil), initial...)
	var writes []string
	chainFor := func(cmd string) *[]string {
		if strings.HasPrefix(cmd, "ip6") {
			return &v6
		}
		return &v4
	}
	render := func(chain []string) string {
		var b strings.Builder
		b.WriteString("Chain OUTPUT (policy ACCEPT)\nnum  target  prot opt source destination\n")
		for i, rule := range chain {
			name, extra, _ := strings.Cut(rule, "|")
			b.WriteString(fmt.Sprintf("%d    %s  all  --  0.0.0.0/0  0.0.0.0/0  %s\n", i+1, name, extra))
		}
		return b.String()
	}
	run = func(args ...string) (string, error) {
		chain := chainFor(args[0])
		for i, a := range args {
			if a == "-L" && i+1 < len(args) && args[i+1] == "OUTPUT" {
				return render(*chain), nil
			}
			if a == "-D" && i+2 < len(args) && args[i+1] == "OUTPUT" {
				writes = append(writes, strings.Join(args, " "))
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
		writes = append(writes, strings.Join(args, " "))
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
	return &v4, &v6, &writes
}

func outTargets(chain []string) string {
	names := make([]string, 0, len(chain))
	for _, rule := range chain {
		name, _, _ := strings.Cut(rule, "|")
		names = append(names, name)
	}
	return strings.Join(names, ",")
}

func TestOutJumpsBelowTheQueueAcceptAreDetected(t *testing.T) {
	listing := func(rules ...string) string {
		var b strings.Builder
		b.WriteString("Chain OUTPUT (policy ACCEPT)\nnum  target  prot opt source destination\n")
		for i, r := range rules {
			b.WriteString(fmt.Sprintf("%d    %s\n", i+1, r))
		}
		return b.String()
	}
	for _, tc := range []struct {
		name  string
		out   string
		queue uint32
		want  bool
	}{
		{"below the accept", listing(
			"CONNMARK  all  --  0.0.0.0/0  0.0.0.0/0  mark match 0x8000/0x8000 CONNMARK save mask 0x8000",
			"ACCEPT    all  --  0.0.0.0/0  0.0.0.0/0  mark match 0x8000/0x8000",
			"NFQUEUE   udp  --  0.0.0.0/0  0.0.0.0/0  udp dpt:53 NFQUEUE num 537 bypass",
			"b4r_x_out all  --  0.0.0.0/0  0.0.0.0/0",
		), 0x8000, true},
		{"above the accept", listing(
			"b4r_x_out all  --  0.0.0.0/0  0.0.0.0/0",
			"ACCEPT    all  --  0.0.0.0/0  0.0.0.0/0  mark match 0x8000/0x8000",
		), 0x8000, false},
		{"no queue accept", listing(
			"ACCEPT    all  --  0.0.0.0/0  0.0.0.0/0  mark match 0x10000/0x10000",
			"b4r_x_out all  --  0.0.0.0/0  0.0.0.0/0",
		), 0x8000, false},
		{"an inverted mark match is not the queue accept", listing(
			"ACCEPT    all  --  0.0.0.0/0  0.0.0.0/0  mark match ! 0x8000/0x8000",
			"b4r_x_out all  --  0.0.0.0/0  0.0.0.0/0",
		), 0x8000, false},
		{"custom queue mark", listing(
			"ACCEPT    all  --  0.0.0.0/0  0.0.0.0/0  mark match 0x100000/0x100000",
			"b4r_x_out all  --  0.0.0.0/0  0.0.0.0/0",
		), 0x100000, true},
		{"old iptables prints the match in capitals", listing(
			"ACCEPT    all  --  0.0.0.0/0  0.0.0.0/0  MARK match 0x8000/0x8000",
			"b4r_x_out all  --  0.0.0.0/0  0.0.0.0/0",
		), 0x8000, true},
		{"a set's prerouting chain is not an output jump", listing(
			"ACCEPT    all  --  0.0.0.0/0  0.0.0.0/0  mark match 0x8000/0x8000",
			"b4r_x_pre all  --  0.0.0.0/0  0.0.0.0/0",
		), 0x8000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := iptOutJumpsDisplaced(tc.out, tc.queue); got != tc.want {
				t.Errorf("iptOutJumpsDisplaced = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOutJumpPrecedenceLiftsTheJumpsAboveTheQueueAccept(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)
	cfg := config.NewConfig()
	v4, v6, _ := simulatedOutput(t, []string{
		"CONNMARK|mark match 0x8000/0x8000",
		outQueueAccept,
		"NFQUEUE|udp dpt:53",
		"NFQUEUE|udp spt:53",
		"b4r_first_out",
		"b4r_second_out",
		"B4",
	})

	routeEnsureOutJumpPrecedence(&routeIptBackend{}, &cfg)

	want := "b4r_first_out,b4r_second_out,CONNMARK,ACCEPT,NFQUEUE,NFQUEUE,B4"
	for family, chain := range map[string]*[]string{"iptables": v4, "ip6tables": v6} {
		if got := outTargets(*chain); got != want {
			t.Errorf("after a capture refresh the engine's ACCEPT ends the mangle table for every packet b4 injects, so the sets' jumps have to go back above it; %s mangle OUTPUT is %s, want %s", family, got, want)
		}
	}
}

func TestOutJumpPrecedenceLeavesACorrectChainAlone(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)
	cfg := config.NewConfig()
	for _, initial := range [][]string{
		{"b4r_first_out", "b4r_second_out", "CONNMARK|mark match 0x8000/0x8000", outQueueAccept, "B4"},
		{"XRAY_OUT", "b4r_first_out", outQueueAccept, "B4"},
		{"CONNMARK|mark match 0x8000/0x8000", outQueueAccept, "B4"},
	} {
		_, _, writes := simulatedOutput(t, initial)
		routeEnsureOutJumpPrecedence(&routeIptBackend{}, &cfg)
		if len(*writes) != 0 {
			t.Errorf("mangle OUTPUT %v needs no change, yet b4 wrote %v", initial, *writes)
		}
	}
}

func TestOutJumpPrecedenceDropsADuplicateJump(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)
	cfg := config.NewConfig()
	v4, v6, _ := simulatedOutput(t, []string{outQueueAccept, "b4r_first_out", "B4", "b4r_first_out"})

	routeEnsureOutJumpPrecedence(&routeIptBackend{}, &cfg)

	for family, chain := range map[string]*[]string{"iptables": v4, "ip6tables": v6} {
		if got := outTargets(*chain); got != "b4r_first_out,ACCEPT,B4" {
			t.Errorf("%s mangle OUTPUT is %s, want one jump above the ACCEPT", family, got)
		}
	}
}

func TestOutJumpPrecedenceIsIptablesOnly(t *testing.T) {
	cfg := config.NewConfig()
	_, _, writes := simulatedOutput(t, []string{outQueueAccept, "b4r_first_out"})
	routeEnsureOutJumpPrecedence(&mockRouteBackend{}, &cfg)
	if len(*writes) != 0 {
		t.Errorf("nftables orders its base chains by priority, so there is nothing to lift; b4 wrote %v", *writes)
	}
}

func TestJumpPrecedenceLiftsTheOutputJumpsAfterACaptureRefresh(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)
	cfg := withOrderedCache(t, orderTestSet("first", config.RoutingModeInterface, nil))
	origEngine := routeEngine
	t.Cleanup(func() { routeEngine = origEngine })
	routeEngine = &routeIptBackend{}

	v4, v6, _ := simulatedOutput(t, []string{"CONNMARK|mark match 0x8000/0x8000", outQueueAccept, "b4r_first_out", "B4"})

	RoutingEnsureJumpPrecedence(cfg)

	for family, chain := range map[string]*[]string{"iptables": v4, "ip6tables": v6} {
		if got := outTargets(*chain); got != "b4r_first_out,CONNMARK,ACCEPT,B4" {
			t.Errorf("the capture refresh and the firewall monitor reach the lift through RoutingEnsureJumpPrecedence; %s mangle OUTPUT is %s", family, got)
		}
	}
}
