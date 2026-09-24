package handler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/utils"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpProbeMaxDomains = 3
	mcpProbeMaxTimeout = 20
)

var mcpProbe = &mcp.ToolAnnotations{
	ReadOnlyHint:    false,
	DestructiveHint: boolPtr(false),
	IdempotentHint:  false,
	OpenWorldHint:   boolPtr(true),
}

type mcpProbeIn struct {
	Domain     string `json:"domain" jsonschema:"Domain to fetch, e.g. rutracker.org. At most 3, comma-separated."`
	Mode       string `json:"mode,omitempty" jsonschema:"'both' (default) fetches through b4 and again with b4 bypassed; 'through_b4' or 'baseline' does one of them."`
	TimeoutSec int    `json:"timeout_sec,omitempty" jsonschema:"Per-fetch timeout in seconds. Default 10, maximum 20."`
}

type mcpProbeResult struct {
	Domain    string  `json:"domain"`
	Mode      string  `json:"mode"`
	OK        bool    `json:"ok"`
	Verdict   string  `json:"verdict,omitempty"`
	Error     string  `json:"error,omitempty"`
	KBPerSec  float64 `json:"kb_per_sec,omitempty"`
	BytesRead int64   `json:"bytes_read,omitempty"`
}

type mcpProbeOut struct {
	Results []mcpProbeResult `json:"results"`
	Note    string           `json:"note"`
}

const (
	probeModeThroughB4 = "through_b4"
	probeModeBaseline  = "baseline"
)

func probeDomain(ctx context.Context, cfg *config.Config, domain, mode string, timeout time.Duration) mcpProbeResult {
	opts := watchdog.ProbeOptions{Timeout: timeout}
	if mode == probeModeBaseline {
		opts.Mark = cfg.MainInjectedMark()
	}
	res, err := watchdog.ProbeHost(ctx, domain, opts)
	var priv *watchdog.ErrPrivateDestination
	if errors.As(err, &priv) {
		return mcpProbeResult{
			Domain: domain, Mode: mode,
			Verdict: string(netprobe.DomainDNSFake),
			Error: fmt.Sprintf("every address %s resolves to is private or local (%s), so the name is sinkholed: "+
				"the answer is being forged, and no packet strategy fixes that", domain, priv.Addr),
		}
	}
	if err != nil {
		return mcpProbeResult{Domain: domain, Mode: mode, Error: err.Error()}
	}
	return mcpProbeResult{
		Domain:    domain,
		Mode:      mode,
		OK:        res.OK,
		Verdict:   string(res.Verdict),
		Error:     res.Error,
		KBPerSec:  res.Speed / 1024,
		BytesRead: res.BytesRead,
	}
}

func probeDomainBothWays(ctx context.Context, cfg *config.Config, domain string, timeout time.Duration) (through, baseline mcpProbeResult) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		through = probeDomain(ctx, cfg, domain, probeModeThroughB4, timeout)
	}()
	go func() {
		defer wg.Done()
		baseline = probeDomain(ctx, cfg, domain, probeModeBaseline, timeout)
	}()
	wg.Wait()
	return through, baseline
}

func mcpProbeTimeout(v int) time.Duration {
	switch {
	case v <= 0:
		return 10 * time.Second
	case v > mcpProbeMaxTimeout:
		return mcpProbeMaxTimeout * time.Second
	default:
		return time.Duration(v) * time.Second
	}
}

func mcpProbeVerdictNote(domain string, through, baseline *mcpProbeResult) string {
	switch {
	case through == nil && baseline == nil:
		return ""
	case through == nil:
		if baseline.OK {
			return fmt.Sprintf("%s loads with b4 bypassed", domain)
		}
		return fmt.Sprintf("%s fails with b4 bypassed (%s)", domain, baseline.Verdict)
	case baseline == nil:
		if through.OK {
			return fmt.Sprintf("%s loads through b4", domain)
		}
		return fmt.Sprintf("%s fails through b4 (%s)", domain, through.Verdict)
	case through.OK && !baseline.OK:
		return fmt.Sprintf("%s is censored and b4's bypass is working: it fails without b4 (%s) and loads through it", domain, baseline.Verdict)
	case through.OK && baseline.OK:
		return fmt.Sprintf("%s loads either way, so nothing here is being blocked and no set is needed for it", domain)
	case !through.OK && baseline.OK:
		return fmt.Sprintf("%s loads with b4 bypassed but FAILS through b4 (%s) - a b4 setting is breaking it, not the censor", domain, through.Verdict)
	default:
		return fmt.Sprintf("%s fails both ways (%s through b4, %s bypassed), so this is not something a packet strategy fixes - the address itself may be blocked, or the site may be down", domain, through.Verdict, baseline.Verdict)
	}
}

