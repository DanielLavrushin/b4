package tables

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func captureCollectedKeep(t *testing.T) {
	t.Helper()
	rulesMu.Lock()
	saved, synced := captureCollectedApplied, captureCollectedSynced
	captureCollectedApplied, captureCollectedSynced = map[string]*captureCollectedSet{}, nil
	rulesMu.Unlock()
	t.Cleanup(func() {
		rulesMu.Lock()
		captureCollectedApplied, captureCollectedSynced = saved, synced
		rulesMu.Unlock()
	})
}

func captureCollectedTestConfig(backend string, sources ...string) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backend
	cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = true, true
	clamp := config.NewSetConfig()
	clamp.Id, clamp.Name, clamp.Enabled = "collected-clamp", "collected clamp", true
	clamp.Targets.UseCollectedIPs = true
	clamp.Targets.SourceDevices = sources
	clamp.MSSClamp.Enabled, clamp.MSSClamp.Size = true, 88
	dup := config.NewSetConfig()
	dup.Id, dup.Name, dup.Enabled = "collected-dup", "collected dup", true
	dup.Targets.UseCollectedIPs = true
	dup.TCP.Duplicate.Enabled, dup.TCP.Duplicate.Count = true, 2
	cfg.Sets = []*config.SetConfig{&clamp, &dup}
	return &cfg
}

func captureCollectedNames() []string {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	names := make([]string, 0, len(captureCollectedApplied))
	for name := range captureCollectedApplied {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func captureCollectedEntries(name string) []string {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if s := captureCollectedApplied[name]; s != nil {
		return slices.Sorted(slices.Values(s.entries))
	}
	return nil
}

var captureCollectedAllNames = []string{"b4_dup_v4c", "b4_dup_v6c", "b4_mss_0_v4c", "b4_mss_0_v6c"}

func TestACollectedOnlyClampAndDuplicationGetTheirOwnCaptureSets(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("a clamp and a duplication set whose only addresses are collected ones must validate: %v", err)
			}
			h.take()
			if err := AddRules(cfg.WithCollectedIPs(nil)); err != nil {
				t.Fatal(err)
			}
			lines := h.take()
			all := strings.Join(lines, "\n")
			for _, name := range captureCollectedAllNames {
				if backend == backendNFTables {
					created := false
					for _, line := range lines {
						if strings.Contains(line, "add set inet "+nftTableName+" "+name+" ") {
							created = true
							if strings.Contains(line, "interval") || strings.Contains(line, "timeout") || strings.Contains(line, "auto-merge") {
								t.Errorf("%s must be a plain address set, got %q", name, line)
							}
						}
					}
					if !created || !strings.Contains(all, "@"+name+" ") {
						t.Errorf("the firewall built with an empty pool has no set %s matched by a rule, so a collected address could join the clamp or the duplication only through a rebuild:\n%s", name, all)
					}
					continue
				}
				family := "inet"
				if strings.HasSuffix(name, "6c") {
					family = "inet6"
				}
				if !strings.Contains(all, "ipset create "+name+" hash:ip family "+family) || !strings.Contains(all, "--match-set "+name+" ") {
					t.Errorf("the firewall built with an empty pool has no hash:ip set %s matched by a rule, so a collected address could join the clamp or the duplication only through a rebuild:\n%s", name, all)
				}
			}
			if got := captureCollectedNames(); !slices.Equal(got, captureCollectedAllNames) {
				t.Errorf("the build recorded the capture sets %q, want %q", got, captureCollectedAllNames)
			}
		})
	}
}

