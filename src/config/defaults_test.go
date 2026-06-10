package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/log"
)

func TestApplyDefaults_String(t *testing.T) {
	type S struct{ V string }

	t.Run("empty gets default", func(t *testing.T) {
		target := S{}
		defaults := S{V: "def"}
		ApplyDefaults(&target, &defaults)
		if target.V != "def" {
			t.Errorf("want def, got %q", target.V)
		}
	})

	t.Run("non-empty preserved", func(t *testing.T) {
		target := S{V: "user"}
		defaults := S{V: "def"}
		ApplyDefaults(&target, &defaults)
		if target.V != "user" {
			t.Errorf("want user, got %q", target.V)
		}
	})
}

func TestApplyDefaults_Int(t *testing.T) {
	type S struct{ V int }

	t.Run("zero gets default", func(t *testing.T) {
		target := S{}
		defaults := S{V: 42}
		ApplyDefaults(&target, &defaults)
		if target.V != 42 {
			t.Errorf("want 42, got %d", target.V)
		}
	})

	t.Run("non-zero preserved", func(t *testing.T) {
		target := S{V: 7}
		defaults := S{V: 42}
		ApplyDefaults(&target, &defaults)
		if target.V != 7 {
			t.Errorf("want 7, got %d", target.V)
		}
	})

	t.Run("explicit zero is clobbered by applier (design limitation)", func(t *testing.T) {
		// Reflection cannot distinguish "user set 0" from "unset". This is why
		// ApplyConfigDefaults is NOT called from LoadFromFile / LoadWithMigration —
		// NewConfig() + json.Unmarshal already yields correct defaults without
		// this ambiguity. Applier is kept for partial-preset fill-in (discovery).
		target := S{V: 0}
		defaults := S{V: 42}
		ApplyDefaults(&target, &defaults)
		if target.V != 42 {
			t.Errorf("applier is expected to clobber 0, got %d", target.V)
		}
	})
}

func TestApplyDefaults_Bool(t *testing.T) {
	type S struct{ V bool }

	t.Run("false stays false even when default is true", func(t *testing.T) {
		target := S{V: false}
		defaults := S{V: true}
		ApplyDefaults(&target, &defaults)
		if target.V != false {
			t.Errorf("want false, got %v", target.V)
		}
	})

	t.Run("true preserved", func(t *testing.T) {
		target := S{V: true}
		defaults := S{V: false}
		ApplyDefaults(&target, &defaults)
		if target.V != true {
			t.Errorf("want true, got %v", target.V)
		}
	})
}

func TestApplyDefaults_Slice(t *testing.T) {
	type S struct{ V []string }

	t.Run("nil gets default", func(t *testing.T) {
		target := S{}
		defaults := S{V: []string{"a", "b"}}
		ApplyDefaults(&target, &defaults)
		if len(target.V) != 2 || target.V[0] != "a" {
			t.Errorf("want [a b], got %v", target.V)
		}
	})

	t.Run("empty non-nil slice preserved (not clobbered)", func(t *testing.T) {
		target := S{V: []string{}}
		defaults := S{V: []string{"a"}}
		ApplyDefaults(&target, &defaults)
		if target.V == nil || len(target.V) != 0 {
			t.Errorf("want empty non-nil, got %v", target.V)
		}
	})

	t.Run("non-empty preserved", func(t *testing.T) {
		target := S{V: []string{"x"}}
		defaults := S{V: []string{"a"}}
		ApplyDefaults(&target, &defaults)
		if len(target.V) != 1 || target.V[0] != "x" {
			t.Errorf("want [x], got %v", target.V)
		}
	})
}

func TestApplyDefaults_Map(t *testing.T) {
	type S struct{ V map[string]int }

	t.Run("nil gets default", func(t *testing.T) {
		target := S{}
		defaults := S{V: map[string]int{"a": 1}}
		ApplyDefaults(&target, &defaults)
		if target.V["a"] != 1 {
			t.Errorf("want a=1, got %v", target.V)
		}
	})

	t.Run("non-nil preserved", func(t *testing.T) {
		target := S{V: map[string]int{}}
		defaults := S{V: map[string]int{"a": 1}}
		ApplyDefaults(&target, &defaults)
		if _, has := target.V["a"]; has {
			t.Errorf("want empty, got %v", target.V)
		}
	})
}

