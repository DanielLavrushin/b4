package config

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/engine"
)

func setDSCPTestConfig(dscp SetDSCPConfig) (*Config, *SetConfig) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "video"
	set.Name = "Video"
	set.Targets.IPs = []string{"203.0.113.0/24"}
	set.DSCP = dscp
	cfg.Sets = []*SetConfig{&set}
	return &cfg, &set
}

func TestSetDSCPStamp(t *testing.T) {
	cases := []struct {
		dscp  SetDSCPConfig
		value int
		on    bool
	}{
		{SetDSCPConfig{}, 0, false},
		{SetDSCPConfig{Value: 7}, 0, false},
		{SetDSCPConfig{Enabled: true}, 0, true},
		{SetDSCPConfig{Enabled: true, Value: 46}, 46, true},
		{SetDSCPConfig{Enabled: true, Value: MaxDSCPValue}, MaxDSCPValue, true},
		{SetDSCPConfig{Enabled: true, Value: -1}, 0, false},
		{SetDSCPConfig{Enabled: true, Value: 64}, 0, false},
	}
	for _, tc := range cases {
		set := NewSetConfig()
		set.DSCP = tc.dscp
		if v, on := set.DSCPStamp(); v != tc.value || on != tc.on {
			t.Errorf("%+v: got value %d, on %v; want value %d, on %v", tc.dscp, v, on, tc.value, tc.on)
		}
	}
}

func TestAnySetDSCP(t *testing.T) {
	cfg := NewConfig()
	if cfg.AnySetDSCP() {
		t.Fatal("a configuration without sets has no set that stamps")
	}

	on := NewSetConfig()
	on.Id = "on"
	on.DSCP = SetDSCPConfig{Enabled: true, Value: 31}
	off := NewSetConfig()
	off.Id = "off"
	off.DSCP = SetDSCPConfig{Value: 31}
	disabled := NewSetConfig()
	disabled.Id = "disabled"
	disabled.Enabled = false
	disabled.DSCP = SetDSCPConfig{Enabled: true, Value: 31}

	cfg.Sets = []*SetConfig{nil, &off, &disabled}
	if cfg.AnySetDSCP() {
		t.Error("a set whose stamp is off and a disabled set must not count as stamping")
	}
	cfg.Sets = append(cfg.Sets, &on)
	if !cfg.AnySetDSCP() {
		t.Error("an enabled set with its stamp on must count as stamping")
	}
}

func TestSetDSCPRange(t *testing.T) {
	for _, bad := range []int{-1, 64, 255} {
		cfg, _ := setDSCPTestConfig(SetDSCPConfig{Enabled: true, Value: bad})
		other := NewSetConfig()
		other.Id = "first"
		other.Name = "First"
		cfg.Sets = append([]*SetConfig{&other}, cfg.Sets...)

		ve := mustValidationErr(t, cfg.Validate())
		f := findField(ve, "sets[1].dscp.value", "out_of_range")
		if f == nil {
			t.Fatalf("value %d: missing sets[1].dscp.value out_of_range; got %+v", bad, ve.Fields)
		}
		if f.Params["set"] != "Video" || f.Params["value"] != bad || f.Params["min"] != 0 || f.Params["max"] != MaxDSCPValue {
			t.Errorf("value %d: unexpected params %v", bad, f.Params)
		}
	}
	for _, good := range []int{0, 7, MaxDSCPValue} {
		cfg, set := setDSCPTestConfig(SetDSCPConfig{Enabled: true, Value: good})
		if err := cfg.Validate(); err != nil {
			t.Errorf("value %d must be accepted: %v", good, err)
		}
		if set.DSCP != (SetDSCPConfig{Enabled: true, Value: good}) {
			t.Errorf("value %d: validation changed the stamp to %+v", good, set.DSCP)
		}
	}
}

func TestSetDSCPValueUncheckedWhileOff(t *testing.T) {
	for _, value := range []int{-5, 64, 999} {
		cfg, set := setDSCPTestConfig(SetDSCPConfig{Value: value})
		if err := cfg.Validate(); err != nil {
			t.Errorf("value %d on a stamp that is off must not stop b4 from starting: %v", value, err)
		}
		if set.DSCP.Value != value {
			t.Errorf("value %d on a stamp that is off was rewritten to %d", value, set.DSCP.Value)
		}
	}
}

