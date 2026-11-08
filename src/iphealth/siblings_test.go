package iphealth

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestMediaNodeClusterReadsBothNameForms(t *testing.T) {
	for _, tc := range []struct {
		host    string
		cluster string
	}{
		{"rr3---sn-ab5l6ndr.googlevideo.com", "sn-ab5l6ndr"},
		{"rr3.sn-ab5l6ndr.googlevideo.com", "sn-ab5l6ndr"},
		{"RR12---SN-GVNUXAXJVH-XNAL.googlevideo.com.", "sn-gvnuxaxjvh-xnal"},
		{"r4---sn-aj5goc-5x.googlevideo.com", "sn-aj5goc-5x"},
		{"redirector.googlevideo.com", ""},
		{"manifest.googlevideo.com", ""},
		{"rr---sn-ab5l6ndr.googlevideo.com", ""},
		{"rrr3---sn-ab5l6ndr.googlevideo.com", ""},
		{"3---sn-ab5l6ndr.googlevideo.com", ""},
		{"rr3---ab5l6ndr.googlevideo.com", ""},
		{"rr3---sn-.googlevideo.com", ""},
		{"rr3---sn-ab5.l6ndr.googlevideo.com", ""},
		{"rr3---sn-ab5l6ndr.googlevideo.com.evil.example", ""},
		{"rr3---sn-ab5l6ndr.example.com", ""},
		{"googlevideo.com", ""},
		{"", ""},
	} {
		cluster, ok := mediaNodeCluster(tc.host)
		if ok != (tc.cluster != "") || cluster != tc.cluster {
			t.Errorf("mediaNodeCluster(%q) = %q, %v, want %q", tc.host, cluster, ok, tc.cluster)
		}
	}
}

func TestSameServerBlockComparesTheSlash24OrSlash48(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		same bool
	}{
		{"188.128.43.204", "188.128.43.205", true},
		{"188.128.43.204", "188.128.44.204", false},
		{"188.128.43.204", "::ffff:188.128.43.9", true},
		{"2a00:1450:4010:c0b::8", "2a00:1450:4010:ffff::1", true},
		{"2a00:1450:4010:c0b::8", "2a00:1450:4011:c0b::8", false},
		{"188.128.43.204", "2a00:1450:4010:c0b::8", false},
	} {
		if got := sameServerBlock(net.ParseIP(tc.a), net.ParseIP(tc.b)); got != tc.same {
			t.Errorf("sameServerBlock(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.same)
		}
	}
}

