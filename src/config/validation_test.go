package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "system.web_server.tls_cert", "tls_pair_required") == nil {
			t.Errorf("missing tls_pair_required; got %+v", ve.Fields)
		}
	})

	t.Run("missing cert file", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.WebServer.TLSCert = "/nonexistent/cert.pem"
		cfg.System.WebServer.TLSKey = "/nonexistent/key.pem"
		ve := mustValidationErr(t, cfg.Validate())
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
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "system.web_server.tls_key", "file_not_found") == nil {
			t.Errorf("missing tls_key file_not_found; got %+v", ve.Fields)
		}
	})
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

	t.Run("socks5 loopback loop detected", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Socks5.Enabled = true
		cfg.System.Socks5.Port = 1080
		set := NewSetConfig()
		set.Id = "s1"
		set.Routing.Enabled = true
		set.Routing.Mode = RoutingModeProxy
		set.Routing.Upstream.Host = "127.0.0.1"
		set.Routing.Upstream.Port = 1080
		cfg.Sets = []*SetConfig{&set}

		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "sets[0].routing.upstream.port", "socks5_loop") == nil {
			t.Errorf("missing socks5_loop; got %+v", ve.Fields)
		}
	})
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

	t.Run("invalid queue mode", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mode = "bogus"
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "queue.mode", "invalid") == nil {
			t.Errorf("missing queue.mode invalid; got %+v", ve.Fields)
		}
	})

	t.Run("tun mode follows default route when out_interface is empty", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mode = "tun"
		cfg.Queue.TUN.OutInterface = ""
		if err := cfg.Validate(); err != nil {
			t.Errorf("empty out_interface (follow-default) rejected: %v", err)
		}
		if !cfg.Queue.TUN.FollowsDefaultRoute() {
			t.Errorf("empty out_interface should follow the default route")
		}
	})

	t.Run("tun mode follows default route when out_interface is auto", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mode = "tun"
		cfg.Queue.TUN.OutInterface = "auto"
		if err := cfg.Validate(); err != nil {
			t.Errorf("out_interface=auto (follow-default) rejected: %v", err)
		}
		if !cfg.Queue.TUN.FollowsDefaultRoute() {
			t.Errorf("out_interface=auto should follow the default route")
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
		cfg.System.Tables.Masquerade = true
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

	t.Run("MSS clamp clamped to bounds", func(t *testing.T) {
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
	})
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
