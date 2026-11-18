package mtproto

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func telegramSetTestConfig(enabled bool) *config.Config {
	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "tg", "Telegram", enabled
	cfg.Sets = []*config.SetConfig{&set}
	cfg.System.MTProto.UpstreamSet = "tg"
	return &cfg
}

func useTelegramSet(t *testing.T, cfg *config.Config) {
	t.Helper()
	setTelegramSet(cfg)
	t.Cleanup(func() { setTelegramSet(nil) })
}

func everyRouteKind() map[string]transportPlan {
	return map[string]transportPlan{
		"direct TCP":         {kind: transportTCP, addr: "149.154.167.51:443"},
		"DC relay":           {kind: transportTCP, addr: "198.51.100.7:7008", relay: true},
		"Telegram's WS edge": {kind: transportWS, sni: "kws2.web.telegram.org", dialHost: "149.154.167.220", native: true},
		"CF reserve":         {kind: transportWS, sni: "kws2.example.co.uk", cfBase: "example.co.uk"},
		"custom WS domain":   {kind: transportWS, sni: "kws2.relay.example", cfBase: "relay.example"},
		"Worker":             {kind: transportWS, sni: "w.example.workers.dev", dialHost: "w.example.workers.dev", isWorker: true},
	}
}

func TestEveryTelegramRouteCarriesTheTelegramSetMark(t *testing.T) {
	useTelegramSet(t, telegramSetTestConfig(true))

	base := selfDialMark()
	if base != uint(config.TelegramSetDialMark) {
		t.Fatalf("self-dial mark 0x%x, want 0x%x while a Telegram set is chosen", base, config.TelegramSetDialMark)
	}
	for name, p := range everyRouteKind() {
		if got := planDialMark(p, base); got != uint(config.TelegramSetDialMark) {
			t.Errorf("%s dials with 0x%x, want 0x%x: every route takes the Telegram set's strategy, the reserve and Workers included", name, got, config.TelegramSetDialMark)
		}
	}
}

func TestWithoutAUsableTelegramSetTheReserveAndWorkersStayUnprocessed(t *testing.T) {
	useTelegramSet(t, telegramSetTestConfig(false))

	base := selfDialMark()
	if base != uint(config.SelfDialMark) {
		t.Fatalf("self-dial mark 0x%x, want 0x%x with the chosen set disabled", base, config.SelfDialMark)
	}
	want := map[string]uint{
		"direct TCP":         uint(config.SelfDialMark),
		"DC relay":           uint(config.SelfDialMark),
		"Telegram's WS edge": uint(config.SelfDialMark),
		"CF reserve":         uint(config.SelfDialRelayMark),
		"custom WS domain":   uint(config.SelfDialRelayMark),
		"Worker":             uint(config.SelfDialRelayMark),
	}
	for name, p := range everyRouteKind() {
		if got := planDialMark(p, base); got != want[name] {
			t.Errorf("%s dials with 0x%x, want 0x%x as before the setting existed", name, got, want[name])
		}
	}
}

func TestTheCoverPathNeverCarriesTheTelegramSetMark(t *testing.T) {
	useTelegramSet(t, telegramSetTestConfig(true))
	if got := coverDialMark(); got != uint(config.SelfDialMark) {
		t.Fatalf("cover path mark 0x%x, want 0x%x: the fake SNI site has to be reached the way a local prober sees it", got, config.SelfDialMark)
	}
}

func TestTheProxyRestartsWhenTheTelegramSetTurnsOnOrOff(t *testing.T) {
	off, on := telegramSetTestConfig(false), telegramSetTestConfig(true)
	if !mtprotoNeedsRestart(off, on) || !mtprotoNeedsRestart(on, off) {
		t.Fatal("the pools keep the mark they were built with, so the proxy has to restart when the Telegram set turns on or off")
	}
	tuned := telegramSetTestConfig(true)
	tuned.Sets[0].Fragmentation.SNIPosition = 3
	if mtprotoNeedsRestart(on, tuned) {
		t.Fatal("a change to the set's strategy is read per packet and must not restart the proxy")
	}
}

