package config

import (
	"path/filepath"
	"testing"
)

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