func TestCollectedCaptureSetsFollowThePoolWithoutARebuild(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			if err := AddRules(cfg.WithCollectedIPs([]string{"203.0.113.7"})); err != nil {
				t.Fatal(err)
			}
			if got := captureCollectedEntries("b4_mss_0_v4c"); !slices.Equal(got, []string{"203.0.113.7"}) {
				t.Fatalf("the build filled b4_mss_0_v4c with %q, want the pool's address", got)
			}
			h.take()
			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{"203.0.113.7", "203.0.113.8", "2001:db8::8"})); err != nil {
				t.Fatal(err)
			}
			grown := h.take()
			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{"203.0.113.8"})); err != nil {
				t.Fatal(err)
			}
			shrunk := h.take()
			for _, forbidden := range []string{"flush", "destroy", "delete set", "delete table", "iptables", "ip6tables", "create"} {
				for _, line := range slices.Concat(grown, shrunk) {
					if strings.Contains(line, forbidden) {
						t.Errorf("a pool change ran %q; it must only add and delete elements", line)
					}
				}
			}
			for _, want := range []string{"b4_mss_0_v4c 203.0.113.8", "b4_mss_0_v6c 2001:db8::8", "b4_dup_v4c 203.0.113.8", "b4_dup_v6c 2001:db8::8"} {
				name, addr, _ := strings.Cut(want, " ")
				if !slices.ContainsFunc(grown, func(line string) bool {
					return strings.Contains(line, name) && strings.Contains(line, addr) && !strings.Contains(line, "del")
				}) {
					t.Errorf("the sync never added %s to %s:\n%s", addr, name, strings.Join(grown, "\n"))
				}
			}
			for _, gone := range []string{"b4_mss_0_v4c 203.0.113.7", "b4_mss_0_v6c 2001:db8::8", "b4_dup_v4c 203.0.113.7", "b4_dup_v6c 2001:db8::8"} {
				name, addr, _ := strings.Cut(gone, " ")
				if !slices.ContainsFunc(shrunk, func(line string) bool {
					return strings.Contains(line, "del") && strings.Contains(line, name) && strings.Contains(line, addr)
				}) {
					t.Errorf("the sync never deleted %s from %s:\n%s", addr, name, strings.Join(shrunk, "\n"))
				}
			}
			for name, want := range map[string][]string{
				"b4_mss_0_v4c": {"203.0.113.8"},
				"b4_mss_0_v6c": nil,
				"b4_dup_v4c":   {"203.0.113.8"},
				"b4_dup_v6c":   nil,
			} {
				if got := captureCollectedEntries(name); !slices.Equal(got, want) {
					t.Errorf("%s is recorded with %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestACollectedCaptureSetDeleteTheKernelRefusedIsRetried(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			if err := AddRules(cfg.WithCollectedIPs([]string{"203.0.113.7", "203.0.113.8"})); err != nil {
				t.Fatal(err)
			}
			refused := true
			inner, innerStdin, innerNft := run, runStdin, runNftStdin
			run = func(args ...string) (string, error) {
				if refused && slices.Contains(args, "element") && slices.Contains(args, "delete") {
					return "Error: Could not process rule: Device or resource busy", errors.New("exit status 1")
				}
				return inner(args...)
			}
			runStdin = func(stdin string, args ...string) error {
				if refused && strings.Contains(stdin, "del ") {
					return errors.New("ipset v7.19: Kernel error received: Resource busy")
				}
				return innerStdin(stdin, args...)
			}
			runNftStdin = func(script string) (string, error) {
				if refused && strings.Contains(script, "delete element") {
					return "Error: Could not process rule: Device or resource busy", errors.New("exit status 1")
				}
				return innerNft(script)
			}
			after := cfg.WithCollectedIPs([]string{"203.0.113.8"})
			if err := SyncCollectedCaptureSets(after); err == nil {
				t.Error("the kernel refused to delete 203.0.113.7, yet the sync reported the capture sets as current, so the installer queues no retry")
			}
			if got := captureCollectedEntries("b4_mss_0_v4c"); !slices.Contains(got, "203.0.113.7") {
				t.Errorf("a refused delete was forgotten: b4_mss_0_v4c is recorded with %q", got)
			}
			refused = false
			h.take()
			if err := SyncCollectedCaptureSets(after); err != nil {
				t.Fatalf("the next sync failed with deletes allowed again: %v", err)
			}
			if all := strings.Join(h.take(), "\n"); !strings.Contains(all, "203.0.113.7") {
				t.Errorf("the next sync never tried to delete 203.0.113.7 again:\n%s", all)
			}
			if got := captureCollectedEntries("b4_mss_0_v4c"); !slices.Equal(got, []string{"203.0.113.8"}) {
				t.Errorf("after the retried delete b4_mss_0_v4c is recorded with %q", got)
			}
		})
	}
}