func TestSiblingsAreVerifiedServersOfTheSameClusterAndBlock(t *testing.T) {
	near := []net.IP{net.ParseIP("173.194.185.200")}
	s := NewKnownGood()
	s.Remember("rr1---sn-ab5l6ndr.googlevideo.com", net.ParseIP("173.194.185.199"))
	s.Remember("rr2---sn-ab5l6ndr.googlevideo.com", net.ParseIP("173.194.185.201"))
	time.Sleep(2 * time.Millisecond)
	s.Remember("rr4.sn-ab5l6ndr.googlevideo.com", net.ParseIP("173.194.185.203"))
	s.Remember("rr5---sn-ab5l6ndr.googlevideo.com", net.ParseIP("2a00:1450:4010:c0b::5"))
	s.Observe("rr6---sn-ab5l6ndr.googlevideo.com", net.ParseIP("173.194.185.204"))
	s.Remember("rr7---sn-ab5l6ndr.googlevideo.com", net.ParseIP("203.0.113.7"))
	s.Remember("rr1---sn-4g5e6nsy.googlevideo.com", net.ParseIP("173.194.185.134"))
	s.Remember("www.youtube.com", net.ParseIP("173.194.185.110"))

	got := s.LookupSiblings("rr3---sn-ab5l6ndr.googlevideo.com", near, nil)
	if len(got) != 3 {
		t.Fatalf("siblings = %v, want the three handshake-verified IPv4 servers of sn-ab5l6ndr in the answer's block", got)
	}
	if got[0].Host != "rr4.sn-ab5l6ndr.googlevideo.com" || got[0].IP.String() != "173.194.185.203" {
		t.Errorf("first sibling = %s %s, want the most recently verified one", got[0].Host, got[0].IP)
	}

	if v6 := s.LookupSiblings("rr3---sn-ab5l6ndr.googlevideo.com", []net.IP{net.ParseIP("2a00:1450:4010:c0b::3")}, nil); len(v6) != 1 || v6[0].IP.String() != "2a00:1450:4010:c0b::5" {
		t.Errorf("IPv6 siblings = %v, want only the IPv6 server", v6)
	}
	if got := s.LookupSiblings("rr1---sn-ab5l6ndr.googlevideo.com", near, nil); len(got) != 2 {
		t.Errorf("the queried name's own entry is never its sibling, got %v", got)
	}
	if got := s.LookupSiblings("rr9.sn-ab5l6ndr.googlevideo.com", near, nil); len(got) != 3 {
		t.Errorf("a name in the CNAME form finds the cluster too, got %v", got)
	}
	if got := s.LookupSiblings("rr3---sn-ab5l6ndr.googlevideo.com", []net.IP{net.ParseIP("74.125.111.132")}, nil); len(got) != 0 {
		t.Errorf("servers outside the block of the answer's addresses are not siblings, got %v", got)
	}
	if got := s.LookupSiblings("rr3---sn-ab5l6ndr.googlevideo.com", nil, nil); got != nil {
		t.Errorf("with no answer address to compare there are no siblings, got %v", got)
	}
	if got := s.LookupSiblings("www.youtube.com", near, nil); got != nil {
		t.Errorf("a name that is not a video server has no siblings, got %v", got)
	}
	var nilStore *KnownGood
	if got := nilStore.LookupSiblings("rr3---sn-ab5l6ndr.googlevideo.com", near, nil); got != nil {
		t.Errorf("a nil store has no siblings, got %v", got)
	}
}

func TestSiblingsSkipUnusableOnesBeforeTheCap(t *testing.T) {
	near := []net.IP{net.ParseIP("188.128.43.204")}
	s := NewKnownGood()
	s.Remember("rr2---sn-gvnuxaxjvh-xnal.googlevideo.com", net.ParseIP("188.128.43.2"))
	time.Sleep(2 * time.Millisecond)
	unusable := map[string]bool{}
	for i := 3; i < 3+maxKnownGoodPerHost; i++ {
		ip := fmt.Sprintf("188.128.43.%d", i)
		s.Remember(fmt.Sprintf("rr%d---sn-gvnuxaxjvh-xnal.googlevideo.com", i), net.ParseIP(ip))
		unusable[ip] = true
	}

	got := s.LookupSiblings("rr1---sn-gvnuxaxjvh-xnal.googlevideo.com", near, func(ip net.IP) bool { return !unusable[ip.String()] })
	if len(got) != 1 || got[0].IP.String() != "188.128.43.2" {
		t.Fatalf("siblings = %v, want the older usable server even behind %d fresher unusable ones", got, maxKnownGoodPerHost)
	}
}

func TestSiblingsSkipExpiredEntriesAndRepeatedAddresses(t *testing.T) {
	near := []net.IP{net.ParseIP("173.194.185.200")}
	s := NewKnownGood()
	s.Remember("rr2---sn-ab5l6ndr.googlevideo.com", net.ParseIP("173.194.185.201"))
	s.Remember("rr2.sn-ab5l6ndr.googlevideo.com", net.ParseIP("173.194.185.201"))
	s.Remember("rr3---sn-ab5l6ndr.googlevideo.com", net.ParseIP("173.194.185.202"))

	s.mu.Lock()
	entries := s.hosts["rr3---sn-ab5l6ndr.googlevideo.com"]
	entries[0].seen = time.Now().Add(-2 * knownGoodTTL)
	s.mu.Unlock()

	got := s.LookupSiblings("rr1---sn-ab5l6ndr.googlevideo.com", near, nil)
	if len(got) != 1 || got[0].IP.String() != "173.194.185.201" {
		t.Errorf("siblings = %v, want rr2's address once and the aged-out rr3 left out", got)
	}
}
