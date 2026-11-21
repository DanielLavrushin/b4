package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func udpFakeSet(name string, fakeLen int, file string) *SetConfig {
	set := NewSetConfig()
	set.Id = name
	set.Name = name
	set.Enabled = true
	set.UDP.Mode = UDPModeFake
	set.UDP.FakeSeqLength = 1
	set.UDP.FakeLen = fakeLen
	set.UDP.FakePayloadFile = file
	return &set
}

func warningLines(buf *bytes.Buffer, parts ...string) []string {
	var out []string
	for _, line := range strings.Split(buf.String(), "\n") {
		all := true
		for _, p := range parts {
			if !strings.Contains(line, p) {
				all = false
				break
			}
		}
		if all && line != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestValidateRefusesANegativeUDPFakeLen(t *testing.T) {
	for _, fakeLen := range []int{-1, -100} {
		set := udpFakeSet("negative-udp", fakeLen, FakePayloadPreset1)
		err := l7Config(t, set).Validate()
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("udp.fake_len=%d accepted (value after Validate: %d, error %v); the UDP fake builder panics on it", fakeLen, set.UDP.FakeLen, err)
		}
		if len(verr.Fields) != 1 || verr.Fields[0].Path != "sets[0].udp.fake_len" || verr.Fields[0].Code != "out_of_range" {
			t.Errorf("udp.fake_len=%d: want one out_of_range error on sets[0].udp.fake_len, got %+v", fakeLen, verr.Fields)
		}
	}
	for _, fakeLen := range []int{0, 64, 1472} {
		set := udpFakeSet("sendable-udp", fakeLen, FakePayloadPreset1)
		if err := l7Config(t, set).Validate(); err != nil {
			t.Errorf("udp.fake_len=%d fits one packet and must load, got %v", fakeLen, err)
		}
		if set.UDP.FakeLen != fakeLen {
			t.Errorf("udp.fake_len=%d fits one packet and must stay as it is, got %d", fakeLen, set.UDP.FakeLen)
		}
	}
}

func TestValidateBringsAUDPFakeLenDownToWhatOnePacketCarries(t *testing.T) {
	buf := captureValidationWarnings(t)
	for _, c := range []struct {
		name, file string
		in, want   int
	}{
		{"quic-preset-1500", FakePayloadPreset1, 1500, 1472},
		{"quic-preset-1473", FakePayloadPreset1, 1473, 1472},
		{"quic-preset-1472", FakePayloadPreset1, 1472, 1472},
		{"stun-1500", FakePayloadAutoSTUN, 1500, 1472},
		{"stun-65504", FakePayloadAutoSTUN, 65504, 1472},
		{"stun-negative", FakePayloadAutoSTUN, -1, 20},
	} {
		set := udpFakeSet(c.name, c.in, c.file)
		if err := l7Config(t, set).Validate(); err != nil {
			t.Fatalf("%s: a value the slider and MCP offered must still load, got %v", c.name, err)
		}
		if set.UDP.FakeLen != c.want {
			t.Errorf("%s: udp.fake_len %d became %d, want %d: a %d-byte fake leaves no room in one 1500-byte IPv4 packet", c.name, c.in, set.UDP.FakeLen, c.want, c.in)
		}
		if c.in != c.want && len(warningLines(buf, "'"+c.name+"'", strconv.Itoa(c.want))) == 0 {
			t.Errorf("%s: no warning names the set and the new length %d; log: %s", c.name, c.want, buf.String())
		}
	}
}

func runUnique(name string) string {
	return name + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func writeCapture(t *testing.T, dir, file string, size int) {
	t.Helper()
	data := bytes.Repeat([]byte{0x16}, size)
	path := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestACaptureFileLargerThanAnUploadCanMakeIsNotLoaded(t *testing.T) {
	buf := captureValidationWarnings(t)
	for _, c := range []struct {
		name   string
		size   int
		loaded bool
	}{
		{"capture-at-upload-limit", 65536, true},
		{"capture-over-upload-limit", 65537, false},
		{"capture-of-a-megabyte", 1 << 20, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			file := "captures/tls_" + c.name + ".bin"
			writeCapture(t, dir, file, c.size)

			name := runUnique(c.name)
			set := NewSetConfig()
			set.Id, set.Name, set.Enabled = name, name, true
			set.Faking.SNI = true
			set.Faking.SNIType = FakePayloadCapture
			set.Faking.PayloadFile = file
			cfg := NewConfig()
			cfg.ConfigPath = filepath.Join(dir, "b4.json")
			cfg.Sets = []*SetConfig{&set}

			cfg.LoadCapturePayloads()

			if c.loaded {
				if len(set.Faking.PayloadData) != c.size {
					t.Fatalf("a %d-byte capture is within what an upload makes and must load, got %d bytes", c.size, len(set.Faking.PayloadData))
				}
				if _, missing := cfg.MissingCaptureFake(&set); missing {
					t.Errorf("a %d-byte capture loads, so it must not be reported missing", c.size)
				}
				return
			}
			if len(set.Faking.PayloadData) != 0 {
				t.Errorf("a %d-byte capture is larger than any upload and must not load, got %d bytes", c.size, len(set.Faking.PayloadData))
			}
			if len(warningLines(buf, "'"+name+"'", file)) == 0 {
				t.Errorf("no warning names the set and the file %s; log: %s", file, buf.String())
			}
			if _, missing := cfg.MissingCaptureFake(&set); !missing {
				t.Errorf("a %d-byte capture does not load, so the set falls back as for a missing file and must be reported missing", c.size)
			}
		})
	}
}

func TestAFakePayloadLargerThanOnePacketIsWarnedAboutUnlessMatchSizesIt(t *testing.T) {
	buf := captureValidationWarnings(t)
	dir := t.TempDir()
	const file = "captures/tls_vk_com_kyber.bin"
	writeCapture(t, dir, file, 1812)

	for _, c := range []struct {
		name   string
		mode   string
		custom int
		warned bool
	}{
		{name: "kyber-capture", warned: true},
		{name: "kyber-capture-matched", mode: "match"},
		{name: "long-custom", custom: 1449, warned: true},
		{name: "long-custom-matched", custom: 1449, mode: "match"},
		{name: "custom-of-one-packet", custom: 1448},
	} {
		name := runUnique(c.name)
		set := NewSetConfig()
		set.Id, set.Name, set.Enabled = name, name, true
		set.Faking.SNI = true
		set.Faking.FakeLenMode = c.mode
		if c.custom > 0 {
			set.Faking.SNIType = FakePayloadCustom
			set.Faking.CustomPayload = strings.Repeat("x", c.custom)
		} else {
			set.Faking.SNIType = FakePayloadCapture
			set.Faking.PayloadFile = file
		}
		cfg := NewConfig()
		cfg.ConfigPath = filepath.Join(dir, "b4.json")
		cfg.Sets = []*SetConfig{&set}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("%s: an oversize payload is warned about, never refused: %v", c.name, err)
		}

		size := strconv.Itoa(len(set.Faking.PayloadData))
		lines := warningLines(buf, "'"+name+"'", size)
		if c.custom == 0 && len(set.Faking.PayloadData) != 1812 {
			t.Fatalf("%s: setup: the 1812-byte capture did not load (%d bytes)", c.name, len(set.Faking.PayloadData))
		}
		switch {
		case c.warned && len(lines) == 0:
			t.Errorf("%s: a %s-byte fake payload with fake_len_mode %q does not fit one 1500-byte packet, and no warning names the set and its size; log: %s", c.name, size, c.mode, buf.String())
		case c.warned && c.custom == 0 && !strings.Contains(lines[0], file):
			t.Errorf("%s: the warning must name the file %s: %s", c.name, file, lines[0])
		case !c.warned && len(lines) > 0:
			t.Errorf("%s: a %s-byte fake payload with fake_len_mode %q is sendable, yet got %q", c.name, size, c.mode, lines)
		}
	}
}

func TestTheSecondBuiltInFakeIsNamedVK(t *testing.T) {
	if got := FakePayloadNames[FakePayloadDefault2]; got != "vk" {
		t.Fatalf("sni_type %d is the vk.com hello, so MCP writes, import notes and Discovery logs must call it \"vk\", got %q", FakePayloadDefault2, got)
	}
}
