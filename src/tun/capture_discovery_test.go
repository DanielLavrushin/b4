package tun

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheCaptureChainReturnsDiscoveryMarksMissingAQueueMarkBit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		queueMark uint
		flow      uint
		injected  uint
		want      []string
		absent    []string
	}{
		{"default marks add no rule", 0x8000, 0x8001, 0x8002, nil, []string{"0x8001/", "0x8002/"}},
		{"explicit marks outside the queue mark", 0x8000, 0x100001, 0x100002, []string{"--mark 0x100001/0xffffffff -j RETURN", "--mark 0x100002/0xefffffff -j RETURN"}, nil},
		{"a queue mark with bit 0 leaves the derived flow mark out", 0x8001, 0x8002, 0x8003, []string{"--mark 0x8002/0xffffffff -j RETURN"}, []string{"0x8003/"}},
		{"an injected mark that carries the reinject bit is matched without it", 0x8000, 0x8001, 0x10100002, []string{"--mark 0x100002/0xefffffff -j RETURN"}, []string{"0x8001/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := orderedCaptureManager(true)
			r.mark = tc.queueMark
			r.discoveryFlow, r.discoveryInjected = tc.flow, tc.injected
			rules := r.captureChainRules([]string{"b4r_abc_v4"}, []string{"192.168.31.0/24"})
			firstMark := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "--set-xmark") })
			if firstMark < 0 {
				t.Fatal("no steer rule to order against")
			}
			for _, want := range tc.want {
				if i := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, want) }); i < 0 || i > firstMark {
					t.Errorf("%q must be in the chain ahead of the first steer rule (%d), got index %d", want, firstMark, i)
				}
			}
			for _, absent := range tc.absent {
				if i := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, absent) }); i >= 0 {
					t.Errorf("a mark the queue-mark rule already returns needs no rule of its own, got %q", strings.Join(rules[i].spec, " "))
				}
			}
		})
	}
}

func TestADiscoveryMarkChangeRebuildsTheCaptureChain(t *testing.T) {
	cfg := config.NewConfig()
	r := &routeManager{}
	r.setCaptureParams(captureParamsFrom(&cfg))
	if r.discoveryFlow != cfg.DiscoveryFlowMark() || r.discoveryInjected != cfg.DiscoveryInjectedMark() {
		t.Fatalf("the capture settings must carry Discovery's marks, got 0x%x 0x%x", r.discoveryFlow, r.discoveryInjected)
	}

	changed := cfg.Clone()
	changed.System.Checker.DiscoveryFlowMark = 0x100001
	if !r.updateCapture(captureParamsFrom(changed)) || !r.captureDirty || r.discoveryFlow != 0x100001 {
		t.Errorf("a new Discovery flow mark must mark the capture chain for a rebuild, got dirty=%v flow=0x%x", r.captureDirty, r.discoveryFlow)
	}
}