func (api *API) addMCPProbeTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_test_domain_now",
		Title: "Fetch a domain and report what happened",
		Description: "Fetch a domain from the router right now, once through b4 and once with b4 bypassed, and report which of the two works. " +
			"This is the only tool that answers whether a site actually LOADS: b4_check_domain says a set targets it and b4_recent_connections says traffic arrived, neither says it works. " +
			"Emits real traffic from the router, so it needs 'Allow active probes'. Private and local addresses are refused.",
		Annotations: mcpProbe,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpProbeIn) (*mcp.CallToolResult, mcpProbeOut, error) {
		cfg := api.getCfg()
		if !cfg.System.WebServer.MCP.AllowActiveProbes {
			return nil, mcpProbeOut{}, fmt.Errorf(
				"active probes are disabled: turn on 'Allow active probes' under Settings -> Integrations -> MCP server to let b4 fetch a domain on request")
		}

		var domains []string
		seen := map[string]bool{}
		for _, raw := range strings.Split(in.Domain, ",") {
			host := watchdog.ExtractDomain(strings.TrimSpace(raw))
			if host == "" || seen[host] {
				continue
			}
			seen[host] = true
			domains = append(domains, host)
		}
		if len(domains) == 0 {
			return nil, mcpProbeOut{}, fmt.Errorf("domain is required")
		}
		if len(domains) > mcpProbeMaxDomains {
			return nil, mcpProbeOut{}, fmt.Errorf(
				"at most %d domains per call; each one is a real fetch", mcpProbeMaxDomains)
		}
		for _, host := range domains {
			if watchdog.IsReservedHost(host) {
				return nil, mcpProbeOut{}, fmt.Errorf(
					"%s is a private or local address: b4 probes the internet from the router, and fetching a LAN address would tell you about the network b4 runs on, not about censorship", host)
			}
		}

		var modes []string
		switch strings.ToLower(strings.TrimSpace(in.Mode)) {
		case "", "both":
			modes = []string{probeModeThroughB4, probeModeBaseline}
		case probeModeThroughB4:
			modes = []string{probeModeThroughB4}
		case probeModeBaseline:
			modes = []string{probeModeBaseline}
		default:
			return nil, mcpProbeOut{}, fmt.Errorf("unknown mode %q: expected both, through_b4 or baseline", in.Mode)
		}

		timeout := mcpProbeTimeout(in.TimeoutSec)
		out := mcpProbeOut{Results: make([]mcpProbeResult, 0, len(domains)*len(modes))}

		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, domain := range domains {
			for _, mode := range modes {
				wg.Add(1)
				go func(domain, mode string) {
					defer wg.Done()
					log.Infof("mcp: probing %s (%s)", domain, mode)
					result := probeDomain(ctx, cfg, domain, mode, timeout)
					mu.Lock()
					out.Results = append(out.Results, result)
					mu.Unlock()
				}(domain, mode)
			}
		}
		wg.Wait()

		if ctx.Err() != nil {
			return nil, mcpProbeOut{}, fmt.Errorf("the probe was cut short, so this is not a complete answer: %w", ctx.Err())
		}

		sort.Slice(out.Results, func(i, j int) bool {
			if out.Results[i].Domain != out.Results[j].Domain {
				return out.Results[i].Domain < out.Results[j].Domain
			}
			return out.Results[i].Mode < out.Results[j].Mode
		})

		var notes []string
		for _, domain := range domains {
			var through, baseline *mcpProbeResult
			for i := range out.Results {
				if out.Results[i].Domain != domain {
					continue
				}
				if out.Results[i].Mode == probeModeThroughB4 {
					through = &out.Results[i]
				} else {
					baseline = &out.Results[i]
				}
			}
			if n := mcpProbeVerdictNote(domain, through, baseline); n != "" {
				notes = append(notes, n)
			}
		}
		out.Note = strings.Join(notes, ". ")
		return nil, out, nil
	})
}

