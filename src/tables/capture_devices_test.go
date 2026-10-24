package tables

import (
	"maps"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var (
	gateMACDevice = config.Device{MAC: "96:AB:60:74:88:DE", Selected: true}
	gateManualV4  = config.Device{MAC: "02:B4:C0:A8:33:02", IP: "192.168.51.2", Selected: true, IsManual: true}
	gateManualV6  = config.Device{MAC: "02:B4:00:00:00:02", IP: "fd42:2::2", Selected: true, IsManual: true}
)

func captureDevicesConfig(backend string, exclude bool, devices ...config.Device) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backend
	cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = true, true
	cfg.Queue.Devices.Enabled = true
	cfg.Queue.Devices.WhiteIsBlack = exclude
	cfg.Queue.Devices.Devices = devices
	return &cfg
}

func TestTheDeviceGateMatchesDevicesAddedByHandInTheirOwnFamilyOnIptables(t *testing.T) {
	for _, exclude := range []bool{false, true} {
		t.Run(map[bool]string{false: "allow", true: "exclude"}[exclude], func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureDevicesConfig(backendIPTables, exclude, gateMACDevice, gateManualV4, gateManualV6)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			for bin, source := range map[string]string{backendIPTables: "-s 192.168.51.2", backendIP6Tables: "-s fd42:2::2"} {
				mangle := h.iptTable(bin, "mangle")
				if got := iptJumpsTo(mangle.chain("FORWARD"), iptForwardChainName); got != 1 {
					t.Errorf("%s: mangle FORWARD jumps to %s %d times, want once", bin, iptForwardChainName, got)
				}
				if got := iptJumpsToB4(mangle.chain("FORWARD")) + iptJumpsToB4(mangle.chain("POSTROUTING")); got != 0 {
					t.Errorf("%s: mangle FORWARD and POSTROUTING jump to %s %d times, which bypasses the device filter", bin, iptChainName, got)
				}
				matches := []string{"-m mac --mac-source 96:AB:60:74:88:DE", source}
				got := slices.Clone(mangle.chain(iptForwardChainName).rules)
				var want []string
				if exclude {
					if n := len(got); n == 0 || got[n-1] != "-j "+iptChainName {
						t.Fatalf("%s: %s holds %q; its last rule must jump to %s for the devices it does not exclude", bin, iptForwardChainName, got, iptChainName)
					}
					got = got[:len(got)-1]
					slices.Sort(got)
					for _, m := range matches {
						want = append(want, m+" -j RETURN")
					}
					slices.Sort(want)
				} else {
					for _, m := range matches {
						want = append(want, m+" -j "+iptChainName)
					}
				}
				if !slices.Equal(got, want) {
					t.Errorf("%s: %s holds %q, want %q: each family matches its own devices added by hand, by address, and the others by MAC", bin, iptForwardChainName, got, want)
				}
			}
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor finds rules missing right after they were installed, so it rebuilds them on every check")
			}
		})
	}
}

func TestAnAllowListWithoutAnIPv6DeviceQueuesNoForwardedIPv6(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	cfg := captureDevicesConfig(backendIPTables, false, gateManualV4)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	v6 := h.iptTable(backendIP6Tables, "mangle")
	if rules := v6.chain(iptForwardChainName).rules; len(rules) != 0 {
		t.Errorf("ip6tables %s holds %q; the only selected device has an IPv4 address alone, so no forwarded IPv6 packet may reach %s", iptForwardChainName, rules, iptChainName)
	}
	if got := iptJumpsToB4(v6.chain("POSTROUTING")); got != 0 {
		t.Errorf("ip6tables mangle POSTROUTING jumps to %s %d times, so every device's IPv6 is queued although the filter selects one device", iptChainName, got)
	}
	m := &Monitor{}
	if !m.checkRules(cfg) {
		t.Fatalf("the monitor wants the IPv6 jump to %s that the filter never emits, so it rebuilds the rules on every check", iptChainName)
	}
	h.iptTable(backendIPTables, "mangle").chain(iptForwardChainName).rules = nil
	if m.checkRules(cfg) {
		t.Errorf("the IPv4 jump to %s for the selected device is gone, yet the monitor finds nothing missing", iptChainName)
	}
}

