package config

import (
	"path/filepath"
	"testing"
)

func TestAnEmptyPayloadChoiceIsWarnedAboutNotRefused(t *testing.T) {
	buf := captureValidationWarnings(t)
	for _, c := range []struct {
		name   string
		build  func(*SetConfig)
		warned bool
	}{
		{"generated-from-an-empty-domain", func(s *SetConfig) {
			s.Faking.SNIType = FakePayloadDomain
			s.Faking.PayloadDomain = ""
		}, true},
		{"payload-file-not-chosen", func(s *SetConfig) {
			s.Faking.SNIType = FakePayloadCapture
			s.Faking.PayloadFile = ""
		}, true},
		{"payload-file-not-chosen-faking-off", func(s *SetConfig) {
			s.Faking.SNI = false
			s.Faking.SNIType = FakePayloadCapture
			s.Faking.PayloadFile = ""
		}, false},
		{"generated-from-a-domain", func(s *SetConfig) {
			s.Faking.SNIType = FakePayloadDomain
			s.Faking.PayloadDomain = "www.example.com"
		}, false},
	} {
		name := runUnique(c.name)
		set := NewSetConfig()
		set.Id, set.Name, set.Enabled = name, name, true
		set.Faking.SNI = true
		c.build(&set)
		cfg := NewConfig()
		cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
		cfg.Sets = []*SetConfig{&set}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%s: an empty payload choice is warned about, never refused: %v", c.name, err)
		}
		lines := warningLines(buf, "[WARN]", "'"+name+"'")
		if c.warned && len(lines) == 0 {
			t.Errorf("%s: saves without a word while its fakes carry the built-in google payload; log: %s", c.name, buf.String())
		}
		if !c.warned && len(lines) > 0 {
			t.Errorf("%s: must not be warned about, got %q", c.name, lines)
		}
	}
}

func TestAnEmptyPayloadFileIsLoggedLikeAMissingOne(t *testing.T) {
	buf := captureValidationWarnings(t)
	dir := t.TempDir()
	writeCapture(t, dir, "captures/tls_empty_choice.bin", 0)
	writeCapture(t, dir, "captures/quic_empty_choice.bin", 0)

	for _, c := range []struct{ name, file string }{
		{"empty-tcp-file", "captures/tls_empty_choice.bin"},
		{"missing-tcp-file", "captures/tls_never_written_choice.bin"},
	} {
		name := runUnique(c.name)
		set := NewSetConfig()
		set.Id, set.Name, set.Enabled = name, name, true
		set.Faking.SNI = true
		set.Faking.SNIType = FakePayloadCapture
		set.Faking.PayloadFile = c.file
		cfg := NewConfig()
		cfg.ConfigPath = filepath.Join(dir, "b4.json")
		cfg.Sets = []*SetConfig{&set}

		cfg.LoadCapturePayloads()

		if set.Faking.PayloadData != nil {
			t.Errorf("%s: the set's payload must be unset, so it falls back like a missing file, got %d bytes (non-nil)", c.name, len(set.Faking.PayloadData))
		}
		if _, missing := cfg.MissingCaptureFake(&set); !missing {
			t.Errorf("%s: MissingCaptureFake must call %s missing", c.name, c.file)
		}
		if len(warningLines(buf, "[ERROR]", c.file)) == 0 {
			t.Errorf("%s: no error line names %s; log: %s", c.name, c.file, buf.String())
		}
	}

	name := runUnique("empty-udp-file")
	set := udpFakeSet(name, 64, "captures/quic_empty_choice.bin")
	cfg := NewConfig()
	cfg.ConfigPath = filepath.Join(dir, "b4.json")
	cfg.Sets = []*SetConfig{set}
	cfg.LoadCapturePayloads()
	if set.UDP.FakePayloadData != nil {
		t.Errorf("an empty UDP payload file must leave the UDP fake payload unset, got %d bytes (non-nil)", len(set.UDP.FakePayloadData))
	}
	if len(warningLines(buf, "[ERROR]", "captures/quic_empty_choice.bin")) == 0 {
		t.Errorf("no error line names the empty UDP payload file; log: %s", buf.String())
	}
}
