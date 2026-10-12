package tun

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestClearStaleArtifactsLeavesAnAbsentRawTableAlone(t *testing.T) {
	orig := rawTablePresent
	t.Cleanup(func() { rawTablePresent = orig })
	rawCommands := func(f *fakeCapture) int {
		return f.count(func(c string) bool { return strings.Contains(c, " -t raw ") })
	}

	for _, present := range []bool{true, false} {
		f := stubCapture(t, true, nil)
		f.fail = func(c string) bool { return strings.Contains(c, " -D ") || strings.HasPrefix(c, "ip rule del") }
		var asked []string
		rawTablePresent = func(ipt string) bool {
			asked = append(asked, ipt)
			return present
		}
		ClearStaleArtifacts(&config.Config{})
		if len(asked) == 0 || asked[0] != "iptables" {
			t.Fatalf("the cleanup asked about %v, want iptables", asked)
		}
		if n := rawCommands(f); present && n == 0 {
			t.Errorf("with a raw table the stale notrack rules of a TUN run must still be removed")
		} else if !present && n != 0 {
			t.Errorf("%d raw table commands on a KeeneticOS kernel without iptable_raw, each of which makes the kernel try to load it, although no rule can be there", n)
		}
	}
}
