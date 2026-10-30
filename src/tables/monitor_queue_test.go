package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const iptCaptureChainListing = `-N B4
-A B4 -m mark --mark 0x8000/0x8cfd8000 -j RETURN
-A B4 -p tcp -m multiport --dports 443 -m connbytes --connbytes 0:19 --connbytes-mode packets --connbytes-dir original -m conntrack --ctdir ORIGINAL -j NFQUEUE --queue-balance 537:540 --queue-bypass
-A B4 -p udp -m udp --dport 53 -j RETURN
-A B4 -p udp -m multiport --dports 443 -m connbytes --connbytes 0:8 --connbytes-mode packets --connbytes-dir original -j NFQUEUE --queue-balance 537:540 --queue-bypass
`

func TestIPTListingQueuesFindsTheCaptureRules(t *testing.T) {
	if !iptListingQueuesTCP(iptCaptureChainListing, "B4") {
		t.Error("expected the TCP queue rule of the capture chain to be found")
	}
	if !iptListingQueuesUDP(iptCaptureChainListing, "B4") {
		t.Error("expected the UDP queue rule of the capture chain to be found")
	}
}

func TestIPTListingQueuesIgnoresOtherChainsAndTargets(t *testing.T) {
	const listing = `-N B4
-A B4 -m mark --mark 0x8000/0x8cfd8000 -j RETURN
-A B4 -p udp -m udp --dport 53 -j RETURN
-A B4_PREROUTING -p tcp -m multiport --sports 443 -j NFQUEUE --queue-balance 537:540 --queue-bypass
`
	if iptListingQueuesTCP(listing, "B4") {
		t.Error("a queue rule that belongs to another chain must not satisfy the capture chain check")
	}
	if iptListingQueuesUDP(listing, "B4") {
		t.Error("the DNS return rule must not be taken for a queued UDP port")
	}
}

func TestNFTListingQueuesMatchesPerLine(t *testing.T) {
	const listing = `chain b4_chain {
		meta mark & 0x8cfd8000 == 0x8000 return
		udp dport 53 return
		tcp dport { 443 } ct original packets 0-19 queue num 537-540 bypass
	}
`
	if !nftListingQueues(listing, "tcp") {
		t.Error("expected the TCP queue rule to be found")
	}
	if nftListingQueues(listing, "udp") {
		t.Error("a listing without a UDP queue rule must report none, even with 'udp dport 53 return' present")
	}
}

func TestCaptureQueueRulesMissing(t *testing.T) {
	cfg := &config.Config{}
	cases := []struct {
		name     string
		tcp, udp bool
		want     bool
	}{
		{"both present", true, true, false},
		{"tcp lost", false, true, true},
		{"udp lost while the default port stays configured", true, false, true},
		{"both lost", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := captureQueueRulesMissing(cfg, c.tcp, c.udp); got != c.want {
				t.Errorf("captureQueueRulesMissing(%v, %v) = %v, want %v", c.tcp, c.udp, got, c.want)
			}
		})
	}
}
