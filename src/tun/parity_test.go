package tun

import (
	"fmt"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/tables"
)

func TestJumpSitsAboveRouting(t *testing.T) {
	above := `-P PREROUTING ACCEPT
-A PREROUTING -j B4_TUN
-A PREROUTING -j b4r_abc_pre
-A PREROUTING -j b4r_def_pre`
	if !jumpSitsAboveRouting(above, "B4_TUN") {
		t.Error("capture jump ahead of the routing chains was not detected")
	}

	below := `-P PREROUTING ACCEPT
-A PREROUTING -j b4r_abc_pre
-A PREROUTING -j b4r_def_pre
-A PREROUTING -j B4_TUN`
	if jumpSitsAboveRouting(below, "B4_TUN") {
		t.Error("capture jump already below the routing chains reported as above")
	}

	if jumpSitsAboveRouting(below, "B4_TUN_GATE") {
		t.Error("absent jump should not report as misordered")
	}

	noRouting := `-P PREROUTING ACCEPT
-A PREROUTING -j B4_TUN`
	if jumpSitsAboveRouting(noRouting, "B4_TUN") {
		t.Error("no routing chains means nothing to sit above")
	}
}

func TestJumpSitsAboveRoutingIgnoresSubstringTargets(t *testing.T) {
	dump := `-A PREROUTING -m set --match-set b4r_abc_v4 dst -j MARK --set-xmark 0x1/0x27fff
-A PREROUTING -j B4_TUN`
	if jumpSitsAboveRouting(dump, "B4_TUN") {
		t.Error("a rule merely mentioning a b4r_ set name must not count as a routing jump")
	}
}

func TestReinjectPlainMarkMatchExcludesRoutingMarks(t *testing.T) {
	got := reinjectPlainMarkMatch()
	want := fmt.Sprintf("0x%x/0x%x", engine.ReinjectMarkBit, uint32(engine.ReinjectMarkBit)|tables.RouteClaimedMarkMask())
	if got != want {
		t.Fatalf("reinjectPlainMarkMatch = %s, want %s", got, want)
	}

	mask := uint32(engine.ReinjectMarkBit) | tables.RouteClaimedMarkMask()
	value := uint32(engine.ReinjectMarkBit)

	plain := uint32(engine.ReinjectMarkBit) | 0x8000
	if plain&mask != value {
		t.Errorf("a plain reinject (0x%x) must still match the bypass rule", plain)
	}

	routed := uint32(engine.ReinjectMarkBit) | 0x8000 | 0x101
	if routed&mask == value {
		t.Errorf("a routing-claimed reinject (0x%x) must fall through to the per-set rule", routed)
	}

	if uint32(engine.ReinjectMarkBit)&tables.RouteClaimedMarkMask() != 0 {
		t.Error("the reinject bit must not overlap the per-set routing mark bits")
	}
}

func TestCaptureInputsEqual(t *testing.T) {
	base := captureInputs{
		tcpPorts:     []string{"443"},
		udpPorts:     []string{"443"},
		tcpLimit:     19,
		udpLimit:     8,
		selectedMACs: []string{"AA:BB:CC:DD:EE:FF"},
	}
	r := &routeManager{
		tcpPorts:     []string{"443"},
		udpPorts:     []string{"443"},
		tcpLimit:     19,
		udpLimit:     8,
		selectedMACs: []string{"AA:BB:CC:DD:EE:FF"},
	}
	if !r.captureInputsEqual(base) {
		t.Fatal("identical inputs reported as changed")
	}

	changed := base
	changed.tcpPorts = []string{"443", "8443"}
	if r.captureInputsEqual(changed) {
		t.Error("a new tcp port must be seen as a change")
	}

	changed = base
	changed.tcpLimit = 4
	if r.captureInputsEqual(changed) {
		t.Error("a new connbytes limit must be seen as a change")
	}

	changed = base
	changed.replyCapture = true
	if r.captureInputsEqual(changed) {
		t.Error("enabling reply capture must be seen as a change")
	}

	changed = base
	changed.selectedMACs = []string{"11:22:33:44:55:66"}
	if r.captureInputsEqual(changed) {
		t.Error("a new device whitelist must be seen as a change")
	}
}

func TestTUNDeviceDefault(t *testing.T) {
	if (config.TUNConfig{}).Device() != defaultDeviceName {
		t.Errorf("an unset device name must resolve to %s", defaultDeviceName)
	}
	if (config.TUNConfig{DeviceName: "tun9"}).Device() != "tun9" {
		t.Error("an explicit device name must be kept")
	}
}

func TestRoutingJumpFloor(t *testing.T) {
	if got := routingJumpFloor("-P PREROUTING ACCEPT"); got != 1 {
		t.Errorf("with no routing jumps the capture jump belongs at position 1, got %d", got)
	}

	dump := `-P PREROUTING ACCEPT
-A PREROUTING -j FOREIGN
-A PREROUTING -j b4r_abc_pre
-A PREROUTING -j b4r_def_pre
-A PREROUTING -j OTHER`
	if got := routingJumpFloor(dump); got != 4 {
		t.Errorf("the capture jump belongs just after the last routing jump (4), got %d", got)
	}
}

func TestJumpPosition(t *testing.T) {
	dump := `-P PREROUTING ACCEPT
-A PREROUTING -j b4r_abc_pre
-A PREROUTING -j B4_TUN`
	if got := jumpPosition(dump, "B4_TUN"); got != 2 {
		t.Errorf("jumpPosition = %d, want 2", got)
	}
	if got := jumpPosition(dump, "B4_TUN_GATE"); got != 0 {
		t.Errorf("an absent jump must report position 0, got %d", got)
	}
}
