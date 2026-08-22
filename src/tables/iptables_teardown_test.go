package tables

import (
	"errors"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func stubBinaryPresence(t *testing.T, present map[string]bool) {
	t.Helper()
	for name, found := range present {
		prev, had := hasBinaryCache.Load(name)
		t.Cleanup(func() {
			if had {
				hasBinaryCache.Store(name, prev)
			} else {
				hasBinaryCache.Delete(name)
			}
		})
		hasBinaryCache.Store(name, found)
	}
}

func stubProbes(manager *IPTablesManager, bins ...string) {
	for _, bin := range bins {
		manager.nfqueueSupport[bin] = nil
		manager.connbytesSupport[bin] = nil
		manager.multiportSupport[bin] = true
		manager.connmarkSupport[bin] = true
	}
}

func failProbe(manager *IPTablesManager, bin string) {
	manager.nfqueueSupport[bin] = errors.New("NFQUEUE target unavailable")
	manager.connbytesSupport[bin] = nil
	manager.multiportSupport[bin] = true
	manager.connmarkSupport[bin] = true
}

func manifestBinaries(m Manifest) map[string]bool {
	out := map[string]bool{}
	for _, r := range m.Rules {
		out[r.IPT] = true
	}
	for _, c := range m.Chains {
		out[c.IPT] = true
	}
	return out
}

func TestTeardownBinariesIgnoreFamilyFlags(t *testing.T) {
	t.Run("ip6tables stays on the teardown list when IPv6 is switched off", func(t *testing.T) {
		stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = false

		manager := NewIPTablesManager(&cfg, false)

		apply := manager.applyBinaries()
		if len(apply) != 1 || apply[0] != backendIPTables {
			t.Fatalf("applyBinaries = %v, want [%s]", apply, backendIPTables)
		}

		teardown := manager.teardownBinaries()
		if len(teardown) != 2 || teardown[0] != backendIPTables || teardown[1] != backendIP6Tables {
			t.Fatalf("teardownBinaries = %v, want [%s %s]", teardown, backendIPTables, backendIP6Tables)
		}
	})

	t.Run("a missing binary is still excluded from teardown", func(t *testing.T) {
		stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = true

		manager := NewIPTablesManager(&cfg, false)

		teardown := manager.teardownBinaries()
		if len(teardown) != 1 || teardown[0] != backendIPTables {
			t.Fatalf("teardownBinaries = %v, want [%s]", teardown, backendIPTables)
		}
	})

	t.Run("teardown covers both families when neither is enabled", func(t *testing.T) {
		stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = false
		cfg.Queue.IPv6Enabled = false

		manager := NewIPTablesManager(&cfg, false)

		if got := len(manager.applyBinaries()); got != 0 {
			t.Fatalf("applyBinaries = %d entries, want 0", got)
		}
		if got := len(manager.teardownBinaries()); got != 2 {
			t.Fatalf("teardownBinaries = %d entries, want 2", got)
		}
	})
}

func TestBuildTeardownManifestCoversDisabledFamily(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true, "ipset": true})
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false

	manager := NewIPTablesManager(&cfg, false)
	stubProbes(manager, backendIPTables, backendIP6Tables)

	applyManifest, err := manager.buildManifest()
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	if manifestBinaries(applyManifest)[backendIP6Tables] {
		t.Errorf("apply manifest carries %s rules while IPv6 is disabled", backendIP6Tables)
	}

	teardownManifest, err := manager.buildTeardownManifest()
	if err != nil {
		t.Fatalf("buildTeardownManifest: %v", err)
	}
	bins := manifestBinaries(teardownManifest)
	if !bins[backendIP6Tables] {
		t.Errorf("teardown manifest is missing %s rules, IPv6 rules installed earlier would be stranded", backendIP6Tables)
	}
	if !bins[backendIPTables] {
		t.Errorf("teardown manifest is missing %s rules", backendIPTables)
	}
}

func TestBuildTeardownManifestWithBothFamiliesDisabled(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = false
	cfg.Queue.IPv6Enabled = false

	manager := NewIPTablesManager(&cfg, false)
	stubProbes(manager, backendIPTables, backendIP6Tables)

	if _, err := manager.buildManifest(); err == nil {
		t.Error("expected buildManifest to fail when no family is enabled")
	}

	m, err := manager.buildTeardownManifest()
	if err != nil {
		t.Fatalf("buildTeardownManifest: %v", err)
	}
	bins := manifestBinaries(m)
	if !bins[backendIPTables] || !bins[backendIP6Tables] {
		t.Fatalf("teardown manifest binaries = %v, want both families", bins)
	}
}

func TestBuildTeardownManifestNoBinaries(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: false, backendIP6Tables: false})
	cfg := config.NewConfig()

	manager := NewIPTablesManager(&cfg, false)
	if _, err := manager.buildTeardownManifest(); err == nil {
		t.Error("expected an error when no iptables binary is present")
	}
}

