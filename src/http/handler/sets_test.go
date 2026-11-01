package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkDomainAPI(t *testing.T, sets []*config.SetConfig) *http.ServeMux {
	t.Helper()

	cfg := config.NewConfig()
	cfg.Sets = sets

	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSetsApi()

	return mux
}

func checkDomain(t *testing.T, mux *http.ServeMux, query string) []SetDomainMatch {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/sets/check-domain?"+query, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var matches []SetDomainMatch
	if err := json.Unmarshal(rec.Body.Bytes(), &matches); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if matches == nil {
		t.Fatal("expected an array, got null")
	}

	return matches
}

func TestHandleCheckDomain(t *testing.T) {
	manual := &config.SetConfig{Id: "manual-set", Name: "Manual", Enabled: true}
	manual.Targets.SNIDomains = []string{"example.com"}
	manual.Targets.DomainsToMatch = []string{"example.com"}

	geo := &config.SetConfig{Id: "geo-set", Name: "Geo", Enabled: true}
	geo.Targets.DomainsToMatch = []string{"cdn.example.org", `regexp:^video-[0-9]+\.example\.net$`}

	disabled := &config.SetConfig{Id: "disabled-set", Name: "Disabled", Enabled: false}
	disabled.Targets.SNIDomains = []string{"blocked.test"}
	disabled.Targets.DomainsToMatch = []string{"blocked.test"}

	mux := checkDomainAPI(t, []*config.SetConfig{manual, geo, disabled})

	t.Run("exact match", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=Example.com")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].SetId != "manual-set" || matches[0].Relation != "exact" || matches[0].Via != "manual" {
			t.Errorf("unexpected match: %+v", matches[0])
		}
		if !matches[0].Enabled {
			t.Error("expected enabled set")
		}
	})

	t.Run("subdomain reported as covered", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=www.example.com")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Relation != "covered" || matches[0].Entry != "example.com" {
			t.Errorf("unexpected match: %+v", matches[0])
		}
	})

	t.Run("parent domain reported as covers", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=example.org")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].SetId != "geo-set" || matches[0].Relation != "covers" || matches[0].Via != "geosite" {
			t.Errorf("unexpected match: %+v", matches[0])
		}
	})

	t.Run("regexp entry", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=video-12.example.net")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Relation != "regexp" || matches[0].Entry != `regexp:^video-[0-9]+\.example\.net$` {
			t.Errorf("unexpected match: %+v", matches[0])
		}
	})

	t.Run("disabled set is reported as disabled", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=blocked.test")
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
		if matches[0].Enabled {
			t.Error("expected disabled set")
		}
	})

	t.Run("exclude skips a set", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=www.example.com&exclude=manual-set")
		if len(matches) != 0 {
			t.Fatalf("expected no matches, got %+v", matches)
		}
	})

	t.Run("no match returns empty array", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=unrelated.test")
		if len(matches) != 0 {
			t.Fatalf("expected no matches, got %+v", matches)
		}
	})

	t.Run("several domains in one request", func(t *testing.T) {
		matches := checkDomain(t, mux, "domain=www.example.com%2Cunrelated.test+cdn.example.org")
		if len(matches) != 2 {
			t.Fatalf("expected 2 matches, got %+v", matches)
		}
		if matches[0].Domain != "www.example.com" || matches[0].Relation != "covered" {
			t.Errorf("unexpected first match: %+v", matches[0])
		}
		if matches[1].Domain != "cdn.example.org" || matches[1].Relation != "exact" {
			t.Errorf("unexpected second match: %+v", matches[1])
		}
	})

	t.Run("missing domain is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sets/check-domain?domain=+", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/sets/check-domain", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}

func TestParseCheckDomains(t *testing.T) {
	got, truncated := parseCheckDomains(" A.com, b.com | c.com;\td.com. a.com ")
	want := []string{"a.com", "b.com", "c.com", "d.com"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if truncated {
		t.Error("four domains must not report truncation")
	}

	if blank, _ := parseCheckDomains("   "); len(blank) != 0 {
		t.Error("expected no domains for blank input")
	}
}

func TestParseCheckDomainsKeepsSeparatorsInsideAURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"https://example.com/search?q=one,two", []string{"example.com"}},
		{"https://example.com/a;b=c;d=e", []string{"example.com"}},
		{"https://example.com/?x=a|b", []string{"example.com"}},
		{"https://us,er:p;w@example.com/x,y", []string{"example.com"}},
		{"https://example.com/search?q=one,,two;three", []string{"example.com"}},
		{"https://a.com/x,https://b.com/y", []string{"a.com", "b.com"}},
		{"youtube.com https://example.com/q?a=1,2 ntc.party", []string{"youtube.com", "example.com", "ntc.party"}},
		{"youtube.com,https://example.com/q?a=1;2", []string{"youtube.com", "example.com"}},
		{"https://example.com,", []string{"example.com"}},
		{"a.com, b.com | c.com;d.com", []string{"a.com", "b.com", "c.com", "d.com"}},
		{"https://a.com,b.com", []string{"a.com", "b.com"}},
		{"\"https://example.com/a,b\"", []string{"example.com"}},
		{"'youtube.com'", []string{"youtube.com"}},
	} {
		got, _ := parseCheckDomains(tc.in)
		if !slices.Equal(got, tc.want) {
			t.Errorf("parseCheckDomains(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseCheckDomainsReportsTruncation(t *testing.T) {
	raw := make([]string, 0, maxCheckDomains+5)
	for i := 0; i < maxCheckDomains+5; i++ {
		raw = append(raw, fmt.Sprintf("d%d.example", i))
	}

	got, truncated := parseCheckDomains(strings.Join(raw, ","))
	if len(got) != maxCheckDomains {
		t.Fatalf("got %d domains, want %d", len(got), maxCheckDomains)
	}
	if !truncated {
		t.Error("dropping domains past the cap must be reported")
	}

	exact, truncated := parseCheckDomains(strings.Join(raw[:maxCheckDomains], ","))
	if len(exact) != maxCheckDomains {
		t.Fatalf("got %d domains, want %d", len(exact), maxCheckDomains)
	}
	if truncated {
		t.Error("a list that exactly fills the cap must not report truncation")
	}
}

func TestParseCheckDomainsIgnoresNamesLongerThanDNSAllows(t *testing.T) {
	longest := strings.Repeat("a", 249) + ".com"
	tooLong := strings.Repeat("a", 250) + ".com"

	got, truncated := parseCheckDomains(strings.Repeat("a", 300) + ".com, youtube.com " + tooLong + " " + longest + ".")
	if len(got) != 2 || got[0] != "youtube.com" || got[1] != longest {
		t.Fatalf("got %v, want youtube.com and the 253-byte name only", got)
	}
	if truncated {
		t.Error("an ignored name is not a dropped one: truncation must stay false")
	}

	if only, _ := parseCheckDomains(tooLong); len(only) != 0 {
		t.Errorf("a 254-byte name must be ignored, got %v", only)
	}
}

func TestCheckDomainOverlongNameCostsNothing(t *testing.T) {
	const entries = 200000
	big := &config.SetConfig{Id: "big", Name: "big", Enabled: true}
	big.Targets.DomainsToMatch = make([]string, 0, entries)
	for i := 0; i < entries; i++ {
		big.Targets.DomainsToMatch = append(big.Targets.DomainsToMatch, fmt.Sprintf("d%d.example", i))
	}
	long := strings.Repeat("a", 300) + ".com"

	allocated := func(call func()) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		call()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	t.Run("rest", func(t *testing.T) {
		mux := checkDomainAPI(t, []*config.SetConfig{big})
		rec := httptest.NewRecorder()
		grew := allocated(func() {
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sets/check-domain?domain="+long, nil))
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("a request whose only name is longer than DNS allows must be refused, got %d", rec.Code)
		}
		if grew > 1<<20 {
			t.Errorf("checking a %d-byte name against %d entries allocated %d bytes", len(long), entries, grew)
		}
	})

	t.Run("mcp", func(t *testing.T) {
		cfg := mcpTestCfg()
		cfg.Sets = append(cfg.Sets, big)
		session, ctx := connectMCP(t, newMCPTestServer(t, cfg))
		var res *mcp.CallToolResult
		var err error
		grew := allocated(func() {
			res, err = session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "b4_check_domain",
				Arguments: map[string]any{"domain": long},
			})
		})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		if grew > 8<<20 {
			t.Errorf("b4_check_domain with a %d-byte name against %d entries allocated %d bytes", len(long), entries, grew)
		}
		if !res.IsError {
			var out mcpCheckDomainOut
			if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(out.Checked) != 0 {
				t.Errorf("a name longer than DNS allows must not be matched against the sets, checked %d names", len(out.Checked))
			}
		}
	})
}

func TestParseCheckDomainsReducesURLsToTheirHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "www.youtube.com"},
		{"HTTP://YouTube.com", "youtube.com"},
		{"https://user:pw@Example.com:8443/a", "example.com"},
		{"www.youtube.com:443", "www.youtube.com"},
		{"youtube.com/shorts", "youtube.com"},
		{"youtube.com?x=1", "youtube.com"},
		{"youtube.com.", "youtube.com"},
		{"1.2.3.4:443", "1.2.3.4"},
		{"2001:db8::1", "2001:db8::1"},
		{"[2001:db8::1]", "2001:db8::1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"https://[2001:db8::1]:8443/x", "2001:db8::1"},
		{"wss://gateway.discord.gg", "gateway.discord.gg"},
		{"tls://dns.google", "dns.google"},
		{"quic://dns.adguard.com:853", "dns.adguard.com"},
		{"ftp://ntc.party/", "ntc.party"},
		{"ntc.party#frag", "ntc.party"},
		{"google.com/url?q=https://rutracker.org", "google.com"},
		{"www.youtube.com:443/watch?next=https://youtu.be/x", "www.youtube.com"},
		{"vk.com/away.php?to=http://ntc.party", "vk.com"},
	} {
		if got, _ := parseCheckDomains(tc.in); len(got) != 1 || got[0] != tc.want {
			t.Errorf("parseCheckDomains(%q) = %v, want [%s]", tc.in, got, tc.want)
		}
	}

	for _, in := range []string{"https://", "wss://"} {
		if got, _ := parseCheckDomains(in); len(got) != 0 {
			t.Errorf("a URL without a host names no domain, so %q must check nothing, got %v", in, got)
		}
	}

	if got, _ := parseCheckDomains("https://youtube.com/a youtube.com:443 youtube.com"); len(got) != 1 {
		t.Errorf("one host written three ways must be checked once, got %v", got)
	}
}

