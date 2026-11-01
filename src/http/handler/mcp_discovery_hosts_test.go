package handler

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

func pageHost(host string, verdict discovery.HostVerdict, context discovery.HostContext) discovery.HostResult {
	return discovery.HostResult{HostCandidate: discovery.HostCandidate{Host: host, Source: discovery.HostFromPage, Context: context}, Verdict: verdict}
}

func svobodaPageHosts(preset string) *discovery.HostCheck {
	video := pageHost("www.youtube.com", discovery.HostNeeded, discovery.HostContextScript)
	video.Service = "youtube"
	beacon := pageHost("static.cloudflareinsights.com", discovery.HostNeeded, discovery.HostContextScript)
	beacon.Tracker, beacon.Blocking = true, true
	return &discovery.HostCheck{Preset: preset, Hosts: []discovery.HostResult{
		pageHost("docs.rferl.org", discovery.HostNeeded, discovery.HostContextStylesheet),
		pageHost("gdb.rferl.org", discovery.HostNeeded, discovery.HostContextImage),
		video,
		beacon,
		pageHost("magaz.global", discovery.HostNeeded, discovery.HostContextInline),
		pageHost("tags.svoboda.example", discovery.HostNotFixed, discovery.HostContextScript),
		pageHost("down.example.org", discovery.HostUnreachable, discovery.HostContextImage),
		pageHost("cdn.jsdelivr.net", discovery.HostLoadsWithout, discovery.HostContextScript),
	}}
}

func pageHostsRows(out mcpDiscoveryOut) map[string]mcpDiscoveryDomain {
	rows := map[string]mcpDiscoveryDomain{}
	for _, d := range out.Domains {
		rows[d.Domain] = d
	}
	return rows
}

func TestMCPDiscoveryVerdictNamesThePageHostsTheStrategyFixes(t *testing.T) {
	cfg := probeCfg(t)
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	combo := config.NewSetConfig()
	combo.Name = "combo"
	combo.Targets.SNIDomains = []string{"holod.media", "svoboda.org"}
	other := config.NewSetConfig()
	other.Name = "tls-sni"
	other.Targets.SNIDomains = []string{"meduza.io"}
	discovery.RegisterSuite(&discovery.CheckSuite{
		Id:     "page-hosts-run",
		Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"svoboda.org": {Domain: "svoboda.org", BestPreset: "combo", BestSuccess: true, Confirmed: 3, PageHosts: svobodaPageHosts("combo")},
			"holod.media": {Domain: "holod.media", BestPreset: "fake-sni", BestSuccess: true, Confirmed: 3, PageHosts: &discovery.HostCheck{
				Preset: "fake-sni", Hosts: []discovery.HostResult{pageHost("storage.ghost.io", discovery.HostNeeded, discovery.HostContextImage)},
			}},
			"meduza.io": {Domain: "meduza.io", BestPreset: "tls-sni", BestSuccess: true, Confirmed: 3},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "combo", Domains: []string{"holod.media", "svoboda.org"}, Set: &combo},
			{WinnerPreset: "tls-sni", Domains: []string{"meduza.io"}, Set: &other},
		},
	})

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": "page-hosts-run"}))
	rows := pageHostsRows(out)

	svoboda := rows["svoboda.org"].Verdict
	for _, want := range []string{
		"docs.rferl.org, gdb.rferl.org, static.cloudflareinsights.com, which are blocked without b4 and load with 'combo'",
		"b4_edit_set_targets (kind sni_domains)",
		"www.youtube.com (part of youtube",
		"magaz.global (only named in the page's code)",
		"does not tick them by default",
		"tags.svoboda.example on its page stays blocked even with 'combo'",
	} {
		if !strings.Contains(svoboda, want) {
			t.Errorf("the verdict of a page that needs other hosts must say %q: %q", want, svoboda)
		}
	}
	for _, never := range []string{"down.example.org", "cdn.jsdelivr.net"} {
		if strings.Contains(svoboda, never) {
			t.Errorf("%s is unreachable or loads without b4, so it is neither needed nor blocked and must not be named: %q", never, svoboda)
		}
	}
	if got := rows["holod.media"].Verdict; strings.Contains(got, "storage.ghost.io") {
		t.Errorf("those hosts were tested with 'fake-sni', not with 'combo', the strategy apply installs for the group: %q", got)
	}
	if got := rows["meduza.io"].Verdict; strings.Contains(got, "b4_edit_set_targets") {
		t.Errorf("a page with no host to add must not ask for one: %q", got)
	}
	if !strings.Contains(out.Note, "The page of svoboda.org also needs other hosts") {
		t.Errorf("the note must point at the verdict that names hosts apply leaves out: %q", out.Note)
	}

	row := rows["svoboda.org"]
	if got := mcpDiscoveryVerdict(row, true); strings.Contains(got, "docs.rferl.org") {
		t.Errorf("a strategy still being searched for has no settled hosts to name: %q", got)
	}
}

