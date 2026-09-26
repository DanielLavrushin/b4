package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/asnprefix"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
)

func useAsnStore(t *testing.T) *config.AsnStore {
	t.Helper()
	s := config.InitAsnStore(filepath.Join(t.TempDir(), "b4.json"))
	drainASNRefresh()
	t.Cleanup(func() {
		config.InitAsnStore("")
		drainASNRefresh()
	})
	return s
}

func drainASNRefresh() bool {
	got := false
	for {
		select {
		case <-config.ASNRefreshRequests():
			got = true
		default:
			return got
		}
	}
}

func asnAPI(t *testing.T, sets ...*config.SetConfig) (*API, *http.ServeMux) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Sets = sets
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterAsnApi()
	api.RegisterGeoipApi()
	api.RegisterConfigApi()
	api.RegisterSetsApi()
	api.RegisterIntegrationApi()
	return api, mux
}

func asnSet(id string, asns ...string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = id, "Set "+id, true
	set.Targets.ASNs = append([]string{}, asns...)
	return &set
}

func swapResolve(t *testing.T, fn func(ctx context.Context, id string, force bool) (*config.AsnInfo, error)) *int {
	t.Helper()
	calls := 0
	prev := asnResolve
	asnResolve = func(ctx context.Context, id string, force bool) (*config.AsnInfo, error) {
		calls++
		return fn(ctx, id, force)
	}
	t.Cleanup(func() { asnResolve = prev })
	return &calls
}

func swapLookup(t *testing.T, fn func(ctx context.Context, ip string) (*asnprefix.Lookup, error)) *int {
	t.Helper()
	calls := 0
	prev := asnLookupUpstream
	asnLookupUpstream = func(ctx context.Context, ip string) (*asnprefix.Lookup, error) {
		calls++
		return fn(ctx, ip)
	}
	t.Cleanup(func() { asnLookupUpstream = prev })
	return &calls
}

func countRefreshes(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := tablesRefreshFunc
	tablesRefreshFunc = func() error { n++; return nil }
	t.Cleanup(func() { tablesRefreshFunc = prev })
	return &n
}

var telegramPrefixes = []string{"91.108.4.0/22", "91.108.8.0/22", "149.154.160.0/20", "2001:67c:4e8::/48"}

func putTelegram(t *testing.T, s *config.AsnStore) {
	t.Helper()
	if err := s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: telegramPrefixes, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
}

func TestAsnResolveReturnsTheViewWithCountsAndUsers(t *testing.T) {
	s := useAsnStore(t)
	_, mux := asnAPI(t, asnSet("tg", "62041"), asnSet("other"))
	calls := swapResolve(t, func(ctx context.Context, id string, force bool) (*config.AsnInfo, error) {
		if id != "62041" || !force {
			t.Errorf("the normalized id and the refresh flag reach the resolver: %q %v", id, force)
		}
		putTelegram(t, s)
		return s.Get(id), nil
	})

	rec := postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": "AS62041", "refresh": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var view AsnView
	decodeInto(t, rec, &view)
	if *calls != 1 || view.ID != "62041" || view.Name != "Telegram" || len(view.Prefixes) != 4 {
		t.Fatalf("view: %+v", view)
	}
	if view.AsnCounts.Prefixes != 4 || view.V4 != 3 || view.V6 != 1 || view.IPv4Addresses != 1024+1024+4096 {
		t.Errorf("counts: %+v", view.AsnCounts)
	}
	if !slices.Equal(view.UsedBy, []string{"Set tg"}) {
		t.Errorf("used_by names the referencing sets: %v", view.UsedBy)
	}
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	for _, key := range []string{"id", "name", "prefixes", "updated_at", "source", "prefix_count", "v4_count", "v6_count", "ipv4_addresses", "used_by"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("the view carries %q: %v", key, raw)
		}
	}
}