func TestBuildTeardownManifestIgnoresFailedProbe(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true

	manager := NewIPTablesManager(&cfg, false)
	stubProbes(manager, backendIPTables)
	failProbe(manager, backendIP6Tables)

	m, err := manager.buildTeardownManifest()
	if err != nil {
		t.Fatalf("buildTeardownManifest: %v", err)
	}
	if !manifestBinaries(m)[backendIP6Tables] {
		t.Errorf("teardown manifest dropped %s because of a failed probe", backendIP6Tables)
	}
}

func TestUsableApplyBinariesDegradesPerFamily(t *testing.T) {
	t.Run("a failing ip6tables probe leaves IPv4 working", func(t *testing.T) {
		stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = true

		manager := NewIPTablesManager(&cfg, false)
		stubProbes(manager, backendIPTables)
		failProbe(manager, backendIP6Tables)

		bins, err := manager.usableApplyBinaries()
		if err != nil {
			t.Fatalf("usableApplyBinaries: %v", err)
		}
		if len(bins) != 1 || bins[0] != backendIPTables {
			t.Fatalf("usableApplyBinaries = %v, want [%s]", bins, backendIPTables)
		}

		m, err := manager.buildManifest()
		if err != nil {
			t.Fatalf("buildManifest: %v", err)
		}
		manifest := manifestBinaries(m)
		if !manifest[backendIPTables] {
			t.Errorf("apply manifest lost %s rules because IPv6 probing failed", backendIPTables)
		}
		if manifest[backendIP6Tables] {
			t.Errorf("apply manifest kept %s rules despite a failed probe", backendIP6Tables)
		}
	})

	t.Run("a failing iptables probe leaves IPv6 working", func(t *testing.T) {
		stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = true

		manager := NewIPTablesManager(&cfg, false)
		stubProbes(manager, backendIP6Tables)
		failProbe(manager, backendIPTables)

		bins, err := manager.usableApplyBinaries()
		if err != nil {
			t.Fatalf("usableApplyBinaries: %v", err)
		}
		if len(bins) != 1 || bins[0] != backendIP6Tables {
			t.Fatalf("usableApplyBinaries = %v, want [%s]", bins, backendIP6Tables)
		}
	})

	t.Run("both families failing is an error", func(t *testing.T) {
		stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = true

		manager := NewIPTablesManager(&cfg, false)
		failProbe(manager, backendIPTables)
		failProbe(manager, backendIP6Tables)

		if _, err := manager.usableApplyBinaries(); err == nil {
			t.Error("expected an error when no family survives probing")
		}
		if _, err := manager.buildManifest(); err == nil {
			t.Error("expected buildManifest to fail when no family survives probing")
		}
	})

	t.Run("connbytes failure drops only that family", func(t *testing.T) {
		stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = true

		manager := NewIPTablesManager(&cfg, false)
		stubProbes(manager, backendIPTables, backendIP6Tables)
		manager.connbytesSupport[backendIP6Tables] = errors.New("xt_connbytes missing")

		bins, err := manager.usableApplyBinaries()
		if err != nil {
			t.Fatalf("usableApplyBinaries: %v", err)
		}
		if len(bins) != 1 || bins[0] != backendIPTables {
			t.Fatalf("usableApplyBinaries = %v, want [%s]", bins, backendIPTables)
		}
	})
}

func TestMSSClampTeardownCoversDisabledFamily(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true, "ipset": true})
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Sets = []*config.SetConfig{mssClampSet("s1", 1300, []string{"2001:db8::1", "10.0.0.1"}, nil)}

	manager := NewIPTablesManager(&cfg, false)

	_, applyRules := manager.buildMSSManifest("PREROUTING")
	for _, r := range applyRules {
		if r.IPT == backendIP6Tables {
			t.Fatalf("apply MSS manifest carries %s rules while IPv6 is disabled", backendIP6Tables)
		}
	}

	_, teardownRules := manager.buildMSSManifestFor(manager.teardownBinaries(), "PREROUTING")
	seen := map[string]bool{}
	for _, r := range teardownRules {
		seen[r.IPT] = true
	}
	if !seen[backendIP6Tables] {
		t.Errorf("MSS teardown manifest is missing %s rules", backendIP6Tables)
	}
	if !seen[backendIPTables] {
		t.Errorf("MSS teardown manifest is missing %s rules", backendIPTables)
	}
}

func TestIPTFamilyLabel(t *testing.T) {
	cases := map[string]string{
		backendIPTables:        "IPv4",
		backendIPTablesLegacy:  "IPv4",
		backendIP6Tables:       "IPv6",
		backendIP6TablesLegacy: "IPv6",
	}
	for bin, want := range cases {
		if got := iptFamilyLabel(bin); got != want {
			t.Errorf("iptFamilyLabel(%q) = %q, want %q", bin, got, want)
		}
	}
}
