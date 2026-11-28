package handler

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/log"
)

func traceTestSet(id, name string, edit func(*config.SetConfig)) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name = id, name
	if edit != nil {
		edit(&set)
	}
	return &set
}

func recordingAPI(t *testing.T, cfg *config.Config) (*API, *http.ServeMux, *lockedBuffer) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	api := &API{
		cfgPtr:                 testCfgPtr(cfg),
		overrideServiceManager: func() string { return "standalone" },
		geodataManager:         geodat.NewGeodataManager("", ""),
		mux:                    http.NewServeMux(),
	}
	api.RegisterLogTraceApi()
	api.RegisterUIApi()
	t.Cleanup(resetTraceState)
	resetTraceState()
	return api, api.mux, traceConsole(t, log.LevelInfo)
}

func traceHeaderDoc(t *testing.T, trace string) map[string]any {
	t.Helper()
	_, rest, found := strings.Cut(trace, "--- configuration ")
	_, rest, named := strings.Cut(rest, " ---\n")
	text, _, ended := strings.Cut(rest, "\n=========================\n")
	if !found || !named || !ended {
		t.Fatalf("the trace header holds no configuration:\n%s", trace)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("the configuration in the header: %v\n%s", err, text)
	}
	return doc
}

func shownTraceConfig(t *testing.T, cfg *config.Config) map[string]any {
	t.Helper()
	_, doc, err := readTraceConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return traceHeaderDoc(t, string(traceHeader(time.Now(), "", Diagnostics{}, traceConfigText(doc, nil), 1<<20)))
}

func traceDocValue(doc any, path ...any) any {
	for _, step := range path {
		switch key := step.(type) {
		case string:
			node, _ := doc.(map[string]any)
			doc = node[key]
		case int:
			list, _ := doc.([]any)
			if key >= len(list) {
				return nil
			}
			doc = list[key]
		}
	}
	return doc
}

func wantTraceValue(t *testing.T, doc map[string]any, want any, path ...any) {
	t.Helper()
	if got := traceDocValue(doc, path...); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%v = %v, want %v", path, got, want)
	}
}

func TestTheTraceHeaderCarriesTheConfiguration(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Socks5 = config.Socks5Config{Enabled: true, Port: 1081, Username: "canaryuser", Password: "canarypass"}
	cfg.System.MTProto.Secrets = []config.MTProtoSecret{{ID: "s1", Name: "canaryname", Secret: "canarysecret"}}
	cfg.Queue.Threads = 7
	cfg.Queue.Devices = config.DevicesConfig{Enabled: true, WhiteIsBlack: true, Devices: []config.Device{{MAC: "02:00:00:00:00:01", IP: "192.0.2.10", Name: "canarydevice"}}}
	cfg.UI.Dashboard.Order = []string{"canarypanel"}
	cfg.Sets = []*config.SetConfig{
		traceTestSet("aaaa1111", "Routed", func(set *config.SetConfig) {
			set.Fragmentation.Combo.FirstDelayMs = 77
			set.Fragmentation.Disorder.SplitPosition = 5
			set.Routing = config.RoutingConfig{Enabled: true, Mode: config.RoutingModeProxy, KillSwitch: true, FWMark: 0x1000, Table: 201,
				Upstream: config.UpstreamProxyConfig{Host: "192.168.1.50", Port: 1080, Username: "canaryuser", Password: "canarypass", FailOpen: true}}
			set.Escalate.To = "bbbb2222"
		}),
		traceTestSet("bbbb2222", "Fallback", func(set *config.SetConfig) { set.Enabled = false }),
	}
	_, mux, _ := recordingAPI(t, &cfg)
	startTrace(t, mux)
	stopTrace(t, mux)

	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"--- system diagnostics ---", "--- configuration (the values that differ from the defaults; long lists are cut) ---"}, []string{"canary", "02:00:00:00:00:01", "192.0.2.10"})
	doc := traceHeaderDoc(t, trace)
	wantTraceValue(t, doc, 1081, "system", "socks5", "port")
	wantTraceValue(t, doc, "[redacted]", "system", "socks5", "password")
	wantTraceValue(t, doc, "[redacted]", "system", "mtproto", "secrets", 0, "secret")
	wantTraceValue(t, doc, 7, "queue", "threads")
	wantTraceValue(t, doc, true, "queue", "devices", "enabled")
	wantTraceValue(t, doc, true, "queue", "devices", "wisb")
	wantTraceValue(t, doc, 1, "queue", "devices", "devices_total")
	wantTraceValue(t, doc, nil, "queue", "devices", "devices")
	wantTraceValue(t, doc, nil, "ui")
	wantTraceValue(t, doc, "Routed", "sets", 0, "name")
	wantTraceValue(t, doc, 77, "sets", 0, "fragmentation", "combo", "first_delay_ms")
	wantTraceValue(t, doc, 5, "sets", 0, "fragmentation", "disorder", "split_position")
	wantTraceValue(t, doc, true, "sets", 0, "routing", "kill_switch")
	wantTraceValue(t, doc, "192.168.1.50", "sets", 0, "routing", "upstream", "host")
	wantTraceValue(t, doc, "[redacted]", "sets", 0, "routing", "upstream", "password")
	wantTraceValue(t, doc, 0x1000, "sets", 0, "routing", "fwmark")
	wantTraceValue(t, doc, "bbbb2222", "sets", 0, "escalate", "to")
	wantTraceValue(t, doc, nil, "sets", 0, "faking")
	wantTraceValue(t, doc, "Fallback", "sets", 1, "name")
	wantTraceValue(t, doc, false, "sets", 1, "enabled")
}

