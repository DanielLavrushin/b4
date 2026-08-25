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
			modes = []string{"through_b4", "baseline"}
		case "through_b4":
			modes = []string{"through_b4"}
		case "baseline":
			modes = []string{"baseline"}
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
					opts := watchdog.ProbeOptions{Timeout: timeout}
					if mode == "baseline" {
						opts.Mark = cfg.MainInjectedMark()
					}
					log.Infof("mcp: probing %s (%s)", domain, mode)
					res, err := watchdog.ProbeHost(ctx, domain, opts)

					mu.Lock()
					defer mu.Unlock()
					var priv *watchdog.ErrPrivateDestination
					if errors.As(err, &priv) {
						out.Results = append(out.Results, mcpProbeResult{
							Domain: domain, Mode: mode,
							Verdict: string(netprobe.DomainDNSFake),
							Error: fmt.Sprintf("every address %s resolves to is private or local (%s), so the name is sinkholed: "+
								"the answer is being forged, and no packet strategy fixes that", domain, priv.Addr),
						})
						return
					}
					if err != nil {
						out.Results = append(out.Results, mcpProbeResult{
							Domain: domain, Mode: mode, Error: err.Error(),
						})
						return
					}
					out.Results = append(out.Results, mcpProbeResult{
						Domain:    domain,
						Mode:      mode,
						OK:        res.OK,
						Verdict:   string(res.Verdict),
						Error:     res.Error,
						KBPerSec:  res.Speed / 1024,
						BytesRead: res.BytesRead,
					})
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
				if out.Results[i].Mode == "through_b4" {
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
}

type mcpWatchdogDomain struct {
	Domain              string  `json:"domain"`
	Status              string  `json:"status"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	LastError           string  `json:"last_error,omitempty"`
	LastCheck           string  `json:"last_check,omitempty"`
	KBPerSec            float64 `json:"kb_per_sec,omitempty"`
	MatchedSet          string  `json:"matched_set,omitempty"`
	IntervalSec         int     `json:"interval_sec,omitempty"`
	CoolingDown         bool    `json:"cooling_down,omitempty"`
}

type mcpWatchdogOut struct {
	Enabled bool                `json:"enabled"`
	Domains []mcpWatchdogDomain `json:"domains,omitempty"`
	Changed bool                `json:"changed"`
	Note    string              `json:"note"`
}

func (api *API) addMCPWatchdogTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_watchdog",
		Title: "Read and edit the watchdog list",
		Description: "The watchdog fetches the domains it is given on a schedule and records whether each one worked. " +
			"action=status is the fastest answer to 'is my bypass working': it returns the last verdict, the error and the throughput per domain, without emitting any traffic, and needs no permission. " +
			"remove and disable need 'Allow configuration changes'. add, enable and check make the router fetch a site, so they need 'Allow active probes' as well. " +
			"Enabling the watchdog lets b4 REWRITE strategy sets on its own when a domain keeps failing.",
		Annotations: mcpDestructive,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpWatchdogIn) (*mcp.CallToolResult, mcpWatchdogOut, error) {
		action := strings.ToLower(strings.TrimSpace(in.Action))
		if action == "" {
			return nil, mcpWatchdogOut{}, fmt.Errorf("action is required: status, add, remove, enable, disable or check")
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
		if err := api.saveAndPushConfig(newCfg); err != nil {
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
		})
		log.Infof("mcp: watchdog %s (%d domains, enabled=%v)", action, len(live.Domains), live.Enabled)

		out.Enabled = live.Enabled
		out.Changed = true
		out.Note = fmt.Sprintf("applied live; the watchdog is %s and watching %d domain(s). Undo with b4_revert_last_change",
			map[bool]string{true: "on", false: "off"}[live.Enabled], len(live.Domains))
		if live.Enabled && action == "enable" {
			out.Note += ". While it is on, b4 may rewrite a set's strategy on its own when a watched domain keeps failing"
		}
		return nil, out, nil
	})
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
	now := time.Now()
	healthy, failing := 0, 0
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
			IntervalSec:         d.Interval,
			CoolingDown:         !d.CooldownUntil.IsZero() && now.Before(d.CooldownUntil),
		}
		if !d.LastCheck.IsZero() {
			row.LastCheck = d.LastCheck.UTC().Format(time.RFC3339)
		}
		if d.Status == watchdog.StatusHealthy {
			healthy++
		} else {
			failing++
		}
		out.Domains = append(out.Domains, row)
	}
	sort.Slice(out.Domains, func(i, j int) bool { return out.Domains[i].Domain < out.Domains[j].Domain })

	switch {
	case !out.Enabled:
		out.Note = fmt.Sprintf("the watchdog is off, so these %d verdict(s) are whatever was recorded before it stopped", len(out.Domains))
	case len(out.Domains) == 0:
		out.Note = "the watchdog is on but watching nothing; add a domain with action=add"
	case failing == 0:
		out.Note = fmt.Sprintf("all %d watched domain(s) were working at their last check", healthy)
	default:
		out.Note = fmt.Sprintf("%d of %d watched domain(s) are not working; 'last_error' says why and 'matched_set' names the set handling each",
			failing, len(out.Domains))
	}
	return nil, out, nil
}
