package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/proto"
)

func writeTestGeoSite(t *testing.T) string {
	t.Helper()
	b, err := proto.Marshal(&v2data.GeoSiteList{Entry: []*v2data.GeoSite{
		{CountryCode: "GOOGLE", Domain: []*v2data.Domain{
			{Type: v2data.Domain_Domain, Value: "google.com"},
			{Type: v2data.Domain_Domain, Value: "youtube.com"},
			{Type: v2data.Domain_Full, Value: "www.google.com"},
		}},
		{CountryCode: "YOUTUBE", Domain: []*v2data.Domain{
			{Type: v2data.Domain_Domain, Value: "youtube.com"},
			{Type: v2data.Domain_Domain, Value: "ytimg.com"},
		}},
		{CountryCode: "NETFLIX", Domain: []*v2data.Domain{
			{Type: v2data.Domain_Domain, Value: "netflix.com"},
		}},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func writeTestGeoIP(t *testing.T) string {
	t.Helper()
	b, err := proto.Marshal(&v2data.GeoIPList{Entry: []*v2data.GeoIP{
		{CountryCode: "CLOUDFLARE", Cidr: []*v2data.CIDR{{Ip: []byte{104, 16, 0, 0}, Prefix: 13}}},
		{CountryCode: "RU", Cidr: []*v2data.CIDR{{Ip: []byte{95, 173, 128, 0}, Prefix: 18}}},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func geoTestCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg := mcpTestCfg()
	cfg.System.Geo.GeoSitePath = writeTestGeoSite(t)
	cfg.System.Geo.GeoIpPath = writeTestGeoIP(t)
	return cfg
}

func callGeo(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) mcpGeoOut {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_geo_lookup", Arguments: args})
	if err != nil {
		t.Fatalf("call b4_geo_lookup %v: %v", args, err)
	}
	if res.IsError {
		t.Fatalf("b4_geo_lookup %v returned an error: %+v", args, res.Content)
	}
	var out mcpGeoOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMCPGeoStatus(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.Sets[0].Targets.GeoSiteCategories = []string{"youtube"}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := callGeo(t, session, ctx, map[string]any{"action": "status"})
	if len(out.Databases) != 2 {
		t.Fatalf("expected both databases reported, got %+v", out.Databases)
	}
	for _, db := range out.Databases {
		if !db.Installed {
			t.Errorf("%s should be reported installed", db.Kind)
		}
		if db.Categories == 0 {
			t.Errorf("%s should report its category count", db.Kind)
		}
		if db.Kind == "geosite" && db.InUse != 1 {
			t.Errorf("geosite categories_used_by_sets = %d, want 1", db.InUse)
		}
	}
}

func TestMCPGeoStatusWithoutDatabase(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	session, ctx := connectMCP(t, srv)

	out := callGeo(t, session, ctx, map[string]any{"action": "status"})
	for _, db := range out.Databases {
		if db.Installed {
			t.Errorf("%s must not report as installed when no path is set", db.Kind)
		}
	}
	if !strings.Contains(out.Note, "neither the geosite nor the geoip database is installed") {
		t.Errorf("note should explain the empty result: %q", out.Note)
	}
}

func TestMCPGeoListAndPreview(t *testing.T) {
	srv := newMCPTestServer(t, geoTestCfg(t))
	session, ctx := connectMCP(t, srv)

	all := callGeo(t, session, ctx, map[string]any{"action": "list"})
	if all.Total != 3 {
		t.Fatalf("expected 3 geosite categories, got %d (%v)", all.Total, all.Categories)
	}

	filtered := callGeo(t, session, ctx, map[string]any{"action": "list", "contains": "tube"})
	if filtered.Total != 1 || filtered.Categories[0] != "youtube" {
		t.Errorf("contains filter failed: %+v", filtered.Categories)
	}

	ip := callGeo(t, session, ctx, map[string]any{"action": "list", "kind": "geoip"})
	if ip.Total != 2 {
		t.Errorf("expected 2 geoip categories, got %d", ip.Total)
	}

	prev := callGeo(t, session, ctx, map[string]any{"action": "preview", "category": "google", "limit": 2})
	if prev.Total != 3 {
		t.Errorf("preview must report the real total, got %d", prev.Total)
	}
	if len(prev.Entries) != 2 || !prev.Truncated {
		t.Errorf("preview should honour the limit and flag truncation: %+v", prev)
	}

	ipPrev := callGeo(t, session, ctx, map[string]any{"action": "preview", "kind": "geoip", "category": "cloudflare"})
	if ipPrev.Total != 1 || ipPrev.Entries[0] != "104.16.0.0/13" {
		t.Errorf("geoip preview = %+v", ipPrev)
	}

	missing := callGeo(t, session, ctx, map[string]any{"action": "preview", "category": "nope"})
	if missing.Total != 0 {
		t.Errorf("unknown category should be empty, got %d", missing.Total)
	}
	if !strings.Contains(missing.Note, "silently matches nothing") {
		t.Errorf("note must explain that an unknown category is accepted silently: %q", missing.Note)
	}
}

func TestMCPGeoFindDomain(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.Sets[0].Targets.GeoSiteCategories = []string{"youtube"}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := callGeo(t, session, ctx, map[string]any{"action": "find_domain", "domain": "youtube.com"})
	got := map[string]string{}
	for _, m := range out.Matches {
		got[m.Category] = m.Relation
	}
	if got["youtube"] != "exact" || got["google"] != "exact" {
		t.Fatalf("youtube.com should be found in both categories: %+v", out.Matches)
	}
	if len(got) != 2 {
		t.Errorf("netflix must not match youtube.com: %+v", out.Matches)
	}
	for _, m := range out.Matches {
		if m.Category == "youtube" && m.UsedBy != "video" {
			t.Errorf("a category already selected should name the set: %+v", m)
		}
	}

	sub := callGeo(t, session, ctx, map[string]any{"action": "find_domain", "domain": "www.youtube.com"})
	if len(sub.Matches) == 0 || sub.Matches[0].Relation != "covered" {
		t.Errorf("subdomain should report a covered relation: %+v", sub.Matches)
	}

	none := callGeo(t, session, ctx, map[string]any{"action": "find_domain", "domain": "example.org"})
	if len(none.Matches) != 0 {
		t.Errorf("example.org should match nothing: %+v", none.Matches)
	}
	if !strings.Contains(none.Note, "sni_domains") {
		t.Errorf("an empty result should point at the alternative: %q", none.Note)
	}
}

func TestMCPGeoLookupIP(t *testing.T) {
	srv := newMCPTestServer(t, geoTestCfg(t))
	session, ctx := connectMCP(t, srv)

	out := callGeo(t, session, ctx, map[string]any{"action": "lookup_ip", "ip": "104.22.45.1"})
	if len(out.Matches) != 1 || out.Matches[0].Category != "cloudflare" {
		t.Fatalf("104.22.45.1 should be in cloudflare: %+v", out.Matches)
	}
	if out.Matches[0].Entry != "104.16.0.0/13" {
		t.Errorf("the containing prefix should be reported: %+v", out.Matches[0])
	}

	none := callGeo(t, session, ctx, map[string]any{"action": "lookup_ip", "ip": "8.8.8.8"})
	if len(none.Matches) != 0 {
		t.Errorf("8.8.8.8 should match nothing: %+v", none.Matches)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_geo_lookup",
		Arguments: map[string]any{"action": "lookup_ip", "ip": "not-an-ip"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Error("a malformed address must be a tool error")
	}
}

func TestMCPGeoRejectsUnknownAction(t *testing.T) {
	srv := newMCPTestServer(t, geoTestCfg(t))
	session, ctx := connectMCP(t, srv)

	for _, args := range []map[string]any{
		{"action": "delete_everything"},
		{},
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_geo_lookup", Arguments: args})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		if !res.IsError {
			t.Errorf("%v should be refused", args)
		}
	}
}

func TestMCPGeoIsReadOnly(t *testing.T) {
	srv := newMCPTestServer(t, geoTestCfg(t))
	session, ctx := connectMCP(t, srv)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "b4_geo_lookup" {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Error("b4_geo_lookup must be annotated read-only")
		}
		return
	}
	t.Fatal("b4_geo_lookup not registered")
}

func TestGeoScanSentinelIsExported(t *testing.T) {
	if geodat.ErrStopScan == nil {
		t.Fatal("geodat.ErrStopScan must be usable by callers that bound a scan")
	}
}

func TestMCPGeoRejectsUnknownKind(t *testing.T) {
	srv := newMCPTestServer(t, geoTestCfg(t))
	session, ctx := connectMCP(t, srv)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "b4_geo_lookup",
		Arguments: map[string]any{"action": "list", "kind": "geosight"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Fatal("a misspelled kind must be refused, not answered from the other database")
	}

	for _, alias := range []string{"geoip_categories", "ip"} {
		out := callGeo(t, session, ctx, map[string]any{"action": "list", "kind": alias})
		if out.Total != 2 {
			t.Errorf("kind %q should reach the geoip database, got %d categories", alias, out.Total)
		}
	}
	for _, alias := range []string{"", "geosite", "geosite_categories"} {
		out := callGeo(t, session, ctx, map[string]any{"action": "list", "kind": alias})
		if out.Total != 3 {
			t.Errorf("kind %q should reach the geosite database, got %d categories", alias, out.Total)
		}
	}
}
