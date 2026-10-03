package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var secretLookingName = regexp.MustCompile(`(?i)pass|secret|token|key|user|auth|cred|url|endpoint|domain|host|relay|mirror|cookie|session`)

var credentialPaths = map[string]string{
	"system.web_server.username":       "web login",
	"system.web_server.password":       "bcrypt hash of the web login",
	"system.web_server.mcp.token":      "MCP bearer token",
	"system.socks5.username":           "SOCKS5 login",
	"system.socks5.password":           "SOCKS5 login",
	"system.mtproto.secrets[].name":    "free-text label, usually a person's name",
	"system.mtproto.secrets[].secret":  "MTProto proxy secret",
	"system.api.ipinfo_token":          "ipinfo.io token",
	"system.ai.api_key_ref":            "AI key reference",
	"sets[].routing.upstream.username": "upstream proxy login",
	"sets[].routing.upstream.password": "upstream proxy login",
}

var sharingPaths = map[string]string{
	"system.mtproto.dc_relay":           "the user's own relay host",
	"system.mtproto.ws_custom_domain":   "the user's own relay domain",
	"system.mtproto.cfworker_domain":    "the user's own Cloudflare Worker",
	"system.mtproto.web_proxy.hostname": "the user's own server",
	"system.mtproto.cfproxy_url":        "a custom source is the user's own host",
	"system.mtproto.dc_fallback_url":    "a custom source is the user's own host",
	"system.ai.endpoint":                "URL that can carry credentials",
	"system.geo.sitedat_url":            "URL that can carry credentials",
	"system.geo.ipdat_url":              "URL that can carry credentials",
	"system.update.mirrors[]":           "the user's own relays",
	"system.hub.urls[]":                 "URL that can carry credentials",
	"sets[].dns.doh_url":                "personal resolver id in the path or the host",
	"sets[].discovery.urls[]":           "URL that can carry credentials",
}

var notSecretPaths = map[string]string{
	"system.web_server.tls_key":         "file path, not key material; the relay hostname inside it is masked",
	"system.mtproto.web_proxy.tls_key":  "file path, not key material; the relay hostname inside it is masked",
	"system.mtproto.ws_endpoint_host":   "override for the public Telegram WebSocket edge",
	"system.hub.public_key":             "public ed25519 key that pins the hub",
	"system.checker.reference_domain":   "public domain Discovery checks against",
	"system.checker.watchdog.domains[]": "domains the watchdog checks, needed to debug it",
	"sets[].targets.sni_domains[]":      "target domains, needed to debug a set",
	"sets[].faking.payload_domain":      "public domain written into the fake ClientHello",
	"sets[].routing.upstream.host":      "needed to debug routing; the log trace and MCP keep it too",
}

type leafPath struct {
	path   string
	canary string
}

func jsonFieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return "", false
	}
	if name == "" {
		return f.Name, true
	}
	return name, true
}

func joinPath(base, name string) string {
	if base == "" {
		return name
	}
	return base + "." + name
}

func fillCanaries(v reflect.Value, path string, depth int, leaves *[]leafPath) {
	if depth > 12 {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillCanaries(v.Elem(), path, depth+1, leaves)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name, ok := jsonFieldName(f)
			if !ok {
				continue
			}
			if f.Anonymous && f.Tag.Get("json") == "" {
				fillCanaries(v.Field(i), path, depth+1, leaves)
				continue
			}
			fillCanaries(v.Field(i), joinPath(path, name), depth+1, leaves)
		}
	case reflect.Map:
		*leaves = append(*leaves, leafPath{path: path + "{}"})
	case reflect.Slice:
		elem := v.Type().Elem()
		if elem.Kind() == reflect.String {
			canary := fmt.Sprintf("canary%04d", len(*leaves)+1)
			s := reflect.MakeSlice(v.Type(), 1, 1)
			s.Index(0).SetString(canary)
			v.Set(s)
			*leaves = append(*leaves, leafPath{path + "[]", canary})
			return
		}
		base := elem
		if base.Kind() == reflect.Pointer {
			base = base.Elem()
		}
		if base.Kind() != reflect.Struct {
			return
		}
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillCanaries(s.Index(0), path+"[]", depth+1, leaves)
		v.Set(s)
	case reflect.String:
		canary := fmt.Sprintf("canary%04d", len(*leaves)+1)
		v.SetString(canary)
		*leaves = append(*leaves, leafPath{path, canary})
	}
}

func canaryConfig(t *testing.T) (*Config, []leafPath) {
	t.Helper()
	var cfg Config
	var leaves []leafPath
	fillCanaries(reflect.ValueOf(&cfg).Elem(), "", 0, &leaves)
	if len(leaves) == 0 {
		t.Fatal("the walker found no string fields")
	}
	return &cfg, leaves
}