func TestHandleCheckDomainMatchesTheHostOfAURL(t *testing.T) {
	set := &config.SetConfig{Id: "manual-set", Name: "Manual", Enabled: true}
	set.Targets.SNIDomains = []string{"example.com"}
	set.Targets.DomainsToMatch = []string{"example.com"}
	mux := checkDomainAPI(t, []*config.SetConfig{set})

	for _, input := range []string{"https://www.example.com/watch?v=1", "www.example.com:443"} {
		matches := checkDomain(t, mux, "domain="+url.QueryEscape(input))
		if len(matches) != 1 || matches[0].Domain != "www.example.com" || matches[0].Relation != "covered" {
			t.Errorf("%q should be checked as www.example.com and found covered, got %+v", input, matches)
		}
	}
}

func TestCreateSetPutsTheNewSetRightAfterTheSetNamedByAfter(t *testing.T) {
	cases := []struct {
		name, query string
		want        []string
	}{
		{"after a set in the middle", "?after=b", []string{"a", "b", "new", "c"}},
		{"after the last set", "?after=c", []string{"a", "b", "c", "new"}},
		{"after a set that does not exist", "?after=missing", []string{"new", "a", "b", "c"}},
		{"without the parameter", "", []string{"new", "a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sets := make([]*config.SetConfig, 0, 3)
			for _, id := range []string{"a", "b", "c"} {
				set := config.NewSetConfig()
				set.Id, set.Name, set.Enabled = id, id, true
				set.Targets.SNIDomains = []string{id + ".example.org"}
				sets = append(sets, &set)
			}
			api, mux := asnAPI(t, sets...)

			rec := serve(mux, http.MethodPost, "/api/sets"+tc.query, `{"name":"new","enabled":true,"targets":{"sni_domains":["new.example.org"]}}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("POST /api/sets%s = %d %s", tc.query, rec.Code, rec.Body.String())
			}
			var got []string
			for _, set := range api.getCfg().Sets {
				got = append(got, set.Name)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("order after POST /api/sets%s = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestAWebDuplicatePlacedAfterAnIPPrioritySetLeavesTheSourceHolding(t *testing.T) {
	ggc := config.NewSetConfig()
	ggc.Id, ggc.Name, ggc.Enabled = "ggc", "YT-GGC-RU", true
	ggc.Targets.IPs = []string{"37.18.153.136/29"}
	ggc.Targets.IPPriority = true
	gw := config.NewSetConfig()
	gw.Id, gw.Name, gw.Enabled = "gw", "YT-GW-AS15169", true
	gw.Targets.SNIDomains = []string{"googlevideo.com"}
	api, mux := asnAPI(t, &ggc, &gw)

	var body map[string]any
	if err := json.Unmarshal(serve(mux, http.MethodGet, "/api/sets/ggc", "").Body.Bytes(), &body); err != nil {
		t.Fatalf("GET /api/sets/ggc: %v", err)
	}
	delete(body, "id")
	delete(body, "revision")
	body["targets"].(map[string]any)["ip_priority"] = false
	body["name"] = "YT-GGC-RU (copy)"
	raw, _ := json.Marshal(body)
	if rec := serve(mux, http.MethodPost, "/api/sets?after=ggc", string(raw)); rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/sets?after=ggc = %d %s", rec.Code, rec.Body.String())
	}

	cfg := api.getCfg().Clone()
	var order []string
	for _, set := range cfg.Sets {
		set.Targets.IpsToMatch = set.Targets.IPs
		set.Targets.DomainsToMatch = set.Targets.SNIDomains
		order = append(order, set.Name)
	}
	if want := []string{"YT-GGC-RU", "YT-GGC-RU (copy)", "YT-GW-AS15169"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	m := sni.NewSuffixSet(cfg.Sets)
	_, addr, prefix := m.MatchIPWithSourceDetail(net.ParseIP("37.18.153.140"), "")
	_, name := m.MatchSNIWithSource("rr1---sn-nxu-nufl.googlevideo.com", "")
	if addr == nil || addr.Id != "ggc" || !m.AddressHolds(addr, prefix, name, "") {
		t.Errorf("the source must stay the address winner and keep holding against the name set, got winner %v", addr)
	}
}
