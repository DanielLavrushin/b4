package discovery

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
)

func TestAPickedPayloadResolvesToItsTLSCapture(t *testing.T) {
	cfg := &config.Config{ConfigPath: filepath.Join(t.TempDir(), "b4.json")}
	manager := capture.GetManager(cfg)
	if err := os.MkdirAll(manager.GetOutputPath(), 0o755); err != nil {
		t.Fatal(err)
	}

	const (
		both     = "both-protocols.custompayload.example"
		quicOnly = "quic-only.custompayload.example"
	)
	hello := []byte{0x16, 0x03, 0x01, 0x00, 0x01, 0x01}
	quic := []byte{0xc0, 0x00, 0x00, 0x00, 0x01}
	saved := []struct{ protocol, domain string }{{"tls", both}, {"quic", both}, {"quic", quicOnly}}
	for _, c := range saved {
		data := quic
		if c.protocol == "tls" {
			data = hello
		}
		if err := manager.SaveUploadedCapture(c.protocol, c.domain, data); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, c := range saved {
			_ = manager.DeleteCapture(c.protocol, c.domain)
		}
	})

	for i := range 64 {
		got := loadCustomPayloads(cfg, []string{" " + strings.ToUpper(both) + " "})
		if len(got) != 1 {
			t.Fatalf("lookup %d: one name must load one capture, got %d", i, len(got))
		}
		if !bytes.Equal(got[0].Data, hello) {
			t.Fatalf("lookup %d: the fake goes out over TCP, so it must be the TLS capture, got %x", i, got[0].Data)
		}
		if !strings.Contains(got[0].Filepath, "tls_") {
			t.Fatalf("lookup %d: the set made from the result must name the TLS file, got %q", i, got[0].Filepath)
		}
	}

	if got := loadCustomPayloads(cfg, []string{quicOnly}); len(got) != 0 {
		t.Errorf("a QUIC capture is no TCP fake, so a name with only that must load nothing, got %+v", got)
	}
}
