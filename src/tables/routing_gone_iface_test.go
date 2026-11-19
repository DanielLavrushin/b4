package tables

import (
	"errors"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const goneIfaceName = "b4gone0"

func goneInterfaceWarning(t *testing.T, seen, killSwitch bool) string {
	t.Helper()
	buf := captureTablesLog(t)
	prevLogged, prevRun, prevDel, prevProto, prevSeen := runLogged, run, routeDelRuleLoop, routeIPSupportsProto, routeIfaceSeen
	t.Cleanup(func() {
		runLogged, run, routeDelRuleLoop, routeIPSupportsProto, routeIfaceSeen = prevLogged, prevRun, prevDel, prevProto, prevSeen
	})
	runLogged = func(string, ...string) bool { return true }
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, " rule del ") || strings.Contains(joined, " -D ") {
			return "", errors.New("stub: no such rule")
		}
		return "", nil
	}
	routeDelRuleLoop = func(bool, string, string) {}
	routeIPSupportsProto = func() bool { return false }
	routeIfaceSeen = map[string]bool{goneIfaceName: seen}

	st := loopTestState(goneIfaceName, false)
	st.setID = "c0ffee-set-id"
	st.set = &config.SetConfig{Id: st.setID, Name: "Office VPN"}
	st.killSwitch = killSwitch

	func() {
		routeMu.Lock()
		prevCache := routeRuleCache
		defer func() {
			routeRuleCache = prevCache
			routeMu.Unlock()
		}()
		routeRuleCache = map[string]routeState{}
		routeEnsurePolicyRouting(st, true, false)
	}()

	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, goneIfaceName+", which has") {
			return line
		}
	}
	t.Fatalf("no warning about %s was logged:\n%s", goneIfaceName, buf.String())
	return ""
}

func TestTheGoneInterfaceWarningSaysTheTrafficLeavesByTheOrdinaryUplinkWithoutTheKillSwitch(t *testing.T) {
	line := goneInterfaceWarning(t, true, false)

	if !strings.Contains(line, "set 'Office VPN' routes to "+goneIfaceName+", which has gone away") {
		t.Errorf("the warning must name the set the way the rest of the routing log does, not by its ID: %q", line)
	}
	if !strings.Contains(line, "leaves by the ordinary uplink until the interface returns") {
		t.Errorf("without a kill switch the set's rule finds no route in its table and the lookup falls through to the main table, so the traffic leaves by the ordinary uplink; the warning must say so: %q", line)
	}
	if strings.Contains(line, "stops rather than") || strings.Contains(line, "drops") {
		t.Errorf("the warning claims the traffic stops, which only a kill switch does: %q", line)
	}
}

func TestTheGoneInterfaceWarningSaysTheKillSwitchDropsTheTraffic(t *testing.T) {
	line := goneInterfaceWarning(t, true, true)

	if !strings.Contains(line, "set 'Office VPN' routes to "+goneIfaceName+", which has gone away") {
		t.Errorf("the warning must name the set the way the rest of the routing log does, not by its ID: %q", line)
	}
	if !strings.Contains(line, "kill switch drops the set's traffic until the interface returns") {
		t.Errorf("with a kill switch the blackhole ends the lookup, so the set's traffic is dropped; the warning must say so: %q", line)
	}
	if strings.Contains(line, "ordinary uplink") {
		t.Errorf("the warning says the traffic leaves by the ordinary uplink while the kill switch holds it: %q", line)
	}
}

func TestTheNeverSeenInterfaceWarningNamesTheSet(t *testing.T) {
	line := goneInterfaceWarning(t, false, true)

	if !strings.Contains(line, "set 'Office VPN' routes to "+goneIfaceName+", which has never appeared") {
		t.Errorf("the warning must name the set the way the rest of the routing log does, not by its ID: %q", line)
	}
	if strings.Contains(line, "c0ffee-set-id") {
		t.Errorf("the warning shows the set's ID, which the user never sees in the web interface: %q", line)
	}
}
