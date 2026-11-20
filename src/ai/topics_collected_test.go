package ai

import (
	"strings"
	"testing"
)

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
