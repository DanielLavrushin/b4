package ai

import (
	"strings"
	"testing"
)

func TestSeqOverlapTopicSaysOnlyTheConnectionsFirstPacketGetsIt(t *testing.T) {
	facts := TopicFacts("fragmentation.seq_overlap_length")
	for _, says := range []string{
		"only a connection's first data packet gets them",
		"from the client's SYN",
		"A later request or ClientHello on the same connection",
		"whose SYN b4 did not see",
	} {
		if !strings.Contains(facts, says) {
			t.Errorf("the topic no longer says %q: %s", says, facts)
		}
	}
	for _, gone := range []string{"the check is per packet", "harmless once the server has those bytes"} {
		if strings.Contains(facts, gone) {
			t.Errorf("a later request on the connection carries no overlap, the topic still says %q: %s", gone, facts)
		}
	}
}
