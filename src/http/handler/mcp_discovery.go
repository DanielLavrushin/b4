package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpDiscoveryMaxDomains = 5
	mcpDiscoveryMaxGroups  = 20
)

var (
	mcpLastSuiteMu sync.Mutex
	mcpLastSuiteID string
)

func mcpRememberSuite(id string) {
	mcpLastSuiteMu.Lock()
	mcpLastSuiteID = id
	mcpLastSuiteMu.Unlock()
}

// mcpResolveSuiteID prefers an explicit id, then the run in progress, then the
// last run this server started. GetCurrentSuite only reports running or pending
// suites, but a FINISHED suite is the one that can be applied, and it stays in
// memory for another 30 seconds.
func mcpResolveSuiteID(explicit string) string {
	if id := strings.TrimSpace(explicit); id != "" {
		return id
	}
	if cur, ok := discovery.GetCurrentSuite(); ok && cur != nil {
		return cur.Id
	}
	mcpLastSuiteMu.Lock()
	defer mcpLastSuiteMu.Unlock()
	return mcpLastSuiteID
}

type mcpDiscoveryIn struct {
	Action  string `json:"action" jsonschema:"One of: start, status, cancel, apply."`
	Domains string `json:"domains,omitempty" jsonschema:"Comma-separated domains to find a strategy for, at most 5. For action=start."`
	Id      string `json:"id,omitempty" jsonschema:"Suite id from start. If omitted, the run in progress is used, then the last run this server started."`
	Domain  string `json:"domain,omitempty" jsonschema:"For action=apply: which domain's winning strategy to turn into a set."`
	Name    string `json:"name,omitempty" jsonschema:"Optional name for the set created by apply."`
	SkipDNS string `json:"skip_dns,omitempty" jsonschema:"'true' to skip the DNS poisoning probe on start."`
}

type mcpSuiteSnapshot struct {
	Id              string `json:"id"`
	Status          string `json:"status"`
	CurrentPhase    string `json:"current_phase"`
	CurrentDomain   string `json:"current_domain"`
	TotalChecks     int    `json:"total_checks"`
	CompletedChecks int    `json:"completed_checks"`
	DomainResults   map[string]struct {
		Domain        string  `json:"domain"`
		BestPreset    string  `json:"best_preset"`
		BestSpeed     float64 `json:"best_speed"`
		BestSuccess   bool    `json:"best_success"`
		BaselineWorks bool    `json:"baseline_works"`
		Confirmed     int     `json:"confirmed"`
		DNSResult     *struct {
			IsPoisoned       bool `json:"is_poisoned"`
			TransportBlocked bool `json:"transport_blocked"`
		} `json:"dns_result"`
	} `json:"domain_discovery_results"`
	StrategyGroups []struct {
		WinnerPreset string            `json:"winner_preset"`
		Family       string            `json:"family"`
		Domains      []string          `json:"domains"`
		Set          *config.SetConfig `json:"set"`
	} `json:"strategy_groups"`
}

func mcpSuiteProjection(suite *discovery.CheckSuite) (*mcpSuiteSnapshot, error) {
	raw, err := json.Marshal(suite)
	if err != nil {
		return nil, fmt.Errorf("read discovery run: %w", err)
	}
	var snap mcpSuiteSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("decode discovery run: %w", err)
	}
	return &snap, nil
}

type mcpDiscoveryDomain struct {
	Domain        string  `json:"domain"`
	BestPreset    string  `json:"best_preset,omitempty"`
	Family        string  `json:"family,omitempty"`
	BestKBPerSec  float64 `json:"best_kb_per_sec,omitempty"`
	Found         bool    `json:"strategy_found"`
	BaselineWorks bool    `json:"works_without_b4"`
	DNSPoisoned   bool    `json:"dns_poisoned,omitempty"`
	Blocked       bool    `json:"transport_blocked,omitempty"`
	Confirmed     int     `json:"confirmed,omitempty"`
	Verdict       string  `json:"verdict"`
}

type mcpDiscoveryOut struct {
	Id        string               `json:"suite_id,omitempty"`
	Status    string               `json:"status,omitempty"`
	Phase     string               `json:"phase,omitempty"`
	Progress  string               `json:"progress,omitempty"`
	Source    string               `json:"source,omitempty"`
	Domains   []mcpDiscoveryDomain `json:"domains,omitempty"`
	Applied   *mcpSetRow           `json:"applied_set,omitempty"`
	MovedFrom []DomainReassignment `json:"moved_from_other_sets,omitempty"`
	Changed   bool                 `json:"changed,omitempty"`
	Note      string               `json:"note"`
}