func TestTurningUseCollectedIPsOffRemovesTheCaptureSets(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			if err := AddRules(cfg.WithCollectedIPs([]string{"203.0.113.7"})); err != nil {
				t.Fatal(err)
			}
			off := *cfg
			off.Sets = nil
			for _, set := range cfg.Sets {
				s := *set
				s.Targets.UseCollectedIPs = false
				s.Targets.IPs, s.Targets.IpsToMatch = []string{"198.51.100.1"}, []string{"198.51.100.1"}
				off.Sets = append(off.Sets, &s)
			}
			h.take()
			if err := RefreshRules(&off); err != nil {
				t.Fatal(err)
			}
			lines := h.take()
			all := strings.Join(lines, "\n")
			if backend == backendNFTables {
				if !strings.Contains(all, "delete table inet "+nftTableName) {
					t.Errorf("turning Use collected IPs off did not rebuild the capture table:\n%s", all)
				}
			} else {
				for _, name := range captureCollectedAllNames {
					if !strings.Contains(all, "ipset destroy "+name) {
						t.Errorf("turning Use collected IPs off never destroyed %s:\n%s", name, all)
					}
				}
			}
			cleared := false
			for _, line := range lines {
				if strings.Contains(line, "delete table") || strings.Contains(line, "ipset destroy") {
					cleared = true
				}
				if cleared && (strings.Contains(line, "add set inet "+nftTableName+" b4_") && strings.Contains(line, "c {") || strings.Contains(line, "ipset create b4_") && strings.Contains(line, "c hash:ip")) {
					t.Errorf("the rebuild without Use collected IPs created a capture set again: %q", line)
				}
			}
			if got := captureCollectedNames(); len(got) != 0 {
				t.Errorf("the capture sets %q are still recorded after Use collected IPs was turned off", got)
			}
			h.take()
			if err := SyncCollectedCaptureSets((&off).WithCollectedIPs([]string{"203.0.113.7"})); err != nil {
				t.Fatal(err)
			}
			if lines := h.take(); len(lines) != 0 {
				t.Errorf("a sync without capture sets ran %q", lines)
			}
		})
	}
}

func TestACollectedCaptureSetIsSkippedWhenIpsetIsUnusable(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	captureCollectedKeep(t)
	run = refuseDeletes(run)
	hasBinaryCache.Store("ipset", false)
	cfg := captureCollectedTestConfig(backendIPTables)
	if err := AddRules(cfg.WithCollectedIPs(nil)); err != nil {
		t.Fatalf("an unusable ipset must skip the collected capture sets, not fail the firewall: %v", err)
	}
	if got := captureCollectedNames(); len(got) != 0 {
		t.Errorf("the capture sets %q were recorded although ipset is unusable", got)
	}
	if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{"203.0.113.7"})); err != nil {
		t.Errorf("a pool change failed because ipset is unusable, so every collected install would fail: %v", err)
	}
}

func TestACollectedClampKeepsItsSourceScopeAndWinsOverDeviceAndGlobalClamps(t *testing.T) {
	const device = "AA:BB:CC:DD:EE:01"
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend, device)
			cfg.Queue.MSSClamp.Enabled, cfg.Queue.MSSClamp.Size = true, 1360
			cfg.Queue.Devices.Devices = []config.Device{{MAC: "AA:BB:CC:DD:EE:02", MSSClamp: 1200}}
			h.take()
			if err := AddRules(cfg.WithCollectedIPs([]string{"203.0.113.7"})); err != nil {
				t.Fatal(err)
			}
			var collected, device2, global = -1, -1, -1
			var scoped []string
			for i, line := range h.take() {
				isClamp := strings.Contains(line, "TCPMSS") || strings.Contains(line, "maxseg")
				written := strings.Contains(line, " -I ") || strings.Contains(line, "add rule")
				if !isClamp || !written {
					continue
				}
				switch {
				case strings.Contains(line, "b4_mss_0_v4c"):
					scoped = append(scoped, line)
					if collected < 0 {
						collected = i
					}
				case strings.Contains(strings.ToUpper(line), "EE:02"):
					if device2 < 0 {
						device2 = i
					}
				case strings.Contains(line, "1360"):
					if global < 0 {
						global = i
					}
				}
			}
			if len(scoped) == 0 || device2 < 0 || global < 0 {
				t.Fatalf("the build must write the collected-address, per-device and global clamps, or this test proves nothing; positions: collected %d, device %d, global %d", collected, device2, global)
			}
			for _, line := range scoped {
				if !strings.Contains(strings.ToUpper(line), device) {
					t.Errorf("a clamp rule for the collected addresses of a set scoped to %s has no source match: %q", device, line)
				}
			}
			if backend == backendIPTables {
				if collected > device2 || collected > global {
					t.Errorf("iptables inserts at the top and TCPMSS does not terminate, so the collected-address clamp must be written before the per-device and global ones to win over them; positions: collected %d, device %d, global %d", collected, device2, global)
				}
			} else if collected < device2 || collected < global {
				t.Errorf("nftables applies the last matching clamp, so the collected-address clamp must follow the per-device and global ones; positions: collected %d, device %d, global %d", collected, device2, global)
			}
		})
	}
}

