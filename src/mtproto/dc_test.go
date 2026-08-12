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
		{"91.108.56.192", 5, true},
		{"149.154.171.255", 5, true},
		{"149.154.170.96", 5, true},
		{"149.154.175.53", 1, true},
		{"149.154.162.123", 0, false},
		{"149.154.175.100", 0, false},
		{"149.154.175.211", 0, false},
		{"91.108.5.1", 0, false},
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

func TestDirectAddressesV6(t *testing.T) {
	got := DirectAddressesV6()
	if len(got) != 5 {
		t.Fatalf("DirectAddressesV6() returned %d entries, want 5", len(got))
	}
	for dc := 1; dc <= 5; dc++ {
		addr, ok := got[dc]
		if !ok {
			t.Errorf("DirectAddressesV6() missing DC%d", dc)
			continue
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Errorf("DC%d address %q does not split: %v", dc, addr, err)
			continue
		}
		if port != "443" {
			t.Errorf("DC%d address %q port = %s, want 443", dc, addr, port)
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() != nil {
			t.Errorf("DC%d host %q is not an IPv6 literal", dc, host)
		}
	}
	if _, ok := got[203]; ok {
		t.Error("DirectAddressesV6() contains DC203, which has no IPv6 address")
	}

	got[1] = "mutated"
	if DirectAddressesV6()[1] == "mutated" {
		t.Error("DirectAddressesV6() returned a reference to the shared map, want a copy")
	}
}

func TestDCForIPRangeAssertsRealDCs(t *testing.T) {
	for _, e := range dcRangesV4 {
		if !validTransparentDC(e.dc) {
			t.Errorf("dcRangesV4 entry %s maps to DC%d, which is not a data center b4 can route", e.net, e.dc)
		}
	}
	for _, e := range dcRangesV6 {
		if !validTransparentDC(e.dc) {
			t.Errorf("dcRangesV6 entry %s maps to DC%d, which is not a data center b4 can route", e.net, e.dc)
		}
	}
}

// A range that covers two data centers sends half its sessions to the wrong one
// through kws<dc>.<domain>, so no range may contain a known address of another DC.
func TestDCRangesDoNotSwallowAnotherDCsAddress(t *testing.T) {
	known := map[string]int{}
	for dc, addr := range dcAddressesV4 {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("dcAddressesV4[%d] = %q does not split: %v", dc, addr, err)
		}
		known[host] = dc
	}
	for host, dc := range dcExtraV4 {
		known[host] = dc
	}
	for _, e := range dcRangesV4 {
		for host, dc := range known {
			ip := net.ParseIP(host)
			if ip == nil || !e.net.Contains(ip) || dc == e.dc {
				continue
			}
			if got, ok := dcForIP(ip); !ok || got != dc {
				t.Errorf("range %s claims DC%d but contains %s which is DC%d, and dcForIP does not rescue it (got %d, ok=%v)",
					e.net, e.dc, host, dc, got, ok)
			}
		}
	}
}