func TestAsnResolveRejectsAnInvalidNumber(t *testing.T) {
	useAsnStore(t)
	_, mux := asnAPI(t)
	calls := swapResolve(t, func(ctx context.Context, id string, force bool) (*config.AsnInfo, error) {
		return nil, errors.New("unreachable")
	})
	for _, bad := range []string{"AS64500", "abc", "", "0"} {
		rec := postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": bad})
		expectCode(t, rec, http.StatusBadRequest, "asn_invalid")
	}
	rec := serve(mux, http.MethodPost, "/api/asn/resolve", "{")
	expectCode(t, rec, http.StatusBadRequest, "invalid_json")
	if *calls != 0 {
		t.Error("nothing is fetched for an invalid number")
	}
}

func TestAsnResolveReportsAnUpstreamFailure(t *testing.T) {
	s := useAsnStore(t)
	_, mux := asnAPI(t)
	swapResolve(t, func(ctx context.Context, id string, force bool) (*config.AsnInfo, error) {
		return nil, errors.New("RIPEstat ris-prefixes answered 503 Service Unavailable")
	})

	rec := postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": "62041"})
	expectCode(t, rec, http.StatusBadGateway, "asn_fetch_failed")
	if !strings.Contains(rec.Body.String(), "503") {
		t.Errorf("the message says why: %s", rec.Body.String())
	}

	putTelegram(t, s)
	rec = postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": "62041"})
	var view AsnView
	decodeInto(t, rec, &view)
	if rec.Code != http.StatusOK || len(view.Prefixes) != 4 || !strings.Contains(view.LastError, "503") {
		t.Fatalf("without refresh a cached copy is served with the error: %d %+v", rec.Code, view)
	}

	rec = postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": "62041", "refresh": true})
	expectCode(t, rec, http.StatusBadGateway, "asn_fetch_failed")
}

func TestAsnResolveKeepsTheCachedCopyWhileAShrinkIsHeld(t *testing.T) {
	s := useAsnStore(t)
	tg := asnSet("tg", "62041")
	tg.Targets.IpsToMatch = append([]string{}, telegramPrefixes...)
	api, mux := asnAPI(t, tg)
	putTelegram(t, s)
	held := fmt.Errorf("AS62041: RIPEstat lists 1 prefixes, the known list has 4: %w", asnprefix.ErrCoverageShrunk)
	swapResolve(t, func(ctx context.Context, id string, force bool) (*config.AsnInfo, error) {
		return nil, held
	})
	before := api.getCfg()

	rec := postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": "62041"})
	var view AsnView
	decodeInto(t, rec, &view)
	if rec.Code != http.StatusOK || !slices.Equal(view.Prefixes, telegramPrefixes) || !strings.Contains(view.LastError, "less than half") {
		t.Fatalf("without refresh the cached copy is served with the pending shrink: %d %+v", rec.Code, view)
	}
	if api.getCfg() != before || !slices.Equal(api.getCfg().GetSetById("tg").Targets.IpsToMatch, telegramPrefixes) {
		t.Fatal("a held shrink reloads no set")
	}

	rec = postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": "62041", "refresh": true})
	expectCode(t, rec, http.StatusBadGateway, "asn_fetch_failed")
	if !strings.Contains(rec.Body.String(), "less than half") {
		t.Errorf("the refusal says why: %s", rec.Body.String())
	}
	if api.getCfg() != before {
		t.Fatal("a refused refresh reloads no set")
	}
}