func TestTheTUNMonitorAcceptsAClampOnCollectedAddressesAlone(t *testing.T) {
	cfg := newTUNTestConfig()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "collected-clamp", "collected clamp", true
	set.Targets.UseCollectedIPs = true
	set.MSSClamp.Enabled, set.MSSClamp.Size = true, 88
	cfg.Sets = []*config.SetConfig{&set}
	fw := stubTUNFirewall(t, map[string]string{
		"-t mangle -S OUTPUT":     "-P OUTPUT ACCEPT\n",
		"-t mangle -S FORWARD":    "-P FORWARD ACCEPT\n",
		"-t mangle -S PREROUTING": "-P PREROUTING ACCEPT\n",
	})
	run = refuseDeletes(run)
	captureCollectedKeep(t)
	view := cfg.WithCollectedIPs([]string{"203.0.113.7"})
	if err := ApplyMSSClampOnly(view); err != nil {
		t.Fatal(err)
	}
	if fw.called("_v4c") || fw.called("_v6c") {
		t.Fatalf("the TUN engine's MSS clamp wrote a collected capture set: %v", fw.calls)
	}
	m := newTUNTestMonitor(view)
	for tick := 0; tick < 2; tick++ {
		if !m.checkTUNRules() || m.tunLost.mss {
			t.Fatalf("tick %d: the TUN monitor finds a clamp on collected addresses alone missing, so it would rebuild it every tick", tick)
		}
	}
}

func TestTheRunningTUNEngineGetsNoCollectedCaptureSetsWithNFQUEUESaved(t *testing.T) {
	config.SetRunningEngineTUN(true)
	t.Cleanup(func() { config.SetRunningEngineTUN(false) })
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	captureCollectedKeep(t)
	run = refuseDeletes(run)
	cfg := captureCollectedTestConfig(backendIPTables)
	cfg.Queue.Mode = "nfqueue"
	h.take()
	if err := ApplyMSSClampOnly(cfg.WithCollectedIPs([]string{"203.0.113.7"})); err != nil {
		t.Fatal(err)
	}
	for _, line := range h.take() {
		if strings.Contains(line, "_v4c") || strings.Contains(line, "_v6c") {
			t.Errorf("with queue.mode saved as nfqueue and the TUN engine still running, the MSS clamp emitted a collected capture set; Collect does nothing until the restart: %q", line)
		}
	}
}

func TestTUNGetsNoCollectedCaptureSets(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	captureCollectedKeep(t)
	run = refuseDeletes(run)
	cfg := captureCollectedTestConfig(backendIPTables)
	cfg.Queue.Mode = "tun"
	h.take()
	if err := ApplyMSSClampOnly(cfg.WithCollectedIPs([]string{"203.0.113.7"})); err != nil {
		t.Fatal(err)
	}
	for _, line := range h.take() {
		if strings.Contains(line, "_v4c") || strings.Contains(line, "_v6c") {
			t.Errorf("the TUN engine's MSS clamp emitted a collected capture set: %q", line)
		}
	}
}

