package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingCaptureFake(t *testing.T) {
	const file = "captures/tls_example.bin"
	captured := func() *SetConfig {
		set := NewSetConfig()
		set.TCP.SynFake = false
		set.Fragmentation.Combo.DecoyEnabled = false
		set.Faking.SNI = true
		set.Faking.SNIType = FakePayloadCapture
		set.Faking.PayloadFile = file
		return &set
	}

	cases := []struct {
		name     string
		contents []byte
		writeAt  string
		mutate   func(*SetConfig)
		missing  bool
		named    string
	}{
		{name: "the file is there", contents: []byte("hello")},
		{name: "the file is gone", missing: true, named: file},
		{name: "the file is empty", contents: []byte{}, missing: true, named: file},
		{name: "the bytes in memory do not stand in for the file", mutate: func(s *SetConfig) { s.Faking.PayloadData = []byte("kept from the run") }, missing: true, named: file},
		{name: "a readable file out of the captures folder", contents: []byte("hello"), writeAt: "../secret.bin", mutate: func(s *SetConfig) { s.Faking.PayloadFile = "../secret.bin" }, missing: true, named: "../secret.bin"},
		{name: "a readable file that is not a capture", contents: []byte("hello"), writeAt: "captures/notes.txt", mutate: func(s *SetConfig) { s.Faking.PayloadFile = "captures/notes.txt" }, missing: true, named: "captures/notes.txt"},
		{name: "another spelling of a path that is there", contents: []byte("hello"), mutate: func(s *SetConfig) { s.Faking.PayloadFile = "./captures//tls_example.bin" }},
		{name: "a fake SYN sends the capture too", mutate: func(s *SetConfig) { s.Faking.SNI, s.TCP.SynFake, s.TCP.SynFakeLen = false, true, 64 }, missing: true, named: file},
		{name: "a fake SYN of length 0 sends none of it", mutate: func(s *SetConfig) { s.Faking.SNI, s.TCP.SynFake = false, true }},
		{name: "a combo decoy sends the capture too", mutate: func(s *SetConfig) { s.Faking.SNI, s.Fragmentation.Combo.DecoyEnabled = false, true }, missing: true, named: file},
		{name: "no fake is sent", mutate: func(s *SetConfig) { s.Faking.SNI = false }},
		{name: "a built-in fake with a stale file name", mutate: func(s *SetConfig) { s.Faking.SNIType = FakePayloadDefault1 }},
		{name: "a domain fake with a stale file name", mutate: func(s *SetConfig) { s.Faking.SNIType, s.Faking.PayloadDomain = FakePayloadDomain, "ya.ru" }},
		{name: "no file is named", mutate: func(s *SetConfig) { s.Faking.PayloadFile = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "inner")
			cfg := NewConfig()
			cfg.ConfigPath = filepath.Join(dir, "b4.json")
			if c.contents != nil {
				writeAt := c.writeAt
				if writeAt == "" {
					writeAt = file
				}
				path := filepath.Join(dir, writeAt)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, c.contents, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			set := captured()
			if c.mutate != nil {
				c.mutate(set)
			}
			named, missing := cfg.MissingCaptureFake(set)
			if missing != c.missing {
				t.Fatalf("missing = %t, want %t", missing, c.missing)
			}
			if c.missing && named != c.named {
				t.Errorf("named %q, want %q", named, c.named)
			}
		})
	}
}

func TestMissingCaptureFakeAgreesWithWhatASetLoads(t *testing.T) {
	const file = "captures/tls_example.bin"
	dir := t.TempDir()
	cfg := NewConfig()
	cfg.ConfigPath = filepath.Join(dir, "b4.json")

	set := NewSetConfig()
	set.Enabled = true
	set.Faking.SNI = true
	set.Faking.SNIType = FakePayloadCapture
	set.Faking.PayloadFile = file
	cfg.Sets = []*SetConfig{&set}

	cfg.LoadCapturePayloads()
	if _, missing := cfg.MissingCaptureFake(&set); !missing || len(set.Faking.PayloadData) != 0 {
		t.Fatalf("no file: missing %t with %d bytes loaded", missing, len(set.Faking.PayloadData))
	}

	path := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.LoadCapturePayloads()
	if _, missing := cfg.MissingCaptureFake(&set); missing || len(set.Faking.PayloadData) == 0 {
		t.Fatalf("with the file: missing %t with %d bytes loaded", missing, len(set.Faking.PayloadData))
	}
}

func TestSetNamesCapture(t *testing.T) {
	const file = "captures/tls_example.bin"
	cases := []struct {
		name   string
		mutate func(*SetConfig)
		names  bool
	}{
		{"its fake uses the file", func(s *SetConfig) { s.Faking.SNIType, s.Faking.PayloadFile = FakePayloadCapture, file }, true},
		{"its fake uses the file while faking is off", func(s *SetConfig) {
			s.Faking.SNI, s.Faking.SNIType, s.Faking.PayloadFile = false, FakePayloadCapture, file
		}, true},
		{"its UDP fake uses the file", func(s *SetConfig) { s.UDP.FakePayloadFile = file }, true},
		{"its fake spells the path another way", func(s *SetConfig) {
			s.Faking.SNIType, s.Faking.PayloadFile = FakePayloadCapture, "./captures//tls_example.bin"
		}, true},
		{"its UDP fake spells the path another way", func(s *SetConfig) { s.UDP.FakePayloadFile = "captures/./tls_example.bin" }, true},
		{"it names no file", func(s *SetConfig) { s.Faking.SNIType, s.Faking.PayloadFile = FakePayloadCapture, "" }, false},
		{"its fake type moved on from the file", func(s *SetConfig) { s.Faking.SNIType, s.Faking.PayloadFile = FakePayloadDefault1, file }, false},
		{"it uses another file", func(s *SetConfig) {
			s.Faking.SNIType, s.Faking.PayloadFile = FakePayloadCapture, "captures/tls_other.bin"
			s.UDP.FakePayloadFile = "captures/quic_other.bin"
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := NewSetConfig()
			c.mutate(&set)
			if got := set.NamesCapture(file); got != c.names {
				t.Errorf("NamesCapture = %t, want %t", got, c.names)
			}
		})
	}
}