func TestMCPDiscoveryPageHostsAdviceAgreesWithItsCount(t *testing.T) {
	one := mcpPageHostsAdvice("its page also loads", []discovery.HostResult{pageHost("docs.rferl.org", discovery.HostNeeded, discovery.HostContextStylesheet)}, "combo", "")
	if !strings.Contains(one, "docs.rferl.org, which is blocked without b4 and loads with 'combo': apply does not add it, so add it with") {
		t.Errorf("one host reads in the singular: %q", one)
	}

	inline := mcpPageHostsAdvice("its page also loads", []discovery.HostResult{pageHost("t.me", discovery.HostNeeded, discovery.HostContextInline)}, "combo", ` to set "news"`)
	for _, want := range []string{"t.me (only named in the page's code)", "does not tick it by default", `add it to set "news" with b4_edit_set_targets`, "only if the user asks"} {
		if !strings.Contains(inline, want) {
			t.Errorf("a host the web interface does not tick must still be named, with how to add it and why to ask first (%q): %q", want, inline)
		}
	}

	many := make([]string, 0, mcpPageHostsListMax+3)
	for i := range mcpPageHostsListMax + 3 {
		many = append(many, strings.Repeat("a", i+1)+".example.org")
	}
	if got := mcpHostNames(many); !strings.HasSuffix(got, "and 3 more") {
		t.Errorf("a long list must stay bounded: %q", got)
	}
}

func TestMCPPageHostsOffersATrackerThePageWaitsFor(t *testing.T) {
	waited := pageHost("cdn.onesignal.com", discovery.HostNeeded, discovery.HostContextScript)
	waited.Tracker, waited.Blocking = true, true
	pixel := pageHost("mc.yandex.ru", discovery.HostNeeded, discovery.HostContextImage)
	pixel.Tracker = true

	advice := mcpPageHostsAdvice("its page also loads", []discovery.HostResult{waited, pixel}, "combo", "")
	if !strings.Contains(advice, "its page also loads cdn.onesignal.com, which is blocked without b4") {
		t.Errorf("a tracker the page waits for is offered like the page's own files, as the web interface ticks it: %q", advice)
	}
	if !strings.Contains(advice, "mc.yandex.ru (a tracker) also failed without b4") {
		t.Errorf("a tracker the page does not wait for stays unticked and is named apart: %q", advice)
	}
}

func TestMCPDiscoveryPageHostsStepIsNoLongerASearch(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "combo"
	set.Targets.SNIDomains = []string{"rutracker.org"}
	suite := &discovery.CheckSuite{
		Id:              "hosts-step-run",
		Status:          discovery.CheckStatusRunning,
		CurrentPhase:    discovery.PhaseHosts,
		CurrentDomain:   "rutracker.org",
		TotalChecks:     40,
		CompletedChecks: 40,
		HostsTotal:      7,
		HostsDone:       3,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"rutracker.org": {Domain: "rutracker.org", BestPreset: "combo", BestSuccess: true, Confirmed: 3, ConfirmTries: 3},
		},
		StrategyGroups: []discovery.StrategyGroup{{WinnerPreset: "combo", Domains: []string{"rutracker.org"}, Set: &set}},
	}
	discovery.RegisterSuite(suite)
	t.Cleanup(func() { _ = discovery.CancelCheckSuite(suite.Id) })

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": suite.Id}))
	if len(out.Domains) != 1 {
		t.Fatalf("expected one row, got %+v", out.Domains)
	}
	row := out.Domains[0]
	if row.Provisional || strings.Contains(row.Verdict, "PROVISIONAL") || !strings.Contains(row.Verdict, "working strategy was found") {
		t.Errorf("the search and its confirmation are over in page_hosts, so the strategy is no longer provisional: %+v", row)
	}
	for _, want := range []string{"confirmation pass has run", "cancel skips only this step and keeps every result", "at most about two minutes"} {
		if !strings.Contains(out.Note, want) {
			t.Errorf("the note must say %q while the run checks page hosts: %q", want, out.Note)
		}
	}
	if strings.Contains(out.Note, "BEST SO FAR") || strings.Contains(out.Note, "still running on") {
		t.Errorf("the note must not describe a search that is over: %q", out.Note)
	}
	if out.Phase != string(discovery.PhaseHosts) || out.Progress != "3/7 host checks" {
		t.Errorf("the step and its own progress must show, got phase %q and progress %q", out.Phase, out.Progress)
	}

	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "rutracker.org", "id": suite.Id})
	msg := mcpErrorText(res)
	if !res.IsError || !strings.Contains(msg, "skip that step") || !strings.Contains(msg, "every result kept") {
		t.Errorf("apply must wait for the run to end and say that cancel only skips the step: %q", msg)
	}
	if strings.Contains(msg, "unconfirmed") || strings.Contains(msg, "best so far") {
		t.Errorf("the strategies are confirmed by then, and the refusal must not say otherwise: %q", msg)
	}
}

