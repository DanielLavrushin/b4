package tables

import (
	"strings"
	"testing"
)

const forwardWithInvalidDrop = `Chain FORWARD (policy ACCEPT)
target     prot opt source               destination
ACCEPT     all  --  0.0.0.0/0            0.0.0.0/0            state RELATED,ESTABLISHED
DROP       all  --  0.0.0.0/0            0.0.0.0/0            state INVALID
ACCEPT     all  --  0.0.0.0/0            0.0.0.0/0
`

const forwardWithoutInvalidDrop = `Chain FORWARD (policy ACCEPT)
target     prot opt source               destination
ACCEPT     all  --  0.0.0.0/0            0.0.0.0/0            state RELATED,ESTABLISHED
ACCEPT     all  --  0.0.0.0/0            0.0.0.0/0
`

func forwardDropsInvalidIn(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "DROP") && strings.Contains(line, "INVALID") {
			return true
		}
	}
	return false
}

func TestARouterThatDropsWhatStrictTrackingRejectsIsRecognised(t *testing.T) {
	if !forwardDropsInvalidIn(forwardWithInvalidDrop) {
		t.Fatal("asuswrt-merlin drops every packet conntrack calls invalid. With strict window tracking that " +
			"is what stalls a large download partway, so b4 has to recognise the rule to be able to warn about it")
	}
	if forwardDropsInvalidIn(forwardWithoutInvalidDrop) {
		t.Fatal("a router that does not drop invalid packets is not affected and must not be warned about")
	}
}

func TestTheWarningOnlyFiresWhenTheRouterWasStrict(t *testing.T) {
	forgetConntrackStrictWarning()
	t.Cleanup(forgetConntrackStrictWarning)

	warnIfStrictConntrackBites("1")
	if conntrackStrictBites {
		t.Fatal("a router that already tracked windows liberally loses nothing when b4 puts that value back, " +
			"so there is nothing to warn about")
	}

	warnIfStrictConntrackBites("")
	if conntrackStrictBites {
		t.Fatal("an unreadable previous value is not evidence of anything")
	}
}

func TestTheWarningIsSaidOnce(t *testing.T) {
	forgetConntrackStrictWarning()
	t.Cleanup(forgetConntrackStrictWarning)

	conntrackStrictBites = true
	warnIfStrictConntrackBites("0")
	if !conntrackStrictBites {
		t.Fatal("the flag must stay set so a re-apply does not repeat the warning every monitor tick")
	}
}

func TestTheRelaxedSettingIsKeptWhenPuttingItBackWouldBreakForwarding(t *testing.T) {
	forgetConntrackStrictWarning()
	t.Cleanup(forgetConntrackStrictWarning)

	var wrote []string
	prev := setSysctl
	setSysctl = func(name, val string) { wrote = append(wrote, name+"="+val) }
	t.Cleanup(func() { setSysctl = prev })

	liberal := SysctlSetting{Name: conntrackLiberalSysctl, Desired: "1", Revert: "0"}

	conntrackStrictBites = false
	liberal.RevertBack()
	if len(wrote) == 0 {
		t.Fatal("on a router that forwards fine either way b4 must put the original value back rather than " +
			"leave a global kernel setting changed behind it")
	}

	wrote = nil
	conntrackStrictBites = true
	liberal.RevertBack()
	if len(wrote) != 0 {
		t.Fatalf("putting this router's 0 back stalls a large download partway with no reset and no error, so "+
			"b4 must leave the relaxed value rather than hand the network back broken, got %v", wrote)
	}

	wrote = nil
	other := SysctlSetting{Name: "net.netfilter.nf_conntrack_checksum", Desired: "0", Revert: "1"}
	other.RevertBack()
	if len(wrote) == 0 {
		t.Fatal("only the window-tracking setting is held back; every other sysctl b4 touched is still restored")
	}
}
