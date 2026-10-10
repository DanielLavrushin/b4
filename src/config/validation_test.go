package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/engine"
)

func mustValidationErr(t *testing.T, err error) *ValidationError {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T (%v)", err, err)
	}
	return ve
}

func findField(ve *ValidationError, path, code string) *ValidationField {
	for i := range ve.Fields {
		f := &ve.Fields[i]
		if f.Path == path && f.Code == code {
			return f
		}
	}
	return nil
}

func TestValidate_PortInUse(t *testing.T) {
	t.Run("mtproto collides with web_server", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.MTProto.Enabled = true
		cfg.System.MTProto.Port = cfg.System.WebServer.Port

		ve := mustValidationErr(t, cfg.Validate())
		f := findField(ve, "system.mtproto.port", "port_in_use")
		if f == nil {
			t.Fatalf("missing system.mtproto.port port_in_use; got %+v", ve.Fields)
		}
		if f.Params["port"] != cfg.System.WebServer.Port {
			t.Errorf("expected params.port=%d, got %v", cfg.System.WebServer.Port, f.Params["port"])
		}
		if f.Params["conflict"] != "system.web_server.port" {
			t.Errorf("expected params.conflict=system.web_server.port, got %v", f.Params["conflict"])
		}
	})

	t.Run("socks5 collides with web_server", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Socks5.Enabled = true
		cfg.System.Socks5.Port = cfg.System.WebServer.Port

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "system.socks5.port", "port_in_use") == nil {
			t.Errorf("missing system.socks5.port port_in_use; got %+v", ve.Fields)
		}
	})

	t.Run("three-way collision reports both extras", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Socks5.Enabled = true
		cfg.System.MTProto.Enabled = true
		cfg.System.Socks5.Port = cfg.System.WebServer.Port
		cfg.System.MTProto.Port = cfg.System.WebServer.Port

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "system.socks5.port", "port_in_use") == nil ||
			findField(ve, "system.mtproto.port", "port_in_use") == nil {
			t.Errorf("expected port_in_use on both socks5 and mtproto; got %+v", ve.Fields)
		}
	})

	t.Run("disabled service is ignored", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.MTProto.Enabled = false
		cfg.System.MTProto.Port = cfg.System.WebServer.Port

		if err := cfg.Validate(); err != nil {
			t.Errorf("disabled mtproto on same port should not fail: %v", err)
		}
	})
}

func TestValidate_PortOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		path string
	}{
		{"mtproto > 65535", func(c *Config) {
			c.System.MTProto.Enabled = true
			c.System.MTProto.Port = 70000
		}, "system.mtproto.port"},
		{"socks5 > 65535", func(c *Config) {
			c.System.Socks5.Enabled = true
			c.System.Socks5.Port = 70000
		}, "system.socks5.port"},
		{"mtproto enabled with port 0", func(c *Config) {
			c.System.MTProto.Enabled = true
			c.System.MTProto.Port = 0
		}, "system.mtproto.port"},
		{"socks5 enabled with port 0", func(c *Config) {
			c.System.Socks5.Enabled = true
			c.System.Socks5.Port = 0
		}, "system.socks5.port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewConfig()
			tc.mut(&cfg)
			ve := mustValidationErr(t, cfg.Validate())
			if findField(ve, tc.path, "out_of_range") == nil {
				t.Errorf("missing %s out_of_range; got %+v", tc.path, ve.Fields)
			}
		})
	}
}