func mcpDiscoveryVerdict(d mcpDiscoveryDomain) string {
	switch {
	case d.BaselineWorks:
		return "works without b4 — do not create a set for it"
	case d.Blocked:
		return "the address itself is unreachable, so no packet strategy can help; only a proxy or VPN route would"
	case d.Found:
		return fmt.Sprintf("a working strategy was found (%s)", d.BestPreset)
	default:
		return "no strategy tried made it work"
	}
}

func (api *API) mcpDiscoverySuiteRows(snap *mcpSuiteSnapshot) []mcpDiscoveryDomain {
	family := map[string]string{}
	for _, g := range snap.StrategyGroups {
		for _, d := range g.Domains {
			family[sni.NormalizeDomain(d)] = g.Family
		}
	}

	rows := make([]mcpDiscoveryDomain, 0, len(snap.DomainResults))
	for key, r := range snap.DomainResults {
		name := r.Domain
		if name == "" {
			name = key
		}
		row := mcpDiscoveryDomain{
			Domain:        name,
			BestPreset:    r.BestPreset,
			Family:        family[sni.NormalizeDomain(name)],
			BestKBPerSec:  r.BestSpeed / 1024,
			Found:         r.BestSuccess && !r.BaselineWorks,
			BaselineWorks: r.BaselineWorks,
			Confirmed:     r.Confirmed,
		}
		if r.DNSResult != nil {
			row.DNSPoisoned = r.DNSResult.IsPoisoned
			row.Blocked = r.DNSResult.TransportBlocked
		}
		row.Verdict = mcpDiscoveryVerdict(row)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Domain < rows[j].Domain })
	return rows
}

func (api *API) addMCPDiscoveryTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_find_bypass_strategy",
		Title: "Search for a working bypass strategy",
		Description: "Brute-forces b4's strategies against a domain until one makes it load, then turns the winner into a set. " +
			"action=start begins a run and returns immediately; status polls it; cancel stops it; apply creates the set. " +
			"A run takes MINUTES, emits heavy traffic from the router, holds firewall rules, and only one can be in flight — do not poll in a loop, tell the user it is running and check once when they ask. " +
			"Results leave memory 30 seconds after a run ends, and the applicable set is lost with them, so apply promptly.",
		Annotations: mcpProbe,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
		action := strings.ToLower(strings.TrimSpace(in.Action))
		switch action {
		case "start":
			return api.mcpDiscoveryStart(in)
		case "status":
			return api.mcpDiscoveryStatus(in)
		case "cancel":
			return api.mcpDiscoveryCancel(in)
		case "apply":
			return api.mcpDiscoveryApply(in)
		case "":
			return nil, mcpDiscoveryOut{}, fmt.Errorf("action is required: start, status, cancel or apply")
		default:
			return nil, mcpDiscoveryOut{}, fmt.Errorf("unknown action %q: expected start, status, cancel or apply", action)
		}
	})
}

func (api *API) mcpDiscoveryStart(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	cfg := api.getCfg()
	if !cfg.System.WebServer.MCP.AllowActiveProbes {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"active probes are disabled: a discovery run is the heaviest traffic b4 can generate, so it needs 'Allow active probes' under Settings -> Integrations -> MCP server")
	}
	if api.discoveryRT == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("the discovery runtime is not configured in this process")
	}

	var urls []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(in.Domains, ",") {
		host := sni.NormalizeDomain(raw)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		urls = append(urls, host)
	}
	if len(urls) == 0 {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("domains is required for action=start")
	}
	if len(urls) > mcpDiscoveryMaxDomains {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"at most %d domains per run; each one multiplies the traffic and the time", mcpDiscoveryMaxDomains)
	}

	opts := discovery.StartSuiteOptions{ValidationTries: 1}
	if strings.EqualFold(strings.TrimSpace(in.SkipDNS), "true") {
		opts.SkipDNS = true
	}

	suite, err := api.discoveryRT.StartSuite(cfg, urls, opts)
	if err != nil {
		if errors.Is(err, discovery.ErrDiscoveryAlreadyRunning) {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"a discovery run is already in progress — only one can run at a time, and the watchdog's self-healing shares the same runtime. Poll it with action=status, or stop it with action=cancel")
		}
		return nil, mcpDiscoveryOut{}, fmt.Errorf("could not start discovery: %w", err)
	}

	mcpRememberSuite(suite.Id)
	log.Infof("mcp: discovery started for %v (suite %s)", urls, suite.Id)
	out := mcpDiscoveryOut{
		Id:     suite.Id,
		Status: string(suite.Status),
		Note: fmt.Sprintf(
			"started for %s. This runs for minutes, not seconds: about %d strategies per domain, each preceded by a config-propagation pause. "+
				"Do NOT poll in a loop — tell the user it is running and call action=status once when they ask. "+
				"While it runs, the watchdog cannot heal and a firewall refresh will block.",
			strings.Join(urls, ", "), len(discovery.GetPhase1Presets())+15),
	}
	return nil, out, nil
}

