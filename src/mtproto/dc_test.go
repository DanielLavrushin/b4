package mtproto

import (
	"net"
	"testing"
)

func TestDCForIPRange(t *testing.T) {
	cases := []struct {
		ip   string
		want int
		ok   bool
	}{
		{"149.154.167.50", 2, true},
		{"149.154.167.222", 2, true},
		{"149.154.161.144", 2, true},
		{"149.154.166.121", 4, true},
		{"149.154.165.109", 4, true},
		{"91.108.4.140", 4, true},
		{"2001:67c:4e8:f002::a", 2, true},
		{"2001:67c:4e8:f004::a", 4, true},
		{"149.154.162.123", 0, false},
		{"149.154.175.50", 0, false},
		{"91.105.192.100", 0, false},
		{"8.8.8.8", 0, false},
	}
	for _, c := range cases {
		got, ok := dcForIPRange(net.ParseIP(c.ip))
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("dcForIPRange(%s) = (%d, %v), want (%d, %v)", c.ip, got, ok, c.want, c.ok)
		}
	}
}

func TestDCForIPRangeOnlyWSServedDCs(t *testing.T) {
	for _, e := range dcRangesV4 {
		if !wsEdgeServesDC(e.dc) {
			t.Errorf("dcRangesV4 entry %s maps to DC%d which is not WS-served; range resolution should only assert WS-served DCs", e.net, e.dc)
		}
	}
	for _, e := range dcRangesV6 {
		if !wsEdgeServesDC(e.dc) {
			t.Errorf("dcRangesV6 entry %s maps to DC%d which is not WS-served", e.net, e.dc)
		}
	}
}
