package ai

import (
	"strings"
	"testing"
)

func TestIPPriorityTopicsSayWhereTheSwitchStops(t *testing.T) {
	cases := []struct {
		key        string
		says       []string
		mustNotSay []string
	}{
		{
			key: "targets.ip_priority",
			says: []string{
				"It has no effect while targets.tls is set.",
				"nameless datagrams stay with this set",
				"Escalation is per site",
				"a site escalated to a set ranked above this one takes this set's connections too",
				"a site escalated from this set moves to this set's backup on every address",
				"whatever this set's port filter;",
				"sets on this set's escalate.to chain still learn it",
				"but no other such set does, even one a site escalates to from a set ranked above",
				"place such backups above this set or on its chain",
				"still blocks DNS queries for its names",
				"so does a duplicate",
				"the Traffic page shows the set that handles the connection, this set or the backup it follows, as ip-set, with the domain and no sni-set",
			},
			mustNotSay: []string{
				"exactly as without the switch",
				"held off like any other set",
				"fitting version",
				"port and TLS filters",
				"QUIC counts as TLS 1.3",
				"started from the name set does not move it",
				"learned for one of this set's own escalated sites",
				"QUIC ignores learned entries",
				"shows this set as ip-set",
			},
		},
		{key: "targets.tls", says: []string{"targets.ip_priority has no effect while targets.tls is set"}},
		{
			key: "routing.enabled",
			says: []string{
				"a set higher in the list and without source devices holds with targets.ip_priority, when this set has no source devices either",
				"escalate.to chain",
				"or the set is skipped and the log says why",
			},
			mustNotSay: []string{"addresses that a set higher in the list holds with", "skipped silently"},
		},
		{
			key: "dscp.enabled",
			says: []string{
				"sets below one without source devices that has targets.ip_priority on",
				"targets.ip_priority on learn what it holds only if on its escalate.to chain",
				"never a private destination, except over TCP when a set lists its private range)",
			},
			mustNotSay: []string{"sets below one with targets.ip_priority on", "when a set's IP targets cover it"},
		},
	}
	for _, tc := range cases {
		facts := TopicFacts(tc.key)
		for _, want := range tc.says {
			if !strings.Contains(facts, want) {
				t.Errorf("%s no longer says %q", tc.key, want)
			}
		}
		for _, claim := range tc.mustNotSay {
			if strings.Contains(facts, claim) {
				t.Errorf("%s still says %q, which the code no longer does", tc.key, claim)
			}
		}
	}
}
