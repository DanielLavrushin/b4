package handler

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func listedByMAC(t *testing.T, devices []DeviceInfo) map[string]DeviceInfo {
	t.Helper()
	out := make(map[string]DeviceInfo, len(devices))
	for _, d := range devices {
		key := normalizeMAC(d.MAC)
		if _, dup := out[key]; dup {
			t.Fatalf("device %s listed twice", d.MAC)
		}
		out[key] = d
	}
	return out
}

func TestListDevicesKeepsEveryStoredDeviceVisible(t *testing.T) {
	dc := &config.DevicesConfig{Devices: []config.Device{
		{MAC: "AC:BB:CC:00:00:01", Name: "Bedroom TV"},
		{MAC: "AC:BB:CC:00:00:02", Selected: true},
		{MAC: "AC:BB:CC:00:00:03", MSSClamp: 1200},
		{MAC: "ac:bb:cc:00:00:04", Name: "Laptop", MSSClamp: 900},
		{MAC: ""},
	}}
	mappings := map[string]string{"192.168.50.10": "AC:BB:CC:00:00:04"}
	hostnames := map[string]string{"ac:bb:cc:00:00:02": "old-phone"}

	got := listedByMAC(t, listDevices(dc, mappings, hostnames, nil))
	if len(got) != 4 {
		t.Fatalf("want 4 devices, got %d: %+v", len(got), got)
	}

	laptop := got["ACBBCC000004"]
	if !laptop.IsOnline || laptop.IP != "192.168.50.10" || laptop.Alias != "Laptop" || laptop.MSSClamp != 900 {
		t.Fatalf("online stored device lost its settings: %+v", laptop)
	}

	for _, mac := range []string{"ACBBCC000001", "ACBBCC000002", "ACBBCC000003"} {
		d, ok := got[mac]
		if !ok {
			t.Fatalf("stored device %s is not listed", mac)
		}
		if d.IsOnline {
			t.Fatalf("device %s is not in the ARP table but listed online", mac)
		}
	}
	if got["ACBBCC000002"].Hostname != "old-phone" {
		t.Fatalf("offline device lost its lease hostname: %+v", got["ACBBCC000002"])
	}
	if got["ACBBCC000003"].MSSClamp != 1200 {
		t.Fatalf("offline device lost its MSS value: %+v", got["ACBBCC000003"])
	}
}

func TestListDevicesVendor(t *testing.T) {
	dc := &config.DevicesConfig{Devices: []config.Device{{MAC: "02:11:22:33:44:55", Name: "Phone"}}}
	mappings := map[string]string{"192.168.1.2": "00:11:22:33:44:55"}
	lookup := func(string) string { return "Acme" }

	got := listedByMAC(t, listDevices(dc, mappings, nil, lookup))
	if d := got["001122334455"]; d.Vendor != "Acme" || d.IsPrivate {
		t.Fatalf("vendor lookup not applied: %+v", d)
	}
	if d := got["021122334455"]; d.Vendor != "Private" || !d.IsPrivate {
		t.Fatalf("randomized MAC not marked private: %+v", d)
	}

	got = listedByMAC(t, listDevices(dc, mappings, nil, nil))
	if d := got["001122334455"]; d.Vendor != "" {
		t.Fatalf("vendor filled while lookup is off: %+v", d)
	}
}
