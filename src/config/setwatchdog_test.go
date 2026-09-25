package config

import "testing"

func watchableSet(id string) *SetConfig {
	set := NewSetConfig()
	set.Id, set.Name, set.Enabled = id, id, true
	set.Discovery.URLs = []string{"https://www.youtube.com/"}
	set.Discovery.Watchdog = true
	set.Targets.SNIDomains = []string{"youtube.com"}
	return &set
}

func TestSetWatchdogDefaultsOff(t *testing.T) {
	set := NewSetConfig()
	if set.Discovery.Watchdog {
		t.Fatal("a set's watchdog must default to off, the zero value")
	}
}

func TestValidateTurnsTheWatchdogOffOnlyForStructuralBlockers(t *testing.T) {
	disabled := watchableSet("disabled")
	disabled.Enabled = false

	routed := watchableSet("routed")
	routed.Routing.Enabled = true
	routed.Routing.EgressInterface = "wg0"

	noURLs := watchableSet("no-urls")
	noURLs.Discovery.URLs = []string{"http://192.168.1.1/"}

	devices := watchableSet("devices")
	devices.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}

	disabledAndRouted := watchableSet("disabled-routed")
	disabledAndRouted.Enabled = false
	disabledAndRouted.Routing.Enabled = true
	disabledAndRouted.Routing.EgressInterface = "wg0"

	ipOnly := watchableSet("ip-only")
	ipOnly.Targets.SNIDomains = nil
	ipOnly.Targets.IPs = []string{"203.0.113.7"}

	excluded := watchableSet("excluded")
	excluded.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	excluded.Targets.SourceDevicesExclude = true

	fine := watchableSet("fine")

	cfg := NewConfig()
	cfg.Sets = []*SetConfig{disabled, routed, noURLs, devices, disabledAndRouted, ipOnly, excluded, fine}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an unwatchable set must never fail validation: %v", err)
	}

	for _, set := range []*SetConfig{routed, devices, disabledAndRouted} {
		if set.Discovery.Watchdog {
			t.Errorf("set %q can never be watched as it is built, its watchdog must be turned off", set.Id)
		}
	}
	for _, set := range []*SetConfig{disabled, noURLs, ipOnly} {
		if !set.Discovery.Watchdog {
			t.Errorf("set %q keeps its watchdog flag while it is not watchable", set.Id)
		}
		if set.WatchdogActive() {
			t.Errorf("set %q is not watched while it is blocked", set.Id)
		}
	}
	for _, set := range []*SetConfig{excluded, fine} {
		if !set.Discovery.Watchdog || !set.WatchdogActive() {
			t.Errorf("set %q is watchable, its watchdog must stay on and be active", set.Id)
		}
	}
}

func TestWatchdogBlockerCodes(t *testing.T) {
	cases := map[string]func(*SetConfig){
		WatchdogBlockedDisabled: func(s *SetConfig) { s.Enabled = false },
		WatchdogBlockedRouted:   func(s *SetConfig) { s.Routing.Enabled = true },
		WatchdogBlockedNoURLs:   func(s *SetConfig) { s.Discovery.URLs = nil },
		WatchdogBlockedDevices:  func(s *SetConfig) { s.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"} },
		WatchdogBlockedIPOnly: func(s *SetConfig) {
			s.Targets.SNIDomains = nil
			s.Targets.IPs = []string{"203.0.113.7"}
			s.Targets.GeoIpCategories = []string{"ru"}
		},
	}
	for want, mutate := range cases {
		set := watchableSet("s")
		mutate(set)
		if got := set.WatchdogBlocker(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if set.WatchdogActive() {
			t.Errorf("%s: a blocked set is not watched", want)
		}
	}
	if !watchableSet("ok").WatchdogActive() {
		t.Error("an enabled, direct set with URLs and the flag on is watched")
	}

	routedOff := watchableSet("routed-off")
	routedOff.Enabled = false
	routedOff.Routing.Enabled = true
	if got := routedOff.WatchdogBlocker(); got != WatchdogBlockedRouted {
		t.Errorf("a disabled routed set reports the blocker that clears its flag on save, got %q", got)
	}
	geosite := watchableSet("geosite")
	geosite.Targets.SNIDomains = nil
	geosite.Targets.GeoSiteCategories = []string{"youtube"}
	if !geosite.WatchdogActive() {
		t.Error("a set that targets domains through geosite categories is watched")
	}
}

func TestResetToDefaultsKeepsTheWatchdogFlag(t *testing.T) {
	set := watchableSet("yt")
	set.ResetToDefaults()
	if !set.Discovery.Watchdog {
		t.Error("a strategy reset keeps the set's monitoring switch")
	}
}
