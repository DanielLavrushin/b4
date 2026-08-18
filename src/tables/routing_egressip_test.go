package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func egressIPState(egressIP string) routeState {
	return routeState{
		mode:      config.RoutingModeInterface,
		mark:      0x2bd8,
		table:     136,
		iface:     "lo",
		egressIP:  egressIP,
		setV4:     "b4r_test_v4",
		setV6:     "b4r_test_v6",
		chainSNAT: "b4r_test_snat",
	}
}

func TestEgressIP_EmptyKeepsMasquerade(t *testing.T) {
	be := &mockRouteBackend{}
	routeAddEgressRules(be, egressIPState(""), true, true)

	if len(be.snat) != 0 {
		t.Fatalf("a set with no egress IP must not emit SNAT: %+v", be.snat)
	}
	if len(be.masq) != 2 {
		t.Fatalf("expected a masquerade rule per family, got %+v", be.masq)
	}
}

func TestEgressIP_EmitsSNATDiscriminatedByTheSetNotTheSharedMark(t *testing.T) {
	be := &mockRouteBackend{}
	routeAddEgressRules(be, egressIPState("127.0.0.1"), true, false)

	if len(be.snat) != 1 {
		t.Fatalf("expected one SNAT rule for the v4 family, got %+v", be.snat)
	}
	r := be.snat[0]
	if r.srcIP != "127.0.0.1" {
		t.Errorf("SNAT source is %q, want 127.0.0.1", r.srcIP)
	}
	if r.iface != "lo" {
		t.Errorf("SNAT rule dropped the output interface match (%q); on multi-WAN failover it would stamp this source onto the other uplink", r.iface)
	}
	if r.setName != "b4r_test_v4" {
		t.Errorf("SNAT rule is keyed on %q; it must select on the per-set ipset, because sets sharing an egress interface also share a fwmark", r.setName)
	}
}

func TestEgressIP_TwoSetsSharingAMarkStaySeparate(t *testing.T) {
	first := egressIPState("127.0.0.1")
	second := egressIPState("127.0.0.1")
	second.setV4 = "b4r_other_v4"
	second.chainSNAT = "b4r_other_snat"

	if first.mark != second.mark {
		t.Fatal("this test only means something while sets on one interface share a fwmark")
	}

	be := &mockRouteBackend{}
	routeAddEgressRules(be, first, true, false)
	routeAddEgressRules(be, second, true, false)

	if len(be.snat) != 2 {
		t.Fatalf("expected one SNAT rule per set, got %+v", be.snat)
	}
	if be.snat[0].setName == be.snat[1].setName {
		t.Errorf("both sets emitted the same selector %q, so whichever rule lands first would rewrite the other set's traffic too", be.snat[0].setName)
	}
}

func TestEgressIP_UnknownAddressFallsBackToMasquerade(t *testing.T) {
	be := &mockRouteBackend{}
	routeAddEgressRules(be, egressIPState("203.0.113.7"), true, false)

	if len(be.snat) != 0 {
		t.Errorf("an address that is on no local interface must not be used as a SNAT source; replies to it would never arrive: %+v", be.snat)
	}
	if len(be.masq) != 1 {
		t.Fatalf("expected the masquerade fallback, got %+v", be.masq)
	}
}

func TestEgressIP_OnlyRewritesItsOwnFamily(t *testing.T) {
	be := &mockRouteBackend{}
	routeAddEgressRules(be, egressIPState("127.0.0.1"), true, true)

	if len(be.snat) != 1 || be.snat[0].v6 {
		t.Fatalf("a v4 egress IP must produce exactly one v4 SNAT rule, got %+v", be.snat)
	}
	if len(be.masq) != 1 || !be.masq[0].v6 {
		t.Fatalf("the v6 family has no egress IP, so it must keep masquerading, got %+v", be.masq)
	}
}

func TestEgressIP_MovesTheNATJumpAheadOfWhateverElseMasquerades(t *testing.T) {
	postJump := func(st routeState) mockRouteJump {
		be := &mockRouteBackend{}
		routeEnsureChainJumps(be, st, routeDeviceGate{})
		for _, j := range be.jumps {
			if j.baseChain == "POSTROUTING" {
				return j
			}
		}
		t.Fatal("the set's nat chain is never hung off POSTROUTING")
		return mockRouteJump{}
	}

	if postJump(egressIPState("")).atTop {
		t.Error("without an egress IP the nat jump must stay appended, behind whatever the firmware put in nat POSTROUTING")
	}
	if !postJump(egressIPState("127.0.0.1")).atTop {
		t.Error("with an egress IP the nat jump must be inserted at the top; a MASQUERADE above it terminates the nat table and the source rewrite would silently never happen")
	}
}