func TestTheTraceHeaderSpellsANameAsTheChangeLinesDo(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{traceTestSet("aaaa1111", "Talk & more <beta>", nil)}
	_, mux, _ := recordingAPI(t, &cfg)
	startTrace(t, mux)
	stopTrace(t, mux)

	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{`"name": "Talk & more <beta>"`}, nil)
}

func TestTheTraceHeaderSaysThatABlockIsOff(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Socks5.Port = 1089
	cfg.System.Hub.PublicKey = "key"
	cfg.Sets = []*config.SetConfig{
		traceTestSet("off", "Off", func(set *config.SetConfig) {
			set.Routing.Mode, set.Routing.Upstream.Host = config.RoutingModeProxy, "192.0.2.50"
			set.DNS.DoHURL = "https://dns.google/dns-query"
			set.TCP.WSSize.Window = 1
			set.TCP.Duplicate.Count = 7
			set.MSSClamp.Size = 88
			set.DSCP.Value = 46
		}),
		traceTestSet("on", "On", func(set *config.SetConfig) {
			set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
			set.DSCP.Enabled = true
		}),
		traceTestSet("plain", "Plain", nil),
	}
	doc := shownTraceConfig(t, &cfg)
	for _, block := range [][]any{{"routing"}, {"dns"}, {"tcp", "wssize"}, {"tcp", "duplicate"}, {"mss_clamp"}, {"dscp"}} {
		path := append(append([]any{"sets", 0}, block...), "enabled")
		wantTraceValue(t, doc, false, path...)
		path[1] = 2
		wantTraceValue(t, doc, nil, path[:len(path)-1]...)
	}
	wantTraceValue(t, doc, nil, "sets", 0, "tcp", "enabled")
	wantTraceValue(t, doc, true, "sets", 1, "routing", "enabled")
	wantTraceValue(t, doc, true, "sets", 1, "dscp", "enabled")
	wantTraceValue(t, doc, false, "system", "socks5", "enabled")
	wantTraceValue(t, doc, config.NewConfig().System.Hub.Enabled, "system", "hub", "enabled")
	wantTraceValue(t, doc, nil, "system", "enabled")
	wantTraceValue(t, doc, nil, "system", "mtproto")
}