func TestTheMonitorChecksTheCollectedCaptureRules(t *testing.T) {
	t.Run(backendIPTables, func(t *testing.T) {
		captureCollectedKeep(t)
		orig := run
		t.Cleanup(func() { run = orig })
		present := true
		run = refuseDeletes(func(args ...string) (string, error) {
			if slices.Contains(args, "-C") && slices.Contains(args, "b4_mss_0_v4c") && !present {
				return "iptables: Bad rule (does a matching rule exist in that chain?).", errors.New("exit status 1")
			}
			return "", nil
		})
		rule := Rule{IPT: backendIPTables, Table: "mangle", Chain: "FORWARD", Action: "I", Spec: []string{"-p", "tcp", "--dport", "443", "-m", "set", "--match-set", "b4_mss_0_v4c", "dst", "--tcp-flags", "SYN,RST", "SYN", "-j", "TCPMSS", "--set-mss", "88"}}
		rulesMu.Lock()
		captureCollectedApplied["b4_mss_0_v4c"] = &captureCollectedSet{setID: "collected-clamp", rules: []Rule{rule}}
		rulesMu.Unlock()
		if !iptCaptureCollectedRulesPresent() {
			t.Fatal("the rule of a recorded capture set is in place, yet the check fails, so the monitor would rebuild every tick")
		}
		present = false
		if iptCaptureCollectedRulesPresent() {
			t.Fatal("the rule of a recorded capture set is gone, yet the check passes")
		}
	})
	t.Run(backendNFTables, func(t *testing.T) {
		captureCollectedKeep(t)
		dir := t.TempDir()
		listing := filepath.Join(dir, "listing")
		stub := "#!/bin/sh\ncase \"$*\" in\n*delete*) exit 1 ;;\n*\"list chain inet " + nftTableName + " forward\"*) cat " + listing + " ;;\nesac\nexit 0\n"
		if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		rulesMu.Lock()
		captureCollectedApplied["b4_mss_0_v4c"] = &captureCollectedSet{setID: "collected-clamp"}
		rulesMu.Unlock()
		write := func(text string) {
			if err := os.WriteFile(listing, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("table inet b4_mangle {\n\tchain forward {\n\t\tmeta nfproto ipv4 ip daddr @b4_mss_0_v4c tcp dport 443 tcp flags syn / syn,rst tcp option maxseg size set 88\n\t}\n}\n")
		n := NewNFTablesManager(captureCollectedTestConfig(backendNFTables))
		if !nftCaptureCollectedRulesPresent(n) {
			t.Fatal("the rule of a recorded capture set is in place, yet the check fails, so the monitor would rebuild every tick")
		}
		write("table inet b4_mangle {\n\tchain forward {\n\t\tmeta nfproto ipv4 ip daddr @b4_mss_0_v4 tcp dport 443 tcp flags syn / syn,rst tcp option maxseg size set 88\n\t}\n}\n")
		if nftCaptureCollectedRulesPresent(n) {
			t.Fatal("only the static set's rule is left, yet the check passes for the capture set")
		}
	})
}

const (
	captureSyncedA  = "203.0.113.7"
	captureSyncedB  = "203.0.113.8"
	captureSyncedC6 = "2001:db8::8"
)

func captureCollectedRestore(t *testing.T, backend string, published *config.Config) {
	t.Helper()
	m, ptr := newLockTestMonitor(t)
	m.backend = backend
	ptr.Store(published)
	rulesMu.Lock()
	err := m.restoreRules(m.config())
	rulesMu.Unlock()
	if err != nil {
		t.Fatalf("the monitor's restore failed: %v", err)
	}
}

func captureCollectedFilledWith(lines []string, name, addr string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		return strings.Contains(line, name) && strings.Contains(line, addr) && !strings.Contains(line, "del")
	})
}

func captureCollectedWant(t *testing.T, when string, want map[string][]string) {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if got := captureCollectedEntries(name); !slices.Equal(got, want[name]) {
			t.Errorf("%s: %s is recorded with %q, want %q", when, name, got, want[name])
		}
	}
}

func TestARestoreDuringACollectedInstallKeepsTheAddressesItAlreadySynced(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			published := cfg.WithCollectedIPs([]string{captureSyncedA})
			if err := AddRules(published); err != nil {
				t.Fatal(err)
			}
			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{captureSyncedA, captureSyncedB, captureSyncedC6})); err != nil {
				t.Fatal(err)
			}
			h.take()
			captureCollectedRestore(t, backend, published)
			lines := h.take()
			captureCollectedWant(t, "a restore between the install's capture step and its publish", map[string][]string{
				"b4_mss_0_v4c": {captureSyncedA, captureSyncedB},
				"b4_mss_0_v6c": {captureSyncedC6},
				"b4_dup_v4c":   {captureSyncedA, captureSyncedB},
				"b4_dup_v6c":   {captureSyncedC6},
			})
			for _, name := range []string{"b4_mss_0_v4c", "b4_dup_v4c"} {
				if !captureCollectedFilledWith(lines, name, captureSyncedB) {
					t.Errorf("the restore rebuilt %s without %s, which the install had already synced; nothing but the next pool change would put it back:\n%s", name, captureSyncedB, strings.Join(lines, "\n"))
				}
			}
		})
	}
}

