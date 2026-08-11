package tproxy

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

// The listener's outbound dials must never carry the queue mark: that one also
// means "already handled by the engine", so those connections would leave with
// none of b4's own bypass applied - worst of all on a fail-open dial, made at
// the moment a destination is already in trouble.
func TestProxyBypassMark_IsSelfDialNotQueueMark(t *testing.T) {
	withQueueMark := &config.Config{}
	withQueueMark.Queue.Mark = 0x8000

	cases := map[string]uint32{
		"nil config":     proxyBypassMark(nil),
		"default":        proxyBypassMark(&config.Config{}),
		"queue mark set": proxyBypassMark(withQueueMark),
	}
	for name, got := range cases {
		if got != config.SelfDialMark {
			t.Errorf("%s: got 0x%x, want the self-dial mark 0x%x", name, got, config.SelfDialMark)
		}
		if got == uint32(withQueueMark.Queue.Mark) {
			t.Errorf("%s: the bypass mark must not be the queue mark 0x%x", name, got)
		}
	}
}