func TestSetDSCPClientMarkConflict(t *testing.T) {
	clientMark := uint(engine.ClientMark) | 0x8000

	cfg, _ := setDSCPTestConfig(SetDSCPConfig{Enabled: true, Value: 31})
	cfg.Queue.Mark = clientMark
	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "queue.mark", "mark_conflict") == nil {
		t.Fatalf("a queue mark carrying the client bit would exempt every injected packet from the set's stamp; got %+v", ve.Fields)
	}

	cfg, _ = setDSCPTestConfig(SetDSCPConfig{Value: 31})
	cfg.Queue.Mark = clientMark
	if err := cfg.Validate(); err != nil {
		t.Errorf("with the set's stamp off the same mark stays valid: %v", err)
	}

	cfg, set := setDSCPTestConfig(SetDSCPConfig{Enabled: true, Value: 31})
	set.Enabled = false
	cfg.Queue.Mark = clientMark
	if err := cfg.Validate(); err != nil {
		t.Errorf("a disabled set stamps nothing, so the same mark stays valid: %v", err)
	}
}

func TestSetDSCPRefusals(t *testing.T) {
	const mac = "AA:BB:CC:DD:EE:FF"
	selected := func(cfg *Config, set *SetConfig) {
		cfg.Queue.Devices.Enabled = true
		cfg.Queue.Devices.Devices = []Device{{MAC: mac, Selected: true}}
	}
	route := func(mode string) func(*Config, *SetConfig) {
		return func(_ *Config, set *SetConfig) {
			set.Routing.Enabled = true
			set.Routing.Mode = mode
		}
	}
	noTargets := func(_ *Config, set *SetConfig) {
		set.Targets.IPs = nil
		set.Targets.IpsToMatch = nil
		set.Targets.DomainsToMatch = nil
	}
	cases := []struct {
		name   string
		change []func(*Config, *SetConfig)
		want   string
	}{
		{"ip targets", nil, ""},
		{"domain targets", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.SNIDomains = []string{"example.com"}
			s.Targets.DomainsToMatch = []string{"example.com"}
		}}, ""},
		{"interface routing", []func(*Config, *SetConfig){route(RoutingModeInterface), func(_ *Config, s *SetConfig) {
			s.Routing.EgressInterface = "wg0"
		}}, ""},
		{"routing with an empty mode", []func(*Config, *SetConfig){route("")}, ""},
		{"block routing", []func(*Config, *SetConfig){route(RoutingModeBlock)}, DSCPRefusedRoutingBlock},
		{"proxy routing", []func(*Config, *SetConfig){route(RoutingModeProxy)}, DSCPRefusedRoutingProxy},
		{"mtproto-ws routing", []func(*Config, *SetConfig){route(RoutingModeMTProtoWS)}, DSCPRefusedRoutingMTProtoWS},
		{"block mode with routing off", []func(*Config, *SetConfig){func(_ *Config, s *SetConfig) {
			s.Routing.Mode = RoutingModeBlock
		}}, ""},
		{"included source devices", []func(*Config, *SetConfig){func(_ *Config, s *SetConfig) {
			s.Targets.SourceDevices = []string{mac}
		}}, DSCPRefusedSourceDevices},
		{"excluded source devices", []func(*Config, *SetConfig){func(_ *Config, s *SetConfig) {
			s.Targets.SourceDevices = []string{mac}
			s.Targets.SourceDevicesExclude = true
		}}, DSCPRefusedSourceDevices},
		{"blank source device entry", []func(*Config, *SetConfig){func(_ *Config, s *SetConfig) {
			s.Targets.SourceDevices = []string{" "}
		}}, ""},
		{"source interfaces with routing on", []func(*Config, *SetConfig){route(RoutingModeInterface), func(_ *Config, s *SetConfig) {
			s.Routing.SourceInterfaces = []string{"br-lan"}
		}}, DSCPRefusedSourceInterfaces},
		{"source interfaces with routing off", []func(*Config, *SetConfig){func(_ *Config, s *SetConfig) {
			s.Routing.SourceInterfaces = []string{"br-lan"}
		}}, ""},
		{"device filter with a selected device", []func(*Config, *SetConfig){selected}, DSCPRefusedDeviceFilter},
		{"device filter excluding a selected device", []func(*Config, *SetConfig){selected, func(c *Config, _ *SetConfig) {
			c.Queue.Devices.WhiteIsBlack = true
		}}, DSCPRefusedDeviceFilter},
		{"device filter with a selected manual device", []func(*Config, *SetConfig){func(c *Config, _ *SetConfig) {
			c.Queue.Devices.Enabled = true
			c.Queue.Devices.Devices = []Device{{MAC: "02:00:C0:A8:01:14", IP: "192.168.1.20", IsManual: true, Selected: true}}
		}}, DSCPRefusedDeviceFilter},
		{"device filter enabled with nothing selected", []func(*Config, *SetConfig){func(c *Config, _ *SetConfig) {
			c.Queue.Devices.Enabled = true
			c.Queue.Devices.Devices = []Device{{MAC: mac}}
		}}, ""},
		{"selected device with the device filter off", []func(*Config, *SetConfig){selected, func(c *Config, _ *SetConfig) {
			c.Queue.Devices.Enabled = false
		}}, ""},
		{"no targets", []func(*Config, *SetConfig){noTargets}, DSCPRefusedNoAddresses},
		{"domain only without ip targets", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.DomainOnly = true
			s.Targets.SNIDomains = []string{"example.com"}
			s.Targets.DomainsToMatch = []string{"example.com"}
		}}, DSCPRefusedNoAddresses},
		{"domain only with ip targets", []func(*Config, *SetConfig){func(_ *Config, s *SetConfig) {
			s.Targets.DomainOnly = true
			s.Targets.SNIDomains = []string{"example.com"}
			s.Targets.DomainsToMatch = []string{"example.com"}
		}}, ""},
		{"asn targets whose prefixes are not fetched yet", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.ASNs = []string{"64496"}
		}}, ""},
		{"geoip targets without a database entry", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.GeoIpCategories = []string{"zz"}
		}}, ""},
		{"geosite targets without a database", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.GeoSiteCategories = []string{"youtube"}
		}}, ""},
		{"domain targets not expanded yet", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.SNIDomains = []string{"example.com"}
		}}, ""},
		{"domain only with domains not expanded yet", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.DomainOnly = true
			s.Targets.SNIDomains = []string{"example.com"}
		}}, DSCPRefusedNoAddresses},
		{"domain only with geosite targets only", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.DomainOnly = true
			s.Targets.GeoSiteCategories = []string{"youtube"}
		}}, DSCPRefusedNoAddresses},
		{"domain only with asn targets not fetched yet", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.DomainOnly = true
			s.Targets.SNIDomains = []string{"example.com"}
			s.Targets.ASNs = []string{"64496"}
		}}, ""},
		{"domain only with geoip targets without a database entry", []func(*Config, *SetConfig){noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.DomainOnly = true
			s.Targets.SNIDomains = []string{"example.com"}
			s.Targets.GeoIpCategories = []string{"zz"}
		}}, ""},
		{"routing mode before source devices", []func(*Config, *SetConfig){route(RoutingModeProxy), selected, noTargets, func(_ *Config, s *SetConfig) {
			s.Targets.SourceDevices = []string{mac}
		}}, DSCPRefusedRoutingProxy},
		{"source devices before source interfaces", []func(*Config, *SetConfig){route(RoutingModeInterface), func(_ *Config, s *SetConfig) {
			s.Targets.SourceDevices = []string{mac}
			s.Routing.SourceInterfaces = []string{"br-lan"}
		}}, DSCPRefusedSourceDevices},
		{"source interfaces before the device filter", []func(*Config, *SetConfig){route(RoutingModeInterface), selected, func(_ *Config, s *SetConfig) {
			s.Routing.SourceInterfaces = []string{"br-lan"}
		}}, DSCPRefusedSourceInterfaces},
		{"device filter before missing addresses", []func(*Config, *SetConfig){selected, noTargets}, DSCPRefusedDeviceFilter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, set := setDSCPTestConfig(SetDSCPConfig{Enabled: true, Value: 31})
			set.Targets.IpsToMatch = append([]string(nil), set.Targets.IPs...)
			for _, change := range tc.change {
				change(cfg, set)
			}
			if got := cfg.DSCPRefusal(set); got != tc.want {
				t.Errorf("got refusal %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetDSCPRefusalIsNotAValidationError(t *testing.T) {
	cfg, set := setDSCPTestConfig(SetDSCPConfig{Enabled: true, Value: 31})
	set.Routing.Enabled = true
	set.Routing.Mode = RoutingModeBlock
	set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a refused set must still validate: %v", err)
	}
	if set.DSCP != (SetDSCPConfig{Enabled: true, Value: 31}) {
		t.Errorf("validation must keep the stamp of a refused set, got %+v", set.DSCP)
	}
}

func TestSetDSCPSparseRoundTrip(t *testing.T) {
	cases := []struct {
		dscp  SetDSCPConfig
		saved map[string]any
	}{
		{SetDSCPConfig{}, nil},
		{SetDSCPConfig{Enabled: true}, map[string]any{"enabled": true}},
		{SetDSCPConfig{Value: 12}, map[string]any{"value": float64(12)}},
		{SetDSCPConfig{Enabled: true, Value: 31}, map[string]any{"enabled": true, "value": float64(31)}},
	}
	for _, tc := range cases {
		cfg, _ := setDSCPTestConfig(tc.dscp)
		data, err := MarshalSparse(cfg)
		if err != nil {
			t.Fatalf("MarshalSparse: %v", err)
		}
		var raw struct {
			Sets []map[string]json.RawMessage `json:"sets"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(raw.Sets) != 1 {
			t.Fatalf("%+v: expected one saved set, got %s", tc.dscp, data)
		}
		saved, present := raw.Sets[0]["dscp"]
		if tc.saved == nil {
			if present {
				t.Errorf("%+v: the default stamp must be omitted from the saved file, got %s", tc.dscp, saved)
			}
		} else {
			var got map[string]any
			if err := json.Unmarshal(saved, &got); err != nil {
				t.Fatalf("%+v: saved dscp %s: %v", tc.dscp, saved, err)
			}
			if !reflect.DeepEqual(got, tc.saved) {
				t.Errorf("%+v: saved as %v, want %v", tc.dscp, got, tc.saved)
			}
		}

		back := NewConfig()
		if err := back.decodeFile(data); err != nil {
			t.Fatalf("%+v: decode: %v", tc.dscp, err)
		}
		if len(back.Sets) != 1 || back.Sets[0].DSCP != tc.dscp {
			t.Errorf("%+v: round trip gave %+v", tc.dscp, back.Sets)
		}
	}
}

func TestApplySetDefaultsKeepsDSCPZero(t *testing.T) {
	if DefaultSetConfig.DSCP != (SetDSCPConfig{}) {
		t.Fatalf("the default set stamp must be the zero value, or ApplySetDefaults overwrites a saved value 0; got %+v", DefaultSetConfig.DSCP)
	}

	set := NewSetConfig()
	set.DSCP = SetDSCPConfig{Enabled: true, Value: 0}
	ApplySetDefaults(&set)
	if set.DSCP != (SetDSCPConfig{Enabled: true, Value: 0}) {
		t.Errorf("ApplySetDefaults changed a stamp of 0 to %+v", set.DSCP)
	}

	var decoded SetConfig
	if err := json.Unmarshal([]byte(`{"id":"video","dscp":{"enabled":true}}`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ApplySetDefaults(&decoded)
	if decoded.DSCP != (SetDSCPConfig{Enabled: true, Value: 0}) {
		t.Errorf("a decoded stamp of 0 became %+v after ApplySetDefaults", decoded.DSCP)
	}
}

func TestSetDSCPLearnTTL(t *testing.T) {
	cases := []struct {
		name    string
		routing bool
		ttl     int
		dns     time.Duration
	}{
		{"routing off", false, 900, time.Hour},
		{"routing on", true, 900, 900 * time.Second},
		{"routing on without a ttl", true, 0, time.Hour},
	}
	for _, tc := range cases {
		set := NewSetConfig()
		set.Routing.Enabled = tc.routing
		set.Routing.IPTTLSeconds = tc.ttl
		if got := set.DSCPLearnTTL(false); got != tc.dns {
			t.Errorf("%s: DNS-learned ttl %v, want %v", tc.name, got, tc.dns)
		}
		if got := set.DSCPLearnTTL(true); got != 10*time.Minute {
			t.Errorf("%s: TLS-learned ttl %v, want the matcher's 10 minutes", tc.name, got)
		}
	}
}

func TestSetDSCPDoesNotNeedFirewallRefresh(t *testing.T) {
	before, _ := setDSCPTestConfig(SetDSCPConfig{})
	after, _ := setDSCPTestConfig(SetDSCPConfig{Enabled: true, Value: 31})
	if FirewallRefreshNeeded(before, after) {
		t.Error("a set's stamp must not ask for a full firewall refresh")
	}
}