func TestAsnResolveReloadsTheSetsUsingAChangedASN(t *testing.T) {
	s := useAsnStore(t)
	api, mux := asnAPI(t, asnSet("tg", "62041"))
	swapResolve(t, func(ctx context.Context, id string, force bool) (*config.AsnInfo, error) {
		putTelegram(t, s)
		return s.Get(id), nil
	})
	rec := postJSON(t, mux, "/api/asn/resolve", map[string]any{"asn": "62041"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := api.getCfg().GetSetById("tg").Targets.IpsToMatch; !slices.Equal(got, telegramPrefixes) {
		t.Fatalf("the set now matches the resolved prefixes: %v", got)
	}
}

func TestAsnLookupPrefersTheRIPEstatOriginOverACachedAggregate(t *testing.T) {
	s := useAsnStore(t)
	_, mux := asnAPI(t)
	if err := s.Put(&config.AsnInfo{ID: "3356", Name: "LEVEL3", Prefixes: []string{"8.0.0.0/9"}, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
	calls := swapLookup(t, func(ctx context.Context, ip string) (*asnprefix.Lookup, error) {
		entry := asnprefix.LookupASN{ID: "15169", Name: "GOOGLE"}
		if info := config.Asns().Get("15169"); info != nil {
			entry.Cached = len(info.Prefixes) > 0
		}
		return &asnprefix.Lookup{IP: ip, Prefix: "8.8.8.0/24", ASNs: []asnprefix.LookupASN{entry}}, nil
	})

	rec := getJSON(t, mux, "/api/asn/lookup?ip=8.8.8.8")
	var out asnprefix.Lookup
	decodeInto(t, rec, &out)
	if rec.Code != http.StatusOK || *calls != 1 || out.Prefix != "8.8.8.0/24" || len(out.ASNs) != 1 || out.ASNs[0] != (asnprefix.LookupASN{ID: "15169", Name: "GOOGLE"}) {
		t.Fatalf("the announcing origin is reported, not the cached transit aggregate: %d %+v", rec.Code, out)
	}

	if err := s.Put(&config.AsnInfo{ID: "15169", Name: "GOOGLE", Prefixes: []string{"8.8.8.0/24"}, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
	rec = getJSON(t, mux, "/api/asn/lookup?ip=8.8.8.8")
	decodeInto(t, rec, &out)
	if *calls != 2 || len(out.ASNs) != 1 || !out.ASNs[0].Cached {
		t.Fatalf("a cached origin is flagged as cached: %+v", out)
	}
}

func TestAsnLookupAnswersFromTheStoreWhenRIPEstatFails(t *testing.T) {
	s := useAsnStore(t)
	_, mux := asnAPI(t)
	putTelegram(t, s)
	calls := swapLookup(t, func(ctx context.Context, ip string) (*asnprefix.Lookup, error) {
		return nil, errors.New("RIPEstat network-info: dial tcp: i/o timeout")
	})

	rec := getJSON(t, mux, "/api/asn/lookup?ip=149.154.167.51:443")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out asnprefix.Lookup
	decodeInto(t, rec, &out)
	if out.IP != "149.154.167.51" || out.Prefix != "149.154.160.0/20" || len(out.ASNs) != 1 || out.ASNs[0] != (asnprefix.LookupASN{ID: "62041", Name: "Telegram", Cached: true}) {
		t.Fatalf("store answer: %+v", out)
	}
	if *calls != 1 {
		t.Errorf("RIPEstat is asked first: %d", *calls)
	}
	expectCode(t, getJSON(t, mux, "/api/asn/lookup?ip=142.250.120.139"), http.StatusBadGateway, "asn_lookup_failed")
}

func TestAsnLookupFallsBackToTheStoreWhenRIPEstatHangs(t *testing.T) {
	s := useAsnStore(t)
	_, mux := asnAPI(t)
	putTelegram(t, s)
	prev := asnLookupTimeout
	asnLookupTimeout = 50 * time.Millisecond
	t.Cleanup(func() { asnLookupTimeout = prev })
	swapLookup(t, func(ctx context.Context, ip string) (*asnprefix.Lookup, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	start := time.Now()
	rec := getJSON(t, mux, "/api/asn/lookup?ip=149.154.167.51")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("lookup waited %v for a hung RIPEstat", elapsed)
	}
	var out asnprefix.Lookup
	decodeInto(t, rec, &out)
	if rec.Code != http.StatusOK || len(out.ASNs) != 1 || out.ASNs[0].ID != "62041" || !out.ASNs[0].Cached {
		t.Fatalf("store answer after the lookup deadline: %d %+v", rec.Code, out)
	}
	expectCode(t, getJSON(t, mux, "/api/asn/lookup?ip=142.250.120.139"), http.StatusBadGateway, "asn_lookup_failed")
}

func TestAsnLookupRelaysRIPEstat(t *testing.T) {
	useAsnStore(t)
	_, mux := asnAPI(t)
	var fail bool
	calls := swapLookup(t, func(ctx context.Context, ip string) (*asnprefix.Lookup, error) {
		if fail {
			return nil, errors.New("RIPEstat network-info: dial tcp: i/o timeout")
		}
		return &asnprefix.Lookup{IP: ip, Prefix: "142.250.120.0/24", ASNs: []asnprefix.LookupASN{{ID: "15169", Name: "GOOGLE - Google LLC"}}}, nil
	})

	rec := getJSON(t, mux, "/api/asn/lookup?ip=142.250.120.139")
	var out asnprefix.Lookup
	decodeInto(t, rec, &out)
	if rec.Code != http.StatusOK || *calls != 1 || out.Prefix != "142.250.120.0/24" || out.ASNs[0].Cached {
		t.Fatalf("upstream answer relayed: %d %+v", rec.Code, out)
	}

	fail = true
	expectCode(t, getJSON(t, mux, "/api/asn/lookup?ip=142.250.120.139"), http.StatusBadGateway, "asn_lookup_failed")
	expectCode(t, getJSON(t, mux, "/api/asn/lookup?ip=example.com"), http.StatusBadRequest, "ip_invalid")
	expectCode(t, getJSON(t, mux, "/api/asn/lookup"), http.StatusBadRequest, "ip_invalid")
}

func TestAsnDeleteRefusesAnASNASetUses(t *testing.T) {
	s := useAsnStore(t)
	_, mux := asnAPI(t, asnSet("tg", "62041"), asnSet("off", "AS62041"))
	putTelegram(t, s)
	_ = s.Put(&config.AsnInfo{ID: "13335", Name: "Cloudflare", Prefixes: []string{"104.16.0.0/13"}})

	rec := serve(mux, http.MethodDelete, "/api/asn?id=AS62041", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body AsnInUseError
	decodeInto(t, rec, &body)
	if body.Code != "asn_in_use" || !slices.Equal(body.UsedBy, []string{"Set tg", "Set off"}) || body.Message == "" {
		t.Fatalf("409 names every referencing set: %+v", body)
	}
	if s.Get("62041") == nil {
		t.Fatal("a referenced ASN is kept")
	}

	rec = serve(mux, http.MethodDelete, "/api/asn?id=13335", "")
	if rec.Code != http.StatusOK || s.Get("13335") != nil {
		t.Fatalf("an unused ASN is deleted: %d %s", rec.Code, rec.Body.String())
	}
	expectCode(t, serve(mux, http.MethodDelete, "/api/asn?id=nope", ""), http.StatusBadRequest, "asn_invalid")
}

func TestAsnPutAndTheRIPEstatRelaysAreGone(t *testing.T) {
	useAsnStore(t)
	_, mux := asnAPI(t)
	if rec := serve(mux, http.MethodPut, "/api/asn", `{"id":"15169","prefixes":["8.8.8.0/24"]}`); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("the browser can no longer write prefixes: %d", rec.Code)
	}
	for _, path := range []string{"/api/integration/ripestat?ip=1.1.1.1", "/api/integration/ripestat/asn?asn=13335"} {
		if rec := getJSON(t, mux, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s is removed: %d", path, rec.Code)
		}
	}
}

func TestAsnListIncludesReferencedASNsNotResolvedYet(t *testing.T) {
	s := useAsnStore(t)
	_, mux := asnAPI(t, asnSet("tg", "62041", "15169"))
	putTelegram(t, s)
	_ = s.Put(&config.AsnInfo{ID: "13335", Name: "Cloudflare", Prefixes: []string{"104.16.0.0/13"}})

	var out map[string]AsnView
	decodeInto(t, getJSON(t, mux, "/api/asn"), &out)
	if len(out) != 3 {
		t.Fatalf("store entries plus referenced ones: %v", out)
	}
	if v := out["62041"]; v.AsnCounts.Prefixes != 4 || !slices.Equal(v.UsedBy, []string{"Set tg"}) {
		t.Errorf("stored and used: %+v", v)
	}
	if v := out["13335"]; v.AsnCounts.Prefixes != 1 || len(v.UsedBy) != 0 || v.UsedBy == nil {
		t.Errorf("stored, unused: %+v", v)
	}
	if v := out["15169"]; v.ID != "15169" || len(v.Prefixes) != 0 || v.Prefixes == nil || v.UpdatedAt != 0 || !slices.Equal(v.UsedBy, []string{"Set tg"}) {
		t.Errorf("referenced but unresolved: %+v", v)
	}
}

func TestReloadASNTargetsReExpandsOnlyTheAffectedSets(t *testing.T) {
	s := useAsnStore(t)
	tg := asnSet("tg", "62041")
	tg.TCP.Duplicate.Enabled = true
	tg.TCP.Duplicate.Count = 2
	if err := s.Put(&config.AsnInfo{ID: "15169", Name: "Google", Prefixes: []string{"8.8.8.0/24"}, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
	google := asnSet("google", "15169")
	google.Targets.IpsToMatch = []string{"8.8.8.0/24"}
	plain := asnSet("plain")
	plain.Targets.IPs = []string{"198.51.100.0/24"}
	plain.Targets.IpsToMatch = []string{"198.51.100.0/24"}
	api, _ := asnAPI(t, tg, google, plain)
	refreshed := countRefreshes(t)
	putTelegram(t, s)

	api.ReloadASNTargets([]string{"62041"})
	cfg := api.getCfg()
	if got := cfg.GetSetById("tg").Targets.IpsToMatch; !slices.Equal(got, telegramPrefixes) {
		t.Fatalf("the set using the changed ASN is re-expanded: %v", got)
	}
	if got := cfg.GetSetById("google").Targets.IpsToMatch; !slices.Equal(got, []string{"8.8.8.0/24"}) {
		t.Errorf("a set using another ASN is left alone: %v", got)
	}
	if got := cfg.GetSetById("plain").Targets.IpsToMatch; !slices.Equal(got, []string{"198.51.100.0/24"}) {
		t.Errorf("a set without ASNs is left alone: %v", got)
	}
	if *refreshed != 1 {
		t.Errorf("the duplicate ipset changed, so the firewall is refreshed: %d", *refreshed)
	}

	before := api.getCfg()
	api.ReloadASNTargets([]string{"13335", "bogus"})
	api.ReloadASNTargets(nil)
	if api.getCfg() != before || *refreshed != 1 {
		t.Error("an ASN no set references changes nothing")
	}
}

func TestLoadTargetsForSetCachedExpandsASNsLikeTheCompiler(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	api, _ := asnAPI(t)

	for _, version := range []string{"", "4", "6"} {
		set := asnSet("tg", "62041")
		set.Targets.IPs = []string{"198.51.100.7"}
		set.Targets.IPVersion = version
		twin := *set
		twin.Targets.ASNs = append([]string{}, set.Targets.ASNs...)
		cfg := config.NewConfig()
		_, want, err := cfg.GetTargetsForSetWithCache(&twin, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		report := api.loadTargetsForSetCached(set)
		if !slices.Equal(set.Targets.IpsToMatch, want) {
			t.Errorf("ip_version %q: handler %v, compiler %v", version, set.Targets.IpsToMatch, want)
		}
		if report.ASNIPs != len(want)-1 || report.IPs != len(want) {
			t.Errorf("ip_version %q: report %+v", version, report)
		}
	}
	drainASNRefresh()

	set := asnSet("x", "62041", "AS15169")
	report := api.loadTargetsForSetCached(set)
	if !slices.Equal(report.UnresolvedASNs, []string{"15169"}) {
		t.Errorf("unresolved ASNs are reported: %+v", report)
	}
	if !drainASNRefresh() {
		t.Error("an unresolved ASN asks the refresher to resolve it")
	}
}

func TestInitializeSetDefaultsGivesAsnsAnEmptyList(t *testing.T) {
	api, _ := asnAPI(t)
	var set config.SetConfig
	api.initializeSetDefaults(&set)
	if set.Targets.ASNs == nil {
		t.Fatal("asns defaults to an empty list, never null")
	}
}

func TestConfigStatisticsCountASNPrefixes(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	tg := asnSet("tg", "62041", "15169")
	tg.Targets.IPs = []string{"198.51.100.7"}
	v4 := asnSet("v4", "62041")
	v4.Targets.IPVersion = "4"
	_, mux := asnAPI(t, tg, v4)

	var resp struct {
		Sets []struct {
			ID    string        `json:"id"`
			Stats SetStatistics `json:"stats"`
		} `json:"sets"`
	}
	decodeInto(t, getJSON(t, mux, "/api/config"), &resp)
	stats := map[string]SetStatistics{}
	for _, s := range resp.Sets {
		stats[s.ID] = s.Stats
	}
	got := stats["tg"]
	if got.ASNIPs != 4 || got.TotalIPs != 5 || got.ManualIPs != 1 {
		t.Errorf("asn prefixes count toward the IP total: %+v", got)
	}
	if got.ASNBreakdown["62041"] != 4 || !slices.Equal(got.ASNUnresolved, []string{"15169"}) {
		t.Errorf("breakdown and unresolved: %+v", got)
	}
	if got := stats["v4"]; got.ASNIPs != 3 || got.ASNBreakdown["62041"] != 3 || got.ASNUnresolved != nil {
		t.Errorf("ip_version filters the count: %+v", got)
	}
}

func TestUpdateConfigResponseCarriesGeoipAndASNStatistics(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	api, mux := asnAPI(t, asnSet("tg", "62041"))
	raw := map[string]any{}
	data, _ := json.Marshal(api.getCfg())
	_ = json.Unmarshal(data, &raw)
	body, _ := json.Marshal(raw)

	rec := serve(mux, http.MethodPut, "/api/config", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Sets []struct {
			Stats map[string]any `json:"stats"`
		} `json:"sets"`
	}
	decodeInto(t, rec, &resp)
	st := resp.Sets[0].Stats
	if _, ok := st["geoip_ips"]; !ok {
		t.Errorf("geoip_ips is filled on PUT too: %v", st)
	}
	if st["asn_ips"] != float64(4) || st["total_ips"] != float64(4) {
		t.Errorf("asn stats on PUT: %v", st)
	}
	if got := api.getCfg().GetSetById("tg").Targets.IpsToMatch; !slices.Equal(got, telegramPrefixes) {
		t.Errorf("PUT /config expands ASNs: %v", got)
	}
}

func TestDiagnosticsReportASNTargets(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	api, _ := asnAPI(t, asnSet("tg", "62041", "15169"))
	info := api.collectGeodataInfo()
	if info.TotalIPs != 4 {
		t.Errorf("asn prefixes count toward total_ips: %d", info.TotalIPs)
	}
	if info.ASN == nil || info.ASN.Cached != 1 || len(info.ASN.Referenced) != 2 || !slices.Equal(info.ASN.Unresolved, []string{"15169"}) {
		t.Fatalf("asn section: %+v", info.ASN)
	}
	if r := info.ASN.Referenced[1]; r.ID != "62041" || r.Prefixes != 4 || r.Name != "Telegram" || !slices.Equal(r.Sets, []string{"Set tg"}) {
		t.Errorf("referenced entry: %+v", r)
	}

	empty, _ := asnAPI(t)
	config.InitAsnStore("")
	if empty.collectGeodataInfo().ASN != nil {
		t.Error("no ASN section when there is nothing to report")
	}
}

func TestTraceTargetsListASNsAndUnresolvedOnes(t *testing.T) {
	s := useAsnStore(t)
	putTelegram(t, s)
	got := traceTargets(asnSet("tg", "62041", "15169").Targets)
	if !slices.Equal(got.ASNs, []string{"62041", "15169"}) || !slices.Equal(got.ASNsUnresolved, []string{"15169"}) {
		t.Fatalf("trace: %+v", got)
	}
	if plain := traceTargets(asnSet("x").Targets); plain.ASNs != nil && len(plain.ASNs) != 0 || plain.ASNsUnresolved != nil {
		t.Errorf("nothing for a set without ASNs: %+v", plain)
	}
}
