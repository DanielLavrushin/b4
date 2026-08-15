package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func mssClampSet(id string, size int, ips, macs []string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Enabled = true
	set.MSSClamp.Enabled = true
	set.MSSClamp.Size = size
	set.Targets.IPs = ips
	set.Targets.IpsToMatch = ips
	set.Targets.SourceDevices = macs
	return &set
}

func mssSpecs(t *testing.T, cfg *config.Config, ipt string) []string {
	t.Helper()
	manager := NewIPTablesManager(cfg, false)
	_, rules := manager.buildMSSManifest("PREROUTING")
	var out []string
	for _, r := range rules {
		if r.IPT != ipt {
			continue
		}
		out = append(out, strings.Join(r.Spec, " "))
	}
	return out
}

func TestBuildMSSManifestSetScope(t *testing.T) {
	t.Run("a device-scoped set with no IPs clamps that device", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{"AA:BB:CC:DD:EE:FF"})}

		specs := mssSpecs(t, &cfg, "iptables")
		if len(specs) != 1 {
			t.Fatalf("expected 1 rule, got %d: %v", len(specs), specs)
		}
		if !strings.Contains(specs[0], "--mac-source AA:BB:CC:DD:EE:FF") {
			t.Errorf("expected MAC scope, got %q", specs[0])
		}
		if strings.Contains(specs[0], "--match-set") {
			t.Errorf("a set with no IPs has no ipset to match, got %q", specs[0])
		}
	})

	t.Run("a set with both scopes narrows to its addresses", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Sets = []*config.SetConfig{
			mssClampSet("s1", 88, []string{"203.0.113.7"}, []string{"AA:BB:CC:DD:EE:FF"}),
		}

		specs := mssSpecs(t, &cfg, "iptables")
		if len(specs) != 1 {
			t.Fatalf("expected 1 rule, got %d: %v", len(specs), specs)
		}
		if !strings.Contains(specs[0], "--match-set") || !strings.Contains(specs[0], "--mac-source") {
			t.Errorf("expected both scopes, got %q", specs[0])
		}
	})

	t.Run("an IPv4-scoped set emits no unscoped IPv6 rule", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = true
		cfg.Sets = []*config.SetConfig{
			mssClampSet("s1", 88, []string{"203.0.113.7"}, []string{"AA:BB:CC:DD:EE:FF"}),
		}

		for _, spec := range mssSpecs(t, &cfg, "ip6tables") {
			if !strings.Contains(spec, "--match-set") {
				t.Errorf("a set scoped to IPv4 addresses must not clamp all IPv6 traffic from its devices, got %q", spec)
			}
		}
	})
}
