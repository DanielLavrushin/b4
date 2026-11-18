package warmtuple

import (
	"net/netip"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestFlowLookupWithNoFlowsTakesNoLock(t *testing.T) {
	f := NewFlows()
	local := netip.MustParseAddrPort("192.0.2.1:40000")
	remote := netip.MustParseAddrPort("203.0.113.7:443")
	f.mu.Lock()
	done := make(chan *FlowProfile, 1)
	go func() { done <- f.Lookup(local, remote) }()
	select {
	case p := <-done:
		if p != nil {
			t.Fatal("profile from an empty registry")
		}
	case <-time.After(time.Second):
		f.mu.Unlock()
		<-done
		t.Fatal("a lookup in an empty registry waited for the registry lock")
	}
	f.mu.Unlock()
	var nilFlows *Flows
	if nilFlows.Active() != 0 || nilFlows.Lookup(local, remote) != nil {
		t.Fatal("a nil registry reports flows")
	}

	cfg := config.NewConfig()
	set := config.NewSetConfig()
	release, err := f.Register(local, remote, &cfg, &set, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if f.Active() != 1 || f.Lookup(local, remote) == nil {
		t.Fatalf("a registered flow is not found: active=%d", f.Active())
	}
	release()
	release()
	if f.Active() != 0 || f.Lookup(local, remote) != nil {
		t.Fatalf("a released flow is still counted: active=%d", f.Active())
	}
}
