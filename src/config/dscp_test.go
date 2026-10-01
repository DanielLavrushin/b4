package config

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/engine"
)

func TestDSCPConfigEqual(t *testing.T) {
	base := DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0", "wg0"}}
	if !base.Equal(DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"wg0", "eth0"}}) {
		t.Error("interface order must not count as a change")
	}
	if base.Equal(DSCPConfig{Enabled: true, Value: 31, Interfaces: []string{"eth0", "wg0"}}) {
		t.Error("a new value must count as a change")
	}
	if base.Equal(DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0"}}) {
		t.Error("a different interface set must count as a change")
	}
	if base.Equal(DSCPConfig{Enabled: false, Value: 7, Interfaces: []string{"eth0", "wg0"}}) {
		t.Error("switching the stamp off must count as a change")
	}
	if !(DSCPConfig{Value: 7}).Equal(DSCPConfig{Value: 31, Interfaces: []string{"eth0"}}) {
		t.Error("the value and interfaces of a stamp that is off do not reach the firewall and must not count as a change")
	}
}

func TestDSCPStamp(t *testing.T) {
	cfg := NewConfig()
	if _, _, on := cfg.DSCPStamp(); on {
		t.Fatal("the default configuration must not stamp anything")
	}
	cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: 0}
	if v, ifaces, on := cfg.DSCPStamp(); !on || v != 0 || len(ifaces) != 0 {
		t.Errorf("value 0 is a valid stamp that clears the field, got value %d, interfaces %v, on %v", v, ifaces, on)
	}
	cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: 46, Interfaces: []string{" eth0 ", "eth0", "wg;0", "", "br-lan"}}
	v, ifaces, on := cfg.DSCPStamp()
	if !on || v != 46 || !reflect.DeepEqual(ifaces, []string{"eth0", "wg0", "br-lan"}) {
		t.Errorf("got value %d, interfaces %v, on %v", v, ifaces, on)
	}
	for _, bad := range []int{-1, 64} {
		cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: bad}
		if _, _, on := cfg.DSCPStamp(); on {
			t.Errorf("value %d must never reach the firewall", bad)
		}
	}
}

func TestValidateDSCP(t *testing.T) {
	for _, bad := range []int{-1, 64, 255} {
		cfg := NewConfig()
		cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: bad}
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "system.tables.dscp.value", "out_of_range") == nil {
			t.Errorf("value %d: missing system.tables.dscp.value out_of_range; got %+v", bad, ve.Fields)
		}
	}
	for _, good := range []int{0, 7, 63} {
		cfg := NewConfig()
		cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: good}
		if err := cfg.Validate(); err != nil {
			t.Errorf("value %d must be accepted: %v", good, err)
		}
	}
	cfg := NewConfig()
	cfg.System.Tables.DSCP = DSCPConfig{Enabled: false, Value: 99}
	if err := cfg.Validate(); err != nil {
		t.Errorf("an out-of-range value on a stamp that is off must not stop b4 from starting: %v", err)
	}

	cfg = NewConfig()
	cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0\"; flush ruleset", "eth0", ""}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := cfg.System.Tables.DSCP.Interfaces; !reflect.DeepEqual(got, []string{"eth0flushruleset", "eth0"}) {
		t.Errorf("interface names must be reduced to safe characters and deduplicated, got %q", got)
	}
}

func TestValidateDSCPRejectsAQueueMarkWithTheClientBit(t *testing.T) {
	cfg := NewConfig()
	cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: 7}
	cfg.Queue.Mark = uint(engine.ClientMark) | 0x8000
	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "queue.mark", "mark_conflict") == nil {
		t.Fatalf("a queue mark carrying the client bit would exempt every injected packet from the stamp; got %+v", ve.Fields)
	}

	cfg = NewConfig()
	cfg.Queue.Mark = uint(engine.ClientMark) | 0x8000
	if err := cfg.Validate(); err != nil {
		t.Errorf("with the stamp off the same mark stays valid: %v", err)
	}
}

func TestMarshalSparseDSCP(t *testing.T) {
	read := func(cfg *Config) map[string]any {
		t.Helper()
		data, err := MarshalSparse(cfg)
		if err != nil {
			t.Fatalf("MarshalSparse: %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		system, _ := raw["system"].(map[string]any)
		tables, _ := system["tables"].(map[string]any)
		dscp, _ := tables["dscp"].(map[string]any)
		return dscp
	}

	cfg := NewConfig()
	if dscp := read(&cfg); len(dscp) != 0 {
		t.Errorf("the default stamp must be omitted from the saved file, got %v", dscp)
	}

	cfg.System.Tables.DSCP = DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0"}}
	dscp := read(&cfg)
	if dscp["enabled"] != true || dscp["value"] != float64(7) {
		t.Errorf("a configured stamp must be saved, got %v", dscp)
	}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	back := NewConfig()
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal into defaults: %v", err)
	}
	if !back.System.Tables.DSCP.Equal(cfg.System.Tables.DSCP) {
		t.Errorf("round trip lost the stamp: %+v", back.System.Tables.DSCP)
	}
}
