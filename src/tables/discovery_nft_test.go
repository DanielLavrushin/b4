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

	t.Run("main engine rebuild in progress", func(t *testing.T) {
		listing := `table inet b4_mangle {
	chain b4_chain {
	}

	chain prerouting {
		type filter hook prerouting priority mangle; policy accept;
	}

	chain output {
		type filter hook output priority mangle; policy accept;
	}
}`
		if nftTableIsEmpty(listing) {
			t.Error("table holding a chain discovery never creates must not be reported empty")
		}
	})

	t.Run("foreign base chain present", func(t *testing.T) {
		listing := `table inet b4_mangle {
	chain forward {
		type filter hook forward priority mangle; policy accept;
	}
}`
		if nftTableIsEmpty(listing) {
			t.Error("forward chain belongs to the MSS clamp path, not to discovery")
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

func TestNftLineHasMark(t *testing.T) {
	cases := []struct {
		name string
		line string
		mark uint
		want bool
	}{
		{"padded accept matches", "meta mark 0x00008001 accept # handle 12", 0x8001, true},
		{"padded injected matches", "meta mark 0x00008002 accept # handle 13", 0x8002, true},
		{"unpadded accept matches", "meta mark 0x8001 accept # handle 12", 0x8001, true},
		{"different mark ignored", "meta mark 0x00008000 accept # handle 9", 0x8001, false},
		{"masked main engine rule ignored", "meta mark & 0x00008000 == 0x00008000 return # handle 4", 0x8001, false},
		{"no mark at all", "jump b4_chain # handle 7", 0x8001, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nftLineHasMark(tc.line, tc.mark); got != tc.want {
				t.Errorf("nftLineHasMark(%q, 0x%x) = %v, want %v", tc.line, tc.mark, got, tc.want)
			}
		})
	}
}
