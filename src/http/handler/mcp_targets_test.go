package handler

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callEditTargets(t *testing.T, session *mcp.ClientSession, ctx context.Context, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_edit_set_targets", Arguments: args})
	if err != nil {
		t.Fatalf("call b4_edit_set_targets %v: %v", args, err)
	}
	return res
}

func decodeEditTargets(t *testing.T, res *mcp.CallToolResult) mcpEditTargetsOut {
	t.Helper()
	if res.IsError {
		t.Fatalf("b4_edit_set_targets returned an error: %+v", res.Content)
	}
	var out mcpEditTargetsOut
	if err := json.Unmarshal(mustStructured(t, res), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMCPEditTargetsRequiresAllowWrites(t *testing.T) {
	cfg := geoTestCfg(t)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	if toolNames(t, session, ctx)["b4_edit_set_targets"] {
		t.Fatal("the tool must not be offered while allow_writes is off")
	}
	if len(api.getCfg().Sets[0].Targets.SNIDomains) != 1 {
		t.Error("config must be untouched when writes are disabled")
	}
}

func TestMCPEditTargetsAddsAndRemovesDomains(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "rutracker.org, example.net",
	}))
	if !out.Changed || len(out.Added) != 2 {
		t.Fatalf("expected two additions: %+v", out)
	}
	if out.EntryCount != 3 {
		t.Errorf("entry_count = %d, want 3", out.EntryCount)
	}

	live := api.getCfg().Sets[0].Targets
	if strings.Join(live.SNIDomains, ",") != "youtube.com,rutracker.org,example.net" {
		t.Fatalf("selector not saved: %v", live.SNIDomains)
	}
	if strings.Join(live.DomainsToMatch, ",") != "youtube.com,rutracker.org,example.net" {
		t.Fatalf("match list was not re-expanded: %v", live.DomainsToMatch)
	}
	if out.Expansion == nil || out.Expansion.Domains != 3 {
		t.Errorf("expansion should report 3 domains: %+v", out.Expansion)
	}
	if len(mcpHistory) != 1 {
		t.Fatalf("the edit must be undoable, history = %d", len(mcpHistory))
	}

	back := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "remove": "RUTRACKER.ORG",
	}))
	if len(back.Removed) != 1 || back.Removed[0] != "rutracker.org" {
		t.Errorf("removal should be case-insensitive: %+v", back)
	}
	if got := api.getCfg().Sets[0].Targets.DomainsToMatch; len(got) != 2 {
		t.Errorf("match list after removal = %v", got)
	}
}

func TestMCPEditTargetsReportsNoOp(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "youtube.com", "remove": "absent.example",
	}))
	if out.Changed {
		t.Errorf("nothing actually changed: %+v", out)
	}
	if len(out.AlreadySet) != 1 || len(out.NotPresent) != 1 {
		t.Errorf("both outcomes should be reported: %+v", out)
	}
	if !strings.Contains(out.Note, "already present") || !strings.Contains(out.Note, "not present") {
		t.Errorf("note should explain both: %q", out.Note)
	}
}

func TestMCPEditTargetsRewritesWildcard(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "*.example.org",
	}))
	if len(out.Added) != 1 || out.Added[0] != "example.org" {
		t.Fatalf("a wildcard entry matches nothing in b4 and must be rewritten: %+v", out)
	}
	if len(out.Rewritten) != 1 || !strings.Contains(out.Rewritten[0], "example.org") {
		t.Errorf("the rewrite must be reported, not silent: %+v", out.Rewritten)
	}
	for _, d := range api.getCfg().Sets[0].Targets.SNIDomains {
		if strings.HasPrefix(d, "*") {
			t.Errorf("a wildcard reached the config: %v", d)
		}
	}
}

func TestMCPEditTargetsMovesDomainFromOtherSet(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[1].Enabled = true
	cfg.Sets[1].Targets.SNIDomains = []string{"example.org", "keepme.test"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "example.org",
	}))
	if len(out.MovedFrom) != 1 || out.MovedFrom[0].SetName != "disabled-set" {
		t.Fatalf("the domain should have been taken from the other set: %+v", out.MovedFrom)
	}
	if !strings.Contains(out.Note, "took") {
		t.Errorf("the note must surface the move: %q", out.Note)
	}
	other := api.getCfg().Sets[1].Targets
	if strings.Join(other.SNIDomains, ",") != "keepme.test" {
		t.Errorf("only the claimed domain should be released: %v", other.SNIDomains)
	}
	if strings.Join(other.DomainsToMatch, ",") != "keepme.test" {
		t.Errorf("the donor set's match list must be re-expanded too: %v", other.DomainsToMatch)
	}
}