func TestMCPDiscoverySetApplyWaitsForThePageHostsStep(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	news := config.NewSetConfig()
	news.Id, news.Name, news.Enabled = "news", "news", true
	news.Targets.SNIDomains = []string{"svoboda.org"}
	cfg.Sets = append(cfg.Sets, &news)
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	suite := &discovery.CheckSuite{Id: "set-hosts-step", Status: discovery.CheckStatusRunning, CurrentPhase: discovery.PhaseHosts, SetId: "news"}
	discovery.RegisterSuite(suite)
	t.Cleanup(func() { _ = discovery.CancelCheckSuite(suite.Id) })

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": suite.Id}))
	if !strings.Contains(out.Note, "The set verdict is published when the run ends") {
		t.Errorf("a set run's verdict reads pending during the step, and the note must say when it comes: %q", out.Note)
	}

	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "set": "news", "id": suite.Id})
	if !res.IsError || !strings.Contains(mcpErrorText(res), "skip that step") {
		t.Errorf("a set run in page_hosts must be refused with the step's own reason: %q", mcpErrorText(res))
	}
}

func TestMCPDiscoveryCancelDuringPageHostsSkipsOnlyTheStep(t *testing.T) {
	snap := func(status discovery.CheckStatus, phase discovery.DiscoveryPhase) *mcpSuiteSnapshot {
		return &mcpSuiteSnapshot{Status: string(status), CurrentPhase: string(phase)}
	}
	running, complete, canceled := discovery.CheckStatusRunning, discovery.CheckStatusComplete, discovery.CheckStatusCanceled
	for _, c := range []struct {
		name          string
		before, after *mcpSuiteSnapshot
		skipped       bool
	}{
		{"skipped while the step runs", snap(running, discovery.PhaseHosts), snap(running, discovery.PhaseHosts), true},
		{"the step ended and the run completed meanwhile", snap(running, discovery.PhaseHosts), snap(complete, discovery.PhaseHosts), true},
		{"the step began just before the cancel landed", snap(running, discovery.PhaseConfirm), snap(running, discovery.PhaseHosts), true},
		{"cancelled during the search", snap(running, discovery.PhaseStrategy), snap(canceled, discovery.PhaseStrategy), false},
		{"the firewall rules were lost during the step", snap(running, discovery.PhaseHosts), snap(canceled, discovery.PhaseHosts), false},
		{"a run that had already finished", snap(complete, discovery.PhaseHosts), snap(complete, discovery.PhaseHosts), false},
		{"a run that left memory", snap(running, discovery.PhaseHosts), nil, false},
		{"a run never seen", nil, snap(running, discovery.PhaseHosts), false},
	} {
		if got := mcpCancelSkippedPageHosts(c.before, c.after); got != c.skipped {
			t.Errorf("%s: skipped only the page hosts step = %v, want %v", c.name, got, c.skipped)
		}
	}
	if strings.Contains(mcpPageHostsSkipNote, "partial") || !strings.Contains(mcpPageHostsSkipNote, "every result kept") {
		t.Errorf("a cancel in page_hosts keeps a finished, confirmed run, and the note must say so: %q", mcpPageHostsSkipNote)
	}
}

