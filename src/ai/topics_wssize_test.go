package ai

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestWSSizeTopicsSayWhatZeroAndTheCutoffMean(t *testing.T) {
	says := map[string][]string{
		"tcp.wssize.enabled": {
			"With both numbers 0 the switch changes no packet",
			"It is not tcp.win",
			"chosen at the SYN",
			"Only the NFQUEUE engine applies it",
			"wssize_unsupported",
			"targets.tls (the TLS version filter) does not limit the rewrite",
			"A Discovery run does not",
		},
		"tcp.wssize.window": {
			"0 means LEAVE THE WINDOW UNCHANGED",
			"a SYN's window is never scaled",
			"an MCP write refuses a value outside it at any time",
			"queue.tcp_conn_bytes_limit",
		},
		"tcp.wssize.scale": {
			"0 means LEAVE THE OPTION UNCHANGED",
			"b4 only lowers",
			"WHOLE connection",
		},
		"tcp.wssize.cutoff": {
			"'' (empty, the default): after the TLS ClientHello ends",
			"'" + config.WSSizeCutoffFirstData + "': after the first packet that carries data",
			"as zapret2 does",
			"is read as ''",
		},
	}
	seen := map[string]string{}
	for key, phrases := range says {
		facts := TopicFacts(key)
		for _, phrase := range phrases {
			if !strings.Contains(facts, phrase) {
				t.Errorf("%s no longer says %q: %s", key, phrase, facts)
			}
		}
		for _, gone := range []string{"meaningful zero", "bypass", "B4 ", "through first outgoing data packet inclusive by default", "5 seconds from SYN", "each config swap Discovery makes", "2 bytes at a time", "the range is checked only then"} {
			if strings.Contains(facts, gone) {
				t.Errorf("%s still says %q, which the code no longer does: %s", key, gone, facts)
			}
		}
		if other, dup := seen[facts]; dup {
			t.Errorf("%s repeats the text of %s: each setting needs its own facts", key, other)
		}
		seen[facts] = key
	}
}
