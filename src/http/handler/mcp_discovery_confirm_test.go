package handler

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

type confirmState int

const (
	fastestOnly confirmState = iota
	winnerOnly
	fastestFailed
)

func confirmationSuite(id string, status discovery.CheckStatus, state confirmState) *discovery.CheckSuite {
	offered := config.NewSetConfig()
	offered.Name = "cached-1-tcpmd5"
	offered.Targets.SNIDomains = []string{"www.svoboda.org"}
	fastest := config.NewSetConfig()
	fastest.Name = "cached-10-fake_sni"
	winner := &discovery.DomainPresetResult{PresetName: "cached-1-tcpmd5", Family: discovery.FamilyTCPMD5, Status: discovery.CheckStatusComplete, Set: &offered}
	fast := &discovery.DomainPresetResult{PresetName: "cached-10-fake_sni", Family: discovery.FamilyFakeSNI, Status: discovery.CheckStatusComplete, Set: &fastest}
	dr := &discovery.DomainDiscoveryResult{
		Domain: "www.svoboda.org", BestPreset: "cached-10-fake_sni", BestSuccess: true, Outcome: discovery.OutcomeFound,
		Results: map[string]*discovery.DomainPresetResult{"cached-1-tcpmd5": winner, "cached-10-fake_sni": fast},
	}
	switch state {
	case fastestOnly:
		fast.Confirmed, fast.ConfirmTries = 3, 3
		dr.Confirmed, dr.ConfirmTries = 3, 3
	case winnerOnly:
		winner.Confirmed, winner.ConfirmTries = 3, 3
		dr.Unconfirmed = true
	case fastestFailed:
		winner.Confirmed, winner.ConfirmTries = 3, 3
		fast.Status, fast.Set = discovery.CheckStatusFailed, nil
		dr.BestPreset, dr.Unconfirmed = "cached-1-tcpmd5", true
	}
	return &discovery.CheckSuite{
		Id: id, Status: status, EndTime: time.Now(),
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{"www.svoboda.org": dr},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "cached-1-tcpmd5", Family: discovery.FamilyTCPMD5, Domains: []string{"www.svoboda.org"}, Set: &offered},
		},
	}
}

func TestMCPDiscoveryConfirmationFollowsTheOfferedPreset(t *testing.T) {
	cfg := probeCfg(t)
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)
	t.Cleanup(func() { mcpRememberSuite("") })

	cases := []struct {
		name        string
		status      discovery.CheckStatus
		state       confirmState
		unconfirmed bool
	}{
		{"stopped after the fastest passed", discovery.CheckStatusCanceled, fastestOnly, true},
		{"confirmation gave up before the winner", discovery.CheckStatusComplete, fastestOnly, true},
		{"the fastest failed and the winner passed", discovery.CheckStatusComplete, fastestFailed, false},
		{"stopped after the winner passed", discovery.CheckStatusCanceled, winnerOnly, true},
	}
	for i, c := range cases {
		for _, source := range []string{"run", "history"} {
			id := fmt.Sprintf("confirm-%d-%s", i, source)
			suite := confirmationSuite(id, c.status, c.state)
			if source == "run" {
				discovery.RegisterSuite(suite)
			} else {
				discovery.SaveToHistory(suite, cfg.ConfigPath)
			}
			out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": id}))
			if out.Source != source || len(out.Domains) != 1 {
				t.Fatalf("%s: want one row from %s, got %+v", c.name, source, out)
			}
			row := out.Domains[0]
			if row.Unconfirmed != c.unconfirmed || strings.Contains(row.Verdict, "UNCONFIRMED") != c.unconfirmed {
				t.Errorf("%s (%s): want unconfirmed=%v, got unconfirmed=%v verdict %q", c.name, source, c.unconfirmed, row.Unconfirmed, row.Verdict)
			}
		}
	}
}