func (api *API) mcpDiscoveryStatus(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	id := mcpResolveSuiteID(in.Id)
	if id != "" {
		if suite, ok := discovery.GetCheckSuite(id); ok && suite != nil {
			return api.mcpDiscoverySuiteStatus(suite)
		}
	}

	hist := discovery.GetHistory(api.getCfg().ConfigPath)
	if hist == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("no run with id %q is in memory and no history is available", id)
	}
	out := mcpDiscoveryOut{Source: "history", Status: "complete"}
	for _, e := range hist.Entries {
		row := mcpDiscoveryDomain{
			Domain:        e.Domain,
			BestPreset:    e.BestPreset,
			Family:        string(e.BestFamily),
			BestKBPerSec:  e.BestSpeed / 1024,
			Found:         e.BestSuccess && !e.BaselineWorks,
			BaselineWorks: e.BaselineWorks,
			Confirmed:     e.Confirmed,
		}
		row.Verdict = mcpDiscoveryVerdict(row)
		out.Domains = append(out.Domains, row)
	}
	sort.Slice(out.Domains, func(i, j int) bool { return out.Domains[i].Domain < out.Domains[j].Domain })
	if len(out.Domains) > mcpDiscoveryMaxGroups {
		out.Domains = out.Domains[:mcpDiscoveryMaxGroups]
	}
	out.Note = "that run is no longer in memory, so this is the saved history — one entry per domain, newest result kept. " +
		"The set a run builds is NOT saved with it, so action=apply is only possible within 30 seconds of a run finishing; " +
		"re-run discovery for that domain to get an applicable strategy back."
	if len(out.Domains) == 0 {
		out.Note = "no run is in progress and nothing has been discovered yet"
	}
	return nil, out, nil
}

func (api *API) mcpDiscoverySuiteStatus(suite *discovery.CheckSuite) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	snap, err := mcpSuiteProjection(suite)
	if err != nil {
		return nil, mcpDiscoveryOut{}, err
	}
	out := mcpDiscoveryOut{
		Id:      snap.Id,
		Status:  snap.Status,
		Phase:   snap.CurrentPhase,
		Source:  "run",
		Domains: api.mcpDiscoverySuiteRows(snap),
	}
	if snap.TotalChecks > 0 {
		out.Progress = fmt.Sprintf("%d/%d checks", snap.CompletedChecks, snap.TotalChecks)
	}

	switch discovery.CheckStatus(snap.Status) {
	case discovery.CheckStatusRunning, discovery.CheckStatusPending:
		out.Note = fmt.Sprintf("still running (%s, %s). Do not poll in a loop: check again only when the user asks.",
			out.Phase, out.Progress)
		if snap.CurrentDomain != "" {
			out.Note = fmt.Sprintf("still running on %s (%s, %s). Do not poll in a loop: check again only when the user asks.",
				snap.CurrentDomain, out.Phase, out.Progress)
		}
	case discovery.CheckStatusCanceled:
		out.Note = "the run was cancelled; any results below are partial"
	default:
		applicable := 0
		for _, d := range out.Domains {
			if d.Found {
				applicable++
			}
		}
		if api.discoveryRT != nil && api.discoveryRT.IsActive() {
			out.Status = "finishing"
			out.Note = fmt.Sprintf("the search is done (%d of %d domain(s) have a strategy) but the run is still tearing down its firewall rules. "+
				"apply is refused until that finishes — call status once more in a few seconds.",
				applicable, len(out.Domains))
			break
		}
		out.Note = fmt.Sprintf("finished. %d of %d domain(s) have a strategy worth applying. "+
			"Apply within 30 seconds with action=apply, the domain and this suite_id — after that the set is gone and only the summary survives.",
			applicable, len(out.Domains))
	}
	return nil, out, nil
}

