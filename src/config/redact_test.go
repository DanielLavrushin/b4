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

	"github.com/daniellavrushin/b4/geodat"
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
	"system.web_server.mcp.allowed_origins[]": "browser origins, often the router's own name",
	"system.mtproto.dc_relay":                 "the user's own relay host",
	"system.mtproto.ws_custom_domain":         "the user's own relay domain",
	"system.mtproto.cfworker_domain":          "the user's own Cloudflare Worker",
	"system.mtproto.web_proxy.hostname":       "the user's own server",
	"system.mtproto.cfproxy_url":              "a custom source is the user's own host",
	"system.mtproto.dc_fallback_url":          "a custom source is the user's own host",
	"system.ai.endpoint":                      "URL that can carry credentials",
	"system.geo.sitedat_url":                  "URL that can carry credentials",
	"system.geo.ipdat_url":                    "URL that can carry credentials",
	"system.update.mirrors[]":                 "the user's own relays",
	"system.hub.urls[]":                       "URL that can carry credentials",
	"sets[].dns.doh_url":                      "personal resolver id in the path or the host",
	"sets[].discovery.urls[]":                 "URL that can carry credentials",
}

var notSecretPaths = map[string]string{
	"system.web_server.tls_key":         "file path, not key material; host names inside it are masked",
	"system.mtproto.web_proxy.tls_key":  "file path, not key material; host names inside it are masked",
	"system.mtproto.ws_endpoint_host":   "override for the public Telegram WebSocket edge",
	"system.hub.public_key":             "public ed25519 key that pins the hub",
	"system.checker.reference_domain":   "public domain Discovery checks against",
	"system.checker.watchdog.domains[]": "bare domains stay to debug the watchdog; URL entries lose credentials, query values and fragments",
	"sets[].targets.sni_domains[]":      "target domains, needed to debug a set",
	"sets[].faking.payload_domain":      "public domain written into the fake ClientHello",
	"sets[].routing.upstream.host":      "needed to debug routing; the log trace and MCP keep it too",
}

var placeholderOnlyPaths = map[string]string{
	"system.web_server.tls_cert":        "file path; host names inside it are masked",
	"system.web_server.tls_key":         "file path; host names inside it are masked",
	"system.mtproto.web_proxy.tls_cert": "file path; host names inside it are masked",
	"system.mtproto.web_proxy.tls_key":  "file path; host names inside it are masked",
	"system.mtproto.secret":             "legacy single secret that the v50 migration moves into secrets",
	"system.checker.watchdog.domains[]": "URL entries are masked like Discovery URLs",
}

var indexInPath = regexp.MustCompile(`\[\d+\]`)

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

