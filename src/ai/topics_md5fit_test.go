package ai

import (
	"strings"
	"testing"
)

func TestMD5TopicsSayWhatHappensOnTheFirstSegmentOfASplitHello(t *testing.T) {
	checkTopicWording(t, "faking.md5_on_fake",
		[]string{"continues in a later segment", "unless faking.strategy is 'md5sum'", "b4 sends the fake without it", "a fake no longer than that packet", "every 'match' fake of such a packet"},
		[]string{"no fake is sent"})

	md5sum := clauseBetween(t, TopicFacts("faking.strategy"), "'md5sum' (", "'timestamp' (")
	for _, want := range []string{"continues in a later segment", "a fake no longer than that packet", "cuts the difference off the fake's payload", "20 bytes for a 'match' fake", "TLS record and handshake lengths"} {
		if !strings.Contains(md5sum, want) {
			t.Errorf("the md5sum clause does not say %q: %s", want, md5sum)
		}
	}

	checkTopicWording(t, "faking.fake_len_mode",
		[]string{"'md5sum' fake has a TCP payload 20 bytes shorter", "under any other strategy a faking.md5_on_fake fake goes out without the option"},
		nil)
}

func TestUDPFakeLengthTopicStatesTheSTUNFitAndNoUnreachableCeiling(t *testing.T) {
	checkTopicWording(t, "udp.fake_len",
		[]string{"'@stun'", "a negative value included, becomes 20", "multiple of 4 no larger than 1472", "nothing below 30 bytes"},
		[]string{"16409"})
}