func leafName(path string) string {
	path = strings.TrimSuffix(strings.TrimSuffix(path, "[]"), "{}")
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

func TestEverySecretLookingFieldIsClassified(t *testing.T) {
	_, leaves := canaryConfig(t)
	lists := []map[string]string{credentialPaths, sharingPaths, notSecretPaths}
	known := map[string]bool{}
	var missing []string
	for _, leaf := range leaves {
		known[leaf.path] = true
		if !secretLookingName.MatchString(leafName(leaf.path)) {
			continue
		}
		classified := false
		for _, list := range lists {
			if _, ok := list[leaf.path]; ok {
				classified = true
			}
		}
		if !classified {
			missing = append(missing, leaf.path)
		}
	}
	sort.Strings(missing)
	for _, path := range missing {
		t.Errorf("%s looks like it may hold a secret: redact it and add it to credentialPaths or sharingPaths, or add it to notSecretPaths with the reason", path)
	}
	seen := map[string]int{}
	for _, list := range lists {
		for path := range list {
			seen[path]++
			if !known[path] {
				t.Errorf("%s is classified but is not a string field of Config", path)
			}
		}
	}
	for path, n := range seen {
		if n > 1 {
			t.Errorf("%s is classified more than once", path)
		}
	}
}

func canaryOutput(t *testing.T, cfg *Config) string {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func TestRedactCredentialsKeepsWhatMCPMayNeed(t *testing.T) {
	cfg, leaves := canaryConfig(t)
	cfg.RedactCredentials()
	out := canaryOutput(t, cfg)
	for _, leaf := range leaves {
		if leaf.canary == "" {
			continue
		}
		_, credential := credentialPaths[leaf.path]
		_, sharing := sharingPaths[leaf.path]
		present := strings.Contains(out, leaf.canary)
		if credential && present {
			t.Errorf("%s still carries its value after RedactCredentials", leaf.path)
		}
		if sharing && !present {
			t.Errorf("%s was masked by RedactCredentials, but MCP reads and writes it in clear", leaf.path)
		}
	}
}

func TestRedactForSharingRemovesEveryClassifiedValue(t *testing.T) {
	cfg, leaves := canaryConfig(t)
	cfg.RedactForSharing()
	out := canaryOutput(t, cfg)
	for _, leaf := range leaves {
		if leaf.canary == "" {
			continue
		}
		_, credential := credentialPaths[leaf.path]
		_, sharing := sharingPaths[leaf.path]
		if (credential || sharing) && strings.Contains(out, leaf.canary) {
			t.Errorf("%s still carries its value after RedactForSharing", leaf.path)
		}
	}
}

func TestRedactMasksOnlySetValues(t *testing.T) {
	cfg := NewConfig()
	cfg.RedactForSharing()
	ws := cfg.System.WebServer
	if ws.Username != "" || ws.Password != "" || ws.PasswordSet || ws.MCP.Token != "" {
		t.Errorf("empty web credentials must stay empty, got %q %q %v %q", ws.Username, ws.Password, ws.PasswordSet, ws.MCP.Token)
	}
	if cfg.System.Socks5.Username != "" || cfg.System.Socks5.Password != "" || cfg.System.API.IPInfoToken != "" {
		t.Error("empty credentials must stay empty")
	}
	if cfg.System.MTProto.CFProxyURL != TGCFProxyURL || cfg.System.MTProto.DCFallbackURL != TGDCFallbackURL {
		t.Error("the public default sources must stay visible")
	}
	if paths := cfg.RedactedValuePaths(); len(paths) != 0 {
		t.Errorf("a default config must not carry placeholders, got %v", paths)
	}
}

func TestRedactCredentialsMarksSetCredentials(t *testing.T) {
	cfg := NewConfig()
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "$2a$12$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234"
	cfg.System.WebServer.MCP.Token = "0123456789abcdef"
	cfg.RedactCredentials()
	ws := cfg.System.WebServer
	if ws.Password != "" || !ws.PasswordSet {
		t.Errorf("the web password must be emptied and flagged as set, got %q %v", ws.Password, ws.PasswordSet)
	}
	if ws.Username != RedactedMarker || ws.MCP.Token != RedactedMarker {
		t.Errorf("web username and MCP token must carry the marker, got %q %q", ws.Username, ws.MCP.Token)
	}
}

func TestRedactForSharingMasksCustomSourcesAndRelayHostInTLSPaths(t *testing.T) {
	cfg := NewConfig()
	cfg.System.MTProto.CFProxyURL = "https://my.worker.example/list.txt"
	cfg.System.MTProto.WebProxy.Hostname = "relay.example.com"
	cfg.System.MTProto.WebProxy.TLSCert = "/etc/letsencrypt/live/relay.example.com/fullchain.pem"
	cfg.System.WebServer.TLSKey = "/etc/letsencrypt/live/relay.example.com/privkey.pem"
	cfg.RedactForSharing()
	out := canaryOutput(t, &cfg)
	if strings.Contains(out, "relay.example.com") || strings.Contains(out, "my.worker.example") {
		t.Errorf("the relay host or a custom source survived: %s", out)
	}
	if !strings.HasSuffix(cfg.System.MTProto.WebProxy.TLSCert, "/fullchain.pem") {
		t.Errorf("the certificate file name should stay, got %q", cfg.System.MTProto.WebProxy.TLSCert)
	}
}

func TestRedactURL(t *testing.T) {
	cases := []struct {
		in       string
		keepPath bool
		want     string
	}{
		{"", true, ""},
		{"https://github.com/owner/repo/releases/latest/download/geosite.dat", true, "https://github.com/owner/repo/releases/latest/download/geosite.dat"},
		{"https://user:pw@example.com:8443/path/file.dat?token=abc&x=1#part", true, "https://[redacted]@example.com:8443/path/file.dat?token=[redacted]&x=[redacted]#[redacted]"},
		{"https://example.com/file?abc123", true, "https://example.com/file?[redacted]"},
		{"not a url", true, RedactedMarker},
		{"example.com/path", true, RedactedMarker},
		{"https://cloudflare-dns.com/dns-query", false, "https://cloudflare-dns.com/dns-query"},
		{"https://dns.google", false, "https://dns.google"},
		{"https://1.1.1.1/dns-query", false, "https://1.1.1.1/dns-query"},
		{"https://[2606:4700::1111]/dns-query", false, "https://[2606:4700::1111]/dns-query"},
		{"https://dns.nextdns.io/abc123", false, "https://[redacted].nextdns.io/[redacted]"},
		{"https://d.adguard-dns.com/dns-query/abc123", false, "https://[redacted].adguard-dns.com/[redacted]"},
		{"https://a1b2c3d4e5.cloudflare-gateway.com/dns-query", false, "https://[redacted].cloudflare-gateway.com/dns-query"},
		{"https://a1b2c3d4e5.cloudflare-gateway.com:8443/dns-query", false, "https://[redacted].cloudflare-gateway.com:8443/dns-query"},
	}
	for _, tc := range cases {
		if got := redactURL(tc.in, tc.keepPath); got != tc.want {
			t.Errorf("redactURL(%q, %v) = %q, want %q", tc.in, tc.keepPath, got, tc.want)
		}
	}
}

func TestRedactedValuePaths(t *testing.T) {
	cfg := NewConfig()
	if paths := cfg.RedactedValuePaths(); len(paths) != 0 {
		t.Fatalf("clean config reported %v", paths)
	}
	cfg.System.Socks5.Password = RedactedMarker
	set := NewSetConfig()
	set.DNS.DoHURL = "https://[redacted].nextdns.io/[redacted]"
	cfg.Sets = []*SetConfig{&set}
	got := cfg.RedactedValuePaths()
	want := []string{"sets[0].dns.doh_url", "system.socks5.password"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RedactedValuePaths = %v, want %v", got, want)
	}
}

func TestLoadWithMigrationRefusesASafeCopy(t *testing.T) {
	cfg := NewConfig()
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "$2a$12$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234"
	cfg.System.WebServer.MCP.Enabled = true
	cfg.System.WebServer.MCP.Token = "0123456789abcdef"
	cfg.System.Socks5.Username = "user"
	cfg.System.Socks5.Password = "pass"
	set := NewSetConfig()
	set.Name = "example"
	cfg.Sets = []*SetConfig{&set}

	safe, err := cfg.RedactedCopy()
	if err != nil {
		t.Fatalf("RedactedCopy: %v", err)
	}
	data, err := safe.FileBytes()
	if err != nil {
		t.Fatalf("FileBytes: %v", err)
	}
	path := filepath.Join(t.TempDir(), "b4.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var loaded Config
	_, err = loaded.LoadWithMigration(path)
	if err == nil || !strings.Contains(err.Error(), "safe copy") {
		t.Fatalf("loading a safe copy must fail with an explanation, got %v", err)
	}
	if loaded.ConfigPath != path {
		t.Errorf("ConfigPath = %q, want %q", loaded.ConfigPath, path)
	}
	if loaded.System.WebServer.Username != "" || loaded.System.WebServer.MCP.Enabled || loaded.System.Socks5.Username != "" || len(loaded.Sets) != 0 {
		t.Error("a refused safe copy must not leave its values in the config b4 starts with")
	}
}

func TestRedactedCopyLeavesTheOriginalAlone(t *testing.T) {
	cfg := NewConfig()
	cfg.System.Socks5.Password = "socks-pw"
	cfg.System.WebServer.MCP.Token = "mcp-token"
	set := NewSetConfig()
	set.Routing.Upstream.Password = "upstream-pw"
	cfg.Sets = []*SetConfig{&set}

	redacted, err := cfg.RedactedCopy()
	if err != nil {
		t.Fatalf("RedactedCopy: %v", err)
	}
	if redacted.System.Socks5.Password != RedactedMarker || redacted.Sets[0].Routing.Upstream.Password != RedactedMarker {
		t.Error("the copy was not redacted")
	}
	if cfg.System.Socks5.Password != "socks-pw" || cfg.System.WebServer.MCP.Token != "mcp-token" || cfg.Sets[0].Routing.Upstream.Password != "upstream-pw" {
		t.Error("RedactedCopy changed the live config")
	}
}
