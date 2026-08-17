package ai

import (
	_ "embed"
	"encoding/json"
	"sort"
)

//go:embed topics.json
var topicsRaw []byte

var topicFacts = func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(topicsRaw, &m); err != nil {
		panic("ai: invalid topics.json: " + err.Error())
	}
	return m
}()

// TopicFacts returns authoritative b4-specific facts for the given UI topic
// key, or empty string if the topic is not yet documented. Used to ground LLM
// explanations so the model paraphrases known facts instead of inventing them.
func TopicFacts(topic string) string {
	return topicFacts[topic]
}

// TopicKeys returns every documented topic key, sorted, so callers can
// enumerate the grounding corpus without reaching into topics.json.
func TopicKeys() []string {
	keys := make([]string, 0, len(topicFacts))
	for k := range topicFacts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