func TestValidate_TLSPair(t *testing.T) {
	t.Run("cert without key", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.WebServer.TLSCert = "/tmp/cert.pem"
		ve := mustValidationErr(t, cfg.ValidateTLSFiles())
		if findField(ve, "system.web_server.tls_cert", "tls_pair_required") == nil {
			t.Errorf("missing tls_pair_required; got %+v", ve.Fields)
		}
	})

	t.Run("missing cert file", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.WebServer.TLSCert = "/nonexistent/cert.pem"
		cfg.System.WebServer.TLSKey = "/nonexistent/key.pem"
		ve := mustValidationErr(t, cfg.ValidateTLSFiles())
		f := findField(ve, "system.web_server.tls_cert", "file_not_found")
		if f == nil {
			t.Fatalf("missing file_not_found; got %+v", ve.Fields)
		}
		if f.Params["path"] != "/nonexistent/cert.pem" {
			t.Errorf("expected params.path=/nonexistent/cert.pem, got %v", f.Params["path"])
		}
	})

	t.Run("cert exists but key missing", func(t *testing.T) {
		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		if err := os.WriteFile(certPath, []byte("dummy"), 0644); err != nil {
			t.Fatalf("failed to write cert file: %v", err)
		}
		cfg := NewConfig()
		cfg.System.WebServer.TLSCert = certPath
		cfg.System.WebServer.TLSKey = filepath.Join(dir, "missing.key")
		ve := mustValidationErr(t, cfg.ValidateTLSFiles())
		if findField(ve, "system.web_server.tls_key", "file_not_found") == nil {
			t.Errorf("missing tls_key file_not_found; got %+v", ve.Fields)
		}
	})

	t.Run("encrypted key", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeSelfSignedPair(t, dir)
		for name, body := range map[string]string{
			"pkcs8":  "-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIB\n-----END ENCRYPTED PRIVATE KEY-----\n",
			"legacy": "-----BEGIN RSA PRIVATE KEY-----\nProc-Type: 4,ENCRYPTED\nDEK-Info: AES-256-CBC,00\n\nMIIB\n-----END RSA PRIVATE KEY-----\n",
		} {
			if err := os.WriteFile(keyPath, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := NewConfig()
			cfg.System.WebServer.TLSCert = certPath
			cfg.System.WebServer.TLSKey = keyPath
			ve := mustValidationErr(t, cfg.ValidateTLSFiles())
			if findField(ve, "system.web_server.tls_key", "tls_key_encrypted") == nil {
				t.Errorf("%s: missing tls_key_encrypted; got %+v", name, ve.Fields)
			}
		}
	})

	t.Run("pair does not load", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeSelfSignedPair(t, dir)
		if err := os.WriteFile(keyPath, []byte("not a key"), 0600); err != nil {
			t.Fatal(err)
		}
		cfg := NewConfig()
		cfg.System.WebServer.TLSCert = certPath
		cfg.System.WebServer.TLSKey = keyPath
		ve := mustValidationErr(t, cfg.ValidateTLSFiles())
		if findField(ve, "system.web_server.tls_cert", "tls_pair_unloadable") == nil {
			t.Errorf("missing tls_pair_unloadable; got %+v", ve.Fields)
		}
	})

	t.Run("valid pair", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeSelfSignedPair(t, dir)
		cfg := NewConfig()
		cfg.System.WebServer.TLSCert = certPath
		cfg.System.WebServer.TLSKey = keyPath
		if err := cfg.ValidateTLSFiles(); err != nil {
			t.Errorf("valid pair rejected: %v", err)
		}
		if err := cfg.ValidateWebServerTLS(); err != nil {
			t.Errorf("valid pair rejected: %v", err)
		}
	})

	t.Run("mtproto relay pair only checked when mtproto is enabled", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.MTProto.WebProxy.TLSCert = "/nonexistent/cert.pem"
		cfg.System.MTProto.WebProxy.TLSKey = "/nonexistent/key.pem"
		if err := cfg.ValidateTLSFiles(); err != nil {
			t.Errorf("disabled mtproto must not be checked: %v", err)
		}
		if err := cfg.ValidateWebServerTLS(); err != nil {
			t.Errorf("web server check must ignore the relay pair: %v", err)
		}
		cfg.System.MTProto.Enabled = true
		cfg.System.MTProto.Port = 1443
		ve := mustValidationErr(t, cfg.ValidateTLSFiles())
		if findField(ve, "system.mtproto.web_proxy.tls_cert", "file_not_found") == nil {
			t.Errorf("missing relay file_not_found; got %+v", ve.Fields)
		}
	})

	t.Run("startup validation ignores TLS files", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.WebServer.TLSCert = "/nonexistent/cert.pem"
		cfg.System.WebServer.TLSKey = "/nonexistent/key.pem"
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate must not fail on TLS files: %v", err)
		}
	})
}

func writeSelfSignedPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "b4"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestValidate_GeoPathMissing(t *testing.T) {
	t.Run("geosite categories without path", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Geo.GeoSitePath = ""
		set := NewSetConfig()
		set.Id = "s1"
		set.Targets.GeoSiteCategories = []string{"youtube"}
		cfg.Sets = []*SetConfig{&set}

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "sets[0].targets.geosite_categories", "geosite_path_missing") == nil {
			t.Errorf("missing geosite_path_missing; got %+v", ve.Fields)
		}
	})

	t.Run("geoip categories without path", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Geo.GeoIpPath = ""
		set := NewSetConfig()
		set.Id = "s1"
		set.Targets.GeoIpCategories = []string{"ru"}
		cfg.Sets = []*SetConfig{&set}

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "sets[0].targets.geoip_categories", "geoip_path_missing") == nil {
			t.Errorf("missing geoip_path_missing; got %+v", ve.Fields)
		}
	})
}

func TestValidate_MSSClampScope(t *testing.T) {
	t.Run("source devices satisfy MAC scope", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.MSSClamp.Enabled = true
		set.MSSClamp.Size = 88
		set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid config, got %v", err)
		}
	})

	t.Run("excluded source devices do not satisfy MAC scope", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.MSSClamp.Enabled = true
		set.MSSClamp.Size = 88
		set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		set.Targets.SourceDevicesExclude = true
		cfg.Sets = []*SetConfig{&set}

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "sets[0].mss_clamp", "mss_clamp_scope_required") == nil {
			t.Errorf("missing mss_clamp_scope_required; got %+v", ve.Fields)
		}
	})

	t.Run("exclude with IP scope stays valid", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.MSSClamp.Enabled = true
		set.MSSClamp.Size = 88
		set.Targets.IPs = []string{"1.2.3.4"}
		set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
		set.Targets.SourceDevicesExclude = true
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid config, got %v", err)
		}
	})
}

func TestValidate_LoggingDirectory(t *testing.T) {
	t.Run("relative path rejected", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Logging.Directory = "home/data/b4"

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "system.logging.directory", "must_be_absolute") == nil {
			t.Errorf("missing must_be_absolute; got %+v", ve.Fields)
		}
	})

	t.Run("trailing slash cleaned", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Logging.Directory = "/home/dala/b4/"

		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.System.Logging.Directory != "/home/dala/b4" {
			t.Errorf("want cleaned /home/dala/b4, got %q", cfg.System.Logging.Directory)
		}
	})

	t.Run("empty stays empty (file logging disabled)", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Logging.Directory = ""

		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.System.Logging.Directory != "" {
			t.Errorf("want empty, got %q", cfg.System.Logging.Directory)
		}
	})
}

func TestValidate_RoutingMode(t *testing.T) {
	t.Run("invalid routing mode", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.Routing.Mode = "garbage"
		cfg.Sets = []*SetConfig{&set}

		ve := mustValidationErr(t, cfg.Validate())
		f := findField(ve, "sets[0].routing.mode", "invalid_routing_mode")
		if f == nil {
			t.Fatalf("missing invalid_routing_mode; got %+v", ve.Fields)
		}
		if f.Params["mode"] != "garbage" {
			t.Errorf("expected params.mode=garbage, got %v", f.Params["mode"])
		}
	})

	t.Run("upstream port out of range", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.Routing.Enabled = true
		set.Routing.Mode = RoutingModeProxy
		set.Routing.Upstream.Host = "10.0.0.1"
		set.Routing.Upstream.Port = 70000
		cfg.Sets = []*SetConfig{&set}

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "sets[0].routing.upstream.port", "out_of_range") == nil {
			t.Errorf("missing upstream.port out_of_range; got %+v", ve.Fields)
		}
	})

	for _, host := range []string{"127.0.0.1", "::"} {
		t.Run("socks5 loopback loop detected for "+host, func(t *testing.T) {
			cfg := NewConfig()
			cfg.System.Socks5.Enabled = true
			cfg.System.Socks5.Port = 1080
			set := NewSetConfig()
			set.Id = "s1"
			set.Routing.Enabled = true
			set.Routing.Mode = RoutingModeProxy
			set.Routing.Upstream.Host = host
			set.Routing.Upstream.Port = 1080
			cfg.Sets = []*SetConfig{&set}

			ve := mustValidationErr(t, cfg.Validate())
			if findField(ve, "sets[0].routing.upstream.port", "socks5_loop") == nil {
				t.Errorf("missing socks5_loop; got %+v", ve.Fields)
			}
		})
	}
}

