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
		"a save or payload change that alters running settings beyond collected addresses forgets these failures",
		"a connect error is, even one from the router's own route or firewall",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.wssize_reuse.enabled does not say %q: %s", want, facts)
		}
	}
	if strings.Contains(facts, "failures on b4's own side are not remembered") {
		t.Errorf("tcp.wssize_reuse.enabled still says failures on b4's own side are not remembered, but a connect error from the router's own route or firewall is: %s", facts)
	}
	if !strings.Contains(facts, "After a warm fails in one of the ways in parentheses above,") || strings.Contains(facts, "in one of the ways listed above") {
		t.Errorf("tcp.wssize_reuse.enabled does not limit the remembered failures to the list in parentheses; the cases listed after it (a skipped warm, an egress route that cannot be looked up, a failed pass, busy ports or slots) are not remembered: %s", facts)
	}
}

func TestWSReuseTopicSaysAPayloadReloadForgetsFailures(t *testing.T) {
	checkTopicWording(t, "tcp.wssize_reuse.enabled", []string{
		"a save or payload change that alters running settings beyond collected addresses forgets these failures.",
		"a skipped or cancelled warm, and a local port or capture error are not remembered",
	}, []string{
		"any saved settings change other than collected addresses forgets these failures",
		"a warm the client or a save cancelled",
	})
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

func TestWSReuseAgeTopicKeepsATupleOnlyWhenItsAnswerReachedTheClient(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.max_age_seconds")
	if !strings.Contains(facts, "a tuple whose last relayed connection passed no server data to the client before the client closed is dropped when that connection closes") {
		t.Errorf("tcp.wssize_reuse.max_age_seconds does not say that a tuple is kept only when server data reached the client before it closed: %s", facts)
	}
	if strings.Contains(facts, "got no data from the server") {
		t.Errorf("tcp.wssize_reuse.max_age_seconds still keeps a tuple on any data from the server, but server data read after the client left no longer keeps it: %s", facts)
	}
}

func TestWSReuseTopicsSayASilentPassFallsBackToTheSetsStrategy(t *testing.T) {
	age := TopicFacts("tcp.wssize_reuse.max_age_seconds")
	for _, want := range []string{
		"A silent pass is one whose client sent a lone ClientHello, without early data, and got no server data within 5 seconds:",
		"b4 closes it, drops the tuple and relays that ClientHello over a new connection from an ordinary source port with the set's own TCP strategy, flagged fallback in Traffic",
		"This is not remembered as a warm failure, so reuse stays on for that address.",
		"A pass whose server closes without data is not relayed again.",
	} {
		if !strings.Contains(age, want) {
			t.Errorf("tcp.wssize_reuse.max_age_seconds does not say %q: %s", want, age)
		}
	}
	enabled := TopicFacts("tcp.wssize_reuse.enabled")
	if !strings.Contains(enabled, "the client's bytes go out only after that choice, except on a silent pass (tcp.wssize_reuse.max_age_seconds).") {
		t.Errorf("tcp.wssize_reuse.enabled does not point to the silent pass, whose ClientHello goes out before the fallback is chosen: %s", enabled)
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

func TestWSReuseTopicSaysTheEngineLeavesReuseInactiveNotRefused(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.enabled")
	for _, want := range []string{
		"Domain/TLS filters, routing, duplication and escalation are refused.",
		"Under the TUN engine, with firewall setup skipped or a queue.mark overlapping 0x240000, reuse does nothing (one warning per set) and the set gets its usual strategy.",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("tcp.wssize_reuse.enabled does not say %q: %s", want, facts)
		}
	}
	if config.SelfDialRelayMark != 0x240000 {
		t.Errorf("tcp.wssize_reuse.enabled names the relay mark 0x240000, but config.SelfDialRelayMark is 0x%x", config.SelfDialRelayMark)
	}
	for _, stale := range []string{"TUN are refused", "Requires NFQUEUE, firewall setup"} {
		if strings.Contains(facts, stale) {
			t.Errorf("tcp.wssize_reuse.enabled still says %q, but the TUN engine, skipped firewall setup and an overlapping queue mark now leave a reuse set loaded and inactive with a warning: %s", stale, facts)
		}
	}
}

func TestWSReuseTopicSaysARefusedTProxyRuleLeavesTheSetUndiverted(t *testing.T) {
	facts := TopicFacts("tcp.wssize_reuse.enabled")
	if !strings.Contains(facts, "when it refuses the set's TCP TPROXY rule for an enabled address family") {
		t.Errorf("tcp.wssize_reuse.enabled does not say what a refused TPROXY rule does: %s", facts)
	}
	if !strings.Contains(facts, "for an enabled address family its ip_version allows,") {
		t.Errorf("tcp.wssize_reuse.enabled does not say that only a refused rule for a family the set's ip_version allows leaves it undiverted: %s", facts)
	}
	if !strings.Contains(facts, "b4 logs a warning for the set and tries the rule again") || strings.Contains(facts, "b4 logs that rule") {
		t.Errorf("tcp.wssize_reuse.enabled does not say that a refused TPROXY rule gets a warning for the set; the rule itself is logged only at Trace when the refusal reads 'No such file or directory': %s", facts)
	}
	if strings.Contains(facts, "connections that the firewall does not divert get") {
		t.Errorf("tcp.wssize_reuse.enabled still ties the set's strategy to what the firewall does not divert, which a refused TPROXY rule contradicts: %s", facts)
	}
	if !strings.Contains(facts, "In both cases the set's TCP/443 connections reaching b4's packet queue get its own TCP strategy too.") {
		t.Errorf("tcp.wssize_reuse.enabled does not name the set as the owner of the TCP/443 connections that still get its strategy without TPROXY; after 'b4' and 'the rule', a bare 'its' reads as b4's own connections: %s", facts)
	}
}
