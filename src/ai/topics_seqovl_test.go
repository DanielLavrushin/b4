package ai

import (
	"strings"
	"testing"
)

func TestSeqOverlapTopicSaysTheCheckIsPerPacket(t *testing.T) {
	facts := TopicFacts("fragmentation.seq_overlap_length")
	for _, says := range []string{
		"the check is per packet",
		"On a later one they reach back over earlier bytes of the stream",
		"pipelining, or right after CONNECT",
	} {
		if !strings.Contains(facts, says) {
			t.Errorf("the topic no longer says %q: %s", says, facts)
		}
	}
	if strings.Contains(facts, "other packets carry no overlap") {
		t.Errorf("a later request on the connection does carry an overlap, the topic says otherwise: %s", facts)
	}
}
