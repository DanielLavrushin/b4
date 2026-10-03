package config

import (
	"bytes"
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

var redactedPaths = map[string]string{
	"system.web_server.username":        "web login",
	"system.web_server.password":        "bcrypt hash of the web login",
	"system.web_server.mcp.token":       "MCP bearer token",
	"system.socks5.username":            "SOCKS5 login",
	"system.socks5.password":            "SOCKS5 login",
	"system.mtproto.secrets[].name":     "free-text label, usually a person's name",
	"system.mtproto.secrets[].secret":   "MTProto proxy secret",
	"system.mtproto.dc_relay":           "the user's own relay host",
	"system.mtproto.ws_custom_domain":   "the user's own relay domain",
	"system.mtproto.cfworker_domain":    "the user's own Cloudflare Worker",
	"system.mtproto.web_proxy.hostname": "the user's own server",
	"system.mtproto.cfproxy_url":        "URL that can carry credentials",
	"system.mtproto.dc_fallback_url":    "URL that can carry credentials",
	"system.api.ipinfo_token":           "ipinfo.io token",
	"system.ai.api_key_ref":             "AI key reference",
	"system.ai.endpoint":                "URL that can carry credentials",
	"system.geo.sitedat_url":            "URL that can carry credentials",
	"system.geo.ipdat_url":              "URL that can carry credentials",
	"system.update.mirrors[]":           "the user's own relays",
	"system.hub.urls[]":                 "URL that can carry credentials",
	"sets[].routing.upstream.username":  "upstream proxy login",
	"sets[].routing.upstream.password":  "upstream proxy login",
	"sets[].dns.doh_url":                "personal resolver id in the path",
	"sets[].discovery.urls[]":           "URL that can carry credentials",
}

var notSecretPaths = map[string]string{
	"system.web_server.tls_key":         "file path, not key material",
	"system.mtproto.web_proxy.tls_key":  "file path, not key material",
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
	path = strings.TrimSuffix(path, "[]")
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}

func TestEverySecretLookingFieldIsClassified(t *testing.T) {
	_, leaves := canaryConfig(t)
	known := map[string]bool{}
	var missing []string
	for _, leaf := range leaves {
		known[leaf.path] = true
		if !secretLookingName.MatchString(leafName(leaf.path)) {
			continue
		}
		_, redacted := redactedPaths[leaf.path]
		_, plain := notSecretPaths[leaf.path]
		if !redacted && !plain {
			missing = append(missing, leaf.path)
		}
	}
	sort.Strings(missing)
	for _, path := range missing {
		t.Errorf("%s looks like it may hold a secret: add it to redactedPaths and RedactSecrets, or to notSecretPaths with the reason", path)
	}
	for _, list := range []map[string]string{redactedPaths, notSecretPaths} {
		for path := range list {
			if !known[path] {
				t.Errorf("%s is classified but is not a string field of Config", path)
			}
		}
	}
	for path := range redactedPaths {
		if _, ok := notSecretPaths[path]; ok {
			t.Errorf("%s is in both redactedPaths and notSecretPaths", path)
		}
	}
}

func TestRedactSecretsRemovesEveryRedactedValue(t *testing.T) {
	cfg, leaves := canaryConfig(t)
	cfg.RedactSecrets()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(raw)
	for _, leaf := range leaves {
		if _, ok := redactedPaths[leaf.path]; !ok {
			continue
		}
		if strings.Contains(out, `"`+leaf.canary+`"`) || strings.Contains(out, leaf.canary) {
			t.Errorf("%s still carries its value after RedactSecrets", leaf.path)
		}
	}
}

func TestRedactSecretsMasksOnlySetValues(t *testing.T) {
	cfg := NewConfig()
	cfg.RedactSecrets()
	ws := cfg.System.WebServer
	if ws.Username != "" || ws.Password != "" || ws.PasswordSet {
		t.Errorf("empty web credentials must stay empty, got %q %q %v", ws.Username, ws.Password, ws.PasswordSet)
	}
	if cfg.System.Socks5.Username != "" || cfg.System.Socks5.Password != "" {
		t.Error("empty SOCKS5 credentials must stay empty")
	}
	if cfg.System.API.IPInfoToken != "" {
		t.Error("an empty token must stay empty")
	}
}

func TestRedactSecretsLeavesNoUsableCredential(t *testing.T) {
	cfg := NewConfig()
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "$2a$12$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz01234"
	cfg.System.WebServer.MCP.Token = "0123456789abcdef"
	cfg.RedactSecrets()
	ws := cfg.System.WebServer
	if ws.Password != "" || !ws.PasswordSet {
		t.Errorf("the web password must be emptied and flagged as set, got %q %v", ws.Password, ws.PasswordSet)
	}
	if ws.Username != RedactedMarker {
		t.Errorf("web username = %q, want the marker", ws.Username)
	}
	if ws.MCP.Token != "" {
		t.Errorf("the MCP token must be emptied so a restored copy refuses MCP clients, got %q", ws.MCP.Token)
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
		{"https://dns.nextdns.io/abc123", false, "https://dns.nextdns.io/[redacted]"},
		{"https://d.adguard-dns.com/dns-query/abc123", false, "https://d.adguard-dns.com/[redacted]"},
		{"https://dns.google", false, "https://dns.google"},
	}
	for _, tc := range cases {
		if got := redactURL(tc.in, tc.keepPath); got != tc.want {
			t.Errorf("redactURL(%q, %v) = %q, want %q", tc.in, tc.keepPath, got, tc.want)
		}
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

func TestFileBytesMatchesTheSavedFile(t *testing.T) {
	cfg := NewConfig()
	cfg.System.Socks5.Enabled = true
	cfg.System.Socks5.Password = "socks-pw"
	set := NewSetConfig()
	set.Name = "example"
	cfg.Sets = []*SetConfig{&set}

	path := filepath.Join(t.TempDir(), "b4.json")
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	data, err := cfg.FileBytes()
	if err != nil {
		t.Fatalf("FileBytes: %v", err)
	}
	if !bytes.Equal(saved, data) {
		t.Errorf("FileBytes differs from the file SaveToFile writes:\n%s\n---\n%s", data, saved)
	}
}
