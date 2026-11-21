package ai

import (
	"strings"
	"testing"
)

func TestCollectTopicSaysAnInactiveReuseSetOwnsCollectionAsAnyOther(t *testing.T) {
	facts := TopicFacts("tcp.ip_block_detect.action")
	if !strings.Contains(facts, "but its rules are not in effect (a set whose reuse does nothing counts as any other), and any other must win the address match") {
		t.Errorf("tcp.ip_block_detect.action does not say that a reuse set whose reuse does nothing owns collection only as a queue consumer would: %s", facts)
	}
}
