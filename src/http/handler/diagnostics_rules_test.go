package handler

import "testing"

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
