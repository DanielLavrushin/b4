package tun

import (
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
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

func markSpecMatches(spec []string, mark uint) bool {
	for i := 0; i+1 < len(spec); i++ {
		if spec[i] != "--mark" {
			continue
		}
		value, mask, ok := strings.Cut(spec[i+1], "/")
		if !ok {
			return false
		}
		v, err1 := strconv.ParseUint(value, 0, 32)
		m, err2 := strconv.ParseUint(mask, 0, 32)
		return err1 == nil && err2 == nil && uint64(mark)&m == v
	}
	return false
}

func TestDiscoveryMarkReturnedAgreesWithTheEmittedReturnRules(t *testing.T) {
	for _, marks := range []struct{ queue, flow, injected uint }{
		{0x8000, 0x8001, 0x8002},
		{0x8000, 0x100001, 0x100002},
		{0x8001, 0x8002, 0x8003},
		{0x8000, 0x8001, 0x10100002},
		{0x8000, 0x200001, 0x8002},
	} {
		r := orderedCaptureManager(true)
		r.mark = marks.queue
		r.discoveryFlow, r.discoveryInjected = marks.flow, marks.injected
		rules := r.captureChainRules(nil, nil)
		returns := append([][]string{rules[0].spec}, discoveryCaptureReturns(marks.queue, marks.flow, marks.injected)...)
		for _, packet := range []uint{
			marks.flow, marks.injected, marks.injected | engine.ReinjectMarkBit,
			0x100001, 0x100002, 0x10100002, 0x200001, 0x200002, 0x8002, 0x8003, 0x1, 0,
		} {
			want := false
			for _, spec := range returns {
				if markSpecMatches(spec, packet) {
					want = true
				}
			}
			if got := discoveryMarkReturned(marks.queue, marks.flow, marks.injected, packet); got != want {
				t.Errorf("queue 0x%x, flow 0x%x, injected 0x%x: mark 0x%x returned=%v, but the emitted RETURN rules say %v", marks.queue, marks.flow, marks.injected, packet, got, want)
			}
		}
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