func (api *API) mcpDiscoveryCancel(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	id := mcpResolveSuiteID(in.Id)
	if id == "" {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("no discovery run is in progress")
	}
	if err := discovery.CancelCheckSuite(id); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("could not cancel %q: %w", id, err)
	}
	log.Infof("mcp: discovery run %s cancelled", id)
	return nil, mcpDiscoveryOut{
		Id:     id,
		Status: "canceled",
		Note: "cancellation requested. A run that had already finished is left alone, and the firewall rules it installed " +
			"are torn down once the run goroutine stops, which is not instant.",
	}, nil
}

func (api *API) mcpDiscoveryApply(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	if !api.getCfg().System.WebServer.MCP.AllowWrites {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> API -> MCP server to permit them")
	}
	domain := sni.NormalizeDomain(in.Domain)
	if domain == "" {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("domain is required for action=apply")
	}
	if api.discoveryRT != nil && api.discoveryRT.IsActive() {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"a discovery run is still tearing down: applying now would block on a firewall refresh for up to five minutes. Call action=status until it reports finished, then apply")
	}

	id := mcpResolveSuiteID(in.Id)
	suite, ok := discovery.GetCheckSuite(id)
	if !ok || suite == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"no discovery run with id %q is still in memory. The set a run builds is not saved to history, so it cannot be applied later — run discovery for %s again", id, domain)
	}

	snap, err := mcpSuiteProjection(suite)
	if err != nil {
		return nil, mcpDiscoveryOut{}, err
	}
	var chosen *config.SetConfig
	var preset string
	for _, g := range snap.StrategyGroups {
		for _, d := range g.Domains {
			if sni.NormalizeDomain(d) == domain && g.Set != nil {
				chosen, preset = g.Set, g.WinnerPreset
			}
		}
	}
	if chosen == nil {
		if r, ok := snap.DomainResults[domain]; ok {
			if r.BaselineWorks {
				return nil, mcpDiscoveryOut{}, fmt.Errorf(
					"%s works without b4, so the run deliberately produced no strategy for it. Creating a set would be wrong", domain)
			}
			if !r.BestSuccess {
				return nil, mcpDiscoveryOut{}, fmt.Errorf(
					"the run found no working strategy for %s, so there is nothing to apply", domain)
			}
		}
		return nil, mcpDiscoveryOut{}, fmt.Errorf("that run holds no applicable strategy for %s", domain)
	}

	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	set := *chosen
	set.Id = uuid.New().String()
	set.Enabled = true
	if name := strings.TrimSpace(in.Name); name != "" {
		set.Name = name
	} else if set.Name == "" {
		set.Name = domain
	}
	set.Routing.Upstream.Username = ""
	set.Routing.Upstream.Password = ""
	api.initializeSetDefaults(&set)

	moved := api.releaseDomainsFromOtherSets(newCfg.Sets, set.Id, set.Targets.SNIDomains)
	newCfg.Sets = append(newCfg.Sets, &set)
	api.loadTargetsForSetCached(&set)

	if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("rejected: %w", err)
	}
	snapshot := oldCfg.Clone()
	if err := api.saveAndPushConfig(newCfg); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("rejected: %w", err)
	}
	api.applyRuntimeChanges(newCfg, oldCfg)
	api.PerformSoftRestart(newCfg, oldCfg)

	live := api.getCfg()
	out := mcpDiscoveryOut{Id: snap.Id, Changed: true, MovedFrom: moved}
	for i, s := range live.Sets {
		if s.Id == set.Id {
			out.Applied = &mcpSetRow{Position: i + 1, Id: s.Id, Name: s.Name, Enabled: s.Enabled}
			break
		}
	}
	if out.Applied == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("set %q disappeared while saving", set.Name)
	}

	mcpRecordChange(mcpChange{
		Path:     "sets",
		Previous: fmt.Sprintf("%d sets", len(oldCfg.Sets)),
		Current:  fmt.Sprintf("%d sets", len(live.Sets)),
		When:     time.Now(), Snapshot: snapshot,
	})
	log.Infof("mcp: applied discovery strategy %q for %s as set %q", preset, domain, set.Name)

	out.Note = fmt.Sprintf("created set %q from the %s strategy, at position %d of %d — earlier sets match first. Undo with b4_revert_last_change",
		set.Name, preset, out.Applied.Position, len(live.Sets))
	if len(moved) > 0 {
		names := make([]string, 0, len(moved))
		for _, m := range moved {
			names = append(names, fmt.Sprintf("%s from %q", m.Domain, m.SetName))
		}
		out.Note += ". A domain belongs to one enabled set, so b4 took " + strings.Join(names, ", ")
	}
	out.Note += ". Confirm it with b4_test_domain_now before telling the user it is fixed"
	return nil, out, nil
}
