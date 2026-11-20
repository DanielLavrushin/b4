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
