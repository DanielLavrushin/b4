package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func netnsManyUDPPortRangesStillInstallTheCaptureRules(t *testing.T, engine string) {
	netnsRequire(t)
	netnsRequireEngine(t, engine)

	ranges := []string{"1000-1010", "2000-2010", "3000-3010", "4000-4010", "5000-5010", "6000-6010", "7000-7010", "8000-8010"}
	cfg := netnsConfig(engine)
	set := config.NewSetConfig()
	set.Id = "netns-port-ranges"
	set.Name = "netnsportranges"
	set.Enabled = true
	set.UDP.DPortFilter = strings.Join(ranges, ",")
	cfg.Sets = []*config.SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	routeEngine = nil
	defer func() { routeEngine = nil }()
	if err := AddRules(cfg); err != nil {
		t.Fatalf("eight UDP port ranges must not stop the capture rules from installing: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	queued := ""
	for _, line := range strings.Split(netnsRun(t, engine, "-w", "-t", "mangle", "-S"), "\n") {
		if strings.Contains(line, "-p udp") && strings.Contains(line, "--dports") && strings.Contains(line, "NFQUEUE") {
			queued += line + "\n"
		}
	}
	for _, r := range append([]string{"443"}, ranges...) {
		if !strings.Contains(queued, strings.Replace(r, "-", ":", 1)) {
			t.Errorf("UDP port %s reaches no queue rule, so the sets that list it never see its traffic:\n%s", r, queued)
		}
	}
}

func TestNetnsManyUDPPortRangesStillInstallTheCaptureRules(t *testing.T) {
	netnsManyUDPPortRangesStillInstallTheCaptureRules(t, backendIPTables)
}

func TestNetnsManyUDPPortRangesStillInstallTheCaptureRulesLegacy(t *testing.T) {
	netnsManyUDPPortRangesStillInstallTheCaptureRules(t, backendIPTablesLegacy)
}
