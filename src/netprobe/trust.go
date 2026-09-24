package netprobe

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"sync"
)

const ProbeUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36"

var extraCASources = []string{
	"/opt/etc/ssl/certs/*.crt",
	"/opt/etc/ssl/certs/*.pem",
	"/opt/etc/ssl/cert.pem",
}

var (
	rootsOnce   sync.Once
	rootsPool   *x509.CertPool
	rootsVerify bool
)

func TLSRoots() (*x509.CertPool, bool) {
	rootsOnce.Do(func() {
		rootsPool, rootsVerify = loadTLSRoots(x509.SystemCertPool, extraCASources)
	})
	return rootsPool, rootsVerify
}

func loadTLSRoots(systemPool func() (*x509.CertPool, error), extra []string) (*x509.CertPool, bool) {
	empty := x509.NewCertPool()
	system, err := systemPool()
	hasSystem := err == nil && system != nil && !system.Equal(empty)

	pool := empty
	if hasSystem {
		pool = system.Clone()
	}
	loaded := false
	seen := map[string]bool{}
	for _, pattern := range extra {
		paths, _ := filepath.Glob(pattern)
		for _, path := range paths {
			real, err := filepath.EvalSymlinks(path)
			if err != nil || seen[real] {
				continue
			}
			seen[real] = true
			pem, err := os.ReadFile(real)
			if err == nil && pool.AppendCertsFromPEM(pem) {
				loaded = true
			}
		}
	}
	switch {
	case loaded:
		return pool, true
	case hasSystem:
		return nil, true
	}
	return nil, false
}