func TestAClearWithAnotherConfigStillTakesTheDeviceGateAway(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	gated := captureDevicesConfig(backendIPTables, true, gateMACDevice, gateManualV4, gateManualV6)
	if err := AddRules(gated); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	plain := captureDevicesConfig(backendIPTables, false)
	plain.Queue.Devices.Enabled = false
	if err := ClearRules(plain); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		mangle := h.iptTable(bin, "mangle")
		if c := mangle.chain(iptForwardChainName); c != nil {
			t.Errorf("%s: the clear left %s behind, holding %q", bin, iptForwardChainName, c.rules)
		}
		for _, rule := range mangle.chain("FORWARD").rules {
			switch goldenIptTarget(rule) {
			case iptChainName, iptForwardChainName, "RETURN":
				t.Errorf("%s: the clear left %q in mangle FORWARD; a stale RETURN there skips every later mangle FORWARD rule for that device", bin, rule)
			}
		}
	}
}

func captureHooks(h *goldenHost) map[string][]string {
	hooks := map[string][]string{}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		mangle := h.iptTable(bin, "mangle")
		for _, chain := range []string{"OUTPUT", "POSTROUTING", "FORWARD", iptForwardChainName} {
			if c := mangle.chain(chain); c != nil {
				hooks[bin+" "+chain] = slices.Clone(c.rules)
			}
		}
	}
	return hooks
}

func TestSavingADeviceFilterChangeLeavesTheHooksOfTheNewFilter(t *testing.T) {
	off := captureDevicesConfig(backendIPTables, false, gateMACDevice)
	off.Queue.Devices.Enabled = false
	allowOne := captureDevicesConfig(backendIPTables, false, gateMACDevice)
	allowTwo := captureDevicesConfig(backendIPTables, false, gateMACDevice, gateManualV4)
	excludeTwo := captureDevicesConfig(backendIPTables, true, gateMACDevice, gateManualV4)
	for _, c := range []struct {
		name     string
		from, to *config.Config
	}{
		{"turning the filter on", off, allowTwo},
		{"turning the filter off", allowTwo, off},
		{"selecting a device added by hand", allowOne, allowTwo},
		{"excluding the devices instead", allowTwo, excludeTwo},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			if err := AddRules(c.from); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			m, ptr := newLockTestMonitor(t)
			ptr.Store(c.to)
			if config.FirewallRefreshNeeded(c.from, c.to) {
				if err := RefreshRules(c.to); err != nil {
					t.Fatalf("RefreshRules: %v", err)
				}
			}
			m.ensureRules(false)
			got := captureHooks(h)

			if err := ClearRules(c.to); err != nil {
				t.Fatalf("ClearRules: %v", err)
			}
			if err := AddRules(c.to); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			want := captureHooks(h)
			if !maps.EqualFunc(got, want, slices.Equal) {
				t.Errorf("after the save and a monitor check the capture hooks are\n%q\nwant the hooks of the new filter\n%q\nstacked hooks queue the router's own packets twice, and missing ones leave the change for a restart", got, want)
			}
		})
	}
}

func TestNFTablesTheDeviceGateMatchesDevicesAddedByHandByAddress(t *testing.T) {
	for _, exclude := range []bool{false, true} {
		t.Run(map[bool]string{false: "allow", true: "exclude"}[exclude], func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureDevicesConfig(backendNFTables, exclude, gateMACDevice, gateManualV4, gateManualV6)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			table := nftEmittedTable(h.take())
			matches := []string{"ether saddr 96:ab:60:74:88:de", "ip saddr 192.168.51.2", "ip6 saddr fd42:2::2"}
			var want []string
			for _, m := range matches {
				if exclude {
					want = append(want, m+" return")
				} else {
					want = append(want, m+" jump "+nftChainName)
				}
			}
			if exclude {
				want = append(want, "jump "+nftChainName)
			}
			if got := table.chains["forward"]; !slices.Equal(got, want) {
				t.Errorf("forward holds %q, want %q: a device added by hand has no MAC address on the wire, so only its address matches it", got, want)
			}
			if got := table.chains["postrouting"]; len(got) != 0 {
				t.Errorf("postrouting holds %q, so every device is queued although the filter selects devices", got)
			}
			serveNftListings(t, table)
			if !(&Monitor{}).checkRules(cfg) {
				t.Errorf("the monitor finds rules missing right after they were installed, so it rebuilds them on every check")
			}
		})
	}
}