type mcpWatchdogIn struct {
	Action string `json:"action" jsonschema:"One of: status, add, remove, enable, disable, check."`
	Domain string `json:"domain,omitempty" jsonschema:"Domain, for add, remove and check."`
	Set    string `json:"set,omitempty" jsonschema:"Set id or exact name: act on that set's own watchdog."`
	URL    string `json:"url,omitempty" jsonschema:"With set: the discovery URL to add or remove."`
}

type mcpWatchdogDomain struct {
	Domain              string  `json:"domain"`
	Status              string  `json:"status"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	LastError           string  `json:"last_error,omitempty"`
	LastCheck           string  `json:"last_check,omitempty"`
	KBPerSec            float64 `json:"kb_per_sec,omitempty"`
	MatchedSet          string  `json:"matched_set,omitempty"`
	WatchedBySet        string  `json:"watched_by_set,omitempty"`
	IntervalSec         int     `json:"interval_sec,omitempty"`
	CoolingDown         bool    `json:"cooling_down,omitempty"`
}

type mcpWatchdogOut struct {
	Enabled bool                      `json:"enabled"`
	Domains []mcpWatchdogDomain       `json:"domains,omitempty"`
	Sets    []watchdog.SetWatchStatus `json:"sets,omitempty"`
	Set     *watchdog.SetWatchStatus  `json:"set,omitempty"`
	Changed bool                      `json:"changed"`
	Note    string                    `json:"note"`
}

func (api *API) addMCPWatchdogTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_watchdog",
		Title: "Read and edit the watchdog",
		Description: "Fetches watched targets on a schedule; when one keeps failing b4 REWRITES a set's strategy on its own. " +
			"Without set: the global domain list. With set: that set's own watchdog, which checks its discovery URLs, runs a discovery for the set on repeated failure, writes a confirmed strategy, verifies it and rolls back on failure. " +
			"status emits no traffic and needs no permission; without set it also lists watched sets. " +
			"remove and disable need 'Allow configuration changes'; add and enable also need 'Allow active probes'; check needs only 'Allow active probes'.",
		Annotations: mcpDestructive,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpWatchdogIn) (*mcp.CallToolResult, any, error) {
		res, out, err := api.mcpWatchdog(in)
		if err != nil {
			return nil, nil, err
		}
		return res, out, nil
	})
}

func (api *API) mcpWatchdog(in mcpWatchdogIn) (*mcp.CallToolResult, mcpWatchdogOut, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		return nil, mcpWatchdogOut{}, fmt.Errorf("action is required: status, add, remove, enable, disable or check")
	}
	if ref := strings.TrimSpace(in.Set); ref != "" {
		return api.mcpWatchdogSet(action, ref, in)
	}
	if action == "status" {
		return api.mcpWatchdogStatus()
	}

	mcpCfg := api.getCfg().System.WebServer.MCP
	if mcpWatchdogEmitsTraffic(action) && !mcpCfg.AllowActiveProbes {
		return nil, mcpWatchdogOut{}, fmt.Errorf(
			"active probes are disabled: %s makes the router fetch a site, so it needs 'Allow active probes' under Settings -> Integrations -> MCP server. "+
				"action=status reports the verdicts already recorded and needs no permission", action)
	}
	if action == "check" {
		return api.mcpWatchdogCheck(in.Domain)
	}
	if !mcpCfg.AllowWrites {
		return nil, mcpWatchdogOut{}, fmt.Errorf(
			"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> Integrations -> MCP server to permit them")
	}

	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()
	wd := &newCfg.System.Checker.Watchdog
	out := mcpWatchdogOut{}

	switch action {
	case "enable", "disable":
		want := action == "enable"
		if wd.Enabled == want {
			out.Enabled = want
			out.Note = fmt.Sprintf("the watchdog is already %sd", action)
			return nil, out, nil
		}
		wd.Enabled = want

	case "add", "remove":
		domain := strings.ToLower(watchdog.ExtractDomain(strings.TrimSpace(in.Domain)))
		if domain == "" {
			return nil, mcpWatchdogOut{}, fmt.Errorf("domain is required for action=%s", action)
		}
		if action == "add" && watchdog.IsReservedHost(domain) {
			return nil, mcpWatchdogOut{}, fmt.Errorf(
				"%s is a private or local address: the watchdog would fetch it from the router on a timer, which reports on the network b4 runs on rather than on censorship", domain)
		}
		idx := -1
		for i, d := range wd.Domains {
			if strings.EqualFold(watchdog.ExtractDomain(d), domain) {
				idx = i
				break
			}
		}
		if action == "add" {
			if idx >= 0 {
				out.Enabled = wd.Enabled
				out.Note = fmt.Sprintf("%s is already watched", domain)
				return nil, out, nil
			}
			wd.Domains = append(wd.Domains, domain)
		} else {
			if idx < 0 {
				out.Enabled = wd.Enabled
				out.Note = fmt.Sprintf("%s is not watched", domain)
				return nil, out, nil
			}
			wd.Domains = append(wd.Domains[:idx], wd.Domains[idx+1:]...)
		}

	default:
		return nil, mcpWatchdogOut{}, fmt.Errorf("unknown action %q: expected status, add, remove, enable, disable or check", action)
	}

	snapshot := oldCfg.Clone()
	if err := api.mcpSave(oldCfg, newCfg); err != nil {
		return nil, mcpWatchdogOut{}, fmt.Errorf("rejected: %w", err)
	}
	api.applyRuntimeChanges(newCfg, oldCfg)
	api.PerformSoftRestart(newCfg, oldCfg)

	live := api.getCfg().System.Checker.Watchdog
	mcpRecordChange(mcpChange{
		Path:     "system.checker.watchdog",
		Previous: mcpWatchdogSummary(oldCfg),
		Current:  mcpWatchdogSummary(api.getCfg()),
		When:     time.Now(), Snapshot: snapshot,
	}, oldCfg, newCfg)
	log.Infof("mcp: watchdog %s (%d domains, enabled=%v)", action, len(live.Domains), live.Enabled)

	out.Enabled = live.Enabled
	out.Changed = true
	out.Note = fmt.Sprintf("applied live; the watchdog is %s and watching %d domain(s). Undo with b4_revert_last_change",
		map[bool]string{true: "on", false: "off"}[live.Enabled], len(live.Domains))
	if live.Enabled && action == "enable" {
		out.Note += ". While it is on, b4 may rewrite a set's strategy on its own when a watched domain keeps failing"
	}
	return nil, out, nil
}

func mcpWatchdogEmitsTraffic(action string) bool {
	switch action {
	case "add", "enable", "check":
		return true
	}
	return false
}

func (api *API) mcpWatchdogCheck(raw string) (*mcp.CallToolResult, mcpWatchdogOut, error) {
	want := strings.ToLower(watchdog.ExtractDomain(strings.TrimSpace(raw)))
	if want == "" {
		return nil, mcpWatchdogOut{}, fmt.Errorf("domain is required for action=check")
	}
	watched := api.getCfg().System.Checker.Watchdog.Domains
	stored := ""
	for _, d := range watched {
		if strings.EqualFold(d, want) || strings.EqualFold(watchdog.ExtractDomain(d), want) {
			stored = d
			break
		}
	}
	if stored == "" {
		return nil, mcpWatchdogOut{}, fmt.Errorf(
			"%s is not watched, and check only re-tests a domain that already is: %s. Add it with action=add, or fetch it once with b4_test_domain_now",
			want, mcpSummarizeList(watched))
	}

	if globalWatchdog == nil {
		return nil, mcpWatchdogOut{}, fmt.Errorf("the watchdog is not running, so there is nothing to re-check")
	}

	globalWatchdog.ForceCheck(stored)
	_, res, _ := api.mcpWatchdogStatus()
	res.Note = fmt.Sprintf("scheduled an out-of-band check of %s; call action=status again in a few seconds for the result", stored)
	for _, row := range res.Domains {
		if row.Domain == stored && row.WatchedBySet != "" {
			res.Note = fmt.Sprintf("nothing will happen: %s is checked through set %q, which has its own watchdog, so the global list entry is not fetched on its own. Use set=%q action=check instead",
				stored, row.WatchedBySet, row.WatchedBySet)
			return nil, res, nil
		}
	}
	if !res.Enabled {
		res.Note = fmt.Sprintf("nothing will happen until the watchdog master switch is turned on with action=enable: %s is not checked while it is off. Its cooldown was cleared", stored)
	}
	return nil, res, nil
}

func mcpWatchdogSummary(cfg *config.Config) string {
	wd := cfg.System.Checker.Watchdog
	return fmt.Sprintf("enabled=%v domains=%s", wd.Enabled, mcpSummarizeList(wd.Domains))
}

func (api *API) mcpWatchdogStatus() (*mcp.CallToolResult, mcpWatchdogOut, error) {
	cfg := api.getCfg()
	wd := cfg.System.Checker.Watchdog
	out := mcpWatchdogOut{Enabled: wd.Enabled}

	if globalWatchdog == nil {
		out.Note = "the watchdog is not running in this process, so no verdicts are available"
		if len(wd.Domains) > 0 {
			out.Note += fmt.Sprintf("; %d domain(s) are configured", len(wd.Domains))
		}
		return nil, out, nil
	}

	state := globalWatchdog.GetState()
	out.Enabled = state.Enabled
	out.Sets = state.Sets
	now := time.Now()
	healthy, failing, delegated := 0, 0, 0
	for _, d := range state.Domains {
		if d == nil {
			continue
		}
		row := mcpWatchdogDomain{
			Domain:              d.Domain,
			Status:              d.Status,
			ConsecutiveFailures: d.ConsecutiveFailures,
			LastError:           d.LastError,
			KBPerSec:            d.LastSpeed / 1024,
			MatchedSet:          d.MatchedSet,
			WatchedBySet:        d.WatchedBySetName,
			IntervalSec:         d.Interval,
			CoolingDown:         !d.CooldownUntil.IsZero() && now.Before(d.CooldownUntil),
		}
		if !d.LastCheck.IsZero() {
			row.LastCheck = d.LastCheck.UTC().Format(time.RFC3339)
		}
		switch {
		case d.WatchedBySetId != "":
			delegated++
		case d.Status == watchdog.StatusHealthy:
			healthy++
		default:
			failing++
		}
		out.Domains = append(out.Domains, row)
	}
	sort.Slice(out.Domains, func(i, j int) bool { return out.Domains[i].Domain < out.Domains[j].Domain })

	switch {
	case !out.Enabled:
		out.Note = fmt.Sprintf("the watchdog is off, so these %d verdict(s) are whatever was recorded before it stopped", len(out.Domains))
	case len(out.Domains) == 0 && len(state.Sets) > 0:
		out.Note = fmt.Sprintf("the global list is empty; %d set(s) are watched on their own, see 'sets'", len(state.Sets))
	case len(out.Domains) == 0:
		out.Note = "the watchdog is on but watching nothing; add a domain with action=add, or watch a set with set=<set> action=enable"
	case healthy+failing == 0:
		out.Note = fmt.Sprintf("all %d entry(ies) on the global list are checked through the set named in 'watched_by_set', which has its own watchdog; see 'sets'", delegated)
	case failing == 0:
		out.Note = fmt.Sprintf("all %d watched domain(s) were working at their last check", healthy)
	default:
		out.Note = fmt.Sprintf("%d of %d watched domain(s) are not working; 'last_error' says why and 'matched_set' names the set handling each",
			failing, healthy+failing)
	}
	if delegated > 0 && out.Enabled && healthy+failing > 0 {
		out.Note += fmt.Sprintf("; %d entry(ies) are checked through the set named in 'watched_by_set', which has its own watchdog, so their own rows are not updated", delegated)
	}
	return nil, out, nil
}

func mcpSetNotWatchedWhy(set *config.SetConfig) string {
	if !set.Discovery.Watchdog {
		return "its watchdog is off; turn it on with action=enable"
	}
	if blocker := set.WatchdogBlocker(); blocker != "" {
		return fmt.Sprintf("%s (%s)", config.WatchdogBlockerText(blocker), blocker)
	}
	return "it is not watched"
}

func mcpForceCheckNote(name, outcome string, cleared bool) string {
	switch outcome {
	case watchdog.ForceCheckNotWatched:
		return fmt.Sprintf("nothing will happen: set %q is not watched by the running watchdog", name)
	case watchdog.ForceCheckHealing:
		note := fmt.Sprintf("nothing will happen now: set %q is being healed, and the heal verifies the set itself when it ends", name)
		if cleared {
			note += "; its heal failures and any give-up were cleared"
		}
		return note
	case watchdog.ForceCheckMasterOff:
		note := fmt.Sprintf("nothing will happen until the watchdog master switch is turned on (action=enable without set): set %q is not checked while it is off. Its cooldown and failure count were cleared", name)
		if cleared {
			note += ", and so were its heal failures and any give-up"
		}
		return note
	}
	note := fmt.Sprintf("scheduled a check of every URL of set %q and cleared its cooldown and failure count", name)
	if cleared {
		note += ", its heal failures and any give-up"
	} else {
		note += "; a give-up and the heal failures stay, clearing them needs 'Allow configuration changes'"
	}
	return note + "; call action=status again in a few seconds for the result"
}

func mcpWatchdogSetSummary(set *config.SetConfig) string {
	if set == nil {
		return "deleted"
	}
	return fmt.Sprintf("watchdog=%v urls=%s", set.Discovery.Watchdog, mcpSummarizeList(set.Discovery.URLs))
}

func mcpWatchdogSetRow(set *config.SetConfig) *watchdog.SetWatchStatus {
	if globalWatchdog == nil || set == nil {
		return nil
	}
	st, ok := globalWatchdog.GetSetState(set.Id)
	if !ok {
		return nil
	}
	return &st
}

func (api *API) mcpWatchdogSet(action, ref string, in mcpWatchdogIn) (*mcp.CallToolResult, mcpWatchdogOut, error) {
	cfg := api.getCfg()
	target, err := mcpFindDiscoverySet(cfg, ref)
	if err != nil {
		return nil, mcpWatchdogOut{}, err
	}
	mcpCfg := cfg.System.WebServer.MCP
	out := mcpWatchdogOut{Enabled: cfg.System.Checker.Watchdog.Enabled}

	switch action {
	case "status":
		row := mcpWatchdogSetRow(target)
		out.Set = row
		switch {
		case !target.Discovery.Watchdog:
			out.Note = fmt.Sprintf("set %q is not watched on its own; its discovery URLs are %s", target.Name, mcpSummarizeList(target.Discovery.URLs))
		case !target.WatchdogActive():
			out.Note = fmt.Sprintf("set %q has its watchdog switched on but is not checked: %s", target.Name, mcpSetNotWatchedWhy(target))
		case row == nil:
			out.Note = fmt.Sprintf("set %q is watched, but the watchdog is not running in this process, so there are no verdicts", target.Name)
		case !out.Enabled:
			out.Note = fmt.Sprintf("set %q is watched, but the master switch is off, so nothing is checked", target.Name)
		default:
			out.Note = fmt.Sprintf("set %q is %s", target.Name, row.Status)
			if row.Reason != "" {
				out.Note += " (" + row.Reason + ")"
			}
			if row.LastError != "" {
				out.Note += ": " + row.LastError
			}
		}
		return nil, out, nil
	case "enable", "disable", "add", "remove", "check":
	default:
		return nil, mcpWatchdogOut{}, fmt.Errorf("unknown action %q: expected status, add, remove, enable, disable or check", action)
	}

	if mcpWatchdogEmitsTraffic(action) && !mcpCfg.AllowActiveProbes {
		return nil, mcpWatchdogOut{}, fmt.Errorf(
			"active probes are disabled: %s makes the router fetch a site, so it needs 'Allow active probes' under Settings -> Integrations -> MCP server. "+
				"action=status reports the verdicts already recorded and needs no permission", action)
	}

	if action == "check" {
		if !target.WatchdogActive() {
			return nil, mcpWatchdogOut{}, fmt.Errorf("set %q is not watched, so there is nothing to re-check: %s", target.Name, mcpSetNotWatchedWhy(target))
		}
		if globalWatchdog == nil {
			return nil, mcpWatchdogOut{}, fmt.Errorf("the watchdog is not running, so there is nothing to re-check")
		}
		outcome := globalWatchdog.ForceCheckSet(target.Id, mcpCfg.AllowWrites)
		out.Set = mcpWatchdogSetRow(target)
		out.Note = mcpForceCheckNote(target.Name, outcome, mcpCfg.AllowWrites)
		return nil, out, nil
	}

	if !mcpCfg.AllowWrites {
		return nil, mcpWatchdogOut{}, fmt.Errorf(
			"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> Integrations -> MCP server to permit them")
	}

	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()
	set := newCfg.GetSetById(target.Id)
	if set == nil {
		return nil, mcpWatchdogOut{}, fmt.Errorf("set %q disappeared while the change was prepared; retry", target.Name)
	}
	before := mcpWatchdogSetSummary(oldCfg.GetSetById(target.Id))
	wasWatched := set.Discovery.Watchdog

	switch action {
	case "enable", "disable":
		want := action == "enable"
		if want {
			if blocker := set.WatchdogBlocker(); blocker != "" {
				return nil, mcpWatchdogOut{}, fmt.Errorf("set %q cannot be watched (%s): %s", set.Name, blocker, config.WatchdogBlockerText(blocker))
			}
		}
		if set.Discovery.Watchdog == want {
			out.Set = mcpWatchdogSetRow(set)
			out.Note = fmt.Sprintf("the watchdog of set %q is already %s", set.Name, map[bool]string{true: "on", false: "off"}[want])
			return nil, out, nil
		}
		set.Discovery.Watchdog = want

	case "add", "remove":
		raw := strings.TrimSpace(in.URL)
		if raw == "" {
			raw = strings.TrimSpace(in.Domain)
		}
		if raw == "" {
			return nil, mcpWatchdogOut{}, fmt.Errorf("url is required for action=%s with set", action)
		}
		canonical, host, err := utils.NormalizeProbeURL(raw)
		if err != nil {
			if errors.Is(err, utils.ErrProbeURLReservedHost) || watchdog.IsReservedHost(raw) {
				return nil, mcpWatchdogOut{}, fmt.Errorf(
					"%s is a private or local address: the watchdog would fetch it from the router on a timer, which reports on the network b4 runs on rather than on censorship", raw)
			}
			return nil, mcpWatchdogOut{}, fmt.Errorf("%q cannot be a discovery URL: %v", raw, err)
		}
		idx := -1
		for i, existing := range set.Discovery.URLs {
			if _, existingHost, err := utils.NormalizeProbeURL(existing); existing == canonical || (err == nil && existingHost == host) {
				idx = i
				break
			}
		}
		if action == "add" {
			if idx >= 0 {
				out.Set = mcpWatchdogSetRow(set)
				out.Note = fmt.Sprintf("set %q already probes %s with %s", set.Name, host, set.Discovery.URLs[idx])
				return nil, out, nil
			}
			if len(set.Discovery.URLs) >= utils.MaxProbeURLs {
				return nil, mcpWatchdogOut{}, fmt.Errorf("set %q already has %d discovery URLs, the most it keeps; remove one first", set.Name, utils.MaxProbeURLs)
			}
			set.Discovery.URLs = append(set.Discovery.URLs, canonical)
		} else {
			if idx < 0 {
				out.Set = mcpWatchdogSetRow(set)
				out.Note = fmt.Sprintf("set %q does not probe %s", set.Name, host)
				return nil, out, nil
			}
			set.Discovery.URLs = append(set.Discovery.URLs[:idx], set.Discovery.URLs[idx+1:]...)
		}
	}

	if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
		return nil, mcpWatchdogOut{}, fmt.Errorf("rejected: %w", err)
	}

	snapshot := oldCfg.Clone()
	if err := api.mcpSave(oldCfg, newCfg); err != nil {
		return nil, mcpWatchdogOut{}, fmt.Errorf("rejected: %w", err)
	}
	api.applyRuntimeChanges(newCfg, oldCfg)
	api.PerformSoftRestart(newCfg, oldCfg)

	live := api.getCfg().GetSetById(target.Id)
	mcpRecordChange(mcpChange{
		Path:     fmt.Sprintf("sets[%s].discovery", target.Name),
		Previous: before,
		Current:  mcpWatchdogSetSummary(live),
		When:     time.Now(), Snapshot: snapshot,
	}, oldCfg, newCfg)
	log.Infof("mcp: set %q watchdog %s (%s)", target.Name, action, mcpWatchdogSetSummary(live))

	if action == "enable" && globalWatchdog != nil {
		globalWatchdog.ForceCheckSet(target.Id, true)
	}

	out.Changed = true
	out.Set = mcpWatchdogSetRow(live)
	out.Note = fmt.Sprintf("applied live; set %q: %s. Undo with b4_revert_last_change", target.Name, mcpWatchdogSetSummary(live))
	if live != nil && action == "remove" && wasWatched && len(live.Discovery.URLs) == 0 {
		out.Note += ". That was its last URL, so the set is not checked until a URL is added; its watchdog switch stays on"
	}
	if live != nil && live.Discovery.Watchdog && (action == "enable" || action == "add") {
		out.Note += ". While it is on, b4 may rewrite this set's strategy on its own when its URLs keep failing"
		if !out.Enabled {
			out.Note += "; the watchdog master switch is off, so nothing is checked until it is enabled"
		}
	}
	return nil, out, nil
}
