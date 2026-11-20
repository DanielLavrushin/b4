package ai

import (
	"strings"
	"testing"
)

func TestFilterL7TopicSaysACallsOnlySetCannotUseCollectedIPs(t *testing.T) {
	facts := TopicFacts("udp.filter_l7")
	if !strings.Contains(facts, "may carry no domain, IP, GeoSite, GeoIP or ASN target nor use collected IPs") {
		t.Errorf("udp.filter_l7 does not say that validation refuses Use collected IPs on a set that selects UDP protocols: %s", facts)
	}
}
