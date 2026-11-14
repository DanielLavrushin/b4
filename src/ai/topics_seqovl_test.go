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

func TestTopicsSayWhereDisorderCutsForItsOverlap(t *testing.T) {
	overlap := TopicFacts("fragmentation.seq_overlap_length")
	for _, says := range []string{
		"with it off, at fragmentation.disorder.split_position when that is set",
		"otherwise at byte 1, which never leaves room",
		"on every packet it splits",
	} {
		if !strings.Contains(overlap, says) {
			t.Errorf("the overlap length topic no longer says %q: %s", says, overlap)
		}
	}
	position := TopicFacts("fragmentation.disorder.split_position")
	for _, says := range []string{
		"Default 0, which keeps disorder's own three cuts",
		"Used only while SNI-based splitting (fragmentation.middle_sni) is off",
		"the position has to be greater than the overlap length",
	} {
		if !strings.Contains(position, says) {
			t.Errorf("the split position topic no longer says %q: %s", says, position)
		}
	}
	if says := "has no effect under combo, disorder"; !strings.Contains(TopicFacts("fragmentation.sni_position"), says) {
		t.Errorf("disorder has its own split position and never reads sni_position, the topic no longer says %q", says)
	}
}
