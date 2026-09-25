package netprobe

import (
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTLSRootsFallsBackToExtraBundles(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()
	bundle := filepath.Join(t.TempDir(), "ca-certificates.crt")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(bundle, block, 0o600); err != nil {
		t.Fatal(err)
	}
	empty := func() (*x509.CertPool, error) { return x509.NewCertPool(), nil }

	pool, verify := loadTLSRoots(empty, []string{filepath.Join(t.TempDir(), "missing.pem"), bundle})
	if !verify || pool == nil {
		t.Fatalf("an Entware bundle must be loaded when the system pool is empty, got pool=%v verify=%v", pool, verify)
	}
	if _, err := srv.Certificate().Verify(x509.VerifyOptions{Roots: pool}); err != nil {
		t.Errorf("the loaded pool must trust the bundle's certificate: %v", err)
	}

	if pool, verify := loadTLSRoots(empty, nil); verify || pool != nil {
		t.Errorf("with no bundle anywhere, verification must be off, got pool=%v verify=%v", pool, verify)
	}

	other := httptest.NewTLSServer(nil)
	defer other.Close()
	system := func() (*x509.CertPool, error) {
		p := x509.NewCertPool()
		p.AddCert(other.Certificate())
		return p, nil
	}
	if pool, verify := loadTLSRoots(system, nil); !verify || pool != nil {
		t.Errorf("a non-empty system pool alone is used as it is, got pool=%v verify=%v", pool, verify)
	}
	merged, verify := loadTLSRoots(system, []string{bundle})
	if !verify || merged == nil {
		t.Fatalf("Entware certificates are added to the system pool, got pool=%v verify=%v", merged, verify)
	}
	for _, cert := range []*x509.Certificate{srv.Certificate(), other.Certificate()} {
		if _, err := cert.Verify(x509.VerifyOptions{Roots: merged}); err != nil {
			t.Errorf("the merged pool trusts both sources: %v", err)
		}
	}
}

func TestLoadTLSRootsReadsACertificateDirectory(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()
	dir := t.TempDir()
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(dir, "ISRG_Root_X1.crt"), block, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ISRG_Root_X1.crt", filepath.Join(dir, "4042bcee.0")); err != nil {
		t.Fatal(err)
	}
	empty := func() (*x509.CertPool, error) { return x509.NewCertPool(), nil }
	pool, verify := loadTLSRoots(empty, []string{filepath.Join(dir, "*.crt"), filepath.Join(dir, "*.pem")})
	if !verify || pool == nil {
		t.Fatalf("the per-certificate layout of Entware's ca-certificates is read, got pool=%v verify=%v", pool, verify)
	}
	if _, err := srv.Certificate().Verify(x509.VerifyOptions{Roots: pool}); err != nil {
		t.Errorf("the pool trusts the directory's certificate: %v", err)
	}
}
