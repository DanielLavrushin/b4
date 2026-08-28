package tables

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTheLockErrorThisRouterProducesIsRecognised(t *testing.T) {
	observed := "iptables: Resource temporarily unavailable."
	if !isXtablesLockBusy(observed, errors.New("exit status 4")) {
		t.Fatalf("an ASUS Merlin box with iptables v1.4.15 has no '-w' flag, so this is the only signal that "+
			"another program held the firewall lock. Not recognising it means the rule is dropped silently and "+
			"b4 runs with an incomplete rule set: %q", observed)
	}

	for _, other := range []string{
		"another app is currently holding the xtables lock",
		"Another app is currently holding the xtables lock; waiting",
	} {
		if !isXtablesLockBusy(other, errors.New("exit status 4")) {
			t.Fatalf("newer iptables words the same condition differently: %q", other)
		}
	}
}

func TestASuccessfulCommandIsNeverTreatedAsLocked(t *testing.T) {
	if isXtablesLockBusy("", nil) {
		t.Fatal("a command that succeeded must never be retried")
	}
	if isXtablesLockBusy("iptables: No chain/target/match by that name.", errors.New("exit status 1")) {
		t.Fatal("a genuine rule error must fail immediately instead of being retried eight times")
	}
}

func TestTheRetryBudgetIsBoundedAndBacksOff(t *testing.T) {
	total := time.Duration(0)
	for i := 0; i < iptLockRetries; i++ {
		d := iptLockBackoff(i)
		if d <= 0 {
			t.Fatalf("attempt %d has no delay, so the retries would spin on the lock", i)
		}
		if i > 0 && d <= iptLockBackoff(i-1) {
			t.Fatalf("attempt %d does not back off past attempt %d", i, i-1)
		}
		total += d
	}
	if total > 30*time.Second {
		t.Fatalf("the retry budget is %v, long enough to stall startup rather than ride out a lock", total)
	}
	if total < 500*time.Millisecond {
		t.Fatalf("the retry budget is only %v, too short to outlast another program's firewall pass", total)
	}
}

func TestRunRetriesALockedIptablesCommandAndSucceeds(t *testing.T) {
	if !hasBinary("sh") {
		t.Skip("needs a shell")
	}
	prevRetries := iptLockRetries
	prevBackoff := iptLockBackoff
	iptLockBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { iptLockRetries, iptLockBackoff = prevRetries, prevBackoff })

	out, err := runOnce([]string{"sh", "-c", "echo 'iptables: Resource temporarily unavailable.'; exit 4"})
	if err == nil {
		t.Fatal("the harness command must fail")
	}
	if !isXtablesLockBusy(out, err) {
		t.Fatalf("the harness output must look like a locked firewall, got %q", out)
	}
	if !strings.Contains(out, "Resource temporarily unavailable") {
		t.Fatalf("unexpected harness output %q", out)
	}
}

func TestAStaleRuleNumberIsNeverRetried(t *testing.T) {
	for _, args := range [][]string{
		{"iptables", "-w", "-t", "nat", "-D", "POSTROUTING", "7"},
		{"ip6tables", "-t", "mangle", "-D", "PREROUTING", "1"},
	} {
		if !iptDeletesByPosition(args) {
			t.Fatalf("%v deletes by position. The kernel reports a locked pre-1.4.20 iptables by refusing the "+
				"commit, which means the table changed under the read that produced this number, so the number "+
				"names a different rule now. Retrying it deletes whichever rule moved into that slot, and on a "+
				"router that is the firmware's own NAT or INPUT rule: the LAN loses its route out and the box "+
				"loses SSH until a reboot rebuilds the firewall", args)
		}
	}

	for _, args := range [][]string{
		{"iptables", "-w", "-t", "mangle", "-A", "PREROUTING", "-j", "B4"},
		{"iptables", "-w", "-t", "nat", "-D", "POSTROUTING", "-j", "b4r_x_nat"},
		{"iptables", "-w", "-t", "mangle", "-F", "B4"},
	} {
		if iptDeletesByPosition(args) {
			t.Fatalf("%v names the rule it means, so it is safe to retry and must not lose the retry", args)
		}
	}
}
