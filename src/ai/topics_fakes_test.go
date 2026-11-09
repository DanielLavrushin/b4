package ai

import (
	"strings"
	"testing"
)

func checkTopicWording(t *testing.T, key string, says, gone []string) {
	t.Helper()
	facts := TopicFacts(key)
	if facts == "" {
		t.Fatalf("topic %s is missing", key)
	}
	for _, want := range says {
		if !strings.Contains(facts, want) {
			t.Errorf("%s no longer says %q: %s", key, want, facts)
		}
	}
	for _, stale := range gone {
		if strings.Contains(facts, stale) {
			t.Errorf("%s still says %q: %s", key, stale, facts)
		}
	}
	if strings.Contains(facts, "\u2014") {
		t.Errorf("%s contains an em-dash", key)
	}
}

func clauseBetween(t *testing.T, facts, from, to string) string {
	t.Helper()
	start := strings.Index(facts, from)
	if start < 0 {
		t.Fatalf("no %q in %s", from, facts)
	}
	rest := facts[start:]
	if end := strings.Index(rest, to); end > 0 {
		return rest[:end]
	}
	return rest
}

func TestFakeStrategyTopicNamesTheNoTimestampFallback(t *testing.T) {
	checkTopicWording(t, "faking.strategy",
		[]string{
			"faking.timestamp_decrease",
			"without TCP timestamps",
			"a wrong TCP checksum and a wrong acknowledgment number",
			"logs a warning naming the set and the connection",
			"once per set while b4 runs",
			"turn on TCP timestamps there",
			"pick another strategy",
		},
		[]string{
			"since otherwise the server would accept it",
			"the fake goes out with a bad checksum instead",
		})

	facts := TopicFacts("faking.strategy")
	timestamp := clauseBetween(t, facts, "'timestamp' (", "Picking the right one")
	if !strings.Contains(timestamp, "wrong acknowledgment number") {
		t.Errorf("the timestamp clause does not say its fallback moves the acknowledgment number: %s", timestamp)
	}
}

func TestTCPCheckTopicSaysTheAckMovesAndWhy(t *testing.T) {
	facts := TopicFacts("faking.strategy")
	tcpCheck := clauseBetween(t, facts, "'tcp_check' (", "'md5sum' (")
	for _, want := range []string{
		"wrong TCP checksum",
		"wrong acknowledgment number",
		"acknowledges data it never sent",
		"November 2026",
		"only a bad TCP checksum",
	} {
		if !strings.Contains(tcpCheck, want) {
			t.Errorf("the tcp_check clause no longer says %q: %s", want, tcpCheck)
		}
	}
	if strings.Contains(tcpCheck, "the server's stack drops") {
		t.Errorf("the tcp_check clause still claims every server drops a bad checksum: %s", tcpCheck)
	}
}

func TestMD5TopicsAreConditionalOnTheServersStack(t *testing.T) {
	facts := TopicFacts("faking.strategy")
	md5sum := clauseBetween(t, facts, "'md5sum' (", "'timestamp' (")
	for _, want := range []string{"kind 19, length 18", "checks the option", "takes the fake as data", "no fake is sent"} {
		if !strings.Contains(md5sum, want) {
			t.Errorf("the md5sum clause no longer says %q: %s", want, md5sum)
		}
	}

	checkTopicWording(t, "faking.tcp_md5",
		[]string{"checks the option", "Linux built with TCP MD5 support", "faking.ttl", "not tcp.syn_ttl"},
		[]string{"Real servers without an MD5 key configured silently discard", "treats the fake SYN as a connection attempt"})

	checkTopicWording(t, "faking.md5_on_fake",
		[]string{"checks the option", "Linux built with TCP MD5 support", "takes the fake as data"},
		[]string{"The server discards the signed fake because it has no MD5 key configured", "no fake is sent"})

	for _, key := range []string{"faking.tcp_md5", "faking.md5_on_fake"} {
		for _, cdn := range []string{"Cloudflare", "CloudFront", "Fastly", "Akamai"} {
			if strings.Contains(TopicFacts(key), cdn) {
				t.Errorf("%s names %s, but no CDN's MD5 handling was measured", key, cdn)
			}
		}
	}
}

func TestFakeLengthTopicTellsBothTCPCheckFormsApart(t *testing.T) {
	checkTopicWording(t, "faking.fake_len_mode",
		[]string{"the connection must carry TCP timestamps", "only a bad TCP checksum", "wrong acknowledgment number", "newer form was measured"},
		[]string{"Pair with faking.strategy 'timestamp' or 'tcp_check' at full TTL", "takes a 'tcp_check' fake as data"})
}

func TestIncomingTopicsSayWhereTheFakesGo(t *testing.T) {
	checkTopicWording(t, "tcp.incoming.strategy",
		[]string{
			"from the client's address toward the server",
			"tcp.incoming.fake_ttl",
			"'all' applies all three to the same packet",
			"'rand' picks one of badsum, badseq, badack and all for each fake",
			"The modes 'reset', 'fin' and 'desync' do not use this setting",
		},
		[]string{"LOCAL TCP stack", "tries each in turn", "most DPI is more lax"})

	checkTopicWording(t, "tcp.incoming.mode",
		[]string{
			"header-only ACKs from the client toward the server",
			"exact sequence number the server expects and a valid checksum",
			"only tcp.incoming.fake_ttl keeps them",
			"'fake' ignores them",
		},
		[]string{"injects a fake server response", "so it doesn't trigger on every tiny ACK"})
}
