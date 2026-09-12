package mtproto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func webListenerConfig(t *testing.T, port int) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.System.MTProto.Enabled = true
	cfg.System.MTProto.BindAddress = "127.0.0.1"
	cfg.System.MTProto.WebProxy.Enabled = true
	cfg.System.MTProto.WebProxy.Hostname = "relay.example.org"
	cfg.System.MTProto.WebProxy.Port = port
	return cfg
}

func fetchWithHost(t *testing.T, port int, host, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	var res *http.Response
	for i := 0; i < 50; i++ {
		res, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("relay port did not answer: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func TestWebListenerServesPlaceholderForForeignHosts(t *testing.T) {
	port := freeTCPPort(t)
	cfg := webListenerConfig(t, port)
	srv := &Server{}
	srv.cfg.Store(cfg)
	srv.mu.Lock()
	srv.startWebListenerLocked(cfg)
	srv.mu.Unlock()
	if srv.webSrv == nil {
		t.Fatal("relay listener did not start")
	}
	defer func() {
		srv.mu.Lock()
		srv.stopWebListenerLocked()
		srv.mu.Unlock()
	}()

	status, body := fetchWithHost(t, port, "203.0.113.5", "/")
	if status != http.StatusOK || !strings.Contains(body, "Service status") {
		t.Errorf("IP host root = %d %q, want 200 placeholder", status, body)
	}
	if strings.Contains(body, "B4") || strings.Contains(body, "<div id=\"root\"") {
		t.Error("the relay port leaked the b4 interface")
	}
	status, _ = fetchWithHost(t, port, "203.0.113.5", "/api/version")
	if status != http.StatusNotFound {
		t.Errorf("IP host /api/version = %d, want 404", status)
	}
	status, _ = fetchWithHost(t, port, "relay.example.org", "/")
	if status != http.StatusOK {
		t.Errorf("relay host root = %d, want 200", status)
	}
	if srv.WebProxyPort() != port {
		t.Errorf("WebProxyPort = %d, want %d", srv.WebProxyPort(), port)
	}
}

func TestWebListenerReconcilesOnConfigChange(t *testing.T) {
	first := freeTCPPort(t)
	cfg := webListenerConfig(t, first)
	srv := &Server{}
	srv.cfg.Store(cfg)
	srv.mu.Lock()
	srv.startWebListenerLocked(cfg)
	srv.mu.Unlock()
	defer func() {
		srv.mu.Lock()
		srv.stopWebListenerLocked()
		srv.mu.Unlock()
	}()
	fetchWithHost(t, first, "203.0.113.5", "/")

	same := *cfg
	srv.mu.Lock()
	before := srv.webSrv
	srv.reconcileWebListenerLocked(&same)
	if srv.webSrv != before {
		t.Error("an unchanged listener spec restarted the relay port")
	}
	srv.mu.Unlock()

	second := freeTCPPort(t)
	moved := *cfg
	moved.System.MTProto.WebProxy.Port = second
	srv.cfg.Store(&moved)
	srv.mu.Lock()
	srv.reconcileWebListenerLocked(&moved)
	srv.mu.Unlock()
	fetchWithHost(t, second, "203.0.113.5", "/")
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(first), 200*time.Millisecond); err == nil {
		t.Error("the old relay port is still open after the move")
	}

	off := moved
	off.System.MTProto.WebProxy.Port = 0
	srv.cfg.Store(&off)
	srv.mu.Lock()
	srv.reconcileWebListenerLocked(&off)
	if srv.webSrv != nil {
		t.Error("port 0 must fall back to the shared web server and close the relay port")
	}
	srv.mu.Unlock()
}

func TestWebSiteBodyPrefersCustomPage(t *testing.T) {
	cfg := webListenerConfig(t, 0)
	srv := &Server{}
	srv.cfg.Store(cfg)
	if got := string(srv.webSiteBody()); !strings.Contains(got, "Service status") {
		t.Fatalf("without a file the built-in page must be served, got %q", got)
	}
	path := WebProxyPagePath(cfg)
	if err := os.WriteFile(path, []byte("<html><body>custom site</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.ReloadWebProxyPage()
	if got := string(srv.webSiteBody()); got != "<html><body>custom site</body></html>" {
		t.Errorf("custom page not served, got %q", got)
	}
	if err := os.WriteFile(path, []byte("<html>v2</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.ReloadWebProxyPage()
	if got := string(srv.webSiteBody()); got != "<html>v2</html>" {
		t.Errorf("edited page not picked up, got %q", got)
	}
	os.Remove(path)
	srv.ReloadWebProxyPage()
	if got := string(srv.webSiteBody()); !strings.Contains(got, "Service status") {
		t.Errorf("removed page must restore the built-in one, got %q", got)
	}

	plain := serveWebProxyTo(t, srv, "relay.example.org", "/")
	if !strings.Contains(plain.body, "Service status") {
		t.Errorf("relay host root must serve the placeholder, got %q", plain.body)
	}
}

func TestWebListenerSpecFallsBackToWebServerTLS(t *testing.T) {
	cfg := webListenerConfig(t, 8443)
	cfg.System.WebServer.TLSCert = "/etc/b4/panel.crt"
	cfg.System.WebServer.TLSKey = "/etc/b4/panel.key"
	spec, ok := webListenerSpecFor(cfg)
	if !ok || spec.cert != "/etc/b4/panel.crt" || spec.addr != "127.0.0.1:8443" {
		t.Errorf("spec = %+v ok=%v, want the web server pair on 127.0.0.1:8443", spec, ok)
	}
	cfg.System.MTProto.WebProxy.TLSCert = "/etc/b4/relay.crt"
	cfg.System.MTProto.WebProxy.TLSKey = "/etc/b4/relay.key"
	spec, _ = webListenerSpecFor(cfg)
	if spec.cert != "/etc/b4/relay.crt" || spec.key != "/etc/b4/relay.key" {
		t.Errorf("own pair must win, got %+v", spec)
	}
	cfg.System.MTProto.WebProxy.Port = 0
	if _, ok := webListenerSpecFor(cfg); ok {
		t.Error("port 0 must not produce a listener spec")
	}
	cfg.System.MTProto.WebProxy.Port = 8443
	cfg.System.MTProto.Enabled = false
	if _, ok := webListenerSpecFor(cfg); ok {
		t.Error("a disabled proxy server must not produce a listener spec")
	}
}

func writeSelfSigned(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay.example.org"},
		DNSNames:     []string{"relay.example.org"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "relay.crt")
	keyPath := filepath.Join(dir, "relay.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestWebListenerServesTLSFromConfiguredPair(t *testing.T) {
	port := freeTCPPort(t)
	cfg := webListenerConfig(t, port)
	cfg.System.MTProto.WebProxy.TLSCert, cfg.System.MTProto.WebProxy.TLSKey = writeSelfSigned(t, filepath.Dir(cfg.ConfigPath))
	srv := &Server{}
	srv.cfg.Store(cfg)
	srv.mu.Lock()
	srv.startWebListenerLocked(cfg)
	srv.mu.Unlock()
	if srv.webSrv == nil {
		t.Fatal("relay listener did not start with a TLS pair")
	}
	defer func() {
		srv.mu.Lock()
		srv.stopWebListenerLocked()
		srv.mu.Unlock()
	}()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, _ := http.NewRequest(http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(port)+"/", nil)
	req.Host = "203.0.113.5"
	var res *http.Response
	var err error
	for i := 0; i < 50; i++ {
		res, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("TLS relay port did not answer: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "Service status") {
		t.Errorf("got %d %q over TLS, want the placeholder", res.StatusCode, body)
	}
	if res.TLS == nil || res.TLS.PeerCertificates[0].Subject.CommonName != "relay.example.org" {
		t.Error("the configured certificate was not the one served")
	}
}

func TestWebListenerRefusesUnloadablePair(t *testing.T) {
	port := freeTCPPort(t)
	cfg := webListenerConfig(t, port)
	cfg.System.MTProto.WebProxy.TLSCert = filepath.Join(t.TempDir(), "missing.crt")
	cfg.System.MTProto.WebProxy.TLSKey = filepath.Join(t.TempDir(), "missing.key")
	srv := &Server{}
	srv.cfg.Store(cfg)
	srv.mu.Lock()
	srv.startWebListenerLocked(cfg)
	srv.mu.Unlock()
	if srv.webSrv != nil {
		srv.mu.Lock()
		srv.stopWebListenerLocked()
		srv.mu.Unlock()
		t.Fatal("a pair that does not load must not start the relay port as plain HTTP")
	}
}
