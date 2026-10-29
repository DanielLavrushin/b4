package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const iptCaptureChainWithDupOnly = `-N B4
-A B4 -m mark --mark 0x8000/0x8cfd8000 -j RETURN
-A B4 -p tcp -m set --match-set b4_dup_v4 dst -m multiport --dports 443 -m connbytes --connbytes 0:19 --connbytes-mode packets --connbytes-dir original -j NFQUEUE --queue-num 537 --queue-bypass
-A B4 -p udp -m multiport --dports 443 -m connbytes --connbytes 0:8 --connbytes-mode packets --connbytes-dir original -j NFQUEUE --queue-num 537 --queue-bypass
`

func TestIPTListingQueuesIgnoresDuplicateOnlyRules(t *testing.T) {
	if iptListingQueuesTCP(iptCaptureChainWithDupOnly, "B4") {
		t.Error("a rule restricted to the duplicate set must not pass for the general TCP capture rule: after the general rule is deleted the rest of the destinations would stay unqueued and nothing would repair them")
	}
	if !iptListingQueuesUDP(iptCaptureChainWithDupOnly, "B4") {
		t.Error("the surviving UDP capture rule is a real one and must still be found")
	}
}

func TestIPTListingQueuesStillFindsTheGeneralRuleNextToADuplicate(t *testing.T) {
	const listing = `-N B4
-A B4 -m mark --mark 0x8000/0x8cfd8000 -j RETURN
-A B4 -p tcp -m set --match-set b4_dup_v4 dst -m multiport --dports 443 -j NFQUEUE --queue-num 537 --queue-bypass
-A B4 -p tcp -m multiport --dports 443 -m connbytes --connbytes 0:19 --connbytes-mode packets --connbytes-dir original -j NFQUEUE --queue-num 537 --queue-bypass
`
	if !iptListingQueuesTCP(listing, "B4") {
		t.Error("the general TCP capture rule is present next to a duplicate rule and must be found")
	}
}

func TestNFTListingQueuesIgnoresDuplicateOnlyRules(t *testing.T) {
	const listing = `chain b4_chain {
		meta mark & 0x8cfd8000 == 0x8000 return
		udp dport 53 return
		meta nfproto ipv4 ip daddr @b4_dup_v4 tcp dport { 443 } ct original packets 0-19 queue num 537-540 bypass
		udp dport { 443 } ct original packets 0-8 queue num 537-540 bypass
	}
`
	if nftListingQueues(listing, "tcp") {
		t.Error("the duplicate-set rule must not pass for the general TCP capture rule in the nftables chain either")
	}
	if !nftListingQueues(listing, "udp") {
		t.Error("the UDP capture rule is still there and must be found")
	}
}

func TestDuplicateOnlyChainTriggersRepair(t *testing.T) {
	cfg := &config.Config{}
	tcpQueued := iptListingQueuesTCP(iptCaptureChainWithDupOnly, "B4")
	udpQueued := iptListingQueuesUDP(iptCaptureChainWithDupOnly, "B4")
	if !captureQueueRulesMissing(cfg, tcpQueued, udpQueued) {
		t.Error("a capture chain holding only the duplicate-set rule lost its general TCP rule, so the monitor has to rebuild the rules")
	}
}