func TestTheBridgeRebuildsItsWorkerPoolForTheTelegramSet(t *testing.T) {
	off := telegramSetTestConfig(false)
	off.System.MTProto.CFWorkerDomain = "w.example.workers.dev"
	b := NewTransparentBridge(off)
	t.Cleanup(func() {
		b.Close()
		setTelegramSet(nil)
	})

	before := b.getPool()
	if before == nil || before.mark != uint(config.SelfDialMark) {
		t.Fatalf("pool %+v, want one built with 0x%x", before, config.SelfDialMark)
	}

	on := telegramSetTestConfig(true)
	on.System.MTProto.CFWorkerDomain = off.System.MTProto.CFWorkerDomain
	b.UpdateConfig(on)
	after := b.getPool()
	if after == nil || after == before || after.mark != uint(config.TelegramSetDialMark) {
		t.Fatalf("pool %+v after the Telegram set was chosen, want a new one built with 0x%x", after, config.TelegramSetDialMark)
	}
}

func TestTelegramSetProblemNamesWhatIsMissing(t *testing.T) {
	for _, c := range []struct {
		name    string
		change  func(*config.Config)
		problem bool
	}{
		{"a usable set", func(*config.Config) {}, false},
		{"no set chosen", func(c *config.Config) { c.System.MTProto.UpstreamSet = "" }, false},
		{"a missing set", func(c *config.Config) { c.System.MTProto.UpstreamSet = "gone" }, true},
		{"a disabled set", func(c *config.Config) { c.Sets[0].Enabled = false }, true},
		{"a block set", func(c *config.Config) {
			c.Sets[0].Routing.Enabled, c.Sets[0].Routing.Mode = true, config.RoutingModeBlock
		}, true},
		{"TUN mode", func(c *config.Config) { c.Queue.Mode = "tun" }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := telegramSetTestConfig(true)
			c.change(cfg)
			if got := telegramSetProblem(cfg) != ""; got != c.problem {
				t.Fatalf("problem %q, want one: %v", telegramSetProblem(cfg), c.problem)
			}
			if c.problem && cfg.TelegramUpstreamSet() != nil {
				t.Fatal("a set with a problem must not be used for dials")
			}
		})
	}
}

func TestTheConnectionTestUsesTheSetChoiceItIsGiven(t *testing.T) {
	setTelegramSet(nil)
	chosen, err := PlanTransportProbe(telegramSetTestConfig(true), 2)
	if err != nil {
		t.Fatal(err)
	}
	if chosen.mark != uint(config.TelegramSetDialMark) {
		t.Fatalf("probe mark 0x%x, want 0x%x: the test checks the set chosen on the page before it is saved", chosen.mark, config.TelegramSetDialMark)
	}
	if got := chosen.TelegramSet(); got != "Telegram" {
		t.Fatalf("probe reports set %q, want %q so the page can say which set the test used", got, "Telegram")
	}

	useTelegramSet(t, telegramSetTestConfig(true))
	none := telegramSetTestConfig(true)
	none.System.MTProto.UpstreamSet = ""
	unchosen, err := PlanTransportProbe(none, 2)
	if err != nil {
		t.Fatal(err)
	}
	if unchosen.mark != uint(config.SelfDialMark) {
		t.Fatalf("probe mark 0x%x, want 0x%x: clearing the choice on the page tests the path without a set, whatever is saved", unchosen.mark, config.SelfDialMark)
	}
	if got := unchosen.TelegramSet(); got != "" {
		t.Fatalf("probe reports set %q for a test without one", got)
	}

	unsaved := telegramSetTestConfig(true)
	unsaved.System.MTProto.UpstreamSet = "created-on-the-page"
	draft, err := PlanTransportProbe(unsaved, 2)
	if err != nil {
		t.Fatal(err)
	}
	if draft.TelegramSet() != "" || draft.mark != uint(config.SelfDialMark) {
		t.Fatalf("probe for a set the server does not know yet reports %q with mark 0x%x, want no set: the page has to be told the test ran without it", draft.TelegramSet(), draft.mark)
	}
}
