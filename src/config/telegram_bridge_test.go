package config

import (
	"net/netip"
	"reflect"
	"testing"
	"time"
)

func resetTelegramCIDRs(t *testing.T) {
	t.Helper()
	prev := telegramCIDRs.Load()
	t.Cleanup(func() { telegramCIDRs.Store(prev) })
	telegramCIDRs.Store(nil)
}

func TestTelegramBuiltinCIDRsAreCanonical(t *testing.T) {
	v4, v6 := 0, 0
	for _, entry := range TelegramBuiltinCIDRs {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			t.Fatalf("%q does not parse: %v", entry, err)
		}
		if p.Masked().String() != entry {
			t.Errorf("%q is not in canonical form, want %q", entry, p.Masked().String())
		}
		if p.Addr().Is4() {
			v4++
		} else {
			v6++
		}
	}
	cur := builtinTelegramCIDRs
	if cur.V4 != v4 || cur.V6 != v6 || len(cur.All) != len(TelegramBuiltinCIDRs) {
		t.Errorf("builtin list counts %d/%d of %d, want %d/%d of %d", cur.V4, cur.V6, len(cur.All), v4, v6, len(TelegramBuiltinCIDRs))
	}
}

func TestNormalizeTelegramCIDRsDropsUnsafeEntries(t *testing.T) {
	got := NormalizeTelegramCIDRs([]string{
		"91.108.4.5/22",
		"91.108.4.0/22",
		"149.154.167.220",
		"0.0.0.0/0",
		"10.0.0.0/8",
		"::/0",
		"2001::/16",
		"::ffff:1.2.3.4/128",
		"not an address",
		"2a0a:f280::/32",
	})
	want := []string{"91.108.4.0/22", "149.154.167.220/32", "2a0a:f280::/32"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSetTelegramCIDRsKeepsTheBuiltinFloor(t *testing.T) {
	resetTelegramCIDRs(t)

	if cur := CurrentTelegramCIDRs(); cur.Source != TelegramCIDRSourceBuiltin || len(cur.All) != len(TelegramBuiltinCIDRs) {
		t.Fatalf("an unset store must report the builtin list, got %s with %d entries", cur.Source, len(cur.All))
	}

	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	next, changed := SetTelegramCIDRs(TelegramCIDRSourceTelegram, at, []string{"91.108.56.0/22", "203.0.113.0/24"})
	if !changed {
		t.Error("adding a range must report a change")
	}
	if next.Source != TelegramCIDRSourceTelegram || !next.UpdatedAt.Equal(at) {
		t.Errorf("source/time not kept: %s %v", next.Source, next.UpdatedAt)
	}
	if len(next.All) != len(TelegramBuiltinCIDRs)+1 {
		t.Errorf("the downloaded list must be merged with the builtin one, got %d entries", len(next.All))
	}
	for _, p := range TelegramBuiltinCIDRs {
		found := false
		for _, q := range next.All {
			if p == q {
				found = true
			}
		}
		if !found {
			t.Errorf("builtin range %s missing after a download that did not list it", p)
		}
	}

	if _, changed := SetTelegramCIDRs(TelegramCIDRSourceMirror, at, []string{"203.0.113.0/24", "91.108.56.0/22"}); changed {
		t.Error("the same ranges from another source are not a change of the diverted addresses")
	}
}

func TestRoutingSetsWithTheBridgeOff(t *testing.T) {
	a, b := NewSetConfig(), NewSetConfig()
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{&a, &b}
	got := cfg.RoutingSets()
	if len(got) != 2 || got[0] != &a || got[1] != &b {
		t.Fatalf("with the bridge off RoutingSets must be the set list itself, got %d entries", len(got))
	}
}

func TestRoutingSetsWithTheBridgeOn(t *testing.T) {
	resetTelegramCIDRs(t)
	a := NewSetConfig()
	a.Id = "user"
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{&a}
	cfg.System.MTProto.Bridge.Enabled = true

	got := cfg.RoutingSets()
	if len(got) != 2 || got[1] != &a {
		t.Fatalf("the bridge set must come first and the user sets follow unchanged, got %d entries", len(got))
	}
	bridge := got[0]
	if !IsTelegramBridgeSet(bridge) || bridge.Name != TelegramBridgeSetName {
		t.Errorf("unexpected bridge set identity %q/%q", bridge.Id, bridge.Name)
	}
	if !bridge.Enabled || !bridge.Routing.Enabled || bridge.Routing.Mode != RoutingModeMTProtoWS {
		t.Errorf("bridge set is not an enabled mtproto-ws routing set: %+v", bridge.Routing)
	}
	if bridge.Routing.FWMark != TelegramBridgeMark {
		t.Errorf("bridge mark 0x%x, want the pinned 0x%x", bridge.Routing.FWMark, TelegramBridgeMark)
	}
	if !bridge.RoutingDivertsPackets() || !reflect.DeepEqual(bridge.Targets.IpsToMatch, CurrentTelegramCIDRs().All) {
		t.Errorf("bridge set must divert the current Telegram ranges, got %v", bridge.Targets.IpsToMatch)
	}
	if len(cfg.Sets) != 1 {
		t.Errorf("RoutingSets must not add the bridge to the saved set list")
	}
}

func TestValidateReleasesTheBridgeReservations(t *testing.T) {
	build := func() (*Config, *SetConfig, *SetConfig) {
		cfg := NewConfig()
		s := NewSetConfig()
		s.Id = TelegramBridgeSetID
		s.Name = "handmade"
		s.Routing.FWMark = TelegramBridgeMark
		other := NewSetConfig()
		other.Id = "other"
		other.Name = "other"
		other.Escalate.To = TelegramBridgeSetID
		cfg.Sets = []*SetConfig{&s, &other}
		return &cfg, &s, &other
	}

	cfg, s, other := build()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if s.Id == TelegramBridgeSetID || s.Id == "" {
		t.Fatalf("a user set kept the reserved id: %q", s.Id)
	}
	if other.Escalate.To != s.Id {
		t.Errorf("the escalation link was not moved to the new id: %q", other.Escalate.To)
	}
	if s.Routing.FWMark != 0 {
		t.Errorf("a user set kept the reserved routing mark 0x%x", s.Routing.FWMark)
	}

	cfg2, s2, _ := build()
	if err := cfg2.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if s2.Id != s.Id {
		t.Errorf("the replacement id must be stable across restarts, got %q then %q", s.Id, s2.Id)
	}
}

func TestIsTelegramBridgeMark(t *testing.T) {
	for mark, want := range map[uint32]bool{
		TelegramBridgeMark:              true,
		TelegramBridgeMark | 0x8000:     true,
		TelegramBridgeMark | 0x10000000: true,
		0:                               false,
		0x8000:                          false,
		SelfDialMark:                    false,
		TelegramBridgeMark + 1:          false,
		TelegramBridgeMark &^ 0x20000:   false,
	} {
		if got := IsTelegramBridgeMark(mark); got != want {
			t.Errorf("IsTelegramBridgeMark(0x%x) = %v, want %v", mark, got, want)
		}
	}
}