func TestValidate_QueueFields(t *testing.T) {
	t.Run("threads < 1", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Threads = 0
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "queue.threads", "out_of_range") == nil {
			t.Errorf("missing queue.threads out_of_range; got %+v", ve.Fields)
		}
	})

	t.Run("start_num out of range", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.StartNum = 70000
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "queue.start_num", "out_of_range") == nil {
			t.Errorf("missing queue.start_num out_of_range; got %+v", ve.Fields)
		}
	})

	t.Run("mark conflicts with per-set bits", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mark = 0x100
		ve := mustValidationErr(t, cfg.Validate())
		f := findField(ve, "queue.mark", "mark_conflict")
		if f == nil {
			t.Fatalf("missing queue.mark mark_conflict; got %+v", ve.Fields)
		}
		if f.Params["mark"] != "0x100" {
			t.Errorf("expected params.mark=0x100, got %v", f.Params["mark"])
		}
	})

	t.Run("mark conflicts with the router's own proxy mark bit", func(t *testing.T) {
		for _, mark := range []uint{0x1000000, 0x1000100} {
			cfg := NewConfig()
			cfg.Queue.Mark = mark
			ve := mustValidationErr(t, cfg.Validate())
			if findField(ve, "queue.mark", "mark_conflict") == nil {
				t.Errorf("queue mark 0x%x fits inside the mark of the router's own connections to a proxy set, so the queue bypass would return them before TPROXY; got %+v", mark, ve.Fields)
			}
		}
		cfg := NewConfig()
		cfg.Queue.Mark = 0x1008000
		if err := cfg.Validate(); err != nil {
			t.Errorf("queue mark 0x1008000 has bit 15, which no proxy-set mark carries, yet it was rejected: %v", err)
		}
	})

	t.Run("invalid queue mode", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mode = "bogus"
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "queue.mode", "invalid") == nil {
			t.Errorf("missing queue.mode invalid; got %+v", ve.Fields)
		}
	})

	t.Run("tun mode follows the default route when no interface is named", func(t *testing.T) {
		for _, iface := range []string{"", "auto"} {
			cfg := NewConfig()
			cfg.Queue.Mode = "tun"
			cfg.Queue.TUN.OutInterface = iface
			if err := cfg.Validate(); err != nil {
				t.Errorf("out_interface=%q (follow-default) rejected: %v", iface, err)
			}
			if !cfg.Queue.TUN.FollowsDefaultRoute() {
				t.Errorf("out_interface=%q should follow the default route", iface)
			}
		}
	})

	t.Run("valid tun config passes", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mode = "tun"
		cfg.Queue.TUN.OutInterface = "eth0"
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid tun config rejected: %v", err)
		}
	})

	t.Run("tun mode falls back to automatic tables for reserved route tables", func(t *testing.T) {
		for _, table := range []int{-1, 253, 254, 255} {
			cfg := NewConfig()
			cfg.Queue.Mode = "tun"
			cfg.Queue.TUN.RouteTable = table
			if err := cfg.Validate(); err != nil {
				t.Errorf("route_table %d must not stop the service: %v", table, err)
			}
			if cfg.Queue.TUN.RouteTable != 0 {
				t.Errorf("route_table %d kept, want 0 (automatic)", table)
			}
		}
	})

	t.Run("tun mode accepts automatic and custom route tables", func(t *testing.T) {
		for _, table := range []int{0, 1, 97, 252, 256, 9999} {
			cfg := NewConfig()
			cfg.Queue.Mode = "tun"
			cfg.Queue.TUN.RouteTable = table
			if err := cfg.Validate(); err != nil {
				t.Errorf("route_table %d rejected: %v", table, err)
			}
			if cfg.Queue.TUN.RouteTable != table {
				t.Errorf("route_table %d changed to %d", table, cfg.Queue.TUN.RouteTable)
			}
		}
	})

	t.Run("tun mode rejects mark overlapping reserved bits", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mode = "tun"
		cfg.Queue.TUN.OutInterface = "eth0"
		cfg.Queue.Mark = engine.TunSteerMark
		ve := mustValidationErr(t, cfg.Validate())
		f := findField(ve, "queue.mark", "mark_conflict")
		if f == nil {
			t.Fatalf("missing queue.mark mark_conflict; got %+v", ve.Fields)
		}
		if !strings.Contains(f.Message, "reserved TUN mark bits") {
			t.Errorf("expected reserved-TUN-bits message, got %q", f.Message)
		}
	})

	t.Run("nfqueue mode rejects mark overlapping client mark bit", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Tables.Masquerade.Enabled = true
		cfg.Queue.Mark = engine.ClientMark
		ve := mustValidationErr(t, cfg.Validate())
		f := findField(ve, "queue.mark", "mark_conflict")
		if f == nil {
			t.Fatalf("missing queue.mark mark_conflict; got %+v", ve.Fields)
		}
		if !strings.Contains(f.Message, "client mark bit") {
			t.Errorf("expected client-mark-bit message, got %q", f.Message)
		}
	})
}