func TestMCPEditTargetsValidatesIPs(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	if res := callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "ip", "add": "10.0.0.0/99",
	}); !res.IsError {
		t.Error("an unparseable CIDR must be refused: nothing else in b4 validates targets.ip")
	}
	if res := callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "ip", "add": "not-an-address",
	}); !res.IsError {
		t.Error("an unparseable address must be refused")
	}

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "ip", "add": "10.1.2.3/24, 1.1.1.1",
	}))
	if len(out.Added) != 2 || out.Added[0] != "10.1.2.0/24" {
		t.Fatalf("host bits should be cleared: %+v", out.Added)
	}
	if len(out.Rewritten) != 1 {
		t.Errorf("the masking should be reported: %+v", out.Rewritten)
	}
	if got := api.getCfg().Sets[0].Targets.IpsToMatch; len(got) != 2 {
		t.Errorf("the IP match list must be re-expanded: %v", got)
	}
}

func TestMCPEditTargetsRefusesUnknownGeoCategory(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "geosite_categories", "add": "youtub",
	})
	if !res.IsError {
		t.Fatal("an unknown category is accepted everywhere else in b4 and must be refused here")
	}
	msg := strings.ToLower(mcpErrorText(res))
	if !strings.Contains(msg, "youtube") {
		t.Errorf("the refusal should suggest the near match: %q", msg)
	}
	if len(api.getCfg().Sets[0].Targets.GeoSiteCategories) != 0 {
		t.Error("the config must be untouched after a refusal")
	}

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "geosite_categories", "add": "YouTube",
	}))
	if len(out.Added) != 1 || out.Added[0] != "youtube" {
		t.Fatalf("category names are lower case: %+v", out)
	}
	if out.Expansion == nil || out.Expansion.Domains != 3 {
		t.Errorf("youtube adds 2 domains to the existing 1: %+v", out.Expansion)
	}
}

func TestMCPEditTargetsValidatesDevices(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	if res := callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "source_devices", "add": "not-a-device",
	}); !res.IsError {
		t.Error("a device that is neither a MAC nor an IP must be refused")
	}

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "source_devices", "add": "aa:bb:cc:dd:ee:ff",
	}))
	if len(out.Added) != 1 || out.Added[0] != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("MACs are stored upper case, as the matcher compares them: %+v", out)
	}
}

func TestMCPEditTargetsRejectsBadInput(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	for name, args := range map[string]map[string]any{
		"unknown kind":  {"set": "video", "kind": "domains", "add": "a.test"},
		"missing kind":  {"set": "video", "add": "a.test"},
		"missing set":   {"kind": "sni_domains", "add": "a.test"},
		"unknown set":   {"set": "nope", "kind": "sni_domains", "add": "a.test"},
		"nothing to do": {"set": "video", "kind": "sni_domains"},
	} {
		if res := callEditTargets(t, session, ctx, args); !res.IsError {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestMCPEditTargetsRevertRestoresList(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "rutracker.org",
	}))
	if len(api.getCfg().Sets[0].Targets.SNIDomains) != 2 {
		t.Fatal("precondition: the add should have landed")
	}

	rev := decodeRevert(t, session, ctx)
	if !rev.Reverted {
		t.Fatalf("a target edit must be undoable: %+v", rev)
	}
	live := api.getCfg().Sets[0].Targets
	if strings.Join(live.SNIDomains, ",") != "youtube.com" {
		t.Errorf("selector not restored: %v", live.SNIDomains)
	}
	if strings.Join(live.DomainsToMatch, ",") != "youtube.com" {
		t.Errorf("match list not restored: %v", live.DomainsToMatch)
	}
}

func mcpErrorText(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestMCPEditTargetsRefusesContradictoryAddRemove(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[1].Enabled = true
	cfg.Sets[1].Targets.SNIDomains = []string{"example.org"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "example.org", "remove": "example.org",
	})
	if !res.IsError {
		t.Fatal("the same entry in add and remove is contradictory and must be refused")
	}
	if got := api.getCfg().Sets[1].Targets.SNIDomains; len(got) != 1 || got[0] != "example.org" {
		t.Fatalf("the other set must keep its domain: %v", got)
	}
}