func TestTheTraceTellsThatASetCameFromTheHubNotWhichOne(t *testing.T) {
	const when = "2026-01-01T00:00:00Z"
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{
		traceTestSet("applied", "Applied", func(set *config.SetConfig) {
			set.Hub = &config.HubOrigin{ID: "canaryapplied", Version: 3, Hash: "canaryhash", AppliedAt: when, Vote: "canaryvote", VotedAt: "canarytime"}
		}),
		traceTestSet("published", "Published", func(set *config.SetConfig) {
			set.Hub = &config.HubOrigin{ID: "canarypublished", Version: 1, Hash: "canaryhash"}
		}),
		traceTestSet("plain", "Plain", nil),
	}
	doc := shownTraceConfig(t, &cfg)
	if got := fmt.Sprint(doc); strings.Contains(got, "canary") {
		t.Errorf("the header names a set of the hub or a vote: %s", got)
	}
	wantTraceValue(t, doc, "map[applied_at:"+when+"]", "sets", 0, "hub")
	wantTraceValue(t, doc, nil, "sets", 1, "hub")
	wantTraceValue(t, doc, nil, "sets", 2, "hub")
	got := changedConfigLines(t, &cfg, func(c *config.Config) {
		c.Sets[0].Hub.Vote, c.Sets[0].Hub.ID, c.Sets[1].Hub.Version = "canaryother", "canaryown", 2
		c.Sets[2].Hub = &config.HubOrigin{ID: "canaryother", Version: 1, AppliedAt: when}
	})
	if want := []string{`set "Plain": hub.applied_at none -> "` + when + `"`}; !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTheTraceMasksALoginInTheProxyHost(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{traceTestSet("p", "Proxy", func(set *config.SetConfig) {
		set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
		set.Routing.Upstream = config.UpstreamProxyConfig{Host: "canaryuser:canarypass@192.0.2.9", Port: 1080}
	})}
	wantTraceValue(t, shownTraceConfig(t, &cfg), config.RedactedMarker, "sets", 0, "routing", "upstream", "host")
	want := []string{`set "Proxy": routing.upstream.host "[redacted]" -> "192.0.2.9"; routing.upstream.password none -> "[redacted]"; routing.upstream.username none -> "[redacted]"`}
	got := changedConfigLines(t, &cfg, func(c *config.Config) {
		c.Sets[0].Routing.Upstream = config.UpstreamProxyConfig{Host: "192.0.2.9", Port: 1080, Username: "canaryuser", Password: "canarypass"}
	})
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

type traceCanary struct {
	path string
	text string
}

func fillTraceCanaries(v reflect.Value, path string, found *[]traceCanary) {
	next := func(at string) string {
		*found = append(*found, traceCanary{at, fmt.Sprintf("canary%04d", len(*found))})
		return (*found)[len(*found)-1].text
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		fillTraceCanaries(v.Elem(), path, found)
	case reflect.Struct:
		for i := range v.NumField() {
			field := v.Type().Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if field.IsExported() && name != "-" {
				fillTraceCanaries(v.Field(i), tracePath(path, name), found)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillTraceCanaries(v.Index(0), path+"[]", found)
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		key, value := reflect.ValueOf(next(path+"{}")), reflect.New(v.Type().Elem()).Elem()
		if value.Kind() == reflect.Interface {
			value.Set(reflect.ValueOf(next(path + "{}")))
		} else {
			fillTraceCanaries(value, path+"{}", found)
		}
		v.SetMapIndex(key, value)
	case reflect.String:
		v.SetString(next(path))
	}
}

func TestTheTraceHeaderHoldsTheSafeCopyWithoutDevicesAndUI(t *testing.T) {
	var cfg config.Config
	var canaries []traceCanary
	fillTraceCanaries(reflect.ValueOf(&cfg).Elem(), "", &canaries)
	cfg.Sets[0].Enabled = true

	copied, err := cfg.RedactedCopy()
	if err != nil {
		t.Fatal(err)
	}
	safe, err := json.Marshal(copied)
	if err != nil {
		t.Fatal(err)
	}
	values, doc, err := readTraceConfig(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	header := traceConfigText(doc, nil)
	lines := strings.Join(values.changesSince(traceConfig{}), "\n")

	withheld := []string{"ui.", "queue.devices.devices[].", "sets[].targets.source_devices[]", "system.socks5.allowed_sources[]", "sets[].hub.id", "sets[].hub.hash", "sets[].hub.vote", "sets[].hub.voted_at", "sets[].hub.group_id"}
	shown := 0
	for _, canary := range canaries {
		want := strings.Contains(string(safe), canary.text) &&
			!slices.ContainsFunc(withheld, func(prefix string) bool { return strings.HasPrefix(canary.path, prefix) })
		if got := strings.Contains(string(header), canary.text); got != want {
			t.Errorf("%s: in the header %v, want %v", canary.path, got, want)
		}
		if got := strings.Contains(lines, canary.text); got != want {
			t.Errorf("%s: in the lines of a change %v, want %v", canary.path, got, want)
		}
		if want {
			shown++
		}
	}
	if len(canaries) < 100 || shown < 50 {
		t.Fatalf("%d of %d string settings are shown, the walker misses most", shown, len(canaries))
	}
}

func TestTheTraceHeaderSamplesLongLists(t *testing.T) {
	names := func(prefix string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%s%02d.example", prefix, i)
		}
		return out
	}
	cfg := config.NewConfig()
	cfg.System.Checker.Watchdog.Domains = names("watched", 9)
	cfg.System.Socks5.AllowedSources = []string{"192.0.2.0/24", "198.51.100.7"}
	cfg.Sets = []*config.SetConfig{
		traceTestSet("long", "Long", func(set *config.SetConfig) {
			set.Targets.SNIDomains = names("domain", 9)
			set.Targets.IPs = []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4", "192.0.2.5", "192.0.2.6", "192.0.2.7", "192.0.2.8", "192.0.2.9", "192.0.2.10"}
			set.Targets.GeoSiteCategories = names("category", 20)
			set.Targets.ASNs = make([]string, 70)
			set.Targets.SourceDevices = []string{"02:00:00:00:00:01", "02:00:00:00:00:02"}
			for _, name := range names("url", 9) {
				set.Discovery.URLs = append(set.Discovery.URLs, "https://"+name+"/canary")
			}
			set.DNS.Pins = map[string][]string{}
			for _, name := range names("pin", 10) {
				set.DNS.Pins[name] = []string{"192.0.2.1"}
			}
			set.Faking.CustomPayload = strings.Repeat("ab", 4000)
		}),
		traceTestSet("short", "Short", func(set *config.SetConfig) { set.Targets.SNIDomains = names("domain", 8) }),
	}
	doc := shownTraceConfig(t, &cfg)
	if got := fmt.Sprint(doc); strings.Contains(got, "02:00:00") || strings.Contains(got, "198.51.100.7") || strings.Contains(got, "canary") {
		t.Errorf("a device, an allowed source or the path of a URL is in the header: %s", got)
	}
	for _, list := range []struct {
		kept, total any
		path        []any
	}{
		{8, 9, []any{"sets", 0, "targets", "sni_domains"}},
		{8, 10, []any{"sets", 0, "targets", "ip"}},
		{20, nil, []any{"sets", 0, "targets", "geosite_categories"}},
		{64, 70, []any{"sets", 0, "targets", "asns"}},
		{0, 2, []any{"sets", 0, "targets", "source_devices"}},
		{8, 9, []any{"sets", 0, "discovery", "urls"}},
		{8, 10, []any{"sets", 0, "dns", "pins"}},
		{8, nil, []any{"sets", 1, "targets", "sni_domains"}},
		{8, 9, []any{"system", "checker", "watchdog", "domains"}},
		{0, 2, []any{"system", "socks5", "allowed_sources"}},
	} {
		kept := 0
		switch v := traceDocValue(doc, list.path...).(type) {
		case []any:
			kept = len(v)
		case map[string]any:
			kept = len(v)
			if got, want := slices.Sorted(maps.Keys(v)), names("pin", 8); !slices.Equal(got, want) {
				t.Errorf("%v keeps %v, want the first names %v", list.path, got, want)
			}
		}
		if kept != list.kept {
			t.Errorf("%v keeps %d entries, want %v", list.path, kept, list.kept)
		}
		last := len(list.path) - 1
		totalPath := append(slices.Clone(list.path[:last]), list.path[last].(string)+"_total")
		wantTraceValue(t, doc, list.total, totalPath...)
	}
	if got, _ := traceDocValue(doc, "sets", 0, "faking", "custom_payload").(string); len(got) != 512 || !strings.HasSuffix(got, traceTextCut) {
		t.Errorf("a long value is %d bytes in the header, ending in %q", len(got), got[max(0, len(got)-10):])
	}
}

func TestTheTraceHeaderShowsWhatTheTargetsResolvedTo(t *testing.T) {
	targets := config.TargetsConfig{
		ASNs:           []string{"64496"},
		DomainsToMatch: []string{"a.example", "b.example", "c.example"},
		IpsToMatch:     []string{"192.0.2.0/24"},
	}
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{
		traceTestSet("on", "On", func(set *config.SetConfig) { set.Targets = targets }),
		traceTestSet("off", "Off", func(set *config.SetConfig) { set.Targets, set.Enabled = targets, false }),
		traceTestSet("none", "None", nil),
	}
	doc := shownTraceConfig(t, &cfg)
	wantTraceValue(t, doc, 3, "sets", 0, "targets", "resolved_domains")
	wantTraceValue(t, doc, 1, "sets", 0, "targets", "resolved_ips")
	wantTraceValue(t, doc, "[64496]", "sets", 0, "targets", "asns_unresolved")
	for _, key := range []string{"resolved_domains", "resolved_ips", "asns_unresolved"} {
		wantTraceValue(t, doc, nil, "sets", 1, "targets", key)
	}
	wantTraceValue(t, doc, 0, "sets", 2, "targets", "resolved_domains")
	wantTraceValue(t, doc, 0, "sets", 2, "targets", "resolved_ips")
}

func TestTheTraceHeaderMasksResolvers(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{
		traceTestSet("list", "List", func(set *config.SetConfig) {
			set.DNS = config.DNSConfig{Enabled: true, Upstreams: []string{"9.9.9.9", "https://dns.nextdns.io/canaryid", "https://dns.example/dns-query?id=canaryid"}}
		}),
		traceTestSet("first", "First", func(set *config.SetConfig) {
			set.DNS = config.DNSConfig{Enabled: true, DoHURL: "https://canaryuser:canarypass@dns.nextdns.io/canaryid?name=canaryname#canaryfragment"}
		}),
		traceTestSet("plain", "Plain", func(set *config.SetConfig) {
			set.DNS = config.DNSConfig{Enabled: true, TargetDNS: "https://canaryuser:canarypass@dns.example/canaryid"}
		}),
		traceTestSet("address", "Address", func(set *config.SetConfig) {
			set.DNS = config.DNSConfig{Enabled: true, TargetDNS: "192.0.2.1:53"}
		}),
	}
	doc := shownTraceConfig(t, &cfg)
	if got := fmt.Sprint(doc); strings.Contains(got, "canary") {
		t.Errorf("a private part of a resolver address is in the header: %s", got)
	}
	wantTraceValue(t, doc, "[9.9.9.9 https://dns.nextdns.io/[redacted] https://[redacted]/dns-query?id=[redacted]]", "sets", 0, "dns", "upstreams")
	wantTraceValue(t, doc, "https://[redacted]@dns.nextdns.io/[redacted]?name=[redacted]#[redacted]", "sets", 1, "dns", "doh_url")
	wantTraceValue(t, doc, "https://[redacted]@[redacted]/[redacted]", "sets", 2, "dns", "target_dns")
	wantTraceValue(t, doc, "192.0.2.1:53", "sets", 3, "dns", "target_dns")
}

func TestAConfigurationThatCannotBeShownLeavesAReasonInTheHeader(t *testing.T) {
	header := string(traceHeader(time.Now(), "", Diagnostics{}, traceConfigText(nil, fmt.Errorf("no such thing")), 1<<20))
	wantLines(t, "the header", header, []string{"--- configuration ", "cannot be shown: no such thing", "\n=========================\n"}, nil)
}

func changedConfigLines(t *testing.T, before *config.Config, edit func(*config.Config)) []string {
	t.Helper()
	after := before.Clone()
	edit(after)
	was, _, err := readTraceConfig(before)
	if err != nil {
		t.Fatal(err)
	}
	now, _, err := readTraceConfig(after)
	if err != nil {
		t.Fatal(err)
	}
	return now.changesSince(was)
}

func TestAConfigurationChangeNamesWhatChanged(t *testing.T) {
	nine := func() []string {
		names := make([]string, 9)
		for i := range names {
			names[i] = fmt.Sprintf("category%d", i)
		}
		return names
	}
	base := func() *config.Config {
		cfg := config.NewConfig()
		cfg.System.Socks5 = config.Socks5Config{Enabled: true, Port: 1089, BindAddress: "192.0.2.5", Username: "user", Password: "canaryold"}
		cfg.Queue.Devices.Devices = []config.Device{{MAC: "02:00:00:00:00:01"}}
		cfg.Sets = []*config.SetConfig{
			traceTestSet("a", "Video", func(set *config.SetConfig) {
				set.Targets.SNIDomains = []string{"a.example", "b.example"}
				set.Targets.SourceDevices = []string{"02:00:00:00:00:01"}
				set.Fragmentation.SeqOverlapPattern = []string{"0x16", "0x03", "0x03", "0x01"}
			}),
			traceTestSet("b", "Chat", func(set *config.SetConfig) {
				set.Targets.SNIDomains = []string{"chat.example"}
				set.DNS.Upstreams = []string{"9.9.9.9", "https://dns.nextdns.io/canarya"}
				set.Discovery.URLs = []string{"https://a.example/canaryone"}
			}),
			traceTestSet("c", "Games", func(set *config.SetConfig) {
				set.Targets.GeoSiteCategories = nine()
				set.TCP.Win.Values = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			}),
		}
		return &cfg
	}
	for name, tc := range map[string]struct {
		edit func(*config.Config)
		want []string
	}{
		"nothing":            {func(*config.Config) {}, nil},
		"the dashboard only": {func(c *config.Config) { c.UI.Dashboard.Hidden = []string{"cpu"} }, nil},
		"a set switched off": {func(c *config.Config) { c.Sets[1].Enabled = false }, []string{
			`set "Chat": enabled true -> false; targets.resolved_domains 0 -> none; targets.resolved_ips 0 -> none`}},
		"a strategy and a global setting": {func(c *config.Config) {
			c.Sets[0].Fragmentation.Strategy = "disorder"
			c.System.Socks5.Port = 1090
		}, []string{`system.socks5.port 1089 -> 1090`, `set "Video": fragmentation.strategy "combo" -> "disorder"`}},
		"back to the default": {func(c *config.Config) { c.System.Socks5.BindAddress = config.NewConfig().System.Socks5.BindAddress }, []string{
			`system.socks5.bind_address "192.0.2.5" -> "0.0.0.0"`}},
		"a masked value": {func(c *config.Config) { c.System.Socks5.Password = "canarynew" }, []string{`system.socks5.password changed`}},
		"a masked resolver": {func(c *config.Config) { c.Sets[0].DNS.DoHURL = "https://dns.nextdns.io/canaryid" }, []string{
			`set "Video": dns.doh_url "" -> "https://dns.nextdns.io/[redacted]"`}},
		"the web password": {func(c *config.Config) { c.System.WebServer.Password = "canaryhash" }, []string{
			`system.web_server.password "" -> [redacted]; system.web_server.password_set none -> true`}},
		"a domain added and one removed": {func(c *config.Config) { c.Sets[0].Targets.SNIDomains = []string{"b.example", "c.example", "d.example"} }, []string{
			`set "Video": targets.sni_domains added ["c.example", "d.example"], removed ["a.example"] (2 -> 3 entries)`}},
		"a domain twice": {func(c *config.Config) { c.Sets[1].Targets.SNIDomains = []string{"chat.example", "chat.example"} }, []string{
			`set "Chat": targets.sni_domains added ["chat.example"] (1 -> 2 entries)`}},
		"domains in another order": {func(c *config.Config) { slices.Reverse(c.Sets[0].Targets.SNIDomains) }, []string{
			`set "Video": targets.sni_domains reordered`}},
		"the first domains of a set": {func(c *config.Config) { c.Sets[2].Targets.SNIDomains = []string{"g.example"} }, []string{
			`set "Games": targets.sni_domains added ["g.example"] (0 -> 1 entries)`}},
		"a device": {func(c *config.Config) {
			c.Queue.Devices.Devices = append(c.Queue.Devices.Devices, config.Device{MAC: "02:00:00:00:00:02"})
			c.Sets[0].Targets.SourceDevices = []string{"02:00:00:00:00:02"}
		}, []string{`queue.devices.devices 1 entries -> 2 entries`, `set "Video": targets.source_devices changed`}},
		"the first source device": {func(c *config.Config) { c.Sets[2].Targets.SourceDevices = []string{"02:00:00:00:00:03"} }, []string{
			`set "Games": targets.source_devices none -> 1 entries`}},
		"the last device": {func(c *config.Config) { c.Queue.Devices.Devices = nil }, []string{`queue.devices.devices 1 entries -> none`}},
		"a long list of categories": {func(c *config.Config) { c.Sets[2].Targets.GeoSiteCategories = append(nine(), "category9") }, []string{
			`set "Games": targets.geosite_categories added ["category9"] (9 -> 10 entries)`}},
		"a long list of numbers": {func(c *config.Config) { c.Sets[2].TCP.Win.Values = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 11} }, []string{
			`set "Games": tcp.win.values added [11], removed [10] (10 -> 10 entries)`}},
		"a new name":     {func(c *config.Config) { c.Sets[1].Name = "Talk & more" }, []string{`set "Talk & more": name "Chat" -> "Talk & more"`}},
		"a removed set":  {func(c *config.Config) { c.Sets = c.Sets[:2] }, []string{`set "Games" removed`}},
		"a set moved up": {func(c *config.Config) { slices.Reverse(c.Sets) }, []string{`the order of the sets: set "Games", set "Chat", set "Video"`}},
		"a set put first": {func(c *config.Config) {
			c.Sets = append([]*config.SetConfig{traceTestSet("d", "New", func(set *config.SetConfig) { set.Targets.SNIDomains = []string{"new.example"} })}, c.Sets...)
		}, []string{`set "New" added as set 1 of 4: id "d"; enabled true; name "New"; targets.resolved_domains 0; targets.resolved_ips 0; targets.sni_domains ["new.example"]`}},
		"a pin": {func(c *config.Config) {
			c.Sets[0].DNS.Pins = map[string][]string{"a.example": {"192.0.2.1", "192.0.2.2"}}
		}, []string{
			`set "Video": dns.pins added ["a.example 192.0.2.1 192.0.2.2"] (0 -> 1 entries)`}},
		"a set removed and two swapped": {func(c *config.Config) { c.Sets = []*config.SetConfig{c.Sets[1], c.Sets[0]} }, []string{
			`set "Games" removed`, `the order of the sets: set "Chat", set "Video"`}},
		"a set without a name": {func(c *config.Config) { c.Sets[2].Name = "" }, []string{`set "c": name "Games" -> ""`}},
		"two sets of one name": {func(c *config.Config) {
			c.Sets[1].Name = "Video"
			c.Sets[0].TCP.Seg2Delay = 5
			c.Sets = []*config.SetConfig{c.Sets[1], c.Sets[0]}
		}, []string{
			`set "Video" (id "b"): name "Chat" -> "Video"`,
			`set "Video" (id "a"): tcp.seg2delay 0 -> 5`,
			`set "Games" removed`,
			`the order of the sets: set "Video" (id "b"), set "Video" (id "a")`}},
		"an unresolved ASN": {func(c *config.Config) { c.Sets[2].Targets.ASNs = []string{"64496"} }, []string{
			`set "Games": targets.asns none -> ["64496"]; targets.asns_unresolved none -> ["64496"]`}},
		"a pattern in another order": {func(c *config.Config) {
			c.Sets[0].Fragmentation.SeqOverlapPattern = []string{"0x16", "0x01", "0x03", "0x03"}
		}, []string{`set "Video": fragmentation.seq_overlap_pattern ["0x16", "0x03", "0x03", "0x01"] -> ["0x16", "0x01", "0x03", "0x03"]`}},
		"a resolver put second": {func(c *config.Config) {
			c.Sets[1].DNS.Upstreams = []string{"9.9.9.9", "8.8.8.8", "https://dns.nextdns.io/canarya"}
		}, []string{
			`set "Chat": dns.upstreams ["9.9.9.9", "https://dns.nextdns.io/[redacted]"] -> ["9.9.9.9", "8.8.8.8", "https://dns.nextdns.io/[redacted]"]`}},
		"another masked resolver": {func(c *config.Config) { c.Sets[1].DNS.Upstreams[1] = "https://dns.nextdns.io/canaryb" }, []string{`set "Chat": dns.upstreams changed`}},
		"another path of a URL":   {func(c *config.Config) { c.Sets[1].Discovery.URLs = []string{"https://a.example/canarytwo"} }, []string{`set "Chat": discovery.urls changed`}},
		"resolved targets": {func(c *config.Config) { c.Sets[2].Targets.IpsToMatch = []string{"192.0.2.0/24"} }, []string{
			`set "Games": targets.resolved_ips 0 -> 1`}},
	} {
		t.Run(name, func(t *testing.T) {
			got := changedConfigLines(t, base(), tc.edit)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}

	var ten []string
	for i := range 10 {
		ten = append(ten, fmt.Sprintf("n%d.example", i))
	}
	want := []string{`set "Video": targets.sni_domains added ["n0.example", "n1.example", "n2.example", "n3.example", "n4.example", "n5.example", "n6.example", "n7.example"] of 10 entries, removed ["x.example"] (1 -> 10 entries)`}
	short := base()
	short.Sets[0].Targets.SNIDomains = []string{"x.example"}
	if got := changedConfigLines(t, short, func(c *config.Config) { c.Sets[0].Targets.SNIDomains = ten }); !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	want = []string{`set "Video": targets.sni_domains added ["n0.example", "n1.example", "n2.example", "n3.example", "n4.example", "n5.example", "n6.example", "n7.example"] of 10 entries (0 -> 10 entries)`}
	short.Sets[0].Targets.SNIDomains = nil
	if got := changedConfigLines(t, short, func(c *config.Config) { c.Sets[0].Targets.SNIDomains = ten }); !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestAConfigurationChangeSamplesPins(t *testing.T) {
	pins := func(n int) map[string][]string {
		out := map[string][]string{}
		for i := range n {
			out[fmt.Sprintf("pin%03d.example", i)] = []string{"192.0.2.1"}
		}
		return out
	}
	config200 := func(n int) *config.Config {
		cfg := config.NewConfig()
		cfg.Sets = []*config.SetConfig{traceTestSet("a", "Video", func(set *config.SetConfig) { set.DNS.Pins = pins(n) })}
		return &cfg
	}
	for name, tc := range map[string]struct {
		before int
		edit   func(*config.Config)
		want   []string
	}{
		"added":    {0, func(c *config.Config) { c.Sets[0].DNS.Pins = pins(200) }, []string{"dns.pins added [", "of 200 entries (0 -> 200 entries)"}},
		"removed":  {200, func(c *config.Config) { c.Sets[0].DNS.Pins = nil }, []string{"dns.pins removed [", "of 200 entries (200 -> 0 entries)"}},
		"one more": {200, func(c *config.Config) { c.Sets[0].DNS.Pins["pin200.example"] = []string{"192.0.2.1"} }, []string{`dns.pins added ["pin200.example 192.0.2.1"] (200 -> 201 entries)`}},
		"with the set": {0, func(c *config.Config) {
			c.Sets = append(c.Sets, traceTestSet("b", "New", func(set *config.SetConfig) { set.DNS.Pins = pins(200) }))
		}, []string{"dns.pins [", "of 200 entries"}},
	} {
		got := strings.Join(changedConfigLines(t, config200(tc.before), func(c *config.Config) {
			tc.edit(c)
			c.Sets[len(c.Sets)-1].Fragmentation.Strategy = "disorder"
		}), "\n")
		wantLines(t, name, got, append(tc.want, `"disorder"`), nil)
		if names := strings.Count(got, ".example 192.0.2.1"); names > 8 || len(got) > 1000 {
			t.Errorf("%s: %d pins are named in %d bytes: %s", name, names, len(got), got)
		}
	}
}

func TestUnchangedPinsAreNotTold(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{traceTestSet("a", "Video", func(set *config.SetConfig) {
		set.DNS.Pins = map[string][]string{}
		for i := range 50 {
			set.DNS.Pins[fmt.Sprintf("pin%d.example", i)] = []string{"192.0.2.1"}
		}
	})}
	want := []string{`set "Video": tcp.seg2delay 0 -> 5`}
	if got := changedConfigLines(t, &cfg, func(c *config.Config) { c.Sets[0].TCP.Seg2Delay = 5 }); !slices.Equal(got, want) {
		t.Errorf("got  %.300q\nwant %q", got, want)
	}
}

func TestAConfigurationChangeCutsLongValues(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{traceTestSet("a", "Video", nil)}
	got := changedConfigLines(t, &cfg, func(c *config.Config) {
		c.Sets[0].Faking.CustomPayload = strings.Repeat("ab", 2000)
		c.Sets[0].Fragmentation.Strategy = "disorder"
		c.Sets[0].Targets.SNIDomains = []string{strings.Repeat("d", 400) + ".example"}
	})
	if len(got) != 1 {
		t.Fatalf("got %q", got)
	}
	parts := strings.Split(got[0], "; ")
	if len(parts) != 3 || parts[1] != `fragmentation.strategy "combo" -> "disorder"` {
		t.Fatalf("got %q", parts)
	}
	for i, prefix := range map[int]string{0: `set "Video": faking.custom_payload "" -> `, 2: "targets.sni_domains added ["} {
		value, _, _ := strings.Cut(strings.TrimPrefix(parts[i], prefix), "]")
		if i == 0 {
			value = strings.TrimPrefix(parts[i], prefix)
		} else {
			value = value[:strings.Index(value, "[cut")] + "[cut]"
		}
		if len(value) != 160 || !strings.HasSuffix(value, traceTextCut) {
			t.Errorf("%s: the value takes %d bytes, ending in %q", prefix, len(value), value[max(0, len(value)-12):])
		}
	}
}

func TestASaveDuringARecordingLeavesALineInTheTrace(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Socks5.Password = "canaryold"
	cfg.System.Socks5.Username = "user"
	cfg.Sets = []*config.SetConfig{traceTestSet("a", "Video", nil)}
	api, mux, console := recordingAPI(t, &cfg)
	save := func(edit func(*config.Config)) {
		t.Helper()
		next := api.getCfg().Clone()
		edit(next)
		if err := api.saveAndPushConfig(next); err != nil {
			t.Fatal(err)
		}
	}

	save(func(c *config.Config) { c.Sets[0].TCP.Seg2Delay = 3 })
	startTrace(t, mux)
	save(func(c *config.Config) { c.Sets[0].TCP.Seg2Delay = 5 })
	save(func(c *config.Config) { c.Sets[0].TCP.Seg2Delay = 9; c.System.Socks5.Password = "canarynew" })
	save(func(*config.Config) {})
	if rec := serve(mux, http.MethodPut, "/api/ui/dashboard", `{"hidden":["cpu"]}`); rec.Code != http.StatusOK {
		t.Fatalf("dashboard: %d %s", rec.Code, rec.Body)
	}
	next := api.getCfg().Clone()
	next.Sets[0].Targets.IpsToMatch = []string{"192.0.2.0/24"}
	api.publishConfig(next)
	save(func(c *config.Config) {
		for i := range 60 {
			c.Sets = append(c.Sets, traceTestSet(fmt.Sprintf("s%02d", i), fmt.Sprintf("Set %02d %s", i, strings.Repeat("n", 5000)), nil))
		}
	})
	stopTrace(t, mux)
	save(func(c *config.Config) { c.Sets[0].TCP.Seg2Delay = 11 })

	trace := downloadedTrace(t, mux)
	wantTraceValue(t, traceHeaderDoc(t, trace), 3, "sets", 0, "tcp", "seg2delay")
	var got []string
	for _, line := range strings.Split(trace, "\n") {
		if _, change, found := strings.Cut(line, "[DEBUG] Configuration changed: "); found {
			got = append(got, change)
		}
	}
	want := []string{
		`set "Video": tcp.seg2delay 3 -> 5`,
		`system.socks5.password changed`,
		`set "Video": tcp.seg2delay 5 -> 9`,
		`set "Video": targets.resolved_ips 0 -> 1`,
	}
	if len(got) != len(want)+5 || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("the trace tells %d changes, %.300q, want %q and five lines of the import", len(got), got, want)
	}
	for _, line := range got[len(want) : len(got)-1] {
		if len(line) != 4096 || !strings.HasSuffix(line, traceTextCut) {
			t.Errorf("an added set takes %d bytes, ending in %q", len(line), line[max(0, len(line)-12):])
		}
	}
	if last := got[len(got)-1]; last != "56 more changes" {
		t.Errorf("the import ends with %.100q", last)
	}
	wantLines(t, "the trace", trace, nil, []string{"canary"})
	wantLines(t, "the log of a b4 at info", console.String(), []string{"Log trace session started"}, []string{"Configuration changed"})
	if recordedConfig.Load() != nil {
		t.Error("b4 still compares configurations after the trace")
	}
}

func TestAComparisonFromAnEndedTraceLeavesNoLine(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{traceTestSet("a", "Video", nil)}
	api, mux, _ := recordingAPI(t, &cfg)
	startTrace(t, mux)
	old := recordedConfig.Load()
	stopTrace(t, mux)
	next := api.getCfg().Clone()
	next.Sets[0].TCP.Seg2Delay = 4
	api.publishConfig(next)
	startTrace(t, mux)
	old.note()
	stopTrace(t, mux)
	wantLines(t, "the second trace", downloadedTrace(t, mux), nil, []string{"Configuration changed"})
}

func TestADroppedLoginIsGuardedBeforeTheChangeIsTold(t *testing.T) {
	api := exposeTestAPI(t, func(c *config.Config) {
		c.System.WebServer.Username = "admin"
		c.System.WebServer.Password = "$2a$12$hash"
	})
	var order []string
	prevDropped, prevSync := loginDroppedFunc, routingSyncFunc
	t.Cleanup(func() {
		loginDroppedFunc, routingSyncFunc = prevDropped, prevSync
		recordedConfig.Store(nil)
	})
	loginDroppedFunc = func() { order = append(order, "login") }
	routingSyncFunc = func(*config.Config) { order = append(order, "routing") }
	recordedConfig.Store(&traceConfigLog{load: func() *config.Config {
		order = append(order, "note")
		return api.getCfg()
	}})

	next := api.getCfg().Clone()
	next.System.WebServer.Username, next.System.WebServer.Password = "", ""
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatal(err)
	}
	if want := []string{"login", "note", "routing"}; !slices.Equal(order, want) {
		t.Errorf("the save did %v, want %v: the note takes time, and until the hook ran a new connection needs no login", order, want)
	}
}

func TestASaveWhileTheHeaderIsWrittenIsToldInTheTrace(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{traceTestSet("a", "Video", nil)}
	api, mux, _ := recordingAPI(t, &cfg)
	orig := createTracePart
	t.Cleanup(func() { createTracePart = orig })
	createTracePart = func(dir *traceDir, part int) (*os.File, error) {
		next := api.getCfg().Clone()
		next.Sets[0].TCP.Seg2Delay = 4
		api.publishConfig(next)
		return orig(dir, part)
	}
	startTrace(t, mux)
	stopTrace(t, mux)
	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{`Configuration changed: set "Video": tcp.seg2delay 0 -> 4`}, nil)
	wantTraceValue(t, traceHeaderDoc(t, trace), nil, "sets", 0, "tcp")
}

func TestConfigurationLinesAreBounded(t *testing.T) {
	long := strings.Repeat("x", traceLineBytes)
	lines := boundTraceLines([]string{"one\ntwo\tthree\x1b", long + "tail", long, long, long, long, "last"})
	if len(lines) != 5 || lines[0] != "one two three " || lines[4] != "3 more changes" {
		t.Fatalf("%d lines, the first %q, the last %q", len(lines), lines[0], lines[len(lines)-1])
	}
	if len(lines[1]) != traceLineBytes || !strings.HasSuffix(lines[1], traceTextCut) || len(lines[2]) != traceLineBytes {
		t.Errorf("a long line takes %d bytes, the next %d", len(lines[1]), len(lines[2]))
	}
	if got := cutTraceText(strings.Repeat("я", 100), 100); len(got) > 100 || !strings.HasSuffix(got, traceTextCut) || strings.ContainsRune(got, '�') {
		t.Errorf("a cut text is %q", got)
	}
}

func TestEveryPublishedConfigurationIsNotedInTheTrace(t *testing.T) {
	stores := map[string]int{}
	for _, pattern := range []string{"*.go", "../*.go", "../../*.go"} {
		files, err := filepath.Glob(pattern)
		if err != nil || len(files) == 0 {
			t.Fatalf("no Go files match %s: %v", pattern, err)
		}
		for _, path := range files {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(source), "cfgPtr.Store("); n > 0 && !strings.HasSuffix(path, "_test.go") {
				stores[path] = n
			}
		}
	}
	if want := map[string]int{"config.go": 2, "../../main.go": 2}; !maps.Equal(stores, want) {
		t.Errorf("cfgPtr.Store is called in %v, want %v: the start of b4, the two publishConfig functions and pushConfigLocked, which note the change in a running trace", stores, want)
	}
	source, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "cfgPtr.Store(c)\n\t\thandler.NoteTraceConfig()\n") {
		t.Error("main.go publishes a configuration without NoteTraceConfig right after it")
	}
}
