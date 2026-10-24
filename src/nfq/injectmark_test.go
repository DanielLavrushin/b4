package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
)

func TestTheInjectorMarksThePacketsItSendsIntoConnections(t *testing.T) {
	cfg := config.NewConfig()
	if got, want := injectorMark(&cfg), cfg.Queue.Mark|engine.InjectedMarkBit; got != want {
		t.Errorf("the main pool's injector mark is %#x, want %#x: the firewall saves the connection mark of b4's own connections from the queue mark without that bit", got, want)
	}

	cfg.Queue.Mark = 0x400000
	if got, want := injectorMark(&cfg), uint(0x400000|engine.InjectedMarkBit); got != want {
		t.Errorf("with a configured queue mark the injector mark is %#x, want %#x", got, want)
	}

	discovery := config.NewConfig()
	discovery.Queue.IsDiscovery = true
	discovery.Queue.Mark = discovery.DiscoveryInjectedMark()
	if got := injectorMark(&discovery); got != discovery.Queue.Mark {
		t.Errorf("Discovery's injector mark is %#x, want its own %#x: Discovery's rules accept and return its injected packets by the exact mark", got, discovery.Queue.Mark)
	}
}

func TestSelfInjectedMark_PacketsSentIntoConnections(t *testing.T) {
	cfg := selfMarkConfig()
	for _, m := range []uint32{0x8000 | engine.InjectedMarkBit, 0x8000 | engine.InjectedMarkBit | 0x2bd8, uint32(injectorMark(cfg))} {
		if !selfInjectedMark(m, 0x8000, cfg) {
			t.Errorf("mark %#x is a packet b4 sent into a connection; dispatching it feeds b4 its own output", m)
		}
	}
	for _, m := range []uint32{engine.InjectedMarkBit, engine.InjectedMarkBit | 0x2bd8, 0x8000 | engine.InjectedMarkBit | engine.ClientMark} {
		if selfInjectedMark(m, 0x8000, cfg) {
			t.Errorf("mark %#x is not one b4's injector puts on a packet; accepting it would let traffic past the engine", m)
		}
	}
}

func TestOwnLookupsLeaveFromTheSocketWithoutTheInjectedBit(t *testing.T) {
	injector, lookups := &sock.Sender{}, &sock.Sender{}
	w := &Worker{sock: injector}
	if w.lookupSender() != injector {
		t.Error("a worker without a separate lookup socket (TUN, Discovery) must keep sending its lookups from its injector")
	}
	w.lookupSock = lookups
	if w.lookupSender() != lookups {
		t.Error("b4's own DNS lookups left from the injector, so their connections get the injected bit and the answers to them reach b4's queue")
	}
}
