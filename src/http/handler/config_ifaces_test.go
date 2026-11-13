package handler

import (
	"net"
	"reflect"
	"testing"
)

func TestGetSystemInterfacesListsDownInterfacesWithTheirState(t *testing.T) {
	savedList, savedAddrs, savedLabels := systemInterfaces, interfaceHasAddrs, interfaceLabels
	t.Cleanup(func() {
		systemInterfaces, interfaceHasAddrs, interfaceLabels = savedList, savedAddrs, savedLabels
	})

	up := net.FlagUp | net.FlagRunning
	systemInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "lo", Flags: up | net.FlagLoopback},
			{Name: "nwg4", Flags: up},
			{Name: "nwg7"},
			{Name: "eth2.2", Flags: up},
			{Name: "br0", Flags: up},
			{Name: "br1"},
			{Name: "tunl0"},
			{Name: "dummy0"},
			{Name: "ra2"},
			{Name: "gre0", Flags: up},
		}, nil
	}
	withAddrs := map[string]bool{"nwg4": true, "nwg7": true, "eth2.2": true, "gre0": true}
	interfaceHasAddrs = func(iface net.Interface) bool { return withAddrs[iface.Name] }
	interfaceLabels = func() map[string]string {
		return map[string]string{"nwg4": "WARP", "nwg7": "MY_GER_AM", "eth2.2": "CACTUS"}
	}

	names, details, err := getSystemInterfaces(false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"br0", "eth2.2", "nwg4", "nwg7"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	want := map[string]IfaceDetail{
		"br0":    {Up: true},
		"eth2.2": {Up: true, Label: "CACTUS"},
		"nwg4":   {Up: true, Label: "WARP"},
		"nwg7":   {Up: false, Label: "MY_GER_AM"},
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("details = %v, want %v", details, want)
	}
}

func TestGetSystemInterfacesInDockerHidesDownInterfacesWithoutAddress(t *testing.T) {
	savedList, savedAddrs, savedLabels := systemInterfaces, interfaceHasAddrs, interfaceLabels
	t.Cleanup(func() {
		systemInterfaces, interfaceHasAddrs, interfaceLabels = savedList, savedAddrs, savedLabels
	})
	systemInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Name: "eth0", Flags: net.FlagUp},
			{Name: "eth1"},
			{Name: "wg0"},
			{Name: "docker0", Flags: net.FlagUp},
		}, nil
	}
	withAddrs := map[string]bool{"wg0": true}
	interfaceHasAddrs = func(iface net.Interface) bool { return withAddrs[iface.Name] }
	interfaceLabels = func() map[string]string { return nil }

	names, details, _ := getSystemInterfaces(true)
	if want := []string{"docker0", "eth0", "wg0"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	want := map[string]IfaceDetail{
		"docker0": {Up: true},
		"eth0":    {Up: true},
		"wg0":     {Up: false},
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("details = %v, want %v", details, want)
	}
}
