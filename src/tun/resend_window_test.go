package tun

import (
	"fmt"
	"strings"
	"testing"
)

func steerWindows(specs [][]string) map[string]string {
	windows := map[string]string{}
	for _, s := range specs {
		joined := strings.Join(s, " ")
		at := strings.Index(joined, "--connbytes ")
		if at < 0 {
			continue
		}
		proto := strings.Fields(joined)[1]
		windows[proto] = strings.Fields(joined[at:])[1]
	}
	return windows
}

func TestTheSteerWindowCountsTrackedResendsTwice(t *testing.T) {
	for _, multiport := range []bool{true, false} {
		for _, tc := range []struct {
			notrack  bool
			tcp, udp string
		}{
			{true, "0:19", "0:8"},
			{false, "0:38", "0:16"},
		} {
			t.Run(fmt.Sprintf("multiport=%v notrack=%v", multiport, tc.notrack), func(t *testing.T) {
				r := dupCaptureManager(true, multiport)
				r.notrackAdded = tc.notrack
				windows := steerWindows(r.steerSpecs())
				if windows["tcp"] != tc.tcp || windows["udp"] != tc.udp {
					what := "re-sends are not tracked, so conntrack counts each steered packet once"
					if !tc.notrack {
						what = "without NOTRACK every packet b4 steers is counted again when b4 sends it back out, so the window must take twice as many packets"
					}
					t.Errorf("steer windows tcp %q udp %q, want %q and %q: %s", windows["tcp"], windows["udp"], tc.tcp, tc.udp, what)
				}
				for _, s := range joinSpecs(r.steerSpecs()) {
					if strings.Contains(s, "--match-set") && strings.Contains(s, "connbytes") {
						t.Errorf("the duplication steer must keep taking whole connections: %q", s)
					}
				}
			})
		}
	}
}

func TestTheSteerWindowStaysOffWithoutConnbytes(t *testing.T) {
	r := dupCaptureManager(true, true)
	r.noConnbytes = true
	for _, notrack := range []bool{true, false} {
		r.notrackAdded = notrack
		if windows := steerWindows(r.steerSpecs()); len(windows) != 0 {
			t.Errorf("notrack=%v: a kernel without xt_connbytes cannot load the windows %v", notrack, windows)
		}
	}
}