func TestMCPEditTargetsRemovesNonCanonicalEntry(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].Targets.SNIDomains = []string{"*.example.com", "youtube.com"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "remove": "*.example.com",
	}))
	if len(out.Removed) != 1 {
		t.Fatalf("a wildcard entry already in the list must be removable: %+v", out)
	}
	if got := api.getCfg().Sets[0].Targets.SNIDomains; len(got) != 1 || got[0] != "youtube.com" {
		t.Fatalf("live list = %v", got)
	}
}

func TestMCPEditTargetsDoesNotDuplicateNonCanonicalEntry(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].Targets.SNIDomains = []string{"*.example.com"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "example.com",
	}))
	if len(out.Added) != 0 || len(out.AlreadySet) != 1 {
		t.Fatalf("example.com is what *.example.com already means: %+v", out)
	}
	if got := api.getCfg().Sets[0].Targets.SNIDomains; len(got) != 1 {
		t.Errorf("the entry was duplicated: %v", got)
	}
}

func TestMCPEditTargetsRejectsMalformedDomains(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, bad := range []string{
		"https://example.com/path",
		"example.com:443",
		"user@example.com",
		"rutracker",
		`regexp:example\.com(`,
	} {
		if res := callEditTargets(t, session, ctx, map[string]any{
			"set": "video", "kind": "sni_domains", "add": bad,
		}); !res.IsError {
			t.Errorf("%q matches nothing on the packet path and must be refused", bad)
		}
	}

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": `regexp:^ad[0-9]+\.example\.com$`,
	}))
	if len(out.Added) != 1 {
		t.Errorf("a valid regexp entry must be accepted: %+v", out)
	}
}

func TestMCPEditTargetsWarnsOnCatchAll(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].Routing.Enabled = true
	cfg.Sets[0].Routing.Mode = "block"
	cfg.Sets[0].Targets.IPs = []string{"10.0.0.0/8"}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "ip", "add": "0.0.0.0/0",
	}))
	if !strings.Contains(out.Note, "catch-all") {
		t.Errorf("adding a catch-all must be called out: %q", out.Note)
	}
	if !strings.Contains(out.Note, "routing is enabled") {
		t.Errorf("a catch-all on a routing set blackholes traffic and must say so: %q", out.Note)
	}
}

func TestMCPEditTargetsWarnsWhenDevicesEmptied(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "source_devices", "remove": "AA:BB:CC:DD:EE:FF",
	}))
	if !strings.Contains(out.Note, "EVERY device") {
		t.Errorf("emptying the device list widens the set and must say so: %q", out.Note)
	}
}

func TestMCPEditTargetsWarnsWhenSetIsDisabled(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "disabled-set", "kind": "sni_domains", "add": "example.net",
	}))
	if !strings.Contains(out.Note, "DISABLED") {
		t.Errorf("adding to a disabled set matches nothing and must say so: %q", out.Note)
	}
}

func TestMCPEditTargetsRefusesDeviceIP(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	res := callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "source_devices", "add": "192.168.1.50",
	})
	if !res.IsError {
		t.Fatal("b4 scopes a set by MAC; an IP entry matches nothing and reaches nftables malformed")
	}
	if !strings.Contains(mcpErrorText(res), "MAC") {
		t.Errorf("the refusal should say what is expected: %q", mcpErrorText(res))
	}
}