func TestMCPDiscoveryApplyNamesThePageHostsItLeavesOut(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "combo"
	set.Targets.SNIDomains = []string{"holod.media", "svoboda.org"}
	holod := &discovery.HostCheck{Preset: "combo", Hosts: []discovery.HostResult{
		pageHost("storage.ghost.io", discovery.HostNeeded, discovery.HostContextImage),
		pageHost("docs.rferl.org", discovery.HostNeeded, discovery.HostContextStylesheet),
	}}
	suite := &discovery.CheckSuite{
		Id:     "apply-hosts-run",
		Status: discovery.CheckStatusComplete,
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"svoboda.org": {Domain: "svoboda.org", BestPreset: "combo", BestSuccess: true, Confirmed: 3, PageHosts: svobodaPageHosts("combo")},
			"holod.media": {Domain: "holod.media", BestPreset: "combo", BestSuccess: true, Confirmed: 3, PageHosts: holod},
		},
		StrategyGroups: []discovery.StrategyGroup{{WinnerPreset: "combo", Domains: []string{"holod.media", "svoboda.org"}, Set: &set}},
	}
	discovery.RegisterSuite(suite)
	t.Cleanup(func() { mcpRememberSuite("") })

	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "svoboda.org", "id": suite.Id}))
	if !applied.Changed || applied.Applied == nil {
		t.Fatalf("apply should create the set: %+v", applied)
	}
	for _, want := range []string{
		"Their pages also load storage.ghost.io, docs.rferl.org, gdb.rferl.org, static.cloudflareinsights.com, which are blocked without b4",
		`add them to set "combo" with b4_edit_set_targets (kind sni_domains)`,
		"www.youtube.com (part of youtube",
	} {
		if !strings.Contains(applied.Note, want) {
			t.Errorf("the apply note must name the hosts the new set leaves out, each once (%q): %q", want, applied.Note)
		}
	}
	if strings.Count(applied.Note, "docs.rferl.org") != 1 {
		t.Errorf("a host both pages load is named once: %q", applied.Note)
	}
	for _, s := range api.getCfg().Sets {
		if s.Id == applied.Applied.Id && slices.Contains(s.Targets.SNIDomains, "docs.rferl.org") {
			t.Errorf("MCP apply must not add page hosts, b4_edit_set_targets does: %v", s.Targets.SNIDomains)
		}
	}

	again := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "holod.media", "id": suite.Id}))
	if again.Changed || !strings.Contains(again.Note, `add them to set "combo"`) {
		t.Errorf("the set already there still lacks the hosts, so the note must name it as the place to add them: %+v", again)
	}
}

func TestMCPDiscoveryHistoryNamesThePageHosts(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "combo"
	set.Targets.SNIDomains = []string{"svoboda.org"}
	discovery.SaveToHistory(&discovery.CheckSuite{
		Id: "saved-hosts-run", Status: discovery.CheckStatusComplete, EndTime: time.Now(),
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"svoboda.org": {Domain: "svoboda.org", BestPreset: "combo", BestSuccess: true, Confirmed: 3, PageHosts: svobodaPageHosts("combo")},
		},
		StrategyGroups: []discovery.StrategyGroup{{WinnerPreset: "combo", Domains: []string{"svoboda.org"}, Set: &set}},
	}, cfg.ConfigPath)
	mcpRememberSuite("")
	t.Cleanup(func() { mcpRememberSuite("") })

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "id": "saved-hosts-run"}))
	if out.Source != "history" || len(out.Domains) != 1 {
		t.Fatalf("precondition: the run is read from history, got %+v", out)
	}
	if got := out.Domains[0].Verdict; !strings.Contains(got, "docs.rferl.org, gdb.rferl.org, static.cloudflareinsights.com, which are blocked without b4") || !strings.Contains(got, "tags.svoboda.example on its page stays blocked") {
		t.Errorf("history keeps the hosts to add and the ones still blocked, and its verdict must name them: %q", got)
	}
	if !strings.Contains(out.Note, "The page of svoboda.org also needs other hosts") {
		t.Errorf("the history note must point at them too: %q", out.Note)
	}

	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "svoboda.org"}))
	if !applied.Changed || !strings.Contains(applied.Note, "Its page also loads docs.rferl.org, gdb.rferl.org") {
		t.Errorf("an apply from history must name the hosts the set leaves out: %+v", applied)
	}
}
