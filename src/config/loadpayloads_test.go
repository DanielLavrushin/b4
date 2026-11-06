package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestATCPFakeThatCannotLoadLeavesTheUDPFakeLoaded(t *testing.T) {
	const udpFile = "captures/quic_example.bin"
	udpBytes := []byte("the QUIC payload of this set")

	tcpFakes := []struct {
		name   string
		mutate func(*FakingConfig)
	}{
		{"a capture that is gone", func(f *FakingConfig) {
			f.SNIType, f.PayloadFile = FakePayloadCapture, "captures/tls_gone.bin"
		}},
		{"a capture path out of the folder", func(f *FakingConfig) {
			f.SNIType, f.PayloadFile = FakePayloadCapture, "../secret.bin"
		}},
		{"a capture type with no file named", func(f *FakingConfig) {
			f.SNIType, f.PayloadFile = FakePayloadCapture, ""
		}},
		{"a domain type with no domain named", func(f *FakingConfig) {
			f.SNIType, f.PayloadDomain = FakePayloadDomain, ""
		}},
	}
	udpFakes := []struct {
		file string
		want []byte
	}{
		{udpFile, udpBytes},
		{FakePayloadPreset1, FakeQUIC1},
		{FakePayloadPreset2, FakeQUIC2},
	}

	for _, tcp := range tcpFakes {
		for _, udp := range udpFakes {
			t.Run(tcp.name+", UDP "+udp.file, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, udpFile)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, udpBytes, 0o644); err != nil {
					t.Fatal(err)
				}

				set := NewSetConfig()
				set.Enabled = true
				tcp.mutate(&set.Faking)
				set.UDP.FakePayloadFile = udp.file
				cfg := NewConfig()
				cfg.ConfigPath = filepath.Join(dir, "b4.json")
				cfg.Sets = []*SetConfig{&set}

				cfg.LoadCapturePayloads()

				if len(set.Faking.PayloadData) != 0 {
					t.Errorf("the TCP fake has nothing to load, got %d bytes", len(set.Faking.PayloadData))
				}
				if !bytes.Equal(set.UDP.FakePayloadData, udp.want) {
					t.Errorf("the UDP fake's own payload must load, got %d bytes, want %d", len(set.UDP.FakePayloadData), len(udp.want))
				}
			})
		}
	}
}

func TestATCPCaptureWithNoConfigPathLeavesTheUDPPresetLoaded(t *testing.T) {
	set := NewSetConfig()
	set.Enabled = true
	set.Faking.SNIType, set.Faking.PayloadFile = FakePayloadCapture, "captures/tls_example.bin"
	set.UDP.FakePayloadFile = FakePayloadPreset1
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{&set}

	cfg.LoadCapturePayloads()

	if !bytes.Equal(set.UDP.FakePayloadData, FakeQUIC1) {
		t.Errorf("a preset needs no file, got %d bytes, want %d", len(set.UDP.FakePayloadData), len(FakeQUIC1))
	}
}