func TestMCPEditTargetsDetectsAChangePastTheSummaryCutoff(t *testing.T) {
	long := []string{
		"aaaaaaaaaaaaaaaaaaaa.com", "bbbbbbbbbbbbbbbbbbbb.com",
		"cccccccccccccccccccc.com", "dddddddddddddddddddd.com",
		"eeeeeeeeeeeeeeeeeeee.com", "ffffffffffffffffffff.com",
	}
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].Targets.SNIDomains = append([]string(nil), long...)
	mcpResetHistory()
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	before := append([]string(nil), long...)
	if mcpSummarizeList(before) != mcpSummarizeList(append(before[:5:5], "gggggggggggggggggggg.com")) {
		t.Fatal("precondition: this fixture is meant to collide under the display summary")
	}

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains",
		"add": "gggggggggggggggggggg.com", "remove": "ffffffffffffffffffff.com",
	}))

	live := api.getCfg().Sets[0].Targets.SNIDomains
	if slices.Contains(live, "ffffffffffffffffffff.com") || !slices.Contains(live, "gggggggggggggggggggg.com") {
		t.Fatalf("precondition: the swap should have been saved, got %v", live)
	}
	if !out.Changed {
		t.Errorf("the swap was saved and applied, so reporting changed=false tells the model the opposite of what happened: %+v", out)
	}

	rev := decodeRevert(t, session, ctx)
	if !rev.Reverted {
		t.Fatalf("a saved target edit must be recorded for undo: %+v", rev)
	}
	if got := api.getCfg().Sets[0].Targets.SNIDomains; !slices.Equal(got, long) {
		t.Errorf("undo should restore the list the edit replaced, got %v", got)
	}
}

func TestMCPEditTargetsWarnsWhenAPortScopedSetLosesItsLastTarget(t *testing.T) {
	const otherDevice = "AA:BB:CC:DD:EE:01"
	for _, tc := range []struct {
		name    string
		prepare func(*config.Config)
		args    map[string]any
		want    []string
		reject  []string
		applies func(*sni.SuffixSet) bool
	}{
		{
			name:    "tcp port filter, last domain removed",
			prepare: func(c *config.Config) { c.Sets[0].TCP.DPortFilter = "5222" },
			args:    map[string]any{"set": "video", "kind": "sni_domains", "remove": "youtube.com"},
			want:    []string{"EVERY connection on TCP 5222", "from every device"},
			reject:  []string{"matches 0 domains"},
			applies: func(m *sni.SuffixSet) bool {
				ok, s := m.MatchTCPPort(5222, otherDevice)
				return ok && s.Name == "video"
			},
		},
		{
			name: "udp port filter, last address removed",
			prepare: func(c *config.Config) {
				c.Sets[0].Targets = config.TargetsConfig{IPs: []string{"10.0.0.0/8"}}
				c.Sets[0].UDP.DPortFilter = "50000-65535"
			},
			args:   map[string]any{"set": "video", "kind": "ip", "remove": "10.0.0.0/8"},
			want:   []string{"EVERY connection on UDP 50000-65535"},
			reject: []string{"matches 0 domains"},
			applies: func(m *sni.SuffixSet) bool {
				ok, s := m.MatchUDPPort(50001, otherDevice)
				return ok && s.Name == "video"
			},
		},
		{
			name: "device-scoped set",
			prepare: func(c *config.Config) {
				c.Sets[0].TCP.DPortFilter = "5222"
				c.Sets[0].Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
			},
			args:   map[string]any{"set": "video", "kind": "sni_domains", "remove": "youtube.com"},
			want:   []string{"EVERY connection on TCP 5222", "from the devices it allows"},
			reject: []string{"matches 0 domains", "from every device"},
		},
		{
			name:    "disabled set",
			prepare: func(c *config.Config) { c.Sets[1].TCP.DPortFilter = "5222" },
			args:    map[string]any{"set": "disabled-set", "kind": "sni_domains", "remove": "example.org"},
			want:    []string{"DISABLED", "once enabled it applies to EVERY connection on TCP 5222"},
			reject:  []string{"matches 0 domains"},
		},
		{
			name:    "no port filter",
			prepare: func(c *config.Config) {},
			args:    map[string]any{"set": "video", "kind": "sni_domains", "remove": "youtube.com"},
			want:    []string{"matches 0 domains and 0 addresses"},
			reject:  []string{"EVERY connection"},
		},
		{
			name: "a target is left",
			prepare: func(c *config.Config) {
				c.Sets[0].TCP.DPortFilter = "5222"
				c.Sets[0].Targets.SNIDomains = []string{"youtube.com", "example.net"}
			},
			args:   map[string]any{"set": "video", "kind": "sni_domains", "remove": "example.net"},
			want:   []string{"matches 1 domains"},
			reject: []string{"EVERY connection"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := geoTestCfg(t)
			cfg.System.WebServer.MCP.AllowWrites = true
			tc.prepare(cfg)
			srv, api := newMCPTestServerAPI(t, cfg)
			session, ctx := connectMCP(t, srv)

			out := decodeEditTargets(t, callEditTargets(t, session, ctx, tc.args))
			if !out.Changed {
				t.Fatalf("precondition: the edit should have been applied: %+v", out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out.Note, w) {
					t.Errorf("note should contain %q: %q", w, out.Note)
				}
			}
			for _, r := range tc.reject {
				if strings.Contains(out.Note, r) {
					t.Errorf("note must not contain %q: %q", r, out.Note)
				}
			}
			if tc.applies != nil && !tc.applies(sni.NewSuffixSet(api.getCfg().Sets)) {
				t.Error("precondition: the matcher should apply a set without destination targets to every connection on its ports")
			}
		})
	}
}

