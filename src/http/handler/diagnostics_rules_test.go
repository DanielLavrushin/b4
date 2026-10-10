package handler

import (
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func TestIsB4IptablesRuleKeepsTheRulesThatDecideTUNBehaviour(t *testing.T) {
	keep := []string{
		"-A OUTPUT -m mark --mark 0x10000000/0x10000000 -j CT --notrack",
		"-A OUTPUT -m mark --mark 0x20000000/0x20000000 -j CT --notrack",
		"-A PREROUTING -j B4_TUN_GATE",
		"-A OUTPUT -j B4_TUN",
		"-A POSTROUTING -o b4tun0 -j SNAT --to-source 62.78.37.161",
		"-A FORWARD -o b4tun0 -j ACCEPT",
		"-A PREROUTING -j b4r_93dd530478d242a_d037_pre",
		"-A b4r_93dd530478d242a_d037_pre -p tcp -m set --match-set b4r_93dd530478d242a_d037_v4 dst -j TPROXY --on-port 58312",
		"-A B4_PREROUTING_X -p udp --dport 53 -j NFQUEUE --queue-num 537",
	}
	for _, l := range keep {
		if !isB4IptablesRule(l) {
			t.Errorf("dropped a b4 rule from the diagnostics dump: %q", l)
		}
	}
}

func TestIsB4IptablesRuleSkipsChainsDumpedSeparately(t *testing.T) {
	skip := []string{
		"-N B4",
		"-N B4_TUN",
		"-N B4_TUN_GATE",
		"-N B4_DISCOVERY",
		"-N B4_MASQ",
		"-A B4_TUN -m mark --mark 0x8000/0x8000 -j RETURN",
		"-A B4_TUN_GATE -m mac --mac-source 02:42:AC:11:00:03 -j RETURN",
		"-A B4_DISCOVERY -m mark --mark 0x8002 -j ACCEPT",
		"-A B4_PREROUTING -p udp --dport 53 -j NFQUEUE --queue-num 537",
	}
	for _, l := range skip {
		if isB4IptablesRule(l) {
			t.Errorf("duplicated a rule already dumped as its own chain: %q", l)
		}
	}
}

func TestIsB4IptablesRuleIgnoresForeignRules(t *testing.T) {
	foreign := []string{
		"-A PREROUTING -i br-lan -m comment --comment \"!fw3: lan CT helper assignment\" -j zone_lan_helper",
		"-A FORWARD -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu",
		"-P FORWARD DROP",
	}
	for _, l := range foreign {
		if isB4IptablesRule(l) {
			t.Errorf("picked up a rule b4 does not own: %q", l)
		}
	}
}

func TestDiagB4ChainsCoverTheTUNChains(t *testing.T) {
	want := map[string]string{
		"B4":            "mangle",
		"B4_PREROUTING": "mangle",
		"B4_TUN":        "mangle",
		"B4_TUN_GATE":   "mangle",
		"B4_DISCOVERY":  "mangle",
		"B4_DSCP":       "mangle",
		"B4_MASQ":       "nat",
	}
	got := make(map[string]string, len(diagB4Chains))
	for _, c := range diagB4Chains {
		got[c.chain] = c.table
	}
	for chain, table := range want {
		if got[chain] != table {
			t.Errorf("chain %s dumped from table %q, want %q", chain, got[chain], table)
		}
	}
}

func stubDSCPStatus(t *testing.T, st tables.DSCPState) {
	t.Helper()
	orig := dscpStatus
	dscpStatus = func(*config.Config) tables.DSCPState { return st }
	t.Cleanup(func() { dscpStatus = orig })
}

func TestCollectDSCPRuleGroupListsOneRowPerSet(t *testing.T) {
	stubDSCPStatus(t, tables.DSCPState{
		Backend:   "iptables",
		Incapable: map[string]string{"ip6tables": "the ipset command is not installed"},
		Sets: []tables.DSCPSetState{
			{ID: "yt", Name: "YouTube", Value: 31, Applied: true, Static: 12, LearnedDNS: 5, LearnedTLS: 2, LearnedPreResolve: 40},
			{ID: "px", Name: "Proxy", Value: 25, Refusal: config.DSCPRefusedRoutingProxy},
			{ID: "new", Name: "New set", Value: 0},
		},
	})
	cfg := config.NewConfig()
	groups := collectDSCPRuleGroup(&cfg)
	if len(groups) != 1 || groups[0].Title != "Per-set DSCP" {
		t.Fatalf("groups = %+v, want one Per-set DSCP group", groups)
	}
	want := []string{
		`"YouTube" (yt): DSCP 31, applied, 12 static entries, learned addresses written: 5 from DNS answers, 2 from TLS/QUIC names, 40 from lookups`,
		`"Proxy" (px): DSCP 25, refused: routing_proxy`,
		`"New set" (new): DSCP 0, not applied, 0 static entries, learned addresses written: 0 from DNS answers, 0 from TLS/QUIC names, 0 from lookups`,
		"ip6tables writes no per-set value: the ipset command is not installed",
		"installed with iptables",
	}
	if !slices.Equal(groups[0].Rules, want) {
		t.Errorf("rows:\n%s\nwant:\n%s", strings.Join(groups[0].Rules, "\n"), strings.Join(want, "\n"))
	}
}

func TestCollectDSCPRuleGroupStateLine(t *testing.T) {
	row := []tables.DSCPSetState{{ID: "a", Name: "a", Value: 7}}
	for _, tc := range []struct {
		name string
		st   tables.DSCPState
		want string
	}{
		{"skip_setup", tables.DSCPState{Backend: "nftables", SkipSetup: true, Sets: row}, "skip_setup is on: no rule installed"},
		{"nothing applied", tables.DSCPState{Sets: row}, "no DSCP rule installed"},
		{"pending", tables.DSCPState{Backend: "nftables", Pending: true, Sets: row}, "installed with nftables, the firewall monitor retries a step that did not apply"},
		{"installed", tables.DSCPState{Backend: "iptables-legacy", Sets: row}, "installed with iptables-legacy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubDSCPStatus(t, tc.st)
			cfg := config.NewConfig()
			groups := collectDSCPRuleGroup(&cfg)
			if len(groups) != 1 {
				t.Fatalf("groups = %+v", groups)
			}
			if rules := groups[0].Rules; rules[len(rules)-1] != tc.want {
				t.Errorf("last row = %q, want %q", rules[len(rules)-1], tc.want)
			}
		})
	}
}

func TestCollectDSCPRuleGroupAbsentWithoutSetDSCP(t *testing.T) {
	if groups := collectDSCPRuleGroup(nil); groups != nil {
		t.Errorf("no config must add no group, got %+v", groups)
	}
	cfg := config.NewConfig()
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: true, Value: 7}
	set := config.NewSetConfig()
	set.Id = "plain"
	set.Targets.IpsToMatch = []string{"10.0.0.0/8"}
	off := config.NewSetConfig()
	off.Id = "off"
	off.Enabled = false
	off.DSCP = config.SetDSCPConfig{Enabled: true, Value: 31}
	off.Targets.IpsToMatch = []string{"10.1.0.0/16"}
	cfg.Sets = []*config.SetConfig{&set, &off}
	if groups := collectDSCPRuleGroup(&cfg); groups != nil {
		t.Errorf("a config where no enabled set writes its own DSCP value must leave the diagnostics as they were, got %+v", groups)
	}
}
