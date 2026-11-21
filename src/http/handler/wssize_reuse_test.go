package handler

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestMCPNeverChangesWSReuseTransport(t *testing.T) {
	cfg := mcpTestCfg()
	for _, path := range []string{"sets[].tcp.wssize_reuse.enabled", "sets[].tcp.wssize_reuse.max_age_seconds"} {
		if _, err := mcpResolvePath(cfg, path, "video"); err == nil {
			t.Fatal("local transport is writable by MCP", path)
		}
	}
	for _, field := range mcpWritablePaths(cfg, cfg.Sets[0]) {
		if strings.Contains(field.Path, "wssize_reuse") {
			t.Fatal("transport appears in writable paths", field.Path)
		}
	}
}

func TestDiscoveryApplyKeepsWSReuseScope(t *testing.T) {
	dst := config.NewSetConfig()
	dst.Enabled = true
	dst.TCP.WSSizeReuse.Enabled = true
	from := config.NewSetConfig()
	from.Targets.TLSVersion = "1.3"
	from.Targets.IPVersion = "4"
	replaceStrategy(&dst, &from)
	if dst.Targets.TLSVersion != "" || dst.Targets.IPVersion != "" || !dst.ReusesWSSize() {
		t.Fatal("discovery result changed local transport scope")
	}
}
