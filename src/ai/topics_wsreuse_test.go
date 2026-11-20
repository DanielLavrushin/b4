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
		"a connect error is, even one from the router's own route or firewall",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.wssize_reuse.enabled does not say %q: %s", want, facts)
		}
	}
	if strings.Contains(facts, "failures on b4's own side are not remembered") {
		t.Errorf("tcp.wssize_reuse.enabled still says failures on b4's own side are not remembered, but a connect error from the router's own route or firewall is: %s", facts)
	}
}

func TestWSReuseTopicNamesWhatTheWarmNeedsBesidesTheServer(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.enabled")
	for _, want := range []string{
		"net.ipv4.tcp_timestamps not 0, and negotiated by the server",
		"CA certificates in the system store or under /opt/etc/ssl",
		"no TCP timestamps from the server",
		"its egress route cannot be looked up",
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

func TestWSReuseTopicSaysAnyHTTPStatusWarms(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.enabled")
	for _, want := range []string{
		"needs a complete final HTTP answer, whatever its status (interim 1xx answers are read past)",
		"more data or a reset from the server fails the warm",
		"a missing TLS close_notify does not",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.wssize_reuse.enabled does not say %q: %s", want, facts)
		}
	}
	if strings.Contains(facts, "then a clean TLS and TCP close") {
		t.Errorf("tcp.wssize_reuse.enabled still asks for a clean TLS close, but a server close without TLS close_notify still warms: %s", facts)
	}
	for _, stale := range []string{"204 answer", "another HTTP status"} {
		if strings.Contains(facts, stale) {
			t.Errorf("tcp.wssize_reuse.enabled still says %q, but any HTTP status now counts as a good warm: %s", stale, facts)
		}
	}
}

func TestWSReuseTopicSaysARefusedTProxyRuleLeavesTheSetUndiverted(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.enabled")
	if !strings.Contains(facts, "when it refuses the set's TCP TPROXY rule for an enabled address family") {
		t.Errorf("tcp.wssize_reuse.enabled does not say what a refused TPROXY rule does: %s", facts)
	}
	if strings.Contains(facts, "connections that the firewall does not divert get") {
		t.Errorf("tcp.wssize_reuse.enabled still ties the set's strategy to what the firewall does not divert, which a refused TPROXY rule contradicts: %s", facts)
	}
}
