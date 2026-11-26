package tables

import (
	"strings"
	"testing"
)

const fakeMangleRejection = "No chain/target/match by that name"

func TestKmodHintAfterRejectionLeavesTheRejectionToTheCaller(t *testing.T) {
	fx := newKmodFixture(t)
	fx.noLibModules()
	fx.stubContainerModprobe()
	if err := loadKernelModule("xt_DSCP"); err == nil {
		t.Fatal("modprobe without /lib/modules loaded xt_DSCP")
	}
	out := "iptables v1.8.13 (legacy): Couldn't load target `DSCP':No such file or directory"
	kmodNoteRejected("xt_DSCP", "iptables", out)

	hint := kmodHintAfterRejection([]string{"xt_DSCP"})
	if strings.Contains(hint, "Couldn't load target") {
		t.Errorf("the hint repeats the rejection the caller already printed: %s", hint)
	}
	if !strings.Contains(hint, "xt_DSCP: modprobe could not load it") {
		t.Errorf("the hint lost the load error: %s", hint)
	}
	reasons := strings.Join(KernelModuleReasons([]string{"xt_DSCP"}), "; ")
	if !strings.Contains(reasons, "Couldn't load target") {
		t.Errorf("System Info lost the rejection, which only it prints: %s", reasons)
	}
}

func TestKmodHintAfterRejectionNamesAModuleThatIsPresent(t *testing.T) {
	fx := newKmodFixture(t)
	fx.loaded("xt_DSCP")
	kmodNoteRejected("xt_DSCP", "iptables", "iptables: "+fakeMangleRejection+".")
	hint := kmodHintAfterRejection([]string{"xt_DSCP"})
	if !strings.HasSuffix(hint, "; xt_DSCP: the kernel module is present") || strings.Contains(hint, fakeMangleRejection) {
		t.Errorf("hint = %q", hint)
	}
}

func assertRejectionOnce(t *testing.T, text, rejection, context string) {
	t.Helper()
	if !strings.Contains(text, context) {
		t.Fatalf("the message lost its own wording %q: %s", context, text)
	}
	if n := strings.Count(text, rejection); n != 1 {
		t.Errorf("the rejection appears %d times, want once: %s", n, text)
	}
}

func TestDSCPRejectionIsPrintedOnce(t *testing.T) {
	t.Run("global stamp", func(t *testing.T) {
		newKmodFixture(t)
		f := newFakeMangle(backendIPTables, backendIP6Tables)
		f.rejectDSCP[backendIP6Tables] = true
		installFakeMangle(t, f)
		err := applyDSCPFor(dscpTestConfig(true, 7), backendIPTables)
		if err == nil {
			t.Fatal("a rejected DSCP target must be reported")
		}
		assertRejectionOnce(t, err.Error(), fakeMangleRejection, "the DSCP target was rejected")
	})
	t.Run("set values", func(t *testing.T) {
		newKmodFixture(t)
		h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
		h.mangle.rejectDSCP[backendIPTables] = true
		cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 25, "10.1.2.0/24"))
		_, err := dscpIptApplyPlan(cfg, backendIPTables, dscpPlanFor(cfg), nil)
		if err == nil {
			t.Fatal("a rejected DSCP target must be reported")
		}
		assertRejectionOnce(t, err.Error(), fakeMangleRejection, "the DSCP target was rejected")
	})
	t.Run("Discovery probes", func(t *testing.T) {
		newKmodFixture(t)
		f := newFakeMangle(backendIPTables)
		f.rejectDSCP[backendIPTables] = true
		installFakeMangle(t, f)
		err := discoveryIptStamp(backendIPTables, "0x8001/0xffffffff", "0x8002/0xffffffff", &discoveryStamp{value: 25, v4: true, v6: true})
		if err == nil {
			t.Fatal("a rejected DSCP target must be reported")
		}
		assertRejectionOnce(t, err.Error(), fakeMangleRejection, "into Discovery's probes")
	})
}

func TestAddressTypeRejectionIsPrintedOnce(t *testing.T) {
	newKmodFixture(t)
	localGuardReset(t)
	logs := captureTablesLog(t)
	routeLocalGuardReject("nft", "nft_fib_inet", "Error: Could not process rule: No such file or directory")
	assertRejectionOnce(t, logs.String(), "Could not process rule", "rejected the address-type match")
}
