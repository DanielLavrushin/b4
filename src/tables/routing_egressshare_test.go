package tables

import (
	"strings"
	"testing"
)

func captureEgressRelease(t *testing.T, cache map[string]routeState, owned map[string]bool, iface, ip string) []string {
	t.Helper()

	origRun := runLogged
	origCache := routeRuleCache
	origOwned := routeOwnedAddrs
	t.Cleanup(func() {
		runLogged = origRun
		routeRuleCache = origCache
		routeOwnedAddrs = origOwned
	})

	var ran []string
	runLogged = func(op string, args ...string) {
		ran = append(ran, strings.Join(args, " "))
	}

	routeRuleCache = cache
	routeOwnedAddrs = owned

	routeReleaseEgressAddress(iface, ip)
	return ran
}

func egressShareState(iface, egressIP string) routeState {
	st := egressIPState(egressIP)
	st.iface = iface
	return st
}

func TestEgressAddressKeptWhileAnotherSetStillUsesIt(t *testing.T) {
	const iface, ip = "eth0", "192.0.2.10"
	key := routeEgressAddrKey(iface, ip)

	cache := map[string]routeState{
		"set-a": egressShareState(iface, ip),
		"set-b": egressShareState(iface, ip),
	}
	owned := map[string]bool{key: true}

	ran := captureEgressRelease(t, cache, owned, iface, ip)

	for _, cmd := range ran {
		if strings.Contains(cmd, "addr del") {
			t.Fatalf("the address was removed while another set still uses it: %q", cmd)
		}
	}
	if !routeOwnedAddrs[key] {
		t.Fatal("ownership was dropped while another set still uses the address, so the last release cannot clean it up")
	}
}

func TestEgressAddressRemovedForTheLastSetUsingIt(t *testing.T) {
	const iface, ip = "eth0", "192.0.2.10"
	key := routeEgressAddrKey(iface, ip)

	cache := map[string]routeState{"set-a": egressShareState(iface, ip)}
	owned := map[string]bool{key: true}

	ran := captureEgressRelease(t, cache, owned, iface, ip)

	found := false
	for _, cmd := range ran {
		if strings.Contains(cmd, "addr del") && strings.Contains(cmd, ip) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the last set using the address must release it, commands were %q", ran)
	}
	if routeOwnedAddrs[key] {
		t.Fatal("ownership must be dropped once the address is removed")
	}
}

func TestEgressAddressNotOwnedByB4IsNeverRemoved(t *testing.T) {
	const iface, ip = "eth0", "192.0.2.10"

	cache := map[string]routeState{"set-a": egressShareState(iface, ip)}

	ran := captureEgressRelease(t, cache, map[string]bool{}, iface, ip)

	if len(ran) != 0 {
		t.Fatalf("an address b4 did not add must be left alone, commands were %q", ran)
	}
}

func TestEgressAddressOnAnotherInterfaceDoesNotCount(t *testing.T) {
	const iface, ip = "eth0", "192.0.2.10"
	key := routeEgressAddrKey(iface, ip)

	cache := map[string]routeState{
		"set-a": egressShareState(iface, ip),
		"set-b": egressShareState("eth1", ip),
	}
	owned := map[string]bool{key: true}

	ran := captureEgressRelease(t, cache, owned, iface, ip)

	found := false
	for _, cmd := range ran {
		if strings.Contains(cmd, "addr del") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a set holding the same address on a different interface must not keep this one alive, commands were %q", ran)
	}
}