func TestApplyDefaults_NestedStruct(t *testing.T) {
	type Inner struct {
		Name string
		Port int
	}
	type Outer struct {
		In Inner
	}

	target := Outer{In: Inner{Name: "user"}}
	defaults := Outer{In: Inner{Name: "def", Port: 8080}}
	ApplyDefaults(&target, &defaults)

	if target.In.Name != "user" {
		t.Errorf("Name: want user, got %q", target.In.Name)
	}
	if target.In.Port != 8080 {
		t.Errorf("Port: want 8080, got %d", target.In.Port)
	}
}

func TestApplyConfigDefaults_NewConfigIsIdempotent(t *testing.T) {
	cfg := NewConfig()
	before := cfg.System.Logging

	ApplyConfigDefaults(&cfg)

	if cfg.System.Logging != before {
		t.Errorf("Logging changed: before=%+v after=%+v", before, cfg.System.Logging)
	}
}

func TestNewConfig_PrePopulatesBoolDefaults(t *testing.T) {
	cfg := NewConfig()

	if cfg.System.Logging.Instaflush != true {
		t.Errorf("Instaflush: want true from NewConfig(), got %v", cfg.System.Logging.Instaflush)
	}
	if cfg.System.WebServer.IsEnabled != true {
		t.Errorf("IsEnabled: want true from NewConfig(), got %v", cfg.System.WebServer.IsEnabled)
	}
}

func writeTempJSON(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write temp json: %v", err)
	}
	return path
}

// -----------------------------------------------------------------------------
// LoadFromFile scenarios — NewConfig()+unmarshal must give correct defaults
// for absent fields AND preserve explicit user values (zero/false/empty).
// -----------------------------------------------------------------------------

