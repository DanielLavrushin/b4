package tables

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func dscpStatusRow(t *testing.T, st DSCPState, id string) DSCPSetState {
	t.Helper()
	for _, row := range st.Sets {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("no status row for set %s in %+v", id, st.Sets)
	return DSCPSetState{}
}

func TestSetDSCPStatusIptables(t *testing.T) {
	h, _ := dscpLearnIptSetup(t, backendIPTables, backendIP6Tables)
	h.probeErr[backendIP6Tables] = errors.New("ip6tables rejected the set match, which needs the xt_set kernel module and the iptables set extension (exit status 2)")
	a := dscpPlanTestSet("a", 31, "10.0.0.0/8", "10.1.0.0/16", "fd00::/16", "not-an-address")
	proxy := dscpPlanTestSet("proxy", 6, "10.2.0.0/16")
	proxy.Routing.Enabled = true
	proxy.Routing.Mode = config.RoutingModeProxy
	only6 := dscpPlanTestSet("only6", 25, "10.3.0.0/16", "fd03::/32")
	only6.Targets.IPVersion = "6"
	off := dscpPlanTestSet("off", 12, "10.4.0.0/16")
	off.DSCP.Enabled = false
	disabled := dscpPlanTestSet("disabled", 14, "10.5.0.0/16")
	disabled.Enabled = false
	cfg := dscpIptTestConfig(7, true, nil, a, proxy, only6, off, disabled)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpLearnStart()
	dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.10", "203.0.113.11"), false))
	dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.12"), true))

	st := DSCPStatus(cfg)
	if st.Backend != backendIPTables || st.Pending || st.SkipSetup {
		t.Errorf("backend %q, pending %v, skip_setup %v", st.Backend, st.Pending, st.SkipSetup)
	}
	if len(st.Incapable) != 1 || !strings.Contains(st.Incapable[backendIP6Tables], "xt_set") {
		t.Errorf("incapable = %v, want only %s naming xt_set", st.Incapable, backendIP6Tables)
	}
	if len(st.Sets) != 3 {
		t.Fatalf("rows = %+v, want one per enabled set with DSCP on", st.Sets)
	}
	if got := dscpStatusRow(t, st, "a"); got != (DSCPSetState{ID: "a", Name: "a", Value: 31, Applied: true, Static: 3, LearnedDNS: 2, LearnedTLS: 1}) {
		t.Errorf("row a = %+v", got)
	}
	if got := dscpStatusRow(t, st, "proxy"); got != (DSCPSetState{ID: "proxy", Name: "proxy", Value: 6, Refusal: config.DSCPRefusedRoutingProxy}) {
		t.Errorf("a refused set must report only its value and reason, got %+v", got)
	}
	if got := dscpStatusRow(t, st, "only6"); got.Applied || got.Static != 1 || got.Refusal != "" {
		t.Errorf("an IPv6-only set behind an ip6tables without ipset support must read as not applied with one static entry, got %+v", got)
	}

	next := cfg.Clone()
	next.Sets[0].DSCP.Value = 32
	if got := dscpStatusRow(t, DSCPStatus(next), "a"); got.Applied || got.Value != 32 {
		t.Errorf("a value the firewall does not carry yet must read as not applied, got %+v", got)
	}
}

func TestSetDSCPStatusNftables(t *testing.T) {
	f, _ := dscpLearnNftSetup(t)
	a := dscpPlanTestSet("a", 31, "10.1.2.0/24", "2001:db8::/32")
	cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a)
	dscpLearnApply(t, cfg, backendNFTables)
	st := DSCPStatus(cfg)
	if got := dscpStatusRow(t, st, "a"); st.Backend != backendNFTables || !got.Applied || got.Static != 2 || st.Incapable != nil {
		t.Errorf("backend %q, incapable %v, row %+v", st.Backend, st.Incapable, got)
	}

	stalled := cfg.Clone()
	stalled.Sets[0].DSCP.Value = 25
	f.fail = func(script string) (string, error) {
		if strings.Contains(script, "flush chain inet "+dscpNftTable+" "+dscpNftChain+"\n") {
			return "", fmt.Errorf("command [nft -f -] gave up after 15s: %w", context.DeadlineExceeded)
		}
		return "", nil
	}
	if err := applyDSCPFor(stalled, backendNFTables); err == nil {
		t.Fatal("the stalled swap must be reported")
	}
	st = DSCPStatus(stalled)
	if got := dscpStatusRow(t, st, "a"); !st.Pending || got.Applied {
		t.Errorf("a value whose rules nftables has not confirmed must read as not applied, pending %v, row %+v", st.Pending, got)
	}

	f.fail = func(script string) (string, error) {
		if strings.Contains(script, "vmap") {
			return "Error: syntax error", errors.New("command [nft -f -] failed: exit status 1 (Error: syntax error)")
		}
		return "", nil
	}
	moved := cfg.Clone()
	moved.Sets[0].DSCP.Value = 6
	if err := applyDSCPFor(moved, backendNFTables); err == nil {
		t.Fatal("the rejected per-set rules must be reported")
	}
	st = DSCPStatus(moved)
	if got := dscpStatusRow(t, st, "a"); st.Pending || got.Applied {
		t.Errorf("after the fallback to the global stamp the set must read as not applied, pending %v, row %+v", st.Pending, got)
	}
}

func TestSetDSCPStatusWithoutSetDSCP(t *testing.T) {
	resetDSCPState(t)
	if st := DSCPStatus(nil); st.Sets != nil || st.Backend != "" || st.Incapable != nil {
		t.Errorf("status of no config = %+v", st)
	}
	plain := config.NewSetConfig()
	plain.Id = "plain"
	plain.Targets.IpsToMatch = []string{"10.0.0.0/8"}
	cfg := dscpIptTestConfig(7, true, nil, &plain)
	if st := DSCPStatus(cfg); st.Sets != nil {
		t.Errorf("a config without set DSCP must list no rows, got %+v", st.Sets)
	}

	skipped := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
	skipped.System.Tables.SkipSetup = true
	st := DSCPStatus(skipped)
	if got := dscpStatusRow(t, st, "a"); !st.SkipSetup || got.Applied || got.Static != 1 {
		t.Errorf("skip_setup %v, row %+v", st.SkipSetup, got)
	}
}
