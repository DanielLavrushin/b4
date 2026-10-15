package ai

import (
	"sort"
	"strings"
	"testing"
)

const topicMaxLen = 2800

func TestTopicFactsAreWellFormed(t *testing.T) {
	keys := TopicKeys()
	if len(keys) == 0 {
		t.Fatal("no topics are embedded")
	}
	if !sort.StringsAreSorted(keys) {
		t.Error("TopicKeys must return sorted keys")
	}

	for _, key := range keys {
		if key != strings.ToLower(key) {
			t.Errorf("key %q must be lower case: lookups normalise the caller's path", key)
		}
		if strings.ContainsAny(key, " \t") {
			t.Errorf("key %q must not contain whitespace", key)
		}

		facts := TopicFacts(key)
		switch {
		case strings.TrimSpace(facts) == "":
			t.Errorf("%s has an empty body", key)
		case strings.ContainsAny(facts, "\n\r"):
			t.Errorf("%s contains a newline: entries are one paragraph of prose", key)
		case len(facts) > topicMaxLen:
			t.Errorf("%s is %d chars, over the %d cap: tool results share the model's context",
				key, len(facts), topicMaxLen)
		case len(facts) < 120:
			t.Errorf("%s is only %d chars, too short to be worth grounding on", key, len(facts))
		}
		for _, markup := range []string{"* ", "- ", "#", "|"} {
			if strings.HasPrefix(strings.TrimSpace(facts), markup) {
				t.Errorf("%s starts with %q: entries are plain prose, not markdown", key, markup)
			}
		}
	}
}

func TestDSCPTopicCountsLookupsForTheFirstPacket(t *testing.T) {
	facts := TopicFacts("dscp.enabled")
	i := strings.Index(facts, "from the first packet")
	if i < 0 {
		t.Fatal("dscp.enabled no longer says which addresses carry the set's value from the first packet")
	}
	start, end := strings.LastIndex(facts[:i], ". ")+2, len(facts)
	if n := strings.Index(facts[i:], ". "); n >= 0 {
		end = i + n
	}
	sentence := facts[start:end]
	for _, way := range []string{"DNS answer", "own lookup"} {
		if !strings.Contains(sentence, way) {
			t.Errorf("the first-packet sentence leaves out %q, although b4 learns addresses that way before the connection: %q", way, sentence)
		}
	}
}

func TestTopicFactsMissIsEmpty(t *testing.T) {
	if got := TopicFacts("no.such.setting"); got != "" {
		t.Errorf("an unknown topic must return empty so callers can detect the miss, got %q", got)
	}
}
