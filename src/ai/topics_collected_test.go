package ai

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestCollectedTopicSaysRoutingAndReuseConsumersTakeEveryConnection(t *testing.T) {
	facts := TopicFacts("targets.use_collected_ips")
	for _, want := range []string{
		"A consumer that routes or reuses WSSIZE takes every connection its rules cover to a collected address, whatever the SNI or set order;",
		"other consumers follow the usual device/interface, family, port, set-order and IP-priority rules.",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("targets.use_collected_ips does not say %q: %s", want, facts)
		}
	}
	if strings.Contains(facts, "IP-priority rules still apply") {
		t.Errorf("targets.use_collected_ips still says IP-priority rules apply to every consumer, but a routing or WSSIZE-reuse consumer takes a collected address in the firewall whatever the set order: %s", facts)
	}
}

func TestCollectedTopicStatesTheExpiryFromTheConstant(t *testing.T) {
	hours := int(config.CollectedIPTTL / time.Hour)
	if time.Duration(hours)*time.Hour != config.CollectedIPTTL {
		t.Fatalf("CollectedIPTTL is %v, not whole hours: the topic states hours", config.CollectedIPTTL)
	}
	facts := TopicFacts("targets.use_collected_ips")
	if want := fmt.Sprintf("Each address expires %d hours after it was collected", hours); !strings.Contains(facts, want) {
		t.Errorf("targets.use_collected_ips does not say %q from config.CollectedIPTTL: %s", want, facts)
	}
}

func TestIPBlockDetectTopicWarnsThatAnEarlierVersionRunsBlock(t *testing.T) {
	facts := TopicFacts("tcp.ip_block_detect")
	for _, want := range []string{
		"An earlier b4 version without Collect ignores the action and runs Block",
		"turn this switch off in such sets before installing an earlier version",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.ip_block_detect does not say %q: %s", want, facts)
		}
	}
}

func TestCollectedTopicSaysWhereTheTakesFollowTheFirewallOnlyInPart(t *testing.T) {
	facts := TopicFacts("targets.use_collected_ips")
	for _, want := range []string{
		"ingress interface (reuse: any)",
		"or to source interfaces except for reuse",
		"also while that interface is down or gone without a kill switch, when such connections leave by the uplink yet are still treated as taken",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("targets.use_collected_ips does not say %q: %s", want, facts)
		}
	}
	if strings.Contains(facts, "(not when scoped to source devices or interfaces;") {
		t.Errorf("targets.use_collected_ips still says source interfaces keep a WSSIZE reuse consumer off the router's own connections, but the reuse firewall rules ignore them: %s", facts)
	}
}

func TestCollectTopicSaysTheFirstConsumerOwnsWhileItsRulesAreNotInEffect(t *testing.T) {
	facts := TopicFacts("tcp.ip_block_detect.action")
	if !strings.Contains(facts, "does if it routes or reuses but its rules are not in effect") {
		t.Errorf("tcp.ip_block_detect.action does not say the first routing or reusing consumer owns collection whenever its rules are not in effect: %s", facts)
	}
	if strings.Contains(facts, "has no rules yet") {
		t.Errorf("tcp.ip_block_detect.action still says only a consumer with no rules yet owns collection, but one whose egress never appeared, whose install failed or whose reuse is not diverted does too: %s", facts)
	}
}

func TestDSCPTopicNamesCollectedIPsAsAddressesOfTheSet(t *testing.T) {
	facts := TopicFacts("dscp.enabled")
	i := strings.Index(facts, "An address belongs to the set")
	if i < 0 {
		t.Fatalf("dscp.enabled no longer says which addresses belong to the set: %s", facts)
	}
	sentence := facts[i:]
	if end := strings.Index(sentence, ". "); end >= 0 {
		sentence = sentence[:end]
	}
	if !strings.Contains(sentence, "targets.use_collected_ips") {
		t.Errorf("the address sentence of dscp.enabled leaves out collected IPs, although the DSCP plan stamps them through SetConfig.TargetIPs and DSCPRefusal counts use_collected_ips as an address target: %q", sentence)
	}
}