func TestLoadFromFile_LogLevelErrorZeroSurvives(t *testing.T) {
	// Regression: user picks "Error" (=0). Sparse save writes `level: 0`.
	// Reload must keep 0, not promote to LevelInfo.
	path := writeTempJSON(t, `{"version":34,"system":{"logging":{"level":0}}}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Logging.Level != log.LevelError {
		t.Errorf("Level: want %d (Error), got %d", log.LevelError, cfg.System.Logging.Level)
	}
}

func TestLoadFromFile_LogLevelAbsentUsesDefault(t *testing.T) {
	path := writeTempJSON(t, `{"version":34}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Logging.Level != log.LevelInfo {
		t.Errorf("Level absent from JSON should fall back to LevelInfo, got %d", cfg.System.Logging.Level)
	}
}

func TestLoadFromFile_BoolDefaultTrueAbsentStaysTrue(t *testing.T) {
	path := writeTempJSON(t, `{"version":34}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Logging.Instaflush != true {
		t.Errorf("Instaflush: want true (default), got %v", cfg.System.Logging.Instaflush)
	}
	if cfg.System.WebServer.IsEnabled != true {
		t.Errorf("IsEnabled: want true (default), got %v", cfg.System.WebServer.IsEnabled)
	}
}

func TestLoadFromFile_BoolDefaultTrueUserSetFalseSurvives(t *testing.T) {
	path := writeTempJSON(t, `{"version":34,"system":{"logging":{"instaflush":false}}}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Logging.Instaflush != false {
		t.Errorf("Instaflush: want false (user), got %v", cfg.System.Logging.Instaflush)
	}
}

func TestLoadFromFile_BoolDefaultFalseUserSetTrueSurvives(t *testing.T) {
	path := writeTempJSON(t, `{"version":34,"system":{"socks5":{"enabled":true}}}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Socks5.Enabled != true {
		t.Errorf("Socks5.Enabled: want true (user), got %v", cfg.System.Socks5.Enabled)
	}
	if cfg.System.Socks5.Port != 1080 {
		t.Errorf("Socks5.Port: want 1080 (default), got %d", cfg.System.Socks5.Port)
	}
}

func TestLoadFromFile_NumericZeroSurvives(t *testing.T) {
	path := writeTempJSON(t, `{"version":34,"queue":{"mark":0}}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Queue.Mark != 0 {
		t.Errorf("Queue.Mark: want 0 (user), got %d", cfg.Queue.Mark)
	}
}

func TestLoadFromFile_NumericAbsentUsesDefault(t *testing.T) {
	path := writeTempJSON(t, `{"version":34}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Queue.Threads != 4 {
		t.Errorf("Queue.Threads: want 4, got %d", cfg.Queue.Threads)
	}
	if cfg.Queue.StartNum != 537 {
		t.Errorf("Queue.StartNum: want 537, got %d", cfg.Queue.StartNum)
	}
	if cfg.System.WebServer.Port != 7000 {
		t.Errorf("WebServer.Port: want 7000, got %d", cfg.System.WebServer.Port)
	}
}

func TestLoadFromFile_StringEmptyUserSurvives(t *testing.T) {
	path := writeTempJSON(t, `{"version":34,"system":{"logging":{"directory":""}}}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Logging.Directory != "" {
		t.Errorf("Directory: want \"\" (user), got %q", cfg.System.Logging.Directory)
	}
}

func TestLoadFromFile_StringAbsentUsesDefault(t *testing.T) {
	path := writeTempJSON(t, `{"version":34}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Logging.Directory != "/var/log/b4" {
		t.Errorf("Directory default lost, got %q", cfg.System.Logging.Directory)
	}
	if cfg.System.WebServer.BindAddress != "0.0.0.0" {
		t.Errorf("WebServer.BindAddress default lost, got %q", cfg.System.WebServer.BindAddress)
	}
	if cfg.System.WebServer.Language != "en" {
		t.Errorf("WebServer.Language default lost, got %q", cfg.System.WebServer.Language)
	}
}

func TestLoadFromFile_NestedStructPartialKeepsSiblings(t *testing.T) {
	path := writeTempJSON(t, `{"version":34,"system":{"logging":{"level":0}}}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Logging.Level != log.LevelError {
		t.Errorf("Level: want 0, got %d", cfg.System.Logging.Level)
	}
	if cfg.System.Logging.Instaflush != true {
		t.Errorf("Instaflush sibling: want true, got %v", cfg.System.Logging.Instaflush)
	}
	if cfg.System.Logging.Directory != "/var/log/b4" {
		t.Errorf("Directory sibling: got %q", cfg.System.Logging.Directory)
	}
}

func TestLoadFromFile_SliceAbsentUsesDefault(t *testing.T) {
	path := writeTempJSON(t, `{"version":34}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	want := []string{"9.9.9.9", "1.1.1.1", "8.8.8.8", "9.9.1.1", "8.8.4.4"}
	if !reflect.DeepEqual(cfg.System.Checker.ReferenceDNS, want) {
		t.Errorf("ReferenceDNS: want %v, got %v", want, cfg.System.Checker.ReferenceDNS)
	}
}

func TestLoadFromFile_SliceEmptyUserSurvives(t *testing.T) {
	path := writeTempJSON(t, `{"version":34,"system":{"checker":{"reference_dns":[]}}}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.System.Checker.ReferenceDNS == nil {
		t.Errorf("ReferenceDNS: want empty non-nil slice, got nil")
	}
	if len(cfg.System.Checker.ReferenceDNS) != 0 {
		t.Errorf("ReferenceDNS: want empty, got %v", cfg.System.Checker.ReferenceDNS)
	}
}

func TestLoadFromFile_SetDefaultsPrePopulated(t *testing.T) {
	path := writeTempJSON(t, `{
		"version":34,
		"sets":[{"id":"abc","name":"test","tcp":{"conn_bytes_limit":19}}]
	}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if len(cfg.Sets) != 1 {
		t.Fatalf("want 1 set, got %d", len(cfg.Sets))
	}
	s := cfg.Sets[0]
	if s.Enabled != true {
		t.Errorf("Set.Enabled: want true (default), got %v", s.Enabled)
	}
	if s.TCP.SynTTL != 7 {
		t.Errorf("TCP.SynTTL: want 7 (default), got %d", s.TCP.SynTTL)
	}
	if s.Fragmentation.Strategy != "combo" {
		t.Errorf("Fragmentation.Strategy: want combo, got %q", s.Fragmentation.Strategy)
	}
	if s.Faking.SNI != true {
		t.Errorf("Faking.SNI: want true (default), got %v", s.Faking.SNI)
	}
}

func TestLoadFromFile_SetBoolFalseSurvives(t *testing.T) {
	path := writeTempJSON(t, `{
		"version":34,
		"sets":[{"id":"abc","name":"test","enabled":false}]
	}`)

	cfg := NewConfig()
	if err := cfg.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Sets[0].Enabled != false {
		t.Errorf("Set.Enabled: want false (user), got %v", cfg.Sets[0].Enabled)
	}
}

func TestLoadFromFile_SparseRoundtripPreservesZero(t *testing.T) {
	orig := NewConfig()
	orig.System.Logging.Level = log.LevelError

	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")
	if err := orig.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}

	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if loaded.System.Logging.Level != log.LevelError {
		t.Errorf("roundtrip lost Level=0, got %d", loaded.System.Logging.Level)
	}
}
