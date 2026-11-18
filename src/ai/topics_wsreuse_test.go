package ai

import (
	"strings"
	"testing"
)

func TestWSReuseTopicSaysWhatEndsARelay(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.max_age_seconds")
	if !strings.Contains(facts, "The age never cuts or re-warms a connection that is already relayed.") {
		t.Errorf("tcp.wssize_reuse.max_age_seconds no longer says that the age leaves a relayed connection alone: %s", facts)
	}
	if strings.Contains(facts, "never interrupted or refreshed") {
		t.Errorf("tcp.wssize_reuse.max_age_seconds still claims active connections are never interrupted or refreshed, which a shutdown contradicts: %s", facts)
	}
}
