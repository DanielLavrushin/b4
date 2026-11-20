package ai

import (
	"fmt"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
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

func TestWSReuseTopicSaysAFailedWarmGetsTheSetsStrategy(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.enabled")
	for _, want := range []string{
		"When the warm fails",
		"when the pass after a good warm fails",
		"the set's own TCP strategy applies to that whole connection",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.wssize_reuse.enabled does not say %q: %s", want, facts)
		}
	}
	if strings.Contains(facts, "the configured strategy changes only warm packets on that port;") {
		t.Errorf("tcp.wssize_reuse.enabled still says the strategy changes only warm packets, which a failed warm contradicts: %s", facts)
	}
}

func TestWSReuseTopicSaysAFailedWarmIsSkippedForTenMinutes(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.enabled")
	for _, want := range []string{
		"that address and SNI name skip the warm for 10 minutes",
		"any saved settings change other than collected addresses forgets these failures",
		"A failed pass after a good warm and failures on b4's own side are not remembered.",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.wssize_reuse.enabled does not say %q: %s", want, facts)
		}
	}
}

func TestWSReuseAgeTopicStatesTheDefaultAndTheCap(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.max_age_seconds")
	for _, want := range []string{
		fmt.Sprintf("default %d, range 1-%d;", config.DefaultWSReuseMaxAgeSeconds, config.MaxWSReuseMaxAgeSeconds),
		fmt.Sprintf("a value above %d is refused on every set", config.MaxWSReuseMaxAgeSeconds),
		"warms a new source port",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.wssize_reuse.max_age_seconds does not say %q: %s", want, facts)
		}
	}
}