func TestMCPEditTargetsRefusesNonASCIIDomains(t *testing.T) {
	cfg := geoTestCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.Sets[0].Targets.SNIDomains = []string{"youtube.com", "bücher.de"}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	for _, bad := range []string{"пример.рф", "*.пример.рф", "example.net, ПРИМЕР.РФ"} {
		res := callEditTargets(t, session, ctx, map[string]any{
			"set": "video", "kind": "sni_domains", "add": bad,
		})
		if !res.IsError {
			t.Errorf("%q can never match: SNI carries a non-ASCII name only in its xn-- form", bad)
			continue
		}
		if msg := mcpErrorText(res); !strings.Contains(msg, "xn--") {
			t.Errorf("the refusal should ask for the xn-- form: %q", msg)
		}
	}
	if got := api.getCfg().Sets[0].Targets.SNIDomains; !slices.Equal(got, []string{"youtube.com", "bücher.de"}) {
		t.Fatalf("a refused add must leave the list untouched: %v", got)
	}

	out := decodeEditTargets(t, callEditTargets(t, session, ctx, map[string]any{
		"set": "video", "kind": "sni_domains", "add": "xn--e1afmkfd.xn--p1ai", "remove": "bücher.de",
	}))
	if !slices.Equal(out.Added, []string{"xn--e1afmkfd.xn--p1ai"}) || !slices.Equal(out.Removed, []string{"bücher.de"}) {
		t.Fatalf("the xn-- form must be accepted, and a non-ASCII entry already stored must stay removable: %+v", out)
	}
}

func TestMCPEditTargetsReportsTheSavedListNotAConcurrentWritersOne(t *testing.T) {
	for _, tc := range []struct {
		name  string
		other func(*config.Config)
	}{
		{"another writer saves a different list", func(c *config.Config) {
			c.Sets[0].Targets.IPs = []string{"9.9.9.9", "8.8.8.8"}
		}},
		{"another writer puts the old list back", func(c *config.Config) {
			c.Sets[0].Targets.IPs = nil
		}},
		{"another writer deletes the set", func(c *config.Config) {
			c.Sets = c.Sets[1:]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := geoTestCfg(t)
			cfg.System.WebServer.MCP.AllowWrites = true
			cfg.Sets[0].TCP.Duplicate.Enabled = true
			mcpResetHistory()
			t.Cleanup(mcpResetHistory)
			srv, api := newMCPTestServerAPI(t, cfg)
			session, ctx := connectMCP(t, srv)

			refreshes := 0
			prev := tablesRefreshFunc
			tablesRefreshFunc = func() error {
				refreshes++
				next := api.getCfg().Clone()
				tc.other(next)
				return api.saveAndPushConfig(next)
			}
			t.Cleanup(func() { tablesRefreshFunc = prev })

			res := callEditTargets(t, session, ctx, map[string]any{
				"set": "video", "kind": "ip", "add": "1.2.3.4",
			})
			if refreshes != 1 {
				t.Fatalf("precondition: a new address on a duplicating set refreshes the firewall once, got %d", refreshes)
			}
			out := decodeEditTargets(t, res)
			if !out.Changed || out.EntryCount != 1 || !strings.Contains(out.Note, "now holds 1 entries") {
				t.Errorf("b4 saved the one requested address, so that is what the result must report: %+v", out)
			}
			if len(mcpHistory) != 1 || mcpHistory[0].Current != "1.2.3.4" {
				currents := make([]string, 0, len(mcpHistory))
				for _, c := range mcpHistory {
					currents = append(currents, c.Current)
				}
				t.Errorf("the undo list must hold one entry recording the saved list, got %q", currents)
			}
		})
	}
}