func TestPlaceholderPathsMatchTheRedactedFields(t *testing.T) {
	redacted := []map[string]string{credentialPaths, sharingPaths}
	for _, list := range []map[string]string{credentialPaths, sharingPaths, placeholderOnlyPaths} {
		for path := range list {
			if !placeholderPaths[path] {
				t.Errorf("%s is missing from placeholderPaths, so b4 would load a safe copy that holds it", path)
			}
		}
	}
	for path := range placeholderPaths {
		_, credential := credentialPaths[path]
		_, sharing := sharingPaths[path]
		_, extra := placeholderOnlyPaths[path]
		if !credential && !sharing && !extra {
			t.Errorf("%s is in placeholderPaths, but no list says why", path)
		}
	}

	cfg, _ := canaryConfig(t)
	cfg.RedactForSharing()
	reported := map[string]bool{}
	for _, path := range cfg.RedactedValuePaths() {
		reported[indexInPath.ReplaceAllString(path, "[]")] = true
	}
	for _, list := range redacted {
		for path := range list {
			if path != "system.web_server.password" && !reported[path] {
				t.Errorf("%s holds the placeholder in a safe copy, but RedactedValuePaths does not report it", path)
			}
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

func TestMaskFilePath(t *testing.T) {
	cases := []struct {
		path string
		host string
		want string
	}{
		{"", "", ""},
		{"/jffs/ssl/cert.pem", "", "/jffs/ssl/cert.pem"},
		{"../certs/key.pem", "", "../certs/key.pem"},
		{"/etc/letsencrypt/live/myrouter.duckdns.org/fullchain.pem", "", "/etc/letsencrypt/live/[redacted]/fullchain.pem"},
		{"/etc/ssl/acme/myrouter.duckdns.org.fullchain.crt", "", "/etc/ssl/acme/[redacted].crt"},
		{"/jffs/.le/myrouter.asuscomm.com_ecc/fullchain.cer", "", "/jffs/.le/[redacted]/fullchain.cer"},
		{"/root/.acme.sh/certs/.hidden.pem", "", "/root/[redacted]/certs/.hidden.pem"},
		{"/opt/relay.example.com-cert.pem", "relay.example.com", "/opt/[redacted]-cert.pem"},
	}
	for _, tc := range cases {
		if got := maskFilePath(tc.path, tc.host); got != tc.want {
			t.Errorf("maskFilePath(%q, %q) = %q, want %q", tc.path, tc.host, got, tc.want)
		}
	}
}

func TestRedactForSharingMasksTheRouterName(t *testing.T) {
	cfg := NewConfig()
	ws := &cfg.System.WebServer
	ws.TLSCert = "/etc/letsencrypt/live/myrouter.duckdns.org/fullchain.pem"
	ws.TLSKey = "/etc/ssl/acme/myrouter.duckdns.org.key"
	ws.MCP.AllowedOrigins = []string{"https://myrouter.duckdns.org:7000", "https://myrouter.duckdns.org", "http://localhost:5173", "http://192.168.1.1:7000", "http://[fd00::1]:7000", "*", "not an origin", "https://admin:secret@192.168.1.1:7000", "http://localhost:5173/", "http://localhost:5173/app?token=abc#x"}
	cfg.RedactForSharing()
	if ws.TLSCert != "/etc/letsencrypt/live/[redacted]/fullchain.pem" || ws.TLSKey != "/etc/ssl/acme/[redacted].key" {
		t.Errorf("the router name survived in a certificate path: %q %q", ws.TLSCert, ws.TLSKey)
	}
	want := []string{"https://[redacted]:7000", "https://[redacted]", "http://localhost:5173", "http://192.168.1.1:7000", "http://[fd00::1]:7000", "*", RedactedMarker, "https://[redacted]@192.168.1.1:7000", "http://localhost:5173/", "http://localhost:5173/[redacted]?token=[redacted]#[redacted]"}
	if !reflect.DeepEqual(ws.MCP.AllowedOrigins, want) {
		t.Errorf("allowed origins = %v, want %v", ws.MCP.AllowedOrigins, want)
	}
}

func TestRedactForSharingMasksCredentialsInWatchdogEntries(t *testing.T) {
	cfg := NewConfig()
	cfg.System.Checker.Watchdog.Domains = []string{
		"youtube.com",
		"https://www.youtube.com/watch",
		"https://admin:hunter2@nas.example.com/health?token=0123abcd",
		"admin:hunter2@nas.example.com",
		"nas.example.com/health?token=0123abcd#part",
		"hc.example.com/ping/0f1e2d3c4b5a",
	}
	cfg.RedactForSharing()
	want := []string{
		"youtube.com",
		"https://www.youtube.com/[redacted]",
		"https://[redacted]@nas.example.com/[redacted]?token=[redacted]",
		"[redacted]@nas.example.com",
		"nas.example.com/[redacted]?token=[redacted]#[redacted]",
		"hc.example.com/[redacted]",
	}
	if got := cfg.System.Checker.Watchdog.Domains; !reflect.DeepEqual(got, want) {
		t.Errorf("watchdog entries = %v, want %v", got, want)
	}
}

func TestMaskURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com/"},
		{"https://github.com/owner/repo/releases/latest/download/geosite.dat", "https://github.com/[redacted]"},
		{"https://example.com/private-token/geosite.dat", "https://example.com/[redacted]"},
		{"https://user:pw@example.com:8443/path/file.dat?token=abc&x=1#part", "https://[redacted]@example.com:8443/[redacted]?token=[redacted]&x=[redacted]#[redacted]"},
		{"https://example.com/file?abc123", "https://example.com/[redacted]?[redacted]"},
		{"not a url", RedactedMarker},
		{"example.com/path", RedactedMarker},
	}
	for _, tc := range cases {
		if got := maskURL(tc.in); got != tc.want {
			t.Errorf("maskURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskDoHURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://cloudflare-dns.com/dns-query", "https://cloudflare-dns.com/dns-query"},
		{"https://dns.google", "https://dns.google"},
		{"https://1.1.1.1/dns-query", "https://1.1.1.1/dns-query"},
		{"https://[2606:4700::1111]/dns-query", "https://[2606:4700::1111]/dns-query"},
		{"https://dns.nextdns.io/abc123", "https://dns.nextdns.io/[redacted]"},
		{"https://family.adguard-dns.com/dns-query", "https://family.adguard-dns.com/dns-query"},
		{"https://dns.quad9.net/dns-query", "https://dns.quad9.net/dns-query"},
		{"https://d.adguard-dns.com/dns-query/abc123", "https://[redacted].adguard-dns.com/[redacted]"},
		{"https://a1b2c3d4e5.cloudflare-gateway.com/dns-query", "https://[redacted].cloudflare-gateway.com/dns-query"},
		{"https://a1b2c3d4e5.cloudflare-gateway.com:8443/dns-query", "https://[redacted].cloudflare-gateway.com:8443/dns-query"},
		{"https://abc123.dns.nextdns.io/dns-query", "https://[redacted].nextdns.io/dns-query"},
		{"https://ivanov.ru/dns-query", "https://[redacted]/dns-query"},
		{"https://IVANOV.RU/dns-query", "https://[redacted]/dns-query"},
		{"https://dns.ivanov.ru/dns-query", "https://[redacted]/dns-query"},
		{"https://dns.ivanov.ru:8443/dns-query", "https://[redacted]:8443/dns-query"},
	}
	for _, tc := range cases {
		if got := maskDoHURL(tc.in); got != tc.want {
			t.Errorf("maskDoHURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskGeoURLKeepsTheBuiltInSources(t *testing.T) {
	sources, err := geodat.Sources()
	if err != nil || len(sources) == 0 {
		t.Fatalf("no built-in geodata sources: %v", err)
	}
	for _, s := range sources {
		for _, u := range []string{s.GeositeURL, s.GeoipURL} {
			if u != "" && maskGeoURL(u) != u {
				t.Errorf("the built-in source %s was masked: %q", s.Name, maskGeoURL(u))
			}
		}
	}
	custom := "https://files.example.com/9f8e7d6c5b4a/geosite.dat?sig=abc"
	if got, want := maskGeoURL(custom), "https://files.example.com/[redacted]?sig=[redacted]"; got != want {
		t.Errorf("maskGeoURL(%q) = %q, want %q", custom, got, want)
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
	cfg.System.WebServer.Port = 8443
	cfg.System.WebServer.BindAddress = "192.168.1.1"
	cfg.System.WebServer.MCP.Enabled = true
	cfg.System.WebServer.MCP.Token = "0123456789abcdef"
	cfg.System.Socks5.Username = "user"
	cfg.System.Socks5.Password = "pass"
	set := NewSetConfig()
	set.Name = "example"
	cfg.Sets = []*SetConfig{&set}

	path := writeConfigFile(t, safeCopyBytes(t, &cfg))

	var loaded Config
	_, err := loaded.LoadWithMigration(path)
	if err == nil || !strings.Contains(err.Error(), "safe copy") {
		t.Fatalf("loading a safe copy must fail with an explanation, got %v", err)
	}
	if loaded.ConfigPath != path {
		t.Errorf("ConfigPath = %q, want %q", loaded.ConfigPath, path)
	}
	if loaded.System.WebServer.Username != "" || loaded.System.WebServer.MCP.Enabled || loaded.System.Socks5.Username != "" || len(loaded.Sets) != 0 {
		t.Error("a refused safe copy must not leave its values in the config b4 starts with")
	}
	if loaded.System.WebServer.Port != 8443 || loaded.System.WebServer.BindAddress != "192.168.1.1" {
		t.Errorf("the web UI must stay where the file put it, got %s:%d", loaded.System.WebServer.BindAddress, loaded.System.WebServer.Port)
	}
}

func safeCopyBytes(t *testing.T, cfg *Config) []byte {
	t.Helper()
	safe, err := cfg.RedactedCopy()
	if err != nil {
		t.Fatalf("RedactedCopy: %v", err)
	}
	data, err := safe.FileBytes()
	if err != nil {
		t.Fatalf("FileBytes: %v", err)
	}
	return data
}

func writeConfigFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "b4.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadWithMigrationRefusesASafeCopyThatDoesNotDecode(t *testing.T) {
	cfg := NewConfig()
	cfg.System.Socks5.Username = "user"
	cfg.System.Socks5.Password = "pass"
	var doc map[string]any
	if err := json.Unmarshal(safeCopyBytes(t, &cfg), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	system, _ := doc["system"].(map[string]any)
	socks, _ := system["socks5"].(map[string]any)
	if socks == nil {
		t.Fatalf("the safe copy has no system.socks5 object: %v", doc)
	}
	socks["port"] = "1080"
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(data, &Config{}); err == nil {
		t.Fatal("the edited copy decodes, so this test no longer covers a type error")
	}

	var loaded Config
	_, err = loaded.LoadWithMigration(writeConfigFile(t, data))
	if err == nil || !strings.Contains(err.Error(), "system.socks5.password") {
		t.Fatalf("a safe copy must be refused before it is decoded, got %v", err)
	}
	if paths := loaded.RedactedValuePaths(); len(paths) != 0 {
		t.Errorf("the refused copy left placeholders behind: %v", paths)
	}
}

func TestLoadWithMigrationRefusesALegacySecretPlaceholder(t *testing.T) {
	path := writeConfigFile(t, []byte(`{"version": 49, "system": {"mtproto": {"enabled": true, "secret": "[redacted]"}}}`))
	var loaded Config
	_, err := loaded.LoadWithMigration(path)
	if err == nil || !strings.Contains(err.Error(), "system.mtproto.secret") {
		t.Fatalf("the legacy secret must be guarded too, got %v", err)
	}
	if loaded.System.MTProto.Enabled || len(loaded.System.MTProto.Secrets) != 0 {
		t.Error("the refused file was migrated into the running config")
	}
	if _, err := os.Stat(path + ".v49.bak"); !os.IsNotExist(err) {
		t.Errorf("a refused file must not be backed up for migration, stat: %v", err)
	}
}

func TestLoadWithMigrationLoadsThePlaceholderOutsideRedactedFields(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Name = "Work " + RedactedMarker
	cfg.Sets = []*SetConfig{&set}
	if paths := cfg.RedactedValuePaths(); len(paths) != 0 {
		t.Fatalf("a set name is not a redacted field, got %v", paths)
	}
	data, err := cfg.FileBytes()
	if err != nil {
		t.Fatalf("FileBytes: %v", err)
	}

	var loaded Config
	if _, err := loaded.LoadWithMigration(writeConfigFile(t, data)); err != nil {
		t.Fatalf("LoadWithMigration: %v", err)
	}
	if len(loaded.Sets) != 1 || loaded.Sets[0].Name != set.Name {
		t.Errorf("the set did not load as written, got %d sets", len(loaded.Sets))
	}
}

func TestLoadWithMigrationRefusesASafeCopyWithMaskedTLSPaths(t *testing.T) {
	cfg := NewConfig()
	cfg.System.WebServer.TLSCert = "/jffs/.le/myrouter.asuscomm.com_ecc/fullchain.cer"
	cfg.System.WebServer.TLSKey = "/jffs/.le/myrouter.asuscomm.com_ecc/domain.key"
	cfg.System.MTProto.WebProxy.TLSCert = "/etc/letsencrypt/live/myrouter.duckdns.org/fullchain.pem"
	cfg.System.MTProto.WebProxy.TLSKey = "/etc/letsencrypt/live/myrouter.duckdns.org/privkey.pem"
	var loaded Config
	_, err := loaded.LoadWithMigration(writeConfigFile(t, safeCopyBytes(t, &cfg)))
	if err == nil {
		t.Fatal("a safe copy whose only placeholders are masked TLS paths must be refused")
	}
	for _, path := range []string{"system.web_server.tls_cert", "system.web_server.tls_key", "system.mtproto.web_proxy.tls_cert", "system.mtproto.web_proxy.tls_key"} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("the refusal does not name %s: %v", path, err)
		}
	}
	if loaded.System.WebServer.TLSCert != "" || loaded.System.MTProto.WebProxy.TLSCert != "" {
		t.Error("a refused safe copy must not leave its TLS paths in the config b4 starts with")
	}
}

func TestLoadWithMigrationRefusesAMaskedWatchdogEntry(t *testing.T) {
	cfg := NewConfig()
	cfg.System.Checker.Watchdog.Domains = []string{"youtube.com", "https://admin:hunter2@nas.example.com/health"}
	var loaded Config
	_, err := loaded.LoadWithMigration(writeConfigFile(t, safeCopyBytes(t, &cfg)))
	if err == nil || !strings.Contains(err.Error(), "system.checker.watchdog.domains[1]") {
		t.Fatalf("a masked watchdog entry must be refused, got %v", err)
	}
}

func TestLoadWithMigrationRefusesPlaceholdersUnderAnyKeySpelling(t *testing.T) {
	cases := map[string]string{
		"object key case":   `{"version": 52, "system": {"Socks5": {"enabled": true, "username": "[redacted]", "password": "[redacted]"}}}`,
		"field key case":    `{"version": 52, "system": {"socks5": {"enabled": true, "Username": "[redacted]", "PASSWORD": "[redacted]"}}}`,
		"MCP token":         `{"version": 52, "system": {"web_server": {"MCP": {"enabled": true, "token": "[redacted]"}}}}`,
		"repeated object":   `{"version": 52, "system": {"socks5": {"username": "[redacted]", "password": "[redacted]"}, "socks5": {"enabled": true}}}`,
		"set field case":    `{"version": 52, "sets": [{"name": "a", "routing": {"upstream": {"Password": "[redacted]"}}}]}`,
		"repeated routing":  `{"version": 52, "sets": [{"name": "a", "routing": {"upstream": {"password": "[redacted]"}}, "routing": {"enabled": true}}]}`,
		"top-level Sets":    `{"version": 52, "Sets": [{"name": "a", "routing": {"upstream": {"password": "[redacted]"}}}]}`,
		"with a type error": `{"version": 52, "system": {"Socks5": {"enabled": true, "username": "[redacted]"}, "web_server": {"port": "8443"}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var loaded Config
			_, err := loaded.LoadWithMigration(writeConfigFile(t, []byte(body)))
			if err == nil || !strings.Contains(err.Error(), "safe copy") {
				t.Fatalf("the file must be refused as a safe copy, got %v", err)
			}
			if paths := loaded.RedactedValuePaths(); len(paths) != 0 {
				t.Errorf("the refused file left placeholders behind: %v", paths)
			}
		})
	}
}

func TestLoadWithMigrationKeepsTheWebUIOffWhenItRefusesAFile(t *testing.T) {
	cfg := NewConfig()
	cfg.System.WebServer.Port = 0
	cfg.System.Socks5.Username = "user"
	cfg.System.Socks5.Password = "pass"
	var loaded Config
	_, err := loaded.LoadWithMigration(writeConfigFile(t, safeCopyBytes(t, &cfg)))
	if err == nil || !strings.Contains(err.Error(), "safe copy") {
		t.Fatalf("loading a safe copy must fail with an explanation, got %v", err)
	}
	if loaded.System.WebServer.Port != 0 {
		t.Errorf("the file had the web UI off, but the refused config opens it on port %d", loaded.System.WebServer.Port)
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
