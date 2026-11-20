package nfq

import (
	"fmt"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/ai"
)

func TestCollectedTopicsStateThePoolLimitAndTheCountFromTheConstants(t *testing.T) {
	count, ok := map[int]string{2: "two", 3: "three", 4: "four"}[silentHellosToCollect]
	if !ok {
		t.Fatalf("silentHellosToCollect is %d, which this test cannot spell", silentHellosToCollect)
	}
	for _, c := range []struct {
		topic string
		want  string
	}{
		{"targets.use_collected_ips", fmt.Sprintf("At most %d unique addresses", maxCollectedIPs)},
		{"targets.use_collected_ips", fmt.Sprintf("after %s silent connections in a row to the same address and port", count)},
		{"tcp.ip_block_detect.action", fmt.Sprintf("after %s connections in a row to the same address and port are silent", count)},
	} {
		if facts := ai.TopicFacts(c.topic); !strings.Contains(facts, c.want) {
			t.Errorf("%s does not say %q from the collector's constants: %s", c.topic, c.want, facts)
		}
	}
}