func TestValidate_RequiredSetID(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = ""
	cfg.Sets = []*SetConfig{&set}

	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[0].id", "required") == nil {
		t.Errorf("missing sets[0].id required; got %+v", ve.Fields)
	}
}

func TestValidate_DefaultsApplied(t *testing.T) {
	t.Run("routing mode defaults to interface", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.Routing.Mode = ""
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Sets[0].Routing.Mode != RoutingModeInterface {
			t.Errorf("expected default routing mode=%s, got %q", RoutingModeInterface, cfg.Sets[0].Routing.Mode)
		}
	})

	t.Run("TCP ConnBytesLimit capped to queue limit", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.TCP.ConnBytesLimit = cfg.Queue.TCPConnBytesLimit + 50
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Sets[0].TCP.ConnBytesLimit != cfg.Queue.TCPConnBytesLimit {
			t.Errorf("expected cap to %d, got %d", cfg.Queue.TCPConnBytesLimit, cfg.Sets[0].TCP.ConnBytesLimit)
		}
	})
}

func TestValidate_MSSClampBounds(t *testing.T) {
	cfg := NewConfig()
	cfg.Queue.MSSClamp.Enabled = true
	cfg.Queue.MSSClamp.Size = 5
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Queue.MSSClamp.Size != 10 {
		t.Errorf("expected MSSClamp.Size raised to 10, got %d", cfg.Queue.MSSClamp.Size)
	}

	cfg.Queue.MSSClamp.Size = 99999
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Queue.MSSClamp.Size != 1460 {
		t.Errorf("expected MSSClamp.Size capped to 1460, got %d", cfg.Queue.MSSClamp.Size)
	}
}

