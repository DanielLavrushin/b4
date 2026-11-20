package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
)

func inactiveReuseHelloVerdict(t *testing.T, reuse bool, edit func(*config.Config)) engine.PacketVerdict {
	t.Helper()
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	set := newFragmentingYTImagesSet()
	set.Id, set.Name = "reuse-inactive", "reuse-inactive"
	set.Targets.DomainsToMatch = nil
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = reuse
	wsReuseDiverts = func(id string) bool { return id == set.Id }
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&set}
	if edit != nil {
		edit(&cfg)
	}
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	t.Cleanup(w.wg.Wait)
	hello := buildClientHello("r1.googlevideo.com", 16, 0xAB)
	return w.ProcessPacket(makeV4TCPPacket(hello, 1000))
}

func TestInactiveReuseSetKeepsItsStrategy(t *testing.T) {
	for _, c := range []struct {
		name    string
		edit    func(*config.Config)
		running bool
	}{
		{"the TUN engine", func(c *config.Config) { c.Queue.Mode = "tun" }, false},
		{"the TUN engine running, NFQUEUE saved", func(c *config.Config) { c.Queue.Mode = "nfqueue" }, true},
		{"firewall setup skipped", func(c *config.Config) { c.System.Tables.SkipSetup = true }, false},
		{"a queue mark with the relay bit", func(c *config.Config) { c.Queue.Mark = 0x48000 }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.running {
				config.SetRunningEngineTUN(true)
				t.Cleanup(func() { config.SetRunningEngineTUN(false) })
			}
			if got := inactiveReuseHelloVerdict(t, false, c.edit); got != engine.VerdictDrop {
				t.Fatalf("setup: the set's fragmentation did not run on its ClientHello without reuse (verdict %v), so this case proves nothing", got)
			}
			if got := inactiveReuseHelloVerdict(t, true, c.edit); got != engine.VerdictDrop {
				t.Errorf("%s: a reuse set's ClientHello to 443 was passed untouched (verdict %v, want drop by the set's fragmentation) because a diversion is published under its id; reuse does nothing here, so its connections must get the set's usual strategy", c.name, got)
			}
		})
	}
	t.Run("an active reuse set", func(t *testing.T) {
		if got := inactiveReuseHelloVerdict(t, true, nil); got != engine.VerdictAccept {
			t.Errorf("an active, diverted reuse set's ClientHello to 443 got verdict %v, want accept; the reuse relay owns that connection", got)
		}
	})
}