func TestARolledBackCollectedInstallLeavesTheRestoreWithThePublishedPool(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			published := cfg.WithCollectedIPs([]string{captureSyncedA})
			if err := AddRules(published); err != nil {
				t.Fatal(err)
			}
			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{captureSyncedA, captureSyncedB})); err != nil {
				t.Fatal(err)
			}
			_ = SyncCollectedCaptureSets(published)
			h.take()
			captureCollectedRestore(t, backend, published)
			lines := h.take()
			captureCollectedWant(t, "a restore after the installer's rollback re-synced the published pool", map[string][]string{
				"b4_mss_0_v4c": {captureSyncedA},
				"b4_dup_v4c":   {captureSyncedA},
			})
			for _, name := range []string{"b4_mss_0_v4c", "b4_dup_v4c"} {
				if captureCollectedFilledWith(lines, name, captureSyncedB) {
					t.Errorf("the restore put %s back into %s, although the rollback had synced the published pool without it:\n%s", captureSyncedB, name, strings.Join(lines, "\n"))
				}
			}

			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{captureSyncedA, captureSyncedB})); err != nil {
				t.Fatal(err)
			}
			if err := ClearRules(published); err != nil {
				t.Fatal(err)
			}
			if err := AddRules(published); err != nil {
				t.Fatal(err)
			}
			captureCollectedWant(t, "a build after ClearRules", map[string][]string{
				"b4_mss_0_v4c": {captureSyncedA},
				"b4_dup_v4c":   {captureSyncedA},
			})
		})
	}
}

func TestACaptureRebuildTakesAFamilyTheLastSyncHadNoConsumerForFromTheView(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			for _, set := range cfg.Sets {
				set.Targets.IPVersion = "4"
			}
			if err := AddRules(cfg.WithCollectedIPs([]string{captureSyncedA, captureSyncedC6})); err != nil {
				t.Fatal(err)
			}
			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{captureSyncedA, captureSyncedB, captureSyncedC6})); err != nil {
				t.Fatal(err)
			}
			both := captureCollectedTestConfig(backend)
			if err := AddRules(both.WithCollectedIPs([]string{captureSyncedA, captureSyncedC6})); err != nil {
				t.Fatal(err)
			}
			captureCollectedWant(t, "a rebuild whose sets now take IPv6 too", map[string][]string{
				"b4_mss_0_v4c": {captureSyncedA, captureSyncedB},
				"b4_mss_0_v6c": {captureSyncedC6},
				"b4_dup_v4c":   {captureSyncedA, captureSyncedB},
				"b4_dup_v6c":   {captureSyncedC6},
			})
		})
	}
}

func TestACaptureRebuildUsesAPoolSyncedWhileNoCaptureSetExisted(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			captureCollectedKeep(t)
			run = refuseDeletes(run)
			cfg := captureCollectedTestConfig(backend)
			if err := AddRules(cfg.WithCollectedIPs([]string{captureSyncedA})); err != nil {
				t.Fatal(err)
			}
			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{captureSyncedA})); err != nil {
				t.Fatal(err)
			}
			bare := captureCollectedTestConfig(backend)
			bare.Sets[0].MSSClamp.Enabled = false
			bare.Sets[1].TCP.Duplicate.Enabled = false
			if err := AddRules(bare.WithCollectedIPs([]string{captureSyncedA})); err != nil {
				t.Fatal(err)
			}
			if names := captureCollectedNames(); len(names) != 0 {
				t.Fatalf("setup: without a clamp or duplication set the build kept the capture sets %v", names)
			}
			if err := SyncCollectedCaptureSets(bare.WithCollectedIPs([]string{captureSyncedA, captureSyncedB})); err != nil {
				t.Fatal(err)
			}
			if err := AddRules(cfg.WithCollectedIPs([]string{captureSyncedA})); err != nil {
				t.Fatal(err)
			}
			captureCollectedWant(t, "a rebuild after a sync that found no capture set", map[string][]string{
				"b4_mss_0_v4c": {captureSyncedA, captureSyncedB},
				"b4_dup_v4c":   {captureSyncedA, captureSyncedB},
			})
		})
	}
}