func TestValidate_Idempotent(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	mark1 := cfg.Queue.Mark
	flow1 := cfg.System.Checker.DiscoveryFlowMark
	mode1 := cfg.Sets[0].Routing.Mode

	if err := cfg.Validate(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if cfg.Queue.Mark != mark1 || cfg.System.Checker.DiscoveryFlowMark != flow1 || cfg.Sets[0].Routing.Mode != mode1 {
		t.Errorf("Validate is not idempotent: marks/mode changed on second call")
	}
}

func egressIPSet() SetConfig {
	set := NewSetConfig()
	set.Id = "egress-set"
	set.Name = "egress"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = RoutingModeInterface
	set.Routing.EgressInterface = "eth0"
	set.Routing.EgressIP = "192.0.2.10"
	return set
}

func gatewaySet() SetConfig {
	set := NewSetConfig()
	set.Id = "gateway-set"
	set.Name = "gateway"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = RoutingModeInterface
	set.Routing.EgressInterface = "eth0"
	set.Routing.EgressGateway = "192.0.2.1"
	return set
}

var egressNextHops = []struct {
	label string
	path  string
	code  string
	field func(*SetConfig) *string
	new   func() SetConfig
	v6    string
	bad   []string
}{
	{
		label: "egress_ip",
		path:  "sets[0].routing.egress_ip",
		code:  "invalid_egress_ip",
		field: func(s *SetConfig) *string { return &s.Routing.EgressIP },
		new:   egressIPSet,
		v6:    "2001:DB8::10",
		bad:   []string{"not-an-ip", "0.0.0.0", "127.0.0.1"},
	},
	{
		label: "egress_gateway",
		path:  "sets[0].routing.egress_gateway",
		code:  "invalid_egress_gateway",
		field: func(s *SetConfig) *string { return &s.Routing.EgressGateway },
		new:   gatewaySet,
		v6:    "2001:DB8::1",
		bad:   []string{"192.0.2.1 10.0.0.1", "0.0.0.0", "127.0.0.1", "224.0.0.1", "255.255.255.255", "::", "::1", "ff02::1"},
	},
}

var egressDropCases = []struct {
	name string
	tune func(*SetConfig)
}{
	{"proxy mode takes the connection over", func(s *SetConfig) {
		s.Routing.Mode = RoutingModeProxy
		s.Routing.Upstream.Host = "10.0.0.1"
		s.Routing.Upstream.Port = 1080
	}},
	{"block mode has no next hop", func(s *SetConfig) { s.Routing.Mode = RoutingModeBlock }},
	{"no output interface to reach it on", func(s *SetConfig) { s.Routing.EgressInterface = "" }},
}

func TestValidate_EgressNextHopRejected(t *testing.T) {
	for _, opt := range egressNextHops {
		t.Run(opt.label, func(t *testing.T) {
			for _, bad := range opt.bad {
				t.Run(bad, func(t *testing.T) {
					cfg := NewConfig()
					set := opt.new()
					*opt.field(&set) = bad
					cfg.Sets = []*SetConfig{&set}

					ve := mustValidationErr(t, cfg.Validate())
					if findField(ve, opt.path, opt.code) == nil {
						t.Errorf("missing %s; got %+v", opt.code, ve.Fields)
					}
				})
			}
		})
	}
}

func TestValidate_EgressNextHopNormalized(t *testing.T) {
	for _, opt := range egressNextHops {
		t.Run(opt.label, func(t *testing.T) {
			base := opt.new()
			canonical := *opt.field(&base)
			for _, in := range []struct{ in, want string }{
				{"  " + canonical + "  ", canonical},
				{opt.v6, strings.ToLower(opt.v6)},
			} {
				cfg := NewConfig()
				set := opt.new()
				*opt.field(&set) = in.in
				cfg.Sets = []*SetConfig{&set}

				if err := cfg.Validate(); err != nil {
					t.Fatalf("%q on an interface-mode set must validate: %v", in.in, err)
				}
				if got := *opt.field(cfg.Sets[0]); got != in.want {
					t.Errorf("%q stored as %q, want %q", in.in, got, in.want)
				}
			}
		})
	}
}

func TestValidate_EgressNextHopDropped(t *testing.T) {
	for _, opt := range egressNextHops {
		t.Run(opt.label, func(t *testing.T) {
			for _, tc := range egressDropCases {
				t.Run(tc.name, func(t *testing.T) {
					cfg := NewConfig()
					set := opt.new()
					tc.tune(&set)
					cfg.Sets = []*SetConfig{&set}

					if err := cfg.Validate(); err != nil {
						t.Fatalf("switching a set away from %s must not block the save, the stale value should just be dropped: %v", opt.label, err)
					}
					if got := *opt.field(cfg.Sets[0]); got != "" {
						t.Errorf("%s %q was kept where it cannot take effect", opt.label, got)
					}
				})
			}
		})
	}
}

func TestValidate_EgressGatewayAcceptsLinkLocal(t *testing.T) {
	cfg := NewConfig()
	set := gatewaySet()
	set.Routing.EgressGateway = "fe80::1"
	cfg.Sets = []*SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fe80::1 is an ordinary IPv6 next hop and must validate: %v", err)
	}
	if got := cfg.Sets[0].Routing.EgressGateway; got != "fe80::1" {
		t.Errorf("link-local gateway stored as %q", got)
	}
}
func TestValidate_EgressGatewayRejectsLocalAddress(t *testing.T) {
	if !egressGatewayIsLocal(net.ParseIP("127.0.0.1")) {
		t.Fatal("loopback must read as local, or the guard below never fires")
	}
	if egressGatewayIsLocal(net.ParseIP("192.0.2.1")) {
		t.Fatal("TEST-NET-1 must not read as local")
	}
	var local string
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
outer:
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP != nil && !ipNet.IP.IsLoopback() {
				local = ipNet.IP.String()
				break outer
			}
		}
	}
	if local == "" {
		t.Skip("no non-loopback address to offer as a gateway")
	}
	cfg := NewConfig()
	set := gatewaySet()
	set.Routing.EgressGateway = local
	cfg.Sets = []*SetConfig{&set}
	ve := mustValidationErr(t, cfg.Validate())
	if findField(ve, "sets[0].routing.egress_gateway", "invalid_egress_gateway") == nil {
		t.Errorf("a router address as gateway must fail validation; got %+v", ve.Fields)
	}
}
func TestValidate_SharedManualTableWithDifferentGatewaysWarns(t *testing.T) {
	mk := func(name, gw string) *SetConfig {
		set := gatewaySet()
		set.Id = name
		set.Name = name
		set.Routing.FWMark = 0x100
		set.Routing.Table = 200
		set.Routing.EgressGateway = gw
		return &set
	}
	t.Run("different gateways clash", func(t *testing.T) {
		got := findSharedTableGatewayClashes([]*SetConfig{mk("a", "192.0.2.1"), mk("b", "192.0.2.2")})
		if len(got) != 1 || got[0].table != 200 {
			t.Fatalf("expected one clash on table 200, got %+v", got)
		}
	})
	t.Run("gateway against interface default clashes", func(t *testing.T) {
		got := findSharedTableGatewayClashes([]*SetConfig{mk("a", "192.0.2.1"), mk("b", "")})
		if len(got) != 1 || got[0].table != 200 {
			t.Fatalf("a gateway set sharing a pinned table with the interface default must warn, got %+v", got)
		}
	})
	t.Run("two interface defaults share quietly", func(t *testing.T) {
		got := findSharedTableGatewayClashes([]*SetConfig{mk("a", ""), mk("b", "")})
		if len(got) != 0 {
			t.Fatalf("two empty gateways write the same default, got %+v", got)
		}
	})
	t.Run("same gateway shares quietly", func(t *testing.T) {
		got := findSharedTableGatewayClashes([]*SetConfig{mk("a", "192.0.2.1"), mk("b", "192.0.2.1")})
		if len(got) != 0 {
			t.Fatalf("one gateway per table needs no warning, got %+v", got)
		}
	})
	t.Run("automatic tables do not clash", func(t *testing.T) {
		a, b := mk("a", "192.0.2.1"), mk("b", "192.0.2.2")
		a.Routing.FWMark, a.Routing.Table = 0, 0
		if got := findSharedTableGatewayClashes([]*SetConfig{a, b}); len(got) != 0 {
			t.Fatalf("automatic tables are allocated apart, got %+v", got)
		}
	})
}
