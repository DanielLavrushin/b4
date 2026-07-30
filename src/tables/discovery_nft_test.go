package tables

import "testing"

func TestNftTableIsEmpty(t *testing.T) {
	t.Run("only base chains left", func(t *testing.T) {
		listing := `table inet b4_mangle {
	chain prerouting {
		type filter hook prerouting priority mangle; policy accept;
	}

	chain output {
		type filter hook output priority mangle; policy accept;
	}
}`
		if !nftTableIsEmpty(listing) {
			t.Error("table with empty base chains should be reported empty")
		}
	})

	t.Run("main engine rules present", func(t *testing.T) {
		listing := `table inet b4_mangle {
	chain b4_chain {
		tcp dport { 443 } ct original packets < 20 counter queue num 200 bypass
	}

	chain output {
		type filter hook output priority mangle; policy accept;
		jump b4_chain
	}
}`
		if nftTableIsEmpty(listing) {
			t.Error("table carrying rules must not be reported empty")
		}
	})

	t.Run("sets present", func(t *testing.T) {
		listing := `table inet b4_mangle {
	set b4_ips {
		type ipv4_addr
		elements = { 1.2.3.4 }
	}

	chain output {
		type filter hook output priority mangle; policy accept;
	}
}`
		if nftTableIsEmpty(listing) {
			t.Error("table carrying a set must not be reported empty")
		}
	})
}
